//! Setup wizard backend commands.
//!
//! Provides Tauri commands for the first-run setup wizard:
//! - Status check (is setup complete?)
//! - Software detection (WSJT-X, JS8Call, N1MM+, Flrig, rigctld)
//! - Config persistence
//! - Wizard completion flag
//! - Server connection testing
//! - Settings save

use std::net::{TcpStream, UdpSocket};
use std::path::PathBuf;
use std::time::Duration;

use serde::{Deserialize, Serialize};
use tracing::{debug, info};

use crate::config;
use crate::error::AppError;

// ─── Data structures ──────────────────────────────────────────────────────────

/// Result of testing a server connection via `test_server_connection`.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ConnectionTestResult {
    /// Whether the server responded with a 2xx status.
    pub reachable: bool,
    /// HTTP status code returned, if a response was received.
    pub status_code: Option<u16>,
    /// Human-readable error description, or null on success.
    pub error: Option<String>,
}

/// Request body for the `save_settings` command.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct SaveSettingsRequest {
    /// Server URL to persist (empty string keeps the existing value).
    pub server_url: String,
    /// Authentication mode: "cloud" or "local".
    pub auth_mode: String,
}

/// Result payload for desktop logbook column preferences.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct LogbookColumnsResponse {
    pub visible_columns: Vec<String>,
}

/// Request payload for persisting desktop logbook column preferences.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct SaveLogbookColumnsRequest {
    pub visible_columns: Vec<String>,
}

/// Result of the software auto-detection scan.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct DetectedSoftware {
    pub wsjtx: SoftwareDetection,
    pub js8call: SoftwareDetection,
    pub n1mm: SoftwareDetection,
    pub flrig: SoftwareDetection,
    pub rigctld: SoftwareDetection,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct SoftwareDetection {
    /// Whether the software appears to be running.
    pub detected: bool,
    /// Port that was probed.
    pub port: u16,
    /// Human-readable description.
    pub note: String,
    /// Whether the binary is installed on the system (independent of running).
    pub installed: bool,
    /// Path where the binary was found, if installed.
    pub binary_path: Option<String>,
    /// Install instructions if not found.
    pub install_hint: Option<String>,
}

/// Status of the setup wizard (complete or not).
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct WizardStatus {
    pub setup_complete: bool,
}

/// Wizard configuration collected during the wizard steps.
/// Fields are optional — only what the user configures is written.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct WizardConfig {
    /// Authentication mode: "cloud" (OAuth2 PKCE) or "local" (email/password).
    pub auth_mode: String,
    /// Server URL — used for self-hosted deployments (auth_mode = "local").
    /// Also overrides the default cloud URL when set.
    pub server_url: Option<String>,
    /// WSJT-X listener enabled.
    pub wsjtx_enabled: bool,
    /// WSJT-X UDP port.
    pub wsjtx_port: u16,
    /// JS8Call listener enabled.
    pub js8call_enabled: bool,
    /// JS8Call UDP port.
    pub js8call_port: u16,
    /// N1MM+ listener enabled.
    pub n1mm_enabled: bool,
    /// N1MM+ UDP port.
    pub n1mm_port: u16,
    /// LoTW tQSL path (optional override).
    pub lotw_tqsl_path: Option<String>,
    /// LoTW station location.
    pub lotw_station_location: Option<String>,
    /// Rig control preferred method (none / flrig / hamlib).
    pub rig_preferred_method: String,
    /// Rig control host.
    pub rig_host: String,
    /// Flrig port.
    pub rig_flrig_port: u16,
    /// rigctld port.
    pub rig_rigctld_port: u16,
}

impl Default for WizardConfig {
    fn default() -> Self {
        WizardConfig {
            auth_mode: "cloud".to_string(),
            server_url: None,
            wsjtx_enabled: true,
            wsjtx_port: 2237,
            js8call_enabled: false,
            js8call_port: 2242,
            n1mm_enabled: false,
            n1mm_port: 12060,
            lotw_tqsl_path: None,
            lotw_station_location: None,
            rig_preferred_method: "none".to_string(),
            rig_host: "127.0.0.1".to_string(),
            rig_flrig_port: 12345,
            rig_rigctld_port: 4532,
        }
    }
}

// ─── Tauri commands ───────────────────────────────────────────────────────────

