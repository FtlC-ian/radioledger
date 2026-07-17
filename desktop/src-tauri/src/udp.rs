//! WSJT-X / JS8Call UDP listener.
//!
//! ## Overview
//! Listens on loopback UDP ports for QSO-logged messages from WSJT-X (default
//! port 2237) and JS8Call (default port 2242).  Both applications use the same
//! WSJT-X binary wire protocol, so a single parser handles both.
//!
//! On receipt of a "QSO Logged" packet (message type 5), the QSO is:
//!   1. Validated (magic, type allowlist, field bounds, callsign plausibility).
//!   2. Assigned a client UUID for server-side deduplication.
//!   3. Stored in the local SQLite queue via `crate::db::Database::enqueue_qso`.
//!   4. Immediately forwarded to the RadioLedger API via HTTP POST (if the user
//!      is authenticated); on failure the queue handles retry with back-off.
//!
//! ## Security
//! - All sockets are bound to `127.0.0.1` (loopback) by default.
//! - Strict validation: magic number check, field length bounds, type allowlist.
//! - Rate limit: more than 10 QSO-logged messages per second are dropped.
//!
//! ## Wire format (WSJT-X binary / QDataStream)
//!
//! ```text
//! bytes 0..3   magic     u32 BE  = 0xADBCCBDA
//! bytes 4..7   schema    u32 BE  (1, 2, or 3)
//! bytes 8..11  msg_type  u32 BE
//! bytes 12..15 id_len    u32 BE
//! bytes 16..   id        UTF-8 (id_len bytes)
//!              payload   type-specific
//! ```
//!
//! QSO Logged (type 5) payload: datetime_off, dx_call, dx_grid, tx_freq(Hz u64),
//! mode, report_sent, report_rcvd, tx_power, comments, name, datetime_on,
//! operator_call, my_call, my_grid, exchange_sent, exchange_rcvd, adif_prop_mode.
//!
//! QDateTime = i64 Julian-day + u32 ms-since-midnight + u8 timespec.
//! utf8 = u32-BE length (0xFFFFFFFF = null) + bytes, no NUL.

use chrono::Utc;
use serde::{Deserialize, Serialize};
use socket2::{Domain, Protocol, Socket, Type};
use std::net::{Ipv4Addr, SocketAddrV4};
use std::sync::{
    atomic::{AtomicBool, AtomicU64, Ordering},
    Arc,
};
use tokio::net::UdpSocket;
use tokio::sync::Mutex;
use tracing::{error, info, warn};

use crate::db::Database;
use crate::error::AppError;
use crate::AppState;

mod listener_start;
mod wsjtx_integrations;
mod wsjtx_message_parser;
mod wsjtx_recent_decodes;
mod wsjtx_runtime;
mod wsjtx_wire;

pub(crate) use wsjtx_integrations::validate_and_enqueue;
use wsjtx_recent_decodes::snapshot_recent_decodes;
use wsjtx_runtime::run_listener;
use wsjtx_wire::{RecentDecodeRecord, WsjtxStatusSnapshot};

// ─── Protocol constants ───────────────────────────────────────────────────────

const WSJTX_MAGIC: u32 = 0xADBCCBDA;
const SUPPORTED_SCHEMAS: [u32; 3] = [1, 2, 3];
const MSG_HEARTBEAT: u32 = 0;
const MSG_STATUS: u32 = 1;
const MSG_DECODE: u32 = 2;
const MSG_CLEAR: u32 = 3;
const MSG_QSO_LOGGED: u32 = 5;
const MSG_ADIF_RECORD: u32 = 12;
const MAX_ID_LEN: usize = 256;
const MAX_STRING_LEN: usize = 65_536;
const RATE_LIMIT_PER_SEC: u64 = 10;

#[inline]
fn is_allowed_message_type(msg_type: u32) -> bool {
    matches!(
        msg_type,
        MSG_HEARTBEAT | MSG_STATUS | MSG_DECODE | MSG_CLEAR | MSG_QSO_LOGGED | MSG_ADIF_RECORD
    )
}

// ─── Per-source listener handle ───────────────────────────────────────────────

struct SourceHandle {
    listening: Arc<AtomicBool>,
    packets_received: Arc<AtomicU64>,
    port: u16,
    bind: String,
    multicast_group: Option<String>,
    shutdown_tx: Option<tokio::sync::oneshot::Sender<()>>,
}

impl SourceHandle {
    fn new(port: u16, bind: impl Into<String>) -> Self {
        SourceHandle {
            listening: Arc::new(AtomicBool::new(false)),
            packets_received: Arc::new(AtomicU64::new(0)),
            port,
            bind: bind.into(),
            multicast_group: None,
            shutdown_tx: None,
        }
    }

    fn is_listening(&self) -> bool {
        self.listening.load(Ordering::SeqCst)
    }

    fn record_started(
        &mut self,
        port: u16,
        bind: impl Into<String>,
        multicast_group: Option<String>,
    ) -> tokio::sync::oneshot::Receiver<()> {
        self.port = port;
        self.bind = bind.into();
        self.multicast_group = multicast_group;
        self.listening.store(true, Ordering::SeqCst);
        self.packets_received.store(0, Ordering::Relaxed);

        let (tx, rx) = tokio::sync::oneshot::channel();
        self.shutdown_tx = Some(tx);
        rx
    }
}

// ─── UdpState ─────────────────────────────────────────────────────────────────

/// Shared UDP subsystem state (held behind `Mutex<UdpState>` in `AppState`).
pub struct UdpState {
    db: Arc<Database>,
    recent_decodes: Arc<tokio::sync::Mutex<RecentDecodeStore>>,
    wsjtx: SourceHandle,
    js8call: SourceHandle,
    n1mm: SourceHandle,
}

impl UdpState {
    pub fn new(db: Arc<Database>) -> Self {
        UdpState {
            db,
            recent_decodes: Arc::new(tokio::sync::Mutex::new(RecentDecodeStore::default())),
            wsjtx: SourceHandle::new(2237, "127.0.0.1"),
            js8call: SourceHandle::new(2242, "127.0.0.1"),
            n1mm: SourceHandle::new(12060, "127.0.0.1"),
        }
    }

