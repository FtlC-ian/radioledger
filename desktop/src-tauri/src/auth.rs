//! OAuth2 Authorization Code + PKCE authentication flow.
//!
//! ## Design (per DESKTOP_CLIENT.md and RFC 8252 §7.3)
//!
//! 1. Generate a PKCE code verifier (43+ chars, crypto-random) and S256 challenge.
//! 2. Generate a random `state` parameter to prevent CSRF on the OAuth redirect.
//! 3. Bind an ephemeral TCP listener on a random loopback port.
//! 4. Open the system browser to the Zitadel authorization endpoint.
//! 5. Zitadel redirects to `http://127.0.0.1:{port}/callback?code=...&state=...`.
//! 6. Validate that `state` matches what we sent.
//! 7. Exchange the code + code verifier for access + refresh tokens.
//! 8. Store tokens in OS keychain when available, with config-file fallback.
//! 9. Immediately close the ephemeral HTTP listener.
//!
//! ## Token refresh
//! Call `refresh_access_token_with_backoff()` before the access token expires.
//! Retries use exponential back-off with jitter capped at 30 s.
//!
//! ## Logout
//! `logout` revokes the refresh token at the Zitadel revocation endpoint
//! (RFC 7009) then clears all stored credentials from keychain + config callsign.
//!
//! ## Why loopback, not custom URI scheme
//! Custom URI schemes (`radioledger://callback`) are interceptable — any app
//! can register the same scheme. A loopback redirect binds a specific port at
//! auth time; the server validates the redirect_uri against the registered
//! pattern `http://127.0.0.1:*/callback`. See RFC 8252 §7.3.

use anyhow::Context;
use base64::{engine::general_purpose::URL_SAFE_NO_PAD, Engine as _};
use keyring::{Entry, Error as KeyringError};
use rand::distributions::Alphanumeric;
use rand::{thread_rng, Rng};
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};
use std::collections::HashMap;
use std::net::TcpListener;
use std::sync::OnceLock;
use std::time::Duration;
use tokio::sync::oneshot;
use tracing::{debug, error, info, warn};

use crate::error::AppError;
use crate::AppState;

// ─── Local auth (self-hosted) ─────────────────────────────────────────────────

/// Request body for local email/password login against a self-hosted server.
#[derive(Debug, Deserialize)]
pub struct LocalLoginRequest {
    pub server_url: String,
    pub email: String,
    pub password: String,
}

/// Outer envelope returned by POST /v1/auth/login
#[derive(Debug, Deserialize)]
struct LocalLoginEnvelope {
    success: bool,
    data: Option<LocalLoginData>,
    #[allow(dead_code)]
    error: Option<String>,
}

#[derive(Debug, Deserialize)]
struct LocalLoginData {
    token: String,
    user: LocalLoginUser,
}

#[derive(Debug, Deserialize)]
struct LocalLoginUser {
    callsign: String,
    #[allow(dead_code)]
    email: Option<String>,
    #[allow(dead_code)]
    uuid: Option<String>,
}

// ─── OAuth configuration ─────────────────────────────────────────────────────
const OAUTH_CLIENT_ID: &str = "radioledger-desktop";
const OAUTH_SCOPES: &str =
    "openid profile qsos:read qsos:write sync:status logbooks:read offline_access";

// ─── Keychain configuration ──────────────────────────────────────────────────
const KEYCHAIN_SERVICE: &str = "com.radioledger.desktop";
const KEYCHAIN_USERNAME: &str = "oauth_tokens";

static KEYCHAIN_AVAILABLE: OnceLock<bool> = OnceLock::new();

// ─── Token refresh back-off parameters ───────────────────────────────────────
const REFRESH_INITIAL_DELAY_MS: u64 = 500;
const REFRESH_MAX_DELAY_MS: u64 = 30_000;
const REFRESH_MAX_ATTEMPTS: u32 = 8;
const REFRESH_JITTER_MS: u64 = 200;

// ─── Data structures ─────────────────────────────────────────────────────────

/// Auth status returned to the frontend.
#[derive(Debug, Serialize, Deserialize, Clone)]
pub struct AuthStatus {
    pub logged_in: bool,
    pub callsign: Option<String>,
}

/// Richer user info returned by `get_current_user`.
#[derive(Debug, Serialize, Deserialize, Clone)]
pub struct CurrentUser {
    pub callsign: String,
    pub display_name: Option<String>,
    pub email: Option<String>,
}

/// Token set received from the Zitadel token endpoint.
#[derive(Debug, Deserialize)]
struct TokenResponse {
    access_token: String,
    refresh_token: Option<String>,
    id_token: Option<String>,
    #[allow(dead_code)]
    expires_in: Option<u64>,
}

#[derive(Debug, Serialize, Deserialize, Clone)]
struct StoredTokens {
    access_token: String,
    refresh_token: Option<String>,
    id_token: Option<String>,
}

/// PKCE session state held in memory during the auth flow.
struct PkceSession {
    code_verifier: String,
    state: String,
    redirect_uri: String,
}

// ─── Tauri commands ───────────────────────────────────────────────────────────

