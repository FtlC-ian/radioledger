// Package sync provides River job workers for synchronizing QSOs with external
// ham radio log services (eQSL, ClubLog, LoTW, etc.).
//
// # Architecture
//
// The sync system uses River background workers for reliable, retryable job processing.
// Each external service has dedicated workers for upload and download operations.
//
// ## Workers
//
//   - EQSLUploadWorker: uploads pending QSOs from a user's logbooks to eQSL
//   - EQSLDownloadWorker: downloads incoming eQSLs and matches them to local QSOs
//   - ClubLogUploadWorker: uploads pending QSOs to Club Log
//   - ClubLogDeleteWorker: deletes a QSO from Club Log when it's deleted locally
//   - ClubLogPollWorker: polls Club Log for DXCC entity worked/confirmed status
//
// ## Circuit Breaker
//
// Each service has a Postgres-backed circuit breaker. After consecutive failures
// (default: 5), the circuit opens and skips sync for that service for a recovery
// timeout (default: 60s). This prevents hammering a downed service or bad credentials.
//
// State is persisted in sync_circuit_state and survives process restarts. Supports
// closed → open → half_open → closed transitions with a single in-flight probe.
//
// ## Rate Limiting
//
// Each service has a global, Postgres-backed rate limiter shared across all workers
// for that service, keyed by per-second time buckets in sync_rate_limit_window.
// eQSL: 1 RPS, Club Log: 5 RPS, QRZ: 2 RPS.
//
// ## Sync Status Tracking
//
// The sync_status table tracks per-QSO sync state for each service.
// Workers update it on success/failure with exponential backoff timing.
//
// ## Retry / Backoff
//
//   - On failure: retry_count incremented, next_retry_at = now + 2^retry_count * 30s + jitter
//   - Max retry delay: 6 hours (cap at 12 retries)
//   - Circuit breaker trips at 10 consecutive failures
//
// References:
//   - docs/SYNC_SERVICES.md
//   - SCHEMA.md § sync_status
package sync

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/FtlC-ian/radioledger/api/internal/crypto"
	"github.com/FtlC-ian/radioledger/api/internal/metrics"
	"github.com/FtlC-ian/radioledger/api/internal/services/clublog"
	"github.com/FtlC-ian/radioledger/api/internal/services/eqsl"
	adifpkg "github.com/FtlC-ian/radioledger/pkg/adif"
)

// infraOrFallback returns the initialized worker infrastructure.
func infraOrFallback(pool *pgxpool.Pool) *Infra {
	if inf := getInfra(); inf != nil {
		return inf
	}
	inf := NewInfra(pool, defaultInfraConfig())
	SetWorkerInfraForTests(inf)
	return inf
}

// nextRetryAt computes when to next retry after a failure using configured
// exponential backoff + jitter for the service.
func nextRetryAt(pool *pgxpool.Pool, service string, retryCount int16) (time.Time, bool) {
	return infraOrFallback(pool).NextRetryAt(service, retryCount)
}

// ──────────────────────────────────────────────────────────────────────────────
// ADIF Formatter for Sync
// ──────────────────────────────────────────────────────────────────────────────

// pendingQSORow holds the data fetched from the DB for a pending sync QSO.
type pendingQSORow struct {
	SyncID          int64
	QSOID           int64
	RetryCount      int16
	SyncStatus      string
	RemoteID        *string
	Callsign        string
	Band            string
	Mode            string
	Submode         *string
	DatetimeOn      time.Time
	RstSent         *string
	RstRcvd         *string
	Gridsquare      *string
	MyGridsquare    *string
	Name            *string
	StationCallsign *string
	FrequencyHz     *int64
	UserID          int64
}

// qsosToADIF formats a slice of pending QSO rows as an ADIF string for upload.
func qsosToADIF(rows []pendingQSORow) (string, error) {
	var buf bytes.Buffer
	w := adifpkg.NewWriter(&buf)

	for _, row := range rows {
		rec := adifpkg.Record{}
		rec.Fields = []adifpkg.Field{
			{Name: "CALL", Value: row.Callsign},
			{Name: "BAND", Value: row.Band},
			{Name: "MODE", Value: row.Mode},
			{Name: "QSO_DATE", Value: row.DatetimeOn.UTC().Format("20060102")},
			{Name: "TIME_ON", Value: row.DatetimeOn.UTC().Format("1504")},
		}
		if row.RstSent != nil {
			rec.Fields = append(rec.Fields, adifpkg.Field{Name: "RST_SENT", Value: *row.RstSent})
		}
		if row.RstRcvd != nil {
			rec.Fields = append(rec.Fields, adifpkg.Field{Name: "RST_RCVD", Value: *row.RstRcvd})
		}
		if row.Gridsquare != nil {
			rec.Fields = append(rec.Fields, adifpkg.Field{Name: "GRIDSQUARE", Value: *row.Gridsquare})
		}
		if row.MyGridsquare != nil {
			rec.Fields = append(rec.Fields, adifpkg.Field{Name: "MY_GRIDSQUARE", Value: *row.MyGridsquare})
		}
		if row.StationCallsign != nil {
			rec.Fields = append(rec.Fields, adifpkg.Field{Name: "STATION_CALLSIGN", Value: *row.StationCallsign})
		}
		if row.FrequencyHz != nil {
			mhz := float64(*row.FrequencyHz) / 1_000_000.0
			rec.Fields = append(rec.Fields, adifpkg.Field{Name: "FREQ", Value: fmt.Sprintf("%.6f", mhz)})
		}
		adifpkg.CanonicalizeRecordMode(&rec)

		if err := w.WriteRecord(&rec); err != nil {
			return "", fmt.Errorf("write ADIF record: %w", err)
		}
	}

	return buf.String(), nil
}

// ──────────────────────────────────────────────────────────────────────────────
// DB helpers (raw SQL, worker role has cross-tenant read access)
// ──────────────────────────────────────────────────────────────────────────────

const fetchPendingSQL = `
SELECT
    ss.id,
    ss.qso_id,
    ss.retry_count,
    ss.status,
    ss.remote_id,
    q.callsign,
    q.band,
    q.mode,
    q.datetime_on,
    q.rst_sent,
    q.rst_rcvd,
    q.gridsquare,
    q.my_gridsquare,
    q.name,
    q.station_callsign,
    q.frequency_hz,
    lb.user_id
FROM sync_status ss
JOIN qsos q ON q.id = ss.qso_id
JOIN logbooks lb ON lb.id = q.logbook_id
WHERE ss.service = $1
  AND lb.user_id = $2
  AND ss.status IN ('pending', 'dirty', 'error')
  AND (ss.next_retry_at IS NULL OR ss.next_retry_at <= NOW())
  AND q.deleted_at IS NULL
ORDER BY ss.created_at ASC
LIMIT $3
`

