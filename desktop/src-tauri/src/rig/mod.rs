//! Rig control abstraction and polling runtime.

use std::fmt::{Display, Formatter};
use std::sync::Arc;
use std::time::{Duration, Instant};

use serde::{Deserialize, Serialize};
use tauri::Emitter;
use tokio::sync::Mutex;
use tracing::{debug, info, warn};

use crate::config::{RigConfig, RigControlMethod, RigInterfaceType, RigProfile};
use crate::error::AppError;
use crate::AppState;

pub mod flrig;
pub mod managed;
pub mod profiles;
pub mod rigctld;
pub mod tci;

pub const EVENT_RIG_FREQUENCY_CHANGED: &str = "rig_frequency_changed";
pub const EVENT_RIG_MODE_CHANGED: &str = "rig_mode_changed";
pub const EVENT_RIG_STATUS_CHANGED: &str = "rig_status_changed";

/// Supported rig control backends.
#[derive(Debug, Clone, Copy, Serialize, Deserialize, PartialEq, Eq)]
#[serde(rename_all = "snake_case")]
pub enum RigBackend {
    Flrig,
    Hamlib,
    Tci,
}

impl Display for RigBackend {
    fn fmt(&self, f: &mut Formatter<'_>) -> std::fmt::Result {
        match self {
            RigBackend::Flrig => write!(f, "flrig"),
            RigBackend::Hamlib => write!(f, "hamlib"),
            RigBackend::Tci => write!(f, "tci"),
        }
    }
}

/// Full rig telemetry snapshot.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct RigTelemetry {
    pub frequency_hz: Option<u64>,
    pub mode: Option<String>,
    pub bandwidth_hz: Option<i32>,
    pub s_meter: Option<f32>,
    pub power: Option<f32>,
    pub vfo: Option<String>,
    pub strength: Option<f32>,
}

/// Frontend-ready runtime status.
#[derive(Debug, Clone, Serialize, Deserialize, Default, PartialEq)]
pub struct RigStatus {
    pub connected: bool,
    pub backend: Option<String>,
    pub host: Option<String>,
    pub port: Option<u16>,
    pub frequency_hz: Option<u64>,
    pub frequency_display: Option<String>,
    pub mode: Option<String>,
    pub band: Option<String>,
    pub bandwidth_hz: Option<i32>,
    pub s_meter: Option<f32>,
    pub power: Option<f32>,
    pub vfo: Option<String>,
    pub strength: Option<f32>,
    pub last_error: Option<String>,
}

#[derive(Debug, Clone, Serialize)]
pub struct RigFrequencyChangedEvent {
    pub frequency_hz: u64,
    pub frequency_display: String,
    pub band: String,
}

#[derive(Debug, Clone, Serialize)]
pub struct RigModeChangedEvent {
    pub mode: String,
}