/// Returns `true` if the user has a stored access token in keychain.
/// Fast synchronous check; does NOT attempt a token refresh.
#[tauri::command]
pub fn is_authenticated(_state: tauri::State<AppState>) -> bool {
    matches!(load_access_token(), Ok(Some(_)))
}

/// Returns auth status (logged_in + callsign) for the frontend.
#[tauri::command]
pub fn get_auth_status(_state: tauri::State<AppState>) -> Result<AuthStatus, AppError> {
    let logged_in = load_access_token().ok().flatten().is_some();
    let callsign = if logged_in {
        load_callsign().ok().flatten()
    } else {
        None
    };

    Ok(AuthStatus {
        logged_in,
        callsign,
    })
}

/// Returns the current user's profile information (callsign, name, email).
/// Returns `AppError::Auth` if not logged in.
#[tauri::command]
pub fn get_current_user(_state: tauri::State<AppState>) -> Result<CurrentUser, AppError> {
    let callsign = load_callsign()
        .map_err(|e| AppError::Config(e.to_string()))?
        .ok_or_else(|| AppError::Auth("Not logged in".into()))?;

    // Best-effort: decode the stored id_token for richer claims.
    let from_token = load_stored_tokens()
        .ok()
        .flatten()
        .and_then(|t| t.id_token)
        .and_then(|id| parse_id_token_claims(&id));

    Ok(CurrentUser {
        callsign,
        display_name: from_token.as_ref().and_then(|u| u.display_name.clone()),
        email: from_token.and_then(|u| u.email),
    })
}

/// Initiates the PKCE login flow. Opens the system browser; returns when the
/// user has authenticated and tokens are stored in OS keychain.
#[tauri::command]
pub async fn login(app: tauri::AppHandle) -> Result<AuthStatus, AppError> {
    let cfg = crate::config::load().map_err(|e| AppError::Config(e.to_string()))?;
    let auth_base = format!("{}/oauth/v2/authorize", cfg.server.url);
    let token_endpoint = format!("{}/oauth/v2/token", cfg.server.url);

    // ── Step 1: PKCE verifier + S256 challenge ────────────────────────────────
    let code_verifier = generate_code_verifier();
    let code_challenge = generate_code_challenge(&code_verifier);
    let state_param = generate_state();

    // ── Step 2: bind ephemeral loopback port ──────────────────────────────────
    let listener = TcpListener::bind("127.0.0.1:0")
        .map_err(|e| AppError::Auth(format!("Failed to bind loopback port: {e}")))?;
    let port = listener
        .local_addr()
        .map_err(|e| AppError::Auth(format!("Could not read port: {e}")))?
        .port();
    let redirect_uri = format!("http://127.0.0.1:{port}/callback");

    let pkce = PkceSession {
        code_verifier,
        state: state_param.clone(),
        redirect_uri: redirect_uri.clone(),
    };

    // ── Step 3: build authorization URL ──────────────────────────────────────
    let auth_url = build_auth_url(
        &auth_base,
        OAUTH_CLIENT_ID,
        &redirect_uri,
        &code_challenge,
        &state_param,
        OAUTH_SCOPES,
    );

    info!("Opening system browser for PKCE login (port {port})");
    debug!("Authorization URL: {auth_url}");

    // ── Step 4: open system browser ──────────────────────────────────────────
    #[allow(deprecated)]
    {
        use tauri_plugin_shell::ShellExt;
        app.shell()
            .open(&auth_url, None)
            .map_err(|e| AppError::Auth(format!("Failed to open browser: {e}")))?;
    }

    // ── Step 5: await callback ────────────────────────────────────────────────
    let (tx, rx) = oneshot::channel::<HashMap<String, String>>();
    let async_listener = tokio::net::TcpListener::from_std(listener)
        .map_err(|e| AppError::Auth(format!("Async listener error: {e}")))?;

    tauri::async_runtime::spawn(async move {
        if let Ok((mut stream, _)) = async_listener.accept().await {
            if let Ok(params) = read_callback_params(&mut stream).await {
                let _ = tx.send(params);
            }
        }
    });

    let params = rx
        .await
        .map_err(|_| AppError::Auth("Auth callback never received".into()))?;

    // ── Step 6: CSRF state check ──────────────────────────────────────────────
    let returned_state = params
        .get("state")
        .ok_or_else(|| AppError::Auth("Missing state in callback".into()))?;
    if returned_state != &pkce.state {
        error!("PKCE state mismatch — possible CSRF attack; aborting login");
        return Err(AppError::Auth(
            "State parameter mismatch (CSRF check failed)".into(),
        ));
    }

    let code = params
        .get("code")
        .ok_or_else(|| AppError::Auth("Missing code in callback".into()))?
        .clone();

    // ── Step 7: exchange code for tokens ─────────────────────────────────────
    let tokens = exchange_code_for_tokens(
        &token_endpoint,
        OAUTH_CLIENT_ID,
        &code,
        &pkce.code_verifier,
        &pkce.redirect_uri,
    )
    .await
    .map_err(|e| AppError::Auth(format!("Token exchange failed: {e}")))?;

    // ── Step 8: store tokens in keychain ─────────────────────────────────────
    store_token_response(&tokens)
        .map_err(|e| AppError::Keychain(format!("Keychain store failed: {e}")))?;

    // ── Step 9: extract and store callsign ────────────────────────────────────
    let callsign = tokens
        .id_token
        .as_deref()
        .and_then(extract_callsign_from_id_token)
        .unwrap_or_else(|| "UNKNOWN".into());

    store_callsign(&callsign)
        .map_err(|e| AppError::Auth(format!("Failed to store callsign: {e}")))?;

    if let Err(e) = hydrate_default_logbook_uuid(&cfg, &tokens.access_token).await {
        warn!("Failed to cache default logbook UUID after login: {e}");
    }

    info!("Login successful for callsign {callsign}");
    crate::tray::refresh_tray(&app).await;
    Ok(AuthStatus {
        logged_in: true,
        callsign: Some(callsign),
    })
}

