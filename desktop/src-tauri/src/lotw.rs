//! LoTW bridge via local tQSL binary.
//!
//! Security model:
//! - tQSL credentials/certificates remain on the operator's machine.
//! - Only status metadata is sent to the RadioLedger API server.

use std::collections::HashMap;
use std::env;
use std::fs;
use std::path::{Path, PathBuf};
use std::process::Command;

use chrono::{DateTime, NaiveDate, NaiveTime, Utc};
use serde::{Deserialize, Serialize};
use tracing::{debug, info, warn};

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
struct CanonicalMode<'a> {
    mode: &'a str,
    submode: Option<&'a str>,
}

use crate::auth;
use crate::db::{LotwStatusSummary, PendingLotwQso};
use crate::error::AppError;
use crate::AppState;

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct TqslDetection {
    pub found: bool,
    pub path: Option<String>,
    pub candidates_checked: Vec<String>,
    pub source: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct LotwActionResult {
    pub ok: bool,
    pub message: String,
    pub submitted: usize,
    pub confirmed: usize,
    pub rejected: usize,
    pub details: Vec<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct LotwCertEntry {
    pub path: String,
    pub subject: Option<String>,
    pub expires_at: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct LotwCertInfo {
    pub tqsl_found: bool,
    pub tqsl_path: Option<String>,
    pub certs: Vec<LotwCertEntry>,
    pub cert_expiry_at: Option<String>,
    pub note: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct LotwStatus {
    pub tqsl_found: bool,
    pub tqsl_path: Option<String>,
    pub pending: i64,
    pub submitted: i64,
    pub confirmed: i64,
    pub rejected: i64,
    pub last_sync_at: Option<String>,
    pub last_error: Option<String>,
}

#[derive(Debug, Clone, Deserialize)]
struct ExportQso {
    callsign: String,
    band: String,
    mode: String,
    datetime_on: String,
    rst_sent: Option<String>,
    rst_rcvd: Option<String>,
    comments: Option<String>,
    grid: Option<String>,
}

#[tauri::command]
pub async fn detect_tqsl() -> Result<TqslDetection, AppError> {
    Ok(resolve_tqsl_path())
}

#[tauri::command]
pub async fn sign_and_upload(
    limit: Option<usize>,
    state: tauri::State<'_, AppState>,
) -> Result<LotwActionResult, AppError> {
    let detection = resolve_tqsl_path();
    let tqsl_path = detection
        .path
        .clone()
        .ok_or_else(|| AppError::Sync("tQSL executable not found".to_string()))?;

    let rows = state
        .db
        .pending_lotw_qsos(limit.unwrap_or(100))
        .map_err(|e| AppError::Sync(e.to_string()))?;

    if rows.is_empty() {
        return Ok(LotwActionResult {
            ok: true,
            message: "No synced QSOs pending LoTW upload".to_string(),
            submitted: 0,
            confirmed: 0,
            rejected: 0,
            details: vec![],
        });
    }

    let adif = build_adif(&rows)?;
    let temp = write_temp_adif(&adif).map_err(|e| AppError::Sync(e.to_string()))?;

    let cfg = crate::config::load().unwrap_or_default();
    let mut args = vec!["-x".to_string(), "-d".to_string()];
    if cfg.lotw.auto_upload {
        args.push("-u".to_string());
    }
    if let Some(station) = cfg.lotw.station_location.as_ref() {
        args.push("-l".to_string());
        args.push(station.clone());
    }
    args.push(temp.to_string_lossy().to_string());

    let output = run_command(&tqsl_path, &args)?;
    let stderr = String::from_utf8_lossy(&output.stderr).to_string();

    let mut rejected = 0usize;
    let mut submitted = 0usize;
    let mut details = vec![];

    if output.status.success() && !looks_like_tqsl_error(&stderr) {
        for row in &rows {
            state
                .db
                .set_lotw_status(&row.client_uuid, "submitted", None)
                .map_err(|e| AppError::Sync(e.to_string()))?;
        }
        submitted = rows.len();
        state
            .db
            .set_setting("lotw_last_sync_at", &Utc::now().to_rfc3339())
            .ok();
        state.db.set_setting("lotw_last_error", "").ok();
        details.push("tQSL signing/upload completed".to_string());
    } else {
        let msg = if stderr.trim().is_empty() {
            format!("tQSL failed with exit code {:?}", output.status.code())
        } else {
            stderr.clone()
        };
        for row in &rows {
            state
                .db
                .set_lotw_status(&row.client_uuid, "rejected", Some(&msg))
                .map_err(|e| AppError::Sync(e.to_string()))?;
            rejected += 1;
        }
        state.db.set_setting("lotw_last_error", &msg).ok();
        details.push(msg.clone());
    }

    let _ = fs::remove_file(&temp);

    post_lotw_sync_status(
        "upload",
        &cfg,
        submitted,
        0,
        rejected,
        details.last().cloned(),
    )
    .await;

    Ok(LotwActionResult {
        ok: rejected == 0,
        message: if rejected == 0 {
            format!("Submitted {submitted} QSO(s) to LoTW")
        } else {
            format!("LoTW submission failed for {rejected} QSO(s)")
        },
        submitted,
        confirmed: 0,
        rejected,
        details,
    })
}

#[tauri::command]
pub async fn check_confirmations(
    report_path: Option<String>,
    state: tauri::State<'_, AppState>,
) -> Result<LotwActionResult, AppError> {
    let cfg = crate::config::load().unwrap_or_default();
    let detection = resolve_tqsl_path();

    let path = if let Some(p) = report_path {
        PathBuf::from(p)
    } else if let Some(p) = cfg.lotw.confirmation_report_path.clone() {
        PathBuf::from(p)
    } else {
        // Optional download via configured args.
        if let Some(tqsl) = detection.path {
            if !cfg.lotw.confirmation_download_args.is_empty() {
                let out = temp_path("lotw-confirmations", "adi");
                let args: Vec<String> = cfg
                    .lotw
                    .confirmation_download_args
                    .iter()
                    .map(|a| a.replace("{output}", &out.to_string_lossy()))
                    .collect();
                let output = run_command(&tqsl, &args)?;
                let stderr = String::from_utf8_lossy(&output.stderr);
                if !output.status.success() || looks_like_tqsl_error(&stderr) {
                    return Err(AppError::Sync(format!(
                        "Failed to download LoTW confirmations via tQSL: {stderr}"
                    )));
                }
                out
            } else {
                return Err(AppError::Sync(
                    "No confirmation report path configured (lotw.confirmation_report_path) and no lotw.confirmation_download_args set"
                        .to_string(),
                ));
            }
        } else {
            return Err(AppError::Sync(
                "tQSL not found and no local confirmation ADIF path provided".to_string(),
            ));
        }
    };

    let adif = fs::read_to_string(&path)
        .map_err(|e| AppError::Sync(format!("Failed to read confirmation ADIF: {e}")))?;
    let records = parse_adif_records(&adif);

    let mut confirmation_keys = Vec::new();
    for rec in records {
        let lotw_rcvd = rec
            .get("LOTW_QSL_RCVD")
            .or_else(|| rec.get("QSL_RCVD"))
            .map(|v| v.eq_ignore_ascii_case("Y"))
            .unwrap_or(false);
        if !lotw_rcvd {
            continue;
        }

        if let Some(key) = adif_record_key(&rec) {
            confirmation_keys.push(key);
        }
    }

    let mut index = HashMap::<String, String>::new(); // key -> client_uuid
    for row in state
        .db
        .all_synced_qsos()
        .map_err(|e| AppError::Sync(e.to_string()))?
    {
        if let Ok(q) = serde_json::from_str::<ExportQso>(&row.data) {
            if let Some(k) = qso_key(&q) {
                index.insert(k, row.client_uuid.clone());
            }
        }
    }

    let mut confirmed = 0usize;
    for key in confirmation_keys {
        if let Some(uuid) = index.get(&key) {
            state
                .db
                .set_lotw_status(uuid, "confirmed", None)
                .map_err(|e| AppError::Sync(e.to_string()))?;
            confirmed += 1;
        }
    }

    if confirmed > 0 {
        state
            .db
            .set_setting("lotw_last_sync_at", &Utc::now().to_rfc3339())
            .ok();
    }

    post_lotw_sync_status("confirmations", &cfg, 0, confirmed, 0, None).await;

    Ok(LotwActionResult {
        ok: true,
        message: format!("Processed LoTW confirmations: {confirmed} matched"),
        submitted: 0,
        confirmed,
        rejected: 0,
        details: vec![format!("Source report: {}", path.display())],
    })
}

#[tauri::command]
pub async fn get_cert_info() -> Result<LotwCertInfo, AppError> {
    let detection = resolve_tqsl_path();
    let certs = read_cert_entries();
    let expiry = certs.iter().filter_map(|c| c.expires_at.clone()).min();

    let cfg = crate::config::load().unwrap_or_default();
    if let Some(exp) = expiry.clone() {
        post_lotw_sync_status("cert_expiry", &cfg, 0, 0, 0, Some(exp)).await;
    }

    Ok(LotwCertInfo {
        tqsl_found: detection.found,
        tqsl_path: detection.path,
        certs,
        cert_expiry_at: expiry,
        note: Some(
            "Certificate metadata is read locally; private keys never leave this machine"
                .to_string(),
        ),
    })
}

#[tauri::command]
pub async fn get_lotw_status(state: tauri::State<'_, AppState>) -> Result<LotwStatus, AppError> {
    let detection = resolve_tqsl_path();
    let summary: LotwStatusSummary = state
        .db
        .lotw_status_summary()
        .map_err(|e| AppError::Sync(e.to_string()))?;

    let last_sync_at = state
        .db
        .get_setting("lotw_last_sync_at")
        .ok()
        .flatten()
        .filter(|v| !v.is_empty());
    let last_error = state
        .db
        .get_setting("lotw_last_error")
        .ok()
        .flatten()
        .filter(|v| !v.is_empty());

    Ok(LotwStatus {
        tqsl_found: detection.found,
        tqsl_path: detection.path,
        pending: summary.pending,
        submitted: summary.submitted,
        confirmed: summary.confirmed,
        rejected: summary.rejected,
        last_sync_at,
        last_error,
    })
}

fn resolve_tqsl_path() -> TqslDetection {
    let cfg = crate::config::load().unwrap_or_default();
    let mut checked = Vec::new();

    if let Some(manual) = cfg.lotw.tqsl_path {
        checked.push(manual.clone());
        if is_executable(Path::new(&manual)) {
            return TqslDetection {
                found: true,
                path: Some(manual),
                candidates_checked: checked,
                source: Some("config".to_string()),
            };
        }
    }

    for c in platform_candidates() {
        checked.push(c.to_string_lossy().to_string());
        if is_executable(&c) {
            return TqslDetection {
                found: true,
                path: Some(c.to_string_lossy().to_string()),
                candidates_checked: checked,
                source: Some("platform".to_string()),
            };
        }
    }

    if let Some(path) = find_in_path("tqsl") {
        checked.push(path.to_string_lossy().to_string());
        return TqslDetection {
            found: true,
            path: Some(path.to_string_lossy().to_string()),
            candidates_checked: checked,
            source: Some("PATH".to_string()),
        };
    }

    TqslDetection {
        found: false,
        path: None,
        candidates_checked: checked,
        source: None,
    }
}

fn platform_candidates() -> Vec<PathBuf> {
    let mut out = Vec::new();
    #[cfg(target_os = "macos")]
    {
        out.push(PathBuf::from("/Applications/tqsl.app/Contents/MacOS/tqsl"));
        if let Some(home) = dirs::home_dir() {
            out.push(home.join("Applications/tqsl.app/Contents/MacOS/tqsl"));
        }
        out.push(PathBuf::from("/usr/local/bin/tqsl"));
    }
    #[cfg(target_os = "windows")]
    {
        out.push(PathBuf::from(r"C:\Program Files\TrustedQSL\tqsl.exe"));
        out.push(PathBuf::from(r"C:\Program Files (x86)\TrustedQSL\tqsl.exe"));
    }
    #[cfg(target_os = "linux")]
    {
        out.push(PathBuf::from("/usr/bin/tqsl"));
        out.push(PathBuf::from("/usr/local/bin/tqsl"));
    }
    out
}

fn is_executable(path: &Path) -> bool {
    path.exists() && path.is_file()
}

fn find_in_path(bin: &str) -> Option<PathBuf> {
    let path = env::var_os("PATH")?;
    for part in env::split_paths(&path) {
        let full = part.join(bin);
        if full.exists() {
            return Some(full);
        }
        #[cfg(target_os = "windows")]
        {
            let full_exe = part.join(format!("{bin}.exe"));
            if full_exe.exists() {
                return Some(full_exe);
            }
        }
    }
    None
}

fn build_adif(rows: &[PendingLotwQso]) -> Result<String, AppError> {
    let mut out = String::from("<ADIF_VER:5>3.1.4 <PROGRAMID:10>RadioLedger <EOH>\n");

    for row in rows {
        let q: ExportQso = serde_json::from_str(&row.data).map_err(|e| {
            AppError::Sync(format!("Invalid QSO JSON for {}: {e}", row.client_uuid))
        })?;

        let dt = DateTime::parse_from_rfc3339(&q.datetime_on)
            .map_err(|e| {
                AppError::Sync(format!("Invalid datetime_on for {}: {e}", row.client_uuid))
            })?
            .with_timezone(&Utc);

        let qso_date = dt.format("%Y%m%d").to_string();
        let time_on = dt.format("%H%M%S").to_string();

        append_field(&mut out, "CALL", &q.callsign);
        append_field(&mut out, "BAND", &q.band);
        let canonical_mode = canonicalize_mode_pair(&q.mode, None);
        append_field(&mut out, "MODE", canonical_mode.mode);
        if let Some(submode) = canonical_mode.submode {
            append_field(&mut out, "SUBMODE", submode);
        }
        append_field(&mut out, "QSO_DATE", &qso_date);
        append_field(&mut out, "TIME_ON", &time_on);

        if let Some(v) = q.rst_sent.as_ref().filter(|v| !v.is_empty()) {
            append_field(&mut out, "RST_SENT", v);
        }
        if let Some(v) = q.rst_rcvd.as_ref().filter(|v| !v.is_empty()) {
            append_field(&mut out, "RST_RCVD", v);
        }
        if let Some(v) = q.grid.as_ref().filter(|v| !v.is_empty()) {
            append_field(&mut out, "GRIDSQUARE", v);
        }
        if let Some(v) = q.comments.as_ref().filter(|v| !v.is_empty()) {
            append_field(&mut out, "COMMENT", v);
        }

        out.push_str("<EOR>\n");
    }

    Ok(out)
}

fn append_field(buf: &mut String, key: &str, value: &str) {
    buf.push_str(&format!("<{key}:{}>{}", value.len(), value));
}

fn canonicalize_mode_pair<'a>(mode: &'a str, submode: Option<&'a str>) -> CanonicalMode<'a> {
    let mode_trimmed = mode.trim();
    let submode_trimmed = submode.and_then(|s| {
        let trimmed = s.trim();
        if trimmed.is_empty() {
            None
        } else {
            Some(trimmed)
        }
    });

    let upper_mode = mode_trimmed.to_ascii_uppercase();
    let upper_submode = submode_trimmed.map(|s| s.to_ascii_uppercase());

    if let Some(submode_upper) = upper_submode.as_deref() {
        if let Some(canonical) = resolve_mode_alias(submode_upper) {
            if canonical.submode == Some(submode_upper) {
                let mode_upper = upper_mode.as_str();
                if canonical.mode == mode_upper || resolve_mode_alias(mode_upper).map(|m| m.mode) == Some(canonical.mode) {
                    return CanonicalMode {
                        mode: canonical.mode,
                        submode: canonical.submode,
                    };
                }
            }
        }
    }

    resolve_mode_alias(&upper_mode).unwrap_or(CanonicalMode {
        mode: mode_trimmed,
        submode: submode_trimmed,
    })
}

fn resolve_mode_alias(mode: &str) -> Option<CanonicalMode<'static>> {
    match mode {
        "USB" => Some(CanonicalMode {
            mode: "SSB",
            submode: Some("USB"),
        }),
        "LSB" => Some(CanonicalMode {
            mode: "SSB",
            submode: Some("LSB"),
        }),
        "DMR" => Some(CanonicalMode {
            mode: "DIGITALVOICE",
            submode: Some("DMR"),
        }),
        "C4FM" => Some(CanonicalMode {
            mode: "DIGITALVOICE",
            submode: Some("C4FM"),
        }),
        "DSTAR" => Some(CanonicalMode {
            mode: "DIGITALVOICE",
            submode: Some("DSTAR"),
        }),
        "FREEDV" => Some(CanonicalMode {
            mode: "DIGITALVOICE",
            submode: Some("FREEDV"),
        }),
        "M17" => Some(CanonicalMode {
            mode: "DIGITALVOICE",
            submode: Some("M17"),
        }),
        "FREEDATA" => Some(CanonicalMode {
            mode: "DYNAMIC",
            submode: Some("FREEDATA"),
        }),
        "VARA" | "VARAFH" | "VARAHF" => Some(CanonicalMode {
            mode: "DYNAMIC",
            submode: Some("VARA HF"),
        }),
        "VARAFM1200" => Some(CanonicalMode {
            mode: "DYNAMIC",
            submode: Some("VARA FM 1200"),
        }),
        "VARAFM9600" => Some(CanonicalMode {
            mode: "DYNAMIC",
            submode: Some("VARA FM 9600"),
        }),
        "VARASAT" | "VARASATELLITE" => Some(CanonicalMode {
            mode: "DYNAMIC",
            submode: Some("VARA SATELLITE"),
        }),
        "FT2" => Some(CanonicalMode {
            mode: "MFSK",
            submode: Some("FT2"),
        }),
        "FT4" => Some(CanonicalMode {
            mode: "MFSK",
            submode: Some("FT4"),
        }),
        "JS8" => Some(CanonicalMode {
            mode: "MFSK",
            submode: Some("JS8"),
        }),
        "Q65" => Some(CanonicalMode {
            mode: "MFSK",
            submode: Some("Q65"),
        }),
        "PACKET" => Some(CanonicalMode {
            mode: "PKT",
            submode: None,
        }),
        _ => None,
    }
}

fn write_temp_adif(content: &str) -> anyhow::Result<PathBuf> {
    let path = temp_path("radioledger-lotw", "adi");
    fs::write(&path, content)?;
    Ok(path)
}

fn temp_path(prefix: &str, ext: &str) -> PathBuf {
    let stamp = Utc::now().timestamp_millis();
    env::temp_dir().join(format!("{prefix}-{stamp}.{ext}"))
}

fn run_command(bin: &str, args: &[String]) -> Result<std::process::Output, AppError> {
    debug!("running command: {} {:?}", bin, args);
    Command::new(bin)
        .args(args)
        .output()
        .map_err(|e| AppError::Sync(format!("Failed to execute {bin}: {e}")))
}

fn looks_like_tqsl_error(stderr: &str) -> bool {
    let s = stderr.to_ascii_lowercase();
    s.contains("error") || s.contains("failed") || s.contains("invalid")
}

fn parse_adif_records(content: &str) -> Vec<HashMap<String, String>> {
    let lower = content.to_ascii_lowercase();
    let Some(eoh_idx) = lower.find("<eoh>") else {
        return Vec::new();
    };

    let mut records = Vec::new();
    // Split the body on <EOR> case-insensitively without altering the actual
    // record content (field values like callsigns must not be lowercased).
    //
    // Strategy: scan for <eor> positions case-insensitively, then slice the
    // original (unmodified) body between those positions.
    let body = &content[eoh_idx + 5..];
    let body_lower_for_split = body.to_ascii_lowercase();
    let eor_tag = "<eor>";
    let mut record_slices: Vec<&str> = Vec::new();
    let mut search_start = 0usize;
    loop {
        if let Some(rel) = body_lower_for_split[search_start..].find(eor_tag) {
            let eor_pos = search_start + rel;
            record_slices.push(&body[search_start..eor_pos]);
            search_start = eor_pos + eor_tag.len();
        } else {
            // Remainder after the last <EOR> (may be empty or trailing whitespace)
            record_slices.push(&body[search_start..]);
            break;
        }
    }

    for raw in record_slices {
        let mut rec = HashMap::new();
        let mut i = 0usize;
        let bytes = raw.as_bytes();

        while i < bytes.len() {
            if bytes[i] != b'<' {
                i += 1;
                continue;
            }
            let mut j = i + 1;
            while j < bytes.len() && bytes[j] != b'>' {
                j += 1;
            }
            if j >= bytes.len() {
                break;
            }

            let hdr = &raw[i + 1..j];
            let mut parts = hdr.split(':');
            let field = parts.next().unwrap_or("").trim().to_ascii_uppercase();
            let len: usize = parts
                .next()
                .and_then(|n| n.parse::<usize>().ok())
                .unwrap_or(0);

            let val_start = j + 1;
            let val_end = val_start.saturating_add(len);
            if val_end > raw.len() {
                break;
            }

            let val = raw[val_start..val_end].trim().to_string();
            if !field.is_empty() {
                rec.insert(field, val);
            }

            i = val_end;
        }

        if !rec.is_empty() {
            records.push(rec);
        }
    }

    records
}

fn adif_record_key(rec: &HashMap<String, String>) -> Option<String> {
    let call = rec.get("CALL")?.trim().to_uppercase();
    let band = rec.get("BAND")?.trim().to_uppercase();
    let mode = rec.get("MODE")?.trim().to_uppercase();
    let qso_date = rec.get("QSO_DATE")?.trim();
    let time_on = rec.get("TIME_ON")?.trim();

    let date = NaiveDate::parse_from_str(qso_date, "%Y%m%d").ok()?;
    let t = normalize_time(time_on)?;
    let dt = date.and_time(t);

    Some(format!(
        "{}|{}|{}|{}",
        call,
        band,
        mode,
        dt.format("%Y-%m-%dT%H:%M")
    ))
}

fn qso_key(q: &ExportQso) -> Option<String> {
    let dt = DateTime::parse_from_rfc3339(&q.datetime_on)
        .ok()?
        .with_timezone(&Utc);
    Some(format!(
        "{}|{}|{}|{}",
        q.callsign.trim().to_uppercase(),
        q.band.trim().to_uppercase(),
        q.mode.trim().to_uppercase(),
        dt.format("%Y-%m-%dT%H:%M")
    ))
}

fn normalize_time(s: &str) -> Option<NaiveTime> {
    match s.len() {
        4 => NaiveTime::parse_from_str(s, "%H%M").ok(),
        6 => NaiveTime::parse_from_str(s, "%H%M%S").ok(),
        _ => None,
    }
}

fn read_cert_entries() -> Vec<LotwCertEntry> {
    let mut files = Vec::new();
    for dir in cert_dirs() {
        if let Ok(read_dir) = fs::read_dir(dir) {
            for e in read_dir.flatten() {
                let p = e.path();
                let ext = p
                    .extension()
                    .and_then(|s| s.to_str())
                    .unwrap_or("")
                    .to_ascii_lowercase();
                if ["pem", "crt", "cer"].contains(&ext.as_str()) {
                    files.push(p);
                }
            }
        }
    }

    let mut out = Vec::new();
    for p in files {
        let subject = openssl_read(&["x509", "-noout", "-subject", "-in"], &p)
            .ok()
            .and_then(|s| s.lines().next().map(|l| l.trim().to_string()));

        let expires_at = openssl_read(&["x509", "-noout", "-enddate", "-in"], &p)
            .ok()
            .and_then(parse_openssl_enddate);

        out.push(LotwCertEntry {
            path: p.to_string_lossy().to_string(),
            subject,
            expires_at,
        });
    }

    out
}

fn cert_dirs() -> Vec<PathBuf> {
    let mut dirs_out = Vec::new();
    if let Some(home) = dirs::home_dir() {
        dirs_out.push(home.join(".tqsl"));
        #[cfg(target_os = "macos")]
        {
            dirs_out.push(home.join("Library/Application Support/TrustedQSL"));
        }
    }
    #[cfg(target_os = "windows")]
    {
        if let Some(appdata) = env::var_os("APPDATA") {
            dirs_out.push(PathBuf::from(appdata).join("TrustedQSL"));
        }
    }
    dirs_out
}

fn openssl_read(args_prefix: &[&str], file: &Path) -> anyhow::Result<String> {
    let mut args: Vec<String> = args_prefix.iter().map(|s| s.to_string()).collect();
    args.push(file.to_string_lossy().to_string());

    let output = Command::new("openssl").args(&args).output()?;
    if !output.status.success() {
        anyhow::bail!("openssl failed");
    }
    Ok(String::from_utf8_lossy(&output.stdout).to_string())
}

fn parse_openssl_enddate(raw: String) -> Option<String> {
    let line = raw.lines().next()?.trim();
    let v = line.strip_prefix("notAfter=")?.trim();
    // Example: Apr  3 12:00:00 2027 GMT
    let dt = DateTime::parse_from_str(v, "%b %e %H:%M:%S %Y %Z").ok()?;
    Some(dt.with_timezone(&Utc).to_rfc3339())
}

async fn post_lotw_sync_status(
    event: &str,
    cfg: &crate::config::Config,
    submitted: usize,
    confirmed: usize,
    rejected: usize,
    note: Option<String>,
) {
    let token = match auth::load_access_token() {
        Ok(Some(t)) => t,
        _ => return,
    };

    let endpoint = cfg.lotw.status_endpoint.trim();
    if endpoint.is_empty() {
        return;
    }

    let url = if endpoint.starts_with("http://") || endpoint.starts_with("https://") {
        endpoint.to_string()
    } else {
        format!(
            "{}/{}",
            cfg.server.url.trim_end_matches('/'),
            endpoint.trim_start_matches('/')
        )
    };

    let payload = serde_json::json!({
        "service": "lotw",
        "event": event,
        "submitted": submitted,
        "confirmed": confirmed,
        "rejected": rejected,
        "note": note,
        "timestamp": Utc::now().to_rfc3339(),
    });

    let client = reqwest::Client::new();
    match client
        .post(url)
        .bearer_auth(token)
        .json(&payload)
        .send()
        .await
    {
        Ok(resp) if resp.status().is_success() => {
            info!("Posted LoTW sync status to API server");
        }
        Ok(resp) => {
            warn!("LoTW status POST failed: HTTP {}", resp.status());
        }
        Err(e) => {
            warn!("LoTW status POST error: {e}");
        }
    }
}

// ─────────────────────────────────────────────────────────────────────────────
// LoTW certificate expiry push
//
// The desktop client reads certificate expiry dates locally and pushes only
// the expiry DATE to the server. Private keys and raw cert files never leave
// this machine.
// ─────────────────────────────────────────────────────────────────────────────

/// Response from POST /v1/desktop/cert-expiry.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct CertExpiryPushResult {
    pub ok: bool,
    pub message: String,
    /// Number of station locations updated on the server.
    pub locations_found: Option<i64>,
    pub pushed_certs: usize,
}

/// A single cert expiry record ready to push to the API.
#[derive(Debug, Clone, Serialize, Deserialize)]
struct CertExpiryRecord {
    pub station_callsign: String,
    pub location_name: String,
    pub expires_at: String,
}

/// push_cert_expiry reads local tQSL certificate files, extracts expiry dates,
/// and POSTs them to POST /v1/desktop/cert-expiry on the RadioLedger API server.
///
/// Security: only the expiry DATE and callsign are sent. Private keys never
/// leave this machine.
#[tauri::command]
pub async fn push_cert_expiry(
    _state: tauri::State<'_, AppState>,
) -> Result<CertExpiryPushResult, AppError> {
    let token = match crate::auth::load_access_token() {
        Ok(Some(t)) => t,
        Ok(None) => {
            return Ok(CertExpiryPushResult {
                ok: false,
                message: "Not authenticated — please log in first".to_string(),
                locations_found: None,
                pushed_certs: 0,
            });
        }
        Err(e) => {
            return Err(AppError::Sync(format!("Failed to load auth token: {e}")));
        }
    };

    let cfg = crate::config::load().unwrap_or_default();
    let base_url = cfg.server.url.trim_end_matches('/').to_string();
    let endpoint = format!("{base_url}/v1/desktop/cert-expiry");

    // Read all certificates (PEM, CRT, CER, and P12 with empty password).
    let certs = read_cert_entries_full();

    if certs.is_empty() {
        return Ok(CertExpiryPushResult {
            ok: true,
            message: "No tQSL certificates found in local cert directories".to_string(),
            locations_found: Some(0),
            pushed_certs: 0,
        });
    }

    let client = reqwest::Client::new();
    let mut pushed = 0usize;
    let mut last_locations_found = Some(0i64);
    let mut errors: Vec<String> = Vec::new();

    for cert in &certs {
        let expires_at = match &cert.expires_at {
            Some(e) => {
                // expires_at is RFC3339; extract the date part (YYYY-MM-DD).
                e.get(..10).unwrap_or(e.as_str()).to_string()
            }
            None => continue,
        };

        // Extract callsign from CN= in subject, or skip.
        let callsign = match extract_callsign_from_subject(cert.subject.as_deref()) {
            Some(c) => c,
            None => {
                warn!(
                    "push_cert_expiry: could not extract callsign from cert {:?}",
                    cert.path
                );
                continue;
            }
        };

        let payload = CertExpiryRecord {
            station_callsign: callsign,
            location_name: String::new(),
            expires_at,
        };

        match client
            .post(&endpoint)
            .bearer_auth(&token)
            .json(&payload)
            .send()
            .await
        {
            Ok(resp) if resp.status().is_success() => {
                // Try to parse locations_found from response.
                if let Ok(body) = resp.json::<serde_json::Value>().await {
                    if let Some(lf) = body
                        .get("data")
                        .and_then(|d| d.get("locations_found"))
                        .and_then(|v| v.as_i64())
                    {
                        last_locations_found = Some(lf);
                    }
                }
                pushed += 1;
                info!(
                    "push_cert_expiry: pushed expiry for {}",
                    payload.station_callsign
                );
            }
            Ok(resp) => {
                let status = resp.status();
                warn!("push_cert_expiry: server returned {status}");
                errors.push(format!(
                    "Server returned {status} for {}",
                    payload.station_callsign
                ));
            }
            Err(e) => {
                warn!("push_cert_expiry: request failed: {e}");
                errors.push(format!(
                    "Request failed for {}: {e}",
                    payload.station_callsign
                ));
            }
        }
    }

    if errors.is_empty() {
        Ok(CertExpiryPushResult {
            ok: true,
            message: format!("Pushed expiry dates for {pushed} certificate(s)"),
            locations_found: last_locations_found,
            pushed_certs: pushed,
        })
    } else {
        Ok(CertExpiryPushResult {
            ok: pushed > 0,
            message: format!(
                "Pushed {pushed}/{} certs; {} error(s): {}",
                certs.len(),
                errors.len(),
                errors.join("; ")
            ),
            locations_found: last_locations_found,
            pushed_certs: pushed,
        })
    }
}

/// read_cert_entries_full reads PEM/CRT/CER files (via openssl x509) and
/// also attempts to read .p12/.pfx files using openssl pkcs12 with an empty
/// passphrase (the most common case for ARRL LoTW certs that haven't been
/// passphrase-protected by the operator).
fn read_cert_entries_full() -> Vec<LotwCertEntry> {
    let mut files = Vec::new();
    for dir in cert_dirs() {
        if let Ok(read_dir) = fs::read_dir(dir) {
            for e in read_dir.flatten() {
                let p = e.path();
                let ext = p
                    .extension()
                    .and_then(|s| s.to_str())
                    .unwrap_or("")
                    .to_ascii_lowercase();
                if ["pem", "crt", "cer", "p12", "pfx"].contains(&ext.as_str()) {
                    files.push(p);
                }
            }
        }
    }

    let mut out = Vec::new();
    for p in files {
        let ext = p
            .extension()
            .and_then(|s| s.to_str())
            .unwrap_or("")
            .to_ascii_lowercase();

        if ext == "p12" || ext == "pfx" {
            // Try to read the certificate from a PKCS#12 bundle.
            // Most ARRL LoTW P12 exports are created without a passphrase
            // or with an empty passphrase. We attempt both.
            if let Some(entry) = read_p12_cert_entry(&p) {
                out.push(entry);
            }
        } else {
            // PEM / DER / CER — use the existing openssl x509 approach.
            let subject = openssl_read(&["x509", "-noout", "-subject", "-in"], &p)
                .ok()
                .and_then(|s| s.lines().next().map(|l| l.trim().to_string()));

            let expires_at = openssl_read(&["x509", "-noout", "-enddate", "-in"], &p)
                .ok()
                .and_then(parse_openssl_enddate);

            out.push(LotwCertEntry {
                path: p.to_string_lossy().to_string(),
                subject,
                expires_at,
            });
        }
    }

    out
}

/// read_p12_cert_entry attempts to extract certificate metadata from a PKCS#12
/// file using openssl pkcs12. Tries an empty passphrase first, then "password"
/// as a fallback (some tQSL exports use a literal "password" as the default).
///
/// The P12 is only read for the embedded certificate — the private key is
/// never extracted or transmitted.
fn read_p12_cert_entry(path: &Path) -> Option<LotwCertEntry> {
    // Try extracting the certificate (not the key) from the P12 bundle.
    // -nokeys ensures the private key is never loaded into memory.
    let pem = openssl_pkcs12_to_pem(path, "")
        .or_else(|_| openssl_pkcs12_to_pem(path, "password"))
        .ok()?;

    // Parse the extracted PEM with openssl x509.
    let subject = openssl_read_from_stdin(&["x509", "-noout", "-subject"], pem.as_bytes())
        .ok()
        .and_then(|s| s.lines().next().map(|l| l.trim().to_string()));

    let expires_at = openssl_read_from_stdin(&["x509", "-noout", "-enddate"], pem.as_bytes())
        .ok()
        .and_then(parse_openssl_enddate);

    Some(LotwCertEntry {
        path: path.to_string_lossy().to_string(),
        subject,
        expires_at,
    })
}

/// openssl_pkcs12_to_pem extracts the certificate chain (not the private key)
/// from a P12/PFX file and returns it as PEM text.
fn openssl_pkcs12_to_pem(path: &Path, passphrase: &str) -> anyhow::Result<String> {
    let output = Command::new("openssl")
        .args([
            "pkcs12",
            "-in",
            &path.to_string_lossy(),
            "-nokeys",
            "-clcerts",
            "-passin",
            &format!("pass:{passphrase}"),
        ])
        .output()?;

    if !output.status.success() {
        anyhow::bail!(
            "openssl pkcs12 failed: {}",
            String::from_utf8_lossy(&output.stderr)
        );
    }

    let pem = String::from_utf8_lossy(&output.stdout).to_string();
    if pem.trim().is_empty() {
        anyhow::bail!("openssl pkcs12 produced empty output");
    }
    Ok(pem)
}

/// openssl_read_from_stdin runs an openssl command, feeding `data` as stdin.
/// Used to pipe extracted PEM text through openssl x509 for parsing.
fn openssl_read_from_stdin(args: &[&str], data: &[u8]) -> anyhow::Result<String> {
    use std::io::Write;

    let mut child = Command::new("openssl")
        .args(args)
        .stdin(std::process::Stdio::piped())
        .stdout(std::process::Stdio::piped())
        .stderr(std::process::Stdio::piped())
        .spawn()?;

    if let Some(stdin) = child.stdin.take() {
        let mut stdin = stdin;
        stdin.write_all(data)?;
    }

    let output = child.wait_with_output()?;
    if !output.status.success() {
        anyhow::bail!(
            "openssl failed: {}",
            String::from_utf8_lossy(&output.stderr)
        );
    }
    Ok(String::from_utf8_lossy(&output.stdout).to_string())
}

/// extract_callsign_from_subject parses the callsign from a certificate subject string.
///
/// ARRL LoTW certificates have subjects like:
///   subject=C=US, O=ARRL, CN=W1ABC
///   subject= CN = W1ABC/HOME
///   subject=emailAddress=w1abc@example.com, CN=W1ABC
///
/// We extract the CN= field and strip any /LOCATION suffix.
fn extract_callsign_from_subject(subject: Option<&str>) -> Option<String> {
    let s = subject?;

    // Find CN= in the subject string.
    let cn_start = s
        .to_ascii_uppercase()
        .find("CN=")
        .or_else(|| s.to_ascii_uppercase().find("CN ="))?;

    let after_cn = &s[cn_start..];
    // Skip past "CN" and optional spaces and "=".
    let value_start = after_cn.find('=')? + 1;
    let value = after_cn[value_start..].trim();

    // Take up to the first comma (next attribute) or slash (location suffix).
    let end = value.find([',', '/']).unwrap_or(value.len());
    let callsign = value[..end].trim().to_ascii_uppercase();

    if callsign.is_empty() {
        None
    } else {
        Some(callsign)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn build_adif_canonicalizes_alias_modes() {
        let rows = vec![PendingLotwQso {
            id: 1,
            client_uuid: "abc".into(),
            data: serde_json::json!({
                "callsign": "W1AW",
                "band": "20m",
                "mode": "FT2",
                "datetime_on": "2024-01-02T03:04:05Z"
            })
            .to_string(),
        }];

        let adif = build_adif(&rows).expect("build adif");
        assert!(adif.contains("<MODE:4>MFSK"));
        assert!(adif.contains("<SUBMODE:3>FT2"));
        assert!(!adif.contains("<MODE:3>FT2"));
    }

    #[test]
    fn canonicalize_mode_pair_keeps_ft8_as_mode() {
        let canonical = canonicalize_mode_pair("FT8", None);
        assert_eq!(canonical.mode, "FT8");
        assert_eq!(canonical.submode, None);
    }

    #[test]
    fn canonicalize_mode_pair_maps_digitalvoice_and_ssb_aliases() {
        assert_eq!(
            canonicalize_mode_pair("DMR", None),
            CanonicalMode {
                mode: "DIGITALVOICE",
                submode: Some("DMR")
            }
        );
        assert_eq!(
            canonicalize_mode_pair("USB", None),
            CanonicalMode {
                mode: "SSB",
                submode: Some("USB")
            }
        );
    }
}
