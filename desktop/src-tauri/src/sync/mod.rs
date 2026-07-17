//! Background server sync task.
//!
//! Periodically sends queued QSOs to the RadioLedger API server.
//! Retries with exponential backoff on failure.
//! Emits Tauri events to the frontend on status changes.
//!
//! # Module layout
//!
//! - `mod.rs` — sync loop, Tauri commands, state
//! - `callsign` — callsign lookup, WSJT-X payload hydration
//! - `qso_update` — QSO update preparation (normalization, enrichment clearing)
//! - `payload` — sync payload building (field copying, desktop-meta flattening)

use anyhow::Context;
use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use std::sync::Arc;
use std::time::Duration;
use tokio::sync::Mutex;
use tracing::{debug, error, info, warn};
use uuid::Uuid;

use crate::auth;
use crate::db::{Database, QueuedQso};
use crate::error::AppError;
use crate::AppState;

pub mod callsign;
pub mod payload;
pub mod qso_update;

// Re-export callsign items that are part of the public sync API surface.
// This preserves backward compatibility for external consumers that import
// via `crate::sync::` paths (CallsignLookupResult, hydrate_wsjtx_payload_if_needed,
// or lookup_callsign).  Tauri invoke_handler still references
// sync::callsign::lookup_callsign explicitly since `#[tauri::command]`
// must be registered at its definition path.
pub use callsign::{CallsignLookupResult, hydrate_wsjtx_payload_if_needed, lookup_callsign};

// Re-export UpdateQsoRequest so that `crate::sync::UpdateQsoRequest` still
// resolves — the Tauri command and other consumers reference it via that path.
pub use qso_update::UpdateQsoRequest;

// ─── Event names emitted to the frontend ─────────────────────────────────────

pub const EVENT_SYNC_STATUS: &str = "sync-status-changed";

// ─── Sync-specific error types ────────────────────────────────────────────────

/// Errors that can occur when syncing a single QSO.
///
/// Using a typed enum instead of magic strings allows the caller to pattern-match
/// on the specific failure mode without fragile string-prefix inspection.
#[derive(Debug, thiserror::Error)]
pub enum SyncQsoError {
    /// The server returned 401 and the token was successfully refreshed.
    /// The QSO should be retried on the next sync cycle; its failure counter
    /// must NOT be incremented because this is an infrastructure event, not a
    /// data error.
    #[error("Access token refreshed; QSO will be retried on next cycle")]
    TokenRefreshed,

    /// The server returned 401 but the token refresh itself failed.
    /// This is a real error — count it against the QSO.
    #[error("Access token expired and refresh failed: {0}")]
    TokenRefreshFailed(String),

    /// Any other sync error (bad status, network failure, etc.).
    #[error("{0}")]
    Other(anyhow::Error),
}

// ─── Data structures ─────────────────────────────────────────────────────────

/// Runtime state for the sync subsystem.
pub struct SyncState {
    pub(crate) db: Arc<Database>,
    pub last_sync_at: Option<DateTime<Utc>>,
    pub last_error: Option<String>,
}

/// Sync status payload returned to the frontend and emitted as a Tauri event.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct SyncStatus {
    /// Number of QSOs waiting to be sent to the server.
    pub pending: i64,
    /// Number of QSOs cached locally from the server.
    pub cached_qso_count: i64,
    /// UTC timestamp of the last successful sync (ISO 8601), or null.
    pub last_sync: Option<String>,
    /// Human-readable description of the last error, or null.
    pub last_error: Option<String>,
}

impl SyncState {
    pub fn new(db: Arc<Database>) -> Self {
        SyncState {
            db,
            last_sync_at: None,
            last_error: None,
        }
    }
}

// ─── Tauri commands ───────────────────────────────────────────────────────────

