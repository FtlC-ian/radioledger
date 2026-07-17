use bytes::Buf;
use chrono::Utc;

use super::wsjtx_wire::{
    is_maidenhead_grid, maidenhead_distance_km, read_qdatetime, read_wsjtx_bool,
    read_wsjtx_i32, read_wsjtx_string, read_wsjtx_u32, RecentDecodeRecord,
    WsjtxStatusSnapshot,
};
use super::{freq_to_band, is_plausible_callsign, non_empty, QsoLogged};

#[derive(Debug, Clone)]
struct ParsedDecodeMessage {
    snr: Option<i32>,
    mode: Option<String>,
    message: String,
}

pub(crate) fn parse_status_message(mut data: &[u8]) -> anyhow::Result<WsjtxStatusSnapshot> {
    if data.remaining() < 8 {
        anyhow::bail!("status packet too short for dial frequency");
    }

    let frequency_hz = Some(data.get_u64());
    let mode = non_empty(read_wsjtx_string(&mut data)?).map(|mode| mode.to_uppercase());
    let _dx_call = read_wsjtx_string(&mut data).unwrap_or_default();
    let _report = read_wsjtx_string(&mut data).unwrap_or_default();
    let _tx_mode = read_wsjtx_string(&mut data).unwrap_or_default();
    let _tx_enabled = read_wsjtx_bool(&mut data).unwrap_or(false);
    let _transmitting = read_wsjtx_bool(&mut data).unwrap_or(false);
    let _decoding = read_wsjtx_bool(&mut data).unwrap_or(false);
    let _rx_df = read_wsjtx_u32(&mut data).unwrap_or_default();
    let _tx_df = read_wsjtx_u32(&mut data).unwrap_or_default();
    let de_call =
        non_empty(read_wsjtx_string(&mut data).unwrap_or_default()).map(|call| call.to_uppercase());
    let de_grid =
        non_empty(read_wsjtx_string(&mut data).unwrap_or_default()).map(|grid| grid.to_uppercase());

    Ok(WsjtxStatusSnapshot {
        band: frequency_hz.map(|hz| freq_to_band(hz as f64 / 1_000_000.0)),
        frequency_hz,
        mode,
        de_call,
        de_grid,
    })
}

pub(crate) fn parse_decode_message(
    mut data: &[u8],
    source: &str,
    status: &WsjtxStatusSnapshot,
) -> anyhow::Result<Option<RecentDecodeRecord>> {
    let _is_new = read_wsjtx_bool(&mut data)?;
    let _time_ms = read_wsjtx_u32(&mut data)?;
    let snr = read_wsjtx_i32(&mut data).ok();
    let _delta_time = super::wsjtx_wire::read_wsjtx_f64(&mut data).unwrap_or_default();
    let _delta_frequency = read_wsjtx_u32(&mut data).unwrap_or_default();
    let mode =
        non_empty(read_wsjtx_string(&mut data).unwrap_or_default()).map(|mode| mode.to_uppercase());
    let message = read_wsjtx_string(&mut data)?;
    let _low_confidence = read_wsjtx_bool(&mut data).unwrap_or(false);
    let _off_air = read_wsjtx_bool(&mut data).unwrap_or(false);

    let parsed = ParsedDecodeMessage { snr, mode, message };
    let logged_in_callsign = crate::auth::load_callsign().ok().flatten();
    let de_call_for_parsing = status.de_call.as_deref().or(logged_in_callsign.as_deref());
    let (callsign, grid) = extract_decode_callsign_and_grid(&parsed.message, de_call_for_parsing);

    let frequency_hz = status.frequency_hz;
    let mode = parsed.mode.or_else(|| status.mode.clone());
    let band = status
        .band
        .clone()
        .or_else(|| frequency_hz.map(|hz| freq_to_band(hz as f64 / 1_000_000.0)));
    let distance_km = match (status.de_grid.as_deref(), grid.as_deref()) {
        (Some(my_grid), Some(dx_grid)) => maidenhead_distance_km(my_grid, dx_grid),
        _ => None,
    };

    Ok(Some(RecentDecodeRecord {
        callsign,
        message: parsed.message,
        grid,
        distance_km,
        snr: parsed.snr,
        frequency_hz,
        mode,
        band,
        last_activity_at: Utc::now(),
        source: source.to_string(),
    }))
}

