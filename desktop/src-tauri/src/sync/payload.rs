//! Sync payload building.
//!
//! Transforms a raw queued-QSO JSON value into the normalized payload that is
//! sent to the RadioLedger API.  Handles field validation, case-normalisation
//! (callsign/mode → UPPER, band → as-is), desktop-meta flattening (frequency,
//! power), and explicit-null propagation so that the server can distinguish
//! "field absent" from "field cleared".

use anyhow::Context;

// ─── JSON field-copy helpers ─────────────────────────────────────────────────

/// Copy a string-valued field from `raw` into `map`, trimming whitespace,
/// skipping empty strings, and optionally uppercasing the result.
///
/// If the source value is `null`, the key is inserted as `null` in `map`
/// so that the server interprets it as an explicit clear.  If the key is
/// absent from `raw` or the value is a non-string/non-null type, nothing is
/// inserted.
pub(crate) fn copy_json_stringish_field(
    raw: &serde_json::Value,
    map: &mut serde_json::Map<String, serde_json::Value>,
    key: &str,
    uppercase: bool,
) {
    if let Some(value) = raw.get(key) {
        match value {
            serde_json::Value::Null => {
                map.insert(key.to_string(), serde_json::Value::Null);
            }
            serde_json::Value::String(text) => {
                let trimmed = text.trim();
                if !trimmed.is_empty() {
                    let normalized = if uppercase {
                        trimmed.to_uppercase()
                    } else {
                        trimmed.to_string()
                    };
                    map.insert(key.to_string(), serde_json::Value::String(normalized));
                }
            }
            _ => {}
        }
    }
}

/// Copy an integer-valued field from `raw` into `map`.
///
/// `null` → `null`; `Number` → copied verbatim; other types are coerced via
/// `as_i64()` if possible.  If the key is absent from `raw`, nothing is
/// inserted.
pub(crate) fn copy_json_i64_field(
    raw: &serde_json::Value,
    map: &mut serde_json::Map<String, serde_json::Value>,
    key: &str,
) {
    if let Some(value) = raw.get(key) {
        match value {
            serde_json::Value::Null => {
                map.insert(key.to_string(), serde_json::Value::Null);
            }
            serde_json::Value::Number(number) => {
                map.insert(key.to_string(), serde_json::Value::Number(number.clone()));
            }
            _ => {
                if let Some(number) = value.as_i64() {
                    map.insert(key.to_string(), serde_json::Value::Number(number.into()));
                }
            }
        }
    }
}

// ─── Build sync payload ──────────────────────────────────────────────────────

