//! Application configuration (`~/.radioledger/config.yaml`).
//!
//! Stores application settings and, when OS keychain storage is unavailable,
//! auth-token fallback fields needed to keep desktop login working on current
//! unsigned builds.

use anyhow::Context;
use serde::{Deserialize, Serialize};
use std::path::PathBuf;

/// Authentication mode — controls whether the desktop uses OAuth2 PKCE (cloud)
/// or direct email/password login against a self-hosted instance.
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq, Default)]
#[serde(rename_all = "snake_case")]
pub enum AuthMode {
    #[default]
    Cloud, // OAuth2 PKCE against radioledger.app
    Local, // Email/password against self-hosted instance
}

/// Auth metadata stored in config.yaml.
///
/// Tokens are stored in the OS keychain when available. If keychain access is
/// unavailable or intentionally disabled for the current build, desktop falls
/// back to these fields in config.yaml so auth still works.
#[derive(Debug, Clone, Serialize, Deserialize, Default)]
#[serde(default)]
pub struct AuthConfig {
    /// User's callsign (from server login response or id_token claim).
    #[serde(skip_serializing_if = "Option::is_none")]
    pub callsign: Option<String>,

    /// Access token fallback when keychain storage is unavailable.
    #[serde(skip_serializing_if = "Option::is_none")]
    pub access_token: Option<String>,
    /// Refresh token fallback when keychain storage is unavailable.
    #[serde(skip_serializing_if = "Option::is_none")]
    pub refresh_token: Option<String>,
    /// ID token fallback when keychain storage is unavailable.
    #[serde(skip_serializing_if = "Option::is_none")]
    pub id_token: Option<String>,
}

/// Top-level configuration structure mirroring `~/.radioledger/config.yaml`.
#[derive(Debug, Clone, Serialize, Deserialize, Default)]
#[serde(default)]
pub struct Config {
    pub server: ServerConfig,
    pub udp: UdpConfig,
    pub rig: RigConfig,
    pub lotw: LotwConfig,
    pub sync: SyncConfig,
    pub logbook: LogbookConfig,
    /// Authentication mode (cloud OAuth2 or local email/password).
    pub auth_mode: AuthMode,
    /// Set to true after the first-run setup wizard is completed.
    pub setup_complete: bool,
    /// Non-secret auth metadata (for example callsign).
    #[serde(default)]
    pub auth: AuthConfig,
}