pub(crate) fn parse_qso_logged(mut data: &[u8], source: &str) -> anyhow::Result<QsoLogged> {
    let datetime_off = Some(read_qdatetime(&mut data)?);

    let callsign_raw = read_wsjtx_string(&mut data)?;
    let grid_raw = read_wsjtx_string(&mut data)?;

    if data.remaining() < 8 {
        anyhow::bail!("packet too short for tx_freq field");
    }
    let freq_hz = data.get_u64();
    let freq_mhz = freq_hz as f64 / 1_000_000.0;

    let mode = read_wsjtx_string(&mut data)?;
    let rst_sent = read_wsjtx_string(&mut data)?;
    let rst_rcvd = read_wsjtx_string(&mut data)?;
    let tx_power_s = read_wsjtx_string(&mut data)?;
    let comments_s = read_wsjtx_string(&mut data)?;
    let name_s = read_wsjtx_string(&mut data)?;

    let datetime_on = read_qdatetime(&mut data)?;

    let operator_s = read_wsjtx_string(&mut data).unwrap_or_default();
    let my_call_s = read_wsjtx_string(&mut data).unwrap_or_default();
    let my_grid_s = read_wsjtx_string(&mut data).unwrap_or_default();
    let exchange_sent_s = read_wsjtx_string(&mut data).unwrap_or_default();
    let exchange_rcvd_s = read_wsjtx_string(&mut data).unwrap_or_default();
    let adif_prop_s = read_wsjtx_string(&mut data).unwrap_or_default();

    let callsign = callsign_raw.trim().to_uppercase();
    let band = freq_to_band(freq_mhz);

    Ok(QsoLogged {
        callsign,
        datetime_on,
        datetime_off,
        freq_mhz,
        mode,
        band,
        rst_sent,
        rst_rcvd,
        tx_power: tx_power_s.trim().parse::<f64>().ok(),
        comments: non_empty(comments_s),
        name: non_empty(name_s),
        grid: non_empty(grid_raw),
        operator: non_empty(operator_s),
        my_call: non_empty(my_call_s),
        my_grid: non_empty(my_grid_s),
        exchange_sent: non_empty(exchange_sent_s),
        exchange_rcvd: non_empty(exchange_rcvd_s),
        adif_prop_mode: non_empty(adif_prop_s),
        source: source.to_string(),
    })
}

pub(crate) fn extract_decode_callsign_and_grid(
    message: &str,
    de_call: Option<&str>,
) -> (Option<String>, Option<String>) {
    let tokens: Vec<String> = message
        .split_whitespace()
        .map(normalize_decode_token)
        .filter(|token| !token.is_empty())
        .collect();
    if tokens.is_empty() {
        return (None, None);
    }

    let grid = tokens
        .iter()
        .find(|token| is_maidenhead_grid(token))
        .cloned();
    let mut calls: Vec<String> = tokens
        .iter()
        .filter(|token| is_plausible_callsign(token))
        .cloned()
        .collect();
    calls.dedup();

    let my_call = de_call.map(|call| call.trim().to_uppercase());
    if let Some(ref own_call) = my_call {
        if let Some(other) = calls.iter().find(|call| *call != own_call) {
            return (Some(other.clone()), grid);
        }
    }

    if matches!(
        tokens.first().map(String::as_str),
        Some("CQ" | "QRZ" | "DE")
    ) {
        if let Some(call) = calls.last() {
            return (Some(call.clone()), grid);
        }
    }

    let callsign = if calls.len() >= 2 {
        calls.last().cloned()
    } else {
        calls.first().cloned()
    };

    (callsign, grid)
}