/// Transform a raw queued-QSO JSON blob into the normalised API payload.
///
/// Returns `(queued_qso_uuid, resolved_logbook_uuid, payload_value)`.
///
/// * `queued_qso_uuid` — the QSO's server-side UUID if it already exists
///   (enables PUT update); `None` for a new POST create.
/// * `resolved_logbook_uuid` — the logbook UUID, taken from the raw data if
///   present, otherwise falling back to the `logbook_uuid` argument.
/// * `payload_value` — the sanitised `serde_json::Value::Object` to send.
pub(crate) fn build_sync_payload(
    raw: &serde_json::Value,
    logbook_uuid: &str,
) -> anyhow::Result<(Option<String>, String, serde_json::Value)> {
    let queued_qso_uuid = raw
        .get("uuid")
        .and_then(|v| v.as_str())
        .map(str::trim)
        .filter(|s| !s.is_empty())
        .map(str::to_string);

    let callsign = raw["callsign"]
        .as_str()
        .map(str::trim)
        .filter(|s| !s.is_empty())
        .context("Queued QSO missing callsign")?
        .to_uppercase();
    let band = raw["band"]
        .as_str()
        .map(str::trim)
        .filter(|s| !s.is_empty())
        .context("Queued QSO missing band")?
        .to_string();
    let mode = raw["mode"]
        .as_str()
        .map(str::trim)
        .filter(|s| !s.is_empty())
        .context("Queued QSO missing mode")?
        .to_uppercase();
    let datetime_on = raw["datetime_on"]
        .as_str()
        .map(str::trim)
        .filter(|s| !s.is_empty())
        .context("Queued QSO missing datetime_on")?
        .to_string();

    let resolved_logbook_uuid = raw
        .get("logbook_uuid")
        .and_then(|v| v.as_str())
        .map(str::trim)
        .filter(|s| !s.is_empty())
        .unwrap_or(logbook_uuid)
        .to_string();

    let mut data = serde_json::Map::new();
    data.insert("callsign".into(), serde_json::Value::String(callsign));
    data.insert("band".into(), serde_json::Value::String(band));
    data.insert("mode".into(), serde_json::Value::String(mode));
    data.insert("datetime_on".into(), serde_json::Value::String(datetime_on));

    if let Some(name) = raw
        .get("name")
        .and_then(|v| v.as_str())
        .or_else(|| {
            raw.get("desktop_meta")
                .and_then(|v| v.get("name"))
                .and_then(|v| v.as_str())
        })
        .map(str::trim)
        .filter(|s| !s.is_empty())
    {
        data.insert("name".into(), serde_json::Value::String(name.to_string()));
    } else if raw.get("name").is_some() {
        data.insert("name".into(), serde_json::Value::Null);
    }
    if let Some(qth) = raw
        .get("qth")
        .and_then(|v| v.as_str())
        .or_else(|| {
            raw.get("desktop_meta")
                .and_then(|v| v.get("qth"))
                .and_then(|v| v.as_str())
        })
        .map(str::trim)
        .filter(|s| !s.is_empty())
    {
        data.insert("qth".into(), serde_json::Value::String(qth.to_string()));
    } else if raw.get("qth").is_some() {
        data.insert("qth".into(), serde_json::Value::Null);
    }
    if let Some(freq_hz) = raw.get("frequency_hz").and_then(|v| v.as_i64()) {
        data.insert(
            "frequency_hz".into(),
            serde_json::Value::Number(freq_hz.into()),
        );
    } else if let Some(freq_mhz) = raw
        .get("desktop_meta")
        .and_then(|v| v.get("frequency_mhz"))
        .and_then(|v| v.as_f64())
        .filter(|v| v.is_finite() && *v > 0.0)
    {
        data.insert(
            "frequency_hz".into(),
            serde_json::Value::Number(((freq_mhz * 1_000_000.0).round() as i64).into()),
        );
    }
    if let Some(tx_power) = raw.get("tx_power").and_then(|v| v.as_f64()) {
        if let Some(num) = serde_json::Number::from_f64(tx_power) {
            data.insert("tx_power".into(), serde_json::Value::Number(num));
        }
    } else if let Some(power_w) = raw
        .get("desktop_meta")
        .and_then(|v| v.get("power_w"))
        .and_then(|v| v.as_f64())
        .filter(|v| v.is_finite() && *v >= 0.0)
    {
        if let Some(num) = serde_json::Number::from_f64(power_w) {
            data.insert("tx_power".into(), serde_json::Value::Number(num));
        }
    }

    copy_json_stringish_field(raw, &mut data, "rst_sent", false);
    copy_json_stringish_field(raw, &mut data, "rst_rcvd", false);

    if let Some(grid) = raw
        .get("gridsquare")
        .and_then(|v| v.as_str())
        .or_else(|| raw.get("grid").and_then(|v| v.as_str()))
        .map(str::trim)
        .filter(|s| !s.is_empty())
    {
        data.insert(
            "gridsquare".into(),
            serde_json::Value::String(grid.to_uppercase()),
        );
    } else if raw.get("gridsquare").is_some() {
        data.insert("gridsquare".into(), serde_json::Value::Null);
    }

    copy_json_i64_field(raw, &mut data, "dxcc");
    copy_json_stringish_field(raw, &mut data, "country", false);
    copy_json_i64_field(raw, &mut data, "cq_zone");
    copy_json_i64_field(raw, &mut data, "itu_zone");
    copy_json_stringish_field(raw, &mut data, "continent", true);
    copy_json_stringish_field(raw, &mut data, "comment", false);
    copy_json_stringish_field(raw, &mut data, "notes", false);

    Ok((
        queued_qso_uuid,
        resolved_logbook_uuid,
        serde_json::Value::Object(data),
    ))
}