/// Returns the current sync status (pending count + last sync timestamp).
#[tauri::command]
pub async fn get_sync_status(state: tauri::State<'_, AppState>) -> Result<SyncStatus, AppError> {
    let sync = state.sync.lock().await;
    let pending = sync
        .db
        .pending_count()
        .map_err(|e| AppError::Sync(e.to_string()))?;
    let cached_qso_count = sync
        .db
        .count_qsos()
        .map_err(|e| AppError::Sync(e.to_string()))?;

    Ok(SyncStatus {
        pending,
        cached_qso_count,
        last_sync: sync.last_sync_at.map(|t| t.to_rfc3339()),
        last_error: sync.last_error.clone(),
    })
}

/// Trigger an immediate sync cycle (e.g., from the tray menu "Sync Now").
#[tauri::command]
pub async fn sync_now(
    state: tauri::State<'_, AppState>,
    app: tauri::AppHandle,
) -> Result<SyncStatus, AppError> {
    info!("Manual sync triggered");
    let sync = Arc::clone(&state.sync);
    run_sync_cycle(sync, &app).await;

    get_sync_status(state).await
}

/// List cached QSOs for the Log view.
#[tauri::command]
pub async fn list_qsos(
    state: tauri::State<'_, AppState>,
    limit: usize,
    offset: usize,
    callsign: Option<String>,
    band: Option<String>,
    mode: Option<String>,
) -> Result<Vec<crate::db::CachedQso>, AppError> {
    let sync = state.sync.lock().await;
    sync.db
        .list_qsos(
            limit,
            offset,
            callsign.as_deref(),
            band.as_deref(),
            mode.as_deref(),
        )
        .map_err(|e| AppError::Database(e.to_string()))
}

/// Count cached QSOs, optionally filtered by callsign/band/mode.
#[tauri::command]
pub async fn count_qsos(
    state: tauri::State<'_, AppState>,
    callsign: Option<String>,
    band: Option<String>,
    mode: Option<String>,
) -> Result<i64, AppError> {
    let sync = state.sync.lock().await;
    sync.db
        .count_qsos_filtered(callsign.as_deref(), band.as_deref(), mode.as_deref())
        .map_err(|e| AppError::Database(e.to_string()))
}

// ─── Manual QSO entry ────────────────────────────────────────────────────────

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct DesktopQsoMeta {
    pub frequency_mhz: Option<f64>,
    pub power_w: Option<f64>,
    pub name: Option<String>,
    pub qth: Option<String>,
}

/// Request body for creating a QSO manually from the desktop entry form.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct CreateQsoRequest {
    pub callsign: String,
    pub band: String,
    pub mode: String,
    /// ISO 8601 datetime string (e.g. "2024-01-15T14:30:00Z").
    pub datetime_on: String,
    pub rst_sent: Option<String>,
    pub rst_rcvd: Option<String>,
    pub notes: Option<String>,
    pub desktop_meta: Option<DesktopQsoMeta>,
}