/// Parameters for an explicit `connect_rig` call from the frontend.
/// All fields are optional — omit `backend` to use auto-detect.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct ConnectRigParams {
    /// `"flrig"` or `"hamlib"` (`"rigctld"` alias supported).
    /// When `None`, uses configured preferred backend.
    pub backend: Option<String>,
    /// Override host (defaults to config host).
    pub host: Option<String>,
    /// Override port (defaults to backend's configured port).
    pub port: Option<u16>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct RigSettings {
    pub control_type: String,
    pub host: String,
    pub port: u16,
    pub poll_interval_ms: u64,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct SaveRigSettingsRequest {
    pub control_type: String,
    pub host: String,
    pub port: u16,
    pub poll_interval_ms: u64,
}

/// Common CAT operations across supported rig control backends.
#[async_trait::async_trait]
pub trait RigController: Send + Sync {
    fn backend(&self) -> RigBackend;
    fn host(&self) -> &str;
    fn port(&self) -> u16;

    async fn get_frequency(&self) -> anyhow::Result<u64>;
    async fn get_mode(&self) -> anyhow::Result<String>;
    async fn set_frequency(&self, frequency_hz: u64) -> anyhow::Result<()>;
    async fn set_mode(&self, mode: &str) -> anyhow::Result<()>;

    async fn get_telemetry(&self) -> anyhow::Result<RigTelemetry> {
        let frequency = self.get_frequency().await?;
        let mode = self.get_mode().await?;

        Ok(RigTelemetry {
            frequency_hz: Some(frequency),
            mode: Some(mode),
            ..RigTelemetry::default()
        })
    }
}

/// Runtime rig manager (wrapped in a mutex in AppState).
pub struct RigState {
    config: RigConfig,
    controller: Option<Arc<dyn RigController>>,
    status: RigStatus,
    last_frequency_hz: Option<u64>,
    last_mode: Option<String>,
    last_detect_attempt: Option<Instant>,
    reconnect_backoff: Duration,
    detect_failures: u32,
    /// When `true`, polling stays disabled until an explicit connect.
    pub user_disconnected: bool,
    /// Managed rigctld subprocess (for Hamlib profiles).
    managed_rigctld: Option<managed::ManagedRigctld>,
}

impl RigState {
    pub fn new(mut config: RigConfig) -> Self {
        // Migrate legacy single-rig config on first load.
        config.migrate_legacy();
        // Use the resolved active_profile() rather than the raw active_profile_id
        // so that a dangling ID (profile deleted but ID not cleared) is treated the
        // same as no active profile.  This is the defensive half of the fix for
        // issue #190: even if a stale active_profile_id slips through (e.g. from an
        // older config on disk), RigState still starts in the clean disabled state.
        let user_disconnected = matches!(config.preferred_method, RigControlMethod::None)
            && config.active_profile().is_none();
        Self {
            config,
            controller: None,
            status: RigStatus::default(),
            last_frequency_hz: None,
            last_mode: None,
            last_detect_attempt: None,
            reconnect_backoff: Duration::from_secs(2),
            detect_failures: 0,
            user_disconnected,
            managed_rigctld: None,
        }
    }

    /// Update the rig config mirror (called after profile CRUD operations).
    pub fn update_config_rig(&mut self, rig_cfg: RigConfig) {
        self.config = rig_cfg;
        self.last_detect_attempt = None;
        self.reconnect_backoff = Duration::from_secs(2);
        self.detect_failures = 0;
    }

    pub fn poll_interval(&self) -> Duration {
        Duration::from_millis(self.config.poll_interval_ms.max(100))
    }

    pub fn status(&self) -> RigStatus {
        self.status.clone()
    }

    pub fn frequency(&self) -> Option<u64> {
        self.status.frequency_hz
    }

    pub fn settings(&self) -> RigSettings {
        let control_type = match self.config.preferred_method {
            RigControlMethod::None => "none",
            RigControlMethod::Flrig => "flrig",
            RigControlMethod::Hamlib => "hamlib",
        }
        .to_string();

        let port = match self.config.preferred_method {
            RigControlMethod::Flrig => self.config.flrig_port,
            RigControlMethod::Hamlib | RigControlMethod::None => self.config.rigctld_port,
        };

        RigSettings {
            control_type,
            host: self.config.host.clone(),
            port,
            poll_interval_ms: self.config.poll_interval_ms,
        }
    }

    pub fn apply_config(&mut self, config: RigConfig) {
        self.config = config;
        self.controller = None;
        self.managed_rigctld = None; // Drop existing subprocess on config change.
        self.last_detect_attempt = None;
        self.reconnect_backoff = Duration::from_secs(2);
        self.detect_failures = 0;

        // Same resolved-profile check as RigState::new() — a dangling
        // active_profile_id is treated as "no active profile" for the
        // purpose of deciding whether to disconnect.  See issue #190.
        if matches!(self.config.preferred_method, RigControlMethod::None)
            && self.config.active_profile().is_none()
        {
            self.disconnect();
        } else {
            self.user_disconnected = false;
            self.status.last_error = None;
        }
    }

    /// Explicit connect: connect to a specific backend/host/port or preferred backend.
    ///
    /// If an active profile is configured, uses the profile. Otherwise falls
    /// back to legacy single-rig config or provided params.
    ///
    /// Clears `user_disconnected` so the polling loop re-enables auto-reconnect.
    pub async fn connect_explicit(&mut self, params: ConnectRigParams) -> anyhow::Result<()> {
        self.user_disconnected = false;
        self.controller = None;

        // If an active profile is set (and no explicit backend override), use it.
        if params.backend.is_none() {
            if let Some(profile) = self.config.active_profile().cloned() {
                return self.connect_profile(&profile).await;
            }
        }

        let host = params
            .host
            .clone()
            .unwrap_or_else(|| self.config.host.clone());

        match params.backend.as_deref() {
            Some("flrig") => {
                let port = params.port.unwrap_or(self.config.flrig_port);
                let controller = flrig::FlrigController::connect(host, port).await?;
                info!(
                    "connect_rig: connected to Flrig at {}:{}",
                    controller.host(),
                    controller.port()
                );
                self.attach_controller(Arc::new(controller));
            }
            Some("hamlib") | Some("rigctld") => {
                let port = params.port.unwrap_or(self.config.rigctld_port);
                let controller = rigctld::RigctldController::connect(host, port).await?;
                info!(
                    "connect_rig: connected to Hamlib rigctld at {}:{}",
                    controller.host(),
                    controller.port()
                );
                self.attach_controller(Arc::new(controller));
            }
            Some("tci") => {
                let port = params.port.unwrap_or(tci::DEFAULT_TCI_PORT);
                // Explicit connect without a profile defaults to receiver 0 (RX1).
                let controller = tci::TciController::connect(host, port, 0).await?;
                info!(
                    "connect_rig: connected to TCI at {}:{}",
                    controller.host(),
                    controller.port()
                );
                self.attach_controller(Arc::new(controller));
            }
            Some("none") => {
                self.disconnect();
            }
            None => {
                self.last_detect_attempt = None;
                self.detect_controller().await?;
            }
            Some(other) => return Err(anyhow::anyhow!("unsupported rig backend: {other}")),
        }

        Ok(())
    }

    /// Connect using a specific rig profile.
    async fn connect_profile(&mut self, profile: &RigProfile) -> anyhow::Result<()> {
        // Stop any running managed rigctld.
        if let Some(mut old) = self.managed_rigctld.take() {
            let _ = old.stop().await;
        }

        match profile.interface_type {
            RigInterfaceType::Hamlib => {
                // Spawn managed rigctld, then connect via TCP.
                let tcp_port = self.config.rigctld_port;
                let mut managed = managed::ManagedRigctld::start(profile, tcp_port).await?;
                if !managed.is_running() {
                    return Err(anyhow::anyhow!("rigctld exited immediately after start"));
                }
                let actual_port = managed.tcp_port();
                self.managed_rigctld = Some(managed);

                let controller =
                    rigctld::RigctldController::connect("127.0.0.1".to_string(), actual_port)
                        .await?;
                info!(
                    "connect_profile(Hamlib): rigctld started, connected at 127.0.0.1:{actual_port}"
                );
                self.attach_controller(Arc::new(controller));
            }
            RigInterfaceType::Flrig => {
                let port = profile.port;
                let host = profile.host.clone();
                let controller = flrig::FlrigController::connect(host.clone(), port).await?;
                info!("connect_profile(FLRig): connected at {host}:{port}");
                self.attach_controller(Arc::new(controller));
            }
            RigInterfaceType::External => {
                let port = profile.port;
                let host = profile.host.clone();
                let controller = rigctld::RigctldController::connect(host.clone(), port).await?;
                info!("connect_profile(External): connected at {host}:{port}");
                self.attach_controller(Arc::new(controller));
            }
            RigInterfaceType::Tci => {
                let port = profile.port;
                let host = profile.host.clone();
                let receiver_idx = profile.tci_receiver.unwrap_or(0);
                let controller =
                    tci::TciController::connect(host.clone(), port, receiver_idx).await?;
                info!(
                    "connect_profile(TCI): connected at {host}:{port}, receiver RX{}",
                    receiver_idx + 1
                );
                self.attach_controller(Arc::new(controller));
            }
        }

        Ok(())
    }

    /// Explicit disconnect: drop the controller and suppress auto-reconnect.
    pub fn disconnect(&mut self) {
        info!("disconnect_rig: user requested disconnect");
        self.controller = None;
        self.managed_rigctld = None;
        self.user_disconnected = true;
        self.status.connected = false;
        self.status.backend = None;
        self.status.host = None;
        self.status.port = None;
        self.status.last_error = None;
    }

    async fn detect_controller(&mut self) -> anyhow::Result<()> {
        self.last_detect_attempt = Some(Instant::now());

        // Prefer profile-based connection if an active profile is set.
        if let Some(profile) = self.config.active_profile().cloned() {
            return self.connect_profile(&profile).await;
        }

        let host = self.config.host.clone();
        let result = match self.config.preferred_method {
            RigControlMethod::None => Err(anyhow::anyhow!("rig control disabled")),
            RigControlMethod::Flrig => {
                flrig::FlrigController::connect(host, self.config.flrig_port)
                    .await
                    .map(|c| Arc::new(c) as Arc<dyn RigController>)
            }
            RigControlMethod::Hamlib => {
                rigctld::RigctldController::connect(host, self.config.rigctld_port)
                    .await
                    .map(|c| Arc::new(c) as Arc<dyn RigController>)
            }
        };

        match result {
            Ok(controller) => {
                self.detect_failures = 0;
                self.reconnect_backoff = Duration::from_secs(2);
                self.attach_controller(controller);
                Ok(())
            }
            Err(err) => {
                self.controller = None;
                self.status.connected = false;
                self.status.backend = None;
                self.status.host = None;
                self.status.port = None;
                self.status.last_error = Some(err.to_string());

                self.detect_failures = self.detect_failures.saturating_add(1);
                let seconds = 2u64.saturating_pow(self.detect_failures.min(4));
                self.reconnect_backoff = Duration::from_secs(seconds.min(30));
                debug!(
                    "Rig detect failed (attempt={} backoff={}s): {err}",
                    self.detect_failures,
                    self.reconnect_backoff.as_secs()
                );
                Err(err)
            }
        }
    }

    fn attach_controller(&mut self, controller: Arc<dyn RigController>) {
        self.status.connected = true;
        self.status.backend = Some(controller.backend().to_string());
        self.status.host = Some(controller.host().to_string());
        self.status.port = Some(controller.port());
        self.status.last_error = None;
        self.detect_failures = 0;
        self.reconnect_backoff = Duration::from_secs(2);
        self.controller = Some(controller);
    }

    async fn ensure_controller(&mut self) -> anyhow::Result<()> {
        if self.controller.is_some() {
            return Ok(());
        }

        // Respect explicit user disconnect: don't auto-reconnect.
        if self.user_disconnected {
            return Err(anyhow::anyhow!("rig disconnected by user"));
        }

        let should_probe = self
            .last_detect_attempt
            .map(|last| last.elapsed() >= self.reconnect_backoff)
            .unwrap_or(true);

        if !should_probe {
            return Err(anyhow::anyhow!("rig auto-detect backoff active"));
        }

        self.detect_controller().await
    }

    fn handle_disconnect(&mut self, err: anyhow::Error) {
        warn!("Rig control disconnected: {err}");
        self.controller = None;
        self.managed_rigctld = None;
        self.status.connected = false;
        self.status.last_error = Some(err.to_string());
        // Keep last known frequency/mode so the UI degrades gracefully.
    }

    fn apply_telemetry(&mut self, telemetry: RigTelemetry) {
        if let Some(freq) = telemetry.frequency_hz {
            self.status.frequency_hz = Some(freq);
            self.status.frequency_display = Some(format_frequency_hz(freq));
            self.status.band = Some(frequency_to_band(freq).to_string());
        }

        if let Some(mode) = telemetry.mode {
            self.status.mode = Some(mode.to_uppercase());
        }

        self.status.bandwidth_hz = telemetry.bandwidth_hz;
        self.status.s_meter = telemetry.s_meter;
        self.status.power = telemetry.power;
        self.status.vfo = telemetry.vfo;
        self.status.strength = telemetry.strength;
    }

    pub async fn poll_once(&mut self, app: &tauri::AppHandle) -> bool {
        let before = self.status.clone();

        if self.ensure_controller().await.is_err() {
            return self.status != before;
        }

        let Some(controller) = self.controller.clone() else {
            return self.status != before;
        };

        match controller.get_telemetry().await {
            Ok(telemetry) => {
                let previous_frequency = self.last_frequency_hz;
                let previous_mode = self.last_mode.clone();

                self.apply_telemetry(telemetry);
                self.status.connected = true;
                self.status.last_error = None;

                if self.status.backend.is_none() {
                    self.status.backend = Some(controller.backend().to_string());
                    self.status.host = Some(controller.host().to_string());
                    self.status.port = Some(controller.port());
                }

                let mut emitted = false;

                if let Some(frequency_hz) = self.status.frequency_hz {
                    if previous_frequency != Some(frequency_hz) {
                        self.last_frequency_hz = Some(frequency_hz);
                        emitted = true;

                        let payload = RigFrequencyChangedEvent {
                            frequency_hz,
                            frequency_display: format_frequency_hz(frequency_hz),
                            band: frequency_to_band(frequency_hz).to_string(),
                        };
                        let _ = app.emit(EVENT_RIG_FREQUENCY_CHANGED, payload);
                    }
                }

                if let Some(mode) = self.status.mode.clone() {
                    if previous_mode.as_deref() != Some(mode.as_str()) {
                        self.last_mode = Some(mode.clone());
                        emitted = true;

                        let payload = RigModeChangedEvent { mode };
                        let _ = app.emit(EVENT_RIG_MODE_CHANGED, payload);
                    }
                }

                if emitted {
                    let _ = app.emit(EVENT_RIG_STATUS_CHANGED, self.status.clone());
                }
            }
            Err(err) => {
                self.handle_disconnect(err);
                let _ = app.emit(EVENT_RIG_STATUS_CHANGED, self.status.clone());
            }
        }

        self.status != before
    }

    pub async fn set_frequency(
        &mut self,
        frequency_hz: u64,
        app: &tauri::AppHandle,
    ) -> anyhow::Result<RigStatus> {
        self.ensure_controller().await?;

        let controller = self
            .controller
            .clone()
            .ok_or_else(|| anyhow::anyhow!("no active rig controller"))?;

        controller.set_frequency(frequency_hz).await?;
        self.status.frequency_hz = Some(frequency_hz);
        self.status.frequency_display = Some(format_frequency_hz(frequency_hz));
        self.status.band = Some(frequency_to_band(frequency_hz).to_string());
        self.last_frequency_hz = Some(frequency_hz);

        let payload = RigFrequencyChangedEvent {
            frequency_hz,
            frequency_display: format_frequency_hz(frequency_hz),
            band: frequency_to_band(frequency_hz).to_string(),
        };
        let _ = app.emit(EVENT_RIG_FREQUENCY_CHANGED, payload);
        let _ = app.emit(EVENT_RIG_STATUS_CHANGED, self.status.clone());

        Ok(self.status())
    }
}

/// Continuous rig polling task (spawned at startup).
pub async fn run_rig_poll_loop(rig_state: Arc<Mutex<RigState>>, app_handle: tauri::AppHandle) {
    info!("Rig polling loop started");

    loop {
        let (changed, poll_interval) = {
            let mut rig = rig_state.lock().await;
            let changed = rig.poll_once(&app_handle).await;
            let poll_interval = rig.poll_interval();
            (changed, poll_interval)
        };

        if changed {
            crate::tray::refresh_tray(&app_handle).await;
        }

        tokio::time::sleep(poll_interval).await;
    }
}

// ---------------------------------------------------------------------------
// Tauri commands
// ---------------------------------------------------------------------------

/// Connect to a rig control backend.
///
/// Supply `ConnectRigParams` with an optional `backend` (`"flrig"` or
/// `"hamlib"`), `host`, and `port`. When `backend` is omitted, the configured
/// preferred backend is used.
///
/// Emits `rig_status_changed` on the app handle after a successful connection.
#[tauri::command]
pub async fn connect_rig(
    params: Option<ConnectRigParams>,
    state: tauri::State<'_, AppState>,
    app: tauri::AppHandle,
) -> Result<RigStatus, AppError> {
    let mut rig = state.rig.lock().await;
    rig.connect_explicit(params.unwrap_or_default())
        .await
        .map_err(|err| AppError::Rig(err.to_string()))?;
    let status = rig.status();
    let _ = app.emit(EVENT_RIG_STATUS_CHANGED, status.clone());
    drop(rig);
    crate::tray::refresh_tray(&app).await;
    Ok(status)
}

/// Disconnect from the active rig control backend.
///
/// Suppresses auto-reconnect until `connect_rig` is called again.
/// Emits `rig_status_changed` with `connected: false`.
#[tauri::command]
pub async fn disconnect_rig(
    state: tauri::State<'_, AppState>,
    app: tauri::AppHandle,
) -> Result<RigStatus, AppError> {
    let mut rig = state.rig.lock().await;
    rig.disconnect();
    let status = rig.status();
    let _ = app.emit(EVENT_RIG_STATUS_CHANGED, status.clone());
    drop(rig);
    crate::tray::refresh_tray(&app).await;
    Ok(status)
}

/// Return the current rig connection status and last known telemetry.
#[tauri::command]
pub async fn get_rig_status(state: tauri::State<'_, AppState>) -> Result<RigStatus, AppError> {
    let rig = state.rig.lock().await;
    Ok(rig.status())
}

#[tauri::command]
pub async fn get_rig_settings(state: tauri::State<'_, AppState>) -> Result<RigSettings, AppError> {
    let rig = state.rig.lock().await;
    Ok(rig.settings())
}

#[tauri::command]
pub async fn save_rig_settings(
    request: SaveRigSettingsRequest,
    state: tauri::State<'_, AppState>,
    app: tauri::AppHandle,
) -> Result<RigSettings, AppError> {
    let mut cfg = crate::config::load().map_err(|err| AppError::Config(err.to_string()))?;

    let control_type = request.control_type.to_lowercase();
    let method = match control_type.as_str() {
        "none" => RigControlMethod::None,
        "flrig" => RigControlMethod::Flrig,
        "hamlib" | "rigctld" => RigControlMethod::Hamlib,
        other => {
            return Err(AppError::Config(format!(
                "Unsupported rig control type: {other}"
            )))
        }
    };

    cfg.rig.preferred_method = method;
    cfg.rig.host = request.host.trim().to_string();
    cfg.rig.poll_interval_ms = request.poll_interval_ms.max(250);

    match cfg.rig.preferred_method {
        RigControlMethod::Flrig => cfg.rig.flrig_port = request.port,
        RigControlMethod::Hamlib => cfg.rig.rigctld_port = request.port,
        RigControlMethod::None => {}
    }

    crate::config::save(&cfg).map_err(|err| AppError::Config(err.to_string()))?;

    let mut rig = state.rig.lock().await;
    rig.apply_config(cfg.rig);
    let settings = rig.settings();
    let status = rig.status();
    drop(rig);

    let _ = app.emit(EVENT_RIG_STATUS_CHANGED, status);
    crate::tray::refresh_tray(&app).await;
    Ok(settings)
}

/// Return the last known rig frequency in Hz, or `null` if not connected.
#[tauri::command]
pub async fn get_rig_frequency(state: tauri::State<'_, AppState>) -> Result<Option<u64>, AppError> {
    let rig = state.rig.lock().await;
    Ok(rig.frequency())
}

/// Send a new VFO frequency to the rig.
#[tauri::command]
pub async fn set_rig_frequency(
    freq: u64,
    state: tauri::State<'_, AppState>,
    app: tauri::AppHandle,
) -> Result<RigStatus, AppError> {
    let mut rig = state.rig.lock().await;
    let status = rig
        .set_frequency(freq, &app)
        .await
        .map_err(|err| AppError::Rig(err.to_string()))?;
    drop(rig);
    crate::tray::refresh_tray(&app).await;
    Ok(status)
}

// ---------------------------------------------------------------------------
// Frequency / band helpers
// ---------------------------------------------------------------------------

/// Format frequency in Hz as `14.074.000` style display.
pub fn format_frequency_hz(frequency_hz: u64) -> String {
    let mhz = frequency_hz / 1_000_000;
    let khz = (frequency_hz / 1_000) % 1_000;
    let hz = frequency_hz % 1_000;
    format!("{mhz}.{khz:03}.{hz:03}")
}

/// Derive amateur band label from frequency in Hz.
pub fn frequency_to_band(frequency_hz: u64) -> &'static str {
    let mhz = frequency_hz as f64 / 1_000_000.0;
    match mhz {
        1.8..=2.0 => "160m",
        3.5..=4.0 => "80m",
        5.0..=5.5 => "60m",
        7.0..=7.3 => "40m",
        10.1..=10.15 => "30m",
        14.0..=14.35 => "20m",
        18.068..=18.168 => "17m",
        21.0..=21.45 => "15m",
        24.89..=24.99 => "12m",
        28.0..=29.7 => "10m",
        50.0..=54.0 => "6m",
        144.0..=148.0 => "2m",
        420.0..=450.0 => "70cm",
        _ => "unknown",
    }
}