impl Config {
    /// Serialises `self` to `~/.radioledger/config.yaml`, creating the
    /// directory if needed.
    pub fn save(&self) -> anyhow::Result<()> {
        save(self)
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(default)]
pub struct LogbookConfig {
    pub visible_columns: Vec<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub default_uuid: Option<String>,
}

impl Default for LogbookConfig {
    fn default() -> Self {
        LogbookConfig {
            visible_columns: vec![
                "datetime_on".to_string(),
                "callsign".to_string(),
                "band".to_string(),
                "mode".to_string(),
                "rst_sent".to_string(),
                "rst_rcvd".to_string(),
                "name".to_string(),
                "qth".to_string(),
                "country".to_string(),
                "notes".to_string(),
            ],
            default_uuid: None,
        }
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ServerConfig {
    pub url: String,
}

impl Default for ServerConfig {
    fn default() -> Self {
        ServerConfig {
            url: "https://radioledger.app".into(),
        }
    }
}

pub const DEFAULT_FT8BATTLE_ENDPOINT: &str = "ft8battle.com:2237";

/// FT8Battle relay configuration used by the WSJT-X integration path.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(default)]
pub struct Ft8BattleRelayConfig {
    /// When true, WSJT-X logged QSOs are relayed to FT8Battle after they are
    /// accepted into RadioLedger's normal local queue.
    pub enabled: bool,
    /// UDP destination for FT8Battle relay traffic.
    ///
    /// We default to the FT8Battle hostname instead of the currently published
    /// raw IP so endpoint changes can be handled via DNS rather than a desktop
    /// release.
    pub endpoint: String,
}

impl Default for Ft8BattleRelayConfig {
    fn default() -> Self {
        Self {
            enabled: false,
            endpoint: DEFAULT_FT8BATTLE_ENDPOINT.to_string(),
        }
    }
}

/// UDP listener configuration for each supported logging application.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(default)]
pub struct UdpConfig {
    pub wsjtx: UdpSourceConfig,
    pub js8call: UdpSourceConfig,
    pub n1mm: UdpSourceConfig,
    pub ft8battle: Ft8BattleRelayConfig,
}

impl Default for UdpConfig {
    fn default() -> Self {
        UdpConfig {
            wsjtx: UdpSourceConfig {
                enabled: true,
                port: 2237,
                // SECURITY: bind to loopback only. Binding to 0.0.0.0 exposes the
                // UDP listener to the entire network, enabling QSO injection from
                // any host on the LAN. Only change if WSJT-X runs on a different
                // machine (uncommon), and only after understanding the risk.
                bind: "127.0.0.1".into(),
                auto_log: true,
                multicast_group: None,
                auto_start: false,
            },
            js8call: UdpSourceConfig {
                enabled: false,
                port: 2242,
                bind: "127.0.0.1".into(),
                auto_log: true,
                multicast_group: None,
                auto_start: false,
            },
            n1mm: UdpSourceConfig {
                enabled: false,
                port: 12060,
                bind: "127.0.0.1".into(),
                auto_log: true,
                multicast_group: None,
                auto_start: false,
            },
            ft8battle: Ft8BattleRelayConfig::default(),
        }
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct UdpSourceConfig {
    pub enabled: bool,
    pub port: u16,
    /// IP address to bind the UDP socket. Default: "127.0.0.1" (loopback).
    /// WARNING: changing this to "0.0.0.0" opens the listener to all network
    /// interfaces and enables QSO injection from untrusted hosts.
    pub bind: String,
    pub auto_log: bool,
    /// Optional multicast group address (e.g. "224.0.0.73").
    /// When set, the listener joins this multicast group instead of
    /// binding to loopback only.
    ///
    /// NOTE: multicast inherently requires binding to 0.0.0.0 (INADDR_ANY),
    /// but multicast group membership limits which traffic is received.
    #[serde(default)]
    pub multicast_group: Option<String>,
    /// If true, this listener is started automatically when the app launches.
    /// Defaults to false for backward compatibility.
    #[serde(default, skip_serializing_if = "std::ops::Not::not")]
    pub auto_start: bool,
}

/// Preferred rig control backend.
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq, Default)]
#[serde(rename_all = "snake_case")]
pub enum RigControlMethod {
    /// Disable rig control integration.
    #[default]
    #[serde(alias = "auto")]
    None,
    /// Flrig XML-RPC backend.
    Flrig,
    /// Hamlib via rigctld TCP backend.
    #[serde(alias = "rigctld")]
    Hamlib,
}

/// Interface type for a rig profile.
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq, Default)]
#[serde(rename_all = "snake_case")]
pub enum RigInterfaceType {
    /// RadioLedger manages rigctld subprocess (direct serial rig control).
    #[default]
    Hamlib,
    /// Connect to a running FLRig instance via XML-RPC.
    Flrig,
    /// Connect to a user-managed rigctld instance via TCP.
    External,
    /// Connect to a TCI-compatible radio via WebSocket.
    Tci,
}

/// Flow control options for serial port.
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq, Default)]
#[serde(rename_all = "snake_case")]
pub enum FlowControl {
    #[default]
    None,
    Hardware,
    Software,
}

/// Parity options for serial port.
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq, Default)]
#[serde(rename_all = "snake_case")]
pub enum Parity {
    #[default]
    None,
    Even,
    Odd,
}

/// PTT type for rig control.
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq, Default)]
#[serde(rename_all = "snake_case")]
pub enum PttType {
    Cat,
    Dtr,
    Rts,
    #[default]
    None,
}

/// A saved rig profile (one per rig setup).
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(default)]
pub struct RigProfile {
    /// Unique ID (UUID v4).
    pub id: String,
    /// Human-readable profile name (e.g. "Kenwood TS-590SG – shack").
    pub name: String,
    /// Which interface type this profile uses.
    pub interface_type: RigInterfaceType,