#[tauri::command]
pub async fn create_qso(
    state: tauri::State<'_, AppState>,
    app: tauri::AppHandle,
    request: CreateQsoRequest,
) -> Result<serde_json::Value, AppError> {
    let callsign = request.callsign.trim().to_uppercase();
    let band = request.band.trim().to_string();
    let mode = request.mode.trim().to_uppercase();

    if callsign.is_empty() || band.is_empty() || mode.is_empty() {
        return Err(AppError::Other(
            "Callsign, band, and mode are required".into(),
        ));
    }

    let parsed_datetime = DateTime::parse_from_rfc3339(&request.datetime_on)
        .map_err(|e| AppError::Other(format!("Invalid QSO date/time: {e}")))?
        .with_timezone(&Utc);

    let mut body = serde_json::json!({
        "callsign": callsign,
        "band": band,
        "mode": mode,
        "datetime_on": parsed_datetime.to_rfc3339(),
    });

    if let Some(rst) = request
        .rst_sent
        .as_deref()
        .map(str::trim)
        .filter(|s| !s.is_empty())
    {
        body["rst_sent"] = serde_json::Value::String(rst.to_string());
    }
    if let Some(rst) = request
        .rst_rcvd
        .as_deref()
        .map(str::trim)
        .filter(|s| !s.is_empty())
    {
        body["rst_rcvd"] = serde_json::Value::String(rst.to_string());
    }
    if let Some(notes) = request
        .notes
        .as_deref()
        .map(str::trim)
        .filter(|s| !s.is_empty())
    {
        body["notes"] = serde_json::Value::String(notes.to_string());
    }

    if let Some(meta) = request.desktop_meta {
        let mut meta_json = serde_json::Map::new();

        if let Some(freq) = meta.frequency_mhz.filter(|v| v.is_finite() && *v > 0.0) {
            if let Some(num) = serde_json::Number::from_f64(freq) {
                meta_json.insert("frequency_mhz".into(), serde_json::Value::Number(num));
            }
        }
        if let Some(power) = meta.power_w.filter(|v| v.is_finite() && *v >= 0.0) {
            if let Some(num) = serde_json::Number::from_f64(power) {
                meta_json.insert("power_w".into(), serde_json::Value::Number(num));
            }
        }
        if let Some(name) = meta
            .name
            .as_deref()
            .map(str::trim)
            .filter(|s| !s.is_empty())
        {
            meta_json.insert("name".into(), serde_json::Value::String(name.to_string()));
        }
        if let Some(qth) = meta.qth.as_deref().map(str::trim).filter(|s| !s.is_empty()) {
            meta_json.insert("qth".into(), serde_json::Value::String(qth.to_string()));
        }

        if !meta_json.is_empty() {
            body["desktop_meta"] = serde_json::Value::Object(meta_json);
        }
    }

    let client_uuid = Uuid::new_v4().to_string();
    state
        .db
        .enqueue_qso(&client_uuid, &body.to_string(), "manual")
        .map_err(|e| AppError::Database(e.to_string()))?;

    {
        let mut sync = state.sync.lock().await;
        sync.last_error = None;
    }
    emit_status(&app, &state.sync).await;

    Ok(serde_json::json!({
        "queued": true,
        "client_uuid": client_uuid,
        "datetime_on": parsed_datetime.to_rfc3339(),
    }))
}

#[tauri::command]
pub async fn update_qso(
    state: tauri::State<'_, AppState>,
    app: tauri::AppHandle,
    request: UpdateQsoRequest,
) -> Result<serde_json::Value, AppError> {
    let existing_qso = state
        .db
        .get_cached_qso(request.uuid.trim())
        .map_err(|e| AppError::Database(e.to_string()))?
        .ok_or_else(|| AppError::Other("QSO not found in local cache".into()))?;

    let prepared = qso_update::prepare_update_qso(&existing_qso, request, Utc::now())
        .map_err(|e| AppError::Other(e.to_string()))?;

    state
        .db
        .update_cached_qso(&prepared.cached_qso)
        .map_err(|e| AppError::Database(e.to_string()))?;

    let client_uuid = Uuid::new_v4().to_string();
    state
        .db
        .enqueue_qso_update(
            &client_uuid,
            &prepared.cached_qso.uuid,
            &prepared.body.to_string(),
            "update",
        )
        .map_err(|e| AppError::Database(e.to_string()))?;

    {
        let mut sync = state.sync.lock().await;
        sync.last_error = None;
    }
    emit_status(&app, &state.sync).await;

    Ok(serde_json::json!({
        "queued": true,
        "client_uuid": client_uuid,
        "uuid": prepared.cached_qso.uuid,
    }))
}

#[tauri::command]
pub async fn get_callsign_history(
    state: tauri::State<'_, AppState>,
    callsign: String,
    limit: Option<usize>,
) -> Result<Vec<crate::db::CallsignHistoryItem>, AppError> {
    let normalized = callsign.trim().to_uppercase();
    if normalized.len() < 3 {
        return Ok(vec![]);
    }

    state
        .db
        .callsign_history(&normalized, limit.unwrap_or(8).clamp(1, 25))
        .map_err(|e| AppError::Database(e.to_string()))
}

// ─── Background loop ──────────────────────────────────────────────────────────

