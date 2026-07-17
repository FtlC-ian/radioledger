//! RadioLedger Desktop — Tauri application root.
//!
//! This crate wires together all subsystems:
//! - OAuth2 PKCE auth flow via system browser (auth.rs)
//! - WSJT-X UDP listener (udp.rs)
//! - Local SQLite offline cache (db.rs)
//! - Background server sync (sync.rs)
//! - Rig control integration (rig/)
//! - System tray icon and menu (tray.rs)

use std::sync::Arc;

use anyhow::Context;
use tauri::tray::TrayIcon;
use tauri::Manager;
use tokio::sync::Mutex;
use tracing::info;
use tracing_appender::non_blocking::WorkerGuard;
use tracing_subscriber::{fmt, layer::SubscriberExt, util::SubscriberInitExt, EnvFilter};

pub mod auth;
pub mod config;
pub mod db;
pub mod error;
pub mod lotw;
pub mod n1mm;
pub mod rig;
pub mod sync;
#[cfg(test)]
pub(crate) mod test_support;
pub mod tray;
pub mod udp;
pub mod wizard;

use db::Database;
use rig::RigState;
use sync::SyncState;
use udp::UdpState;

/// Shared application state accessible from all Tauri commands.
pub struct AppState {
    pub db: Arc<Database>,
    pub udp: Arc<Mutex<UdpState>>,
    pub sync: Arc<Mutex<SyncState>>,
    pub rig: Arc<Mutex<RigState>>,
    pub tray: Arc<Mutex<Option<TrayIcon>>>,
}

fn init_logging() -> anyhow::Result<WorkerGuard> {
    let log_dir = config::config_dir()?;
    std::fs::create_dir_all(&log_dir)
        .with_context(|| format!("Failed to create log directory at {}", log_dir.display()))?;

    let file_appender = tracing_appender::rolling::RollingFileAppender::builder()
        .rotation(tracing_appender::rolling::Rotation::DAILY)
        .filename_prefix("radioledger.log")
        .max_log_files(3)
        .build(&log_dir)
        .with_context(|| format!("Failed to initialize file logger in {}", log_dir.display()))?;

    let (file_writer, log_guard) = tracing_appender::non_blocking(file_appender);
    let env_filter = EnvFilter::try_from_default_env().unwrap_or_else(|_| EnvFilter::new("info"));
    let log_path = log_dir.join("radioledger.log");

    tracing_subscriber::registry()
        .with(env_filter)
        .with(fmt::layer().with_writer(std::io::stdout))
        .with(fmt::layer().with_ansi(false).with_writer(file_writer))
        .init();

    info!(log_file = %log_path.display(), "File logging enabled");

    Ok(log_guard)
}

