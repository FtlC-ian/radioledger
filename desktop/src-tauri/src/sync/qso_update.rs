//! QSO update preparation.
//!
//! Contains the logic for preparing a QSO update request: normalizing
//! user-supplied fields, detecting callsign changes (which clear stale
//! enrichment data), and building both the API request body and the
//! local-cache record.

use anyhow::Context;
use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};

use crate::db::CachedQso;

// ─── Request / response types ─────────────────────────────────────────────────

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct UpdateQsoRequest {
    pub uuid: String,
    pub logbook_uuid: String,
    pub callsign: String,
    pub band: String,
    pub mode: String,
    pub datetime_on: String,
    pub rst_sent: Option<String>,
    pub rst_rcvd: Option<String>,
    pub name: Option<String>,
    pub qth: Option<String>,
    pub gridsquare: Option<String>,
    pub comment: Option<String>,
    pub notes: Option<String>,
}

#[derive(Debug)]
pub(crate) struct PreparedUpdateQso {
    pub body: serde_json::Value,
    pub cached_qso: CachedQso,
}

// ─── Normalization helpers ────────────────────────────────────────────────────

fn normalize_optional_text(value: Option<String>) -> Option<String> {
    value
        .map(|v| v.trim().to_string())
        .filter(|v| !v.is_empty())
}

fn normalize_optional_upper(value: Option<String>) -> Option<String> {
    normalize_optional_text(value).map(|v| v.to_uppercase())
}

fn same_text(lhs: Option<&str>, rhs: Option<&str>) -> bool {
    lhs.map(str::trim).filter(|s| !s.is_empty()) == rhs.map(str::trim).filter(|s| !s.is_empty())
}

fn same_upper(lhs: Option<&str>, rhs: Option<&str>) -> bool {
    lhs.map(str::trim)
        .filter(|s| !s.is_empty())
        .map(str::to_uppercase)
        == rhs
            .map(str::trim)
            .filter(|s| !s.is_empty())
            .map(str::to_uppercase)
}

// ─── JSON map helpers ─────────────────────────────────────────────────────────

fn set_json_option(
    map: &mut serde_json::Map<String, serde_json::Value>,
    key: &str,
    value: Option<String>,
) {
    map.insert(
        key.to_string(),
        value
            .map(serde_json::Value::String)
            .unwrap_or(serde_json::Value::Null),
    );
}

fn set_json_i32_option(
    map: &mut serde_json::Map<String, serde_json::Value>,
    key: &str,
    value: Option<i32>,
) {
    map.insert(
        key.to_string(),
        value
            .map(|v| serde_json::Value::Number(serde_json::Number::from(v)))
            .unwrap_or(serde_json::Value::Null),
    );
}

// ─── Prepare update ───────────────────────────────────────────────────────────

