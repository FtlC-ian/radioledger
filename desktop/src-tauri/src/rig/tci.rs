//! TCI (Transceiver Control Interface) WebSocket client.
//!
//! TCI is an ASCII, semicolon-delimited protocol transported over WebSocket.
//! This client keeps a persistent socket, updates a cached telemetry snapshot
//! from unsolicited server messages, and lazily reconnects with backoff when
//! the socket drops.

use std::sync::Arc;
use std::time::{Duration, Instant};

use anyhow::{anyhow, Context};
use futures_util::{SinkExt, StreamExt};
use tokio::sync::{mpsc, watch, Mutex};
use tokio::task::JoinHandle;
use tokio_tungstenite::{connect_async, tungstenite::Message};

use super::{RigBackend, RigController, RigTelemetry};

/// Default TCI WebSocket port used by ExpertSDR/SunSDR.
pub const DEFAULT_TCI_PORT: u16 = 50001;

#[derive(Debug, Clone, Default)]
struct TciSnapshot {
    connected: bool,
    frequency_hz: Option<u64>,
    mode: Option<String>,
    s_meter: Option<f32>,
    power: Option<f32>,
    vfo: Option<String>,
    receiver: u32,
    channel: u32,
}

#[derive(Default)]
struct TciRuntimeState {
    writer: Option<mpsc::UnboundedSender<String>>,
    writer_task: Option<JoinHandle<()>>,
    reader_task: Option<JoinHandle<()>>,
    reconnect_failures: u32,
    last_reconnect_attempt: Option<Instant>,
}

struct TciInner {
    host: String,
    port: u16,
    /// Configured receiver index (0 = RX1, 1 = RX2, …). Only messages for
    /// this receiver update the cached telemetry snapshot.
    receiver_idx: u32,
    timeout: Duration,
    runtime: Mutex<TciRuntimeState>,
    updates_tx: watch::Sender<TciSnapshot>,
}

/// Persistent TCI client.
#[derive(Clone)]
pub struct TciController {
    inner: Arc<TciInner>,
    updates_rx: watch::Receiver<TciSnapshot>,
}

#[derive(Debug, Clone, PartialEq)]
enum TciMessage {
    Vfo {
        receiver: u32,
        channel: Option<u32>,
        frequency_hz: u64,
    },
    Modulation {
        receiver: u32,
        mode: String,
    },
    RxSmeter {
        receiver: u32,
        channel: u32,
        value: f32,
    },
    TxPower {
        value: f32,
    },
    Protocol {
        software: String,
        version: String,
    },
    Start,
    Stop,
    Unknown,
}

impl TciController {
    /// Connect to a TCI server and start the background reader.
    ///
    /// `receiver_idx` selects which TCI receiver to read from (0 = RX1, 1 = RX2, …).
    /// Only telemetry messages for the configured receiver update the cached snapshot.
    pub async fn connect(
        host: impl Into<String>,
        port: u16,
        receiver_idx: u32,
    ) -> anyhow::Result<Self> {
        let host = host.into();
        let (updates_tx, updates_rx) = watch::channel(TciSnapshot::default());
        let controller = Self {
            inner: Arc::new(TciInner {
                host,
                port,
                receiver_idx,
                timeout: Duration::from_millis(1000),
                runtime: Mutex::new(TciRuntimeState::default()),
                updates_tx,
            }),
            updates_rx,
        };

        controller.ensure_connected().await?;
        controller.request_refresh().await?;
        Ok(controller)
    }

    pub async fn disconnect(&self) {
        self.clear_connection().await;
    }

    pub async fn get_smeter(&self) -> anyhow::Result<Option<f32>> {
        self.ensure_connected().await?;
        let snapshot = self.snapshot();
        if snapshot.s_meter.is_some() {
            return Ok(snapshot.s_meter);
        }
        self.refresh_and_wait(|s| s.s_meter.is_some()).await?;
        Ok(self.snapshot().s_meter)
    }