/// Revokes the refresh token at Zitadel (RFC 7009), then clears all stored
/// credentials from keychain and config metadata.
#[tauri::command]
pub async fn logout(
    _state: tauri::State<'_, AppState>,
    app: tauri::AppHandle,
) -> Result<(), AppError> {
    // Best-effort token revocation — non-fatal if endpoint unreachable.
    if let (Ok(cfg), Ok(Some(tokens))) = (crate::config::load(), load_stored_tokens()) {
        if let Some(rt) = tokens.refresh_token.as_deref() {
            let revoke_endpoint = format!("{}/oauth/v2/revoke", cfg.server.url);
            let client = reqwest::Client::new();
            match client
                .post(&revoke_endpoint)
                .form(&[("token", rt), ("client_id", OAUTH_CLIENT_ID)])
                .send()
                .await
            {
                Ok(resp) => {
                    if resp.status().is_success() {
                        info!("Refresh token revoked at Zitadel");
                    } else {
                        warn!("Revocation endpoint returned status {}", resp.status());
                    }
                }
                Err(e) => warn!("Token revocation request failed (non-fatal): {e}"),
            }
        }
    }

    clear_stored_credentials()
        .map_err(|e| AppError::Keychain(format!("Failed to clear credentials: {e}")))?;
    info!("User logged out; all credentials cleared from keychain");
    crate::tray::refresh_tray(&app).await;
    Ok(())
}

/// Email/password login against a self-hosted RadioLedger server (`AUTH_MODE=local`).
///
/// POSTs `{ email, password }` to `{server_url}/v1/auth/login`, parses the
/// response envelope, and stores the returned JWT in keychain and callsign in config.
#[tauri::command]
pub async fn login_local(request: LocalLoginRequest) -> Result<AuthStatus, AppError> {
    let url = format!("{}/v1/auth/login", request.server_url.trim_end_matches('/'));
    info!("Local login: POST {url}");

    let client = reqwest::Client::builder()
        .timeout(Duration::from_secs(15))
        .build()
        .map_err(|e| AppError::Network(format!("HTTP client build failed: {e}")))?;

    let resp = client
        .post(&url)
        .json(&serde_json::json!({
            "email": request.email,
            "password": request.password,
        }))
        .send()
        .await
        .map_err(|e| AppError::Network(format!("Login request failed: {e}")))?;

    if !resp.status().is_success() {
        let status = resp.status();
        let body = resp.text().await.unwrap_or_default();
        return Err(AppError::Auth(format!("Server returned {status}: {body}")));
    }

    let envelope: LocalLoginEnvelope = resp
        .json()
        .await
        .map_err(|e| AppError::Auth(format!("Failed to parse login response: {e}")))?;

    if !envelope.success {
        return Err(AppError::Auth("Login rejected by server".into()));
    }

    let data = envelope
        .data
        .ok_or_else(|| AppError::Auth("Login response missing data field".into()))?;

    // Store credentials in keychain
    store_access_token(&data.token)
        .map_err(|e| AppError::Keychain(format!("Failed to store access token: {e}")))?;

    let callsign = data.user.callsign.to_uppercase();
    store_callsign(&callsign)
        .map_err(|e| AppError::Auth(format!("Failed to store callsign: {e}")))?;

    let cfg = crate::config::load().map_err(|e| AppError::Config(e.to_string()))?;
    if let Err(e) = hydrate_default_logbook_uuid(&cfg, &data.token).await {
        warn!("Failed to cache default logbook UUID after local login: {e}");
    }

    info!("Stored credentials for callsign {} in keychain", callsign);
    Ok(AuthStatus {
        logged_in: true,
        callsign: Some(callsign),
    })
}

/// Returns the raw JWT access token for use in API calls (e.g. stats dashboard).
/// Returns `AppError::Auth` if no token is stored.
#[tauri::command]
pub fn get_api_token() -> Result<String, AppError> {
    load_access_token()
        .map_err(|e| AppError::Keychain(e.to_string()))?
        .ok_or_else(|| AppError::Auth("Not logged in — no access token stored".into()))
}

