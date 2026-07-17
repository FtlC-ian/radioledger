//! Tauri commands for rig profile CRUD, serial port enumeration,
//! rig model listing, and test connection.

use std::sync::{Arc, OnceLock};

use anyhow::Result;
use serde::{Deserialize, Serialize};
use tokio::sync::Mutex;
use tracing::{info, warn};

use crate::config::{RigInterfaceType, RigProfile};
use crate::error::AppError;
use crate::AppState;

pub use super::managed::RigModel;

// ---------------------------------------------------------------------------
// Rig model list (cached)
// ---------------------------------------------------------------------------

/// Thread-safe in-memory cache for the rig model list.
static RIG_MODEL_CACHE: OnceLock<Arc<Mutex<Option<Vec<RigModel>>>>> = OnceLock::new();

fn rig_model_cache() -> &'static Arc<Mutex<Option<Vec<RigModel>>>> {
    RIG_MODEL_CACHE.get_or_init(|| Arc::new(Mutex::new(None)))
}

/// Run `rigctl -l` and parse the output into a `Vec<RigModel>`.
async fn fetch_rig_models() -> Result<Vec<RigModel>> {
    let rigctl = super::managed::detect_rigctl()
        .ok_or_else(|| anyhow::anyhow!("rigctl not found — install Hamlib"))?;

    let output = tokio::process::Command::new(&rigctl)
        .arg("-l")
        .output()
        .await
        .map_err(|e| anyhow::anyhow!("Failed to run rigctl -l: {e}"))?;

    let stdout = String::from_utf8_lossy(&output.stdout);
    let models = super::managed::parse_rigctl_list(&stdout);
    Ok(models)
}

/// Tauri command: return the full rig model list (from `rigctl -l`).
/// Cached after the first successful call; pass `force_refresh: true` to re-run.
#[tauri::command]
pub async fn get_rig_models(force_refresh: Option<bool>) -> Result<Vec<RigModel>, AppError> {
    let cache = rig_model_cache();
    let mut guard = cache.lock().await;

    let refresh = force_refresh.unwrap_or(false);
    if !refresh {
        if let Some(cached) = guard.as_ref() {
            return Ok(cached.clone());
        }
    }

    match fetch_rig_models().await {
        Ok(models) => {
            *guard = Some(models.clone());
            Ok(models)
        }
        Err(err) => {
            warn!("get_rig_models: {err}");
            // Return empty list rather than an error so the UI can still render.
            Ok(Vec::new())
        }
    }
}

// ---------------------------------------------------------------------------
// Serial port enumeration
// ---------------------------------------------------------------------------

/// A serial port discovered on the system.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct SerialPortInfo {
    pub port_name: String,
    pub description: String,
}

/// Tauri command: list available serial ports.
#[tauri::command]
pub async fn list_serial_ports() -> Result<Vec<SerialPortInfo>, AppError> {
    match serialport::available_ports() {
        Ok(ports) => {
            let result = ports
                .into_iter()
                .map(|p| {
                    let description = match &p.port_type {
                        serialport::SerialPortType::UsbPort(info) => {
                            let product = info.product.as_deref().unwrap_or("");
                            let manufacturer = info.manufacturer.as_deref().unwrap_or("");
                            if !product.is_empty() && !manufacturer.is_empty() {
                                format!("{manufacturer} {product}")
                            } else if !product.is_empty() {
                                product.to_string()
                            } else if !manufacturer.is_empty() {
                                manufacturer.to_string()
                            } else {
                                "USB Serial".to_string()
                            }
                        }
                        serialport::SerialPortType::BluetoothPort => "Bluetooth Serial".to_string(),
                        serialport::SerialPortType::PciPort => "PCI Serial".to_string(),
                        serialport::SerialPortType::Unknown => String::new(),
                    };

                    SerialPortInfo {
                        port_name: p.port_name,
                        description,
                    }
                })
                .collect();
            Ok(result)
        }
        Err(err) => {
            warn!("list_serial_ports: {err}");
            Ok(Vec::new())
        }
    }
}

// ---------------------------------------------------------------------------
// Profile CRUD Tauri commands
// ---------------------------------------------------------------------------

/// Tauri command: list all rig profiles.
#[tauri::command]
pub async fn list_rig_profiles(
    _state: tauri::State<'_, AppState>,
) -> Result<Vec<RigProfile>, AppError> {
    let cfg = crate::config::load().map_err(|e| AppError::Config(e.to_string()))?;
    Ok(cfg.rig.profiles)
}

/// Tauri command: return the currently active rig profile ID, if any.
#[tauri::command]
pub async fn get_active_rig_profile_id(
    _state: tauri::State<'_, AppState>,
) -> Result<Option<String>, AppError> {
    let cfg = crate::config::load().map_err(|e| AppError::Config(e.to_string()))?;
    Ok(cfg.rig.active_profile_id)
}