    pub async fn get_power(&self) -> anyhow::Result<Option<f32>> {
        self.ensure_connected().await?;
        Ok(self.snapshot().power)
    }

    fn snapshot(&self) -> TciSnapshot {
        self.updates_rx.borrow().clone()
    }

    async fn ensure_connected(&self) -> anyhow::Result<()> {
        {
            let runtime = self.inner.runtime.lock().await;
            if runtime.writer.is_some() && self.snapshot().connected {
                return Ok(());
            }

            let backoff = reconnect_backoff(runtime.reconnect_failures);
            if let Some(last_attempt) = runtime.last_reconnect_attempt {
                if last_attempt.elapsed() < backoff {
                    return Err(anyhow!(
                        "TCI reconnect backoff active ({} ms remaining)",
                        (backoff - last_attempt.elapsed()).as_millis()
                    ));
                }
            }
        }

        let url = format!("ws://{}:{}", self.inner.host, self.inner.port);
        let connect_result = tokio::time::timeout(self.inner.timeout, connect_async(&url))
            .await
            .with_context(|| format!("TCI connect timeout to {url}"));
        let (stream, _) = match connect_result {
            Ok(Ok(pair)) => pair,
            Ok(Err(err)) => {
                let mut runtime = self.inner.runtime.lock().await;
                runtime.reconnect_failures = runtime.reconnect_failures.saturating_add(1);
                runtime.last_reconnect_attempt = Some(Instant::now());
                return Err(err).with_context(|| format!("TCI connect failed to {url}"));
            }
            Err(err) => {
                let mut runtime = self.inner.runtime.lock().await;
                runtime.reconnect_failures = runtime.reconnect_failures.saturating_add(1);
                runtime.last_reconnect_attempt = Some(Instant::now());
                return Err(err);
            }
        };

        let (mut ws_write, mut ws_read) = stream.split();
        let (writer_tx, mut writer_rx) = mpsc::unbounded_channel::<String>();

        let inner_for_writer = Arc::clone(&self.inner);
        let writer_task = tokio::spawn(async move {
            while let Some(command) = writer_rx.recv().await {
                if ws_write.send(Message::Text(command)).await.is_err() {
                    break;
                }
            }
            let _ = ws_write.close().await;
            let _ = update_snapshot(&inner_for_writer, |snapshot| snapshot.connected = false);
        });

        let inner_for_reader = Arc::clone(&self.inner);
        let reader_task = tokio::spawn(async move {
            while let Some(message) = ws_read.next().await {
                match message {
                    Ok(Message::Text(text)) => {
                        apply_payload(&inner_for_reader, &text);
                    }
                    Ok(Message::Binary(data)) => {
                        if let Ok(text) = String::from_utf8(data.to_vec()) {
                            apply_payload(&inner_for_reader, &text);
                        }
                    }
                    Ok(Message::Ping(_)) | Ok(Message::Pong(_)) => {}
                    Ok(Message::Frame(_)) => {}
                    Ok(Message::Close(_)) | Err(_) => break,
                }
            }

            {
                let mut runtime = inner_for_reader.runtime.lock().await;
                runtime.writer = None;
                runtime.writer_task = None;
                runtime.reader_task = None;
            }
            let _ = update_snapshot(&inner_for_reader, |snapshot| snapshot.connected = false);
        });

        {
            let mut runtime = self.inner.runtime.lock().await;
            runtime.writer = Some(writer_tx);
            runtime.writer_task = Some(writer_task);
            runtime.reader_task = Some(reader_task);
            runtime.reconnect_failures = 0;
            runtime.last_reconnect_attempt = Some(Instant::now());
        }

        update_snapshot(&self.inner, |snapshot| snapshot.connected = true)?;
        Ok(())
    }