/// Get the user's primary logbook UUID from the server.
pub(crate) async fn fetch_primary_logbook_uuid(
    cfg: &crate::config::Config,
    token: &str,
) -> anyhow::Result<String> {
    let url = format!("{}/v1/logbooks/default", cfg.server.url);
    let resp = reqwest::Client::new()
        .get(&url)
        .bearer_auth(token)
        .send()
        .await?
        .error_for_status()?
        .json::<serde_json::Value>()
        .await?;

    resp["data"]["uuid"]
        .as_str()
        .context("Default logbook not found or missing UUID")
        .map(String::from)
}

pub(crate) async fn get_primary_logbook_uuid(
    cfg: &crate::config::Config,
    token: &str,
) -> anyhow::Result<String> {
    if let Some(uuid) = cfg
        .logbook
        .default_uuid
        .as_deref()
        .map(str::trim)
        .filter(|s| !s.is_empty())
    {
        return Ok(uuid.to_string());
    }

    let uuid = fetch_primary_logbook_uuid(cfg, token).await?;
    let mut fresh_cfg = crate::config::load()?;
    fresh_cfg.logbook.default_uuid = Some(uuid.clone());
    fresh_cfg.save()?;
    Ok(uuid)
}

/// Pull the logbook from the server and cache QSOs locally.
async fn pull_logbook(sync: Arc<Mutex<SyncState>>, _app: &tauri::AppHandle) -> anyhow::Result<()> {
    // 1. Get access token from keychain
    let token = auth::load_access_token()?.context("Not logged in")?;

    // 2. Get config for server URL
    let cfg = crate::config::load()?;

    // 3. Get user's logbook UUID
    let logbook_uuid = get_primary_logbook_uuid(&cfg, &token).await?;

    // 4. Cursor-based pagination loop
    let mut cursor: Option<String> = None;
    let mut total_cached = 0;

    loop {
        let url = if let Some(c) = &cursor {
            format!(
                "{}/v1/logbooks/{}/qsos?limit=100&after={}",
                cfg.server.url, logbook_uuid, c
            )
        } else {
            format!(
                "{}/v1/logbooks/{}/qsos?limit=100",
                cfg.server.url, logbook_uuid
            )
        };

        let resp = reqwest::Client::new()
            .get(&url)
            .bearer_auth(&token)
            .send()
            .await?
            .error_for_status()?
            .json::<serde_json::Value>()
            .await?;

        let items = resp["data"]["items"]
            .as_array()
            .context("Invalid response format")?;

        // 5. Upsert each QSO into cached_qsos
        {
            let db = &sync.lock().await.db;
            for item in items {
                // Extract mandatory string fields with proper error handling.
                // A malformed item from the server is logged and skipped rather
                // than panicking the entire sync loop.
                let uuid = match item["uuid"].as_str() {
                    Some(v) => v.to_string(),
                    None => {
                        warn!("pull_logbook: skipping item missing 'uuid': {item}");
                        continue;
                    }
                };
                let logbook_uuid = match item["logbook_uuid"].as_str() {
                    Some(v) => v.to_string(),
                    None => {
                        warn!("pull_logbook: skipping item {uuid} missing 'logbook_uuid'");
                        continue;
                    }
                };
                let callsign = match item["callsign"].as_str() {
                    Some(v) => v.to_string(),
                    None => {
                        warn!("pull_logbook: skipping item {uuid} missing 'callsign'");
                        continue;
                    }
                };
                let band = match item["band"].as_str() {
                    Some(v) => v.to_string(),
                    None => {
                        warn!("pull_logbook: skipping item {uuid} missing 'band'");
                        continue;
                    }
                };
                let mode = match item["mode"].as_str() {
                    Some(v) => v.to_string(),
                    None => {
                        warn!("pull_logbook: skipping item {uuid} missing 'mode'");
                        continue;
                    }
                };
                let datetime_on = match item["datetime_on"].as_str() {
                    Some(v) => v.to_string(),
                    None => {
                        warn!("pull_logbook: skipping item {uuid} missing 'datetime_on'");
                        continue;
                    }
                };
                let created_at = match item["created_at"].as_str() {
                    Some(v) => v.to_string(),
                    None => {
                        warn!("pull_logbook: skipping item {uuid} missing 'created_at'");
                        continue;
                    }
                };
                let updated_at = match item["updated_at"].as_str() {
                    Some(v) => v.to_string(),
                    None => {
                        warn!("pull_logbook: skipping item {uuid} missing 'updated_at'");
                        continue;
                    }
                };

                let qso = crate::db::CachedQso {
                    id: 0, // auto-increment
                    uuid,
                    logbook_uuid,
                    callsign,
                    band,
                    mode,
                    datetime_on,
                    rst_sent: item
                        .get("rst_sent")
                        .and_then(|v| v.as_str())
                        .map(String::from),
                    rst_rcvd: item
                        .get("rst_rcvd")
                        .and_then(|v| v.as_str())
                        .map(String::from),
                    name: item.get("name").and_then(|v| v.as_str()).map(String::from),
                    qth: item.get("qth").and_then(|v| v.as_str()).map(String::from),
                    gridsquare: item
                        .get("gridsquare")
                        .and_then(|v| v.as_str())
                        .map(String::from),
                    dxcc: item.get("dxcc").and_then(|v| v.as_i64()).map(|n| n as i32),
                    country: item
                        .get("country")
                        .and_then(|v| v.as_str())
                        .map(String::from),
                    cq_zone: item
                        .get("cq_zone")
                        .and_then(|v| v.as_i64())
                        .map(|n| n as i32),
                    itu_zone: item
                        .get("itu_zone")
                        .and_then(|v| v.as_i64())
                        .map(|n| n as i32),
                    continent: item
                        .get("continent")
                        .and_then(|v| v.as_str())
                        .map(String::from),
                    comment: item
                        .get("comment")
                        .and_then(|v| v.as_str())
                        .map(String::from),
                    notes: item.get("notes").and_then(|v| v.as_str()).map(String::from),
                    created_at,
                    updated_at,
                    synced_at: Utc::now().to_rfc3339(),
                };
                db.upsert_qso(&qso)?;
                total_cached += 1;
            }
        }

        // 6. Check for next_cursor; if none, break
        cursor = resp["data"]
            .get("next_cursor")
            .and_then(|v| v.as_str())
            .map(String::from);

        if cursor.is_none() {
            break;
        }
    }

    info!("Cached {total_cached} QSO(s) from server logbook");
    Ok(())
}