    /// Returns true if the WSJT-X UDP listener is currently active.
    pub fn is_wsjtx_listening(&self) -> bool {
        self.wsjtx.is_listening()
    }

    /// Returns true if the JS8Call UDP listener is currently active.
    pub fn is_js8call_listening(&self) -> bool {
        self.js8call.is_listening()
    }

    /// Returns true if the N1MM+ UDP listener is currently active.
    pub fn is_n1mm_listening(&self) -> bool {
        self.n1mm.is_listening()
    }
}

// ─── Public data types ────────────────────────────────────────────────────────

/// Fully parsed WSJT-X "QSO Logged" message (type 5).
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
pub struct QsoLogged {
    pub callsign: String,
    pub datetime_on: String,
    pub datetime_off: Option<String>,
    pub freq_mhz: f64,
    pub mode: String,
    pub band: String,
    pub rst_sent: String,
    pub rst_rcvd: String,
    pub tx_power: Option<f64>,
    pub comments: Option<String>,
    pub name: Option<String>,
    pub grid: Option<String>,
    pub operator: Option<String>,
    pub my_call: Option<String>,
    pub my_grid: Option<String>,
    pub exchange_sent: Option<String>,
    pub exchange_rcvd: Option<String>,
    pub adif_prop_mode: Option<String>,
    pub source: String,
}

/// Status of a single UDP source listener.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct SourceStatus {
    pub listening: bool,
    pub port: u16,
    pub bind: String,
    pub packets_received: u64,
    pub multicast_group: Option<String>,
}

/// Combined UDP listener status returned to the frontend.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct UdpStatus {
    pub wsjtx: SourceStatus,
    pub js8call: SourceStatus,
    pub n1mm: SourceStatus,
}

pub const EVENT_WSJTX_DECODE_LIST_CHANGED: &str = "wsjtx-decode-list-changed";
const RECENT_DECODE_TTL_SECS: i64 = 7 * 60;
const WSJTX_DECODE_PANEL_SETTING_KEY: &str = "wsjtx.decode_panel_enabled";
const WSJTX_REPLY_ID: &str = "RadioLedger";

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
pub struct RecentDecode {
    pub callsign: Option<String>,
    pub message: String,
    pub grid: Option<String>,
    pub distance_km: Option<u32>,
    pub snr: Option<i32>,
    pub frequency_hz: Option<u64>,
    pub freq_mhz: Option<f64>,
    pub mode: Option<String>,
    pub band: Option<String>,
    pub last_activity: String,
    pub source: String,
    pub log_status: crate::db::DecodeLogStatus,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct WsjtxDecodePanelSettings {
    pub enabled: bool,
}


#[derive(Debug, Default)]
pub(super) struct RecentDecodeStore {
    items: Vec<RecentDecodeRecord>,
    wsjtx_status: WsjtxStatusSnapshot,
}

impl RecentDecodeStore {
    fn prune(&mut self) {
        let cutoff = Utc::now() - chrono::Duration::seconds(RECENT_DECODE_TTL_SECS);
        self.items.retain(|item| item.last_activity_at >= cutoff);
        self.items
            .sort_by(|left, right| right.last_activity_at.cmp(&left.last_activity_at));
    }