/// Tauri command: create a new rig profile and save it to config.
///
/// The `id` field of the provided profile is ignored — a new UUID is generated.
#[tauri::command]
pub async fn create_rig_profile(
    mut profile: RigProfile,
    state: tauri::State<'_, AppState>,
) -> Result<RigProfile, AppError> {
    // Always generate a fresh ID.
    profile.id = uuid::Uuid::new_v4().to_string();

    let mut cfg = crate::config::load().map_err(|e| AppError::Config(e.to_string()))?;
    cfg.rig.profiles.push(profile.clone());
    crate::config::save(&cfg).map_err(|e| AppError::Config(e.to_string()))?;

    // Update the rig state's config mirror.
    {
        let mut rig = state.rig.lock().await;
        rig.update_config_rig(cfg.rig);
    }

    info!("Created rig profile: {} ({})", profile.name, profile.id);
    Ok(profile)
}

/// Tauri command: update an existing rig profile.
#[tauri::command]
pub async fn update_rig_profile(
    profile: RigProfile,
    state: tauri::State<'_, AppState>,
) -> Result<RigProfile, AppError> {
    let mut cfg = crate::config::load().map_err(|e| AppError::Config(e.to_string()))?;

    let pos = cfg
        .rig
        .profiles
        .iter()
        .position(|p| p.id == profile.id)
        .ok_or_else(|| AppError::Config(format!("Profile not found: {}", profile.id)))?;

    cfg.rig.profiles[pos] = profile.clone();
    crate::config::save(&cfg).map_err(|e| AppError::Config(e.to_string()))?;

    // If this is the active profile, apply the update.
    {
        let mut rig = state.rig.lock().await;
        rig.update_config_rig(cfg.rig);
    }

    info!("Updated rig profile: {} ({})", profile.name, profile.id);
    Ok(profile)
}

/// Tauri command: delete a rig profile by ID.
#[tauri::command]
pub async fn delete_rig_profile(
    profile_id: String,
    state: tauri::State<'_, AppState>,
) -> Result<(), AppError> {
    let mut cfg = crate::config::load().map_err(|e| AppError::Config(e.to_string()))?;

    let before = cfg.rig.profiles.len();
    cfg.rig.profiles.retain(|p| p.id != profile_id);
    if cfg.rig.profiles.len() == before {
        return Err(AppError::Config(format!("Profile not found: {profile_id}")));
    }

    // If this was the active profile, clear it.
    if cfg.rig.active_profile_id.as_deref() == Some(&profile_id) {
        cfg.rig.active_profile_id = None;
    }

    crate::config::save(&cfg).map_err(|e| AppError::Config(e.to_string()))?;

    {
        let mut rig = state.rig.lock().await;
        rig.update_config_rig(cfg.rig);
    }

    info!("Deleted rig profile: {profile_id}");
    Ok(())
}

/// Tauri command: set the active rig profile.
///
/// Disconnects any current rig connection and reconnects using the new profile.
#[tauri::command]
pub async fn set_active_rig_profile(
    profile_id: String,
    state: tauri::State<'_, AppState>,
    _app: tauri::AppHandle,
) -> Result<(), AppError> {
    let mut cfg = crate::config::load().map_err(|e| AppError::Config(e.to_string()))?;

    // Validate the profile exists.
    if !cfg.rig.profiles.iter().any(|p| p.id == profile_id) {
        return Err(AppError::Config(format!("Profile not found: {profile_id}")));
    }

    cfg.rig.active_profile_id = Some(profile_id.clone());
    crate::config::save(&cfg).map_err(|e| AppError::Config(e.to_string()))?;

    {
        let mut rig = state.rig.lock().await;
        rig.update_config_rig(cfg.rig);
        // Disconnect current, will auto-reconnect on next poll if profile is active.
        rig.disconnect();
    }

    info!("Active rig profile set to: {profile_id}");
    Ok(())
}

// ---------------------------------------------------------------------------
// Test connection
// ---------------------------------------------------------------------------

/// Result returned by `test_rig_connection`.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct TestConnectionResult {
    pub success: bool,
    pub message: String,
    pub frequency_hz: Option<u64>,
    pub mode: Option<String>,
}

/// Tauri command: test a rig connection using the given profile.
///
/// For Hamlib: spawns a temporary rigctld, connects, reads freq+mode, stops.
/// For FLRig: connects XML-RPC, reads freq+mode.
/// For External: connects TCP, reads freq+mode.
#[tauri::command]
pub async fn test_rig_connection(profile: RigProfile) -> Result<TestConnectionResult, AppError> {
    match profile.interface_type {
        RigInterfaceType::Hamlib => test_hamlib_connection(&profile).await,
        RigInterfaceType::Flrig => test_flrig_connection(&profile).await,
        RigInterfaceType::External => test_external_connection(&profile).await,
        RigInterfaceType::Tci => test_tci_connection(&profile).await,
    }
    .map_err(|e| AppError::Rig(e.to_string()))
}