/// Returns whether the setup wizard has been completed.
#[tauri::command]
pub fn get_wizard_status() -> Result<WizardStatus, AppError> {
    let cfg = config::load().map_err(|e| AppError::Config(e.to_string()))?;
    Ok(WizardStatus {
        setup_complete: cfg.setup_complete,
    })
}

/// Returns the current auth mode ("cloud" or "local") from config.
/// Used by the frontend to determine how the Login button should behave.
#[tauri::command]
pub fn get_auth_mode() -> Result<String, AppError> {
    let cfg = config::load().map_err(|e| AppError::Config(e.to_string()))?;
    let mode = match cfg.auth_mode {
        config::AuthMode::Local => "local",
        config::AuthMode::Cloud => "cloud",
    };
    Ok(mode.to_string())
}

/// Marks the setup wizard as complete in config.yaml.
#[tauri::command]
pub fn complete_wizard() -> Result<(), AppError> {
    let mut cfg = config::load().map_err(|e| AppError::Config(e.to_string()))?;
    cfg.setup_complete = true;
    config::save(&cfg).map_err(|e| AppError::Config(e.to_string()))?;
    info!("Setup wizard marked as complete");
    Ok(())
}

/// Probes common ports to detect running ham radio software.
///
/// Detection strategy:
/// - WSJT-X / JS8Call / N1MM+: bind a UDP socket; if binding to 127.0.0.1:port
///   fails (EADDRINUSE), the port is in use — software is likely running.
/// - Flrig: TCP connect to 127.0.0.1:12345 (XML-RPC server).
/// - rigctld: TCP connect to 127.0.0.1:4532, plus binary installation check.
#[tauri::command]
pub fn detect_software() -> Result<DetectedSoftware, AppError> {
    let wsjtx = probe_udp_port(2237, "WSJT-X");
    let js8call = probe_udp_port(2242, "JS8Call");
    let n1mm = probe_udp_port(12060, "N1MM+");
    let flrig = probe_tcp_port(12345, "Flrig XML-RPC");

    // rigctld: probe the TCP port (is it running?) and also check for binary installation.
    let mut rigctld = probe_tcp_port(4532, "rigctld");
    let rigctld_binary = detect_rigctld_wizard();
    rigctld.installed = rigctld_binary.is_some();
    rigctld.binary_path = rigctld_binary.map(|p| p.to_string_lossy().into_owned());
    if !rigctld.installed {
        rigctld.install_hint = Some(get_install_hint());
    }

    info!(
        "Software detection — WSJT-X:{} JS8Call:{} N1MM:{} Flrig:{} rigctld:{} (installed:{})",
        wsjtx.detected,
        js8call.detected,
        n1mm.detected,
        flrig.detected,
        rigctld.detected,
        rigctld.installed,
    );

    Ok(DetectedSoftware {
        wsjtx,
        js8call,
        n1mm,
        flrig,
        rigctld,
    })
}