pub(crate) fn prepare_update_qso(
    existing_qso: &CachedQso,
    request: UpdateQsoRequest,
    now: DateTime<Utc>,
) -> anyhow::Result<PreparedUpdateQso> {
    let qso_uuid = request.uuid.trim().to_string();
    let logbook_uuid = request.logbook_uuid.trim().to_string();
    let callsign = request.callsign.trim().to_uppercase();
    let band = request.band.trim().to_string();
    let mode = request.mode.trim().to_uppercase();

    if qso_uuid.is_empty() || logbook_uuid.is_empty() {
        anyhow::bail!("QSO UUID and logbook UUID are required");
    }
    if callsign.is_empty() || band.is_empty() || mode.is_empty() {
        anyhow::bail!("Callsign, band, and mode are required");
    }

    let parsed_datetime = DateTime::parse_from_rfc3339(&request.datetime_on)
        .context("Invalid QSO date/time")?
        .with_timezone(&Utc);

    let callsign_changed = existing_qso.callsign.trim().to_uppercase() != callsign;

    let rst_sent = normalize_optional_text(request.rst_sent);
    let rst_rcvd = normalize_optional_text(request.rst_rcvd);
    let notes = normalize_optional_text(request.notes);
    let comment = normalize_optional_text(request.comment);

    let mut name = normalize_optional_text(request.name);
    let mut qth = normalize_optional_text(request.qth);
    let mut gridsquare = normalize_optional_upper(request.gridsquare);

    if callsign_changed {
        if same_text(name.as_deref(), existing_qso.name.as_deref()) {
            name = None;
        }
        if same_text(qth.as_deref(), existing_qso.qth.as_deref()) {
            qth = None;
        }
        if same_upper(gridsquare.as_deref(), existing_qso.gridsquare.as_deref()) {
            gridsquare = None;
        }
    }

    let (dxcc, country, cq_zone, itu_zone, continent) = if callsign_changed {
        (None, None, None, None, None)
    } else {
        (
            existing_qso.dxcc,
            existing_qso.country.clone(),
            existing_qso.cq_zone,
            existing_qso.itu_zone,
            existing_qso.continent.clone(),
        )
    };

    let mut body = serde_json::Map::new();
    body.insert("uuid".into(), serde_json::Value::String(qso_uuid.clone()));
    body.insert(
        "logbook_uuid".into(),
        serde_json::Value::String(logbook_uuid.clone()),
    );
    body.insert(
        "callsign".into(),
        serde_json::Value::String(callsign.clone()),
    );
    body.insert("band".into(), serde_json::Value::String(band.clone()));
    body.insert("mode".into(), serde_json::Value::String(mode.clone()));
    body.insert(
        "datetime_on".into(),
        serde_json::Value::String(parsed_datetime.to_rfc3339()),
    );
    set_json_option(&mut body, "rst_sent", rst_sent.clone());
    set_json_option(&mut body, "rst_rcvd", rst_rcvd.clone());
    set_json_option(&mut body, "name", name.clone());
    set_json_option(&mut body, "qth", qth.clone());
    set_json_option(&mut body, "gridsquare", gridsquare.clone());
    set_json_i32_option(&mut body, "dxcc", dxcc);
    set_json_option(&mut body, "country", country.clone());
    set_json_i32_option(&mut body, "cq_zone", cq_zone);
    set_json_i32_option(&mut body, "itu_zone", itu_zone);
    set_json_option(&mut body, "continent", continent.clone());
    set_json_option(&mut body, "comment", comment.clone());
    set_json_option(&mut body, "notes", notes.clone());

    Ok(PreparedUpdateQso {
        body: serde_json::Value::Object(body),
        cached_qso: CachedQso {
            id: existing_qso.id,
            uuid: existing_qso.uuid.clone(),
            logbook_uuid: existing_qso.logbook_uuid.clone(),
            callsign,
            band,
            mode,
            datetime_on: parsed_datetime.to_rfc3339(),
            rst_sent,
            rst_rcvd,
            name,
            qth,
            gridsquare,
            dxcc,
            country,
            cq_zone,
            itu_zone,
            continent,
            comment,
            notes,
            created_at: existing_qso.created_at.clone(),
            updated_at: now.to_rfc3339(),
            synced_at: existing_qso.synced_at.clone(),
        },
    })
}

// ─── Tests ────────────────────────────────────────────────────────────────────

#[cfg(test)]
mod tests {
    use super::*;
    use chrono::Utc;