fn normalize_decode_token(token: &str) -> String {
    token
        .trim_matches(|ch: char| !ch.is_ascii_alphanumeric() && ch != '/' && ch != '-')
        .to_uppercase()
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::test_support::with_temp_home;
    use crate::udp::freq_to_band;

    fn push_u32(v: &mut Vec<u8>, n: u32) {
        v.extend_from_slice(&n.to_be_bytes());
    }
    fn push_i32(v: &mut Vec<u8>, n: i32) {
        v.extend_from_slice(&n.to_be_bytes());
    }
    fn push_i64(v: &mut Vec<u8>, n: i64) {
        v.extend_from_slice(&n.to_be_bytes());
    }
    fn push_u64(v: &mut Vec<u8>, n: u64) {
        v.extend_from_slice(&n.to_be_bytes());
    }
    fn push_f64(v: &mut Vec<u8>, n: f64) {
        v.extend_from_slice(&n.to_be_bytes());
    }
    fn push_bool(v: &mut Vec<u8>, value: bool) {
        v.push(u8::from(value));
    }
    fn push_str(v: &mut Vec<u8>, s: &str) {
        push_u32(v, s.len() as u32);
        v.extend_from_slice(s.as_bytes());
    }
    fn push_null_str(v: &mut Vec<u8>) {
        push_u32(v, 0xFFFF_FFFF);
    }
    fn push_qdatetime(v: &mut Vec<u8>, jd: i64, ms: u32) {
        push_i64(v, jd);
        push_u32(v, ms);
        v.push(1);
    }

    #[allow(clippy::too_many_arguments)]
    fn build_qso_logged_payload(
        jd_off: i64,
        ms_off: u32,
        dx_call: &str,
        dx_grid: &str,
        freq_hz: u64,
        mode: &str,
        rst_sent: &str,
        rst_rcvd: &str,
        tx_power: &str,
        comments: &str,
        name: &str,
        jd_on: i64,
        ms_on: u32,
        operator: &str,
        my_call: &str,
        my_grid: &str,
        exchange_sent: &str,
        exchange_rcvd: &str,
        adif_prop: &str,
    ) -> Vec<u8> {
        let mut pkt = Vec::new();
        push_qdatetime(&mut pkt, jd_off, ms_off);
        push_str(&mut pkt, dx_call);
        push_str(&mut pkt, dx_grid);
        push_u64(&mut pkt, freq_hz);
        push_str(&mut pkt, mode);
        push_str(&mut pkt, rst_sent);
        push_str(&mut pkt, rst_rcvd);
        push_str(&mut pkt, tx_power);
        push_str(&mut pkt, comments);
        push_str(&mut pkt, name);
        push_qdatetime(&mut pkt, jd_on, ms_on);
        push_str(&mut pkt, operator);
        push_str(&mut pkt, my_call);
        push_str(&mut pkt, my_grid);
        push_str(&mut pkt, exchange_sent);
        push_str(&mut pkt, exchange_rcvd);
        push_str(&mut pkt, adif_prop);
        pkt
    }

    const JD_2024_07_04: i64 = 2_460_496;

    #[test]
    fn parse_valid_qso_logged_all_fields() {
        let ms_on = 52_200_000u32;
        let ms_off = 52_260_000u32;

        let qso = parse_qso_logged(
            &build_qso_logged_payload(
                JD_2024_07_04,
                ms_off,
                "W1AW",
                "FN31",
                14_074_000,
                "FT8",
                "-12",
                "-09",
                "100",
                "Nice contact",
                "Joe",
                JD_2024_07_04,
                ms_on,
                "K0ABC",
                "K0ABC",
                "EN34",
                "",
                "",
                "",
            ),
            "wsjtx",
        )
        .expect("parse failed");

        assert_eq!(qso.callsign, "W1AW");
        assert_eq!(qso.grid, Some("FN31".to_string()));
        assert!((qso.freq_mhz - 14.074).abs() < 0.001);
        assert_eq!(qso.mode, "FT8");
        assert_eq!(qso.rst_sent, "-12");
        assert_eq!(qso.rst_rcvd, "-09");
        assert_eq!(qso.tx_power, Some(100.0));
        assert_eq!(qso.comments, Some("Nice contact".to_string()));
        assert_eq!(qso.name, Some("Joe".to_string()));
        assert_eq!(qso.datetime_on, "2024-07-04T14:30:00Z");
        assert_eq!(qso.datetime_off, Some("2024-07-04T14:31:00Z".to_string()));
        assert_eq!(qso.operator, Some("K0ABC".to_string()));
        assert_eq!(qso.my_call, Some("K0ABC".to_string()));
        assert_eq!(qso.my_grid, Some("EN34".to_string()));
        assert_eq!(qso.exchange_sent, None);
        assert_eq!(qso.exchange_rcvd, None);
        assert_eq!(qso.adif_prop_mode, None);
        assert_eq!(qso.band, "20m");
        assert_eq!(qso.source, "wsjtx");
    }

    #[test]
    fn parse_js8call_source_label() {
        let qso = parse_qso_logged(
            &build_qso_logged_payload(
                JD_2024_07_04,
                0,
                "VK2ABC",
                "QF56",
                7_074_000,
                "JS8",
                "+03",
                "+01",
                "50",
                "",
                "",
                JD_2024_07_04,
                0,
                "",
                "W2XYZ",
                "FN20",
                "",
                "",
                "",
            ),
            "js8call",
        )
        .unwrap();

        assert_eq!(qso.source, "js8call");
        assert_eq!(qso.callsign, "VK2ABC");
        assert_eq!(qso.band, "40m");
    }

    #[test]
    fn parse_status_message_extracts_frequency_mode_and_station() {
        let mut payload = Vec::new();
        push_u64(&mut payload, 14_074_000);
        push_str(&mut payload, "FT8");
        push_str(&mut payload, "K1ABC");
        push_str(&mut payload, "-10");
        push_str(&mut payload, "FT8");
        push_bool(&mut payload, true);
        push_bool(&mut payload, false);
        push_bool(&mut payload, true);
        push_u32(&mut payload, 450);
        push_u32(&mut payload, 1200);
        push_str(&mut payload, "N0CALL");
        push_str(&mut payload, "EM10");

        let status = parse_status_message(&payload).expect("status should parse");
        assert_eq!(status.frequency_hz, Some(14_074_000));
        assert_eq!(status.mode.as_deref(), Some("FT8"));
        assert_eq!(status.band.as_deref(), Some("20m"));
        assert_eq!(status.de_call.as_deref(), Some("N0CALL"));
        assert_eq!(status.de_grid.as_deref(), Some("EM10"));
    }

    #[test]
    fn parse_decode_message_prefers_other_station_and_computes_distance() {
        let status = WsjtxStatusSnapshot {
            frequency_hz: Some(14_074_000),
            mode: Some("FT8".to_string()),
            band: Some("20m".to_string()),
            de_call: Some("N0CALL".to_string()),
            de_grid: Some("EM10".to_string()),
        };

        let mut payload = Vec::new();
        push_bool(&mut payload, true);
        push_u32(&mut payload, 1_000);
        push_i32(&mut payload, -12);
        push_f64(&mut payload, 0.2);
        push_u32(&mut payload, 512);
        push_str(&mut payload, "FT8");
        push_str(&mut payload, "N0CALL W1AW FN31");
        push_bool(&mut payload, false);
        push_bool(&mut payload, false);

        let decode = parse_decode_message(&payload, "wsjtx", &status)
            .expect("decode should parse")
            .expect("decode should be usable");

        assert_eq!(decode.callsign.as_deref(), Some("W1AW"));
        assert_eq!(decode.message, "N0CALL W1AW FN31");
        assert_eq!(decode.grid.as_deref(), Some("FN31"));
        assert_eq!(decode.mode.as_deref(), Some("FT8"));
        assert_eq!(decode.band.as_deref(), Some("20m"));
        assert_eq!(decode.frequency_hz, Some(14_074_000));
        assert_eq!(decode.snr, Some(-12));
        assert!(decode.distance_km.is_some());
    }

    #[test]
    fn extract_decode_callsign_and_grid_handles_cq_messages() {
        let (callsign, grid) = extract_decode_callsign_and_grid("CQ DX K1ABC FN31", Some("N0CALL"));
        assert_eq!(callsign.as_deref(), Some("K1ABC"));
        assert_eq!(grid.as_deref(), Some("FN31"));
    }

    #[test]
    fn parse_decode_message_falls_back_to_logged_in_callsign() {
        with_temp_home("radioledger-udp-test", || {
            let mut cfg = crate::config::load().unwrap();
            cfg.auth.callsign = Some("N0CALL".to_string());
            cfg.save().unwrap();

            let status = WsjtxStatusSnapshot {
                de_call: None,
                de_grid: Some("EN34".to_string()),
                frequency_hz: Some(14_074_000),
                mode: Some("FT8".to_string()),
                band: Some("20m".to_string()),
            };

            let mut payload = Vec::new();
            push_bool(&mut payload, true);
            push_u32(&mut payload, 123);
            push_i32(&mut payload, -12);
            push_f64(&mut payload, 0.2);
            push_u32(&mut payload, 512);
            push_str(&mut payload, "FT8");
            push_str(&mut payload, "N0CALL W1AW FN31");
            push_bool(&mut payload, false);
            push_bool(&mut payload, false);

            let decode = parse_decode_message(&payload, "wsjtx", &status)
                .expect("decode should parse")
                .expect("decode should be usable");

            assert_eq!(decode.callsign.as_deref(), Some("W1AW"));
            assert_eq!(decode.message, "N0CALL W1AW FN31");
            assert_eq!(decode.grid.as_deref(), Some("FN31"));
            assert!(decode.distance_km.is_some());
        });
    }

    #[test]
    fn parse_decode_message_keeps_raw_message_when_callsign_cannot_be_parsed() {
        let status = WsjtxStatusSnapshot {
            frequency_hz: Some(14_074_000),
            mode: Some("FT8".to_string()),
            band: Some("20m".to_string()),
            de_call: Some("N0CALL".to_string()),
            de_grid: Some("EM10".to_string()),
        };

        let mut payload = Vec::new();
        push_bool(&mut payload, true);
        push_u32(&mut payload, 1_000);
        push_i32(&mut payload, -7);
        push_f64(&mut payload, 0.1);
        push_u32(&mut payload, 200);
        push_str(&mut payload, "FT8");
        push_str(&mut payload, "HELLO WORLD");
        push_bool(&mut payload, false);
        push_bool(&mut payload, false);

        let decode = parse_decode_message(&payload, "wsjtx", &status)
            .expect("decode should parse")
            .expect("decode should still be kept");

        assert_eq!(decode.callsign, None);
        assert_eq!(decode.message, "HELLO WORLD");
        assert_eq!(decode.grid, None);
        assert_eq!(decode.mode.as_deref(), Some("FT8"));
        assert_eq!(decode.band.as_deref(), Some("20m"));
        assert_eq!(decode.frequency_hz, Some(14_074_000));
        assert_eq!(decode.snr, Some(-7));
        assert_eq!(decode.distance_km, None);
    }

    #[test]
    fn parse_null_strings_become_none() {
        let mut pkt = Vec::new();
        push_qdatetime(&mut pkt, JD_2024_07_04, 0);
        push_str(&mut pkt, "N0CALL");
        push_null_str(&mut pkt);
        push_u64(&mut pkt, 21_074_000u64);
        push_str(&mut pkt, "FT4");
        push_str(&mut pkt, "-05");
        push_str(&mut pkt, "-10");
        push_null_str(&mut pkt);
        push_null_str(&mut pkt);
        push_null_str(&mut pkt);
        push_qdatetime(&mut pkt, JD_2024_07_04, 0);
        push_null_str(&mut pkt);
        push_str(&mut pkt, "K1XYZ");
        push_str(&mut pkt, "FN42");
        push_null_str(&mut pkt);
        push_null_str(&mut pkt);
        push_null_str(&mut pkt);

        let qso = parse_qso_logged(&pkt, "wsjtx").unwrap();

        assert_eq!(qso.grid, None);
        assert_eq!(qso.tx_power, None);
        assert_eq!(qso.comments, None);
        assert_eq!(qso.name, None);
        assert_eq!(qso.operator, None);
        assert_eq!(qso.band, "15m");
    }

    #[test]
    fn callsign_normalised_to_uppercase() {
        let qso = parse_qso_logged(
            &build_qso_logged_payload(
                JD_2024_07_04,
                0,
                "vk3abc/p",
                "",
                7_074_000,
                "FT8",
                "-5",
                "-5",
                "",
                "",
                "",
                JD_2024_07_04,
                0,
                "",
                "",
                "",
                "",
                "",
                "",
            ),
            "wsjtx",
        )
        .unwrap();
        assert_eq!(qso.callsign, "VK3ABC/P");
    }

    #[test]
    fn exchange_fields_parsed() {
        let qso = parse_qso_logged(
            &build_qso_logged_payload(
                JD_2024_07_04,
                0,
                "K5Z",
                "EM20",
                14_074_000,
                "FT8",
                "-10",
                "-08",
                "",
                "",
                "",
                JD_2024_07_04,
                0,
                "",
                "W0X",
                "DN70",
                "001",
                "042",
                "GND",
            ),
            "wsjtx",
        )
        .unwrap();
        assert_eq!(qso.exchange_sent, Some("001".to_string()));
        assert_eq!(qso.exchange_rcvd, Some("042".to_string()));
        assert_eq!(qso.adif_prop_mode, Some("GND".to_string()));
    }

    #[test]
    fn freq_to_band_still_maps_wsjtx_ranges() {
        let cases: &[(f64, &str)] = &[
            (1.84, "160m"),
            (3.573, "80m"),
            (5.357, "60m"),
            (7.074, "40m"),
            (10.136, "30m"),
            (14.074, "20m"),
            (18.100, "17m"),
            (21.074, "15m"),
            (24.915, "12m"),
            (28.074, "10m"),
            (50.313, "6m"),
            (144.174, "2m"),
            (432.100, "70cm"),
            (1296.0, "unknown"),
        ];
        for (mhz, expected) in cases {
            assert_eq!(&freq_to_band(*mhz), expected, "freq {mhz} MHz");
        }
    }
}
