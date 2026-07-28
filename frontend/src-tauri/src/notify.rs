//! Notificações do SO **com clique**: mostrar o aviso é só metade do trabalho — clicar nele tem de
//! abrir o elemento que o originou (o e-mail, a conversa, o ficheiro, o evento…).
//!
//! Porque não o plugin: o `tauri-plugin-notification` no desktop entrega a notificação e esquece-a
//! (`notify_rust::…show()` e pronto). O `onAction`/`onNotificationReceived` do plugin só existe no
//! Android/iOS. Daí este módulo: mostra a notificação, ESPERA pela interação numa thread própria e,
//! se a pessoa clicou, traz a janela principal à frente e emite `notif-open` com o link para a UI
//! navegar (mesmo contrato do `notificationclick` do service worker na web).
//!
//! Por plataforma:
//!  - macOS: `mac-notification-sys` direto, com `wait_for_click` — o `notify-rust` só espera quando
//!    a notificação tem botões, e não queremos um botão "Abrir" pendurado em cada aviso.
//!  - Windows/Linux: `notify-rust` (toast WinRT / D-Bus). No Linux o clique no corpo só chega se a
//!    notificação declarar a ação `default`.

use tauri::{AppHandle, Emitter, Manager, Runtime};

/// Mostra uma notificação do SO. `link` é a rota da app a abrir no clique (ex.:
/// `/mail/mail/inbox/<id>`); vazio = notificação sem destino (só avisa).
///
/// Devolve logo: a espera pela interação corre numa thread à parte, senão o bloqueio do
/// `wait_for_*` prendia o invoke (e, no macOS, o run loop principal).
#[tauri::command]
pub fn notify_show<R: Runtime>(app: AppHandle<R>, title: String, body: String, link: String) {
    let identifier = app.config().identifier.clone();
    std::thread::spawn(move || {
        if show_and_wait_for_click(&identifier, &title, &body) {
            open_link(&app, &link);
        }
    });
}

/// Traz a janela principal à frente e manda a UI navegar para o link.
fn open_link<R: Runtime>(app: &AppHandle<R>, link: &str) {
    if let Some(w) = app.get_webview_window("main") {
        let _ = w.show();
        let _ = w.unminimize();
        let _ = w.set_focus();
    }
    // emit_to("main"): só o webview da janela principal navega. Um emit global levaria as janelas
    // dedicadas (sala do Meet, editor) a saltar para outra rota a meio do que estão a fazer.
    let _ = app.emit_to("main", "notif-open", link.to_string());
}

/// Mostra a notificação e bloqueia até o utilizador interagir. `true` = clicou (abrir o elemento);
/// `false` = ignorou, fechou ou a notificação falhou.
#[cfg(target_os = "macos")]
fn show_and_wait_for_click(identifier: &str, title: &str, body: &str) -> bool {
    use mac_notification_sys::{Notification, NotificationResponse};
    // O `NSUserNotificationCenter` exige um bundle registado. Em `tauri dev` o binário não está
    // empacotado, por isso pedimos emprestado o do Terminal (mesma manha do plugin oficial).
    let bundle = if tauri::is_dev() {
        "com.apple.Terminal"
    } else {
        identifier
    };
    let _ = mac_notification_sys::set_application(bundle); // Err = já definido: nada a fazer

    let mut n = Notification::new();
    n.title(title).message(body).wait_for_click(true);
    match n.send() {
        Ok(r) => matches!(r, NotificationResponse::Click),
        Err(e) => {
            // Rede de segurança: esperar pelo clique nunca pode custar o AVISO. Se a entrega com
            // espera falhar, repete-se em modo dispara-e-esquece (o caminho de sempre) — fica sem
            // clique, mas a pessoa vê a notificação.
            eprintln!("aviso: notificação com clique falhou ({e}); a repetir sem espera");
            let mut plain = Notification::new();
            plain.title(title).message(body).asynchronous(true);
            let _ = plain.send();
            false
        }
    }
}

#[cfg(not(target_os = "macos"))]
fn show_and_wait_for_click(identifier: &str, title: &str, body: &str) -> bool {
    use notify_rust::NotificationResponse;
    use std::sync::atomic::{AtomicBool, Ordering};

    let mut n = notify_rust::Notification::new();
    n.summary(title).body(body);
    // Em dev o executável não está registado no SO; um AppUserModelID desconhecido faz o toast
    // falhar, por isso só o pomos na app instalada.
    #[cfg(windows)]
    if !tauri::is_dev() {
        n.app_id(identifier);
    }
    // No D-Bus o clique no corpo só é reportado se a notificação declarar a ação `default`; o
    // rótulo não chega a aparecer (é a ação implícita da notificação).
    #[cfg(not(windows))]
    {
        let _ = identifier;
        n.action("default", "default");
    }

    let handle = match n.show() {
        Ok(h) => h,
        Err(e) => {
            eprintln!("aviso: notificação não entregue: {e}");
            return false;
        }
    };
    let clicked = AtomicBool::new(false);
    let _ = handle.wait_for_response(|r: &NotificationResponse| {
        clicked.store(
            matches!(
                r,
                NotificationResponse::Default | NotificationResponse::Action(_)
            ),
            Ordering::Relaxed,
        );
    });
    clicked.load(Ordering::Relaxed)
}
