//! Coisas próprias do desktop que a UI web não tem: janela de Definições e a conta mostrada no tray.
//!
//! O tray é construído em Rust e não sabe quem está autenticado — quem sabe é a UI. Por isso a UI
//! empurra o rótulo da conta para cá (`set_account`) e o tray limita-se a mostrá-lo.

use std::sync::Mutex;

use tauri::{AppHandle, Manager, Runtime, WebviewUrl, WebviewWindowBuilder};

/// Quem está autenticado, para o menu do tray. `None` = sessão ainda não iniciada.
#[derive(Default)]
pub struct Account(pub Mutex<Option<String>>);

/// Rótulo atual da conta (ou `None` se ninguém iniciou sessão).
pub fn account_label<R: Runtime>(app: &AppHandle<R>) -> Option<String> {
    app.try_state::<Account>()
        .and_then(|a| a.0.lock().ok().and_then(|g| g.clone()))
}

/// A UI chama isto quando a sessão abre ou fecha (passa `None` ao sair).
#[tauri::command]
pub fn set_account<R: Runtime>(app: AppHandle<R>, label: Option<String>) {
    if let Some(state) = app.try_state::<Account>() {
        if let Ok(mut g) = state.0.lock() {
            *g = label;
        }
    }
}

/// Abre as Definições numa janela própria. Se já estiver aberta, traz-la à frente em vez de abrir
/// outra — abrir duas cópias da mesma configuração convida a gravações contraditórias.
pub fn open_settings<R: Runtime>(app: &AppHandle<R>) {
    if let Some(w) = app.get_webview_window("settings") {
        let _ = w.show();
        let _ = w.unminimize();
        let _ = w.set_focus();
        return;
    }
    // `index.html?wpath=…` e não `/mail/settings`: o servidor de assets do Tauri não faz fallback de
    // SPA, então uma rota direta dá 404 na build. O main.tsx lê o wpath e reescreve a URL.
    let url = WebviewUrl::App("index.html?wpath=%2Fmail%2Fsettings".into());
    let built = WebviewWindowBuilder::new(app, "settings", url)
        .title("Beskos — Definições")
        .inner_size(1000.0, 720.0)
        .min_inner_size(560.0, 480.0)
        .build();
    if let Err(e) = built {
        eprintln!("aviso: não foi possível abrir as Definições: {e}");
    }
}

/// Adds "Import mail" to the File submenu.
///
/// Tauri's default menu builds File on macOS and Windows but not on Linux, and none of its submenus
/// carry a stable id -- so the submenu is found by its label, and a File submenu is created when
/// there is none. Matching on "File" is safe because Tauri writes that label itself, in English,
/// whatever the application's own language is.
pub fn add_import_item<R: Runtime>(
    app: &AppHandle<R>,
    menu: &tauri::menu::Menu<R>,
) -> tauri::Result<()> {
    let item = tauri::menu::MenuItem::with_id(
        app,
        "import-mail",
        "Importar correio…", // lang:ok — native menus are not reachable from i18n
        true,
        None::<&str>,
    )?;

    for entry in menu.items()? {
        if let Some(sub) = entry.as_submenu() {
            if sub.text().map(|t| t == "File").unwrap_or(false) {
                // At the top: "Close window" is the only other entry, and burying an action under it
                // puts the way out above the way in.
                sub.insert(&item, 0)?;
                sub.insert(&tauri::menu::PredefinedMenuItem::separator(app)?, 1)?;
                return Ok(());
            }
        }
    }

    // Linux: no File submenu exists, so make one. Placed after the application submenu when there
    // is one, otherwise first.
    let file = tauri::menu::Submenu::with_items(app, "File", true, &[&item])?;
    let pos = if menu.items()?.is_empty() { 0 } else { 1 };
    menu.insert(&file, pos)?;
    Ok(())
}

/// Raises the main window and asks the UI to open the import screen. The menu does not navigate --
/// only the web side knows the routes, and duplicating them in Rust means two places to be wrong.
pub fn open_import<R: Runtime>(app: &AppHandle<R>) {
    use tauri::Emitter;
    if let Some(w) = app.get_webview_window("main") {
        let _ = w.show();
        let _ = w.unminimize();
        let _ = w.set_focus();
        let _ = w.emit_to("main", "menu-import-mail", ());
    }
}