func fetchPendingQSOs(ctx context.Context, pool *pgxpool.Pool, service string, userID int64, limit int) ([]pendingQSORow, error) {
	rows, err := pool.Query(ctx, fetchPendingSQL, service, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("query pending QSOs: %w", err)
	}
	defer rows.Close()

	var result []pendingQSORow
	for rows.Next() {
		var r pendingQSORow
		var datetimeOn pgtype.Timestamptz
		err := rows.Scan(
			&r.SyncID, &r.QSOID, &r.RetryCount,
			&r.SyncStatus, &r.RemoteID,
			&r.Callsign, &r.Band, &r.Mode,
			&datetimeOn,
			&r.RstSent, &r.RstRcvd,
			&r.Gridsquare, &r.MyGridsquare,
			&r.Name, &r.StationCallsign,
			&r.FrequencyHz,
			&r.UserID,
		)
		if err != nil {
			return nil, fmt.Errorf("scan pending QSO row: %w", err)
		}
		if datetimeOn.Valid {
			r.DatetimeOn = datetimeOn.Time
		}
		result = append(result, r)
	}
	return result, rows.Err()
}

func markUploaded(ctx context.Context, pool *pgxpool.Pool, syncID int64, remoteID string) error {
	_, err := pool.Exec(ctx, `
		UPDATE sync_status SET
			status = 'uploaded',
			last_synced_at = NOW(),
			remote_id = $2,
			error_message = NULL,
			last_error_code = NULL,
			retry_count = 0,
			next_retry_at = NULL,
			updated_at = NOW()
		WHERE id = $1
	`, syncID, remoteID)
	return err
}

func markSyncError(ctx context.Context, pool *pgxpool.Pool, service string, syncID int64, retryCount int16, errMsg, errCode string) error {
	nextRetry, ok := nextRetryAt(pool, service, retryCount)
	if !ok {
		nextRetry = time.Now().Add(5 * time.Minute)
	}
	_, err := pool.Exec(ctx, `
		UPDATE sync_status SET
			status = 'error',
			error_message = $2,
			last_error_code = $3,
			retry_count = retry_count + 1,
			next_retry_at = $4,
			updated_at = NOW()
		WHERE id = $1
	`, syncID, errMsg, errCode, nextRetry)
	return err
}

