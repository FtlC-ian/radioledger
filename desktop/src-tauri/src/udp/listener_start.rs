use std::sync::{
    atomic::{AtomicBool, AtomicU64},
    Arc,
};

use tokio::net::UdpSocket;
use tracing::info;

use super::{
    bind_multicast_socket, spawn_listener, spawn_n1mm_listener, RecentDecodeStore, SourceHandle,
};
use crate::{config::UdpSourceConfig, db::Database, error::AppError};

pub(super) struct PreparedListener {
    socket: UdpSocket,
    port: u16,
    bind: String,
    multicast_group: Option<String>,
}

pub(super) async fn prepare_wsjtx_listener(
    config: &UdpSourceConfig,
    requested_port: Option<u16>,
) -> Result<PreparedListener, AppError> {
    let bind_port = requested_port.unwrap_or(config.port);
    let multicast_group = config
        .multicast_group
        .clone()
        .filter(|group| !group.is_empty());

    let (socket, bind) = if let Some(ref group) = multicast_group {
        let socket = bind_multicast_socket(bind_port, group).map_err(|e| {
            AppError::Udp(format!(
                "Failed to set up multicast socket on port {bind_port}: {e}"
            ))
        })?;
        info!("WSJT-X UDP listener bound to 0.0.0.0:{bind_port} (multicast group {group})");
        (socket, "0.0.0.0".to_string())
    } else {
        prepare_unicast_socket("WSJT-X", &config.bind, bind_port).await?
    };

    Ok(PreparedListener {
        socket,
        port: bind_port,
        bind,
        multicast_group,
    })
}

pub(super) async fn prepare_unicast_listener(
    label: &str,
    config: &UdpSourceConfig,
    requested_port: Option<u16>,
) -> Result<PreparedListener, AppError> {
    let bind_port = requested_port.unwrap_or(config.port);
    let (socket, bind) = prepare_unicast_socket(label, &config.bind, bind_port).await?;

    Ok(PreparedListener {
        socket,
        port: bind_port,
        bind,
        multicast_group: None,
    })
}

pub(super) fn start_packet_listener(
    db: &Arc<Database>,
    recent_decodes: &Arc<tokio::sync::Mutex<RecentDecodeStore>>,
    handle: &mut SourceHandle,
    prepared: PreparedListener,
    app: tauri::AppHandle,
    source: &'static str,
) {
    let PreparedListener {
        socket,
        port,
        bind,
        multicast_group,
    } = prepared;

    let rx = handle.record_started(port, bind, multicast_group);
    let listening_flag: Arc<AtomicBool> = Arc::clone(&handle.listening);
    let counter: Arc<AtomicU64> = Arc::clone(&handle.packets_received);

    spawn_listener(
        socket,
        Arc::clone(db),
        Arc::clone(recent_decodes),
        listening_flag,
        counter,
        rx,
        app,
        source,
    );
}

pub(super) fn start_n1mm_listener(
    db: &Arc<Database>,
    handle: &mut SourceHandle,
    prepared: PreparedListener,
) {
    let PreparedListener {
        socket,
        port,
        bind,
        multicast_group,
    } = prepared;

    let rx = handle.record_started(port, bind, multicast_group);
    let listening_flag: Arc<AtomicBool> = Arc::clone(&handle.listening);
    let counter: Arc<AtomicU64> = Arc::clone(&handle.packets_received);

    spawn_n1mm_listener(socket, Arc::clone(db), listening_flag, counter, rx);
}

async fn prepare_unicast_socket(
    label: &str,
    bind_ip: &str,
    bind_port: u16,
) -> Result<(UdpSocket, String), AppError> {
    let bind_addr = format!("{bind_ip}:{bind_port}");
    let socket = UdpSocket::bind(&bind_addr)
        .await
        .map_err(|e| AppError::Udp(format!("Failed to bind {label} socket on {bind_addr}: {e}")))?;
    info!("{label} UDP listener bound to {bind_addr}");
    Ok((socket, bind_ip.to_string()))
}
