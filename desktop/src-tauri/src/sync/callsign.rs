//! Callsign lookup and WSJT-X payload hydration.
//!
//! Provides the `lookup_callsign` Tauri command for on-demand callsign queries,
//! plus the internal hydration pipeline that enriches WSJT-X QSO payloads with
//! callsign-lookup data (name, grid, DXCC, etc.) before they are pushed to the
//! server.

use anyhow::Context;
use serde::{Deserialize, Serialize};
use tracing::{debug, warn};

use crate::auth;
use crate::error::AppError;

// ─── Data structures ─────────────────────────────────────────────────────────

/// Result of a callsign lookup against the server (FCC ULS / QRZ etc.).
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct CallsignLookupResult {
    pub callsign: String,
    pub full_name: Option<String>,
    pub grid: Option<String>,
    pub dxcc: Option<i32>,
    pub country: Option<String>,
    pub state: Option<String>,
    pub cq_zone: Option<i32>,
    pub itu_zone: Option<i32>,
    pub source: Option<String>,
}

// ─── JSON payload helpers ─────────────────────────────────────────────────────

/// Check whether `map[key]` is a non-empty trimmed string.
pub(crate) fn has_non_empty_string(
    map: &serde_json::Map<String, serde_json::Value>,
    key: &str,
) -> bool {
    map.get(key)
        .and_then(|value| value.as_str())
        .map(str::trim)
        .is_some_and(|value| !value.is_empty())
}

/// Merge callsign-lookup data into an existing QSO payload, filling in fields
/// that are missing or empty.  Existing values are never overwritten.
pub(crate) fn merge_callsign_lookup_into_payload(
    payload: &mut serde_json::Map<String, serde_json::Value>,
    lookup: &CallsignLookupResult,
) {
    if !has_non_empty_string(payload, "name") {
        if let Some(name) = lookup
            .full_name
            .as_deref()
            .map(str::trim)
            .filter(|value| !value.is_empty())
        {
            payload.insert("name".into(), serde_json::Value::String(name.to_string()));
        }
    }

    if !has_non_empty_string(payload, "gridsquare") {
        if let Some(grid) = lookup
            .grid
            .as_deref()
            .map(str::trim)
            .filter(|value| !value.is_empty())
        {
            payload.insert(
                "gridsquare".into(),
                serde_json::Value::String(grid.to_uppercase()),
            );
        }
    }

    if !payload.contains_key("dxcc") {
        if let Some(dxcc) = lookup.dxcc {
            payload.insert(
                "dxcc".into(),
                serde_json::Value::Number(serde_json::Number::from(dxcc)),
            );
        }
    }

    if !has_non_empty_string(payload, "country") {
        if let Some(country) = lookup
            .country
            .as_deref()
            .map(str::trim)
            .filter(|value| !value.is_empty())
        {
            payload.insert(
                "country".into(),
                serde_json::Value::String(country.to_string()),
            );
        }
    }

    if !payload.contains_key("cq_zone") {
        if let Some(cq_zone) = lookup.cq_zone {
            payload.insert(
                "cq_zone".into(),
                serde_json::Value::Number(serde_json::Number::from(cq_zone)),
            );
        }
    }

    if !payload.contains_key("itu_zone") {
        if let Some(itu_zone) = lookup.itu_zone {
            payload.insert(
                "itu_zone".into(),
                serde_json::Value::Number(serde_json::Number::from(itu_zone)),
            );
        }
    }

    if !has_non_empty_string(payload, "qth") {
        let mut parts = Vec::new();
        if let Some(state) = lookup
            .state
            .as_deref()
            .map(str::trim)
            .filter(|value| !value.is_empty())
        {
            parts.push(state.to_string());
        }
        if let Some(country) = lookup
            .country
            .as_deref()
            .map(str::trim)
            .filter(|value| !value.is_empty())
        {
            parts.push(country.to_string());
        }
        if !parts.is_empty() {
            payload.insert("qth".into(), serde_json::Value::String(parts.join(", ")));
        }
    }
}

// ─── WSJT-X hydration ────────────────────────────────────────────────────────