/// Saves wizard-collected settings to config.yaml.
#[tauri::command]
pub fn save_wizard_config(wizard_config: WizardConfig) -> Result<(), AppError> {
    let mut cfg = config::load().map_err(|e| AppError::Config(e.to_string()))?;

    // Auth mode + server URL
    cfg.auth_mode = match wizard_config.auth_mode.as_str() {
        "local" => config::AuthMode::Local,
        _ => config::AuthMode::Cloud,
    };
    if let Some(url) = wizard_config.server_url.filter(|u| !u.trim().is_empty()) {
        let normalized = url.trim_end_matches('/').to_string();
        if cfg.server.url != normalized {
            cfg.logbook.default_uuid = None;
        }
        cfg.server.url = normalized;
    }

    // UDP settings
    cfg.udp.wsjtx.enabled = wizard_config.wsjtx_enabled;
    cfg.udp.wsjtx.port = wizard_config.wsjtx_port;
    cfg.udp.js8call.enabled = wizard_config.js8call_enabled;
    cfg.udp.js8call.port = wizard_config.js8call_port;
    cfg.udp.n1mm.enabled = wizard_config.n1mm_enabled;
    cfg.udp.n1mm.port = wizard_config.n1mm_port;

    // LoTW settings
    cfg.lotw.tqsl_path = wizard_config.lotw_tqsl_path;
    cfg.lotw.station_location = wizard_config.lotw_station_location;

    // Rig settings — create a proper profile so the Rig Profiles UI picks it
    // up immediately.  Writing to the legacy single-rig fields (preferred_method,
    // host, flrig_port, rigctld_port) would only become visible after the lazy
    // migrate_legacy() runs on the next load, and the resulting profile would
    // have placeholder values for fields the wizard already collected.
    let method = wizard_config.rig_preferred_method.as_str();

    // Always clean up any profiles previously created by the wizard, regardless
    // of the new method choice.  This ensures that re-running the wizard with
    // method="none" (rig control disabled) removes stale wizard-created profiles
    // rather than leaving them active.
    cfg.rig.profiles.retain(|p| !p.name.ends_with("(wizard)"));

    // When rig control is being disabled via the wizard, unconditionally clear
    // active_profile_id.  This covers both cases:
    //   (a) the active profile was a wizard-created one we just removed above, or
    //   (b) the active profile was a user-created profile that was active before
    //       the wizard was re-run with rig control disabled.
    // Leaving a non-None active_profile_id when method="none" is the root cause of
    // issue #190: RigState treats any non-None active_profile_id as "a profile is
    // active", so it skips the user_disconnected fast-path and churns with
    // reconnect errors instead of staying cleanly quiet.
    if method == "none" || method.is_empty() {
        cfg.rig.active_profile_id = None;
    } else {
        // For a non-none method: only clear the id if the previously-active profile
        // was a wizard-created one we just removed (we'll set a new one below).
        let active_was_wizard = cfg.rig.active_profile_id.as_ref().is_some_and(|id| {
            // After retain above, if the id is no longer in profiles it was a wizard
            // profile that was removed.
            !cfg.rig.profiles.iter().any(|p| p.id == *id)
        });
        if active_was_wizard {
            cfg.rig.active_profile_id = None;
        }
    }

    if method != "none" && !method.is_empty() {
        let (interface_type, port, profile_name) = match method {
            "flrig" => (
                config::RigInterfaceType::Flrig,
                wizard_config.rig_flrig_port,
                "FLRig (wizard)",
            ),
            _ => (
                // "hamlib" / "rigctld" / anything else with a port
                config::RigInterfaceType::External,
                wizard_config.rig_rigctld_port,
                "rigctld (wizard)",
            ),
        };

        let profile = config::RigProfile {
            id: uuid::Uuid::new_v4().to_string(),
            name: profile_name.to_string(),
            interface_type,
            host: wizard_config.rig_host.clone(),
            port,
            ..Default::default()
        };

        let profile_id = profile.id.clone();
        cfg.rig.profiles.push(profile);
        cfg.rig.active_profile_id = Some(profile_id);
    }

    // Also update legacy fields for backwards compatibility with any code that
    // still reads them before the next full migration pass.
    cfg.rig.preferred_method = match wizard_config.rig_preferred_method.as_str() {
        "flrig" => config::RigControlMethod::Flrig,
        "hamlib" | "rigctld" => config::RigControlMethod::Hamlib,
        _ => config::RigControlMethod::None,
    };
    cfg.rig.host = wizard_config.rig_host;
    cfg.rig.flrig_port = wizard_config.rig_flrig_port;
    cfg.rig.rigctld_port = wizard_config.rig_rigctld_port;

    config::save(&cfg).map_err(|e| AppError::Config(e.to_string()))?;
    info!(
        "Wizard config saved to config.yaml (auth_mode={})",
        wizard_config.auth_mode
    );
    Ok(())
}

/// Returns the currently configured server URL from config.yaml.
/// Used by the frontend to populate the Settings server URL field and to display
/// the server address in the Shack tab auth card when in local mode.
#[tauri::command]
pub fn get_server_url() -> Result<String, AppError> {
    let cfg = config::load().map_err(|e| AppError::Config(e.to_string()))?;
    Ok(cfg.server.url)
}

/// Persists the server URL and auth mode to config.yaml.
/// Called by the Settings tab "Save" button and after a successful local login.
#[tauri::command]
pub fn save_settings(request: SaveSettingsRequest) -> Result<(), AppError> {
    let mut cfg = config::load().map_err(|e| AppError::Config(e.to_string()))?;

    let url = request.server_url.trim().to_string();
    if !url.is_empty() {
        let normalized = url.trim_end_matches('/').to_string();
        if cfg.server.url != normalized {
            cfg.logbook.default_uuid = None;
        }
        cfg.server.url = normalized;
    }

    cfg.auth_mode = match request.auth_mode.as_str() {
        "local" => config::AuthMode::Local,
        _ => config::AuthMode::Cloud,
    };

    config::save(&cfg).map_err(|e| AppError::Config(e.to_string()))?;
    info!(
        "Settings saved to config.yaml (auth_mode={})",
        request.auth_mode
    );
    Ok(())
}