    fn upsert(&mut self, decode: RecentDecodeRecord) {
        self.prune();
        if let Some(existing) = self
            .items
            .iter_mut()
            .find(|item| item.source == decode.source && item.message == decode.message)
        {
            *existing = decode;
        } else {
            self.items.push(decode);
        }
        self.items
            .sort_by(|left, right| right.last_activity_at.cmp(&left.last_activity_at));
    }
}

// ─── Tauri commands ───────────────────────────────────────────────────────────

/// Return the current status of all UDP listeners.
#[tauri::command]
pub async fn get_udp_status(state: tauri::State<'_, AppState>) -> Result<UdpStatus, AppError> {
    let udp = state.udp.lock().await;
    Ok(udp_status_from_state(&udp))
}

#[tauri::command]
pub async fn list_recent_wsjtx_decodes(
    state: tauri::State<'_, AppState>,
) -> Result<Vec<RecentDecode>, AppError> {
    let store = {
        let udp = state.udp.lock().await;
        Arc::clone(&udp.recent_decodes)
    };

    snapshot_recent_decodes(&store, &state.db)
        .await
        .map_err(|e| AppError::Database(e.to_string()))
}

#[tauri::command]
pub fn get_wsjtx_decode_panel_settings(
    state: tauri::State<'_, AppState>,
) -> Result<WsjtxDecodePanelSettings, AppError> {
    let enabled = state
        .db
        .get_setting(WSJTX_DECODE_PANEL_SETTING_KEY)
        .map_err(|e| AppError::Database(e.to_string()))?
        .map(|raw| matches!(raw.trim(), "1" | "true" | "yes" | "on"))
        .unwrap_or(true);

    Ok(WsjtxDecodePanelSettings { enabled })
}

#[tauri::command]
pub fn save_wsjtx_decode_panel_settings(
    state: tauri::State<'_, AppState>,
    enabled: bool,
) -> Result<WsjtxDecodePanelSettings, AppError> {
    state
        .db
        .set_setting(
            WSJTX_DECODE_PANEL_SETTING_KEY,
            if enabled { "true" } else { "false" },
        )
        .map_err(|e| AppError::Database(e.to_string()))?;

    Ok(WsjtxDecodePanelSettings { enabled })
}

/// Start the WSJT-X UDP listener.
///
/// - When `multicast_group` is None: binds to the configured `bind` address
///   (default `127.0.0.1`) — unicast/loopback only, unchanged behaviour.
/// - When `multicast_group` is Some: uses socket2 to bind to `0.0.0.0` (INADDR_ANY)
///   and joins the multicast group. Binding to INADDR_ANY is required by the
///   multicast API; the group membership limits which traffic is received.
///   SO_REUSEADDR (and SO_REUSEPORT on unix) are set so WSJT-X and RadioLedger
///   can share the port simultaneously.
///
/// `port` overrides the config port (default 2237).
#[tauri::command]
pub async fn start_udp_listener(
    port: Option<u16>,
    state: tauri::State<'_, AppState>,
    app: tauri::AppHandle,
) -> Result<UdpStatus, AppError> {
    let mut udp = state.udp.lock().await;
    let cfg = crate::config::load().unwrap_or_default();

    if udp.wsjtx.is_listening() {
        warn!(
            "WSJT-X UDP listener already running on port {}",
            udp.wsjtx.port
        );
        let status = udp_status_from_state(&udp);
        drop(udp);
        crate::tray::refresh_tray(&app).await;
        return Ok(status);
    }

    let prepared = listener_start::prepare_wsjtx_listener(&cfg.udp.wsjtx, port).await?;
    let db = Arc::clone(&udp.db);
    let recent_decodes = Arc::clone(&udp.recent_decodes);

    listener_start::start_packet_listener(
        &db,
        &recent_decodes,
        &mut udp.wsjtx,
        prepared,
        app.clone(),
        "wsjtx",
    );

    let status = udp_status_from_state(&udp);
    drop(udp);
    crate::tray::refresh_tray(&app).await;
    Ok(status)
}

/// Stop the WSJT-X UDP listener.
#[tauri::command]
pub async fn stop_udp_listener(
    state: tauri::State<'_, AppState>,
    app: tauri::AppHandle,
) -> Result<UdpStatus, AppError> {
    let mut udp = state.udp.lock().await;
    stop_source(&mut udp.wsjtx, "WSJT-X");
    let status = udp_status_from_state(&udp);
    drop(udp);
    crate::tray::refresh_tray(&app).await;
    Ok(status)
}

/// Start the JS8Call UDP listener on 127.0.0.1. `port` overrides config (default 2242).
#[tauri::command]
pub async fn start_js8call_listener(
    port: Option<u16>,
    state: tauri::State<'_, AppState>,
    app: tauri::AppHandle,
) -> Result<UdpStatus, AppError> {
    let mut udp = state.udp.lock().await;
    let cfg = crate::config::load().unwrap_or_default();

    if udp.js8call.is_listening() {
        warn!(
            "JS8Call UDP listener already running on port {}",
            udp.js8call.port
        );
        let status = udp_status_from_state(&udp);
        drop(udp);
        crate::tray::refresh_tray(&app).await;
        return Ok(status);
    }

    let prepared =
        listener_start::prepare_unicast_listener("JS8Call", &cfg.udp.js8call, port).await?;
    let db = Arc::clone(&udp.db);
    let recent_decodes = Arc::clone(&udp.recent_decodes);

    listener_start::start_packet_listener(
        &db,
        &recent_decodes,
        &mut udp.js8call,
        prepared,
        app.clone(),
        "js8call",
    );

    let status = udp_status_from_state(&udp);
    drop(udp);
    crate::tray::refresh_tray(&app).await;
    Ok(status)
}

/// Stop the JS8Call UDP listener.
#[tauri::command]
pub async fn stop_js8call_listener(
    state: tauri::State<'_, AppState>,
    app: tauri::AppHandle,
) -> Result<UdpStatus, AppError> {
    let mut udp = state.udp.lock().await;
    stop_source(&mut udp.js8call, "JS8Call");
    let status = udp_status_from_state(&udp);
    drop(udp);
    crate::tray::refresh_tray(&app).await;
    Ok(status)
}

/// Start the N1MM+ UDP listener. `port` overrides config (default 12060).
#[tauri::command]
pub async fn start_n1mm_listener(
    port: Option<u16>,
    state: tauri::State<'_, AppState>,
    app: tauri::AppHandle,
) -> Result<UdpStatus, AppError> {
    let mut udp = state.udp.lock().await;
    let cfg = crate::config::load().unwrap_or_default();

    if udp.n1mm.is_listening() {
        warn!(
            "N1MM+ UDP listener already running on port {}",
            udp.n1mm.port
        );
        let status = udp_status_from_state(&udp);
        drop(udp);
        crate::tray::refresh_tray(&app).await;
        return Ok(status);
    }

    let prepared = listener_start::prepare_unicast_listener("N1MM+", &cfg.udp.n1mm, port).await?;
    let db = Arc::clone(&udp.db);

    listener_start::start_n1mm_listener(&db, &mut udp.n1mm, prepared);

    let status = udp_status_from_state(&udp);
    drop(udp);
    crate::tray::refresh_tray(&app).await;
    Ok(status)
}

/// Stop the N1MM+ UDP listener.
#[tauri::command]
pub async fn stop_n1mm_listener(
    state: tauri::State<'_, AppState>,
    app: tauri::AppHandle,
) -> Result<UdpStatus, AppError> {
    let mut udp = state.udp.lock().await;
    stop_source(&mut udp.n1mm, "N1MM+");
    let status = udp_status_from_state(&udp);
    drop(udp);
    crate::tray::refresh_tray(&app).await;
    Ok(status)
}

// ─── Auto-start ───────────────────────────────────────────────────────────────

/// Called once at app startup. Starts any UDP listeners that have
/// `auto_start: true` in config.yaml.
pub async fn auto_start_listeners(udp_state: Arc<Mutex<UdpState>>, app: tauri::AppHandle) {
    let cfg = match crate::config::load() {
        Ok(c) => c,
        Err(e) => {
            tracing::error!("auto_start_listeners: failed to load config: {e}");
            return;
        }
    };

    if cfg.udp.wsjtx.auto_start {
        info!(
            "Auto-starting WSJT-X UDP listener on port {}",
            cfg.udp.wsjtx.port
        );
        let mut udp = udp_state.lock().await;
        if !udp.wsjtx.is_listening() {
            let wsjtx_bind = cfg
                .udp
                .wsjtx
                .multicast_group
                .as_deref()
                .filter(|group| !group.is_empty())
                .map(|group| format!("0.0.0.0:{} (multicast group {group})", cfg.udp.wsjtx.port))
                .unwrap_or_else(|| format!("{}:{}", cfg.udp.wsjtx.bind, cfg.udp.wsjtx.port));
            match listener_start::prepare_wsjtx_listener(&cfg.udp.wsjtx, None).await {
                Ok(prepared) => {
                    let db = Arc::clone(&udp.db);
                    let recent_decodes = Arc::clone(&udp.recent_decodes);
                    listener_start::start_packet_listener(
                        &db,
                        &recent_decodes,
                        &mut udp.wsjtx,
                        prepared,
                        app.clone(),
                        "wsjtx",
                    );
                    info!("WSJT-X UDP listener auto-started");
                }
                Err(e) => error!("Failed to auto-start WSJT-X listener on {wsjtx_bind}: {e}"),
            }
        }
        drop(udp);
        crate::tray::refresh_tray(&app).await;
    }

    if cfg.udp.js8call.auto_start {
        info!(
            "Auto-starting JS8Call UDP listener on port {}",
            cfg.udp.js8call.port
        );
        let mut udp = udp_state.lock().await;
        if !udp.js8call.is_listening() {
            match listener_start::prepare_unicast_listener("JS8Call", &cfg.udp.js8call, None).await
            {
                Ok(prepared) => {
                    let db = Arc::clone(&udp.db);
                    let recent_decodes = Arc::clone(&udp.recent_decodes);
                    listener_start::start_packet_listener(
                        &db,
                        &recent_decodes,
                        &mut udp.js8call,
                        prepared,
                        app.clone(),
                        "js8call",
                    );
                    info!("JS8Call UDP listener auto-started");
                }
                Err(e) => error!(
                    "Failed to auto-start JS8Call listener on {}:{}: {e}",
                    cfg.udp.js8call.bind, cfg.udp.js8call.port
                ),
            }
        }
        drop(udp);
        crate::tray::refresh_tray(&app).await;
    }

    if cfg.udp.n1mm.auto_start {
        info!(
            "Auto-starting N1MM+ UDP listener on port {}",
            cfg.udp.n1mm.port
        );
        let mut udp = udp_state.lock().await;
        if !udp.n1mm.is_listening() {
            match listener_start::prepare_unicast_listener("N1MM+", &cfg.udp.n1mm, None).await {
                Ok(prepared) => {
                    let db = Arc::clone(&udp.db);
                    listener_start::start_n1mm_listener(&db, &mut udp.n1mm, prepared);
                    info!("N1MM+ UDP listener auto-started");
                }
                Err(e) => error!(
                    "Failed to auto-start N1MM+ listener on {}:{}: {e}",
                    cfg.udp.n1mm.bind, cfg.udp.n1mm.port
                ),
            }
        }
        drop(udp);
        crate::tray::refresh_tray(&app).await;
    }
}

// ─── Internal helpers ─────────────────────────────────────────────────────────

fn udp_status_from_state(udp: &UdpState) -> UdpStatus {
    UdpStatus {
        wsjtx: SourceStatus {
            listening: udp.wsjtx.is_listening(),
            port: udp.wsjtx.port,
            bind: udp.wsjtx.bind.clone(),
            packets_received: udp.wsjtx.packets_received.load(Ordering::Relaxed),
            multicast_group: udp.wsjtx.multicast_group.clone(),
        },
        js8call: SourceStatus {
            listening: udp.js8call.is_listening(),
            port: udp.js8call.port,
            bind: udp.js8call.bind.clone(),
            packets_received: udp.js8call.packets_received.load(Ordering::Relaxed),
            multicast_group: udp.js8call.multicast_group.clone(),
        },
        n1mm: SourceStatus {
            listening: udp.n1mm.is_listening(),
            port: udp.n1mm.port,
            bind: udp.n1mm.bind.clone(),
            packets_received: udp.n1mm.packets_received.load(Ordering::Relaxed),
            multicast_group: udp.n1mm.multicast_group.clone(),
        },
    }
}

fn stop_source(handle: &mut SourceHandle, label: &str) {
    if let Some(tx) = handle.shutdown_tx.take() {
        let _ = tx.send(());
    }
    handle.listening.store(false, Ordering::SeqCst);
    info!("{label} UDP listener stopped");
}

// ─── UDP config / settings commands ──────────────────────────────────────────

/// Current UDP configuration returned to the frontend.
///
/// This is the Tauri-command counterpart to [`UdpSettingsRequest`].  The
/// frontend calls `invoke('get_udp_config')` on the Settings tab to populate
/// the UDP port fields and auto-start checkboxes with their persisted values.
#[derive(Debug, Serialize)]
pub struct UdpConfig {
    pub wsjtx_port: u16,
    pub wsjtx_auto_start: bool,
    pub wsjtx_multicast_group: Option<String>,
    pub js8call_port: u16,
    pub js8call_auto_start: bool,
    pub n1mm_port: u16,
    pub n1mm_auto_start: bool,
    pub ft8battle_relay_enabled: bool,
}

/// Returns the current UDP settings (ports + auto-start flags) from config.yaml.
///
/// Called by the frontend Settings tab via `invoke('get_udp_config')` to
/// populate the port/auto-start fields with their persisted values.
#[tauri::command]
pub fn get_udp_config() -> Result<UdpConfig, AppError> {
    let cfg = crate::config::load()
        .map_err(|e| AppError::Config(format!("Failed to load config: {e}")))?;

    Ok(UdpConfig {
        wsjtx_port: cfg.udp.wsjtx.port,
        wsjtx_auto_start: cfg.udp.wsjtx.auto_start,
        wsjtx_multicast_group: cfg.udp.wsjtx.multicast_group,
        js8call_port: cfg.udp.js8call.port,
        js8call_auto_start: cfg.udp.js8call.auto_start,
        n1mm_port: cfg.udp.n1mm.port,
        n1mm_auto_start: cfg.udp.n1mm.auto_start,
        ft8battle_relay_enabled: cfg.udp.ft8battle.enabled,
    })
}

/// Request payload for `save_udp_settings`.
#[derive(Debug, Deserialize)]
pub struct UdpSettingsRequest {
    pub wsjtx_port: u16,
    pub js8call_port: u16,
    pub n1mm_port: u16,
    /// Multicast group for WSJT-X (e.g. "224.0.0.73"), or None/empty to disable.
    pub wsjtx_multicast_group: Option<String>,
    /// Whether to auto-start the WSJT-X listener on app launch.
    #[serde(default)]
    pub wsjtx_auto_start: bool,
    /// Whether to auto-start the JS8Call listener on app launch.
    #[serde(default)]
    pub js8call_auto_start: bool,
    /// Whether to auto-start the N1MM+ listener on app launch.
    #[serde(default)]
    pub n1mm_auto_start: bool,
    /// Whether WSJT-X logged QSOs should be best-effort relayed to FT8Battle.
    #[serde(default)]
    pub ft8battle_relay_enabled: bool,
}

/// Persist UDP settings (ports + WSJT-X multicast group + auto-start flags) to config.yaml.
/// Changes take effect the next time the respective listener is (re-)started.
#[tauri::command]
pub async fn save_udp_settings(request: UdpSettingsRequest) -> Result<(), AppError> {
    let mut cfg = crate::config::load().unwrap_or_default();

    cfg.udp.wsjtx.port = request.wsjtx_port;
    cfg.udp.js8call.port = request.js8call_port;
    cfg.udp.n1mm.port = request.n1mm_port;

    // Empty string is treated as "disabled"; store as None.
    cfg.udp.wsjtx.multicast_group = request
        .wsjtx_multicast_group
        .filter(|s| !s.trim().is_empty())
        .map(|s| s.trim().to_string());

    cfg.udp.wsjtx.auto_start = request.wsjtx_auto_start;
    cfg.udp.js8call.auto_start = request.js8call_auto_start;
    cfg.udp.n1mm.auto_start = request.n1mm_auto_start;
    cfg.udp.ft8battle.enabled = request.ft8battle_relay_enabled;

    cfg.save()
        .map_err(|e| AppError::Config(format!("Failed to save UDP settings: {e}")))?;
    info!(
        "UDP settings saved — WSJT-X: {} (auto_start: {}), JS8Call: {} (auto_start: {}), \
         N1MM+: {} (auto_start: {}), multicast: {:?}, FT8Battle relay: {}",
        cfg.udp.wsjtx.port,
        cfg.udp.wsjtx.auto_start,
        cfg.udp.js8call.port,
        cfg.udp.js8call.auto_start,
        cfg.udp.n1mm.port,
        cfg.udp.n1mm.auto_start,
        cfg.udp.wsjtx.multicast_group,
        cfg.udp.ft8battle.enabled
    );
    Ok(())
}

// ─── Multicast socket setup ───────────────────────────────────────────────────

/// Create a UDP socket suitable for multicast reception.
///
/// Binds to `0.0.0.0:<port>` (INADDR_ANY — required for multicast), sets
/// `SO_REUSEADDR` (and `SO_REUSEPORT` on unix) so WSJT-X and RadioLedger can
/// coexist on the same port, then joins the requested IPv4 multicast group.
///
/// # Security note
/// Binding to `0.0.0.0` accepts unicast datagrams from all interfaces, not just
/// loopback. The multicast group membership provides filtering for multicast
/// traffic. All received packets are still subject to the same strict validation
/// as loopback packets (magic number, allowlist, rate limit).
fn bind_multicast_socket(port: u16, group: &str) -> anyhow::Result<UdpSocket> {
    let multicast_addr: Ipv4Addr = group
        .parse()
        .map_err(|_| anyhow::anyhow!("Invalid multicast group address: {group}"))?;

    if !multicast_addr.is_multicast() {
        anyhow::bail!("Address {group} is not a multicast address (must be in 224.0.0.0/4)");
    }

    let sock = Socket::new(Domain::IPV4, Type::DGRAM, Some(Protocol::UDP))?;

    // Allow WSJT-X and RadioLedger to share the same UDP port.
    sock.set_reuse_address(true)?;
    #[cfg(unix)]
    sock.set_reuse_port(true)?;

    // Bind to INADDR_ANY so the kernel delivers multicast datagrams to us.
    let bind_addr = SocketAddrV4::new(Ipv4Addr::UNSPECIFIED, port);
    sock.bind(&bind_addr.into())?;

    // Join the multicast group on any interface (interface = INADDR_ANY).
    sock.join_multicast_v4(&multicast_addr, &Ipv4Addr::UNSPECIFIED)?;

    // Hand off to tokio.
    sock.set_nonblocking(true)?;
    let std_socket: std::net::UdpSocket = sock.into();
    Ok(UdpSocket::from_std(std_socket)?)
}

#[allow(clippy::too_many_arguments)]
fn spawn_listener(
    socket: UdpSocket,
    db: Arc<Database>,
    recent_decodes: Arc<tokio::sync::Mutex<RecentDecodeStore>>,
    listening_flag: Arc<AtomicBool>,
    counter: Arc<AtomicU64>,
    shutdown_rx: tokio::sync::oneshot::Receiver<()>,
    app: tauri::AppHandle,
    source: &'static str,
) {
    tauri::async_runtime::spawn(async move {
        run_listener(
            socket,
            db,
            recent_decodes,
            listening_flag,
            counter,
            shutdown_rx,
            app,
            source,
        )
        .await;
    });
}

fn spawn_n1mm_listener(
    socket: UdpSocket,
    db: Arc<Database>,
    listening_flag: Arc<AtomicBool>,
    counter: Arc<AtomicU64>,
    shutdown_rx: tokio::sync::oneshot::Receiver<()>,
) {
    tauri::async_runtime::spawn(async move {
        crate::n1mm::run_n1mm_listener(socket, db, listening_flag, counter, shutdown_rx).await;
    });
}


/// Map an operating frequency in MHz to an amateur band label.
pub fn freq_to_band(mhz: f64) -> String {
    match mhz as u64 {
        1..=2 => "160m",
        3..=4 => "80m",
        5 => "60m",
        7..=8 => "40m",
        10..=11 => "30m",
        14..=15 => "20m",
        18..=19 => "17m",
        21..=22 => "15m",
        24..=25 => "12m",
        28..=30 => "10m",
        50..=54 => "6m",
        144..=148 => "2m",
        420..=450 => "70cm",
        _ => "unknown",
    }
    .into()
}

#[inline]
fn non_empty(s: String) -> Option<String> {
    if s.is_empty() {
        None
    } else {
        Some(s)
    }
}

pub(crate) fn is_plausible_callsign(callsign: &str) -> bool {
    let len_ok = !callsign.is_empty() && callsign.len() <= 20;
    let chars_ok = callsign
        .chars()
        .all(|c| c.is_ascii_alphanumeric() || c == '/' || c == '-');
    let has_letter = callsign.chars().any(|c| c.is_ascii_alphabetic());
    let has_digit = callsign.chars().any(|c| c.is_ascii_digit());

    len_ok && chars_ok && has_letter && has_digit
}

pub(crate) fn parse_iso_utc(ts: &str) -> anyhow::Result<chrono::DateTime<chrono::Utc>> {
    Ok(chrono::DateTime::parse_from_rfc3339(ts)
        .map_err(|e| anyhow::anyhow!("invalid timestamp '{ts}': {e}"))?
        .with_timezone(&chrono::Utc))
}

pub(crate) fn is_within_24h(
    timestamp: chrono::DateTime<chrono::Utc>,
    now: chrono::DateTime<chrono::Utc>,
) -> bool {
    let drift = now.signed_duration_since(timestamp).num_seconds().abs();
    drift <= 86_400
}

// ─── Tests ────────────────────────────────────────────────────────────────────

#[cfg(test)]
mod tests {
    use super::wsjtx_integrations::{
        relay_udp_packet, try_immediate_post_with_client, try_relay_to_ft8battle,
    };
    use super::*;
    use std::collections::VecDeque;
    use std::net::SocketAddr;
    use std::time::Duration;

