use bytes::Buf;
use chrono::{DateTime, Utc};

use super::MAX_STRING_LEN;

#[derive(Debug, Clone)]
pub(super) struct RecentDecodeRecord {
    pub(super) callsign: Option<String>,
    pub(super) message: String,
    pub(super) grid: Option<String>,
    pub(super) distance_km: Option<u32>,
    pub(super) snr: Option<i32>,
    pub(super) frequency_hz: Option<u64>,
    pub(super) mode: Option<String>,
    pub(super) band: Option<String>,
    pub(super) last_activity_at: DateTime<Utc>,
    pub(super) source: String,
}

#[derive(Debug, Clone, Default)]
pub(super) struct WsjtxStatusSnapshot {
    pub(super) frequency_hz: Option<u64>,
    pub(super) mode: Option<String>,
    pub(super) band: Option<String>,
    pub(super) de_call: Option<String>,
    pub(super) de_grid: Option<String>,
}

pub(super) fn push_wsjtx_string(buf: &mut Vec<u8>, value: &str) {
    buf.extend_from_slice(&(value.len() as u32).to_be_bytes());
    buf.extend_from_slice(value.as_bytes());
}

pub(super) fn read_wsjtx_bool(data: &mut &[u8]) -> anyhow::Result<bool> {
    if data.remaining() < 1 {
        anyhow::bail!("not enough bytes for bool field");
    }
    Ok(data.get_u8() != 0)
}

pub(super) fn read_wsjtx_u32(data: &mut &[u8]) -> anyhow::Result<u32> {
    if data.remaining() < 4 {
        anyhow::bail!("not enough bytes for u32 field");
    }
    Ok(data.get_u32())
}

pub(super) fn read_wsjtx_i32(data: &mut &[u8]) -> anyhow::Result<i32> {
    if data.remaining() < 4 {
        anyhow::bail!("not enough bytes for i32 field");
    }
    Ok(data.get_i32())
}

#[allow(dead_code)]
pub(super) fn read_wsjtx_f32(data: &mut &[u8]) -> anyhow::Result<f32> {
    if data.remaining() < 4 {
        anyhow::bail!("not enough bytes for f32 field");
    }
    Ok(data.get_f32())
}

pub(super) fn read_wsjtx_f64(data: &mut &[u8]) -> anyhow::Result<f64> {
    if data.remaining() < 8 {
        anyhow::bail!("not enough bytes for f64 field");
    }
    Ok(data.get_f64())
}

pub(super) fn is_maidenhead_grid(token: &str) -> bool {
    let upper = token.trim().to_uppercase();
    let bytes = upper.as_bytes();
    match bytes.len() {
        4 => {
            bytes[0].is_ascii_alphabetic()
                && bytes[1].is_ascii_alphabetic()
                && bytes[2].is_ascii_digit()
                && bytes[3].is_ascii_digit()
        }
        6 => {
            bytes[0].is_ascii_alphabetic()
                && bytes[1].is_ascii_alphabetic()
                && bytes[2].is_ascii_digit()
                && bytes[3].is_ascii_digit()
                && bytes[4].is_ascii_alphabetic()
                && bytes[5].is_ascii_alphabetic()
        }
        _ => false,
    }
}

pub(super) fn maidenhead_distance_km(from: &str, to: &str) -> Option<u32> {
    let (from_lat, from_lon) = maidenhead_to_lat_lon(from)?;
    let (to_lat, to_lon) = maidenhead_to_lat_lon(to)?;
    let radius_km = 6_371.0f64;
    let dlat = (to_lat - from_lat).to_radians();
    let dlon = (to_lon - from_lon).to_radians();
    let a = (dlat / 2.0).sin().powi(2)
        + from_lat.to_radians().cos() * to_lat.to_radians().cos() * (dlon / 2.0).sin().powi(2);
    let c = 2.0 * a.sqrt().atan2((1.0 - a).sqrt());
    Some((radius_km * c).round() as u32)
}

