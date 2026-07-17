//! N1MM+ UDP broadcast listener and XML parser.
//!
//! N1MM+ broadcasts XML messages on UDP (default port 12060).
//! Unlike WSJT-X which uses a binary protocol, N1MM+ sends plain UTF-8 XML
//! in each datagram — one XML document per UDP packet.
//!
//! ## Supported message types
//! - `<contactinfo>` — QSO logged event → parsed into `QsoLogged` and enqueued
//! - `<RadioInfo>` — rig telemetry → parsed and logged at debug level
//!
//! ## Frequency units
//! N1MM+ sends frequencies in units of 10 Hz.  Multiply by 10 to get Hz.
//! Example: `1407400` → 14,074,000 Hz = 14.074 MHz.
//!
//! ## Security
//! - Socket bound to loopback by default (same policy as WSJT-X listener).
//! - Rate limit: more than 10 QSO-logged messages per second are dropped.

use std::sync::{
    atomic::{AtomicBool, AtomicU64, Ordering},
    Arc,
};
use std::time::{Duration, Instant};
use tokio::net::UdpSocket;
use tracing::{debug, error, info, warn};

use crate::db::Database;
use crate::udp::{freq_to_band, validate_and_enqueue, QsoLogged};

const RATE_LIMIT_PER_SEC: u64 = 10;

// ─── XML helpers ──────────────────────────────────────────────────────────────

/// Return the lowercased name of the root XML element.
///
/// Handles an optional `<?xml … ?>` declaration before the root tag.
pub(crate) fn root_element_name(xml: &str) -> String {
    let trimmed = xml.trim();
    // Skip XML processing instruction / declaration.
    let content = if trimmed.starts_with("<?") {
        match trimmed.find("?>") {
            Some(idx) => trimmed[idx + 2..].trim_start(),
            None => trimmed,
        }
    } else {
        trimmed
    };

    if let Some(stripped) = content.strip_prefix('<') {
        let end = stripped
            .find(|c: char| c.is_whitespace() || c == '>' || c == '/')
            .unwrap_or(stripped.len());
        return stripped[..end].to_lowercase();
    }
    String::new()
}

/// Extract the trimmed text content of the first matching XML element.
///
/// Tag matching is **case-insensitive**.  Does not handle nested elements with
/// the same tag name or element attributes — sufficient for the flat N1MM+ XML.
pub(crate) fn extract_tag(xml: &str, tag: &str) -> Option<String> {
    // We lowercase both the haystack and the needle for comparison, but extract
    // the content from the original string (preserving case, e.g. callsigns).
    let xml_low = xml.to_lowercase();
    let tag_low = tag.to_lowercase();
    let open = format!("<{}>", tag_low);
    let close = format!("</{}>", tag_low);

    let start = xml_low.find(&open)? + open.len();
    let rest_low = &xml_low[start..];
    let end = rest_low.find(&close)?;

    Some(xml[start..start + end].trim().to_string())
}

// ─── Timestamp ────────────────────────────────────────────────────────────────

/// Convert an N1MM+ timestamp to ISO 8601 UTC.
///
/// Input format:  `"2024-07-04 14:30:00"` (space-separated, no timezone)
/// Output format: `"2024-07-04T14:30:00Z"`
pub(crate) fn parse_n1mm_timestamp(ts: &str) -> String {
    let ts = ts.trim();
    if ts.len() >= 19 && ts.as_bytes().get(10) == Some(&b' ') {
        return format!("{}T{}Z", &ts[..10], &ts[11..19]);
    }
    if ts.is_empty() {
        "1970-01-01T00:00:00Z".to_string()
    } else {
        ts.to_string()
    }
}

// ─── Band mapping ─────────────────────────────────────────────────────────────