    #[test]
    fn source_handle_record_started_resets_runtime_state() {
        let mut handle = SourceHandle::new(2237, "127.0.0.1");
        handle.packets_received.store(7, Ordering::Relaxed);

        let _shutdown_rx = handle.record_started(2242, "0.0.0.0", Some("224.0.0.73".into()));

        assert!(handle.is_listening());
        assert_eq!(handle.port, 2242);
        assert_eq!(handle.bind, "0.0.0.0");
        assert_eq!(handle.multicast_group.as_deref(), Some("224.0.0.73"));
        assert_eq!(handle.packets_received.load(Ordering::Relaxed), 0);
        assert!(handle.shutdown_tx.is_some());
    }

    use tokio::io::{AsyncReadExt, AsyncWriteExt};
    use tokio::net::TcpListener;
    use tokio::sync::{oneshot, Mutex as TokioMutex};

    #[derive(Clone)]
    struct MockHttpResponse {
        status_line: &'static str,
        body: String,
        content_type: &'static str,
    }

    impl MockHttpResponse {
        fn json(status_line: &'static str, body: serde_json::Value) -> Self {
            Self {
                status_line,
                body: body.to_string(),
                content_type: "application/json",
            }
        }

        fn text(status_line: &'static str, body: impl Into<String>) -> Self {
            Self {
                status_line,
                body: body.into(),
                content_type: "text/plain",
            }
        }
    }