    #[test]
    fn prepare_update_qso_clears_stale_enrichment_when_callsign_changes() {
        let existing = CachedQso {
            id: 7,
            uuid: "qso-1".to_string(),
            logbook_uuid: "log-1".to_string(),
            callsign: "W1AW".to_string(),
            band: "20m".to_string(),
            mode: "FT8".to_string(),
            datetime_on: "2026-04-09T17:00:00Z".to_string(),
            rst_sent: Some("599".to_string()),
            rst_rcvd: Some("599".to_string()),
            name: Some("Old Name".to_string()),
            qth: Some("Old QTH".to_string()),
            gridsquare: Some("FN31".to_string()),
            dxcc: Some(291),
            country: Some("United States".to_string()),
            cq_zone: Some(5),
            itu_zone: Some(8),
            continent: Some("NA".to_string()),
            comment: Some("Old comment".to_string()),
            notes: Some("Old notes".to_string()),
            created_at: "2026-04-09T17:00:00Z".to_string(),
            updated_at: "2026-04-09T17:00:00Z".to_string(),
            synced_at: "2026-04-09T17:00:00Z".to_string(),
        };

        let prepared = prepare_update_qso(
            &existing,
            UpdateQsoRequest {
                uuid: "qso-1".to_string(),
                logbook_uuid: "log-1".to_string(),
                callsign: "K1ABC".to_string(),
                band: "20m".to_string(),
                mode: "FT8".to_string(),
                datetime_on: "2026-04-09T17:05:00Z".to_string(),
                rst_sent: Some("599".to_string()),
                rst_rcvd: Some("599".to_string()),
                name: Some("Old Name".to_string()),
                qth: Some("Old QTH".to_string()),
                gridsquare: Some("FN31".to_string()),
                comment: None,
                notes: Some("Fresh notes".to_string()),
            },
            Utc::now(),
        )
        .unwrap();

        assert_eq!(prepared.cached_qso.name, None);
        assert_eq!(prepared.cached_qso.qth, None);
        assert_eq!(prepared.cached_qso.gridsquare, None);
        assert_eq!(prepared.cached_qso.dxcc, None);
        assert_eq!(prepared.cached_qso.country, None);
        assert_eq!(prepared.cached_qso.cq_zone, None);
        assert_eq!(prepared.cached_qso.itu_zone, None);
        assert_eq!(prepared.cached_qso.continent, None);
        assert_eq!(prepared.cached_qso.notes.as_deref(), Some("Fresh notes"));

        assert!(prepared.body["name"].is_null());
        assert!(prepared.body["qth"].is_null());
        assert!(prepared.body["gridsquare"].is_null());
        assert!(prepared.body["dxcc"].is_null());
        assert!(prepared.body["country"].is_null());
        assert!(prepared.body["cq_zone"].is_null());
        assert!(prepared.body["itu_zone"].is_null());
        assert!(prepared.body["continent"].is_null());
        assert!(prepared.body["comment"].is_null());
        assert_eq!(prepared.body["notes"].as_str(), Some("Fresh notes"));
    }

    #[test]
    fn prepare_update_qso_preserves_enrichment_when_callsign_unchanged() {
        let existing = CachedQso {
            id: 7,
            uuid: "qso-1".to_string(),
            logbook_uuid: "log-1".to_string(),
            callsign: "W1AW".to_string(),
            band: "20m".to_string(),
            mode: "FT8".to_string(),
            datetime_on: "2026-04-09T17:00:00Z".to_string(),
            rst_sent: Some("599".to_string()),
            rst_rcvd: Some("599".to_string()),
            name: Some("Old Name".to_string()),
            qth: Some("Old QTH".to_string()),
            gridsquare: Some("FN31".to_string()),
            dxcc: Some(291),
            country: Some("United States".to_string()),
            cq_zone: Some(5),
            itu_zone: Some(8),
            continent: Some("NA".to_string()),
            comment: Some("Old comment".to_string()),
            notes: Some("Old notes".to_string()),
            created_at: "2026-04-09T17:00:00Z".to_string(),
            updated_at: "2026-04-09T17:00:00Z".to_string(),
            synced_at: "2026-04-09T17:00:00Z".to_string(),
        };

        // When callsign is unchanged, DXCC/country/zone enrichment is
        // carried over. Name/QTH/gridsquare in the request override the
        // existing values — None in the request means "clear the field".
        let prepared = prepare_update_qso(
            &existing,
            UpdateQsoRequest {
                uuid: "qso-1".to_string(),
                logbook_uuid: "log-1".to_string(),
                callsign: "W1AW".to_string(), // same callsign
                band: "40m".to_string(),
                mode: "CW".to_string(),
                datetime_on: "2026-04-09T18:00:00Z".to_string(),
                rst_sent: Some("599".to_string()),
                rst_rcvd: Some("569".to_string()),
                name: Some("New Name".to_string()),
                qth: Some("New QTH".to_string()),
                gridsquare: Some("EM10".to_string()),
                comment: None,
                notes: Some("Updated notes".to_string()),
            },
            Utc::now(),
        )
        .unwrap();

        // Enrichment carried over when callsign unchanged
        assert_eq!(prepared.cached_qso.dxcc, Some(291));
        assert_eq!(prepared.cached_qso.country, Some("United States".to_string()));
        assert_eq!(prepared.cached_qso.cq_zone, Some(5));
        assert_eq!(prepared.cached_qso.itu_zone, Some(8));
        assert_eq!(prepared.cached_qso.continent, Some("NA".to_string()));

        // New values from the request override old ones
        assert_eq!(prepared.cached_qso.name, Some("New Name".to_string()));
        assert_eq!(prepared.cached_qso.qth, Some("New QTH".to_string()));
        assert_eq!(prepared.cached_qso.gridsquare, Some("EM10".to_string()));
        assert_eq!(prepared.cached_qso.band, "40m");
        assert_eq!(prepared.cached_qso.mode, "CW");
        assert_eq!(prepared.cached_qso.notes.as_deref(), Some("Updated notes"));

        // Body carries the enrichment too
        assert_eq!(prepared.body["dxcc"].as_i64(), Some(291));
        assert_eq!(prepared.body["country"].as_str(), Some("United States"));
        assert_eq!(prepared.body["name"].as_str(), Some("New Name"));
    }

