//! Flrig XML-RPC rig control client.

use anyhow::{anyhow, Context};

use super::{RigBackend, RigController, RigTelemetry};

/// Default Flrig XML-RPC port.
pub const DEFAULT_FLRIG_PORT: u16 = 12345;

/// XML-RPC parameter type.
#[allow(dead_code)]
enum XmlRpcParam<'a> {
    Int(i64),
    Double(f64),
    String(&'a str),
}

/// Flrig CAT controller via XML-RPC.
#[derive(Clone)]
pub struct FlrigController {
    host: String,
    port: u16,
    endpoint: String,
    client: reqwest::Client,
}

impl FlrigController {
    /// Connect to a Flrig XML-RPC endpoint and verify basic CAT reads.
    pub async fn connect(host: impl Into<String>, port: u16) -> anyhow::Result<Self> {
        let host = host.into();
        let endpoint = format!("http://{host}:{port}");

        let controller = Self {
            host,
            port,
            endpoint,
            client: reqwest::Client::builder()
                .timeout(std::time::Duration::from_millis(750))
                .build()
                .context("build Flrig HTTP client")?,
        };

        // Connectivity probe: if this fails, caller treats Flrig as unavailable.
        let _ = controller.get_frequency().await?;
        let _ = controller.get_mode().await?;

        Ok(controller)
    }

    async fn call_method(
        &self,
        method: &str,
        params: &[XmlRpcParam<'_>],
    ) -> anyhow::Result<String> {
        let body = build_xmlrpc_request(method, params);
        let resp = self
            .client
            .post(&self.endpoint)
            .header(reqwest::header::CONTENT_TYPE, "text/xml")
            .body(body)
            .send()
            .await
            .with_context(|| format!("POST XML-RPC {method} to {}", self.endpoint))?
            .error_for_status()
            .with_context(|| format!("Flrig XML-RPC status error calling {method}"))?
            .text()
            .await
            .context("read Flrig XML-RPC response body")?;

        parse_xmlrpc_value(&resp)
    }

    pub(crate) fn parse_frequency_hz(raw: &str) -> anyhow::Result<u64> {
        // Flrig often returns frequencies as decimal string (Hz).
        let cleaned = raw.trim();
        if let Ok(v) = cleaned.parse::<u64>() {
            return Ok(v);
        }
        if let Ok(v) = cleaned.parse::<f64>() {
            if v >= 0.0 {
                return Ok(v.round() as u64);
            }
        }
        Err(anyhow!("invalid frequency value from Flrig: {cleaned}"))
    }

    fn parse_f32(raw: &str) -> Option<f32> {
        raw.trim().parse::<f32>().ok()
    }

    fn parse_i32(raw: &str) -> Option<i32> {
        raw.trim().parse::<i32>().ok()
    }
}

#[async_trait::async_trait]
impl RigController for FlrigController {
    fn backend(&self) -> RigBackend {
        RigBackend::Flrig
    }

    fn host(&self) -> &str {
        &self.host
    }

    fn port(&self) -> u16 {
        self.port
    }

    async fn get_frequency(&self) -> anyhow::Result<u64> {
        let raw = self.call_method("rig.get_vfo", &[]).await?;
        Self::parse_frequency_hz(&raw)
    }

    async fn get_mode(&self) -> anyhow::Result<String> {
        let raw = self.call_method("rig.get_mode", &[]).await?;
        Ok(raw.trim().to_uppercase())
    }

    async fn set_frequency(&self, frequency_hz: u64) -> anyhow::Result<()> {
        let _ = self
            .call_method("rig.set_vfo", &[XmlRpcParam::Double(frequency_hz as f64)])
            .await?;
        Ok(())
    }

    async fn set_mode(&self, mode: &str) -> anyhow::Result<()> {
        let _ = self
            .call_method("rig.set_mode", &[XmlRpcParam::String(mode)])
            .await?;
        Ok(())
    }