    async fn clear_connection(&self) {
        let (writer_task, reader_task) = {
            let mut runtime = self.inner.runtime.lock().await;
            runtime.writer = None;
            (runtime.writer_task.take(), runtime.reader_task.take())
        };

        if let Some(task) = writer_task {
            task.abort();
        }
        if let Some(task) = reader_task {
            task.abort();
        }

        let _ = update_snapshot(&self.inner, |snapshot| snapshot.connected = false);
    }

    async fn send_command(&self, command: impl Into<String>) -> anyhow::Result<()> {
        self.ensure_connected().await?;
        let tx = {
            let runtime = self.inner.runtime.lock().await;
            runtime.writer.clone()
        }
        .ok_or_else(|| anyhow!("TCI writer is not connected"))?;

        tx.send(command.into())
            .map_err(|_| anyhow!("TCI socket send failed"))
    }

    async fn request_refresh(&self) -> anyhow::Result<()> {
        let rx = self.inner.receiver_idx;
        self.send_command(format!("VFO:{rx},0;")).await?;
        self.send_command(format!("VFO:{rx},1;")).await?;
        self.send_command(format!("MODULATION:{rx};")).await?;
        self.send_command(format!("RX_SMETER:{rx},0;")).await?;
        self.send_command(format!("RX_SMETER:{rx},1;")).await?;
        Ok(())
    }

    async fn wait_for(&self, predicate: impl Fn(&TciSnapshot) -> bool) -> anyhow::Result<()> {
        if predicate(&self.snapshot()) {
            return Ok(());
        }

        let mut rx = self.updates_rx.clone();
        let timeout = self.inner.timeout;
        tokio::time::timeout(timeout, async move {
            loop {
                rx.changed().await.context("TCI update channel closed")?;
                if predicate(&rx.borrow().clone()) {
                    return Ok::<(), anyhow::Error>(());
                }
            }
        })
        .await
        .context("Timed out waiting for TCI state")??;

        Ok(())
    }

    async fn refresh_and_wait(
        &self,
        predicate: impl Fn(&TciSnapshot) -> bool + Copy,
    ) -> anyhow::Result<()> {
        self.request_refresh().await?;
        if self.wait_for(predicate).await.is_ok() {
            return Ok(());
        }

        self.clear_connection().await;
        self.ensure_connected().await?;
        self.request_refresh().await?;
        self.wait_for(predicate).await
    }
}

#[async_trait::async_trait]
impl RigController for TciController {
    fn backend(&self) -> RigBackend {
        RigBackend::Tci
    }

    fn host(&self) -> &str {
        &self.inner.host
    }

    fn port(&self) -> u16 {
        self.inner.port
    }

    async fn get_frequency(&self) -> anyhow::Result<u64> {
        self.ensure_connected().await?;
        let snapshot = self.snapshot();
        if let Some(frequency_hz) = snapshot.frequency_hz {
            return Ok(frequency_hz);
        }

        self.refresh_and_wait(|s| s.frequency_hz.is_some()).await?;
        self.snapshot()
            .frequency_hz
            .ok_or_else(|| anyhow!("TCI frequency not available"))
    }

    async fn get_mode(&self) -> anyhow::Result<String> {
        self.ensure_connected().await?;
        let snapshot = self.snapshot();
        if let Some(mode) = snapshot.mode {
            return Ok(mode);
        }

        self.refresh_and_wait(|s| s.mode.is_some()).await?;
        self.snapshot()
            .mode
            .ok_or_else(|| anyhow!("TCI mode not available"))
    }

    async fn set_frequency(&self, frequency_hz: u64) -> anyhow::Result<()> {
        self.ensure_connected().await?;
        let receiver = self.inner.receiver_idx;
        let channel = self.snapshot().channel;
        self.send_command(format!("VFO:{receiver},{channel},{frequency_hz};"))
            .await?;
        update_snapshot(&self.inner, |state| state.frequency_hz = Some(frequency_hz))?;
        Ok(())
    }

