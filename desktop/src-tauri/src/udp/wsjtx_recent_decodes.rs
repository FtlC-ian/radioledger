use std::sync::Arc;

use crate::db::Database;

use super::{RecentDecode, RecentDecodeStore, EVENT_WSJTX_DECODE_LIST_CHANGED};

pub(crate) async fn emit_recent_decodes(
    app: &tauri::AppHandle,
    store: &Arc<tokio::sync::Mutex<RecentDecodeStore>>,
    db: &Database,
) -> anyhow::Result<()> {
    use tauri::Emitter;

    let payload = snapshot_recent_decodes(store, db).await?;
    app.emit(EVENT_WSJTX_DECODE_LIST_CHANGED, &payload)?;
    Ok(())
}

pub(crate) async fn snapshot_recent_decodes(
    store: &Arc<tokio::sync::Mutex<RecentDecodeStore>>,
    db: &Database,
) -> anyhow::Result<Vec<RecentDecode>> {
    let records = {
        let mut store = store.lock().await;
        store.prune();
        store.items.clone()
    };

    let mut items = Vec::with_capacity(records.len());
    for record in records {
        let log_status = match record.callsign.as_deref() {
            Some(callsign) => {
                db.decode_log_status(callsign, record.band.as_deref(), record.mode.as_deref())?
            }
            None => crate::db::DecodeLogStatus {
                state: crate::db::DecodeMatchState::New,
                label: "Raw".to_string(),
                worked_count: 0,
                exact_match_count: 0,
                confirmed_match_count: 0,
                last_worked_at: None,
            },
        };
        items.push(RecentDecode {
            callsign: record.callsign,
            message: record.message,
            grid: record.grid,
            distance_km: record.distance_km,
            snr: record.snr,
            frequency_hz: record.frequency_hz,
            freq_mhz: record.frequency_hz.map(|hz| hz as f64 / 1_000_000.0),
            mode: record.mode,
            band: record.band,
            last_activity: record.last_activity_at.to_rfc3339(),
            source: record.source,
            log_status,
        });
    }

    Ok(items)
}