/// Enrich a WSJT-X QSO payload with callsign-lookup data before pushing.
///
/// If the QSO source is not `"wsjtx"`, this is a no-op.  If the lookup fails
/// the QSO is still synced (just without enrichment); a warning is logged.
pub async fn hydrate_wsjtx_payload_if_needed(
    client: &reqwest::Client,
    api_base: &str,
    access_token: &str,
    source: &str,
    qso_label: &str,
    data: &mut serde_json::Value,
) {
    if source != "wsjtx" {
        return;
    }

    let Some(payload) = data.as_object_mut() else {
        return;
    };

    let callsign = payload
        .get("callsign")
        .and_then(|value| value.as_str())
        .unwrap_or_default();

    match lookup_callsign_for_sync(client, api_base, access_token, callsign).await {
        Ok(Some(lookup)) => merge_callsign_lookup_into_payload(payload, &lookup),
        Ok(None) => debug!("WSJT-X QSO {qso_label} had no callsign hydration data available"),
        Err(err) => warn!("WSJT-X QSO {qso_label} callsign hydration skipped: {err}"),
    }
}

// ─── Internal lookup (used by sync push + WSJT-X hydration) ──────────────────

/// Perform a callsign lookup against the server API.
///
/// Returns `Ok(None)` when the server returns a non-success envelope (e.g. the
/// callsign is not in any database).  Network / auth errors propagate as `Err`.
pub(crate) async fn lookup_callsign_for_sync(
    client: &reqwest::Client,
    api_base: &str,
    access_token: &str,
    callsign: &str,
) -> anyhow::Result<Option<CallsignLookupResult>> {
    let normalized_callsign = callsign.trim().to_uppercase();
    if normalized_callsign.is_empty() {
        return Ok(None);
    }

    let url = format!(
        "{}/v1/callsign/{}",
        api_base.trim_end_matches('/'),
        normalized_callsign
    );

    let resp = client
        .get(&url)
        .bearer_auth(access_token)
        .send()
        .await
        .with_context(|| format!("callsign lookup failed for {normalized_callsign}"))?;

    if !resp.status().is_success() {
        anyhow::bail!("callsign lookup returned {}", resp.status());
    }

    let envelope: serde_json::Value = resp.json().await.with_context(|| {
        format!("failed to parse callsign lookup response for {normalized_callsign}")
    })?;

    if !envelope["success"].as_bool().unwrap_or(false) {
        return Ok(None);
    }

    let data = &envelope["data"];
    Ok(Some(CallsignLookupResult {
        callsign: data["callsign"]
            .as_str()
            .unwrap_or(&normalized_callsign)
            .to_string(),
        full_name: data["full_name"].as_str().map(String::from),
        grid: data["grid"].as_str().map(String::from),
        dxcc: data["dxcc"].as_i64().map(|n| n as i32),
        country: data["country"].as_str().map(String::from),
        state: data["state_province"]
            .as_str()
            .or_else(|| data["state"].as_str())
            .map(String::from),
        cq_zone: data["cq_zone"].as_i64().map(|n| n as i32),
        itu_zone: data["itu_zone"].as_i64().map(|n| n as i32),
        source: data["source"].as_str().map(String::from),
    }))
}

// ─── Tauri command ────────────────────────────────────────────────────────────

/// On-demand callsign lookup (e.g. from the QSO entry form).
#[tauri::command]
pub async fn lookup_callsign(callsign: String) -> Result<CallsignLookupResult, AppError> {
    let cfg = crate::config::load().map_err(|e| AppError::Config(e.to_string()))?;
    let token = auth::load_access_token()
        .map_err(|e| AppError::Auth(e.to_string()))?
        .ok_or_else(|| AppError::Auth("Not logged in".into()))?;

    let normalized_callsign = callsign.trim().to_uppercase();
    // Use the public FCC ULS database endpoint first (has all imported callsigns).
    // Falls back gracefully if not found.
    let url = format!("{}/v1/callsign/{}", cfg.server.url, normalized_callsign);

    let client = reqwest::Client::builder()
        .timeout(std::time::Duration::from_secs(10))
        .build()
        .map_err(|e| AppError::Network(e.to_string()))?;

    let resp = client
        .get(&url)
        .bearer_auth(&token)
        .send()
        .await
        .map_err(|e| AppError::Network(format!("Lookup failed: {e}")))?;

    if !resp.status().is_success() {
        return Err(AppError::Network(format!(
            "Lookup returned {}",
            resp.status()
        )));
    }

    let envelope: serde_json::Value = resp
        .json()
        .await
        .map_err(|e| AppError::Network(format!("Failed to parse response: {e}")))?;

    if !envelope["success"].as_bool().unwrap_or(false) {
        return Ok(CallsignLookupResult {
            callsign: normalized_callsign,
            full_name: None,
            grid: None,
            dxcc: None,
            country: None,
            state: None,
            cq_zone: None,
            itu_zone: None,
            source: None,
        });
    }

    let data = &envelope["data"];
    Ok(CallsignLookupResult {
        callsign: data["callsign"]
            .as_str()
            .unwrap_or(&normalized_callsign)
            .to_string(),
        full_name: data["full_name"].as_str().map(String::from),
        grid: data["grid"].as_str().map(String::from),
        dxcc: data["dxcc"].as_i64().map(|n| n as i32),
        country: data["country"].as_str().map(String::from),
        // FCC endpoint uses "state_province", QRZ lookup uses "state"
        state: data["state_province"]
            .as_str()
            .or_else(|| data["state"].as_str())
            .map(String::from),
        cq_zone: data["cq_zone"].as_i64().map(|n| n as i32),
        itu_zone: data["itu_zone"].as_i64().map(|n| n as i32),
        source: data["source"].as_str().map(String::from),
    })
}