    async fn spawn_mock_http_server(
        responses: Vec<MockHttpResponse>,
        request_count: usize,
    ) -> anyhow::Result<(
        SocketAddr,
        oneshot::Receiver<Vec<String>>,
        tokio::task::JoinHandle<()>,
    )> {
        let listener = TcpListener::bind("127.0.0.1:0").await?;
        let addr = listener.local_addr()?;
        let responses = Arc::new(TokioMutex::new(VecDeque::from(responses)));
        let (requests_tx, requests_rx) = oneshot::channel();

        let handle = tokio::spawn(async move {
            let mut requests = Vec::with_capacity(request_count);

            for _ in 0..request_count {
                let Ok((mut socket, _peer)) = listener.accept().await else {
                    break;
                };

                let mut buf = Vec::new();
                let mut chunk = [0u8; 4096];
                let header_end = loop {
                    let Ok(n) = socket.read(&mut chunk).await else {
                        return;
                    };
                    if n == 0 {
                        return;
                    }
                    buf.extend_from_slice(&chunk[..n]);
                    if let Some(pos) = buf.windows(4).position(|window| window == b"\r\n\r\n") {
                        break pos + 4;
                    }
                };

                let headers = String::from_utf8_lossy(&buf[..header_end]);
                let content_length = headers
                    .lines()
                    .find_map(|line| {
                        let (name, value) = line.split_once(':')?;
                        if name.eq_ignore_ascii_case("content-length") {
                            value.trim().parse::<usize>().ok()
                        } else {
                            None
                        }
                    })
                    .unwrap_or(0);

                while buf.len() < header_end + content_length {
                    let Ok(n) = socket.read(&mut chunk).await else {
                        return;
                    };
                    if n == 0 {
                        return;
                    }
                    buf.extend_from_slice(&chunk[..n]);
                }

                requests.push(String::from_utf8_lossy(&buf).to_string());

                let response = responses.lock().await.pop_front().unwrap_or_else(|| {
                    MockHttpResponse::text(
                        "HTTP/1.1 500 Internal Server Error",
                        "missing mock response",
                    )
                });

                let wire = format!(
                    "{}\r\nContent-Type: {}\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{}",
                    response.status_line,
                    response.content_type,
                    response.body.len(),
                    response.body,
                );

                let _ = socket.write_all(wire.as_bytes()).await;
            }

            let _ = requests_tx.send(requests);
        });

        Ok((addr, requests_rx, handle))
    }

