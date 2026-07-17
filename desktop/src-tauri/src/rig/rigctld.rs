//! Hamlib rigctld TCP client.

use std::time::{Duration, Instant};

use anyhow::{anyhow, Context};
use tokio::io::{AsyncReadExt, AsyncWriteExt};
use tokio::net::TcpStream;
use tracing::debug;

use super::{RigBackend, RigController, RigTelemetry};

/// Default rigctld TCP port.
pub const DEFAULT_RIGCTLD_PORT: u16 = 4532;
/// Default per-operation TCP timeout for remote rigctld probes and commands.
pub const DEFAULT_RIGCTLD_TIMEOUT_MS: u64 = 2500;

#[derive(Clone)]
pub struct RigctldController {
    host: String,
    port: u16,
    timeout: Duration,
}

impl RigctldController {
    /// Connect to rigctld and validate baseline protocol reads.
    pub async fn connect(host: impl Into<String>, port: u16) -> anyhow::Result<Self> {
        Self::connect_with_timeout(
            host,
            port,
            Duration::from_millis(DEFAULT_RIGCTLD_TIMEOUT_MS),
        )
        .await
    }

    /// Connect to rigctld with an explicit per-operation timeout.
    pub async fn connect_with_timeout(
        host: impl Into<String>,
        port: u16,
        timeout: Duration,
    ) -> anyhow::Result<Self> {
        let controller = Self {
            host: host.into(),
            port,
            timeout,
        };

        let timeout_ms = controller.timeout.as_millis() as u64;
        let probe_started = Instant::now();
        debug!(
            host = %controller.host,
            port = controller.port,
            timeout_ms,
            "rigctld connect probe: starting"
        );

        let frequency_started = Instant::now();
        let frequency = match controller.get_frequency().await {
            Ok(frequency) => {
                debug!(
                    host = %controller.host,
                    port = controller.port,
                    timeout_ms,
                    elapsed_ms = frequency_started.elapsed().as_millis() as u64,
                    frequency_hz = frequency,
                    "rigctld connect probe: get_frequency succeeded"
                );
                frequency
            }
            Err(err) => {
                debug!(
                    host = %controller.host,
                    port = controller.port,
                    timeout_ms,
                    elapsed_ms = frequency_started.elapsed().as_millis() as u64,
                    error = %err,
                    "rigctld connect probe: get_frequency failed"
                );
                return Err(err);
            }
        };

        let mode_started = Instant::now();
        let mode = match controller.get_mode().await {
            Ok(mode) => {
                debug!(
                    host = %controller.host,
                    port = controller.port,
                    timeout_ms,
                    elapsed_ms = mode_started.elapsed().as_millis() as u64,
                    mode = %mode,
                    "rigctld connect probe: get_mode succeeded"
                );
                mode
            }
            Err(err) => {
                debug!(
                    host = %controller.host,
                    port = controller.port,
                    timeout_ms,
                    elapsed_ms = mode_started.elapsed().as_millis() as u64,
                    error = %err,
                    "rigctld connect probe: get_mode failed"
                );
                return Err(err);
            }
        };

        debug!(
            host = %controller.host,
            port = controller.port,
            timeout_ms,
            total_elapsed_ms = probe_started.elapsed().as_millis() as u64,
            frequency_hz = frequency,
            mode = %mode,
            "rigctld connect probe: completed"
        );

        Ok(controller)
    }

    async fn send_command(&self, command: &str) -> anyhow::Result<String> {
        let addr = format!("{}:{}", self.host, self.port);
        let timeout_ms = self.timeout.as_millis() as u64;

        let connect_started = Instant::now();
        let mut stream = tokio::time::timeout(self.timeout, TcpStream::connect(&addr))
            .await
            .with_context(|| format!("rigctld connect timeout to {addr}"))??;
        let connect_ms = connect_started.elapsed().as_millis() as u64;

        let wire = format!("{command}\n");
        tokio::time::timeout(self.timeout, stream.write_all(wire.as_bytes()))
            .await
            .context("rigctld write timeout")??;

        tokio::time::timeout(self.timeout, stream.flush())
            .await
            .context("rigctld flush timeout")??;

        let read_started = Instant::now();
        let mut buf = vec![0u8; 4096];
        let n = tokio::time::timeout(self.timeout, stream.read(&mut buf))
            .await
            .context("rigctld read timeout")??;
        let read_ms = read_started.elapsed().as_millis() as u64;

        debug!(
            host = %self.host,
            port = self.port,
            timeout_ms,
            command = %command,
            connect_ms,
            read_ms,
            response_bytes = n,
            "rigctld send_command"
        );

        if n == 0 {
            return Err(anyhow!(
                "rigctld returned empty response for command: {command}"
            ));
        }

        Ok(String::from_utf8_lossy(&buf[..n]).to_string())
    }