#[cfg(test)]
mod tests {
    use super::{format_frequency_hz, frequency_to_band, ConnectRigParams, RigState};
    use crate::config::RigConfig;

    #[test]
    fn formats_frequency_like_radio_display() {
        assert_eq!(format_frequency_hz(14_074_000), "14.074.000");
        assert_eq!(format_frequency_hz(7_040_123), "7.040.123");
        assert_eq!(format_frequency_hz(3_573_000), "3.573.000");
        assert_eq!(format_frequency_hz(50_313_000), "50.313.000");
        assert_eq!(format_frequency_hz(144_174_000), "144.174.000");
    }

    #[test]
    fn derives_band_from_frequency() {
        assert_eq!(frequency_to_band(1_900_000), "160m");
        assert_eq!(frequency_to_band(3_573_000), "80m");
        assert_eq!(frequency_to_band(7_074_000), "40m");
        assert_eq!(frequency_to_band(10_136_000), "30m");
        assert_eq!(frequency_to_band(14_074_000), "20m");
        assert_eq!(frequency_to_band(18_100_000), "17m");
        assert_eq!(frequency_to_band(21_074_000), "15m");
        assert_eq!(frequency_to_band(24_915_000), "12m");
        assert_eq!(frequency_to_band(28_074_000), "10m");
        assert_eq!(frequency_to_band(50_313_000), "6m");
        assert_eq!(frequency_to_band(144_174_000), "2m");
        assert_eq!(frequency_to_band(432_100_000), "70cm");
        assert_eq!(frequency_to_band(123_456), "unknown");
    }