    fn request_json_body(request: &str) -> serde_json::Value {
        let body = request
            .split_once("\r\n\r\n")
            .map(|(_, body)| body)
            .expect("request should have body");
        serde_json::from_str(body).expect("request body should be valid json")
    }

    fn with_temp_home<F>(test_fn: F)
    where
        F: FnOnce(),
    {
        crate::test_support::with_temp_home("radioledger-udp-test", test_fn);
    }

    #[test]
    fn udp_settings_round_trip_through_persisted_config() {
        with_temp_home(|| {
            let rt = tokio::runtime::Runtime::new().expect("runtime should build");
            rt.block_on(async {
                save_udp_settings(UdpSettingsRequest {
                    wsjtx_port: 2239,
                    js8call_port: 2244,
                    n1mm_port: 12062,
                    wsjtx_multicast_group: Some("224.0.0.73".to_string()),
                    wsjtx_auto_start: true,
                    js8call_auto_start: true,
                    n1mm_auto_start: false,
                    ft8battle_relay_enabled: false,
                })
                .await
                .expect("udp settings should save");
            });

            let cfg = crate::config::load().expect("config should reload");
            assert_eq!(cfg.udp.wsjtx.port, 2239);
            assert_eq!(cfg.udp.js8call.port, 2244);
            assert_eq!(cfg.udp.n1mm.port, 12062);
            assert_eq!(cfg.udp.wsjtx.multicast_group.as_deref(), Some("224.0.0.73"));
            assert!(cfg.udp.wsjtx.auto_start);
            assert!(cfg.udp.js8call.auto_start);
            assert!(!cfg.udp.n1mm.auto_start);

            let persisted = get_udp_config().expect("udp config should load");
            assert_eq!(persisted.wsjtx_port, 2239);
            assert_eq!(persisted.js8call_port, 2244);
            assert_eq!(persisted.n1mm_port, 12062);
            assert_eq!(
                persisted.wsjtx_multicast_group.as_deref(),
                Some("224.0.0.73")
            );
            assert!(persisted.wsjtx_auto_start);
            assert!(persisted.js8call_auto_start);
            assert!(!persisted.n1mm_auto_start);
        });
    }