/// Validate a path submitted to `api_get`:
///
/// 1. Reject any percent-encoded characters (prevents encoded traversal like
///    `%2F`, `%2e%2e`, etc. that the server might decode differently).
/// 2. Reject path components that are `.` or `..` to block traversal.
/// 3. Confirm the normalized path matches one of the narrow allowed patterns.
///
/// Allowed patterns (all must start with `/`):
/// - `/v1/stats/<stat>` — stats sub-routes the UI reads
///
/// These are the *only* endpoints the frontend calls via `api_get` (see
/// `desktop/src/main.ts`, `statsApiGet`).  If new
/// frontend endpoints are added, extend this function explicitly.
fn validate_api_get_path(path: &str) -> bool {
    // Reject percent-encoded sequences — no legitimate call needs them, and
    // they are the primary vector for server-side path traversal bypasses.
    if path.contains('%') {
        return false;
    }

    // Reject any `.` or `..` path segments.
    for segment in path.split('/') {
        if segment == "." || segment == ".." {
            return false;
        }
    }

    // Strip the query string before pattern matching (query params are
    // caller-controlled and pass through as-is; we only gate on the path).
    let path_only = path.split('?').next().unwrap_or(path);

    // Pattern: /v1/stats/<stat>
    // Stat segment: alphanumeric and hyphens only.  No further nesting is
    // permitted.
    if let Some(stat_name) = path_only.strip_prefix("/v1/stats/") {
        // Must be exactly /v1/stats/<stat> — no extra segments.
        let stat_ok = !stat_name.is_empty()
            && !stat_name.contains('/')
            && stat_name.chars().all(|c| c.is_ascii_alphanumeric() || c == '-');
        return stat_ok;
    }

    false
}

/// Proxy an authenticated GET request through the Rust backend.
///
/// The WebView's fetch() is sandboxed and may be blocked by CSP or Tauri's
/// security model.  This command lets the frontend call specific server API
/// endpoints via `invoke('api_get', { path: '/v1/stats/overview' })`.
///
/// # Security
/// `path` is validated by [`validate_api_get_path`] before forwarding.
/// The allowed set is intentionally narrow — only the specific endpoints the
/// frontend actually calls.  Requests to any other path are rejected.
#[tauri::command]
pub async fn api_get(path: String) -> Result<String, AppError> {
    // Validate path: reject traversal and restrict to the narrow allowlist.
    if !validate_api_get_path(&path) {
        warn!("api_get: rejected disallowed or unsafe path {:?}", path);
        return Err(AppError::Auth(format!(
            "api_get: path '{path}' is not in the allowed list"
        )));
    }

    let cfg = crate::config::load().map_err(|e| AppError::Config(e.to_string()))?;
    let token = get_api_token()?;
    let url = format!("{}{}", cfg.server.url, path);

    let client = reqwest::Client::builder()
        .timeout(std::time::Duration::from_secs(15))
        .build()
        .map_err(|e| AppError::Network(format!("HTTP client build failed: {e}")))?;

    let resp = client
        .get(&url)
        .bearer_auth(&token)
        .send()
        .await
        .map_err(|e| AppError::Network(format!("API request failed: {e}")))?;

    let body = resp
        .text()
        .await
        .map_err(|e| AppError::Network(format!("Failed to read response: {e}")))?;

    Ok(body)
}

// ─── Token refresh ────────────────────────────────────────────────────────────

/// Refresh the access token once using the stored refresh token.
pub async fn refresh_access_token() -> anyhow::Result<String> {
    let cfg = crate::config::load()?;
    let token_endpoint = format!("{}/oauth/v2/token", cfg.server.url);

    let stored = load_stored_tokens()?.context("No stored credentials; user must log in again")?;
    let refresh_token = stored
        .refresh_token
        .context("No refresh token stored; user must log in again")?;

    let client = reqwest::Client::new();
    let resp: TokenResponse = client
        .post(&token_endpoint)
        .form(&[
            ("grant_type", "refresh_token"),
            ("client_id", OAUTH_CLIENT_ID),
            ("refresh_token", &refresh_token),
        ])
        .send()
        .await?
        .json()
        .await?;

    store_token_response(&resp)?;
    Ok(resp.access_token)
}

/// Refresh the access token with exponential back-off + jitter.
/// Retries up to `REFRESH_MAX_ATTEMPTS` times before returning an error.
pub async fn refresh_access_token_with_backoff() -> anyhow::Result<String> {
    let mut delay_ms = REFRESH_INITIAL_DELAY_MS;
    let mut last_err: anyhow::Error = anyhow::anyhow!("No attempts made");

    for attempt in 1..=REFRESH_MAX_ATTEMPTS {
        match refresh_access_token().await {
            Ok(token) => {
                if attempt > 1 {
                    info!("Token refresh succeeded on attempt {attempt}");
                }
                return Ok(token);
            }
            Err(e) => {
                warn!("Token refresh attempt {attempt}/{REFRESH_MAX_ATTEMPTS} failed: {e}");
                last_err = e;
                if attempt < REFRESH_MAX_ATTEMPTS {
                    let jitter: u64 = thread_rng().gen_range(0..REFRESH_JITTER_MS);
                    let sleep_ms = (delay_ms + jitter).min(REFRESH_MAX_DELAY_MS);
                    tokio::time::sleep(Duration::from_millis(sleep_ms)).await;
                    delay_ms = (delay_ms * 2).min(REFRESH_MAX_DELAY_MS);
                }
            }
        }
    }

    Err(last_err.context(format!(
        "Token refresh failed after {REFRESH_MAX_ATTEMPTS} attempts"
    )))
}