/// Map an N1MM+ band string to a RadioLedger band label.
///
/// N1MM+ uses plain numeric strings: `"20"` = 20 m, `"2"` = 2 m, etc.
pub(crate) fn n1mm_band_to_label(band: &str) -> String {
    match band.trim() {
        "160" => "160m",
        "80" => "80m",
        "60" => "60m",
        "40" => "40m",
        "30" => "30m",
        "20" => "20m",
        "17" => "17m",
        "15" => "15m",
        "12" => "12m",
        "10" => "10m",
        "6" => "6m",
        "4" => "4m",
        "2" => "2m",
        "70cm" | "70" => "70cm",
        other => {
            // Pass through if it already looks like a band label (e.g. "20m").
            if other.ends_with('m') {
                return other.to_string();
            }
            "unknown"
        }
    }
    .to_string()
}

// ─── contactinfo parser ───────────────────────────────────────────────────────

/// Parse an N1MM+ `<contactinfo>` XML datagram into a `QsoLogged`.
pub fn parse_contactinfo(xml: &str) -> anyhow::Result<QsoLogged> {
    // Mandatory: callsign
    let callsign_raw = extract_tag(xml, "call").unwrap_or_default();
    let callsign = callsign_raw.trim().to_uppercase();
    if callsign.is_empty() {
        anyhow::bail!("missing or empty <call> in N1MM+ contactinfo");
    }

    // Timestamp
    let ts_raw = extract_tag(xml, "timestamp").unwrap_or_default();
    let datetime_on = parse_n1mm_timestamp(&ts_raw);

    // Frequency — N1MM+ rxfreq is in units of 10 Hz.
    let rxfreq_raw = extract_tag(xml, "rxfreq").unwrap_or_default();
    let rxfreq_units: u64 = rxfreq_raw.trim().parse().unwrap_or(0);
    let freq_hz = rxfreq_units * 10;
    let freq_mhz = freq_hz as f64 / 1_000_000.0;

    // Mode
    let mode = extract_tag(xml, "mode").unwrap_or_default();

    // Band: prefer <band> field, derive from frequency if absent / unrecognised.
    let band_raw = extract_tag(xml, "band").unwrap_or_default();
    let band = {
        let label = n1mm_band_to_label(&band_raw);
        if label == "unknown" {
            freq_to_band(freq_mhz)
        } else {
            label
        }
    };

    // RST
    let rst_sent = extract_tag(xml, "snt").unwrap_or_default();
    let rst_rcvd = extract_tag(xml, "rcv").unwrap_or_default();

    // Optional fields
    let grid = non_empty(extract_tag(xml, "gridsquare").unwrap_or_default());
    let my_call = non_empty(extract_tag(xml, "mycall").unwrap_or_default());
    let operator = non_empty(extract_tag(xml, "operator").unwrap_or_default());

    // exchange_sent: prefer <exchange1>; fall back to RST sent.
    let exchange_sent =
        non_empty(extract_tag(xml, "exchange1").unwrap_or_default())
            .or_else(|| non_empty(rst_sent.clone()));

    // exchange_rcvd: <nr> (contest serial number)
    let exchange_rcvd = non_empty(extract_tag(xml, "nr").unwrap_or_default());

    // comments: contest name
    let contestname = extract_tag(xml, "contestname").unwrap_or_default();
    let comments = non_empty(contestname.clone()).map(|c| format!("Contest: {c}"));

    Ok(QsoLogged {
        callsign,
        datetime_on,
        datetime_off: None,
        freq_mhz,
        mode,
        band,
        rst_sent,
        rst_rcvd,
        tx_power: None,
        comments,
        name: None,
        grid,
        operator,
        my_call,
        my_grid: None,
        exchange_sent,
        exchange_rcvd,
        adif_prop_mode: None,
        source: "n1mm".to_string(),
    })
}

// ─── RadioInfo parser ─────────────────────────────────────────────────────────

/// Parsed N1MM+ `<RadioInfo>` rig telemetry.
#[derive(Debug, Clone)]
pub struct RadioInfo {
    pub station_name: String,
    pub radio_nr: u32,
    pub freq_hz: u64,
    pub mode: String,
    pub op_call: String,
}