/// Returns the configured set of visible desktop logbook columns.
#[tauri::command]
pub fn get_logbook_columns(
    state: tauri::State<'_, crate::AppState>,
) -> Result<LogbookColumnsResponse, AppError> {
    let visible_columns = state
        .db
        .get_setting("logbook.visible_columns")
        .map_err(|e| AppError::Database(e.to_string()))?
        .and_then(|raw| serde_json::from_str::<Vec<String>>(&raw).ok())
        .unwrap_or_default();

    Ok(LogbookColumnsResponse { visible_columns })
}

/// Persists the visible desktop logbook columns to the database.
#[tauri::command]
pub fn save_logbook_columns(
    state: tauri::State<'_, crate::AppState>,
    request: SaveLogbookColumnsRequest,
) -> Result<(), AppError> {
    let mut columns: Vec<String> = request
        .visible_columns
        .into_iter()
        .map(|column| column.trim().to_string())
        .filter(|column| !column.is_empty())
        .collect();

    columns.sort();
    columns.dedup();

    let payload = serde_json::to_string(&columns)
        .map_err(|e| AppError::Other(format!("Failed to serialize columns: {e}")))?;

    state
        .db
        .set_setting("logbook.visible_columns", &payload)
        .map_err(|e| AppError::Database(e.to_string()))
}

/// Tests connectivity to a server by issuing a GET request to `{url}/health`.
/// Uses reqwest so the request is made from the Rust process (avoids CSP/CORS
/// restrictions that would affect a `fetch()` call in the webview).
#[tauri::command]
pub async fn test_server_connection(url: String) -> Result<ConnectionTestResult, AppError> {
    let health_url = format!("{}/health", url.trim_end_matches('/'));
    let client = reqwest::Client::builder()
        .timeout(std::time::Duration::from_secs(5))
        .build()
        .map_err(|e| AppError::Network(e.to_string()))?;

    match client.get(&health_url).send().await {
        Ok(resp) => {
            let status = resp.status();
            Ok(ConnectionTestResult {
                reachable: status.is_success(),
                status_code: Some(status.as_u16()),
                error: if !status.is_success() {
                    Some(format!("HTTP {}", status))
                } else {
                    None
                },
            })
        }
        Err(e) => Ok(ConnectionTestResult {
            reachable: false,
            status_code: None,
            error: Some(e.to_string()),
        }),
    }
}

// ─── Internal helpers ────────────────────────────────────────────────────────

/// Probe a UDP port by attempting to bind a socket to it.
/// If binding fails, assume the port is already in use (software running).
fn probe_udp_port(port: u16, name: &str) -> SoftwareDetection {
    let addr = format!("127.0.0.1:{port}");
    match UdpSocket::bind(&addr) {
        Ok(_) => {
            // We could bind — port is free, software not detected
            debug!("{name} not detected on port {port} (port free)");
            SoftwareDetection {
                detected: false,
                port,
                note: format!("{name} not running (port {port} is free)"),
                installed: false,
                binary_path: None,
                install_hint: None,
            }
        }
        Err(_) => {
            // Could not bind — port in use, software likely running
            debug!("{name} detected on port {port} (port in use)");
            SoftwareDetection {
                detected: true,
                port,
                note: format!("{name} detected on port {port}"),
                installed: false,
                binary_path: None,
                install_hint: None,
            }
        }
    }
}

/// Probe a TCP port by attempting a short-timeout connection.
fn probe_tcp_port(port: u16, name: &str) -> SoftwareDetection {
    let addr = format!("127.0.0.1:{port}");
    let timeout = Duration::from_millis(500);
    match TcpStream::connect_timeout(
        &addr
            .parse()
            .unwrap_or_else(|_| "127.0.0.1:0".parse().unwrap()),
        timeout,
    ) {
        Ok(_) => {
            debug!("{name} detected on port {port}");
            SoftwareDetection {
                detected: true,
                port,
                note: format!("{name} detected on port {port}"),
                installed: false,
                binary_path: None,
                install_hint: None,
            }
        }
        Err(_) => {
            debug!("{name} not detected on port {port}");
            SoftwareDetection {
                detected: false,
                port,
                note: format!("{name} not running (port {port} not responding)"),
                installed: false,
                binary_path: None,
                install_hint: None,
            }
        }
    }
}