    // --- Hamlib / serial rig fields ---
    /// Hamlib model ID (from rigctl -l).
    pub rig_model_id: u32,
    /// Hamlib model name (display only).
    pub rig_model_name: String,
    /// Serial port path (e.g. /dev/cu.usbserial-0001 or COM3).
    pub serial_port: String,
    /// Baud rate.
    pub baud_rate: u32,
    /// Data bits (7 or 8).
    pub data_bits: u8,
    /// Stop bits (1 or 2).
    pub stop_bits: u8,
    /// Flow control.
    pub flow_control: FlowControl,
    /// Parity.
    pub parity: Parity,
    /// CIV address (hex, optional — for Icom rigs).
    #[serde(skip_serializing_if = "Option::is_none")]
    pub civ_address: Option<String>,
    /// PTT type.
    pub ptt_type: PttType,

    // --- Network rig fields (FLRig / External rigctld / TCI) ---
    /// Host for FLRig, external rigctld, or TCI.
    pub host: String,
    /// Port for FLRig (default 12345), external rigctld (default 4532), or TCI (default 50001).
    pub port: u16,

    /// TCI receiver index (0 = RX1, 1 = RX2, …). Defaults to 0 (RX1) when absent.
    /// Only meaningful when `interface_type` is `Tci`.
    #[serde(skip_serializing_if = "Option::is_none")]
    pub tci_receiver: Option<u32>,

    /// Poll interval in milliseconds.
    pub poll_interval_ms: u64,
}

impl Default for RigProfile {
    fn default() -> Self {
        RigProfile {
            id: uuid::Uuid::new_v4().to_string(),
            name: "Default Profile".to_string(),
            interface_type: RigInterfaceType::Hamlib,
            rig_model_id: 1,
            rig_model_name: "Hamlib Dummy".to_string(),
            serial_port: String::new(),
            baud_rate: 9600,
            data_bits: 8,
            stop_bits: 1,
            flow_control: FlowControl::None,
            parity: Parity::None,
            civ_address: None,
            ptt_type: PttType::None,
            host: "127.0.0.1".to_string(),
            port: 4532,
            tci_receiver: None,
            poll_interval_ms: 1000,
        }
    }
}

/// Rig control connection and polling settings.
///
/// Supports both the legacy single-rig config (for backward compatibility)
/// and the new profiles-based system.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(default)]
pub struct RigConfig {
    // --- Legacy single-rig fields (kept for backward compat) ---
    pub preferred_method: RigControlMethod,
    pub host: String,
    pub flrig_port: u16,
    pub rigctld_port: u16,
    /// Rig polling interval. Default 1000ms.
    pub poll_interval_ms: u64,

    // --- New profiles system ---
    /// All saved rig profiles.
    #[serde(default)]
    pub profiles: Vec<RigProfile>,
    /// ID of the currently active profile (None = no active profile).
    #[serde(skip_serializing_if = "Option::is_none")]
    pub active_profile_id: Option<String>,
}

impl Default for RigConfig {
    fn default() -> Self {
        RigConfig {
            preferred_method: RigControlMethod::None,
            host: "127.0.0.1".to_string(),
            flrig_port: 12345,
            rigctld_port: 4532,
            poll_interval_ms: 1000,
            profiles: Vec::new(),
            active_profile_id: None,
        }
    }
}

impl RigConfig {
    /// Return the active profile, if any.
    pub fn active_profile(&self) -> Option<&RigProfile> {
        let id = self.active_profile_id.as_deref()?;
        self.profiles.iter().find(|p| p.id == id)
    }

    /// Migrate legacy single-rig config into a profile, if profiles are empty
    /// and a non-None preferred_method was set.
    pub fn migrate_legacy(&mut self) {
        if !self.profiles.is_empty() {
            return;
        }
        if self.preferred_method == RigControlMethod::None {
            return;
        }

        let interface_type = match self.preferred_method {
            RigControlMethod::Flrig => RigInterfaceType::Flrig,
            RigControlMethod::Hamlib => RigInterfaceType::External,
            RigControlMethod::None => return,
        };

        let port = match self.preferred_method {
            RigControlMethod::Flrig => self.flrig_port,
            RigControlMethod::Hamlib => self.rigctld_port,
            RigControlMethod::None => self.rigctld_port,
        };

        let profile = RigProfile {
            id: uuid::Uuid::new_v4().to_string(),
            name: "Migrated Profile".to_string(),
            interface_type,
            host: self.host.clone(),
            port,
            poll_interval_ms: self.poll_interval_ms,
            ..Default::default()
        };

        let id = profile.id.clone();
        self.profiles.push(profile);
        self.active_profile_id = Some(id);
    }
}