fn maidenhead_to_lat_lon(grid: &str) -> Option<(f64, f64)> {
    let upper = grid.trim().to_uppercase();
    let bytes = upper.as_bytes();
    if !(bytes.len() == 4 || bytes.len() == 6) {
        return None;
    }
    if !is_maidenhead_grid(&upper) {
        return None;
    }

    let mut lon = -180.0 + (bytes[0] - b'A') as f64 * 20.0;
    let mut lat = -90.0 + (bytes[1] - b'A') as f64 * 10.0;
    lon += (bytes[2] - b'0') as f64 * 2.0;
    lat += (bytes[3] - b'0') as f64;
    let mut lon_size = 2.0;
    let mut lat_size = 1.0;

    if bytes.len() == 6 {
        lon += (bytes[4] - b'A') as f64 * (5.0 / 60.0);
        lat += (bytes[5] - b'A') as f64 * (2.5 / 60.0);
        lon_size = 5.0 / 60.0;
        lat_size = 2.5 / 60.0;
    }

    Some((lat + lat_size / 2.0, lon + lon_size / 2.0))
}

/// Read a WSJT-X length-prefixed UTF-8 string.
/// Length 0xFFFF_FFFF is the Qt null-string sentinel and returns `""`.
pub(crate) fn read_wsjtx_string(data: &mut &[u8]) -> anyhow::Result<String> {
    if data.remaining() < 4 {
        anyhow::bail!("not enough bytes for string length field");
    }
    let raw_len = data.get_u32();
    if raw_len == 0xFFFF_FFFF {
        return Ok(String::new());
    }
    let len = raw_len as usize;
    if len > MAX_STRING_LEN {
        anyhow::bail!("string field length {len} exceeds maximum {MAX_STRING_LEN}");
    }
    if data.remaining() < len {
        anyhow::bail!(
            "string body: need {len} bytes but only {} remain",
            data.remaining()
        );
    }
    let s = std::str::from_utf8(&data[..len])
        .map(|s| s.to_string())
        .unwrap_or_default();
    data.advance(len);
    Ok(s)
}

/// Read a QDateTime value and return an ISO 8601 UTC string.
///
/// ```text
/// i64  Julian day (BE)
/// u32  milliseconds since midnight (BE)
/// u8   timespec: 0=local, 1=UTC, 2=offset, 3=timezone
/// i32  offset in seconds (only when timespec == 2)
/// ```
pub(crate) fn read_qdatetime(data: &mut &[u8]) -> anyhow::Result<String> {
    if data.remaining() < 13 {
        anyhow::bail!("packet too short for QDateTime (need >=13 bytes)");
    }
    let julian_day = data.get_i64();
    let ms = data.get_u32();
    let timespec = data.get_u8();

    if timespec == 2 {
        if data.remaining() < 4 {
            anyhow::bail!("QDateTime: timespec=2 but offset bytes missing");
        }
        let _offset_secs = data.get_i32();
    }

    Ok(julian_to_iso8601(julian_day as u64, ms))
}

/// Convert a Julian Day Number plus milliseconds-since-midnight to ISO 8601 UTC.
pub(crate) fn julian_to_iso8601(julian_day: u64, ms_since_midnight: u32) -> String {
    let l = julian_day + 68_569;
    let n = (4 * l) / 146_097;
    #[allow(clippy::manual_div_ceil)]
    let l = l - (146_097 * n + 3) / 4;
    let i = (4_000 * (l + 1)) / 1_461_001;
    let l = l - (1_461 * i) / 4 + 31;
    let j = (80 * l) / 2_447;
    let day = l - (2_447 * j) / 80;
    let l = j / 11;
    let month = j + 2 - 12 * l;
    let year = 100 * (n - 49) + i + l;

    let total_s = ms_since_midnight / 1_000;
    let hour = total_s / 3_600;
    let minute = (total_s % 3_600) / 60;
    let second = total_s % 60;

    format!("{year:04}-{month:02}-{day:02}T{hour:02}:{minute:02}:{second:02}Z")
}

#[cfg(test)]
mod tests {
    use super::*;

    fn push_u32(v: &mut Vec<u8>, n: u32) {
        v.extend_from_slice(&n.to_be_bytes());
    }
    fn push_i64(v: &mut Vec<u8>, n: i64) {
        v.extend_from_slice(&n.to_be_bytes());
    }
    fn push_qdatetime(v: &mut Vec<u8>, jd: i64, ms: u32) {
        push_i64(v, jd);
        push_u32(v, ms);
        v.push(1);
    }
    fn push_null_str(v: &mut Vec<u8>) {
        push_u32(v, 0xFFFF_FFFF);
    }
    fn push_str(v: &mut Vec<u8>, s: &str) {
        push_u32(v, s.len() as u32);
        v.extend_from_slice(s.as_bytes());
    }