// ─── Keychain credential helpers ─────────────────────────────────────────────

pub fn initialize_keychain_availability() {
    let _ = is_keychain_available();
}

fn keychain_entry() -> anyhow::Result<Entry> {
    Entry::new(KEYCHAIN_SERVICE, KEYCHAIN_USERNAME)
        .map_err(|e| anyhow::anyhow!("Failed to create keychain entry: {e}"))
}

fn is_keychain_available() -> bool {
    *KEYCHAIN_AVAILABLE.get_or_init(|| {
        // The packaged desktop builds are still unsigned, and real-world auth
        // persistence has been flaky across restarts on both macOS and Windows
        // when we rely on the OS keychain alone. Until signing is in place and
        // we can verify stable cross-platform behaviour, prefer the config.yaml
        // fallback so login state actually survives app restarts. See #180.
        info!(
            "Auth token storage: ~/.radioledger/config.yaml (keychain disabled until signed desktop builds are stable)"
        );
        false
    })
}

fn load_stored_tokens_raw() -> anyhow::Result<Option<StoredTokens>> {
    let entry = keychain_entry()?;
    match entry.get_password() {
        Ok(json) => {
            let tokens: StoredTokens =
                serde_json::from_str(&json).context("Failed to decode keychain token payload")?;
            Ok(Some(tokens))
        }
        Err(KeyringError::NoEntry) => Ok(None),
        Err(e) => Err(anyhow::anyhow!("Failed to read keychain entry: {e}")),
    }
}

fn save_stored_tokens_to_keychain(tokens: &StoredTokens) -> anyhow::Result<()> {
    let entry = keychain_entry()?;
    let payload = serde_json::to_string(tokens).context("Failed to encode token payload")?;
    entry
        .set_password(&payload)
        .map_err(|e| anyhow::anyhow!("Failed to write keychain entry: {e}"))?;
    Ok(())
}

fn load_stored_tokens_from_config() -> anyhow::Result<Option<StoredTokens>> {
    let cfg = crate::config::load()?;
    if let Some(access_token) = cfg.auth.access_token {
        Ok(Some(StoredTokens {
            access_token,
            refresh_token: cfg.auth.refresh_token,
            id_token: cfg.auth.id_token,
        }))
    } else {
        Ok(None)
    }
}

fn save_stored_tokens_to_config(tokens: &StoredTokens) -> anyhow::Result<()> {
    let mut cfg = crate::config::load()?;
    cfg.auth.access_token = Some(tokens.access_token.clone());
    cfg.auth.refresh_token = tokens.refresh_token.clone();
    cfg.auth.id_token = tokens.id_token.clone();
    cfg.save()?;
    Ok(())
}

fn clear_stored_tokens_from_config() -> anyhow::Result<()> {
    let mut cfg = crate::config::load()?;
    cfg.auth.access_token = None;
    cfg.auth.refresh_token = None;
    cfg.auth.id_token = None;
    cfg.save()?;
    Ok(())
}

fn save_stored_tokens_with_fallback<F>(
    tokens: &StoredTokens,
    keychain_available: bool,
    save_keychain: F,
) -> anyhow::Result<()>
where
    F: FnOnce(&StoredTokens) -> anyhow::Result<()>,
{
    if keychain_available {
        match save_keychain(tokens) {
            Ok(()) => {
                // Keychain write succeeded — do NOT mirror tokens to the
                // plaintext config file.  The whole point of keychain storage
                // is to keep secrets off disk in an unprotected YAML file.
                return Ok(());
            }
            Err(e) => warn!(
                "Keychain write failed; storing auth tokens in ~/.radioledger/config.yaml instead: {e}"
            ),
        }
    }

    save_stored_tokens_to_config(tokens)
}

fn save_stored_tokens(tokens: &StoredTokens) -> anyhow::Result<()> {
    save_stored_tokens_with_fallback(
        tokens,
        is_keychain_available(),
        save_stored_tokens_to_keychain,
    )
}

fn clear_stored_tokens() -> anyhow::Result<()> {
    if let Err(e) = keychain_entry().and_then(|entry| match entry.delete_credential() {
        Ok(()) | Err(KeyringError::NoEntry) => Ok(()),
        Err(err) => Err(anyhow::anyhow!("Failed to delete keychain entry: {err}")),
    }) {
        warn!("Failed to clear keychain tokens (continuing with config cleanup): {e}");
    }

    clear_stored_tokens_from_config()
}