/// Runs the sync loop indefinitely. Launched from `lib.rs` setup.
pub async fn run_sync_loop(sync: Arc<Mutex<SyncState>>, app_handle: tauri::AppHandle) {
    let interval = {
        crate::config::load()
            .map(|c| c.sync.interval_seconds)
            .unwrap_or(300)
    };

    info!("Sync loop started (interval: {interval}s)");

    loop {
        run_sync_cycle(Arc::clone(&sync), &app_handle).await;
        tokio::time::sleep(Duration::from_secs(interval)).await;
    }
}

/// One sync cycle: fetch pending QSOs, POST each to the server.
async fn run_sync_cycle(sync: Arc<Mutex<SyncState>>, app: &tauri::AppHandle) {
    let db = {
        let s = sync.lock().await;
        Arc::clone(&s.db)
    };

    // PUSH PHASE: Send pending QSOs to server
    let pending = match db.pending_qsos(50) {
        Ok(p) => p,
        Err(e) => {
            error!("Failed to fetch pending QSOs: {e}");
            vec![] // Continue to pull even if push check failed
        }
    };

    if !pending.is_empty() {
        info!("Syncing {} QSO(s) to server", pending.len());

        let access_token = match auth::load_access_token() {
            Ok(Some(t)) => t,
            Ok(None) => {
                debug!("Not logged in — skipping push");
                // Don't return — continue to pull below
                String::new()
            }
            Err(e) => {
                warn!("Failed to load access token for push: {e}");
                String::new()
            }
        };

        if !access_token.is_empty() {
            let cfg = match crate::config::load() {
                Ok(c) => c,
                Err(e) => {
                    error!("Config load failed: {e}");
                    // Continue to pull
                    emit_status(app, &sync).await;
                    return;
                }
            };

            let logbook_uuid = match get_primary_logbook_uuid(&cfg, &access_token).await {
                Ok(uuid) => uuid,
                Err(e) => {
                    let msg = format!("Failed to resolve default logbook: {e}");
                    warn!("{msg}");
                    let mut s = sync.lock().await;
                    s.last_error = Some(msg);
                    emit_status(app, &sync).await;
                    return;
                }
            };

            let client = reqwest::Client::new();
            let api_base = &cfg.server.url;
            let mut saw_push_error = false;

            for qso in &pending {
                let result =
                    sync_single_qso(&client, api_base, &logbook_uuid, &access_token, qso).await;
                match result {
                    Ok(()) => {
                        if let Err(e) = db.mark_synced(qso.id) {
                            error!("Failed to mark QSO {} as synced: {e}", qso.id);
                        }
                    }
                    Err(SyncQsoError::TokenRefreshed) => {
                        info!("QSO {}: token refreshed; will retry next cycle", qso.id);
                    }
                    Err(e) => {
                        let msg = e.to_string();
                        warn!(
                            "Failed to sync QSO {} (attempt {}): {msg}",
                            qso.id,
                            qso.attempts + 1
                        );
                        let _ = db.record_attempt_failure(qso.id, &msg);
                        saw_push_error = true;

                        {
                            let mut s = sync.lock().await;
                            s.last_error = Some(msg);
                        }

                        if qso.attempts >= 5 {
                            warn!(
                                "QSO {} has failed {} times; will retry later",
                                qso.id, qso.attempts
                            );
                        }
                    }
                }
            }

            {
                let mut s = sync.lock().await;
                s.last_sync_at = Some(Utc::now());
                if !saw_push_error {
                    s.last_error = None;
                }
            }
        }
    } else {
        debug!("No pending QSOs to push");
    }

    // PULL PHASE: Fetch logbook from server (ALWAYS, regardless of push result)
    match auth::load_access_token() {
        Ok(Some(_token)) => {
            info!("Pulling logbook from server...");
            if let Err(e) = pull_logbook(Arc::clone(&sync), app).await {
                warn!("Failed to pull logbook: {e}");
                let mut s = sync.lock().await;
                s.last_error = Some(format!("Pull failed: {e}"));
            } else {
                // Clear pull-related errors on success
                let mut s = sync.lock().await;
                if let Some(ref err) = s.last_error {
                    if err.contains("Pull failed") {
                        s.last_error = None;
                    }
                }
            }
        }
        Ok(None) => {
            debug!("Not logged in — skipping logbook pull");
        }
        Err(e) => {
            warn!("Failed to load access token for pull: {e}");
        }
    }

    emit_status(app, &sync).await;
    info!("Sync cycle complete");
}