    const JD_2024_07_04: i64 = 2_460_496;

    #[test]
    fn wsjtx_string_normal() {
        let mut buf = Vec::new();
        push_str(&mut buf, "FT8");
        let mut s: &[u8] = &buf;
        assert_eq!(read_wsjtx_string(&mut s).unwrap(), "FT8");
        assert_eq!(s.len(), 0);
    }

    #[test]
    fn wsjtx_string_empty() {
        let mut buf = Vec::new();
        push_str(&mut buf, "");
        let mut s: &[u8] = &buf;
        assert_eq!(read_wsjtx_string(&mut s).unwrap(), "");
    }

    #[test]
    fn wsjtx_string_null_qt() {
        let mut buf = Vec::new();
        push_null_str(&mut buf);
        let mut s: &[u8] = &buf;
        assert_eq!(read_wsjtx_string(&mut s).unwrap(), "");
    }

    #[test]
    fn wsjtx_string_rejects_overlength() {
        let mut buf: Vec<u8> = Vec::new();
        push_u32(&mut buf, 128 * 1024);
        buf.extend(vec![b'A'; 10]);
        let mut s: &[u8] = &buf;
        assert!(read_wsjtx_string(&mut s).is_err());
    }

    #[test]
    fn wsjtx_string_rejects_truncated() {
        let mut buf: Vec<u8> = Vec::new();
        push_u32(&mut buf, 10);
        buf.extend_from_slice(b"short");
        let mut s: &[u8] = &buf;
        assert!(read_wsjtx_string(&mut s).is_err());
    }

    #[test]
    fn wsjtx_string_too_few_length_bytes() {
        let buf: &[u8] = &[0, 1];
        let mut s = buf;
        assert!(read_wsjtx_string(&mut s).is_err());
    }

    #[test]
    fn julian_j2000_epoch() {
        assert_eq!(julian_to_iso8601(2_451_545, 0), "2000-01-01T00:00:00Z");
    }

    #[test]
    fn julian_date_and_time() {
        let ms = (14 * 3600 + 30 * 60 + 45) * 1000u32;
        assert_eq!(
            julian_to_iso8601(JD_2024_07_04 as u64, ms),
            "2024-07-04T14:30:45Z"
        );
    }

    #[test]
    fn julian_midnight() {
        assert_eq!(
            julian_to_iso8601(JD_2024_07_04 as u64, 0),
            "2024-07-04T00:00:00Z"
        );
    }

    #[test]
    fn qdatetime_utc_round_trip() {
        let ms = (12 * 3600) * 1000u32;
        let mut buf = Vec::new();
        push_qdatetime(&mut buf, JD_2024_07_04, ms);
        let mut s: &[u8] = &buf;
        assert_eq!(read_qdatetime(&mut s).unwrap(), "2024-07-04T12:00:00Z");
        assert_eq!(s.len(), 0);
    }

    #[test]
    fn qdatetime_with_offset_consumes_extra_bytes() {
        let mut buf = Vec::new();
        push_i64(&mut buf, JD_2024_07_04);
        push_u32(&mut buf, 0);
        buf.push(2);
        push_u32(&mut buf, 3_600u32);
        let mut s: &[u8] = &buf;
        assert!(read_qdatetime(&mut s).is_ok());
        assert_eq!(s.len(), 0, "offset bytes must be consumed");
    }

    #[test]
    fn qdatetime_too_short() {
        let buf = vec![0u8; 5];
        let mut s: &[u8] = &buf;
        assert!(read_qdatetime(&mut s).is_err());
    }

    #[test]
    fn is_maidenhead_grid_accepts_expected_shapes() {
        assert!(is_maidenhead_grid("FN31"));
        assert!(is_maidenhead_grid("EM10aa"));
        assert!(!is_maidenhead_grid("FN3"));
        assert!(!is_maidenhead_grid("1234"));
    }

    #[test]
    fn maidenhead_distance_km_returns_distance_for_valid_grids() {
        let distance = maidenhead_distance_km("EM10", "FN31").expect("distance should compute");
        assert!(distance > 0);
    }
}