async fn test_hamlib_connection(profile: &RigProfile) -> Result<TestConnectionResult> {
    // Pick a high TCP port for the test to avoid conflicting with any running rigctld.
    let test_port: u16 = 14532;

    let mut managed = match super::managed::ManagedRigctld::start(profile, test_port).await {
        Ok(m) => m,
        Err(err) => {
            return Ok(TestConnectionResult {
                success: false,
                message: format!("Failed to start rigctld: {err}"),
                frequency_hz: None,
                mode: None,
            });
        }
    };

    // Give it a moment to be ready.
    tokio::time::sleep(std::time::Duration::from_millis(800)).await;

    let result = test_external_connection(&RigProfile {
        interface_type: RigInterfaceType::External,
        host: "127.0.0.1".to_string(),
        port: test_port,
        ..profile.clone()
    })
    .await;

    let _ = managed.stop().await;

    result
}

async fn test_flrig_connection(profile: &RigProfile) -> Result<TestConnectionResult> {
    use crate::rig::flrig::FlrigController;
    use crate::rig::RigController;

    match FlrigController::connect(profile.host.clone(), profile.port).await {
        Ok(ctrl) => {
            let freq = ctrl.get_frequency().await.ok();
            let mode = ctrl.get_mode().await.ok();
            Ok(TestConnectionResult {
                success: true,
                message: format!("Connected to FLRig at {}:{}", profile.host, profile.port),
                frequency_hz: freq,
                mode,
            })
        }
        Err(err) => Ok(TestConnectionResult {
            success: false,
            message: format!("FLRig connection failed: {err}"),
            frequency_hz: None,
            mode: None,
        }),
    }
}

async fn test_external_connection(profile: &RigProfile) -> Result<TestConnectionResult> {
    use crate::rig::rigctld::RigctldController;
    use crate::rig::RigController;

    match RigctldController::connect(profile.host.clone(), profile.port).await {
        Ok(ctrl) => {
            let freq = ctrl.get_frequency().await.ok();
            let mode = ctrl.get_mode().await.ok();
            Ok(TestConnectionResult {
                success: true,
                message: format!("Connected to rigctld at {}:{}", profile.host, profile.port),
                frequency_hz: freq,
                mode,
            })
        }
        Err(err) => Ok(TestConnectionResult {
            success: false,
            message: format!("rigctld connection failed: {err}"),
            frequency_hz: None,
            mode: None,
        }),
    }
}

async fn test_tci_connection(profile: &RigProfile) -> Result<TestConnectionResult> {
    use crate::rig::tci::TciController;
    use crate::rig::RigController;

    match TciController::connect(profile.host.clone(), profile.port, profile.tci_receiver.unwrap_or(0)).await {
        Ok(ctrl) => {
            let freq = ctrl.get_frequency().await.ok();
            let mode = ctrl.get_mode().await.ok();
            ctrl.disconnect().await;
            Ok(TestConnectionResult {
                success: true,
                message: format!("Connected to TCI at {}:{}", profile.host, profile.port),
                frequency_hz: freq,
                mode,
            })
        }
        Err(err) => Ok(TestConnectionResult {
            success: false,
            message: format!("TCI connection failed: {err}"),
            frequency_hz: None,
            mode: None,
        }),
    }
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

#[cfg(test)]
mod tests {
    use super::*;
    use crate::config::{FlowControl, Parity, PttType, RigInterfaceType, RigProfile};

    #[test]
    fn rig_profile_has_generated_id() {
        let p1 = RigProfile::default();
        let p2 = RigProfile::default();
        assert_ne!(p1.id, p2.id);
    }

    #[test]
    fn rig_profile_default_values() {
        let p = RigProfile::default();
        assert_eq!(p.baud_rate, 9600);
        assert_eq!(p.data_bits, 8);
        assert_eq!(p.stop_bits, 1);
        assert!(matches!(p.interface_type, RigInterfaceType::Hamlib));
        assert!(matches!(p.flow_control, FlowControl::None));
        assert!(matches!(p.parity, Parity::None));
        assert!(matches!(p.ptt_type, PttType::None));
    }

    #[test]
    fn serial_port_info_serialize() {
        let info = SerialPortInfo {
            port_name: "/dev/cu.usbserial-0001".to_string(),
            description: "CP2102".to_string(),
        };
        let json = serde_json::to_string(&info).unwrap();
        assert!(json.contains("port_name"));
        assert!(json.contains("description"));
    }
}