/// Push a single queued QSO to the server.
async fn sync_single_qso(
    client: &reqwest::Client,
    api_base: &str,
    logbook_uuid: &str,
    access_token: &str,
    qso: &QueuedQso,
) -> Result<(), SyncQsoError> {
    // Map anyhow errors from parsing/building into SyncQsoError::Other.
    let inner = async {
        let raw: serde_json::Value =
            serde_json::from_str(&qso.data).context("Failed to parse stored QSO JSON")?;

        let (queued_qso_uuid, resolved_logbook_uuid, mut data) =
            payload::build_sync_payload(&raw, logbook_uuid)?;

        callsign::hydrate_wsjtx_payload_if_needed(
            client,
            api_base,
            access_token,
            &qso.source,
            &qso.id.to_string(),
            &mut data,
        )
        .await;

        let request = if let Some(qso_uuid) = queued_qso_uuid {
            let url = format!(
                "{}/v1/logbooks/{}/qsos/{}",
                api_base.trim_end_matches('/'),
                resolved_logbook_uuid,
                qso_uuid
            );
            client.put(&url).bearer_auth(access_token).json(&data)
        } else {
            let url = format!(
                "{}/v1/logbooks/{}/qsos",
                api_base.trim_end_matches('/'),
                resolved_logbook_uuid
            );
            client.post(&url).bearer_auth(access_token).json(&data)
        };

        let resp = request.send().await.context("HTTP request failed")?;

        Ok::<_, anyhow::Error>(resp)
    }
    .await;

    let resp = inner.map_err(SyncQsoError::Other)?;

    if resp.status().is_success() {
        debug!("QSO {} synced successfully", qso.id);
        Ok(())
    } else if resp.status() == reqwest::StatusCode::UNAUTHORIZED {
        // Token has expired.  Attempt a refresh so the next sync cycle
        // can use a fresh token.
        //
        // IMPORTANT: only suppress the QSO failure counter when the refresh
        // actually succeeds.  If the refresh fails (expired refresh token,
        // network error, etc.) we treat it as a real sync error so the
        // failure is counted and the user gets feedback.
        warn!("Access token expired; attempting refresh with retry");
        match auth::refresh_access_token_with_backoff().await {
            Ok(_) => {
                info!(
                    "Token refreshed successfully; QSO {} will retry next cycle",
                    qso.id
                );
                Err(SyncQsoError::TokenRefreshed)
            }
            Err(e) => {
                warn!("Token refresh failed: {e}");
                Err(SyncQsoError::TokenRefreshFailed(e.to_string()))
            }
        }
    } else {
        let status = resp.status();
        let body = resp.text().await.unwrap_or_default();
        Err(SyncQsoError::Other(anyhow::anyhow!(
            "Server returned {status}: {body}"
        )))
    }
}