    #[test]
    fn prepare_update_qso_rejects_empty_required_fields() {
        let existing = CachedQso {
            id: 7,
            uuid: "qso-1".to_string(),
            logbook_uuid: "log-1".to_string(),
            callsign: "W1AW".to_string(),
            band: "20m".to_string(),
            mode: "FT8".to_string(),
            datetime_on: "2026-04-09T17:00:00Z".to_string(),
            rst_sent: None,
            rst_rcvd: None,
            name: None,
            qth: None,
            gridsquare: None,
            dxcc: None,
            country: None,
            cq_zone: None,
            itu_zone: None,
            continent: None,
            comment: None,
            notes: None,
            created_at: "2026-04-09T17:00:00Z".to_string(),
            updated_at: "2026-04-09T17:00:00Z".to_string(),
            synced_at: "2026-04-09T17:00:00Z".to_string(),
        };

        let result = prepare_update_qso(
            &existing,
            UpdateQsoRequest {
                uuid: "".to_string(),
                logbook_uuid: "log-1".to_string(),
                callsign: "K1ABC".to_string(),
                band: "20m".to_string(),
                mode: "FT8".to_string(),
                datetime_on: "2026-04-09T17:05:00Z".to_string(),
                rst_sent: None,
                rst_rcvd: None,
                name: None,
                qth: None,
                gridsquare: None,
                comment: None,
                notes: None,
            },
            Utc::now(),
        );
        assert!(result.is_err());
        assert!(result.unwrap_err().to_string().contains("required"));
    }

    #[test]
    fn normalize_helpers_trim_and_filter_empty() {
        assert_eq!(normalize_optional_text(Some("  hello  ".to_string())), Some("hello".to_string()));
        assert_eq!(normalize_optional_text(Some("   ".to_string())), None);
        assert_eq!(normalize_optional_text(None), None);
        assert_eq!(normalize_optional_upper(Some("  fn31pr  ".to_string())), Some("FN31PR".to_string()));
        assert_eq!(normalize_optional_upper(Some("  ".to_string())), None);
    }

    #[test]
    fn same_text_and_same_upper_compare_semantically() {
        assert!(same_text(Some("hello"), Some("hello")));
        assert!(same_text(Some("  hello  "), Some("hello")));
        assert!(same_text(None, Some("")));
        assert!(!same_text(Some("hello"), Some("world")));
        assert!(same_upper(Some("fn31"), Some("FN31")));
        assert!(same_upper(None, Some("")));
        assert!(!same_upper(Some("fn31"), Some("em10")));
    }
}