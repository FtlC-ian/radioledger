use bytes::Buf;
use std::net::SocketAddr;
use std::sync::{
    atomic::{AtomicBool, AtomicU64, Ordering},
    Arc,
};
use std::time::{Duration, Instant};
use tokio::net::UdpSocket;
use tracing::{debug, error, info, trace, warn};

use crate::db::Database;

use super::wsjtx_integrations::{try_relay_to_ft8battle, validate_and_enqueue};
use super::wsjtx_message_parser::{parse_decode_message, parse_qso_logged, parse_status_message};
use super::wsjtx_recent_decodes::emit_recent_decodes;
use super::wsjtx_wire::push_wsjtx_string;
use super::{
    is_allowed_message_type, RecentDecodeStore, MAX_ID_LEN, MSG_ADIF_RECORD, MSG_CLEAR,
    MSG_DECODE, MSG_HEARTBEAT, MSG_QSO_LOGGED, MSG_STATUS, RATE_LIMIT_PER_SEC,
    SUPPORTED_SCHEMAS, WSJTX_MAGIC, WSJTX_REPLY_ID,
};

#[allow(clippy::too_many_arguments)]
pub(super) async fn run_listener(
    socket: UdpSocket,
    db: Arc<Database>,
    recent_decodes: Arc<tokio::sync::Mutex<RecentDecodeStore>>,
    listening_flag: Arc<AtomicBool>,
    counter: Arc<AtomicU64>,
    mut shutdown_rx: tokio::sync::oneshot::Receiver<()>,
    app: tauri::AppHandle,
    source: &'static str,
) {
    let mut buf = [0u8; 65_536];
    let mut rl_window_start = Instant::now();
    let mut rl_window_qso_cnt: u64 = 0;

    loop {
        tokio::select! {
            _ = &mut shutdown_rx => {
                info!("{source} UDP listener received shutdown signal");
                break;
            }
            result = socket.recv_from(&mut buf) => {
                match result {
                    Ok((len, src)) => {
                        counter.fetch_add(1, Ordering::Relaxed);
                        let packet = &buf[..len];

                        let is_qso = len >= 12 && {
                            let t = u32::from_be_bytes(
                                packet[8..12].try_into().unwrap_or([0; 4])
                            );
                            t == MSG_QSO_LOGGED
                        };

                        if is_qso {
                            if rl_window_start.elapsed() >= Duration::from_secs(1) {
                                rl_window_start = Instant::now();
                                rl_window_qso_cnt = 0;
                            }
                            rl_window_qso_cnt += 1;
                            if rl_window_qso_cnt > RATE_LIMIT_PER_SEC {
                                warn!(
                                    "{source}: rate limit exceeded ({} QSO-logged msgs/s from {src}) , packet dropped",
                                    rl_window_qso_cnt
                                );
                                continue;
                            }
                        }

                        process_packet(packet, src, &socket, &db, &recent_decodes, &app, source).await;
                    }
                    Err(e) => error!("{source} UDP receive error: {e}"),
                }
            }
        }
    }

    listening_flag.store(false, Ordering::SeqCst);
    info!("{source} UDP listener exited");
}