/// Emit a sync-status-changed event to all open windows.
async fn emit_status(app: &tauri::AppHandle, sync: &Arc<Mutex<SyncState>>) {
    let status = {
        let s = sync.lock().await;
        let pending = s.db.pending_count().unwrap_or(0);
        let cached_qso_count = s.db.count_qsos().unwrap_or(0);
        SyncStatus {
            pending,
            cached_qso_count,
            last_sync: s.last_sync_at.map(|t| t.to_rfc3339()),
            last_error: s.last_error.clone(),
        }
    };

    use tauri::Emitter;
    if let Err(e) = app.emit(EVENT_SYNC_STATUS, &status) {
        debug!("Failed to emit sync status event: {e}");
    }

    crate::tray::refresh_tray(app).await;
}

#[cfg(test)]
mod tests {
    use super::*;
    use chrono::Utc;
    use serde_json::json;

    fn test_base_url() -> String {
        std::env::var("RADIOLEDGER_BASE_URL")
            .unwrap_or_else(|_| "http://localhost:9091".to_string())
    }

    fn test_email() -> String {
        std::env::var("RADIOLEDGER_TEST_EMAIL")
            .unwrap_or_else(|_| "test@example.radioledger.local".to_string())
    }

    fn test_password() -> String {
        std::env::var("RADIOLEDGER_TEST_PASSWORD").unwrap_or_else(|_| "TestPassword123!".to_string())
    }

    async fn login_token(client: &reqwest::Client, base_url: &str) -> anyhow::Result<String> {
        let resp = client
            .post(format!("{}/v1/auth/login", base_url.trim_end_matches('/')))
            .json(&json!({
                "email": test_email(),
                "password": test_password(),
            }))
            .send()
            .await?
            .error_for_status()?;

        let body: serde_json::Value = resp.json().await?;
        body["data"]["token"]
            .as_str()
            .map(str::to_string)
            .context("login token missing")
    }

    async fn default_logbook_uuid(
        client: &reqwest::Client,
        base_url: &str,
        token: &str,
    ) -> anyhow::Result<String> {
        let resp = client
            .get(format!(
                "{}/v1/logbooks/default",
                base_url.trim_end_matches('/')
            ))
            .bearer_auth(token)
            .send()
            .await?
            .error_for_status()?;
        let body: serde_json::Value = resp.json().await?;
        body["data"]["uuid"]
            .as_str()
            .map(str::to_string)
            .context("default logbook uuid missing")
    }

