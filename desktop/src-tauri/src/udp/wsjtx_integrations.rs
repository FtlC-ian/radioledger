use anyhow::Context;
use tokio::net::UdpSocket;
use tracing::debug;

use super::QsoLogged;

/// Validate a parsed QSO and enqueue it for server sync.
pub(crate) fn validate_and_enqueue(
    qso: &QsoLogged,
    db: &crate::db::Database,
) -> anyhow::Result<()> {
    if !super::is_plausible_callsign(&qso.callsign) {
        anyhow::bail!("callsign '{}' failed plausibility checks", qso.callsign);
    }

    if qso.band == "unknown" {
        anyhow::bail!(
            "frequency {:.4} MHz is outside recognised amateur bands",
            qso.freq_mhz
        );
    }

    let datetime_on = super::parse_iso_utc(&qso.datetime_on)?;
    let now = chrono::Utc::now();
    if !super::is_within_24h(datetime_on, now) {
        anyhow::bail!(
            "datetime_on '{}' is not within 24 hours of now",
            qso.datetime_on
        );
    }

    if let Some(datetime_off_raw) = &qso.datetime_off {
        let datetime_off = super::parse_iso_utc(datetime_off_raw)?;
        if !super::is_within_24h(datetime_off, now) {
            anyhow::bail!(
                "datetime_off '{}' is not within 24 hours of now",
                datetime_off_raw
            );
        }
        if datetime_off < datetime_on {
            anyhow::bail!(
                "datetime_off '{}' is before datetime_on '{}'",
                datetime_off_raw,
                qso.datetime_on
            );
        }
    }

    let client_uuid = uuid::Uuid::new_v4().to_string();
    let data = serde_json::to_string(qso)?;

    db.enqueue_qso(&client_uuid, &data, &qso.source)?;
    tracing::info!(
        "Queued QSO with {} ({} / {} / {}) uuid={client_uuid}",
        qso.callsign,
        qso.band,
        qso.mode,
        qso.source
    );

    let data_clone = data.clone();
    let uuid_clone = client_uuid.clone();
    tauri::async_runtime::spawn(async move {
        if let Err(e) = try_immediate_post(&uuid_clone, &data_clone).await {
            debug!("Immediate POST skipped/failed ({e}); sync loop will retry");
        }
    });

    Ok(())
}

/// POST a single QSO to the RadioLedger API immediately.
/// Returns Err if not authenticated, config missing, or HTTP fails.
pub(crate) async fn try_immediate_post(client_uuid: &str, json_data: &str) -> anyhow::Result<()> {
    use crate::auth;

    let access_token = match auth::load_access_token()? {
        Some(t) => t,
        None => anyhow::bail!("not authenticated, skipping immediate POST"),
    };

    let cfg = crate::config::load()?;
    let logbook_uuid = crate::sync::get_primary_logbook_uuid(&cfg, &access_token).await?;
    let client = reqwest::Client::new();

    try_immediate_post_with_client(
        &client,
        &cfg.server.url,
        &logbook_uuid,
        &access_token,
        client_uuid,
        json_data,
    )
    .await
}

pub(crate) async fn try_immediate_post_with_client(
    client: &reqwest::Client,
    api_base: &str,
    logbook_uuid: &str,
    access_token: &str,
    client_uuid: &str,
    json_data: &str,
) -> anyhow::Result<()> {
    let url = format!(
        "{}/v1/logbooks/{}/qsos",
        api_base.trim_end_matches('/'),
        logbook_uuid
    );

    let mut payload: serde_json::Value = serde_json::from_str(json_data)?;
    let source = payload["source"].as_str().unwrap_or_default().to_string();
    crate::sync::hydrate_wsjtx_payload_if_needed(
        client,
        api_base,
        access_token,
        &source,
        client_uuid,
        &mut payload,
    )
    .await;
    payload["client_uuid"] = serde_json::Value::String(client_uuid.to_string());

    let resp = client
        .post(&url)
        .bearer_auth(access_token)
        .json(&payload)
        .send()
        .await?;

    if resp.status().is_success() {
        debug!("Immediate POST succeeded for QSO {client_uuid}");
        Ok(())
    } else {
        let status = resp.status();
        let body = resp.text().await.unwrap_or_default();
        anyhow::bail!("server returned {status}: {body}")
    }
}

/// Best-effort FT8Battle relay for WSJT-X logged-QSO packets.
///
/// Returns `Ok(None)` when relay is disabled, `Ok(Some(endpoint))` when the
/// datagram was forwarded successfully, and `Err` when relay was enabled but
/// forwarding failed.
pub(crate) async fn try_relay_to_ft8battle(packet: &[u8]) -> anyhow::Result<Option<String>> {
    let cfg = crate::config::load()?;
    if !cfg.udp.ft8battle.enabled {
        return Ok(None);
    }

    let endpoint = cfg.udp.ft8battle.endpoint.trim();
    if endpoint.is_empty() {
        anyhow::bail!("FT8Battle relay endpoint is empty")
    }

    relay_udp_packet(packet, endpoint).await?;
    Ok(Some(endpoint.to_string()))
}

pub(crate) async fn relay_udp_packet(packet: &[u8], endpoint: &str) -> anyhow::Result<()> {
    let mut resolved = tokio::net::lookup_host(endpoint)
        .await
        .with_context(|| format!("failed to resolve relay endpoint {endpoint}"))?;
    let addr = resolved
        .next()
        .with_context(|| format!("relay endpoint {endpoint} resolved to no addresses"))?;

    let bind_addr = if addr.is_ipv4() {
        "0.0.0.0:0"
    } else {
        "[::]:0"
    };
    let socket = UdpSocket::bind(bind_addr)
        .await
        .with_context(|| format!("failed to bind relay socket for {endpoint}"))?;
    let sent = socket
        .send_to(packet, addr)
        .await
        .with_context(|| format!("failed to send UDP relay packet to {endpoint}"))?;

    if sent != packet.len() {
        anyhow::bail!(
            "short UDP relay write to {endpoint}: sent {sent} of {} bytes",
            packet.len()
        );
    }

    Ok(())
}