/// LoTW/tQSL integration settings.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(default)]
pub struct LotwConfig {
    /// Optional manual override for the local tQSL executable.
    pub tqsl_path: Option<String>,
    /// Optional station location name passed to tQSL.
    pub station_location: Option<String>,
    /// Optional callsign hint for certificate info.
    pub cert_callsign: Option<String>,
    /// Whether sign_and_upload should pass upload flags by default.
    pub auto_upload: bool,
    /// Optional API path used to report LoTW sync status back to server.
    /// Example: /v1/sync/services/lotw/status
    pub status_endpoint: String,
    /// Optional path used when parsing LoTW confirmations ADIF.
    pub confirmation_report_path: Option<String>,
    /// Optional custom tQSL args for downloading confirmations.
    /// Use {output} placeholder for output path.
    pub confirmation_download_args: Vec<String>,
}

impl Default for LotwConfig {
    fn default() -> Self {
        LotwConfig {
            tqsl_path: None,
            station_location: None,
            cert_callsign: None,
            auto_upload: true,
            status_endpoint: "/v1/sync/services/lotw/status".to_string(),
            confirmation_report_path: None,
            confirmation_download_args: Vec::new(),
        }
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(default)]
pub struct SyncConfig {
    pub interval_seconds: u64,
}

impl Default for SyncConfig {
    fn default() -> Self {
        SyncConfig {
            interval_seconds: 300,
        }
    }
}

/// Returns the path to the RadioLedger config directory (`~/.radioledger/`).
pub fn config_dir() -> anyhow::Result<PathBuf> {
    if let Some(override_dir) = std::env::var_os("RADIOLEDGER_CONFIG_DIR") {
        return Ok(PathBuf::from(override_dir));
    }

    let home = dirs::home_dir().context("Could not determine home directory")?;
    Ok(home.join(".radioledger"))
}

/// Serialises `cfg` to `~/.radioledger/config.yaml`.
///
/// Creates the config directory if it does not exist.
pub fn save(cfg: &Config) -> anyhow::Result<()> {
    let dir = config_dir()?;
    std::fs::create_dir_all(&dir).context("Failed to create ~/.radioledger config directory")?;
    let path = dir.join("config.yaml");
    let raw = serde_yaml::to_string(cfg).context("Failed to serialise config to YAML")?;
    std::fs::write(&path, raw).context("Failed to write config.yaml")?;
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        std::fs::set_permissions(&path, std::fs::Permissions::from_mode(0o600))
            .context("Failed to restrict config.yaml permissions")?;
    }
    Ok(())
}

/// Loads `~/.radioledger/config.yaml`, returning defaults if the file does not
/// exist. Creates the config directory on first run.
pub fn load() -> anyhow::Result<Config> {
    let dir = config_dir()?;
    std::fs::create_dir_all(&dir).context("Failed to create ~/.radioledger config directory")?;

    let path = dir.join("config.yaml");
    if !path.exists() {
        return Ok(Config::default());
    }

    let raw = std::fs::read_to_string(&path).context("Failed to read config.yaml")?;
    let mut cfg: Config = serde_yaml::from_str(&raw).context("Failed to parse config.yaml")?;
    // Normalize: strip trailing slashes from server URL to prevent double-slash bugs.
    cfg.server.url = cfg.server.url.trim_end_matches('/').to_string();

    // Persist legacy single-rig configs as profiles the first time we load them
    // so the Settings UI and subsequent loads see the migrated profile.
    let needs_rig_migration = cfg.rig.profiles.is_empty()
        && cfg.rig.active_profile_id.is_none()
        && cfg.rig.preferred_method != RigControlMethod::None;
    if needs_rig_migration {
        cfg.rig.migrate_legacy();
        save(&cfg).context("Failed to persist migrated rig config")?;
    }

    Ok(cfg)
}