    #[test]
    fn default_config_disables_rig_polling_until_enabled() {
        let state = RigState::new(RigConfig::default());
        assert!(state.user_disconnected);
        assert!(!state.status().connected);
    }

    #[test]
    fn user_disconnect_suppresses_reconnect_after_enabled_mode() {
        let cfg = RigConfig {
            preferred_method: crate::config::RigControlMethod::Flrig,
            ..Default::default()
        };

        let mut state = RigState::new(cfg);
        assert!(!state.user_disconnected);

        state.disconnect();

        assert!(state.user_disconnected);
        assert!(!state.status().connected);
        assert!(state.status().backend.is_none());
        assert!(state.status().last_error.is_none());
    }

    #[test]
    fn connect_params_deserialization() {
        let json = r#"{"backend":"flrig","host":"192.168.1.10","port":12345}"#;
        let params: ConnectRigParams = serde_json::from_str(json).unwrap();
        assert_eq!(params.backend.as_deref(), Some("flrig"));
        assert_eq!(params.host.as_deref(), Some("192.168.1.10"));
        assert_eq!(params.port, Some(12345));
    }

    #[test]
    fn connect_params_defaults_to_autodetect() {
        let json = r#"{}"#;
        let params: ConnectRigParams = serde_json::from_str(json).unwrap();
        assert!(params.backend.is_none());
        assert!(params.host.is_none());
        assert!(params.port.is_none());
    }