fn migrate_legacy_tokens_from_config_with<FLoad, FSave>(
    keychain_available: bool,
    load_keychain: FLoad,
    save_keychain: FSave,
) -> anyhow::Result<()>
where
    FLoad: FnOnce() -> anyhow::Result<Option<StoredTokens>>,
    FSave: FnOnce(&StoredTokens) -> anyhow::Result<()>,
{
    let cfg = crate::config::load()?;
    let had_legacy = cfg.auth.access_token.is_some()
        || cfg.auth.refresh_token.is_some()
        || cfg.auth.id_token.is_some();
    if !had_legacy || !keychain_available {
        return Ok(());
    }

    if load_keychain()?.is_none() {
        if let Some(access_token) = cfg.auth.access_token.clone() {
            let migrated = StoredTokens {
                access_token,
                refresh_token: cfg.auth.refresh_token.clone(),
                id_token: cfg.auth.id_token.clone(),
            };

            match save_keychain(&migrated) {
                Ok(()) => {
                    clear_stored_tokens_from_config()?;
                    info!("Migrated legacy auth tokens from config.yaml into keychain");
                }
                Err(e) => {
                    warn!(
                        "Keychain migration failed; keeping auth tokens in config.yaml fallback storage: {e}"
                    );
                }
            }
        }
    }

    Ok(())
}

fn migrate_legacy_tokens_from_config() -> anyhow::Result<()> {
    migrate_legacy_tokens_from_config_with(
        is_keychain_available(),
        load_stored_tokens_raw,
        save_stored_tokens_to_keychain,
    )
}

fn load_stored_tokens_with_fallback<F>(
    keychain_available: bool,
    load_keychain: F,
) -> anyhow::Result<Option<StoredTokens>>
where
    F: FnOnce() -> anyhow::Result<Option<StoredTokens>>,
{
    if keychain_available {
        match load_keychain() {
            Ok(tokens) => {
                if tokens.is_some() {
                    return Ok(tokens);
                }
            }
            Err(e) => warn!(
                "Keychain read failed; falling back to ~/.radioledger/config.yaml for auth tokens: {e}"
            ),
        }
    }

    load_stored_tokens_from_config()
}

fn load_stored_tokens() -> anyhow::Result<Option<StoredTokens>> {
    migrate_legacy_tokens_from_config()?;
    load_stored_tokens_with_fallback(is_keychain_available(), load_stored_tokens_raw)
}

fn store_access_token(token: &str) -> anyhow::Result<()> {
    let mut existing = load_stored_tokens()?.unwrap_or(StoredTokens {
        access_token: token.to_string(),
        refresh_token: None,
        id_token: None,
    });
    existing.access_token = token.to_string();
    save_stored_tokens(&existing)
}

pub fn load_access_token() -> anyhow::Result<Option<String>> {
    Ok(load_stored_tokens()?.map(|t| t.access_token))
}

fn store_callsign(callsign: &str) -> anyhow::Result<()> {
    let mut cfg = crate::config::load()?;
    cfg.auth.callsign = Some(callsign.to_string());
    cfg.save()?;
    Ok(())
}

async fn hydrate_default_logbook_uuid(
    cfg: &crate::config::Config,
    access_token: &str,
) -> anyhow::Result<()> {
    let uuid = crate::sync::fetch_primary_logbook_uuid(cfg, access_token).await?;
    let mut fresh_cfg = crate::config::load()?;
    fresh_cfg.logbook.default_uuid = Some(uuid);
    fresh_cfg.save()?;
    Ok(())
}

pub(crate) fn load_callsign() -> anyhow::Result<Option<String>> {
    let cfg = crate::config::load()?;
    Ok(cfg.auth.callsign.clone())
}

/// Stores all tokens from a token endpoint response, preferring keychain and
/// falling back to config storage if keychain access fails.
fn store_token_response(tokens: &TokenResponse) -> anyhow::Result<()> {
    let existing_refresh = load_stored_tokens()?.and_then(|t| t.refresh_token);
    let stored = StoredTokens {
        access_token: tokens.access_token.clone(),
        refresh_token: tokens.refresh_token.clone().or(existing_refresh),
        id_token: tokens.id_token.clone(),
    };
    save_stored_tokens(&stored)
}

fn clear_stored_credentials() -> anyhow::Result<()> {
    clear_stored_tokens()?;

    let mut cfg = crate::config::load()?;
    cfg.auth.callsign = None;
    cfg.auth.access_token = None;
    cfg.auth.refresh_token = None;
    cfg.auth.id_token = None;
    cfg.logbook.default_uuid = None;
    cfg.save()?;

    Ok(())
}

// ─── PKCE helpers ─────────────────────────────────────────────────────────────

fn generate_code_verifier() -> String {
    let mut rng = thread_rng();
    (0..64).map(|_| rng.sample(Alphanumeric) as char).collect()
}

/// S256 code challenge: BASE64URL(SHA256(verifier)) — no padding (RFC 7636 §4.2)
fn generate_code_challenge(verifier: &str) -> String {
    let hash = Sha256::digest(verifier.as_bytes());
    URL_SAFE_NO_PAD.encode(hash)
}

