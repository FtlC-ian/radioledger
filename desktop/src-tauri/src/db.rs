//! Local SQLite cache for offline operation.
//!
//! ## Design
//! - Encrypted with SQLCipher; the encryption key lives in the OS keychain.
//! - Tables: `qso_queue` (pending server sync), `settings`, `auth_tokens` metadata.
//! - On logout: delete the database file. The server is the source of truth.
//!
//! ## Security note
//! Tokens themselves are stored in the OS keychain, not in this database.
//! This table stores only non-sensitive sync metadata.

use anyhow::Context;
use chrono::{DateTime, Utc};
use rusqlite::{params, Connection};
use serde::{Deserialize, Serialize};
use std::path::PathBuf;
use std::sync::Mutex;
use tracing::{debug, info};

use crate::config;

/// Wrapper around the SQLite connection.
/// Uses a `Mutex<Connection>` because `Connection` is not `Send`.
pub struct Database {
    conn: Mutex<Connection>,
}

/// A QSO waiting to be synced to the server.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct QueuedQso {
    pub id: i64,
    /// Stable client-side UUID used for server-side deduplication.
    pub client_uuid: String,
    /// Full QSO data as a JSON blob (serialised from the WSJT-X message).
    pub data: String,
    /// Source that produced this QSO: "wsjtx", "js8call", "n1mm", "manual".
    pub source: String,
    /// UTC timestamp when the QSO was captured.
    pub created_at: DateTime<Utc>,
    /// Number of sync attempts so far.
    pub attempts: i32,
    /// UTC timestamp of the last sync attempt (null if never tried).
    pub last_attempt_at: Option<DateTime<Utc>>,
    /// Human-readable error from the last failed sync attempt.
    pub last_error: Option<String>,
}

/// A synced QSO that still needs LoTW submission (or retry).
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct PendingLotwQso {
    pub id: i64,
    pub client_uuid: String,
    pub data: String,
}