    async fn set_mode(&self, mode: &str) -> anyhow::Result<()> {
        self.ensure_connected().await?;
        let receiver = self.inner.receiver_idx;
        let mode = mode.trim().to_uppercase();
        self.send_command(format!("MODULATION:{receiver},{mode};"))
            .await?;
        update_snapshot(&self.inner, |state| state.mode = Some(mode.clone()))?;
        Ok(())
    }

    async fn get_telemetry(&self) -> anyhow::Result<RigTelemetry> {
        self.ensure_connected().await?;
        let snapshot = self.snapshot();
        if snapshot.frequency_hz.is_none() || snapshot.mode.is_none() {
            self.refresh_and_wait(|s| s.frequency_hz.is_some() || s.mode.is_some())
                .await?;
        }

        let snapshot = self.snapshot();
        Ok(RigTelemetry {
            frequency_hz: snapshot.frequency_hz,
            mode: snapshot.mode,
            bandwidth_hz: None,
            s_meter: snapshot.s_meter,
            power: snapshot.power,
            vfo: snapshot.vfo,
            strength: snapshot.s_meter,
        })
    }
}

fn reconnect_backoff(failures: u32) -> Duration {
    if failures == 0 {
        return Duration::ZERO;
    }
    let seconds = 2u64.saturating_pow(failures.min(4));
    Duration::from_secs(seconds.min(30))
}

fn update_snapshot(
    inner: &Arc<TciInner>,
    update: impl FnOnce(&mut TciSnapshot),
) -> anyhow::Result<()> {
    let mut snapshot = inner.updates_tx.borrow().clone();
    update(&mut snapshot);
    inner
        .updates_tx
        .send(snapshot)
        .map_err(|_| anyhow!("TCI update channel send failed"))
}

fn apply_payload(inner: &Arc<TciInner>, payload: &str) {
    let receiver_idx = inner.receiver_idx;
    for raw in payload
        .split(';')
        .map(str::trim)
        .filter(|part| !part.is_empty())
    {
        if let Some(message) = parse_message(raw) {
            let _ = update_snapshot(inner, |snapshot| match message {
                TciMessage::Vfo {
                    receiver,
                    channel,
                    frequency_hz,
                } => {
                    snapshot.connected = true;
                    // Only update telemetry for the configured receiver.
                    if receiver == receiver_idx {
                        snapshot.receiver = receiver;
                        if let Some(channel) = channel {
                            snapshot.channel = channel;
                            snapshot.vfo = Some(format_vfo(channel));
                        }
                        snapshot.frequency_hz = Some(frequency_hz);
                    }
                }
                TciMessage::Modulation { receiver, mode } => {
                    snapshot.connected = true;
                    if receiver == receiver_idx {
                        snapshot.receiver = receiver;
                        snapshot.mode = Some(mode);
                    }
                }
                TciMessage::RxSmeter {
                    receiver,
                    channel,
                    value,
                } => {
                    snapshot.connected = true;
                    if receiver == receiver_idx {
                        snapshot.receiver = receiver;
                        snapshot.channel = channel;
                        snapshot.vfo = Some(format_vfo(channel));
                        snapshot.s_meter = Some(value);
                    }
                }
                TciMessage::TxPower { value } => {
                    snapshot.connected = true;
                    snapshot.power = Some(value);
                }
                TciMessage::Protocol { .. }
                | TciMessage::Start
                | TciMessage::Stop
                | TciMessage::Unknown => {
                    snapshot.connected = true;
                }
            });
        }
    }
}

fn format_vfo(channel: u32) -> String {
    match channel {
        0 => "A".to_string(),
        1 => "B".to_string(),
        other => other.to_string(),
    }
}