// ─── Tests ────────────────────────────────────────────────────────────────────

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn merge_callsign_lookup_into_payload_backfills_missing_wsjtx_fields() {
        let mut payload = serde_json::Map::from_iter([
            ("callsign".into(), json!("K1ABC")),
            ("band".into(), json!("20m")),
            ("mode".into(), json!("FT8")),
            ("datetime_on".into(), json!("2026-04-09T17:05:00Z")),
        ]);
        let lookup = CallsignLookupResult {
            callsign: "K1ABC".into(),
            full_name: Some("Jane Example".into()),
            grid: Some("fn31".into()),
            dxcc: Some(291),
            country: Some("United States".into()),
            state: Some("Connecticut".into()),
            cq_zone: Some(5),
            itu_zone: Some(8),
            source: Some("fcc_uls".into()),
        };

        merge_callsign_lookup_into_payload(&mut payload, &lookup);

        assert_eq!(
            payload.get("name").and_then(|v| v.as_str()),
            Some("Jane Example")
        );
        assert_eq!(
            payload.get("qth").and_then(|v| v.as_str()),
            Some("Connecticut, United States")
        );
        assert_eq!(
            payload.get("gridsquare").and_then(|v| v.as_str()),
            Some("FN31")
        );
        assert_eq!(payload.get("dxcc").and_then(|v| v.as_i64()), Some(291));
        assert_eq!(
            payload.get("country").and_then(|v| v.as_str()),
            Some("United States")
        );
        assert_eq!(payload.get("cq_zone").and_then(|v| v.as_i64()), Some(5));
        assert_eq!(payload.get("itu_zone").and_then(|v| v.as_i64()), Some(8));
    }

    #[test]
    fn merge_callsign_lookup_into_payload_preserves_existing_values() {
        let mut payload = serde_json::Map::from_iter([
            ("callsign".into(), json!("K1ABC")),
            ("name".into(), json!("Already Set")),
            ("qth".into(), json!("Austin, United States")),
            ("gridsquare".into(), json!("EM10")),
            ("country".into(), json!("United States")),
            ("dxcc".into(), json!(291)),
        ]);
        let lookup = CallsignLookupResult {
            callsign: "K1ABC".into(),
            full_name: Some("Jane Example".into()),
            grid: Some("fn31".into()),
            dxcc: Some(230),
            country: Some("Germany".into()),
            state: Some("Hessen".into()),
            cq_zone: Some(14),
            itu_zone: Some(28),
            source: Some("fcc_uls".into()),
        };

        merge_callsign_lookup_into_payload(&mut payload, &lookup);

        assert_eq!(
            payload.get("name").and_then(|v| v.as_str()),
            Some("Already Set")
        );
        assert_eq!(
            payload.get("qth").and_then(|v| v.as_str()),
            Some("Austin, United States")
        );
        assert_eq!(
            payload.get("gridsquare").and_then(|v| v.as_str()),
            Some("EM10")
        );
        assert_eq!(
            payload.get("country").and_then(|v| v.as_str()),
            Some("United States")
        );
        assert_eq!(payload.get("dxcc").and_then(|v| v.as_i64()), Some(291));
        assert_eq!(payload.get("cq_zone").and_then(|v| v.as_i64()), Some(14));
        assert_eq!(payload.get("itu_zone").and_then(|v| v.as_i64()), Some(28));
    }

    #[test]
    fn has_non_empty_string_detects_present_text() {
        let mut map = serde_json::Map::new();
        map.insert("name".into(), json!("  Alice  "));
        map.insert("empty".into(), json!("   "));
        map.insert("null".into(), serde_json::Value::Null);
        map.insert("number".into(), json!(42));

        assert!(has_non_empty_string(&map, "name"));
        assert!(!has_non_empty_string(&map, "empty"));
        assert!(!has_non_empty_string(&map, "null"));
        assert!(!has_non_empty_string(&map, "number"));
        assert!(!has_non_empty_string(&map, "absent"));
    }
}