    // parser and wire tests live next to their extracted modules

    // ── callsign + timestamp sanity helpers ───────────────────────────────────

    #[test]
    fn test_is_plausible_callsign() {
        for call in &["W1AW", "VK2ABC/P", "DL1ABC-1", "ZL3/K1XYZ"] {
            assert!(is_plausible_callsign(call), "callsign {call} should pass");
        }

        let too_long = "A".repeat(21);
        for bad in ["", "NOCALL", "12345", "W1AW!", too_long.as_str()] {
            assert!(!is_plausible_callsign(bad), "callsign {bad} should fail");
        }
    }

    #[test]
    fn test_parse_iso_utc() {
        let parsed = parse_iso_utc("2024-07-04T14:30:00Z").unwrap();
        assert_eq!(parsed.to_rfc3339(), "2024-07-04T14:30:00+00:00");
        assert!(parse_iso_utc("not-a-date").is_err());
    }

    #[test]
    fn test_is_within_24h() {
        let now = chrono::DateTime::parse_from_rfc3339("2024-07-04T14:30:00Z")
            .unwrap()
            .with_timezone(&chrono::Utc);
        let inside = chrono::DateTime::parse_from_rfc3339("2024-07-03T14:45:00Z")
            .unwrap()
            .with_timezone(&chrono::Utc);
        let outside = chrono::DateTime::parse_from_rfc3339("2024-07-02T14:29:59Z")
            .unwrap()
            .with_timezone(&chrono::Utc);

        assert!(is_within_24h(inside, now));
        assert!(!is_within_24h(outside, now));
    }

    #[test]
    fn test_allowed_message_type_allowlist() {
        assert!(is_allowed_message_type(MSG_HEARTBEAT));
        assert!(is_allowed_message_type(MSG_STATUS));
        assert!(is_allowed_message_type(MSG_DECODE));
        assert!(is_allowed_message_type(MSG_CLEAR));
        assert!(is_allowed_message_type(MSG_QSO_LOGGED));
        assert!(is_allowed_message_type(MSG_ADIF_RECORD));
        assert!(!is_allowed_message_type(99));
    }

    #[tokio::test]
    async fn test_relay_udp_packet_sends_to_configured_endpoint() {
        let receiver = UdpSocket::bind("127.0.0.1:0").await.unwrap();
        let endpoint = receiver.local_addr().unwrap();
        let packet = b"relay-test-packet";

        relay_udp_packet(packet, &endpoint.to_string())
            .await
            .unwrap();

        let mut buf = [0u8; 64];
        let (len, _) = tokio::time::timeout(Duration::from_secs(1), receiver.recv_from(&mut buf))
            .await
            .expect("receive should complete")
            .expect("udp receive should succeed");
        assert_eq!(&buf[..len], packet);
    }