    fn parse_response_lines(response: &str) -> Vec<String> {
        response
            .lines()
            .map(str::trim)
            .filter(|line| !line.is_empty())
            .map(ToString::to_string)
            .collect()
    }

    fn parse_frequency(response: &str) -> anyhow::Result<u64> {
        let lines = Self::parse_response_lines(response);
        let first = lines
            .iter()
            .find(|line| !line.starts_with("RPRT"))
            .ok_or_else(|| anyhow!("missing frequency response line"))?;

        if let Ok(v) = first.parse::<u64>() {
            return Ok(v);
        }
        if let Ok(v) = first.parse::<f64>() {
            if v >= 0.0 {
                return Ok(v.round() as u64);
            }
        }

        Err(anyhow!("invalid rigctld frequency response: {first}"))
    }

    fn parse_mode_and_bandwidth(response: &str) -> anyhow::Result<(String, Option<i32>)> {
        let lines = Self::parse_response_lines(response);
        let first = lines
            .iter()
            .find(|line| !line.starts_with("RPRT"))
            .ok_or_else(|| anyhow!("missing rigctld mode response"))?
            .trim()
            .to_string();

        // Common format: "USB 2400"
        let mut parts = first.split_whitespace();
        let mode = parts
            .next()
            .ok_or_else(|| anyhow!("invalid mode response"))?
            .to_uppercase();

        let bw_inline = parts.next().and_then(|v| v.parse::<i32>().ok());

        // Older rigctld can return:
        //   USB
        //   2400
        let bw_second_line = lines
            .iter()
            .skip(1)
            .find(|line| !line.starts_with("RPRT"))
            .and_then(|line| line.parse::<i32>().ok());

        Ok((mode, bw_inline.or(bw_second_line)))
    }

    fn parse_optional_f32(response: &str) -> Option<f32> {
        let lines = Self::parse_response_lines(response);
        lines
            .iter()
            .find(|line| !line.starts_with("RPRT"))
            .and_then(|line| line.parse::<f32>().ok())
    }

    fn parse_optional_string(response: &str) -> Option<String> {
        let lines = Self::parse_response_lines(response);
        lines
            .iter()
            .find(|line| !line.starts_with("RPRT"))
            .map(|line| line.to_string())
    }

    fn ensure_ok(response: &str) -> anyhow::Result<()> {
        if response.lines().any(|line| line.trim() == "RPRT 0") {
            return Ok(());
        }
        // Some rigctld builds return no explicit RPRT line for setters.
        if response.trim().is_empty() {
            return Ok(());
        }
        // Treat short "0" as success as well.
        if response.trim() == "0" {
            return Ok(());
        }

        Err(anyhow!("rigctld command failed: {}", response.trim()))
    }
}

#[async_trait::async_trait]
impl RigController for RigctldController {
    fn backend(&self) -> RigBackend {
        RigBackend::Hamlib
    }

    fn host(&self) -> &str {
        &self.host
    }

    fn port(&self) -> u16 {
        self.port
    }

    async fn get_frequency(&self) -> anyhow::Result<u64> {
        let response = self.send_command("f").await?;
        Self::parse_frequency(&response)
    }

    async fn get_mode(&self) -> anyhow::Result<String> {
        let response = self.send_command("m").await?;
        let (mode, _) = Self::parse_mode_and_bandwidth(&response)?;
        Ok(mode)
    }

    async fn set_frequency(&self, frequency_hz: u64) -> anyhow::Result<()> {
        let response = self.send_command(&format!("F {frequency_hz}")).await?;
        Self::ensure_ok(&response)
    }