    // ── Issue #190: dangling active_profile_id treated as disabled ──

    /// A config with preferred_method=None and a dangling active_profile_id
    /// (the profile no longer exists in the list) must behave like a fully
    /// disabled rig config: user_disconnected=true, not connected.
    #[test]
    fn dangling_active_profile_id_treated_as_disabled_in_new() {
        let cfg = RigConfig {
            preferred_method: crate::config::RigControlMethod::None,
            // Stale ID — no matching profile in the profiles list.
            active_profile_id: Some("stale-id-that-does-not-exist".to_string()),
            profiles: vec![],
            ..Default::default()
        };

        let state = RigState::new(cfg);
        assert!(
            state.user_disconnected,
            "dangling active_profile_id with method=None must set user_disconnected=true"
        );
        assert!(!state.status().connected, "must not be connected");
    }

    /// apply_config with method=None and a dangling active_profile_id must
    /// call disconnect() and set user_disconnected=true.
    #[test]
    fn dangling_active_profile_id_treated_as_disabled_in_apply_config() {
        // Start with a real profile active.
        let profile = crate::config::RigProfile {
            id: "real-profile-abc".to_string(),
            name: "Test Profile".to_string(),
            interface_type: crate::config::RigInterfaceType::External,
            host: "127.0.0.1".to_string(),
            port: 4532,
            ..Default::default()
        };
        let cfg_with_profile = RigConfig {
            preferred_method: crate::config::RigControlMethod::None,
            profiles: vec![profile],
            active_profile_id: Some("real-profile-abc".to_string()),
            ..Default::default()
        };
        let mut state = RigState::new(cfg_with_profile);
        // The profile exists, so it should not be user_disconnected.
        assert!(
            !state.user_disconnected,
            "should not be user_disconnected when a valid profile is referenced"
        );

        // Now apply a config where the profile is gone but the ID lingers.
        let cfg_dangling = RigConfig {
            preferred_method: crate::config::RigControlMethod::None,
            profiles: vec![], // profile removed
            active_profile_id: Some("real-profile-abc".to_string()), // stale
            ..Default::default()
        };
        state.apply_config(cfg_dangling);
        assert!(
            state.user_disconnected,
            "apply_config with dangling active_profile_id must set user_disconnected=true"
        );
        assert!(!state.status().connected, "must not be connected after apply_config with dangling id");
    }

    /// A config with a valid active profile and method=None is intentionally
    /// active — should NOT be user_disconnected.
    #[test]
    fn valid_active_profile_with_method_none_is_not_user_disconnected() {
        let profile = crate::config::RigProfile {
            id: "valid-profile-xyz".to_string(),
            name: "FLRig via wizard".to_string(),
            interface_type: crate::config::RigInterfaceType::Flrig,
            host: "127.0.0.1".to_string(),
            port: 12345,
            ..Default::default()
        };
        let cfg = RigConfig {
            preferred_method: crate::config::RigControlMethod::None,
            profiles: vec![profile],
            active_profile_id: Some("valid-profile-xyz".to_string()),
            ..Default::default()
        };
        let state = RigState::new(cfg);
        assert!(
            !state.user_disconnected,
            "a valid active profile should not be user_disconnected even with method=None"
        );
    }
}