// markAllPendingFailed marks all pending/dirty sync_status rows for a user+service as error.
// Used for permanent failures like bad credentials where retrying won't help.
func markAllPendingFailed(ctx context.Context, pool *pgxpool.Pool, service string, userID int64, errMsg string) (int64, error) {
	tag, err := pool.Exec(ctx, `
		UPDATE sync_status SET
			status = 'error',
			error_message = $3,
			last_error_code = 'permanent_failure',
			next_retry_at = NULL,
			updated_at = NOW()
		WHERE service = $1
		  AND qso_id IN (
			SELECT q.id
			FROM qsos q
			JOIN logbooks lb ON lb.id = q.logbook_id
			WHERE lb.user_id = $2
		  )
		  AND status IN ('pending', 'dirty')
	`, service, userID, errMsg)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// CancelPendingSync marks all pending/dirty sync_status rows for a user+service
// as cancelled/skipped by user and returns the number of affected rows.
func CancelPendingSync(ctx context.Context, pool *pgxpool.Pool, userID int64, service string) (int64, error) {
	tag, err := pool.Exec(ctx, `
		UPDATE sync_status ss SET
			status = 'skipped',
			error_message = 'cancelled by user',
			last_error_code = 'cancelled_by_user',
			next_retry_at = NULL,
			updated_at = NOW()
		FROM qsos q
		JOIN logbooks lb ON lb.id = q.logbook_id
		WHERE ss.qso_id = q.id
		  AND lb.user_id = $1
		  AND ss.service = $2
		  AND ss.status IN ('pending', 'dirty')
		  AND q.deleted_at IS NULL
	`, userID, service)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func markSyncConfirmed(ctx context.Context, pool *pgxpool.Pool, qsoID int64, service string) error {
	_, err := pool.Exec(ctx, `
		UPDATE sync_status SET
			status = 'confirmed',
			last_synced_at = NOW(),
			error_message = NULL,
			updated_at = NOW()
		WHERE qso_id = $1 AND service = $2
	`, qsoID, service)
	return err
}

// decryptServiceCredentials retrieves and decrypts credentials for a user+service.
// Returns (nil, nil) if no credentials are configured.
func decryptServiceCredentials(ctx context.Context, pool *pgxpool.Pool, keyring *crypto.Keyring, userID int64, service string) ([]byte, error) {
	row := pool.QueryRow(ctx, `
		SELECT credentials, key_version
		FROM user_service_credentials
		WHERE user_id = $1 AND service = $2 AND is_active = TRUE
	`, userID, service)

	var ciphertext []byte
	var keyVersion int32
	if err := row.Scan(&ciphertext, &keyVersion); errors.Is(err, pgx.ErrNoRows) {
		return nil, nil // not configured
	} else if err != nil {
		return nil, fmt.Errorf("fetch credentials: %w", err)
	}

	if keyring == nil {
		return nil, fmt.Errorf("keyring not configured")
	}
	plaintext, err := keyring.Decrypt(userID, keyVersion, ciphertext)
	if err != nil {
		return nil, fmt.Errorf("decrypt credentials: %w", err)
	}
	return plaintext, nil
}

// ──────────────────────────────────────────────────────────────────────────────
// Club Log Upload Worker
// ──────────────────────────────────────────────────────────────────────────────

// ClubLogUploadArgs is the River job payload for a Club Log upload batch.
type ClubLogUploadArgs struct {
	UserID int64 `json:"user_id"`
}

// Kind returns the unique River job kind for Club Log uploads.
func (ClubLogUploadArgs) Kind() string { return "clublog_upload" }

// ClubLogUploadWorker uploads pending QSOs to Club Log.
type ClubLogUploadWorker struct {
	river.WorkerDefaults[ClubLogUploadArgs]
	Pool          *pgxpool.Pool
	Keyring       *crypto.Keyring
	ClubLogAPIKey string
}

// Work executes the Club Log upload job for a specific user.
func (w *ClubLogUploadWorker) Work(ctx context.Context, job *river.Job[ClubLogUploadArgs]) error {
	userID := job.Args.UserID
	log := slog.With(slog.Int64("user_id", userID), slog.String("service", "clublog"))

	infra := infraOrFallback(w.Pool)
	allowed, retryAfter, err := infra.AllowCircuit(ctx, "clublog")
	if err != nil {
		return fmt.Errorf("clublog circuit check: %w", err)
	}
	if !allowed {
		log.WarnContext(ctx, "clublog circuit breaker open, requeueing", slog.Duration("retry_after", retryAfter))
		return river.JobSnooze(retryAfter)
	}

	plaintext, err := decryptServiceCredentials(ctx, w.Pool, w.Keyring, userID, "clublog")
	if err != nil {
		msg := "Club Log credentials could not be decrypted. Re-save in Settings."
		if _, markErr := markAllPendingFailed(ctx, w.Pool, "clublog", userID, msg); markErr != nil {
			log.ErrorContext(ctx, "failed to mark pending rows after credential error", slog.String("error", markErr.Error()))
		}
		log.ErrorContext(ctx, "failed to load clublog credentials; marked pending as permanent failure", slog.String("error", err.Error()))
		return nil
	}
	if plaintext == nil {
		msg := "No Club Log credentials configured. Add credentials in Settings to sync."
		if _, markErr := markAllPendingFailed(ctx, w.Pool, "clublog", userID, msg); markErr != nil {
			log.ErrorContext(ctx, "failed to mark pending rows for missing credentials", slog.String("error", markErr.Error()))
		}
		log.WarnContext(ctx, "no clublog credentials configured; marked pending as permanent failure")
		return nil
	}

	creds, err := clublog.DecodeCredentials(plaintext)
	if err != nil {
		msg := "Invalid Club Log credentials. Re-save your email/password/callsign in Settings."
		if _, markErr := markAllPendingFailed(ctx, w.Pool, "clublog", userID, msg); markErr != nil {
			log.ErrorContext(ctx, "failed to mark pending rows after decode error", slog.String("error", markErr.Error()))
		}
		log.ErrorContext(ctx, "failed to decode clublog credentials; marked pending as permanent failure", slog.String("error", err.Error()))
		return nil
	}

	pending, err := fetchPendingQSOs(ctx, w.Pool, "clublog", userID, 500)
	if err != nil {
		return fmt.Errorf("fetch pending clublog qsos: %w", err)
	}
	if len(pending) == 0 {
		return nil
	}

	apiKey := strings.TrimSpace(w.ClubLogAPIKey)
	if apiKey == "" {
		msg := "Club Log sync is unavailable: server missing CLUBLOG_API_KEY configuration."
		if _, markErr := markAllPendingFailed(ctx, w.Pool, "clublog", userID, msg); markErr != nil {
			log.ErrorContext(ctx, "failed to mark pending rows when CLUBLOG_API_KEY is missing", slog.String("error", markErr.Error()))
		}
		log.ErrorContext(ctx, "clublog upload disabled: CLUBLOG_API_KEY is not configured")
		return nil
	}

	log.InfoContext(ctx, "clublog upload starting", slog.Int("count", len(pending)))

	client := clublog.New(apiKey, creds, log)

	const batchSize = 100
	for i := 0; i < len(pending); i += batchSize {
		end := i + batchSize
		if end > len(pending) {
			end = len(pending)
		}
		batch := pending[i:end]

		allowedRate, rateErr := infra.ConsumeRateLimit(ctx, "clublog")
		if rateErr != nil {
			return fmt.Errorf("clublog rate limit check: %w", rateErr)
		}
		if !allowedRate {
			return river.JobSnooze(1 * time.Second)
		}

		adifData, err := qsosToADIF(batch)
		if err != nil {
			continue
		}

		start := time.Now()
		result, uploadErr := client.UploadADIF(ctx, adifData)
		metrics.ObserveRiverJobDuration("clublog", time.Since(start))

		if uploadErr != nil {
			errText := uploadErr.Error()

			if isClubLogAuthOrPermanentError(errText) {
				msg := "Invalid Club Log credentials. Re-save your email/password/callsign in Settings."
				if _, markErr := markAllPendingFailed(ctx, w.Pool, "clublog", userID, msg); markErr != nil {
					log.ErrorContext(ctx, "failed to mark pending rows after auth failure", slog.String("error", markErr.Error()))
				}
				log.ErrorContext(ctx, "clublog authentication failure; marked pending as permanent failure", slog.String("error", errText))
				return nil
			}

			if isTransientSyncError(errText) {
				tripped, cbErr := infra.RecordFailure(ctx, "clublog", errText)
				if cbErr != nil {
					log.WarnContext(ctx, "failed to record clublog circuit failure", slog.String("error", cbErr.Error()))
				}
				if tripped {
					log.ErrorContext(ctx, "clublog circuit breaker tripped after consecutive failures")
				}
				metrics.IncRiverJobFailure("clublog")
				return fmt.Errorf("clublog transient upload failure: %w", uploadErr)
			}

			tripped, cbErr := infra.RecordFailure(ctx, "clublog", errText)
			if cbErr != nil {
				log.WarnContext(ctx, "failed to record clublog circuit failure", slog.String("error", cbErr.Error()))
			}
			if tripped {
				log.ErrorContext(ctx, "clublog circuit breaker tripped")
			}
			for _, row := range batch {
				_ = markSyncError(ctx, w.Pool, "clublog", row.SyncID, row.RetryCount, errText, "upload_failed")
			}
			metrics.IncRiverJobFailure("clublog")
			log.ErrorContext(ctx, "clublog upload batch failed", slog.String("error", errText))
			continue
		}

		if err := infra.RecordSuccess(ctx, "clublog"); err != nil {
			log.WarnContext(ctx, "failed to record clublog circuit success", slog.String("error", err.Error()))
		}
		log.InfoContext(ctx, "clublog batch uploaded",
			slog.Int("count", result.Count),
			slog.Int("submitted", len(batch)),
		)

		for _, row := range batch {
			if err := markUploaded(ctx, w.Pool, row.SyncID, ""); err != nil {
				log.WarnContext(ctx, "failed to mark clublog sync as uploaded",
					slog.Int64("sync_id", row.SyncID), slog.String("error", err.Error()))
			}
		}
	}

	return nil
}

// ──────────────────────────────────────────────────────────────────────────────
// Club Log Delete Worker
// ──────────────────────────────────────────────────────────────────────────────

// ClubLogDeleteArgs is the River job payload for deleting a QSO from Club Log.
// This is enqueued when a QSO is soft-deleted in RadioLedger.
type ClubLogDeleteArgs struct {
	UserID        int64  `json:"user_id"`
	TheirCallsign string `json:"their_callsign"`
	Band          string `json:"band"`
	Mode          string `json:"mode"`
	// DatetimeOn is the QSO datetime in RFC3339 format (UTC).
	DatetimeOn string `json:"datetime_on"`
}

// Kind returns the unique River job kind for Club Log deletions.
func (ClubLogDeleteArgs) Kind() string { return "clublog_delete" }

// ClubLogDeleteWorker deletes a QSO from Club Log when it's deleted locally.
type ClubLogDeleteWorker struct {
	river.WorkerDefaults[ClubLogDeleteArgs]
	Pool          *pgxpool.Pool
	Keyring       *crypto.Keyring
	ClubLogAPIKey string
}

// Work deletes the specified QSO from Club Log.
func (w *ClubLogDeleteWorker) Work(ctx context.Context, job *river.Job[ClubLogDeleteArgs]) error {
	userID := job.Args.UserID
	log := slog.With(slog.Int64("user_id", userID), slog.String("service", "clublog"))

	infra := infraOrFallback(w.Pool)
	allowed, retryAfter, err := infra.AllowCircuit(ctx, "clublog")
	if err != nil {
		return fmt.Errorf("clublog circuit check: %w", err)
	}
	if !allowed {
		log.WarnContext(ctx, "clublog circuit breaker open, requeueing", slog.Duration("retry_after", retryAfter))
		return river.JobSnooze(retryAfter)
	}

	plaintext, err := decryptServiceCredentials(ctx, w.Pool, w.Keyring, userID, "clublog")
	if err != nil || plaintext == nil {
		return err
	}

	creds, err := clublog.DecodeCredentials(plaintext)
	if err != nil {
		return fmt.Errorf("decode clublog credentials: %w", err)
	}

	dt, err := time.Parse(time.RFC3339, job.Args.DatetimeOn)
	if err != nil {
		return fmt.Errorf("parse datetime_on: %w", err)
	}

	allowedRate, rateErr := infra.ConsumeRateLimit(ctx, "clublog")
	if rateErr != nil {
		return fmt.Errorf("clublog rate limit check: %w", rateErr)
	}
	if !allowedRate {
		return river.JobSnooze(1 * time.Second)
	}

	apiKey := strings.TrimSpace(w.ClubLogAPIKey)
	if apiKey == "" {
		log.ErrorContext(ctx, "clublog delete skipped: CLUBLOG_API_KEY is not configured")
		return nil
	}

	client := clublog.New(apiKey, creds, log)
	start := time.Now()
	if err := client.DeleteQSO(ctx, job.Args.TheirCallsign, job.Args.Band, job.Args.Mode, dt); err != nil {
		_, _ = infra.RecordFailure(ctx, "clublog", err.Error())
		metrics.ObserveRiverJobDuration("clublog", time.Since(start))
		metrics.IncRiverJobFailure("clublog")
		return fmt.Errorf("clublog delete: %w", err)
	}

	metrics.ObserveRiverJobDuration("clublog", time.Since(start))
	if err := infra.RecordSuccess(ctx, "clublog"); err != nil {
		log.WarnContext(ctx, "failed to record clublog circuit success", slog.String("error", err.Error()))
	}
	log.InfoContext(ctx, "clublog qso deleted",
		slog.String("callsign", job.Args.TheirCallsign),
	)
	return nil
}

// ──────────────────────────────────────────────────────────────────────────────
// Club Log Poll Worker (DXCC status / confirmation polling)
// ──────────────────────────────────────────────────────────────────────────────

// ClubLogPollArgs is the River job payload for polling Club Log DXCC entity status.
// Run periodically (e.g. daily) per user with a Club Log integration configured.
type ClubLogPollArgs struct {
	// UserID is the owning user.
	UserID int64 `json:"user_id"`
}

// Kind returns the unique River job kind for Club Log DXCC status polls.
func (ClubLogPollArgs) Kind() string { return "clublog_poll" }

// ClubLogPollWorker polls Club Log for DXCC entity worked/confirmed status.
//
// Flow:
//  1. Load and decrypt Club Log credentials.
//  2. Call GetDXCCStatus to download the user's per-entity worked/confirmed map.
//  3. For each confirmed entity: find matching QSOs (by dxcc_entity foreign key)
//     and mark them confirmed in sync_status; upsert award_progress for DXCC tracking.
//  4. For worked (but not confirmed) entities: upsert award_progress as worked.
//
// DXCC entity IDs from Club Log follow ADIF/ARRL numbering and map directly
// to dxcc_entities.entity_id in RadioLedger's database — no translation needed.
type ClubLogPollWorker struct {
	river.WorkerDefaults[ClubLogPollArgs]
	Pool          *pgxpool.Pool
	Keyring       *crypto.Keyring
	ClubLogAPIKey string
}

// Work executes the Club Log DXCC status poll for a user.
func (w *ClubLogPollWorker) Work(ctx context.Context, job *river.Job[ClubLogPollArgs]) error {
	userID := job.Args.UserID
	log := slog.With(slog.Int64("user_id", userID), slog.String("service", "clublog"))

	infra := infraOrFallback(w.Pool)
	allowed, retryAfter, err := infra.AllowCircuit(ctx, "clublog")
	if err != nil {
		return fmt.Errorf("clublog poll circuit check: %w", err)
	}
	if !allowed {
		log.WarnContext(ctx, "clublog circuit breaker open (poll), requeueing", slog.Duration("retry_after", retryAfter))
		return river.JobSnooze(retryAfter)
	}

	plaintext, err := decryptServiceCredentials(ctx, w.Pool, w.Keyring, userID, "clublog")
	if err != nil || plaintext == nil {
		return err // nil plaintext = not configured, skip silently
	}

	creds, err := clublog.DecodeCredentials(plaintext)
	if err != nil {
		return fmt.Errorf("clublog poll: decode credentials: %w", err)
	}

	allowedRate, rateErr := infra.ConsumeRateLimit(ctx, "clublog")
	if rateErr != nil {
		return fmt.Errorf("clublog poll: rate limit check: %w", rateErr)
	}
	if !allowedRate {
		return river.JobSnooze(1 * time.Second)
	}

	apiKey := strings.TrimSpace(w.ClubLogAPIKey)
	if apiKey == "" {
		log.ErrorContext(ctx, "clublog poll skipped: CLUBLOG_API_KEY is not configured")
		return nil
	}

	// Club Log does not expose a per-QSO inbound confirmation feed (equivalent to
	// QRZ FETCH or eQSL inbox). We poll worked_entities.php as a best-effort signal
	// for DXCC-level progress only; QSO-level clublog_qsl_rcvd backfill is not supported.
	client := clublog.New(apiKey, creds, log)
	start := time.Now()
	dxccStatus, err := client.GetDXCCStatus(ctx)
	metrics.ObserveRiverJobDuration("clublog", time.Since(start))

	if err != nil {
		_, _ = infra.RecordFailure(ctx, "clublog", err.Error())
		metrics.IncRiverJobFailure("clublog")
		return fmt.Errorf("clublog poll: get dxcc status: %w", err)
	}

	if err := infra.RecordSuccess(ctx, "clublog"); err != nil {
		log.WarnContext(ctx, "clublog poll: failed to record circuit success", slog.String("error", err.Error()))
	}

	log.InfoContext(ctx, "clublog poll: dxcc status downloaded",
		slog.Int("entity_count", len(dxccStatus)),
	)

	workedCount := 0
	confirmedCount := 0

	for entityID, status := range dxccStatus {
		if status.Confirmed {
			// Mark all this user's QSOs for this DXCC entity as confirmed in sync_status.
			if err := markClubLogDXCCConfirmed(ctx, w.Pool, userID, entityID); err != nil {
				log.WarnContext(ctx, "clublog poll: failed to mark dxcc entity confirmed",
					slog.Int("entity_id", entityID), slog.String("error", err.Error()))
			}
			// Upsert award_progress: confirmed.
			if err := upsertDXCCAwardProgress(ctx, w.Pool, userID, entityID, true, "clublog"); err != nil {
				log.WarnContext(ctx, "clublog poll: failed to upsert award progress (confirmed)",
					slog.Int("entity_id", entityID), slog.String("error", err.Error()))
			}
			confirmedCount++
		} else if status.Worked {
			// Upsert award_progress: worked but not yet confirmed.
			if err := upsertDXCCAwardProgress(ctx, w.Pool, userID, entityID, false, ""); err != nil {
				log.WarnContext(ctx, "clublog poll: failed to upsert award progress (worked)",
					slog.Int("entity_id", entityID), slog.String("error", err.Error()))
			}
			workedCount++
		}
	}

	log.InfoContext(ctx, "clublog poll complete",
		slog.Int("worked", workedCount),
		slog.Int("confirmed", confirmedCount),
	)
	return nil
}

// markClubLogDXCCConfirmed updates sync_status rows for all QSOs belonging to
// userID that match the given DXCC entity to 'confirmed'.
// Matches via the qsos.dxcc foreign key (= ADIF entity ID = dxcc_entities.entity_id).
func markClubLogDXCCConfirmed(ctx context.Context, pool *pgxpool.Pool, userID int64, entityID int) error {
	_, err := pool.Exec(ctx, `
		UPDATE sync_status ss
		SET status = 'confirmed',
		    last_synced_at = NOW(),
		    error_message = NULL,
		    updated_at = NOW()
		FROM qsos q
		JOIN logbooks lb ON lb.id = q.logbook_id
		WHERE ss.qso_id = q.id
		  AND ss.service = 'clublog'
		  AND ss.status != 'confirmed'
		  AND q.dxcc = $1
		  AND lb.user_id = $2
		  AND q.deleted_at IS NULL
	`, entityID, userID)
	return err
}

// upsertDXCCAwardProgress inserts or updates an award_progress row for a DXCC entity.
// entity_key is the string form of the ADIF entity ID (e.g. "191").
// If confirmed is true, sets confirmed=TRUE, confirmation_method="clublog".
// If confirmed is false (worked only), inserts a worked row without overwriting
// an existing confirmed row.
//
// Uses an explicit UPDATE-then-INSERT pattern to safely handle the NULL band/mode
// case on all PostgreSQL versions (the UNIQUE constraint does not detect NULL conflicts
// on PostgreSQL < 15 without NULLS NOT DISTINCT).
func upsertDXCCAwardProgress(ctx context.Context, pool *pgxpool.Pool, userID int64, entityID int, confirmed bool, method string) error {
	entityKey := fmt.Sprintf("%d", entityID)

	if confirmed {
		// Try UPDATE first — if a row exists, update it to confirmed.
		tag, err := pool.Exec(ctx, `
			UPDATE award_progress SET
			    confirmed = TRUE,
			    confirmation_method = $3,
			    confirmed_via = 'clublog',
			    confirmed_at = COALESCE(confirmed_at, NOW()),
			    dirty = FALSE,
			    updated_at = NOW()
			WHERE user_id = $1
			  AND award_type = 'dxcc'
			  AND entity_key = $2
			  AND band IS NULL
			  AND mode IS NULL
		`, userID, entityKey, method)
		if err != nil {
			return err
		}
		if tag.RowsAffected() > 0 {
			return nil // updated existing row
		}
		// No existing row — INSERT.
		_, err = pool.Exec(ctx, `
			INSERT INTO award_progress
			    (user_id, award_type, entity_key, confirmed, confirmation_method, confirmed_via, confirmed_at, dirty, updated_at)
			VALUES ($1, 'dxcc', $2, TRUE, $3, 'clublog', NOW(), FALSE, NOW())
			ON CONFLICT DO NOTHING
		`, userID, entityKey, method)
		return err
	}

	// Worked but not confirmed: only insert if no row exists yet.
	// Never overwrite a confirmed row with a worked-only row.
	tag, err := pool.Exec(ctx, `
		UPDATE award_progress SET updated_at = updated_at
		WHERE user_id = $1
		  AND award_type = 'dxcc'
		  AND entity_key = $2
		  AND band IS NULL
		  AND mode IS NULL
	`, userID, entityKey)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		return nil // row already exists, do not downgrade a confirmed row
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO award_progress
		    (user_id, award_type, entity_key, confirmed, dirty, updated_at)
		VALUES ($1, 'dxcc', $2, FALSE, FALSE, NOW())
		ON CONFLICT DO NOTHING
	`, userID, entityKey)
	return err
}

// ──────────────────────────────────────────────────────────────────────────────
// Sync Trigger Helpers (called by handlers to enqueue batch sync jobs)
// ──────────────────────────────────────────────────────────────────────────────

// RiverInserter is the interface satisfied by *river.Client[pgx.Tx] for job insertion.
// Using an interface makes it easier to mock in tests.
type RiverInserter interface {
	Insert(context.Context, river.JobArgs, *river.InsertOpts) (*rivertype.JobInsertResult, error)
}

// userUniqueOpts returns InsertOpts that deduplicate per-user jobs by args and
// active job states. This prevents double-enqueueing when a user triggers the
// same sync operation multiple times in quick succession.
func userUniqueOpts() *river.InsertOpts {
	return &river.InsertOpts{
		UniqueOpts: river.UniqueOpts{
			ByArgs:  true,
			ByState: []rivertype.JobState{rivertype.JobStateAvailable, rivertype.JobStateRunning, rivertype.JobStateScheduled, rivertype.JobStatePending},
		},
	}
}

// EnqueueEQSLUpload enqueues an eQSL upload job for the given user.
// Called when the user manually triggers sync or after QSO creation.
func EnqueueEQSLUpload(ctx context.Context, rc RiverInserter, userID int64) error {
	if rc == nil {
		return fmt.Errorf("river inserter is nil")
	}
	_, err := rc.Insert(ctx, EQSLUploadArgs{UserID: userID}, userUniqueOpts())
	return err
}

// EnqueueEQSLDownload enqueues an eQSL inbox download job for the given user.
func EnqueueEQSLDownload(ctx context.Context, rc RiverInserter, userID int64) error {
	if rc == nil {
		return fmt.Errorf("river inserter is nil")
	}
	_, err := rc.Insert(ctx, EQSLDownloadArgs{UserID: userID}, userUniqueOpts())
	return err
}

// CountPendingEQSLQSOs returns the number of QSOs with pending eQSL sync status for a user.
// Used by the auto-sync scheduler to decide whether to enqueue an upload before the pull.
func CountPendingEQSLQSOs(ctx context.Context, pool *pgxpool.Pool, userID int64) (int, error) {
	if pool == nil {
		return 0, fmt.Errorf("pool is nil")
	}
	var count int
	err := pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM sync_status ss
		JOIN qsos q ON q.id = ss.qso_id
		JOIN logbooks lb ON lb.id = q.logbook_id
		WHERE lb.user_id = $1
		  AND ss.service = 'eqsl'
		  AND ss.status IN ('pending', 'dirty')
		  AND q.deleted_at IS NULL
	`, userID).Scan(&count)
	return count, err
}

// EnqueueClubLogUpload enqueues a Club Log upload job for the given user.
func EnqueueClubLogUpload(ctx context.Context, rc RiverInserter, userID int64) error {
	if rc == nil {
		return fmt.Errorf("river inserter is nil")
	}
	_, err := rc.Insert(ctx, ClubLogUploadArgs{UserID: userID}, userUniqueOpts())
	return err
}

// EnqueueClubLogDelete enqueues a Club Log deletion job when a QSO is deleted.
// Note: no deduplication — each deletion is for a distinct QSO.
func EnqueueClubLogDelete(ctx context.Context, rc RiverInserter, userID int64, callsign, band, mode, datetimeOn string) error {
	_, err := rc.Insert(ctx, ClubLogDeleteArgs{
		UserID:        userID,
		TheirCallsign: callsign,
		Band:          band,
		Mode:          mode,
		DatetimeOn:    datetimeOn,
	}, nil)
	return err
}

// EnqueueClubLogPoll enqueues a Club Log DXCC status poll job for the given user.
// Called periodically (e.g. daily) to update DXCC worked/confirmed status.
func EnqueueClubLogPoll(ctx context.Context, rc RiverInserter, userID int64) error {
	if rc == nil {
		return fmt.Errorf("river inserter is nil")
	}
	_, err := rc.Insert(ctx, ClubLogPollArgs{UserID: userID}, userUniqueOpts())
	return err
}

// InsertPendingSyncForQSO creates pending sync_status rows for a QSO for all enabled
// services. Called transactionally inside the QSO create/update handler.
// Uses INSERT ... ON CONFLICT DO NOTHING so it is idempotent.
//
// Special cases:
//   - "sota": only inserted when the QSO has my_sota_ref set (activator QSOs only).
func InsertPendingSyncForQSO(ctx context.Context, tx pgx.Tx, qsoID int64, services []string) error {
	for _, svc := range services {
		switch svc {
		case "sota":
			// Only create a SOTA sync row when my_sota_ref is populated.
			_, err := tx.Exec(ctx, `
				INSERT INTO sync_status (qso_id, service, status, updated_at)
				SELECT $1, 'sota', 'pending', NOW()
				FROM qsos
				WHERE id = $1
				  AND NULLIF(TRIM(my_sota_ref), '') IS NOT NULL
				ON CONFLICT (qso_id, service) DO NOTHING
			`, qsoID)
			if err != nil {
				return fmt.Errorf("insert pending sync for service sota: %w", err)
			}
		case "pota":
			_, err := tx.Exec(ctx, `
				INSERT INTO sync_status (qso_id, service, status, updated_at)
				SELECT $1, $2, 'pending', NOW()
				WHERE EXISTS (
					SELECT 1
					FROM qsos
					WHERE id = $1
					  AND deleted_at IS NULL
					  AND CARDINALITY(COALESCE(my_pota_refs, ARRAY[]::text[])) > 0
				)
				ON CONFLICT (qso_id, service) DO NOTHING
			`, qsoID, svc)
			if err != nil {
				return fmt.Errorf("insert pending sync for service %s: %w", svc, err)
			}
		default:
			_, err := tx.Exec(ctx, `
				INSERT INTO sync_status (qso_id, service, status, updated_at)
				VALUES ($1, $2, 'pending', NOW())
				ON CONFLICT (qso_id, service) DO NOTHING
			`, qsoID, svc)
			if err != nil {
				return fmt.Errorf("insert pending sync for service %s: %w", svc, err)
			}
		}
	}
	return nil
}

// BackfillCounts holds the result of a backfill operation.
type BackfillCounts struct {
	Confirmed int64
	Pending   int64
}

// BackfillSyncStatusForService creates sync_status rows for all of a user's QSOs that
// don't already have one for the given service. Called when a user first saves or
// activates credentials so existing QSOs appear on the dashboard immediately.
//
// Status is determined by QSO-level confirmation fields so already-confirmed QSOs
// are not re-pushed to the external service:
//   - eqsl:    eqsl_qsl_rcvd = 'Y'                       → confirmed
//   - lotw:    lotw_qsl_rcvd = 'Y' OR lotw_sent_at IS NOT NULL → confirmed
//   - qrz:     qrz_qsl_rcvd = 'Y'                        → confirmed
//   - clublog: no per-QSO confirmation field              → always pending
//   - pota:    only backfill QSOs with my_pota_refs set   → always pending
//   - sota:    only backfill QSOs with my_sota_ref set    → always pending
//   - others:                                             → always pending
//
// Uses INSERT ... ON CONFLICT DO NOTHING — safe to call repeatedly.
func BackfillSyncStatusForService(ctx context.Context, pool *pgxpool.Pool, userID int64, service string) (BackfillCounts, error) {
	var sql string

	switch service {
	case "eqsl":
		sql = `
			WITH inserted AS (
				INSERT INTO sync_status (qso_id, service, status, updated_at)
				SELECT
					q.id,
					'eqsl',
					CASE WHEN q.eqsl_qsl_rcvd = 'Y' THEN 'confirmed' ELSE 'pending' END,
					NOW()
				FROM qsos q
				JOIN logbooks lb ON lb.id = q.logbook_id
				WHERE lb.user_id = $1
				  AND q.deleted_at IS NULL
				  AND NOT EXISTS (
					SELECT 1 FROM sync_status ss WHERE ss.qso_id = q.id AND ss.service = 'eqsl'
				  )
				ON CONFLICT (qso_id, service) DO NOTHING
				RETURNING status
			)
			SELECT
				COUNT(*) FILTER (WHERE status = 'confirmed') AS confirmed_count,
				COUNT(*) FILTER (WHERE status = 'pending')   AS pending_count
			FROM inserted
		`
	case "lotw":
		sql = `
			WITH inserted AS (
				INSERT INTO sync_status (qso_id, service, status, updated_at)
				SELECT
					q.id,
					'lotw',
					CASE WHEN q.lotw_qsl_rcvd = 'Y' OR q.lotw_sent_at IS NOT NULL THEN 'confirmed' ELSE 'pending' END,
					NOW()
				FROM qsos q
				JOIN logbooks lb ON lb.id = q.logbook_id
				WHERE lb.user_id = $1
				  AND q.deleted_at IS NULL
				  AND NOT EXISTS (
					SELECT 1 FROM sync_status ss WHERE ss.qso_id = q.id AND ss.service = 'lotw'
				  )
				ON CONFLICT (qso_id, service) DO NOTHING
				RETURNING status
			)
			SELECT
				COUNT(*) FILTER (WHERE status = 'confirmed') AS confirmed_count,
				COUNT(*) FILTER (WHERE status = 'pending')   AS pending_count
			FROM inserted
		`
	case "qrz":
		sql = `
			WITH inserted AS (
				INSERT INTO sync_status (qso_id, service, status, updated_at)
				SELECT
					q.id,
					'qrz',
					CASE WHEN q.qrz_qsl_rcvd = 'Y' THEN 'confirmed' ELSE 'pending' END,
					NOW()
				FROM qsos q
				JOIN logbooks lb ON lb.id = q.logbook_id
				WHERE lb.user_id = $1
				  AND q.deleted_at IS NULL
				  AND NOT EXISTS (
					SELECT 1 FROM sync_status ss WHERE ss.qso_id = q.id AND ss.service = 'qrz'
				  )
				ON CONFLICT (qso_id, service) DO NOTHING
				RETURNING status
			)
			SELECT
				COUNT(*) FILTER (WHERE status = 'confirmed') AS confirmed_count,
				COUNT(*) FILTER (WHERE status = 'pending')   AS pending_count
			FROM inserted
		`
	case "sota":
		// Only backfill activator QSOs (my_sota_ref set). Status is always pending.
		sql = `
			WITH inserted AS (
				INSERT INTO sync_status (qso_id, service, status, updated_at)
				SELECT q.id, 'sota', 'pending', NOW()
				FROM qsos q
				JOIN logbooks lb ON lb.id = q.logbook_id
				WHERE lb.user_id = $1
				  AND q.deleted_at IS NULL
				  AND NULLIF(TRIM(q.my_sota_ref), '') IS NOT NULL
				  AND NOT EXISTS (
					SELECT 1 FROM sync_status ss WHERE ss.qso_id = q.id AND ss.service = 'sota'
				  )
				ON CONFLICT (qso_id, service) DO NOTHING
				RETURNING status
			)
			SELECT
				COUNT(*) FILTER (WHERE status = 'confirmed') AS confirmed_count,
				COUNT(*) FILTER (WHERE status = 'pending')   AS pending_count
			FROM inserted
		`
	case "pota":
		// Only backfill QSOs with at least one POTA reference. Status is always pending.
		sql = `
			WITH inserted AS (
				INSERT INTO sync_status (qso_id, service, status, updated_at)
				SELECT q.id, 'pota', 'pending', NOW()
				FROM qsos q
				JOIN logbooks lb ON lb.id = q.logbook_id
				WHERE lb.user_id = $1
				  AND q.deleted_at IS NULL
				  AND CARDINALITY(COALESCE(q.my_pota_refs, ARRAY[]::text[])) > 0
				  AND NOT EXISTS (
					SELECT 1 FROM sync_status ss WHERE ss.qso_id = q.id AND ss.service = 'pota'
				  )
				ON CONFLICT (qso_id, service) DO NOTHING
				RETURNING status
			)
			SELECT
				COUNT(*) FILTER (WHERE status = 'confirmed') AS confirmed_count,
				COUNT(*) FILTER (WHERE status = 'pending')   AS pending_count
			FROM inserted
		`
	default:
		// Generic: insert pending rows for all QSOs not yet in sync_status for this service.
		// Uses a parameterised query — $2 is the service name.
		row := pool.QueryRow(ctx, `
			WITH inserted AS (
				INSERT INTO sync_status (qso_id, service, status, updated_at)
				SELECT q.id, $2, 'pending', NOW()
				FROM qsos q
				JOIN logbooks lb ON lb.id = q.logbook_id
				WHERE lb.user_id = $1
				  AND q.deleted_at IS NULL
				  AND NOT EXISTS (
					SELECT 1 FROM sync_status ss WHERE ss.qso_id = q.id AND ss.service = $2
				  )
				ON CONFLICT (qso_id, service) DO NOTHING
				RETURNING status
			)
			SELECT
				COUNT(*) FILTER (WHERE status = 'confirmed') AS confirmed_count,
				COUNT(*) FILTER (WHERE status = 'pending')   AS pending_count
			FROM inserted
		`, userID, service)
		var counts BackfillCounts
		if err := row.Scan(&counts.Confirmed, &counts.Pending); err != nil {
			return BackfillCounts{}, fmt.Errorf("backfill sync_status for %s: %w", service, err)
		}
		return counts, nil
	}

	row := pool.QueryRow(ctx, sql, userID)
	var counts BackfillCounts
	if err := row.Scan(&counts.Confirmed, &counts.Pending); err != nil {
		return BackfillCounts{}, fmt.Errorf("backfill sync_status for %s: %w", service, err)
	}
	return counts, nil
}

// EnabledServicesForUser returns the list of services the user has active credentials for.
// Used to determine which services to enqueue sync jobs for on QSO creation.
func EnabledServicesForUser(ctx context.Context, pool *pgxpool.Pool, userID int64) ([]string, error) {
	rows, err := pool.Query(ctx, `
		SELECT service
		FROM user_service_credentials
		WHERE user_id = $1 AND is_active = TRUE
		  AND service IN ('eqsl', 'clublog', 'qrz', 'sota', 'pota', 'hamqth', 'lotw')
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var services []string
	for rows.Next() {
		var svc string
		if err := rows.Scan(&svc); err != nil {
			return nil, err
		}
		services = append(services, svc)
	}
	return services, rows.Err()
}

// ──────────────────────────────────────────────────────────────────────────────
// Sync Status Query Helpers (used by the sync status API handler)
// ──────────────────────────────────────────────────────────────────────────────

// ServiceSummary holds aggregated sync status for a single service.
type ServiceSummary struct {
	Service      string
	PendingCount int64
	ErrorCount   int64
	LastSyncAt   *time.Time
	Enabled      bool // true if user has active credentials for this service
}

// GetSyncStatusForUser returns the sync summary for all known services for a user.
func GetSyncStatusForUser(ctx context.Context, pool *pgxpool.Pool, userID int64) ([]ServiceSummary, error) {
	// Build a map of enabled services.
	enabledRows, err := pool.Query(ctx, `
		SELECT service FROM user_service_credentials
		WHERE user_id = $1 AND is_active = TRUE
	`, userID)
	if err != nil {
		return nil, err
	}
	defer enabledRows.Close()

	enabled := make(map[string]bool)
	for enabledRows.Next() {
		var svc string
		if err := enabledRows.Scan(&svc); err != nil {
			return nil, err
		}
		enabled[svc] = true
	}
	if err := enabledRows.Err(); err != nil {
		return nil, err
	}
	enabledRows.Close()

	// Query aggregate sync stats.
	summaryRows, err := pool.Query(ctx, `
		SELECT
			ss.service,
			COUNT(*) FILTER (WHERE ss.status IN ('pending', 'dirty', 'error')) AS pending_count,
			COUNT(*) FILTER (WHERE ss.status = 'error')               AS error_count,
			MAX(ss.last_synced_at)                                    AS last_sync_at
		FROM sync_status ss
		JOIN qsos q ON q.id = ss.qso_id
		JOIN logbooks lb ON lb.id = q.logbook_id
		WHERE lb.user_id = $1
		GROUP BY ss.service
	`, userID)
	if err != nil {
		return nil, err
	}
	defer summaryRows.Close()

	summaries := make(map[string]*ServiceSummary)
	for summaryRows.Next() {
		var s ServiceSummary
		var lastSync pgtype.Timestamptz
		if err := summaryRows.Scan(&s.Service, &s.PendingCount, &s.ErrorCount, &lastSync); err != nil {
			return nil, err
		}
		if lastSync.Valid {
			t := lastSync.Time
			s.LastSyncAt = &t
		}
		s.Enabled = enabled[s.Service]
		summaries[s.Service] = &s
	}
	if err := summaryRows.Err(); err != nil {
		return nil, err
	}

	// Include all configured services, even ones with no sync history yet.
	knownServices := []string{"eqsl", "clublog", "lotw", "qrz", "hamqth", "sota", "pota"}
	var result []ServiceSummary
	for _, svc := range knownServices {
		if s, ok := summaries[svc]; ok {
			result = append(result, *s)
		} else {
			result = append(result, ServiceSummary{
				Service: svc,
				Enabled: enabled[svc],
			})
		}
	}

	return result, nil
}

// SyncHistoryRow holds a single row from the sync history query.
type SyncHistoryRow struct {
	ID           int64
	Service      string
	Status       string
	LastSyncedAt *time.Time
	ErrorMessage *string
	ErrorCode    *string
	RetryCount   int16
	QSOUuid      string
	Callsign     string
	Band         string
	Mode         string
	DatetimeOn   time.Time
}

// GetSyncHistory returns recent sync history for a user.
func GetSyncHistory(ctx context.Context, pool *pgxpool.Pool, userID int64, limit int) ([]SyncHistoryRow, error) {
	rows, err := pool.Query(ctx, `
		SELECT
			ss.id,
			ss.service,
			ss.status,
			ss.last_synced_at,
			ss.error_message,
			ss.last_error_code,
			ss.retry_count,
			q.uuid::text,
			q.callsign,
			q.band,
			q.mode,
			q.datetime_on
		FROM sync_status ss
		JOIN qsos q ON q.id = ss.qso_id
		JOIN logbooks lb ON lb.id = q.logbook_id
		WHERE lb.user_id = $1
		  AND ss.last_synced_at IS NOT NULL
		ORDER BY ss.last_synced_at DESC
		LIMIT $2
	`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []SyncHistoryRow
	for rows.Next() {
		var row SyncHistoryRow
		var lastSynced, datetimeOn pgtype.Timestamptz
		if err := rows.Scan(
			&row.ID, &row.Service, &row.Status,
			&lastSynced, &row.ErrorMessage, &row.ErrorCode,
			&row.RetryCount,
			&row.QSOUuid, &row.Callsign, &row.Band, &row.Mode,
			&datetimeOn,
		); err != nil {
			return nil, err
		}
		if lastSynced.Valid {
			t := lastSynced.Time
			row.LastSyncedAt = &t
		}
		if datetimeOn.Valid {
			row.DatetimeOn = datetimeOn.Time
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

func isClubLogAuthOrPermanentError(errMsg string) bool {
	s := strings.ToLower(strings.TrimSpace(errMsg))
	return strings.Contains(s, "invalid") ||
		strings.Contains(s, "authentication failed") ||
		strings.Contains(s, "unauthorized") ||
		strings.Contains(s, "forbidden") ||
		strings.Contains(s, "invalid login") ||
		strings.Contains(s, "credentials") ||
		strings.Contains(s, "password")
}

// Ensure all worker types satisfy the River Worker interface at compile time.
var _ river.Worker[EQSLUploadArgs] = (*EQSLUploadWorker)(nil)
var _ river.Worker[EQSLDownloadArgs] = (*EQSLDownloadWorker)(nil)
var _ river.Worker[ClubLogUploadArgs] = (*ClubLogUploadWorker)(nil)
var _ river.Worker[ClubLogDeleteArgs] = (*ClubLogDeleteWorker)(nil)
var _ river.Worker[ClubLogPollArgs] = (*ClubLogPollWorker)(nil)

// EncodeClubLogCredentials is a convenience export so the credentials handler
// can use it without importing the full clublog package from outside the jobs.
var EncodeClubLogCredentials = func(email, password, callsign string) ([]byte, error) {
	return json.Marshal(clublog.Credentials{Email: email, Password: password, Callsign: callsign})
}

// EncodeEQSLCredentials is a convenience export for the credentials handler.
var EncodeEQSLCredentials = func(username, password string) ([]byte, error) {
	return json.Marshal(eqsl.UsernamePassword{Username: username, Password: password})
}