/// Returns platform-specific install instructions for Hamlib (rigctld).
pub fn get_install_hint() -> String {
    match std::env::consts::OS {
        "macos" => "Install via Homebrew: brew install hamlib".to_string(),
        "linux" => {
            // Detect Fedora/RHEL vs Debian/Ubuntu by presence of distro release files.
            if std::path::Path::new("/etc/fedora-release").exists()
                || std::path::Path::new("/etc/redhat-release").exists()
            {
                "Install via dnf: sudo dnf install hamlib".to_string()
            } else {
                "Install via apt: sudo apt install libhamlib-utils".to_string()
            }
        }
        "windows" => "Download from https://github.com/Hamlib/Hamlib/releases \
             \u{2014} get the latest Windows installer (.exe) or ZIP file"
            .to_string(),
        _ => "Visit https://github.com/Hamlib/Hamlib/releases for downloads".to_string(),
    }
}

/// Locate the `rigctld` binary for wizard display purposes.
///
/// Delegates to [`crate::rig::managed::detect_rigctld`] for standard paths,
/// then additionally checks WSJT-X bundle locations which may ship their own
/// copy of the binary.
fn detect_rigctld_wizard() -> Option<PathBuf> {
    use crate::rig::managed::detect_rigctld;

    // Standard PATH + well-known locations.
    if let Some(p) = detect_rigctld() {
        return Some(p);
    }

    // WSJT-X may bundle rigctld in platform-specific locations.
    let wsjtx_paths: &[&str] = &[
        #[cfg(target_os = "macos")]
        "/Applications/wsjtx.app/Contents/MacOS/rigctld",
        #[cfg(target_os = "linux")]
        "/usr/bin/rigctld", // already checked above, but harmless
        #[cfg(target_os = "windows")]
        r"C:\WSJT\wsjtx\bin\rigctld.exe",
        #[cfg(target_os = "windows")]
        r"C:\Program Files\WSJT-X\bin\rigctld.exe",
    ];

    for path in wsjtx_paths {
        let pb = PathBuf::from(path);
        if pb.exists() {
            return Some(pb);
        }
    }

    None
}

// ─── Tests ───────────────────────────────────────────────────────────────────

#[cfg(test)]
mod tests {
    use super::*;

    fn with_temp_home<F>(test_fn: F)
    where
        F: FnOnce(),
    {
        crate::test_support::with_temp_home("radioledger-wizard-test", test_fn);
    }

    #[test]
    fn save_settings_persists_config_backed_values_across_reload() {
        with_temp_home(|| {
            save_settings(SaveSettingsRequest {
                server_url: "https://example.radioledger.test/".to_string(),
                auth_mode: "local".to_string(),
            })
            .expect("settings should save");

            assert_eq!(
                get_server_url().expect("server url should load"),
                "https://example.radioledger.test"
            );
            assert_eq!(get_auth_mode().expect("auth mode should load"), "local");

            let cfg = crate::config::load().expect("config should reload");
            assert_eq!(cfg.server.url, "https://example.radioledger.test");
            assert_eq!(cfg.auth_mode, crate::config::AuthMode::Local);
        });
    }

    #[test]
    fn save_settings_clears_cached_logbook_when_server_changes() {
        with_temp_home(|| {
            let mut cfg = crate::config::load().expect("config should load");
            cfg.server.url = "https://old.radioledger.test".to_string();
            cfg.logbook.default_uuid = Some("logbook-123".to_string());
            cfg.save().expect("config should save");

            save_settings(SaveSettingsRequest {
                server_url: "https://new.radioledger.test/".to_string(),
                auth_mode: "cloud".to_string(),
            })
            .expect("settings should save");

            let fresh = crate::config::load().expect("config should reload");
            assert_eq!(fresh.server.url, "https://new.radioledger.test");
            assert_eq!(fresh.auth_mode, crate::config::AuthMode::Cloud);
            assert_eq!(fresh.logbook.default_uuid, None);
        });
    }