/// A QSO cached from the server logbook for offline viewing.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct CachedQso {
    pub id: i64,
    pub uuid: String,
    pub logbook_uuid: String,
    pub callsign: String,
    pub band: String,
    pub mode: String,
    pub datetime_on: String, // ISO 8601 UTC
    pub rst_sent: Option<String>,
    pub rst_rcvd: Option<String>,
    pub name: Option<String>,
    pub qth: Option<String>,
    pub gridsquare: Option<String>,
    pub dxcc: Option<i32>,
    pub country: Option<String>,
    pub cq_zone: Option<i32>,
    pub itu_zone: Option<i32>,
    pub continent: Option<String>,
    pub comment: Option<String>,
    pub notes: Option<String>,
    pub created_at: String,
    pub updated_at: String,
    pub synced_at: String, // when we cached it locally
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct CallsignHistoryItem {
    pub datetime_on: String,
    pub band: String,
    pub mode: String,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
#[serde(rename_all = "snake_case")]
pub enum DecodeMatchState {
    New,
    Needed,
    Worked,
    Confirmed,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct DecodeLogStatus {
    pub state: DecodeMatchState,
    pub label: String,
    pub worked_count: i64,
    pub exact_match_count: i64,
    pub confirmed_match_count: i64,
    pub last_worked_at: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct LotwStatusSummary {
    pub pending: i64,
    pub submitted: i64,
    pub confirmed: i64,
    pub rejected: i64,
}

impl Database {
    /// Open (or create) the local SQLite cache at `~/.radioledger/local.db`.
    ///
    /// In a production build this would use SQLCipher with a key retrieved from
    /// the OS keychain. For the skeleton we use plain rusqlite (bundled) so that
    /// `cargo check` passes without SQLCipher build dependencies. SQLCipher
    /// integration is tracked in TODO below.
    ///
    /// TODO: replace `rusqlite` with `rusqlite` + `sqlcipher` feature once the
    ///       CI environment has the necessary OpenSSL/SQLCipher headers.
    pub fn open() -> anyhow::Result<Self> {
        let dir = config::config_dir()?;
        std::fs::create_dir_all(&dir).context("Failed to create ~/.radioledger directory")?;

        let db_path = dir.join("local.db");
        info!("Opening local SQLite cache at {}", db_path.display());

        let conn = Connection::open(&db_path).context("Failed to open local SQLite database")?;

        // Enable WAL mode for better concurrent read performance.
        conn.execute_batch("PRAGMA journal_mode=WAL;")?;
        // Foreign key enforcement.
        conn.execute_batch("PRAGMA foreign_keys=ON;")?;

        let db = Database {
            conn: Mutex::new(conn),
        };
        db.migrate()?;
        Ok(db)
    }

    /// Delete the database file (called on logout). The user is offered a
    /// "Keep local copy" option before this is called.
    pub fn delete(path: PathBuf) -> anyhow::Result<()> {
        if path.exists() {
            std::fs::remove_file(&path).context("Failed to delete local database on logout")?;
            info!("Local SQLite database deleted on logout");
        }
        Ok(())
    }

    // -------------------------------------------------------------------------
    // Schema migrations (inline for the skeleton; replace with a migration
    // library such as `refinery` in production).
    // -------------------------------------------------------------------------

    fn migrate(&self) -> anyhow::Result<()> {
        let conn = self.conn.lock().unwrap();
        conn.execute_batch(SCHEMA_SQL)?;
        ensure_cached_qso_column(&conn, "name", "TEXT")?;
        ensure_cached_qso_column(&conn, "qth", "TEXT")?;
        ensure_cached_qso_column(&conn, "country", "TEXT")?;
        ensure_cached_qso_column(&conn, "cq_zone", "INTEGER")?;
        ensure_cached_qso_column(&conn, "itu_zone", "INTEGER")?;
        ensure_cached_qso_column(&conn, "continent", "TEXT")?;
        debug!("Local database schema applied");
        Ok(())
    }

    // -------------------------------------------------------------------------
    // QSO queue operations
    // -------------------------------------------------------------------------

    /// Queue a QSO for server sync. Returns the new row id.
    pub fn enqueue_qso(&self, client_uuid: &str, data: &str, source: &str) -> anyhow::Result<i64> {
        let conn = self.conn.lock().unwrap();
        conn.execute(
            "INSERT INTO qso_queue (client_uuid, data, source, created_at, attempts)
             VALUES (?1, ?2, ?3, ?4, 0)",
            params![client_uuid, data, source, Utc::now().to_rfc3339()],
        )?;
        let id = conn.last_insert_rowid();
        debug!("Queued QSO {client_uuid} (row {id})");
        Ok(id)
    }

    /// Queue an update for an already-synced server QSO.
    /// Replaces any older unsynced update for the same QSO so only the newest
    /// local edit is pushed upstream.
    pub fn enqueue_qso_update(
        &self,
        client_uuid: &str,
        qso_uuid: &str,
        data: &str,
        source: &str,
    ) -> anyhow::Result<i64> {
        let conn = self.conn.lock().unwrap();
        conn.execute(
            "DELETE FROM qso_queue
             WHERE synced_at IS NULL
               AND source = 'update'
               AND json_extract(data, '$.uuid') = ?1",
            params![qso_uuid],
        )?;
        conn.execute(
            "INSERT INTO qso_queue (client_uuid, data, source, created_at, attempts)
             VALUES (?1, ?2, ?3, ?4, 0)",
            params![client_uuid, data, source, Utc::now().to_rfc3339()],
        )?;
        Ok(conn.last_insert_rowid())
    }

    /// Return all pending QSOs (up to `limit`), ordered oldest-first.
    pub fn pending_qsos(&self, limit: usize) -> anyhow::Result<Vec<QueuedQso>> {
        let conn = self.conn.lock().unwrap();
        let mut stmt = conn.prepare(
            "SELECT id, client_uuid, data, source, created_at, attempts,
                    last_attempt_at, last_error
             FROM qso_queue
             WHERE synced_at IS NULL
             ORDER BY created_at ASC
             LIMIT ?1",
        )?;
        let rows = stmt.query_map(params![limit as i64], |row| {
            Ok(QueuedQso {
                id: row.get(0)?,
                client_uuid: row.get(1)?,
                data: row.get(2)?,
                source: row.get(3)?,
                created_at: row
                    .get::<_, String>(4)?
                    .parse::<DateTime<Utc>>()
                    .unwrap_or_else(|_| Utc::now()),
                attempts: row.get(5)?,
                last_attempt_at: row
                    .get::<_, Option<String>>(6)?
                    .and_then(|s| s.parse::<DateTime<Utc>>().ok()),
                last_error: row.get(7)?,
            })
        })?;
        Ok(rows.collect::<Result<Vec<_>, _>>()?)
    }

    /// Return the count of QSOs that have not yet been synced.
    pub fn pending_count(&self) -> anyhow::Result<i64> {
        let conn = self.conn.lock().unwrap();
        let count: i64 = conn.query_row(
            "SELECT COUNT(*) FROM qso_queue WHERE synced_at IS NULL",
            [],
            |r| r.get(0),
        )?;
        Ok(count)
    }

    /// Mark a queued QSO as successfully synced.
    pub fn mark_synced(&self, id: i64) -> anyhow::Result<()> {
        let conn = self.conn.lock().unwrap();
        conn.execute(
            "UPDATE qso_queue SET synced_at = ?1 WHERE id = ?2",
            params![Utc::now().to_rfc3339(), id],
        )?;
        Ok(())
    }

    /// Record a failed sync attempt with an error message.
    pub fn record_attempt_failure(&self, id: i64, error: &str) -> anyhow::Result<()> {
        let conn = self.conn.lock().unwrap();
        conn.execute(
            "UPDATE qso_queue
             SET attempts = attempts + 1,
                 last_attempt_at = ?1,
                 last_error = ?2
             WHERE id = ?3",
            params![Utc::now().to_rfc3339(), error, id],
        )?;
        Ok(())
    }

    // -------------------------------------------------------------------------
    // LoTW sync tracking
    // -------------------------------------------------------------------------

    /// Return synced QSOs that are pending LoTW submission (or were previously
    /// rejected and should be retried).
    pub fn pending_lotw_qsos(&self, limit: usize) -> anyhow::Result<Vec<PendingLotwQso>> {
        let conn = self.conn.lock().unwrap();
        let mut stmt = conn.prepare(
            "SELECT q.id, q.client_uuid, q.data
             FROM qso_queue q
             LEFT JOIN lotw_status l ON l.client_uuid = q.client_uuid
             WHERE q.synced_at IS NOT NULL
               AND COALESCE(l.status, 'pending') IN ('pending', 'rejected')
             ORDER BY q.created_at ASC
             LIMIT ?1",
        )?;

        let rows = stmt.query_map(params![limit as i64], |row| {
            Ok(PendingLotwQso {
                id: row.get(0)?,
                client_uuid: row.get(1)?,
                data: row.get(2)?,
            })
        })?;

        Ok(rows.collect::<Result<Vec<_>, _>>()?)
    }

    /// Return all synced QSOs (used for matching inbound LoTW confirmations).
    pub fn all_synced_qsos(&self) -> anyhow::Result<Vec<PendingLotwQso>> {
        let conn = self.conn.lock().unwrap();
        let mut stmt = conn.prepare(
            "SELECT id, client_uuid, data
             FROM qso_queue
             WHERE synced_at IS NOT NULL
             ORDER BY created_at ASC",
        )?;

        let rows = stmt.query_map([], |row| {
            Ok(PendingLotwQso {
                id: row.get(0)?,
                client_uuid: row.get(1)?,
                data: row.get(2)?,
            })
        })?;

        Ok(rows.collect::<Result<Vec<_>, _>>()?)
    }

    /// Upsert per-QSO LoTW status.
    pub fn set_lotw_status(
        &self,
        client_uuid: &str,
        status: &str,
        error: Option<&str>,
    ) -> anyhow::Result<()> {
        let now = Utc::now().to_rfc3339();
        let conn = self.conn.lock().unwrap();
        conn.execute(
            "INSERT INTO lotw_status (client_uuid, status, updated_at, submitted_at, confirmed_at, rejected_at, last_error)
             VALUES (?1, ?2, ?3,
                     CASE WHEN ?2 = 'submitted' THEN ?3 ELSE NULL END,
                     CASE WHEN ?2 = 'confirmed' THEN ?3 ELSE NULL END,
                     CASE WHEN ?2 = 'rejected' THEN ?3 ELSE NULL END,
                     ?4)
             ON CONFLICT(client_uuid) DO UPDATE SET
                 status = excluded.status,
                 updated_at = excluded.updated_at,
                 submitted_at = CASE
                   WHEN excluded.status = 'submitted' THEN COALESCE(lotw_status.submitted_at, excluded.updated_at)
                   ELSE lotw_status.submitted_at
                 END,
                 confirmed_at = CASE
                   WHEN excluded.status = 'confirmed' THEN COALESCE(lotw_status.confirmed_at, excluded.updated_at)
                   ELSE lotw_status.confirmed_at
                 END,
                 rejected_at = CASE
                   WHEN excluded.status = 'rejected' THEN excluded.updated_at
                   ELSE lotw_status.rejected_at
                 END,
                 last_error = excluded.last_error",
            params![client_uuid, status, now, error],
        )?;
        Ok(())
    }

    /// Aggregate LoTW status counters for UI status display.
    pub fn lotw_status_summary(&self) -> anyhow::Result<LotwStatusSummary> {
        let conn = self.conn.lock().unwrap();

        let pending: i64 = conn.query_row(
            "SELECT COUNT(*)
             FROM qso_queue q
             LEFT JOIN lotw_status l ON l.client_uuid = q.client_uuid
             WHERE q.synced_at IS NOT NULL
               AND COALESCE(l.status, 'pending') IN ('pending', 'rejected')",
            [],
            |r| r.get(0),
        )?;

        let submitted: i64 = conn.query_row(
            "SELECT COUNT(*) FROM lotw_status WHERE status = 'submitted'",
            [],
            |r| r.get(0),
        )?;

        let confirmed: i64 = conn.query_row(
            "SELECT COUNT(*) FROM lotw_status WHERE status = 'confirmed'",
            [],
            |r| r.get(0),
        )?;

        let rejected: i64 = conn.query_row(
            "SELECT COUNT(*) FROM lotw_status WHERE status = 'rejected'",
            [],
            |r| r.get(0),
        )?;

        Ok(LotwStatusSummary {
            pending,
            submitted,
            confirmed,
            rejected,
        })
    }

    // -------------------------------------------------------------------------
    // Cached QSOs (pulled from server logbook)
    // -------------------------------------------------------------------------

    /// Upsert a QSO from the server into the local cache.
    pub fn upsert_qso(&self, qso: &CachedQso) -> anyhow::Result<()> {
        let conn = self.conn.lock().unwrap();
        conn.execute(
            "INSERT INTO cached_qsos (
                uuid, logbook_uuid, callsign, band, mode, datetime_on,
                rst_sent, rst_rcvd, name, qth, gridsquare, dxcc, country,
                cq_zone, itu_zone, continent, comment, notes,
                created_at, updated_at, synced_at
             )
             VALUES (
                ?1, ?2, ?3, ?4, ?5, ?6,
                ?7, ?8, ?9, ?10, ?11, ?12, ?13,
                ?14, ?15, ?16, ?17, ?18,
                ?19, ?20, ?21
             )
             ON CONFLICT(uuid) DO UPDATE SET
                 logbook_uuid = excluded.logbook_uuid,
                 callsign = excluded.callsign,
                 band = excluded.band,
                 mode = excluded.mode,
                 datetime_on = excluded.datetime_on,
                 rst_sent = excluded.rst_sent,
                 rst_rcvd = excluded.rst_rcvd,
                 name = excluded.name,
                 qth = excluded.qth,
                 gridsquare = excluded.gridsquare,
                 dxcc = excluded.dxcc,
                 country = excluded.country,
                 cq_zone = excluded.cq_zone,
                 itu_zone = excluded.itu_zone,
                 continent = excluded.continent,
                 comment = excluded.comment,
                 notes = excluded.notes,
                 created_at = excluded.created_at,
                 updated_at = excluded.updated_at,
                 synced_at = excluded.synced_at",
            params![
                qso.uuid,
                qso.logbook_uuid,
                qso.callsign,
                qso.band,
                qso.mode,
                qso.datetime_on,
                qso.rst_sent,
                qso.rst_rcvd,
                qso.name,
                qso.qth,
                qso.gridsquare,
                qso.dxcc,
                qso.country,
                qso.cq_zone,
                qso.itu_zone,
                qso.continent,
                qso.comment,
                qso.notes,
                qso.created_at,
                qso.updated_at,
                qso.synced_at,
            ],
        )?;
        Ok(())
    }

    /// List locally-known QSOs (server cache + pending local queue) for the Log
    /// view, ordered by datetime_on descending.
    pub fn list_qsos(
        &self,
        limit: usize,
        offset: usize,
        callsign_filter: Option<&str>,
        band_filter: Option<&str>,
        mode_filter: Option<&str>,
    ) -> anyhow::Result<Vec<CachedQso>> {
        let conn = self.conn.lock().unwrap();
        let mut sql = String::from(
            "SELECT id, uuid, logbook_uuid, callsign, band, mode, datetime_on,
                    rst_sent, rst_rcvd, name, qth, gridsquare, dxcc, country,
                    cq_zone, itu_zone, continent, comment, notes,
                    created_at, updated_at, synced_at
             FROM (
                 SELECT
                     c.id AS id,
                     c.uuid AS uuid,
                     c.logbook_uuid AS logbook_uuid,
                     c.callsign AS callsign,
                     c.band AS band,
                     c.mode AS mode,
                     c.datetime_on AS datetime_on,
                     c.rst_sent AS rst_sent,
                     c.rst_rcvd AS rst_rcvd,
                     c.name AS name,
                     c.qth AS qth,
                     c.gridsquare AS gridsquare,
                     c.dxcc AS dxcc,
                     c.country AS country,
                     c.cq_zone AS cq_zone,
                     c.itu_zone AS itu_zone,
                     c.continent AS continent,
                     c.comment AS comment,
                     c.notes AS notes,
                     c.created_at AS created_at,
                     c.updated_at AS updated_at,
                     c.synced_at AS synced_at
                 FROM cached_qsos c
                 UNION ALL
                 SELECT
                     -q.id AS id,
                     q.client_uuid AS uuid,
                     COALESCE(json_extract(q.data, '$.logbook_uuid'), '') AS logbook_uuid,
                     UPPER(COALESCE(json_extract(q.data, '$.callsign'), '')) AS callsign,
                     COALESCE(json_extract(q.data, '$.band'), '') AS band,
                     COALESCE(json_extract(q.data, '$.mode'), '') AS mode,
                     COALESCE(json_extract(q.data, '$.datetime_on'), q.created_at) AS datetime_on,
                     json_extract(q.data, '$.rst_sent') AS rst_sent,
                     json_extract(q.data, '$.rst_rcvd') AS rst_rcvd,
                     json_extract(q.data, '$.desktop_meta.name') AS name,
                     json_extract(q.data, '$.desktop_meta.qth') AS qth,
                     COALESCE(json_extract(q.data, '$.gridsquare'), json_extract(q.data, '$.grid')) AS gridsquare,
                     json_extract(q.data, '$.dxcc') AS dxcc,
                     json_extract(q.data, '$.country') AS country,
                     json_extract(q.data, '$.cq_zone') AS cq_zone,
                     json_extract(q.data, '$.itu_zone') AS itu_zone,
                     json_extract(q.data, '$.continent') AS continent,
                     json_extract(q.data, '$.comment') AS comment,
                     json_extract(q.data, '$.notes') AS notes,
                     q.created_at AS created_at,
                     COALESCE(q.last_attempt_at, q.created_at) AS updated_at,
                     COALESCE(q.synced_at, q.created_at) AS synced_at
                 FROM qso_queue q
                 WHERE q.synced_at IS NULL
                   AND q.source != 'update'
             ) qso
             WHERE 1=1",
        );

        let mut params: Vec<Box<dyn rusqlite::types::ToSql>> = vec![];

        if let Some(call) = callsign_filter {
            if !call.is_empty() {
                sql.push_str(" AND qso.callsign LIKE ?");
                params.push(Box::new(format!("%{}%", call.to_uppercase())));
            }
        }

        if let Some(band) = band_filter {
            if !band.is_empty() {
                sql.push_str(" AND qso.band = ?");
                params.push(Box::new(band.to_string()));
            }
        }

        if let Some(mode) = mode_filter {
            if !mode.is_empty() {
                sql.push_str(" AND qso.mode = ?");
                params.push(Box::new(mode.to_string()));
            }
        }

        sql.push_str(" ORDER BY qso.datetime_on DESC LIMIT ? OFFSET ?");
        params.push(Box::new(limit as i64));
        params.push(Box::new(offset as i64));

        let param_refs: Vec<&dyn rusqlite::types::ToSql> =
            params.iter().map(|p| p.as_ref()).collect();
        let mut stmt = conn.prepare(&sql)?;
        let rows = stmt.query_map(param_refs.as_slice(), |row| {
            Ok(CachedQso {
                id: row.get(0)?,
                uuid: row.get(1)?,
                logbook_uuid: row.get(2)?,
                callsign: row.get(3)?,
                band: row.get(4)?,
                mode: row.get(5)?,
                datetime_on: row.get(6)?,
                rst_sent: row.get(7)?,
                rst_rcvd: row.get(8)?,
                name: row.get(9)?,
                qth: row.get(10)?,
                gridsquare: row.get(11)?,
                dxcc: row.get(12)?,
                country: row.get(13)?,
                cq_zone: row.get(14)?,
                itu_zone: row.get(15)?,
                continent: row.get(16)?,
                comment: row.get(17)?,
                notes: row.get(18)?,
                created_at: row.get(19)?,
                updated_at: row.get(20)?,
                synced_at: row.get(21)?,
            })
        })?;

        rows.collect::<Result<Vec<_>, _>>().map_err(Into::into)
    }

    /// Return the total count of locally-known QSOs.
    pub fn count_qsos(&self) -> anyhow::Result<i64> {
        let conn = self.conn.lock().unwrap();
        let count: i64 = conn.query_row(
            "SELECT COUNT(*)
             FROM (
                 SELECT uuid FROM cached_qsos
                 UNION ALL
                 SELECT client_uuid FROM qso_queue WHERE synced_at IS NULL AND source != 'update'
             )",
            [],
            |r| r.get(0),
        )?;
        Ok(count)
    }

    /// Return the count of locally-known QSOs matching optional filters.
    pub fn count_qsos_filtered(
        &self,
        callsign_filter: Option<&str>,
        band_filter: Option<&str>,
        mode_filter: Option<&str>,
    ) -> anyhow::Result<i64> {
        let conn = self.conn.lock().unwrap();
        let mut sql = String::from(
            "SELECT COUNT(*)
             FROM (
                 SELECT c.callsign AS callsign, c.band AS band, c.mode AS mode
                 FROM cached_qsos c
                 UNION ALL
                 SELECT
                     UPPER(COALESCE(json_extract(q.data, '$.callsign'), '')) AS callsign,
                     COALESCE(json_extract(q.data, '$.band'), '') AS band,
                     COALESCE(json_extract(q.data, '$.mode'), '') AS mode
                 FROM qso_queue q
                 WHERE q.synced_at IS NULL
                   AND q.source != 'update'
             ) qso
             WHERE 1=1",
        );
        let mut params: Vec<Box<dyn rusqlite::types::ToSql>> = vec![];

        if let Some(call) = callsign_filter {
            if !call.is_empty() {
                sql.push_str(" AND qso.callsign LIKE ?");
                params.push(Box::new(format!("%{}%", call.to_uppercase())));
            }
        }

        if let Some(band) = band_filter {
            if !band.is_empty() {
                sql.push_str(" AND qso.band = ?");
                params.push(Box::new(band.to_string()));
            }
        }

        if let Some(mode) = mode_filter {
            if !mode.is_empty() {
                sql.push_str(" AND qso.mode = ?");
                params.push(Box::new(mode.to_string()));
            }
        }

        let param_refs: Vec<&dyn rusqlite::types::ToSql> =
            params.iter().map(|p| p.as_ref()).collect();
        conn.query_row(&sql, param_refs.as_slice(), |row| row.get(0))
            .map_err(Into::into)
    }

    /// Return recent QSOs for a specific callsign from both the synced cache and
    /// the local pending queue.
    pub fn callsign_history(
        &self,
        callsign: &str,
        limit: usize,
    ) -> anyhow::Result<Vec<CallsignHistoryItem>> {
        let conn = self.conn.lock().unwrap();
        let normalized = callsign.trim().to_uppercase();
        let mut stmt = conn.prepare(
            "SELECT qso.datetime_on, qso.band, qso.mode
             FROM (
                 SELECT c.callsign, c.datetime_on, c.band, c.mode
                 FROM cached_qsos c
                 UNION ALL
                 SELECT
                     UPPER(COALESCE(json_extract(q.data, '$.callsign'), '')) AS callsign,
                     COALESCE(json_extract(q.data, '$.datetime_on'), q.created_at) AS datetime_on,
                     COALESCE(json_extract(q.data, '$.band'), '') AS band,
                     COALESCE(json_extract(q.data, '$.mode'), '') AS mode
                 FROM qso_queue q
                 WHERE q.synced_at IS NULL
                   AND q.source != 'update'
             ) qso
             WHERE qso.callsign = ?1
             ORDER BY qso.datetime_on DESC
             LIMIT ?2",
        )?;

        let rows = stmt.query_map(params![normalized, limit as i64], |row| {
            Ok(CallsignHistoryItem {
                datetime_on: row.get(0)?,
                band: row.get(1)?,
                mode: row.get(2)?,
            })
        })?;

        rows.collect::<Result<Vec<_>, _>>().map_err(Into::into)
    }

    /// Return log-match status for a potential WSJT-X decode using local cache.
    pub fn decode_log_status(
        &self,
        callsign: &str,
        band: Option<&str>,
        mode: Option<&str>,
    ) -> anyhow::Result<DecodeLogStatus> {
        let conn = self.conn.lock().unwrap();
        let normalized_callsign = callsign.trim().to_uppercase();
        let normalized_band = band
            .map(str::trim)
            .filter(|s| !s.is_empty())
            .map(str::to_string);
        let normalized_mode = mode
            .map(str::trim)
            .filter(|s| !s.is_empty())
            .map(|s| s.to_uppercase());

        let worked_count: i64 = conn.query_row(
            "SELECT COUNT(*)
             FROM (
                 SELECT c.callsign AS callsign
                 FROM cached_qsos c
                 UNION ALL
                 SELECT UPPER(COALESCE(json_extract(q.data, '$.callsign'), '')) AS callsign
                 FROM qso_queue q
                 WHERE q.source != 'update'
             ) qso
             WHERE qso.callsign = ?1",
            params![normalized_callsign],
            |row| row.get(0),
        )?;

        let exact_match_count: i64 = conn.query_row(
            "SELECT COUNT(*)
             FROM (
                 SELECT c.callsign AS callsign, c.band AS band, c.mode AS mode
                 FROM cached_qsos c
                 UNION ALL
                 SELECT
                     UPPER(COALESCE(json_extract(q.data, '$.callsign'), '')) AS callsign,
                     COALESCE(json_extract(q.data, '$.band'), '') AS band,
                     UPPER(COALESCE(json_extract(q.data, '$.mode'), '')) AS mode
                 FROM qso_queue q
                 WHERE q.source != 'update'
             ) qso
             WHERE qso.callsign = ?1
               AND (?2 IS NULL OR qso.band = ?2)
               AND (?3 IS NULL OR qso.mode = ?3)",
            params![normalized_callsign, normalized_band, normalized_mode],
            |row| row.get(0),
        )?;

        let confirmed_match_count: i64 = conn.query_row(
            "SELECT COUNT(*)
             FROM qso_queue q
             JOIN lotw_status l ON l.client_uuid = q.client_uuid
             WHERE UPPER(COALESCE(json_extract(q.data, '$.callsign'), '')) = ?1
               AND (?2 IS NULL OR COALESCE(json_extract(q.data, '$.band'), '') = ?2)
               AND (?3 IS NULL OR UPPER(COALESCE(json_extract(q.data, '$.mode'), '')) = ?3)
               AND l.status = 'confirmed'",
            params![normalized_callsign, normalized_band, normalized_mode],
            |row| row.get(0),
        )?;

        let last_worked_at: Option<String> = conn.query_row(
            "SELECT MAX(datetime_on)
             FROM (
                 SELECT c.callsign AS callsign, c.datetime_on AS datetime_on
                 FROM cached_qsos c
                 UNION ALL
                 SELECT
                     UPPER(COALESCE(json_extract(q.data, '$.callsign'), '')) AS callsign,
                     COALESCE(json_extract(q.data, '$.datetime_on'), q.created_at) AS datetime_on
                 FROM qso_queue q
                 WHERE q.source != 'update'
             ) qso
             WHERE qso.callsign = ?1",
            params![normalized_callsign],
            |row| row.get(0),
        )?;

        let (state, label) = if confirmed_match_count > 0 {
            (DecodeMatchState::Confirmed, "Confirmed")
        } else if exact_match_count > 0 {
            (DecodeMatchState::Worked, "Worked")
        } else if worked_count > 0 {
            (DecodeMatchState::Needed, "Needed")
        } else {
            (DecodeMatchState::New, "New")
        };

        Ok(DecodeLogStatus {
            state,
            label: label.to_string(),
            worked_count,
            exact_match_count,
            confirmed_match_count,
            last_worked_at,
        })
    }

    /// Clear all cached QSOs (called on logout or re-sync).
    pub fn clear_cache(&self) -> anyhow::Result<()> {
        let conn = self.conn.lock().unwrap();
        conn.execute("DELETE FROM cached_qsos", [])?;
        info!("Cached QSOs cleared");
        Ok(())
    }

    pub fn update_cached_qso(&self, qso: &CachedQso) -> anyhow::Result<()> {
        let conn = self.conn.lock().unwrap();
        conn.execute(
            "UPDATE cached_qsos
             SET callsign = ?1,
                 band = ?2,
                 mode = ?3,
                 datetime_on = ?4,
                 rst_sent = ?5,
                 rst_rcvd = ?6,
                 name = ?7,
                 qth = ?8,
                 gridsquare = ?9,
                 dxcc = ?10,
                 country = ?11,
                 cq_zone = ?12,
                 itu_zone = ?13,
                 continent = ?14,
                 comment = ?15,
                 notes = ?16,
                 updated_at = ?17
             WHERE uuid = ?18",
            params![
                qso.callsign,
                qso.band,
                qso.mode,
                qso.datetime_on,
                qso.rst_sent,
                qso.rst_rcvd,
                qso.name,
                qso.qth,
                qso.gridsquare,
                qso.dxcc,
                qso.country,
                qso.cq_zone,
                qso.itu_zone,
                qso.continent,
                qso.comment,
                qso.notes,
                qso.updated_at,
                qso.uuid,
            ],
        )?;
        Ok(())
    }

    pub fn get_cached_qso(&self, uuid: &str) -> anyhow::Result<Option<CachedQso>> {
        let conn = self.conn.lock().unwrap();
        let mut stmt = conn.prepare(
            "SELECT id, uuid, logbook_uuid, callsign, band, mode, datetime_on,
                    rst_sent, rst_rcvd, name, qth, gridsquare, dxcc, country,
                    cq_zone, itu_zone, continent, comment, notes,
                    created_at, updated_at, synced_at
             FROM cached_qsos
             WHERE uuid = ?1",
        )?;

        let result = stmt.query_row(params![uuid], |row| {
            Ok(CachedQso {
                id: row.get(0)?,
                uuid: row.get(1)?,
                logbook_uuid: row.get(2)?,
                callsign: row.get(3)?,
                band: row.get(4)?,
                mode: row.get(5)?,
                datetime_on: row.get(6)?,
                rst_sent: row.get(7)?,
                rst_rcvd: row.get(8)?,
                name: row.get(9)?,
                qth: row.get(10)?,
                gridsquare: row.get(11)?,
                dxcc: row.get(12)?,
                country: row.get(13)?,
                cq_zone: row.get(14)?,
                itu_zone: row.get(15)?,
                continent: row.get(16)?,
                comment: row.get(17)?,
                notes: row.get(18)?,
                created_at: row.get(19)?,
                updated_at: row.get(20)?,
                synced_at: row.get(21)?,
            })
        });

        match result {
            Ok(qso) => Ok(Some(qso)),
            Err(rusqlite::Error::QueryReturnedNoRows) => Ok(None),
            Err(e) => Err(e.into()),
        }
    }

    // -------------------------------------------------------------------------
    // Settings
    // -------------------------------------------------------------------------

    /// Get a settings value by key.
    pub fn get_setting(&self, key: &str) -> anyhow::Result<Option<String>> {
        let conn = self.conn.lock().unwrap();
        let result = conn.query_row(
            "SELECT value FROM settings WHERE key = ?1",
            params![key],
            |r| r.get(0),
        );
        match result {
            Ok(v) => Ok(Some(v)),
            Err(rusqlite::Error::QueryReturnedNoRows) => Ok(None),
            Err(e) => Err(e.into()),
        }
    }

    /// Set a settings value by key (upsert).
    pub fn set_setting(&self, key: &str, value: &str) -> anyhow::Result<()> {
        let conn = self.conn.lock().unwrap();
        conn.execute(
            "INSERT INTO settings (key, value, updated_at)
             VALUES (?1, ?2, ?3)
             ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at",
            params![key, value, Utc::now().to_rfc3339()],
        )?;
        Ok(())
    }
}

fn ensure_cached_qso_column(
    conn: &Connection,
    column_name: &str,
    sql_type: &str,
) -> anyhow::Result<()> {
    let mut stmt = conn.prepare("PRAGMA table_info(cached_qsos)")?;
    let rows = stmt.query_map([], |row| row.get::<_, String>(1))?;

    for existing in rows {
        if existing? == column_name {
            return Ok(());
        }
    }

    conn.execute(
        &format!("ALTER TABLE cached_qsos ADD COLUMN {column_name} {sql_type}"),
        [],
    )?;
    Ok(())
}

/// Schema DDL. Applied idempotently on every startup.
const SCHEMA_SQL: &str = "
-- Pending QSOs waiting to be synced to the RadioLedger server.
-- Rows are inserted by the UDP listener and deleted (or marked synced)
-- by the sync background task.
CREATE TABLE IF NOT EXISTS qso_queue (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    -- Stable client-side UUID for server deduplication (prevents double-sync).
    client_uuid     TEXT    NOT NULL UNIQUE,
    -- Full QSO data as JSON (fields from WSJT-X/JS8Call/N1MM+ message).
    data            TEXT    NOT NULL,
    -- Source application: 'wsjtx', 'js8call', 'n1mm', 'manual'.
    source          TEXT    NOT NULL DEFAULT 'wsjtx',
    -- UTC timestamp when the QSO was captured locally.
    created_at      TEXT    NOT NULL,
    -- Number of sync attempts (used for exponential backoff).
    attempts        INTEGER NOT NULL DEFAULT 0,
    -- UTC timestamp of the most recent sync attempt.
    last_attempt_at TEXT,
    -- Human-readable error from the last failed attempt.
    last_error      TEXT,
    -- UTC timestamp when the QSO was successfully synced. NULL = pending.
    synced_at       TEXT
);

-- Cached QSOs pulled from the server logbook for offline viewing.
CREATE TABLE IF NOT EXISTS cached_qsos (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    uuid            TEXT    NOT NULL UNIQUE,
    logbook_uuid    TEXT    NOT NULL,
    callsign        TEXT    NOT NULL,
    band            TEXT    NOT NULL,
    mode            TEXT    NOT NULL,
    datetime_on     TEXT    NOT NULL,  -- ISO 8601 UTC
    rst_sent        TEXT,
    rst_rcvd        TEXT,
    name            TEXT,
    qth             TEXT,
    gridsquare      TEXT,
    dxcc            INTEGER,
    country         TEXT,
    cq_zone         INTEGER,
    itu_zone        INTEGER,
    continent       TEXT,
    comment         TEXT,
    notes           TEXT,
    created_at      TEXT    NOT NULL,
    updated_at      TEXT    NOT NULL,
    synced_at       TEXT    NOT NULL   -- when we cached it locally
);

CREATE INDEX IF NOT EXISTS idx_cached_qsos_datetime ON cached_qsos(datetime_on DESC);
CREATE INDEX IF NOT EXISTS idx_cached_qsos_callsign ON cached_qsos(callsign);
CREATE INDEX IF NOT EXISTS idx_cached_qsos_band ON cached_qsos(band);
CREATE INDEX IF NOT EXISTS idx_cached_qsos_mode ON cached_qsos(mode);

-- Per-QSO LoTW status for the desktop bridge.
CREATE TABLE IF NOT EXISTS lotw_status (
    client_uuid  TEXT PRIMARY KEY REFERENCES qso_queue(client_uuid) ON DELETE CASCADE,
    status       TEXT NOT NULL CHECK (status IN ('pending', 'submitted', 'confirmed', 'rejected')),
    submitted_at TEXT,
    confirmed_at TEXT,
    rejected_at  TEXT,
    last_error   TEXT,
    updated_at   TEXT NOT NULL
);

-- Simple key/value store for application settings.
CREATE TABLE IF NOT EXISTS settings (
    key         TEXT PRIMARY KEY,
    value       TEXT NOT NULL,
    updated_at  TEXT NOT NULL
);
";

#[cfg(test)]
mod tests {
    use super::*;
    use rusqlite::Connection;

    fn test_db() -> Database {
        let conn = Connection::open_in_memory().unwrap();
        conn.execute_batch("PRAGMA foreign_keys=ON;").unwrap();
        conn.execute_batch(SCHEMA_SQL).unwrap();
        Database {
            conn: Mutex::new(conn),
        }
    }

    fn sample_cached_qso(uuid: &str, callsign: &str) -> CachedQso {
        CachedQso {
            id: 0,
            uuid: uuid.to_string(),
            logbook_uuid: "logbook-1".to_string(),
            callsign: callsign.to_string(),
            band: "20m".to_string(),
            mode: "FT8".to_string(),
            datetime_on: "2026-04-09T12:00:00Z".to_string(),
            rst_sent: Some("599".to_string()),
            rst_rcvd: Some("599".to_string()),
            name: Some("Pat".to_string()),
            qth: Some("Austin".to_string()),
            gridsquare: Some("EM10".to_string()),
            dxcc: Some(291),
            country: Some("United States".to_string()),
            cq_zone: Some(4),
            itu_zone: Some(8),
            continent: Some("NA".to_string()),
            comment: Some("comment".to_string()),
            notes: Some("notes".to_string()),
            created_at: "2026-04-09T12:00:00Z".to_string(),
            updated_at: "2026-04-09T12:00:00Z".to_string(),
            synced_at: "2026-04-09T12:00:00Z".to_string(),
        }
    }

    #[test]
    fn list_qsos_hides_pending_update_rows() {
        let db = test_db();
        db.upsert_qso(&sample_cached_qso("server-qso", "W1AW"))
            .unwrap();

        db.enqueue_qso_update(
            "client-update-1",
            "server-qso",
            &serde_json::json!({
                "uuid": "server-qso",
                "logbook_uuid": "logbook-1",
                "callsign": "K1ABC",
                "band": "20m",
                "mode": "FT8",
                "datetime_on": "2026-04-09T12:05:00Z"
            })
            .to_string(),
            "update",
        )
        .unwrap();

        let listed = db.list_qsos(20, 0, None, None, None).unwrap();
        assert_eq!(listed.len(), 1);
        assert_eq!(listed[0].uuid, "server-qso");

        let count = db.count_qsos_filtered(Some("K1ABC"), None, None).unwrap();
        assert_eq!(count, 0);
    }
}