fn generate_state() -> String {
    let bytes: Vec<u8> = (0..32).map(|_| rand::random()).collect();
    URL_SAFE_NO_PAD.encode(bytes)
}

fn build_auth_url(
    base: &str,
    client_id: &str,
    redirect_uri: &str,
    code_challenge: &str,
    state: &str,
    scopes: &str,
) -> String {
    format!(
        "{base}?response_type=code&client_id={client_id}&redirect_uri={}&code_challenge={}&code_challenge_method=S256&state={}&scope={}",
        urlencoding::encode(redirect_uri),
        code_challenge,
        state,
        urlencoding::encode(scopes),
    )
}

// ─── Token exchange ───────────────────────────────────────────────────────────

async fn exchange_code_for_tokens(
    token_endpoint: &str,
    client_id: &str,
    code: &str,
    code_verifier: &str,
    redirect_uri: &str,
) -> anyhow::Result<TokenResponse> {
    let client = reqwest::Client::new();
    let resp: TokenResponse = client
        .post(token_endpoint)
        .form(&[
            ("grant_type", "authorization_code"),
            ("client_id", client_id),
            ("code", code),
            ("code_verifier", code_verifier),
            ("redirect_uri", redirect_uri),
        ])
        .send()
        .await
        .context("Token endpoint request failed")?
        .json()
        .await
        .context("Failed to parse token response")?;
    Ok(resp)
}

// ─── Callback listener ────────────────────────────────────────────────────────

async fn read_callback_params(
    stream: &mut tokio::net::TcpStream,
) -> anyhow::Result<HashMap<String, String>> {
    use tokio::io::{AsyncReadExt, AsyncWriteExt};

    let mut buf = [0u8; 4096];
    let n = stream.read(&mut buf).await?;
    let request = std::str::from_utf8(&buf[..n])?;

    let first_line = request.lines().next().unwrap_or("");
    let path = first_line.split_whitespace().nth(1).unwrap_or("");
    let query = path.split('?').nth(1).unwrap_or("");

    let params: HashMap<String, String> = url::form_urlencoded::parse(query.as_bytes())
        .into_owned()
        .collect();

    let body =
        "<html><body><h2>RadioLedger: login successful. You may close this tab.</h2></body></html>";
    let response = format!(
        "HTTP/1.1 200 OK\r\nContent-Type: text/html\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{}",
        body.len(),
        body
    );
    stream.write_all(response.as_bytes()).await?;

    Ok(params)
}

// ─── JWT helpers ──────────────────────────────────────────────────────────────

/// Decode the JWT payload segment using URL-safe base64 without padding.
///
/// # Signature verification — known limitation
///
/// The JWT **signature is intentionally not verified** here. This is an
/// acceptable trade-off for both auth flows:
///
/// * **Cloud / PKCE flow** — the token was received directly from the Zitadel
///   authorization server over a TLS-protected connection with full CSRF
///   protection (PKCE + `state` check).  The transport itself provides the
///   authenticity guarantee; local signature verification would require
///   fetching and caching the JWKS endpoint on every login.
///
/// * **Local / self-hosted flow** — the token comes from a server that the
///   user has explicitly configured in `~/.radioledger/config.yaml`.  We
///   trust that server by design; if the user points the app at a malicious
///   server, they can also modify config.yaml directly.
///
/// If full offline JWT verification is ever needed (e.g. for multi-tenant
/// deployments), use the `jsonwebtoken` crate with `DecodingKey::from_jwk`
/// and fetch the JWKS from `{server_url}/oauth/v2/keys`.
///
/// Tracked in project issue #172.
fn decode_id_token_claims(id_token: &str) -> Option<serde_json::Value> {
    let payload_b64 = id_token.split('.').nth(1)?;
    let bytes = URL_SAFE_NO_PAD.decode(payload_b64).ok()?;
    serde_json::from_slice(&bytes).ok()
}

fn extract_callsign_from_id_token(id_token: &str) -> Option<String> {
    let claims = decode_id_token_claims(id_token)?;
    claims
        .get("preferred_username")
        .or_else(|| claims.get("callsign"))
        .and_then(|v| v.as_str())
        .map(|s| s.to_uppercase())
}

fn parse_id_token_claims(id_token: &str) -> Option<CurrentUser> {
    let claims = decode_id_token_claims(id_token)?;

    let callsign = claims
        .get("preferred_username")
        .or_else(|| claims.get("callsign"))
        .and_then(|v| v.as_str())
        .map(|s| s.to_uppercase())?;

    Some(CurrentUser {
        callsign,
        display_name: claims
            .get("name")
            .and_then(|v| v.as_str())
            .map(str::to_owned),
        email: claims
            .get("email")
            .and_then(|v| v.as_str())
            .map(str::to_owned),
    })
}

#[cfg(test)]
mod tests {
    use super::*;
    use anyhow::anyhow;

    fn with_temp_home<F>(test_fn: F)
    where
        F: FnOnce(),
    {
        crate::test_support::with_temp_home("radioledger-auth-test", test_fn);
    }

