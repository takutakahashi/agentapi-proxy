mod binary;
mod commands;
mod types;

use tauri::{
    menu::{Menu, MenuItem},
    tray::{MouseButton, MouseButtonState, TrayIconBuilder, TrayIconEvent},
    AppHandle, Manager, WindowEvent,
};

const WINDOW_LABEL: &str = "dashboard";
const REFRESH_EVENT: &str = "dashboard://refresh";

/// Show and focus the dashboard window, creating the menu-bar experience:
/// the window stays alive but is simply revealed.
fn show_dashboard_window(app: &AppHandle) {
    if let Some(window) = app.get_webview_window(WINDOW_LABEL) {
        let _ = window.show();
        let _ = window.set_focus();
    }
}

fn emit_refresh(app: &AppHandle) {
    use tauri::Emitter;
    let _ = app.emit(REFRESH_EVENT, ());
}

/// Build the menu-bar tray with Show / Refresh / Restart / Quit.
fn build_tray(app: &AppHandle) -> tauri::Result<()> {
    let show = MenuItem::with_id(app, "show", "Show Dashboard", true, None::<&str>)?;
    let refresh = MenuItem::with_id(app, "refresh", "Refresh", true, None::<&str>)?;
    let restart = MenuItem::with_id(app, "restart", "Restart", true, None::<&str>)?;
    let quit = MenuItem::with_id(app, "quit", "Quit", true, None::<&str>)?;
    let menu = Menu::with_items(app, &[&show, &refresh, &restart, &quit])?;

    TrayIconBuilder::with_id("native-esm")
        .icon(app.default_window_icon().cloned().expect("default icon"))
        .menu(&menu)
        .show_menu_on_left_click(false)
        .tooltip("agentapi-proxy Native")
        .on_menu_event(|app, event| match event.id.as_ref() {
            "show" => show_dashboard_window(app),
            "refresh" => emit_refresh(app),
            "restart" => {
                // Run restart on a background thread so the tray menu stays
                // responsive; the frontend will re-query after the event.
                let app = app.clone();
                tauri::async_runtime::spawn(async move {
                    let _ = commands::native_restart(app.clone(), None).await;
                    emit_refresh(&app);
                });
            }
            "quit" => app.exit(0),
            _ => {}
        })
        .on_tray_icon_event(|tray, event| {
            // A single left click reveals the dashboard without going through
            // the menu, matching common menu-bar app behaviour.
            if let TrayIconEvent::Click {
                button: MouseButton::Left,
                button_state: MouseButtonState::Up,
                ..
            } = event
            {
                show_dashboard_window(tray.app_handle());
            }
        })
        .build(app)?;
    Ok(())
}

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    tauri::Builder::default()
        .plugin(tauri_plugin_shell::init())
        .invoke_handler(tauri::generate_handler![
            commands::native_is_installed,
            commands::native_status,
            commands::native_list,
            commands::native_sessions,
            commands::native_logs,
            commands::native_doctor,
            commands::native_restart,
            commands::native_update,
            commands::native_install,
            commands::native_environment,
            commands::native_update_environment,
            commands::native_reset,
            show_dashboard,
        ])
        .setup(|app| {
            build_tray(app.handle())?;
            // Start hidden; the user reveals the dashboard from the menu bar.
            Ok(())
        })
        .on_window_event(|window, event| {
            // Intercept the close request so the window hides instead of
            // being destroyed. The native daemon is independent of this UI
            // and keeps running regardless.
            if let WindowEvent::CloseRequested { api, .. } = event {
                if window.label() == WINDOW_LABEL {
                    api.prevent_close();
                    let _ = window.hide();
                }
            }
        })
        .run(tauri::generate_context!())
        .expect("error while running tauri application");
}

/// Frontend-callable command to reveal the dashboard window.
#[tauri::command]
fn show_dashboard(app: AppHandle) {
    show_dashboard_window(&app);
}