// ─── Tests ────────────────────────────────────────────────────────────────────

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn build_sync_payload_preserves_explicit_null_clears() {
        let raw = json!({
            "uuid": "qso-1",
            "logbook_uuid": "log-1",
            "callsign": "k1abc",
            "band": "20m",
            "mode": "ft8",
            "datetime_on": "2026-04-09T17:05:00Z",
            "name": null,
            "qth": null,
            "gridsquare": null,
            "dxcc": null,
            "country": null,
            "cq_zone": null,
            "itu_zone": null,
            "continent": null,
            "comment": null,
            "notes": "Fresh notes"
        });

        let (qso_uuid, logbook_uuid, payload) = build_sync_payload(&raw, "fallback-log").unwrap();
        assert_eq!(qso_uuid.as_deref(), Some("qso-1"));
        assert_eq!(logbook_uuid, "log-1");
        assert_eq!(payload["callsign"].as_str(), Some("K1ABC"));
        assert!(payload["name"].is_null());
        assert!(payload["qth"].is_null());
        assert!(payload["gridsquare"].is_null());
        assert!(payload["dxcc"].is_null());
        assert!(payload["country"].is_null());
        assert!(payload["cq_zone"].is_null());
        assert!(payload["itu_zone"].is_null());
        assert!(payload["continent"].is_null());
        assert!(payload["comment"].is_null());
        assert_eq!(payload["notes"].as_str(), Some("Fresh notes"));
    }

    #[test]
    fn build_sync_payload_requires_mandatory_fields() {
        let raw = json!({
            "callsign": "k1abc",
            // missing band, mode, datetime_on
        });
        assert!(build_sync_payload(&raw, "log-1").is_err());
    }

    #[test]
    fn build_sync_payload_uses_fallback_logbook_uuid() {
        let raw = json!({
            "callsign": "k1abc",
            "band": "20m",
            "mode": "ft8",
            "datetime_on": "2026-04-09T17:05:00Z",
        });
        let (_, logbook, _) = build_sync_payload(&raw, "fallback-log").unwrap();
        assert_eq!(logbook, "fallback-log");
    }

    #[test]
    fn build_sync_payload_prefers_raw_logbook_uuid_over_fallback() {
        let raw = json!({
            "callsign": "k1abc",
            "band": "20m",
            "mode": "ft8",
            "datetime_on": "2026-04-09T17:05:00Z",
            "logbook_uuid": "explicit-log",
        });
        let (_, logbook, _) = build_sync_payload(&raw, "fallback-log").unwrap();
        assert_eq!(logbook, "explicit-log");
    }

    #[test]
    fn build_sync_payload_normalizes_case() {
        let raw = json!({
            "callsign": "k1abc",
            "band": "20m",
            "mode": "ft8",
            "datetime_on": "2026-04-09T17:05:00Z",
            "continent": "na",
        });
        let (_, _, payload) = build_sync_payload(&raw, "log").unwrap();
        assert_eq!(payload["callsign"].as_str(), Some("K1ABC"));
        assert_eq!(payload["mode"].as_str(), Some("FT8"));
        assert_eq!(payload["continent"].as_str(), Some("NA"));
    }

    #[test]
    fn build_sync_payload_flattens_desktop_meta_frequency() {
        let raw = json!({
            "callsign": "k1abc",
            "band": "20m",
            "mode": "ft8",
            "datetime_on": "2026-04-09T17:05:00Z",
            "desktop_meta": {
                "frequency_mhz": 14.074,
                "power_w": 50.0,
                "name": "  Jane  ",
                "qth": "  Austin  ",
            },
        });
        let (_, _, payload) = build_sync_payload(&raw, "log").unwrap();
        assert_eq!(payload["frequency_hz"].as_i64(), Some(14_074_000));
        assert_eq!(payload["tx_power"].as_f64(), Some(50.0));
        assert_eq!(payload["name"].as_str(), Some("Jane"));
        assert_eq!(payload["qth"].as_str(), Some("Austin"));
    }

    #[test]
    fn build_sync_payload_prefers_top_level_frequency_hz() {
        let raw = json!({
            "callsign": "k1abc",
            "band": "20m",
            "mode": "ft8",
            "datetime_on": "2026-04-09T17:05:00Z",
            "frequency_hz": 21_074_000_i64,
            "desktop_meta": {
                "frequency_mhz": 14.074,
            },
        });
        let (_, _, payload) = build_sync_payload(&raw, "log").unwrap();
        // top-level frequency_hz wins; desktop_meta frequency_mhz is ignored
        assert_eq!(payload["frequency_hz"].as_i64(), Some(21_074_000));
    }

    #[test]
    fn copy_json_stringish_field_skips_empty_strings() {
        let raw = json!({"key": "   "});
        let mut map = serde_json::Map::new();
        copy_json_stringish_field(&raw, &mut map, "key", false);
        assert!(!map.contains_key("key"));
    }

    #[test]
    fn copy_json_stringish_field_propagates_null() {
        let raw = json!({"key": null});
        let mut map = serde_json::Map::new();
        copy_json_stringish_field(&raw, &mut map, "key", false);
        assert!(map.contains_key("key"));
        assert!(map["key"].is_null());
    }

    #[test]
    fn copy_json_i64_field_copies_number_and_null() {
        let raw = json!({"a": 42, "b": null, "c": "not-a-number"});
        let mut map = serde_json::Map::new();
        copy_json_i64_field(&raw, &mut map, "a");
        copy_json_i64_field(&raw, &mut map, "b");
        copy_json_i64_field(&raw, &mut map, "c");
        copy_json_i64_field(&raw, &mut map, "absent");
        assert_eq!(map["a"].as_i64(), Some(42));
        assert!(map["b"].is_null());
        assert!(!map.contains_key("c"));
        assert!(!map.contains_key("absent"));
    }
}