    #[test]
    fn get_install_hint_returns_nonempty_string() {
        let hint = get_install_hint();
        assert!(!hint.is_empty(), "install hint must not be empty");
    }

    #[test]
    #[cfg(target_os = "macos")]
    fn get_install_hint_macos() {
        let hint = get_install_hint();
        assert!(hint.contains("brew install hamlib"), "macOS hint: {hint}");
    }

    #[test]
    #[cfg(target_os = "windows")]
    fn get_install_hint_windows() {
        let hint = get_install_hint();
        assert!(
            hint.contains("github.com/Hamlib/Hamlib/releases"),
            "windows hint: {hint}"
        );
    }

    #[test]
    fn detected_software_serializes_with_new_fields() {
        let det = DetectedSoftware {
            wsjtx: SoftwareDetection {
                detected: false,
                port: 2237,
                note: "not running".to_string(),
                installed: false,
                binary_path: None,
                install_hint: None,
            },
            js8call: SoftwareDetection {
                detected: false,
                port: 2242,
                note: "not running".to_string(),
                installed: false,
                binary_path: None,
                install_hint: None,
            },
            n1mm: SoftwareDetection {
                detected: false,
                port: 12060,
                note: "not running".to_string(),
                installed: false,
                binary_path: None,
                install_hint: None,
            },
            flrig: SoftwareDetection {
                detected: false,
                port: 12345,
                note: "not running".to_string(),
                installed: false,
                binary_path: None,
                install_hint: None,
            },
            rigctld: SoftwareDetection {
                detected: false,
                port: 4532,
                note: "not running".to_string(),
                installed: true,
                binary_path: Some("/usr/local/bin/rigctld".to_string()),
                install_hint: None,
            },
        };

        let json = serde_json::to_string(&det).expect("serialization failed");
        assert!(json.contains("\"installed\":true"));
        assert!(json.contains("binary_path"));
        assert!(json.contains("/usr/local/bin/rigctld"));
        assert!(json.contains("\"install_hint\":null"));
    }

    #[test]
    fn software_detection_not_installed_has_hint() {
        let det = SoftwareDetection {
            detected: false,
            port: 4532,
            note: "not running".to_string(),
            installed: false,
            binary_path: None,
            install_hint: Some(get_install_hint()),
        };
        assert!(det.install_hint.is_some());
        let hint = det.install_hint.unwrap();
        assert!(!hint.is_empty());
        // The hint should point somewhere useful.
        let has_command = hint.contains("brew")
            || hint.contains("apt")
            || hint.contains("dnf")
            || hint.contains("github.com")
            || hint.contains("Hamlib");
        assert!(has_command, "hint should contain install guidance: {hint}");
    }

    // ── Issue #190: wizard rig-disable leaves no dangling active_profile_id ──

    fn make_wizard_config_none() -> WizardConfig {
        WizardConfig {
            auth_mode: "cloud".to_string(),
            server_url: None,
            wsjtx_enabled: false,
            wsjtx_port: 2237,
            js8call_enabled: false,
            js8call_port: 2242,
            n1mm_enabled: false,
            n1mm_port: 12060,
            lotw_tqsl_path: None,
            lotw_station_location: None,
            rig_preferred_method: "none".to_string(),
            rig_host: "127.0.0.1".to_string(),
            rig_flrig_port: 12345,
            rig_rigctld_port: 4532,
        }
    }

    /// Running the wizard with method="none" on a fresh config leaves
    /// active_profile_id as None — no dangling reference.
    #[test]
    fn wizard_rig_disable_on_fresh_config_leaves_no_active_profile() {
        with_temp_home(|| {
            save_wizard_config(make_wizard_config_none())
                .expect("save_wizard_config should succeed");

            let cfg = crate::config::load().expect("config should reload");
            assert!(
                cfg.rig.active_profile_id.is_none(),
                "active_profile_id must be None after rig-disable wizard run; got: {:?}",
                cfg.rig.active_profile_id
            );
            assert!(
                cfg.rig.profiles.is_empty(),
                "profiles must be empty after rig-disable wizard run; got {} profiles",
                cfg.rig.profiles.len()
            );
            assert_eq!(
                cfg.rig.preferred_method,
                crate::config::RigControlMethod::None,
                "preferred_method must be None"
            );
        });
    }