    async fn create_qso(
        client: &reqwest::Client,
        base_url: &str,
        token: &str,
        logbook_uuid: &str,
    ) -> anyhow::Result<serde_json::Value> {
        let suffix = &Uuid::new_v4().simple().to_string()[..4];
        let resp = client
            .post(format!(
                "{}/v1/logbooks/{}/qsos",
                base_url.trim_end_matches('/'),
                logbook_uuid
            ))
            .bearer_auth(token)
            .json(&json!({
                "callsign": format!("N5T{}", suffix),
                "band": "20m",
                "mode": "FT8",
                "datetime_on": Utc::now().to_rfc3339(),
                "rst_sent": "599",
                "rst_rcvd": "599",
                "notes": "issue-188 sync integration seed"
            }))
            .send()
            .await?
            .error_for_status()?;


        let body: serde_json::Value = resp.json().await?;
        Ok(body["data"].clone())
    }

    async fn fetch_qso(
        client: &reqwest::Client,
        base_url: &str,
        token: &str,
        logbook_uuid: &str,
        uuid: &str,
    ) -> anyhow::Result<serde_json::Value> {
        let resp = client
            .get(format!(
                "{}/v1/logbooks/{}/qsos?limit=100",
                base_url.trim_end_matches('/'),
                logbook_uuid
            ))
            .bearer_auth(token)
            .send()
            .await?
            .error_for_status()?;

        let body: serde_json::Value = resp.json().await?;
        body["data"]["items"]
            .as_array()
            .and_then(|items| {
                items
                    .iter()
                    .find(|item| item["uuid"].as_str() == Some(uuid))
                    .cloned()
            })
            .context("updated qso not found in list response")
    }

    async fn delete_qso(
        client: &reqwest::Client,
        base_url: &str,
        token: &str,
        logbook_uuid: &str,
        uuid: &str,
    ) -> anyhow::Result<()> {
        client
            .delete(format!(
                "{}/v1/logbooks/{}/qsos/{}",
                base_url.trim_end_matches('/'),
                logbook_uuid,
                uuid
            ))
            .bearer_auth(token)
            .send()
            .await?
            .error_for_status()?;
        Ok(())
    }

    #[tokio::test]
    #[ignore = "requires a reachable live RadioLedger staging server and test credentials"]
    async fn sync_single_qso_updates_existing_server_qso() -> anyhow::Result<()> {
        let client = reqwest::Client::new();
        let base_url = test_base_url();
        let token = login_token(&client, &base_url).await?;
        let logbook_uuid = default_logbook_uuid(&client, &base_url, &token).await?;
        let created = create_qso(&client, &base_url, &token, &logbook_uuid).await?;

        let qso_uuid = created["uuid"]
            .as_str()
            .context("created qso uuid missing")?;
        let callsign = created["callsign"]
            .as_str()
            .context("created qso callsign missing")?;
        let datetime_on = created["datetime_on"]
            .as_str()
            .context("created qso datetime_on missing")?;

        let new_qth = "Issue 188 Test QTH";
        let new_grid = "EM34";
        let new_notes = "issue-188 desktop edit sync verified";

        let queued = QueuedQso {
            id: 1,
            client_uuid: Uuid::new_v4().to_string(),
            data: json!({
                "uuid": qso_uuid,
                "logbook_uuid": logbook_uuid,
                "callsign": callsign,
                "band": "20m",
                "mode": "FT8",
                "datetime_on": datetime_on,
                "rst_sent": "599",
                "rst_rcvd": "599",
                "qth": new_qth,
                "gridsquare": new_grid,
                "notes": new_notes,
            })
            .to_string(),
            source: "update".to_string(),
            created_at: Utc::now(),
            attempts: 0,
            last_attempt_at: None,
            last_error: None,
        };

        sync_single_qso(&client, &base_url, &logbook_uuid, &token, &queued).await?;

        let updated = fetch_qso(&client, &base_url, &token, &logbook_uuid, qso_uuid).await?;
        delete_qso(&client, &base_url, &token, &logbook_uuid, qso_uuid).await?;

        assert_eq!(updated["qth"].as_str(), Some(new_qth));
        assert_eq!(updated["gridsquare"].as_str(), Some(new_grid));
        assert_eq!(updated["notes"].as_str(), Some(new_notes));

        Ok(())
    }
}