pub(super) async fn process_packet(
    packet: &[u8],
    src: SocketAddr,
    socket: &UdpSocket,
    db: &Database,
    recent_decodes: &Arc<tokio::sync::Mutex<RecentDecodeStore>>,
    app: &tauri::AppHandle,
    source: &str,
) {
    if packet.len() < 16 {
        debug!(
            "{source}: ignoring short packet ({} bytes) from {src}",
            packet.len()
        );
        return;
    }

    let mut buf: &[u8] = packet;

    let magic = buf.get_u32();
    if magic != WSJTX_MAGIC {
        debug!("{source}: ignoring packet with unknown magic {magic:#010x} from {src}");
        return;
    }

    let schema = buf.get_u32();
    if !SUPPORTED_SCHEMAS.contains(&schema) {
        warn!("{source}: unsupported schema version {schema} from {src}");
        return;
    }

    let msg_type = buf.get_u32();
    if !is_allowed_message_type(msg_type) {
        debug!("{source}: ignoring unknown/custom message type {msg_type} from {src}");
        return;
    }

    let id_len = buf.get_u32() as usize;
    if id_len > MAX_ID_LEN {
        warn!("{source}: malformed packet from {src}: id_len {id_len} exceeds max {MAX_ID_LEN}");
        return;
    }

    if buf.remaining() < id_len {
        warn!("{source}: malformed packet from {src}: id_len {id_len} > remaining");
        return;
    }

    let source_id = match std::str::from_utf8(&buf[..id_len]) {
        Ok(s) => s,
        Err(_) => {
            warn!("{source}: malformed packet from {src}: non-UTF8 source id");
            return;
        }
    };

    if msg_type == MSG_DECODE {
        trace!("{source}: type={msg_type} id=\"{source_id}\" from {src}");
    } else {
        debug!("{source}: type={msg_type} id=\"{source_id}\" from {src}");
    }
    buf.advance(id_len);

    match msg_type {
        MSG_HEARTBEAT => {
            debug!("{source}: heartbeat from {src}");
            if source == "wsjtx" {
                if let Err(err) = send_wsjtx_heartbeat_reply(socket, src, schema).await {
                    debug!("{source}: heartbeat reply failed: {err}");
                }
            }
        }
        MSG_STATUS => {
            if source == "wsjtx" {
                match parse_status_message(buf) {
                    Ok(status) => {
                        let mut store = recent_decodes.lock().await;
                        store.wsjtx_status = status;
                    }
                    Err(err) => debug!("{source}: failed to parse status packet: {err}"),
                }
            } else {
                debug!("{source}: status from {src}");
            }
        }
        MSG_DECODE => {
            if source == "wsjtx" {
                let status_snapshot = {
                    let store = recent_decodes.lock().await;
                    store.wsjtx_status.clone()
                };
                match parse_decode_message(buf, source, &status_snapshot) {
                    Ok(Some(record)) => {
                        {
                            let mut store = recent_decodes.lock().await;
                            store.upsert(record);
                        }
                        if let Err(err) = emit_recent_decodes(app, recent_decodes, db).await {
                            debug!("{source}: failed to emit decode update: {err}");
                        }
                    }
                    Ok(None) => debug!("{source}: decode packet ignored"),
                    Err(err) => debug!("{source}: failed to parse decode packet: {err}"),
                }
            } else {
                debug!("{source}: decode from {src}");
            }
        }
        MSG_CLEAR => {
            if source == "wsjtx" {
                {
                    let mut store = recent_decodes.lock().await;
                    store.items.clear();
                }
                if let Err(err) = emit_recent_decodes(app, recent_decodes, db).await {
                    debug!("{source}: failed to emit after clear: {err}");
                }
            } else {
                debug!("{source}: clear from {src}");
            }
        }
        MSG_QSO_LOGGED => match parse_qso_logged(buf, source) {
            Ok(qso) => match validate_and_enqueue(&qso, db) {
                Ok(()) => {
                    if source == "wsjtx" {
                        let relay_packet = packet.to_vec();
                        let callsign = qso.callsign.clone();
                        let band = qso.band.clone();
                        let mode = qso.mode.clone();
                        tauri::async_runtime::spawn(async move {
                            match try_relay_to_ft8battle(&relay_packet).await {
                                Ok(Some(endpoint)) => info!(
                                    "wsjtx: relayed QSO {} ({} / {}) to FT8Battle via {}",
                                    callsign, band, mode, endpoint
                                ),
                                Ok(None) => {}
                                Err(err) => warn!(
                                    "wsjtx: FT8Battle relay failed for {} ({} / {}): {}",
                                    callsign, band, mode, err
                                ),
                            }
                        });
                        let _ = emit_recent_decodes(app, recent_decodes, db).await;
                    }
                }
                Err(e) => warn!("{source}: QSO enqueue failed: {e}"),
            },
            Err(e) => {
                warn!("{source}: failed to parse QSO Logged from {src}: {e}");
            }
        },
        MSG_ADIF_RECORD => debug!("{source}: ADIF record from {src} (not yet handled)"),
        other => debug!("{source}: unknown message type {other} from {src}; ignoring"),
    }
}

async fn send_wsjtx_heartbeat_reply(
    socket: &UdpSocket,
    target: SocketAddr,
    schema: u32,
) -> anyhow::Result<()> {
    let mut packet = Vec::new();
    packet.extend_from_slice(&WSJTX_MAGIC.to_be_bytes());
    packet.extend_from_slice(&schema.to_be_bytes());
    packet.extend_from_slice(&MSG_HEARTBEAT.to_be_bytes());
    push_wsjtx_string(&mut packet, WSJTX_REPLY_ID);
    packet.extend_from_slice(
        &(SUPPORTED_SCHEMAS.iter().copied().max().unwrap_or(schema)).to_be_bytes(),
    );
    push_wsjtx_string(&mut packet, env!("CARGO_PKG_VERSION"));
    push_wsjtx_string(&mut packet, "radioledger");
    socket.send_to(&packet, target).await?;
    Ok(())
}