    async fn get_telemetry(&self) -> anyhow::Result<RigTelemetry> {
        let frequency_hz = self.get_frequency().await?;
        let mode = self.get_mode().await?;

        let bandwidth_hz = self
            .call_method("rig.get_bw", &[])
            .await
            .ok()
            .and_then(|raw| Self::parse_i32(&raw));

        let s_meter = self
            .call_method("rig.get_smeter", &[])
            .await
            .ok()
            .and_then(|raw| Self::parse_f32(&raw));

        let power = self
            .call_method("rig.get_power", &[])
            .await
            .ok()
            .and_then(|raw| Self::parse_f32(&raw));

        Ok(RigTelemetry {
            frequency_hz: Some(frequency_hz),
            mode: Some(mode),
            bandwidth_hz,
            s_meter,
            power,
            vfo: None,
            strength: s_meter,
        })
    }
}

fn build_xmlrpc_request(method: &str, params: &[XmlRpcParam<'_>]) -> String {
    let mut payload = String::new();
    payload.push_str("<?xml version=\"1.0\"?>");
    payload.push_str("<methodCall>");
    payload.push_str("<methodName>");
    payload.push_str(method);
    payload.push_str("</methodName>");

    if !params.is_empty() {
        payload.push_str("<params>");
        for param in params {
            payload.push_str("<param><value>");
            match param {
                XmlRpcParam::Int(value) => {
                    payload.push_str("<int>");
                    payload.push_str(&value.to_string());
                    payload.push_str("</int>");
                }
                XmlRpcParam::Double(value) => {
                    payload.push_str("<double>");
                    payload.push_str(&value.to_string());
                    payload.push_str("</double>");
                }
                XmlRpcParam::String(value) => {
                    payload.push_str("<string>");
                    payload.push_str(&xml_escape(value));
                    payload.push_str("</string>");
                }
            }
            payload.push_str("</value></param>");
        }
        payload.push_str("</params>");
    }

    payload.push_str("</methodCall>");
    payload
}

pub(crate) fn xml_escape(value: &str) -> String {
    value
        .replace('&', "&amp;")
        .replace('<', "&lt;")
        .replace('>', "&gt;")
        .replace('"', "&quot;")
        .replace('\'', "&apos;")
}

pub(crate) fn parse_xmlrpc_value(xml: &str) -> anyhow::Result<String> {
    if xml.contains("<fault>") {
        let message = extract_between(xml, "<string>", "</string>")
            .or_else(|| extract_between(xml, "<faultString>", "</faultString>"))
            .unwrap_or_else(|| "unknown XML-RPC fault".to_string());
        return Err(anyhow!("Flrig XML-RPC fault: {message}"));
    }

    for (open, close) in [
        ("<string>", "</string>"),
        ("<double>", "</double>"),
        ("<i4>", "</i4>"),
        ("<int>", "</int>"),
        ("<boolean>", "</boolean>"),
    ] {
        if let Some(value) = extract_between(xml, open, close) {
            return Ok(xml_unescape(value.trim()));
        }
    }

    if let Some(raw_value) = extract_between(xml, "<value>", "</value>") {
        let stripped = strip_xml_tags(raw_value.trim());
        if !stripped.is_empty() {
            return Ok(xml_unescape(stripped.trim()));
        }
    }

    Err(anyhow!("could not parse XML-RPC response value"))
}

fn extract_between(haystack: &str, start: &str, end: &str) -> Option<String> {
    let start_idx = haystack.find(start)? + start.len();
    let rest = &haystack[start_idx..];
    let end_idx = rest.find(end)?;
    Some(rest[..end_idx].to_string())
}

fn strip_xml_tags(input: &str) -> String {
    let mut output = String::with_capacity(input.len());
    let mut in_tag = false;
    for ch in input.chars() {
        match ch {
            '<' => in_tag = true,
            '>' => in_tag = false,
            _ if !in_tag => output.push(ch),
            _ => {}
        }
    }
    output
}

pub(crate) fn xml_unescape(value: &str) -> String {
    value
        .replace("&lt;", "<")
        .replace("&gt;", ">")
        .replace("&quot;", "\"")
        .replace("&apos;", "'")
        .replace("&amp;", "&")
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

#[cfg(test)]
mod tests {
    use std::collections::HashMap;
    use std::net::SocketAddr;

    use tokio::io::{AsyncReadExt, AsyncWriteExt};
    use tokio::net::TcpListener;

    use super::{parse_xmlrpc_value, xml_escape, xml_unescape, FlrigController};
    use crate::rig::RigController;

    // ---- XML-RPC value parser -------------------------------------------

    #[test]
    fn parses_string_value() {
        let xml = r#"<?xml version="1.0"?><methodResponse><params><param><value><string>FT8</string></value></param></params></methodResponse>"#;
        assert_eq!(parse_xmlrpc_value(xml).unwrap(), "FT8");
    }

    #[test]
    fn parses_double_value() {
        let xml = r#"<methodResponse><params><param><value><double>14074000.0</double></value></param></params></methodResponse>"#;
        assert_eq!(parse_xmlrpc_value(xml).unwrap(), "14074000.0");
    }

    #[test]
    fn parses_i4_value() {
        let xml = r#"<methodResponse><params><param><value><i4>2700</i4></value></param></params></methodResponse>"#;
        assert_eq!(parse_xmlrpc_value(xml).unwrap(), "2700");
    }

    #[test]
    fn parses_int_value() {
        let xml = r#"<methodResponse><params><param><value><int>100</int></value></param></params></methodResponse>"#;
        assert_eq!(parse_xmlrpc_value(xml).unwrap(), "100");
    }

    #[test]
    fn returns_error_on_fault_response() {
        let xml = r#"<methodResponse><fault><value><struct>
            <member><name>faultCode</name><value><int>4</int></value></member>
            <member><name>faultString</name><value><string>Method not found: rig.get_vfo</string></value></member>
        </struct></value></fault></methodResponse>"#;
        let err = parse_xmlrpc_value(xml).unwrap_err();
        assert!(err.to_string().contains("Flrig XML-RPC fault"));
    }

    // ---- FlrigController static helpers ---------------------------------

    #[test]
    fn parse_frequency_hz_from_integer_string() {
        assert_eq!(
            FlrigController::parse_frequency_hz("14074000").unwrap(),
            14_074_000
        );
    }

    #[test]
    fn parse_frequency_hz_from_float_string() {
        assert_eq!(
            FlrigController::parse_frequency_hz("14074000.0").unwrap(),
            14_074_000
        );
    }

    #[test]
    fn parse_frequency_hz_trims_whitespace() {
        assert_eq!(
            FlrigController::parse_frequency_hz("  7074000  ").unwrap(),
            7_074_000
        );
    }

    #[test]
    fn parse_frequency_hz_rejects_invalid() {
        assert!(FlrigController::parse_frequency_hz("not_a_number").is_err());
    }

    // ---- XML escape/unescape round-trip ---------------------------------

    #[test]
    fn xml_escape_roundtrip() {
        let original = r#"a & b < c > "d" 'e'"#;
        let escaped = xml_escape(original);
        let unescaped = xml_unescape(&escaped);
        assert_eq!(unescaped, original);
    }

    // ---- Mock HTTP server integration tests -----------------------------

    /// Spin up a minimal HTTP/1.0 server that serves fixed XML-RPC response
    /// bodies keyed by XML-RPC method name.
    async fn spawn_mock_flrig(
        responses: HashMap<String, String>,
    ) -> anyhow::Result<(SocketAddr, tokio::task::JoinHandle<()>)> {
        let listener = TcpListener::bind("127.0.0.1:0").await?;
        let addr = listener.local_addr()?;

        let handle = tokio::spawn(async move {
            loop {
                let Ok((mut socket, _peer)) = listener.accept().await else {
                    break;
                };

                let mut buf = vec![0u8; 8192];
                let n = match socket.read(&mut buf).await {
                    Ok(n) if n > 0 => n,
                    _ => continue,
                };

                let request = String::from_utf8_lossy(&buf[..n]);

                // Extract the methodName from the request body.
                let method = request
                    .find("<methodName>")
                    .and_then(|start| {
                        let rest = &request[start + 12..];
                        rest.find("</methodName>").map(|end| rest[..end].to_string())
                    })
                    .unwrap_or_default();

                let body = responses
                    .get(&method)
                    .cloned()
                    .or_else(|| responses.get("*").cloned())
                    .unwrap_or_else(|| {
                        r#"<?xml version="1.0"?><methodResponse><params><param><value><string>OK</string></value></param></params></methodResponse>"#.to_string()
                    });

                let response = format!(
                    "HTTP/1.0 200 OK\r\nContent-Type: text/xml\r\nContent-Length: {}\r\n\r\n{}",
                    body.len(),
                    body
                );
                let _ = socket.write_all(response.as_bytes()).await;
            }
        });

        Ok((addr, handle))
    }

    fn freq_xml(hz: u64) -> String {
        format!(
            r#"<?xml version="1.0"?><methodResponse><params><param><value><double>{hz}.0</double></value></param></params></methodResponse>"#
        )
    }

    fn mode_xml(mode: &str) -> String {
        format!(
            r#"<?xml version="1.0"?><methodResponse><params><param><value><string>{mode}</string></value></param></params></methodResponse>"#
        )
    }

    #[tokio::test]
    async fn connects_and_reads_frequency_from_mock_flrig() {
        let mut responses = HashMap::new();
        responses.insert("rig.get_vfo".to_string(), freq_xml(14_074_000));
        responses.insert("rig.get_mode".to_string(), mode_xml("FT8"));

        let (addr, handle) = spawn_mock_flrig(responses).await.unwrap();
        let controller = FlrigController::connect(addr.ip().to_string(), addr.port())
            .await
            .unwrap();

        assert_eq!(controller.get_frequency().await.unwrap(), 14_074_000);
        handle.abort();
    }

    #[tokio::test]
    async fn reads_mode_and_normalises_to_uppercase() {
        let mut responses = HashMap::new();
        responses.insert("rig.get_vfo".to_string(), freq_xml(7_074_000));
        responses.insert("rig.get_mode".to_string(), mode_xml("usb"));

        let (addr, handle) = spawn_mock_flrig(responses).await.unwrap();
        let controller = FlrigController::connect(addr.ip().to_string(), addr.port())
            .await
            .unwrap();

        assert_eq!(controller.get_mode().await.unwrap(), "USB");
        handle.abort();
    }

    #[tokio::test]
    async fn connect_fails_gracefully_when_server_down() {
        // Port 1 is almost certainly not listening on loopback.
        let result = FlrigController::connect("127.0.0.1".to_string(), 1).await;
        assert!(result.is_err());
    }

    #[tokio::test]
    async fn get_telemetry_aggregates_multiple_calls() {
        let mut responses = HashMap::new();
        responses.insert("rig.get_vfo".to_string(), freq_xml(21_074_000));
        responses.insert("rig.get_mode".to_string(), mode_xml("FT8"));
        // power and bw will fall back to the wildcard
        responses.insert("*".to_string(), freq_xml(21_074_000));

        let (addr, handle) = spawn_mock_flrig(responses).await.unwrap();
        let controller = FlrigController::connect(addr.ip().to_string(), addr.port())
            .await
            .unwrap();

        let telemetry = controller.get_telemetry().await.unwrap();
        assert_eq!(telemetry.frequency_hz, Some(21_074_000));
        assert_eq!(telemetry.mode.as_deref(), Some("FT8"));
        handle.abort();
    }
}