/// Parse an N1MM+ `<RadioInfo>` XML datagram.
pub fn parse_radioinfo(xml: &str) -> anyhow::Result<RadioInfo> {
    let station_name = extract_tag(xml, "StationName").unwrap_or_default();
    let radio_nr: u32 = extract_tag(xml, "RadioNr")
        .and_then(|s| s.trim().parse().ok())
        .unwrap_or(1);
    let freq_units: u64 = extract_tag(xml, "Freq")
        .and_then(|s| s.trim().parse().ok())
        .unwrap_or(0);
    let freq_hz = freq_units * 10;
    let mode = extract_tag(xml, "Mode").unwrap_or_default();
    let op_call = extract_tag(xml, "OpCall").unwrap_or_default();

    Ok(RadioInfo { station_name, radio_nr, freq_hz, mode, op_call })
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

#[inline]
fn non_empty(s: String) -> Option<String> {
    if s.is_empty() { None } else { Some(s) }
}

// ─── Receive loop ─────────────────────────────────────────────────────────────

/// Async receive loop for the N1MM+ UDP listener.
///
/// Reads UTF-8 XML datagrams, dispatches based on root element name:
/// - `contactinfo` → parse QSO, validate, enqueue
/// - `radioinfo`   → parse and log at debug level
/// - anything else → debug-log and ignore
pub(crate) async fn run_n1mm_listener(
    socket: UdpSocket,
    db: Arc<Database>,
    listening_flag: Arc<AtomicBool>,
    counter: Arc<AtomicU64>,
    mut shutdown_rx: tokio::sync::oneshot::Receiver<()>,
) {
    let mut buf = [0u8; 65_536];
    let mut rl_window_start = Instant::now();
    let mut rl_window_qso_cnt: u64 = 0;

    loop {
        tokio::select! {
            _ = &mut shutdown_rx => {
                info!("N1MM+ UDP listener received shutdown signal");
                break;
            }
            result = socket.recv_from(&mut buf) => {
                match result {
                    Ok((len, src)) => {
                        counter.fetch_add(1, Ordering::Relaxed);

                        let xml = match std::str::from_utf8(&buf[..len]) {
                            Ok(s) => s,
                            Err(_) => {
                                warn!(
                                    "n1mm: non-UTF-8 datagram from {src} ({len} bytes) — ignored"
                                );
                                continue;
                            }
                        };

                        let root = root_element_name(xml);
                        debug!("n1mm: datagram from {src}: root=<{root}> ({len} bytes)");

                        match root.as_str() {
                            "contactinfo" => {
                                // Rate limiting
                                if rl_window_start.elapsed() >= Duration::from_secs(1) {
                                    rl_window_start = Instant::now();
                                    rl_window_qso_cnt = 0;
                                }
                                rl_window_qso_cnt += 1;
                                if rl_window_qso_cnt > RATE_LIMIT_PER_SEC {
                                    warn!(
                                        "n1mm: rate limit exceeded \
                                        ({} QSO msgs/s from {src}) — packet dropped",
                                        rl_window_qso_cnt
                                    );
                                    continue;
                                }

                                match parse_contactinfo(xml) {
                                    Ok(qso) => {
                                        if let Err(e) = validate_and_enqueue(&qso, &db) {
                                            warn!("n1mm: QSO enqueue failed: {e}");
                                        }
                                    }
                                    Err(e) => {
                                        warn!("n1mm: failed to parse contactinfo from {src}: {e}");
                                    }
                                }
                            }
                            "radioinfo" => {
                                match parse_radioinfo(xml) {
                                    Ok(ri) => {
                                        debug!(
                                            "n1mm: RadioInfo station={} radio={} \
                                            freq={}Hz mode={} op={}",
                                            ri.station_name,
                                            ri.radio_nr,
                                            ri.freq_hz,
                                            ri.mode,
                                            ri.op_call,
                                        );
                                    }
                                    Err(e) => {
                                        warn!("n1mm: failed to parse RadioInfo from {src}: {e}");
                                    }
                                }
                            }
                            other => {
                                debug!("n1mm: unknown XML root <{other}> from {src} — ignored");
                            }
                        }
                    }
                    Err(e) => error!("n1mm: UDP receive error: {e}"),
                }
            }
        }
    }

    listening_flag.store(false, Ordering::SeqCst);
    info!("N1MM+ UDP listener exited");
}

// ─── Tests ────────────────────────────────────────────────────────────────────

#[cfg(test)]
mod tests {
    use super::*;

    const SAMPLE_CONTACTINFO: &str = r#"<?xml version="1.0"?>
<contactinfo>
  <app>N1MM</app>
  <contestname>ARRL-DX-CW</contestname>
  <contestnr>1</contestnr>
  <timestamp>2024-07-04 14:30:00</timestamp>
  <mycall>K0ABC</mycall>
  <band>20</band>
  <rxfreq>1407400</rxfreq>
  <txfreq>1407400</txfreq>
  <operator>K0ABC</operator>
  <mode>CW</mode>
  <call>W1AW</call>
  <gridsquare>FN31</gridsquare>
  <exchange1>599</exchange1>
  <snt>599</snt>
  <rcv>599</rcv>
  <nr>42</nr>
  <points>3</points>
  <radionr>1</radionr>
  <StationName>Station1</StationName>
  <IsRunQSO>0</IsRunQSO>
  <NetBiosName>MYPC</NetBiosName>
</contactinfo>"#;

    const SAMPLE_RADIOINFO: &str = r#"<?xml version="1.0"?>
<RadioInfo>
  <app>N1MM</app>
  <StationName>Station1</StationName>
  <RadioNr>1</RadioNr>
  <Freq>1407400</Freq>
  <TXFreq>1407400</TXFreq>
  <Mode>USB</Mode>
  <OpCall>K0ABC</OpCall>
  <IsRunning>False</IsRunning>
  <FocusEntry>0</FocusEntry>
  <Antenna>1</Antenna>
  <Rotors></Rotors>
  <FocusRadioNr>1</FocusRadioNr>
  <IsStereo>False</IsStereo>
  <ActiveRadioNr>1</ActiveRadioNr>
</RadioInfo>"#;

    // ── parse_contactinfo — full fields ──────────────────────────────────────

    #[test]
    fn test_parse_contactinfo_all_fields() {
        let qso = parse_contactinfo(SAMPLE_CONTACTINFO).expect("parse failed");

        assert_eq!(qso.callsign, "W1AW");
        assert_eq!(qso.datetime_on, "2024-07-04T14:30:00Z");
        // 1407400 * 10 = 14_074_000 Hz = 14.074 MHz
        assert!((qso.freq_mhz - 14.074).abs() < 0.001, "freq_mhz={}", qso.freq_mhz);
        assert_eq!(qso.mode, "CW");
        assert_eq!(qso.band, "20m");
        assert_eq!(qso.rst_sent, "599");
        assert_eq!(qso.rst_rcvd, "599");
        assert_eq!(qso.grid, Some("FN31".to_string()));
        assert_eq!(qso.my_call, Some("K0ABC".to_string()));
        assert_eq!(qso.operator, Some("K0ABC".to_string()));
        assert_eq!(qso.exchange_sent, Some("599".to_string()));
        assert_eq!(qso.exchange_rcvd, Some("42".to_string()));
        assert_eq!(qso.comments, Some("Contest: ARRL-DX-CW".to_string()));
        assert_eq!(qso.source, "n1mm");
        assert_eq!(qso.datetime_off, None);
        assert_eq!(qso.tx_power, None);
        assert_eq!(qso.adif_prop_mode, None);
    }

    #[test]
    fn test_parse_contactinfo_callsign_uppercased() {
        let xml = r#"<contactinfo>
            <call>vk3abc</call>
            <timestamp>2024-01-01 00:00:00</timestamp>
            <rxfreq>700000</rxfreq>
            <mode>SSB</mode>
            <snt>59</snt><rcv>59</rcv>
        </contactinfo>"#;
        let qso = parse_contactinfo(xml).unwrap();
        assert_eq!(qso.callsign, "VK3ABC");
    }

    #[test]
    fn test_parse_contactinfo_missing_optional_fields() {
        let xml = r#"<contactinfo>
            <call>K1ABC</call>
            <timestamp>2024-06-15 10:00:00</timestamp>
            <rxfreq>1407400</rxfreq>
            <mode>FT8</mode>
            <snt>-12</snt>
            <rcv>-09</rcv>
        </contactinfo>"#;
        let qso = parse_contactinfo(xml).unwrap();
        assert_eq!(qso.callsign, "K1ABC");
        assert_eq!(qso.grid, None);
        assert_eq!(qso.my_call, None);
        assert_eq!(qso.operator, None);
        assert_eq!(qso.exchange_rcvd, None);
        assert_eq!(qso.comments, None);
        assert_eq!(qso.source, "n1mm");
    }

    #[test]
    fn test_exchange_sent_falls_back_to_snt() {
        // No <exchange1> field — exchange_sent should fall back to <snt>.
        let xml = r#"<contactinfo>
            <call>W1AW</call>
            <timestamp>2024-01-01 00:00:00</timestamp>
            <rxfreq>1407400</rxfreq>
            <mode>CW</mode>
            <snt>599</snt><rcv>599</rcv>
        </contactinfo>"#;
        let qso = parse_contactinfo(xml).unwrap();
        assert_eq!(qso.exchange_sent, Some("599".to_string()));
    }

    #[test]
    fn test_parse_contactinfo_empty_call_returns_error() {
        let xml = r#"<contactinfo>
            <call></call>
            <timestamp>2024-01-01 00:00:00</timestamp>
            <rxfreq>1407400</rxfreq>
            <mode>CW</mode>
            <snt>599</snt><rcv>599</rcv>
        </contactinfo>"#;
        assert!(parse_contactinfo(xml).is_err());
    }

    #[test]
    fn test_parse_contactinfo_missing_call_returns_error() {
        let xml = r#"<contactinfo>
            <timestamp>2024-01-01 00:00:00</timestamp>
            <rxfreq>1407400</rxfreq>
        </contactinfo>"#;
        assert!(parse_contactinfo(xml).is_err());
    }

    // ── Frequency unit conversion ────────────────────────────────────────────

    #[test]
    fn test_frequency_conversion_20m() {
        // 1407400 * 10 = 14_074_000 Hz = 14.074 MHz → 20m
        let xml = r#"<contactinfo>
            <call>W1AW</call>
            <timestamp>2024-01-01 00:00:00</timestamp>
            <rxfreq>1407400</rxfreq>
            <mode>CW</mode>
            <snt>599</snt><rcv>599</rcv>
        </contactinfo>"#;
        let qso = parse_contactinfo(xml).unwrap();
        assert!((qso.freq_mhz - 14.074).abs() < 0.001, "freq_mhz={}", qso.freq_mhz);
        assert_eq!(qso.band, "20m");
    }

    #[test]
    fn test_frequency_conversion_40m() {
        // 700000 * 10 = 7_000_000 Hz = 7.0 MHz → 40m
        let xml = r#"<contactinfo>
            <call>W1AW</call>
            <timestamp>2024-01-01 00:00:00</timestamp>
            <rxfreq>700000</rxfreq>
            <mode>SSB</mode>
            <snt>59</snt><rcv>59</rcv>
        </contactinfo>"#;
        let qso = parse_contactinfo(xml).unwrap();
        assert!((qso.freq_mhz - 7.0).abs() < 0.001, "freq_mhz={}", qso.freq_mhz);
        assert_eq!(qso.band, "40m");
    }

    #[test]
    fn test_frequency_zero() {
        let xml = r#"<contactinfo>
            <call>W1AW</call>
            <timestamp>2024-01-01 00:00:00</timestamp>
            <rxfreq>0</rxfreq>
            <mode>CW</mode>
            <snt>599</snt><rcv>599</rcv>
        </contactinfo>"#;
        let qso = parse_contactinfo(xml).unwrap();
        assert_eq!(qso.freq_mhz, 0.0);
        assert_eq!(qso.band, "unknown");
    }

    #[test]
    fn test_frequency_missing_defaults_to_zero() {
        let xml = r#"<contactinfo>
            <call>W1AW</call>
            <timestamp>2024-01-01 00:00:00</timestamp>
            <mode>CW</mode>
            <snt>599</snt><rcv>599</rcv>
        </contactinfo>"#;
        let qso = parse_contactinfo(xml).unwrap();
        assert_eq!(qso.freq_mhz, 0.0);
    }

    // ── Band string mapping ──────────────────────────────────────────────────

    #[test]
    fn test_band_mapping_standard_bands() {
        let cases: &[(&str, &str)] = &[
            ("160", "160m"),
            ("80", "80m"),
            ("60", "60m"),
            ("40", "40m"),
            ("30", "30m"),
            ("20", "20m"),
            ("17", "17m"),
            ("15", "15m"),
            ("12", "12m"),
            ("10", "10m"),
            ("6", "6m"),
            ("2", "2m"),
            ("70cm", "70cm"),
            ("70", "70cm"),
        ];
        for (input, expected) in cases {
            assert_eq!(&n1mm_band_to_label(input), expected, "band input '{input}'");
        }
    }

    #[test]
    fn test_band_mapping_unknown() {
        assert_eq!(n1mm_band_to_label("9999"), "unknown");
        assert_eq!(n1mm_band_to_label(""), "unknown");
    }

    #[test]
    fn test_band_label_passthrough() {
        // Already has 'm' suffix → returned as-is.
        assert_eq!(n1mm_band_to_label("20m"), "20m");
    }

    #[test]
    fn test_band_falls_back_to_freq_when_absent() {
        // No <band> in XML — derive from frequency.
        let xml = r#"<contactinfo>
            <call>W1AW</call>
            <timestamp>2024-01-01 00:00:00</timestamp>
            <rxfreq>1407400</rxfreq>
            <mode>CW</mode>
            <snt>599</snt><rcv>599</rcv>
        </contactinfo>"#;
        let qso = parse_contactinfo(xml).unwrap();
        assert_eq!(qso.band, "20m");
    }

    // ── Timestamp parsing ────────────────────────────────────────────────────

    #[test]
    fn test_timestamp_standard_format() {
        assert_eq!(parse_n1mm_timestamp("2024-07-04 14:30:00"), "2024-07-04T14:30:00Z");
        assert_eq!(parse_n1mm_timestamp("2000-01-01 00:00:00"), "2000-01-01T00:00:00Z");
        assert_eq!(parse_n1mm_timestamp("1999-12-31 23:59:59"), "1999-12-31T23:59:59Z");
    }

    #[test]
    fn test_timestamp_empty_returns_epoch() {
        assert_eq!(parse_n1mm_timestamp(""), "1970-01-01T00:00:00Z");
    }

    #[test]
    fn test_timestamp_whitespace_trimmed() {
        assert_eq!(
            parse_n1mm_timestamp("  2024-07-04 14:30:00  "),
            "2024-07-04T14:30:00Z"
        );
    }

    // ── Root element detection ───────────────────────────────────────────────

    #[test]
    fn test_root_element_contactinfo_with_declaration() {
        assert_eq!(root_element_name(SAMPLE_CONTACTINFO), "contactinfo");
    }

    #[test]
    fn test_root_element_radioinfo_with_declaration() {
        assert_eq!(root_element_name(SAMPLE_RADIOINFO), "radioinfo");
    }

    #[test]
    fn test_root_element_no_declaration() {
        let xml = "<contactinfo><call>W1AW</call></contactinfo>";
        assert_eq!(root_element_name(xml), "contactinfo");
    }

    #[test]
    fn test_root_element_case_insensitive_output() {
        // root_element_name always lowercases.
        assert_eq!(root_element_name("<RadioInfo></RadioInfo>"), "radioinfo");
        assert_eq!(root_element_name("<CONTACTINFO></CONTACTINFO>"), "contactinfo");
    }

    #[test]
    fn test_root_element_empty_string() {
        assert_eq!(root_element_name(""), "");
    }

    // ── Invalid XML handling ─────────────────────────────────────────────────

    #[test]
    fn test_invalid_xml_empty_string() {
        assert!(parse_contactinfo("").is_err());
    }

    #[test]
    fn test_invalid_xml_garbage_bytes() {
        assert!(parse_contactinfo("not xml at all").is_err());
    }

    #[test]
    fn test_invalid_xml_truncated() {
        // Truncated before the closing </call> tag — extract_tag returns None → error.
        assert!(parse_contactinfo("<contactinfo><call>W1AW").is_err());
    }

    // ── extract_tag ──────────────────────────────────────────────────────────

    #[test]
    fn test_extract_tag_case_insensitive() {
        let xml = "<RadioInfo><Freq>1407400</Freq></RadioInfo>";
        assert_eq!(extract_tag(xml, "freq"), Some("1407400".to_string()));
        assert_eq!(extract_tag(xml, "Freq"), Some("1407400".to_string()));
        assert_eq!(extract_tag(xml, "FREQ"), Some("1407400".to_string()));
    }

    #[test]
    fn test_extract_tag_trims_whitespace() {
        let xml = "<root>  hello world  </root>";
        assert_eq!(extract_tag(xml, "root"), Some("hello world".to_string()));
    }

    #[test]
    fn test_extract_tag_empty_element() {
        let xml = "<root><Rotors></Rotors></root>";
        assert_eq!(extract_tag(xml, "Rotors"), Some(String::new()));
    }

    #[test]
    fn test_extract_tag_missing_returns_none() {
        let xml = "<root><a>1</a></root>";
        assert_eq!(extract_tag(xml, "b"), None);
    }

    // ── RadioInfo parsing ────────────────────────────────────────────────────

    #[test]
    fn test_parse_radioinfo_all_fields() {
        let ri = parse_radioinfo(SAMPLE_RADIOINFO).expect("parse failed");
        assert_eq!(ri.station_name, "Station1");
        assert_eq!(ri.radio_nr, 1);
        assert_eq!(ri.freq_hz, 14_074_000); // 1407400 * 10
        assert_eq!(ri.mode, "USB");
        assert_eq!(ri.op_call, "K0ABC");
    }

    #[test]
    fn test_parse_radioinfo_minimal() {
        let xml = "<RadioInfo><Freq>700000</Freq><Mode>CW</Mode></RadioInfo>";
        let ri = parse_radioinfo(xml).unwrap();
        assert_eq!(ri.freq_hz, 7_000_000); // 700000 * 10
        assert_eq!(ri.mode, "CW");
        assert_eq!(ri.station_name, "");
        assert_eq!(ri.radio_nr, 1); // default
        assert_eq!(ri.op_call, "");
    }

    #[test]
    fn test_parse_radioinfo_80m() {
        let xml = "<RadioInfo><Freq>358000</Freq><Mode>LSB</Mode><OpCall>W1AW</OpCall></RadioInfo>";
        let ri = parse_radioinfo(xml).unwrap();
        assert_eq!(ri.freq_hz, 3_580_000);
        assert_eq!(ri.mode, "LSB");
        assert_eq!(ri.op_call, "W1AW");
    }
}
