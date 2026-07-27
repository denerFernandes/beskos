// Ponto de entrada partilhado (desktop + mobile). O mobile_entry_point é usado pelo Android/iOS.
#[cfg(desktop)]
mod desktop;
mod sync_cmds;
// Ícone na barra de menu: só no desktop (no mobile não há barra de menu).
#[cfg(desktop)]
mod tray;

/// Rede de segurança entre passagens de sincronização; o watcher do sistema de ficheiros acorda o
/// motor antes disto sempre que a pasta muda.
const SYNC_INTERVAL_SECS: u64 = 30;

/// Conteúdo de ficheiros .vcf que o SO mandou abrir com o Beskos antes de a UI estar pronta (ex.:
/// arranque a frio ao dar duplo-clique num contacto). A UI drena isto ao montar (take_pending_vcards)
/// e importa os contactos. Guardamos o TEXTO, não o caminho — o webview não tem acesso ao disco.
#[derive(Default)]
struct PendingVcards(std::sync::Mutex<Vec<String>>);

/// Lê o conteúdo dos caminhos que terminam em .vcf (ignora flags e outros argumentos). O envelope
/// de argumentos varia por plataforma, por isso filtramos por extensão em vez de por posição.
fn read_vcards<I: IntoIterator<Item = String>>(paths: I) -> Vec<String> {
    let mut out = Vec::new();
    for p in paths {
        if p.to_lowercase().ends_with(".vcf") {
            if let Ok(text) = std::fs::read_to_string(&p) {
                if !text.trim().is_empty() {
                    out.push(text);
                }
            }
        }
    }
    out
}

/// Guarda os vCards na fila pendente, avisa a UI (evento) e traz a janela para a frente. Usado
/// quando a app JÁ corre (single-instance no Windows/Linux, RunEvent::Opened no macOS).
#[cfg(desktop)]
fn stash_vcards(app: &tauri::AppHandle, texts: Vec<String>) {
    if texts.is_empty() {
        return;
    }
    use tauri::{Emitter, Manager};
    if let Some(state) = app.try_state::<PendingVcards>() {
        state.0.lock().unwrap().extend(texts);
    }
    let _ = app.emit("vcard-pending", ());
    if let Some(w) = app.get_webview_window("main") {
        let _ = w.show();
        let _ = w.set_focus();
    }
}

/// Devolve e limpa os vCards pendentes. A UI chama isto ao montar e a cada evento "vcard-pending".
#[tauri::command]
fn take_pending_vcards(state: tauri::State<PendingVcards>) -> Vec<String> {
    let mut v = state.0.lock().unwrap();
    std::mem::take(&mut *v)
}

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    // O motor de sincronização vive DENTRO da app (não há segunda aplicação). Arranca já, mas só
    // sincroniza de facto quando houver credenciais gravadas — senão fica quieto à espera de a UI
    // chamar `sync_configure`.
    let engine = mailin_sync::engine::Engine::start(SYNC_INTERVAL_SECS);

    let tray_engine = std::sync::Arc::clone(&engine);

    let mut builder = tauri::Builder::default();
    // single-instance TEM de ser o primeiro plugin. Quando o SO abre um .vcf com a app já viva,
    // reencaminha os argumentos para cá (Windows/Linux) em vez de abrir uma segunda janela.
    #[cfg(desktop)]
    {
        builder = builder.plugin(tauri_plugin_single_instance::init(|app, argv, _cwd| {
            stash_vcards(app, read_vcards(argv));
        }));
    }

    let app = builder
        .plugin(tauri_plugin_opener::init())
        .plugin(tauri_plugin_notification::init())
        .plugin(tauri_plugin_dialog::init())
        .manage(PendingVcards::default())
        .setup(move |app| {
            #[cfg(desktop)]
            {
                use tauri::Manager;
                app.manage(desktop::Account::default());
                // Arranque a frio no Windows/Linux: o .vcf vem nos argumentos do processo. No macOS
                // chega pelo RunEvent::Opened (tratado abaixo), por isso aqui é um no-op.
                let opened = read_vcards(std::env::args());
                if !opened.is_empty() {
                    app.state::<PendingVcards>().0.lock().unwrap().extend(opened);
                }
                // Ícone na barra com o estado da sincronização + menu de ações.
                tray::setup(app.handle(), tray_engine)?;
                // "Definições…" no menu da aplicação, com o ⌘, que toda a gente espera no macOS.
                // Parte-se do menu por omissão para não perder Editar/Janela — sem eles, copiar e
                // colar deixariam de funcionar em toda a app.
                let menu = tauri::menu::Menu::default(app.handle())?;
                if let Some(first) = menu.items()?.first().and_then(|i| i.as_submenu().cloned()) {
                    let item = tauri::menu::MenuItem::with_id(
                        app.handle(),
                        "settings",
                        "Definições…",
                        true,
                        Some("CmdOrCtrl+,"),
                    )?;
                    first.insert(&item, 1)?;
                }
                app.set_menu(menu)?;
                app.on_menu_event(|app, event| {
                    if event.id().as_ref() == "settings" {
                        desktop::open_settings(app);
                    }
                });
            }
            Ok(())
        })
        .manage(sync_cmds::SyncEngine(engine))
        .invoke_handler(tauri::generate_handler![
            sync_cmds::sync_status,
            sync_cmds::sync_connect,
            sync_cmds::sync_add_folder,
            sync_cmds::sync_remove_folder,
            sync_cmds::sync_now,
            sync_cmds::sync_set_paused,
            sync_cmds::sync_bandwidth,
            sync_cmds::sync_set_bandwidth,
            take_pending_vcards,
            #[cfg(desktop)]
            desktop::set_account,
        ])
        .on_window_event(|window, event| {
            // Fechar a janela NÃO fecha a app: ela fica no tray a sincronizar (padrão dos clientes
            // de sync). Sair a sério é pelo menu do tray → "Sair".
            if let tauri::WindowEvent::CloseRequested { api, .. } = event {
                if window.label() == "main" {
                    api.prevent_close();
                    let _ = window.hide();
                    // A janela só ESCONDE — o webview continua vivo, por isso a UI não nota que o
                    // utilizador "fechou" a app. Avisamos quem precisa de encerrar coisas ao vivo
                    // (ex.: quem está a apresentar no Meet tem de acabar a apresentação).
                    // emit_to("main"): só o webview desta janela reage. Um emit global apanharia
                    // também as janelas dedicadas (sala do Meet, editor), que não foram fechadas.
                    use tauri::Emitter;
                    let _ = window.emit_to("main", "window-hidden", ());
                }
            }
        })
        .build(tauri::generate_context!())
        .expect("erro ao arrancar a aplicação Tauri");

    // Loop de eventos. No macOS, abrir um .vcf com a app (arranque a frio OU já viva) chega como
    // RunEvent::Opened com URLs file://. Convertemos para caminho, lemos e enfileiramos para a UI.
    app.run(move |_app_handle, _event| {
        #[cfg(target_os = "macos")]
        if let tauri::RunEvent::Opened { urls } = _event {
            let paths: Vec<String> = urls
                .iter()
                .filter_map(|u| u.to_file_path().ok())
                .map(|p| p.to_string_lossy().into_owned())
                .collect();
            stash_vcards(_app_handle, read_vcards(paths));
        }
    });
}