/// Application entry point — called from main.rs.
pub fn run() {
    // Initialise structured logging to both stdout and ~/.radioledger/radioledger.log.
    // RUST_LOG still overrides the default `info` filter.
    let _log_guard = init_logging().expect("Failed to initialise logging");

    info!("RadioLedger desktop starting");

    // Initialise the local SQLite database.
    let db = Arc::new(Database::open().expect("Failed to open local SQLite cache"));

    // Load config once at startup.
    let cfg = config::load().unwrap_or_default();

    // Probe keychain once at startup so auth storage can gracefully fall back
    // to config-file tokens when secure storage is unavailable.
    auth::initialize_keychain_availability();

    // Initialise subsystem state.
    let udp_state = Arc::new(Mutex::new(UdpState::new(Arc::clone(&db))));
    let sync_state = Arc::new(Mutex::new(SyncState::new(Arc::clone(&db))));
    let rig_state = Arc::new(Mutex::new(RigState::new(cfg.rig.clone())));
    let tray_state = Arc::new(Mutex::new(None));

    // Clone state refs that need to move into the setup closure.
    let sync_state_for_setup = Arc::clone(&sync_state);
    let rig_state_for_setup = Arc::clone(&rig_state);
    let udp_state_for_setup = Arc::clone(&udp_state);

    tauri::Builder::default()
        // Single-instance guard: if a second copy is launched, focus the
        // existing window instead of opening a duplicate.
        .plugin(tauri_plugin_single_instance::init(|app, _args, _cwd| {
            if let Some(window) = app.get_webview_window("main") {
                let _ = window.show();
                let _ = window.set_focus();
            }
        }))
        .plugin(tauri_plugin_shell::init())
        .plugin(tauri_plugin_notification::init())
        .plugin(tauri_plugin_autostart::init(
            tauri_plugin_autostart::MacosLauncher::LaunchAgent,
            None,
        ))
        .manage(AppState {
            db: Arc::clone(&db),
            udp: Arc::clone(&udp_state),
            sync: Arc::clone(&sync_state),
            rig: Arc::clone(&rig_state),
            tray: Arc::clone(&tray_state),
        })
        .setup(move |app| {
            // Build the system tray icon and menu.
            tray::setup_tray(app)?;

            // Kick off the background sync task.
            {
                let sync = Arc::clone(&sync_state_for_setup);
                let app_handle = app.handle().clone();
                tauri::async_runtime::spawn(async move {
                    sync::run_sync_loop(sync, app_handle).await;
                });
            }

            // Kick off rig auto-detect + polling task.
            {
                let rig = Arc::clone(&rig_state_for_setup);
                let app_handle = app.handle().clone();
                tauri::async_runtime::spawn(async move {
                    rig::run_rig_poll_loop(rig, app_handle).await;
                });
            }

            // Auto-start any UDP listeners configured with `auto_start: true`.
            {
                let udp = Arc::clone(&udp_state_for_setup);
                let app_handle = app.handle().clone();
                tauri::async_runtime::spawn(async move {
                    udp::auto_start_listeners(udp, app_handle).await;
                });
            }

            info!("RadioLedger desktop ready");
            Ok(())
        })
        .invoke_handler(tauri::generate_handler![
            auth::is_authenticated,
            auth::get_auth_status,
            auth::get_current_user,
            auth::login,
            auth::login_local,
            auth::get_api_token,
            auth::api_get,
            auth::logout,
            udp::get_udp_status,
            udp::list_recent_wsjtx_decodes,
            udp::get_wsjtx_decode_panel_settings,
            udp::save_wsjtx_decode_panel_settings,
            udp::start_udp_listener,
            udp::stop_udp_listener,
            udp::start_js8call_listener,
            udp::stop_js8call_listener,
            udp::start_n1mm_listener,
            udp::stop_n1mm_listener,
            udp::get_udp_config,
            udp::save_udp_settings,
            sync::get_sync_status,
            sync::sync_now,
            sync::list_qsos,
            sync::count_qsos,
            sync::create_qso,
            sync::update_qso,
            sync::get_callsign_history,
            sync::callsign::lookup_callsign,
            lotw::detect_tqsl,
            lotw::sign_and_upload,
            lotw::check_confirmations,
            lotw::get_cert_info,
            lotw::get_lotw_status,
            lotw::push_cert_expiry,
            rig::connect_rig,
            rig::disconnect_rig,
            rig::get_rig_status,
            rig::get_rig_settings,
            rig::save_rig_settings,
            rig::get_rig_frequency,
            rig::set_rig_frequency,
            rig::profiles::list_rig_profiles,
            rig::profiles::get_active_rig_profile_id,
            rig::profiles::create_rig_profile,
            rig::profiles::update_rig_profile,
            rig::profiles::delete_rig_profile,
            rig::profiles::set_active_rig_profile,
            rig::profiles::get_rig_models,
            rig::profiles::list_serial_ports,
            rig::profiles::test_rig_connection,
            wizard::get_wizard_status,
            wizard::get_auth_mode,
            wizard::complete_wizard,
            wizard::detect_software,
            wizard::save_wizard_config,
            wizard::get_server_url,
            wizard::save_settings,
            wizard::get_logbook_columns,
            wizard::save_logbook_columns,
            wizard::test_server_connection,
        ])
        .run(tauri::generate_context!())
        .expect("Error running RadioLedger desktop application");
}