fn parse_message(raw: &str) -> Option<TciMessage> {
    let (name, args_raw) = match raw.split_once(':') {
        Some((name, args)) => (name.trim().to_ascii_uppercase(), Some(args.trim())),
        None => (raw.trim().to_ascii_uppercase(), None),
    };

    let args: Vec<&str> = args_raw
        .unwrap_or("")
        .split(',')
        .map(str::trim)
        .filter(|arg| !arg.is_empty())
        .collect();

    match name.as_str() {
        "VFO" => parse_vfo(&args),
        "MODULATION" => parse_modulation(&args),
        "RX_SMETER" => parse_rx_smeter(&args),
        "TX_POWER" => parse_tx_power(&args),
        "PROTOCOL" if args.len() >= 2 => Some(TciMessage::Protocol {
            software: args[0].to_string(),
            version: args[1].to_string(),
        }),
        "START" => Some(TciMessage::Start),
        "STOP" => Some(TciMessage::Stop),
        _ => Some(TciMessage::Unknown),
    }
}

fn parse_vfo(args: &[&str]) -> Option<TciMessage> {
    match args {
        [receiver, channel, frequency_hz] => Some(TciMessage::Vfo {
            receiver: receiver.parse().ok()?,
            channel: Some(channel.parse().ok()?),
            frequency_hz: parse_frequency(frequency_hz)?,
        }),
        [receiver, frequency_hz] => Some(TciMessage::Vfo {
            receiver: receiver.parse().ok()?,
            channel: None,
            frequency_hz: parse_frequency(frequency_hz)?,
        }),
        _ => None,
    }
}

fn parse_modulation(args: &[&str]) -> Option<TciMessage> {
    match args {
        [receiver, mode] => Some(TciMessage::Modulation {
            receiver: receiver.parse().ok()?,
            mode: mode.trim().to_uppercase(),
        }),
        _ => None,
    }
}

fn parse_rx_smeter(args: &[&str]) -> Option<TciMessage> {
    match args {
        [receiver, channel, value] => Some(TciMessage::RxSmeter {
            receiver: receiver.parse().ok()?,
            channel: channel.parse().ok()?,
            value: value.parse().ok()?,
        }),
        _ => None,
    }
}

fn parse_tx_power(args: &[&str]) -> Option<TciMessage> {
    match args {
        [value] => Some(TciMessage::TxPower {
            value: value.parse().ok()?,
        }),
        _ => None,
    }
}

fn parse_frequency(value: &str) -> Option<u64> {
    if let Ok(freq) = value.parse::<u64>() {
        return Some(freq);
    }
    value
        .parse::<f64>()
        .ok()
        .filter(|freq| *freq >= 0.0)
        .map(|freq| freq.round() as u64)
}

#[cfg(test)]
mod tests {
    use std::net::SocketAddr;
    use std::sync::{
        atomic::{AtomicUsize, Ordering},
        Arc,
    };

    use futures_util::{SinkExt, StreamExt};
    use tokio::net::TcpListener;
    use tokio_tungstenite::{accept_async, tungstenite::Message};

    use super::{parse_message, TciController, TciMessage};
    use crate::rig::RigController;

    #[test]
    fn parses_vfo_messages_in_both_forms() {
        assert_eq!(
            parse_message("VFO:0,1,14074000"),
            Some(TciMessage::Vfo {
                receiver: 0,
                channel: Some(1),
                frequency_hz: 14_074_000,
            })
        );
        assert_eq!(
            parse_message("VFO:0,14074000"),
            Some(TciMessage::Vfo {
                receiver: 0,
                channel: None,
                frequency_hz: 14_074_000,
            })
        );
    }

    #[test]
    fn parses_mode_smeter_and_power_messages() {
        assert_eq!(
            parse_message("MODULATION:0,ft8"),
            Some(TciMessage::Modulation {
                receiver: 0,
                mode: "FT8".to_string(),
            })
        );
        assert_eq!(
            parse_message("RX_SMETER:0,1,-63"),
            Some(TciMessage::RxSmeter {
                receiver: 0,
                channel: 1,
                value: -63.0,
            })
        );
        assert_eq!(
            parse_message("TX_POWER:13.5"),
            Some(TciMessage::TxPower { value: 13.5 })
        );
    }