    /// Re-running the wizard with method="none" after a previous flrig run:
    /// the old wizard profile is removed and active_profile_id is cleared.
    #[test]
    fn wizard_rig_disable_clears_previous_wizard_profile() {
        with_temp_home(|| {
            // First run: configure flrig.
            save_wizard_config(WizardConfig {
                rig_preferred_method: "flrig".to_string(),
                ..make_wizard_config_none()
            })
            .expect("first save_wizard_config should succeed");

            let cfg = crate::config::load().expect("config should reload after first run");
            assert!(
                cfg.rig.active_profile_id.is_some(),
                "active_profile_id must be set after enabling flrig"
            );
            let profile_id = cfg.rig.active_profile_id.clone().unwrap();
            assert!(
                cfg.rig.profiles.iter().any(|p| p.id == profile_id),
                "active profile must exist in profiles list"
            );

            // Second run: disable rig control.
            save_wizard_config(make_wizard_config_none())
                .expect("second save_wizard_config should succeed");

            let cfg2 = crate::config::load().expect("config should reload after second run");
            assert!(
                cfg2.rig.active_profile_id.is_none(),
                "active_profile_id must be None after rig-disable; got: {:?}",
                cfg2.rig.active_profile_id
            );
            assert!(
                cfg2.rig.profiles.is_empty(),
                "wizard profiles must be gone after rig-disable; got {} profiles",
                cfg2.rig.profiles.len()
            );
        });
    }

    /// Re-running the wizard with method="none" when a non-wizard (user-created)
    /// profile is active also clears active_profile_id (issue #190 scenario B).
    #[test]
    fn wizard_rig_disable_clears_non_wizard_active_profile_id() {
        with_temp_home(|| {
            // Simulate a user-created profile that is active.
            let mut cfg = crate::config::load().expect("config should load");
            let user_profile = crate::config::RigProfile {
                id: "user-profile-001".to_string(),
                name: "My Kenwood TS-590".to_string(),
                interface_type: crate::config::RigInterfaceType::External,
                host: "127.0.0.1".to_string(),
                port: 4532,
                ..Default::default()
            };
            cfg.rig.profiles.push(user_profile);
            cfg.rig.active_profile_id = Some("user-profile-001".to_string());
            cfg.rig.preferred_method = crate::config::RigControlMethod::Hamlib;
            cfg.save().expect("config should save");

            // Run wizard with rig control disabled.
            save_wizard_config(make_wizard_config_none())
                .expect("save_wizard_config should succeed");

            let cfg2 = crate::config::load().expect("config should reload");
            assert!(
                cfg2.rig.active_profile_id.is_none(),
                "active_profile_id must be None even when non-wizard profile was active; got: {:?}",
                cfg2.rig.active_profile_id
            );
            // The user's non-wizard profile is preserved (not deleted by the wizard)
            // but it is no longer active.
            assert!(
                cfg2.rig.profiles.iter().any(|p| p.id == "user-profile-001"),
                "user-created profile should still exist in the list"
            );
        });
    }

    /// Switching from flrig to rigctld via wizard replaces the wizard profile
    /// and points active_profile_id at the new one.
    #[test]
    fn wizard_rig_method_change_replaces_wizard_profile() {
        with_temp_home(|| {
            // First run: flrig.
            save_wizard_config(WizardConfig {
                rig_preferred_method: "flrig".to_string(),
                ..make_wizard_config_none()
            })
            .expect("first save should succeed");

            let cfg1 = crate::config::load().expect("config should load after flrig run");
            let first_id = cfg1.rig.active_profile_id.clone().unwrap();

            // Second run: switch to rigctld.
            save_wizard_config(WizardConfig {
                rig_preferred_method: "rigctld".to_string(),
                ..make_wizard_config_none()
            })
            .expect("second save should succeed");

            let cfg2 = crate::config::load().expect("config should load after rigctld run");
            let second_id = cfg2.rig.active_profile_id.clone().unwrap();
            assert_ne!(
                first_id, second_id,
                "a new profile should have been created for the second wizard run"
            );
            // Only one wizard profile should exist now.
            let wizard_profiles: Vec<_> = cfg2
                .rig
                .profiles
                .iter()
                .filter(|p| p.name.ends_with("(wizard)"))
                .collect();
            assert_eq!(
                wizard_profiles.len(),
                1,
                "exactly one wizard profile should exist after re-run"
            );
        });
    }
}