    async fn set_mode(&self, mode: &str) -> anyhow::Result<()> {
        // Hamlib: M <mode> <passband>
        let response = self
            .send_command(&format!("M {} 0", mode.to_uppercase()))
            .await?;
        Self::ensure_ok(&response)
    }

    async fn get_telemetry(&self) -> anyhow::Result<RigTelemetry> {
        let frequency_hz = self.get_frequency().await?;

        let mode_response = self.send_command("m").await?;
        let (mode, bandwidth_hz) = Self::parse_mode_and_bandwidth(&mode_response)?;

        let vfo = self
            .send_command("v")
            .await
            .ok()
            .and_then(|response| Self::parse_optional_string(&response));

        let power = self
            .send_command("l RFPOWER")
            .await
            .ok()
            .and_then(|response| Self::parse_optional_f32(&response));

        let strength = self
            .send_command("l STRENGTH")
            .await
            .ok()
            .and_then(|response| Self::parse_optional_f32(&response));

        Ok(RigTelemetry {
            frequency_hz: Some(frequency_hz),
            mode: Some(mode),
            bandwidth_hz,
            s_meter: strength,
            power,
            vfo,
            strength,
        })
    }
}

#[cfg(test)]
mod tests {
    use std::collections::HashMap;
    use std::sync::Arc;

    use tokio::io::{AsyncReadExt, AsyncWriteExt};
    use tokio::net::TcpListener;
    use tokio::sync::Mutex;

    use super::RigctldController;
    use crate::rig::RigController;

    async fn spawn_mock_rigctld(
        responses: HashMap<String, String>,
    ) -> anyhow::Result<(String, Arc<Mutex<Vec<String>>>, tokio::task::JoinHandle<()>)> {
        let listener = TcpListener::bind("127.0.0.1:0").await?;
        let addr = listener.local_addr()?;
        let commands = Arc::new(Mutex::new(Vec::new()));
        let commands_clone = Arc::clone(&commands);

        let handle = tokio::spawn(async move {
            loop {
                let Ok((mut socket, _peer)) = listener.accept().await else {
                    break;
                };

                let mut buf = [0u8; 1024];
                let Ok(n) = socket.read(&mut buf).await else {
                    continue;
                };
                if n == 0 {
                    continue;
                }

                let line = String::from_utf8_lossy(&buf[..n]).trim().to_string();
                commands_clone.lock().await.push(line.clone());

                let response = responses
                    .get(&line)
                    .cloned()
                    .or_else(|| responses.get("*").cloned())
                    .unwrap_or_else(|| "RPRT 0\n".to_string());

                let _ = socket.write_all(response.as_bytes()).await;
            }
        });

        Ok((addr.to_string(), commands, handle))
    }

    #[tokio::test]
    async fn parses_frequency_and_mode_from_mock_server() {
        let mut responses = HashMap::new();
        responses.insert("f".to_string(), "14074000\n".to_string());
        responses.insert("m".to_string(), "FT8 3000\n".to_string());

        let (addr, _commands, handle) = spawn_mock_rigctld(responses).await.unwrap();
        let (host, port_str) = addr.split_once(':').unwrap();
        let port = port_str.parse::<u16>().unwrap();

        let controller = RigctldController::connect(host.to_string(), port)
            .await
            .unwrap();
        assert_eq!(controller.get_frequency().await.unwrap(), 14_074_000);
        assert_eq!(controller.get_mode().await.unwrap(), "FT8");

        handle.abort();
    }

    #[tokio::test]
    async fn sends_set_frequency_command() {
        let mut responses = HashMap::new();
        responses.insert("f".to_string(), "7074000\n".to_string());
        responses.insert("m".to_string(), "LSB 2400\n".to_string());
        responses.insert("F 7075000".to_string(), "RPRT 0\n".to_string());

        let (addr, commands, handle) = spawn_mock_rigctld(responses).await.unwrap();
        let (host, port_str) = addr.split_once(':').unwrap();
        let port = port_str.parse::<u16>().unwrap();

        let controller = RigctldController::connect(host.to_string(), port)
            .await
            .unwrap();
        controller.set_frequency(7_075_000).await.unwrap();

        let observed = commands.lock().await.clone();
        assert!(observed.iter().any(|cmd| cmd == "F 7075000"));

        handle.abort();
    }
}