    async fn spawn_mock_tci(
        close_first_connection: bool,
    ) -> anyhow::Result<(SocketAddr, Arc<AtomicUsize>, tokio::task::JoinHandle<()>)> {
        let listener = TcpListener::bind("127.0.0.1:0").await?;
        let addr = listener.local_addr()?;
        let connections = Arc::new(AtomicUsize::new(0));
        let connections_for_task = Arc::clone(&connections);

        let handle = tokio::spawn(async move {
            loop {
                let Ok((stream, _)) = listener.accept().await else {
                    break;
                };
                let conn_no = connections_for_task.fetch_add(1, Ordering::SeqCst) + 1;
                let mut ws = match accept_async(stream).await {
                    Ok(ws) => ws,
                    Err(_) => continue,
                };

                if close_first_connection && conn_no == 1 {
                    let _ = ws.close(None).await;
                    continue;
                }

                let _ = ws
                    .send(Message::Text(
                        "PROTOCOL:ESDR,1.6;VFO:0,0,14074000;MODULATION:0,FT8;RX_SMETER:0,0,-72;TX_POWER:13.5;"
                            .into(),
                    ))
                    .await;

                while let Some(Ok(message)) = ws.next().await {
                    let Ok(text) = message.into_text() else {
                        continue;
                    };
                    for cmd in text
                        .split(';')
                        .map(str::trim)
                        .filter(|part| !part.is_empty())
                    {
                        let response = if cmd == "VFO:0,0" || cmd == "VFO:0,1" {
                            Some("VFO:0,0,14074000;".to_string())
                        } else if cmd == "MODULATION:0" {
                            Some("MODULATION:0,FT8;".to_string())
                        } else if cmd == "RX_SMETER:0,0" || cmd == "RX_SMETER:0,1" {
                            Some("RX_SMETER:0,0,-72;".to_string())
                        } else if let Some(freq) = cmd.strip_prefix("VFO:0,0,") {
                            Some(format!("VFO:0,0,{freq};"))
                        } else {
                            cmd.strip_prefix("MODULATION:0,").map(|mode| format!("MODULATION:0,{mode};"))
                        };

                        if let Some(response) = response {
                            let _ = ws.send(Message::Text(response)).await;
                        }
                    }
                }
            }
        });

        Ok((addr, connections, handle))
    }

    #[tokio::test]
    async fn connects_and_reads_cached_telemetry() {
        let (addr, _connections, handle) = spawn_mock_tci(false).await.unwrap();
        let controller = TciController::connect(addr.ip().to_string(), addr.port(), 0)
            .await
            .unwrap();

        assert_eq!(controller.get_frequency().await.unwrap(), 14_074_000);
        assert_eq!(controller.get_mode().await.unwrap(), "FT8");
        assert_eq!(controller.get_smeter().await.unwrap(), Some(-72.0));
        assert_eq!(controller.get_power().await.unwrap(), Some(13.5));

        handle.abort();
    }

    #[tokio::test]
    async fn reconnects_after_socket_drop() {
        let (addr, connections, handle) = spawn_mock_tci(true).await.unwrap();
        let controller = TciController::connect(addr.ip().to_string(), addr.port(), 0)
            .await
            .unwrap();

        // The mock server drops the first socket immediately, so the first read
        // should force a reconnect to the second accepted connection.
        assert_eq!(controller.get_frequency().await.unwrap(), 14_074_000);
        assert!(connections.load(Ordering::SeqCst) >= 2);

        handle.abort();
    }

    #[tokio::test]
    async fn set_frequency_and_mode_send_tci_commands() {
        let (addr, _connections, handle) = spawn_mock_tci(false).await.unwrap();
        let controller = TciController::connect(addr.ip().to_string(), addr.port(), 0)
            .await
            .unwrap();

        controller.set_frequency(7_074_000).await.unwrap();
        controller.set_mode("usb").await.unwrap();

        assert_eq!(controller.get_frequency().await.unwrap(), 7_074_000);
        assert_eq!(controller.get_mode().await.unwrap(), "USB");

        handle.abort();
    }
}