    #[test]
    fn save_falls_back_to_config_when_keychain_write_fails() {
        with_temp_home(|| {
            let tokens = StoredTokens {
                access_token: "access-1".into(),
                refresh_token: Some("refresh-1".into()),
                id_token: Some("id-1".into()),
            };

            save_stored_tokens_with_fallback(&tokens, true, |_| {
                Err(anyhow!("simulated keychain write failure"))
            })
            .unwrap();

            let cfg = crate::config::load().unwrap();
            assert_eq!(cfg.auth.access_token, Some("access-1".into()));
            assert_eq!(cfg.auth.refresh_token, Some("refresh-1".into()));
            assert_eq!(cfg.auth.id_token, Some("id-1".into()));
        });
    }

    #[test]
    fn load_falls_back_to_config_when_keychain_read_fails() {
        with_temp_home(|| {
            let tokens = StoredTokens {
                access_token: "access-2".into(),
                refresh_token: Some("refresh-2".into()),
                id_token: Some("id-2".into()),
            };
            save_stored_tokens_to_config(&tokens).unwrap();

            let loaded = load_stored_tokens_with_fallback(true, || {
                Err(anyhow!("simulated keychain read failure"))
            })
            .unwrap()
            .unwrap();

            assert_eq!(loaded.access_token, "access-2");
            assert_eq!(loaded.refresh_token.as_deref(), Some("refresh-2"));
            assert_eq!(loaded.id_token.as_deref(), Some("id-2"));
        });
    }

    #[test]
    fn migration_keeps_config_tokens_when_keychain_save_fails() {
        with_temp_home(|| {
            let tokens = StoredTokens {
                access_token: "access-3".into(),
                refresh_token: Some("refresh-3".into()),
                id_token: Some("id-3".into()),
            };
            save_stored_tokens_to_config(&tokens).unwrap();

            migrate_legacy_tokens_from_config_with(
                true,
                || Ok(None),
                |_| Err(anyhow!("simulated keychain migration failure")),
            )
            .unwrap();

            let cfg = crate::config::load().unwrap();
            assert_eq!(cfg.auth.access_token, Some("access-3".into()));
            assert_eq!(cfg.auth.refresh_token, Some("refresh-3".into()));
            assert_eq!(cfg.auth.id_token, Some("id-3".into()));
        });
    }
}

// ─── URL encoding helper ──────────────────────────────────────────────────────

mod urlencoding {
    pub fn encode(s: &str) -> String {
        url::form_urlencoded::byte_serialize(s.as_bytes()).collect()
    }
}

#[cfg(test)]
mod path_tests {
    use super::validate_api_get_path;

    // ── allowed paths ──────────────────────────────────────────────────────

    #[test]
    fn stats_overview_is_allowed() {
        assert!(validate_api_get_path("/v1/stats/overview"));
    }

    #[test]
    fn stats_by_band_is_allowed() {
        assert!(validate_api_get_path("/v1/stats/by-band"));
    }

    #[test]
    fn stats_by_mode_is_allowed() {
        assert!(validate_api_get_path("/v1/stats/by-mode"));
    }

    #[test]
    fn stats_by_period_with_query_is_allowed() {
        assert!(validate_api_get_path("/v1/stats/by-period?group=month"));
    }

    #[test]
    fn stats_countries_over_time_is_allowed() {
        assert!(validate_api_get_path("/v1/stats/countries-over-time"));
    }

    #[test]
    fn stats_top_callsigns_with_query_is_allowed() {
        assert!(validate_api_get_path("/v1/stats/top-callsigns?limit=20"));
    }

    #[test]
    fn stats_top_countries_with_query_is_allowed() {
        assert!(validate_api_get_path("/v1/stats/top-countries?limit=20"));
    }

    #[test]
    fn stats_operating_patterns_is_allowed() {
        assert!(validate_api_get_path("/v1/stats/operating-patterns"));
    }

    // ── disallowed paths ───────────────────────────────────────────────────

    #[test]
    fn old_logbooks_uuid_stats_path_is_rejected() {
        // The old stale endpoint shape must no longer be accepted.
        assert!(!validate_api_get_path(
            "/v1/logbooks/00000000-0000-0000-0000-000000000001/stats/overview"
        ));
    }

    #[test]
    fn logbooks_default_is_rejected() {
        // The logbook UUID lookup is no longer needed for stats.
        assert!(!validate_api_get_path("/v1/logbooks/default"));
    }

    #[test]
    fn traversal_attempt_is_rejected() {
        assert!(!validate_api_get_path("/v1/stats/../admin"));
    }

    #[test]
    fn percent_encoded_traversal_is_rejected() {
        assert!(!validate_api_get_path("/v1/stats/%2e%2e/admin"));
    }

    #[test]
    fn extra_nesting_is_rejected() {
        assert!(!validate_api_get_path("/v1/stats/overview/extra"));
    }

    #[test]
    fn bare_v1_stats_is_rejected() {
        // Requires a stat name after the prefix.
        assert!(!validate_api_get_path("/v1/stats/"));
    }

    #[test]
    fn unrelated_path_is_rejected() {
        assert!(!validate_api_get_path("/v1/qsos"));
    }
}