    #[tokio::test]
    async fn try_immediate_post_hydrates_wsjtx_payload_before_posting() {
        let qso = QsoLogged {
            callsign: "K1ABC".into(),
            datetime_on: "2026-04-10T20:00:00Z".into(),
            datetime_off: Some("2026-04-10T20:01:00Z".into()),
            freq_mhz: 14.074,
            mode: "FT8".into(),
            band: "20m".into(),
            rst_sent: "-10".into(),
            rst_rcvd: "-08".into(),
            tx_power: None,
            comments: None,
            name: None,
            grid: None,
            operator: None,
            my_call: None,
            my_grid: None,
            exchange_sent: None,
            exchange_rcvd: None,
            adif_prop_mode: None,
            source: "wsjtx".into(),
        };

        let (addr, requests_rx, handle) = spawn_mock_http_server(
            vec![
                MockHttpResponse::json(
                    "HTTP/1.1 200 OK",
                    serde_json::json!({
                        "success": true,
                        "data": {
                            "callsign": "K1ABC",
                            "full_name": "Jane Example",
                            "grid": "fn31",
                            "dxcc": 291,
                            "country": "United States",
                            "state_province": "Connecticut",
                            "cq_zone": 5,
                            "itu_zone": 8,
                            "source": "fcc_uls"
                        }
                    }),
                ),
                MockHttpResponse::json("HTTP/1.1 201 Created", serde_json::json!({ "ok": true })),
            ],
            2,
        )
        .await
        .unwrap();

        let client = reqwest::Client::new();
        let json_data = serde_json::to_string(&qso).unwrap();
        try_immediate_post_with_client(
            &client,
            &format!("http://{addr}"),
            "log-1",
            "token-1",
            "client-uuid-1",
            &json_data,
        )
        .await
        .unwrap();

        let requests = requests_rx.await.expect("requests should be captured");
        handle.abort();
        assert_eq!(requests.len(), 2);
        assert!(requests[0].starts_with("GET /v1/callsign/K1ABC "));
        assert!(requests[1].starts_with("POST /v1/logbooks/log-1/qsos "));

        let payload = request_json_body(&requests[1]);
        assert_eq!(payload["client_uuid"].as_str(), Some("client-uuid-1"));
        assert_eq!(payload["name"].as_str(), Some("Jane Example"));
        assert_eq!(payload["gridsquare"].as_str(), Some("FN31"));
        assert_eq!(payload["qth"].as_str(), Some("Connecticut, United States"));
        assert_eq!(payload["dxcc"].as_i64(), Some(291));
        assert_eq!(payload["country"].as_str(), Some("United States"));
        assert_eq!(payload["cq_zone"].as_i64(), Some(5));
        assert_eq!(payload["itu_zone"].as_i64(), Some(8));
    }

    #[tokio::test]
    async fn try_immediate_post_continues_when_wsjtx_lookup_fails() {
        let qso = QsoLogged {
            callsign: "K1ABC".into(),
            datetime_on: "2026-04-10T20:00:00Z".into(),
            datetime_off: Some("2026-04-10T20:01:00Z".into()),
            freq_mhz: 14.074,
            mode: "FT8".into(),
            band: "20m".into(),
            rst_sent: "-10".into(),
            rst_rcvd: "-08".into(),
            tx_power: None,
            comments: None,
            name: None,
            grid: None,
            operator: None,
            my_call: None,
            my_grid: None,
            exchange_sent: None,
            exchange_rcvd: None,
            adif_prop_mode: None,
            source: "wsjtx".into(),
        };

        let (addr, requests_rx, handle) = spawn_mock_http_server(
            vec![
                MockHttpResponse::text("HTTP/1.1 500 Internal Server Error", "lookup failed"),
                MockHttpResponse::json("HTTP/1.1 201 Created", serde_json::json!({ "ok": true })),
            ],
            2,
        )
        .await
        .unwrap();

        let client = reqwest::Client::new();
        let json_data = serde_json::to_string(&qso).unwrap();
        try_immediate_post_with_client(
            &client,
            &format!("http://{addr}"),
            "log-1",
            "token-1",
            "client-uuid-2",
            &json_data,
        )
        .await
        .unwrap();

        let requests = requests_rx.await.expect("requests should be captured");
        handle.abort();
        assert_eq!(requests.len(), 2);
        assert!(requests[0].starts_with("GET /v1/callsign/K1ABC "));
        assert!(requests[1].starts_with("POST /v1/logbooks/log-1/qsos "));

        let payload = request_json_body(&requests[1]);
        assert_eq!(payload["client_uuid"].as_str(), Some("client-uuid-2"));
        assert!(payload["name"].is_null());
        assert!(payload["gridsquare"].is_null());
        assert!(payload["qth"].is_null());
    }

    #[test]
    fn test_save_udp_settings_persists_ft8battle_toggle() {
        with_temp_home(|| {
            let rt = tokio::runtime::Runtime::new().expect("runtime");
            rt.block_on(async {
                save_udp_settings(UdpSettingsRequest {
                    wsjtx_port: 2237,
                    js8call_port: 2242,
                    n1mm_port: 12060,
                    wsjtx_multicast_group: None,
                    wsjtx_auto_start: true,
                    js8call_auto_start: false,
                    n1mm_auto_start: false,
                    ft8battle_relay_enabled: true,
                })
                .await
                .unwrap();
            });

            let cfg = crate::config::load().unwrap();
            assert!(cfg.udp.ft8battle.enabled);
            assert_eq!(
                cfg.udp.ft8battle.endpoint,
                crate::config::DEFAULT_FT8BATTLE_ENDPOINT
            );

            let udp_cfg = get_udp_config().unwrap();
            assert!(udp_cfg.ft8battle_relay_enabled);
        });
    }

    #[test]
    fn test_try_relay_to_ft8battle_respects_toggle_and_default_endpoint() {
        with_temp_home(|| {
            let rt = tokio::runtime::Runtime::new().expect("runtime");
            rt.block_on(async {
                let receiver = UdpSocket::bind("127.0.0.1:0").await.unwrap();
                let endpoint = receiver.local_addr().unwrap().to_string();
                let packet = b"ft8battle-enabled";

                let mut cfg = crate::config::load().unwrap();
                assert_eq!(
                    cfg.udp.ft8battle.endpoint,
                    crate::config::DEFAULT_FT8BATTLE_ENDPOINT
                );
                cfg.udp.ft8battle.enabled = false;
                cfg.udp.ft8battle.endpoint = endpoint.clone();
                cfg.save().unwrap();

                assert_eq!(try_relay_to_ft8battle(packet).await.unwrap(), None);

                cfg.udp.ft8battle.enabled = true;
                cfg.save().unwrap();

                let relayed_to = try_relay_to_ft8battle(packet)
                    .await
                    .unwrap()
                    .expect("relay should be enabled");
                assert_eq!(relayed_to, endpoint);

                let mut buf = [0u8; 64];
                let (len, _) =
                    tokio::time::timeout(Duration::from_secs(1), receiver.recv_from(&mut buf))
                        .await
                        .expect("receive should complete")
                        .expect("udp receive should succeed");
                assert_eq!(&buf[..len], packet);
            });
        });
    }
}
