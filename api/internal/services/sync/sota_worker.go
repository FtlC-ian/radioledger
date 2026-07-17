package sync

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/FtlC-ian/radioledger/api/internal/crypto"
	"github.com/FtlC-ian/radioledger/api/internal/metrics"
	sotapkg "github.com/FtlC-ian/radioledger/api/internal/services/sota"
)

// ──────────────────────────────────────────────────────────────────────────────
// SOTA Upload Worker
// ──────────────────────────────────────────────────────────────────────────────

// SOTAUploadArgs is the River job payload for a SOTA activation log upload.
// One job is created per user when they have pending SOTA QSOs to upload.
type SOTAUploadArgs struct {
	// UserID is the owning user. The worker loads credentials and pending QSOs for this user.
	UserID int64 `json:"user_id"`
}

// Kind returns the unique River job kind for SOTA uploads.
func (SOTAUploadArgs) Kind() string { return "sota_upload" }

// SOTAUploadWorker uploads pending SOTA activation QSOs to the SOTA database.
//
// Only QSOs with my_sota_ref populated are tracked for SOTA sync.
// The V2 CSV format is used: V2,{my_callsign},{my_sota_ref},{date},{time},{band},{mode},{their_callsign},{their_sota_ref},{notes}
//
// Flow:
//  1. Load and decrypt SOTA API key for the user.
//  2. Fetch pending sync_status rows for "sota" with my_sota_ref NOT NULL.
//  3. Format QSOs as SOTA V2 CSV.
//  4. Upload in batches to https://api-db.sota.org.uk/logs/activator/.
//  5. On success: mark rows as 'uploaded'.
//  6. On failure: increment retry_count; apply exponential backoff.
type SOTAUploadWorker struct {
	river.WorkerDefaults[SOTAUploadArgs]
	Pool    *pgxpool.Pool
	Keyring *crypto.Keyring
}

// Work executes the SOTA upload job for a specific user.
func (w *SOTAUploadWorker) Work(ctx context.Context, job *river.Job[SOTAUploadArgs]) error {
	userID := job.Args.UserID
	log := slog.With(slog.Int64("user_id", userID), slog.String("service", "sota"))

	infra := infraOrFallback(w.Pool)
	allowed, retryAfter, err := infra.AllowCircuit(ctx, "sota")
	if err != nil {
		return fmt.Errorf("sota circuit check: %w", err)
	}
	if !allowed {
		log.WarnContext(ctx, "sota circuit breaker open, requeueing", slog.Duration("retry_after", retryAfter))
		return river.JobSnooze(retryAfter)
	}

	// Load and decrypt SOTA API key.
	plaintext, err := decryptServiceCredentials(ctx, w.Pool, w.Keyring, userID, "sota")
	if err != nil {
		msg := "SOTA credentials could not be decrypted. Re-save your API key in Settings."
		if _, markErr := markAllPendingFailed(ctx, w.Pool, "sota", userID, msg); markErr != nil {
			log.ErrorContext(ctx, "failed to mark pending rows after credential error", slog.String("error", markErr.Error()))
		}
		log.ErrorContext(ctx, "failed to load sota credentials; marked pending as permanent failure", slog.String("error", err.Error()))
		return nil
	}
	if plaintext == nil {
		msg := "No SOTA credentials configured. Add your SOTA API key in Settings to sync."
		if _, markErr := markAllPendingFailed(ctx, w.Pool, "sota", userID, msg); markErr != nil {
			log.ErrorContext(ctx, "failed to mark pending rows for missing credentials", slog.String("error", markErr.Error()))
		}
		log.WarnContext(ctx, "no sota credentials configured; marked pending as permanent failure")
		return nil
	}

	creds, err := sotapkg.DecodeCredentials(plaintext)
	if err != nil {
		msg := "Invalid SOTA credentials. Re-save your API key in Settings."
		if _, markErr := markAllPendingFailed(ctx, w.Pool, "sota", userID, msg); markErr != nil {
			log.ErrorContext(ctx, "failed to mark pending rows after decode error", slog.String("error", markErr.Error()))
		}
		log.ErrorContext(ctx, "failed to decode sota credentials; marked pending as permanent failure",
			slog.String("error", err.Error()))
		return nil
	}

	pending, err := fetchPendingSOTAQSOs(ctx, w.Pool, userID, 200)
	if err != nil {
		return fmt.Errorf("fetch pending sota qsos: %w", err)
	}
	if len(pending) == 0 {
		return nil
	}

	log.InfoContext(ctx, "sota upload starting", slog.Int("count", len(pending)))
	client := sotapkg.New(creds.APIKey)

	// Check rate limiter before the upload batch.
	allowedRate, rateErr := infra.ConsumeRateLimit(ctx, "sota")
	if rateErr != nil {
		return fmt.Errorf("sota rate limit check: %w", rateErr)
	}
	if !allowedRate {
		return river.JobSnooze(1 * time.Second)
	}

	// Build V2 CSV for the entire batch.
	csvData, rowMap, err := buildSOTACSV(pending)
	if err != nil || csvData == "" {
		log.WarnContext(ctx, "sota: no valid QSOs to upload after formatting")
		return nil
	}

	start := time.Now()
	uploadErr := client.UploadCSV(ctx, csvData)
	metrics.ObserveRiverJobDuration("sota", time.Since(start))

	if uploadErr != nil {
		errText := uploadErr.Error()

		if isSOTAAuthOrPermanentError(errText) {
			msg := "Invalid SOTA API key. Re-save your API key in Settings."
			if _, markErr := markAllPendingFailed(ctx, w.Pool, "sota", userID, msg); markErr != nil {
				log.ErrorContext(ctx, "failed to mark pending rows after auth failure", slog.String("error", markErr.Error()))
			}
			log.ErrorContext(ctx, "sota authentication failure; marked pending as permanent failure",
				slog.String("error", errText))
			return nil
		}

		if isTransientSyncError(errText) {
			tripped, cbErr := infra.RecordFailure(ctx, "sota", errText)
			if cbErr != nil {
				log.WarnContext(ctx, "failed to record sota circuit failure", slog.String("error", cbErr.Error()))
			}
			if tripped {
				log.ErrorContext(ctx, "sota circuit breaker tripped after consecutive failures")
			}
			metrics.IncRiverJobFailure("sota")
			return fmt.Errorf("sota transient upload failure: %w", uploadErr)
		}

		// Non-transient, non-auth error — mark each QSO with the error.
		tripped, cbErr := infra.RecordFailure(ctx, "sota", errText)
		if cbErr != nil {
			log.WarnContext(ctx, "failed to record sota circuit failure", slog.String("error", cbErr.Error()))
		}
		if tripped {
			log.ErrorContext(ctx, "sota circuit breaker tripped")
		}
		for _, row := range pending {
			_ = markSyncError(ctx, w.Pool, "sota", row.SyncID, row.RetryCount, errText, "upload_failed")
		}
		metrics.IncRiverJobFailure("sota")
		log.ErrorContext(ctx, "sota upload batch failed", slog.String("error", errText))
		return nil
	}

	if err := infra.RecordSuccess(ctx, "sota"); err != nil {
		log.WarnContext(ctx, "failed to record sota circuit success", slog.String("error", err.Error()))
	}

	// Mark all successfully uploaded rows.
	uploaded := 0
	for _, syncID := range rowMap {
		if err := markUploaded(ctx, w.Pool, syncID, ""); err != nil {
			log.WarnContext(ctx, "sota: failed to mark sync as uploaded",
				slog.Int64("sync_id", syncID), slog.String("error", err.Error()))
		} else {
			uploaded++
		}
	}

	log.InfoContext(ctx, "sota upload complete",
		slog.Int("uploaded", uploaded),
		slog.Int("total", len(pending)),
	)
	return nil
}

// ──────────────────────────────────────────────────────────────────────────────
// SOTA-specific QSO fetch (includes my_sota_ref and sota_ref)
// ──────────────────────────────────────────────────────────────────────────────

// sotaPendingQSORow extends pendingQSORow with SOTA-specific fields.
type sotaPendingQSORow struct {
	pendingQSORow
	MySotaRef  string  // always non-empty (filtered in query)
	SotaRef    *string // their SOTA ref (for chaser-to-chaser S2S)
}

const fetchPendingSOTASQL = `
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
    lb.user_id,
    UPPER(TRIM(q.my_sota_ref)) AS my_sota_ref,
    q.sota_ref
FROM sync_status ss
JOIN qsos q ON q.id = ss.qso_id
JOIN logbooks lb ON lb.id = q.logbook_id
WHERE ss.service = 'sota'
  AND lb.user_id = $1
  AND ss.status IN ('pending', 'dirty', 'error')
  AND (ss.next_retry_at IS NULL OR ss.next_retry_at <= NOW())
  AND q.deleted_at IS NULL
  AND NULLIF(TRIM(q.my_sota_ref), '') IS NOT NULL
ORDER BY ss.created_at ASC
LIMIT $2
`

func fetchPendingSOTAQSOs(ctx context.Context, pool *pgxpool.Pool, userID int64, limit int) ([]sotaPendingQSORow, error) {
	rows, err := pool.Query(ctx, fetchPendingSOTASQL, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("query pending SOTA QSOs: %w", err)
	}
	defer rows.Close()

	var result []sotaPendingQSORow
	for rows.Next() {
		var r sotaPendingQSORow
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
			&r.MySotaRef,
			&r.SotaRef,
		)
		if err != nil {
			return nil, fmt.Errorf("scan pending SOTA QSO row: %w", err)
		}
		if datetimeOn.Valid {
			r.DatetimeOn = datetimeOn.Time
		}
		result = append(result, r)
	}
	return result, rows.Err()
}

// ──────────────────────────────────────────────────────────────────────────────
// SOTA V2 CSV formatter
// ──────────────────────────────────────────────────────────────────────────────

// buildSOTACSV formats a slice of sotaPendingQSORow as SOTA V2 CSV.
// Returns the CSV string and a slice of sync IDs that were successfully formatted.
// rowMap maps row index → sync_id for marking uploaded after success.
func buildSOTACSV(rows []sotaPendingQSORow) (string, []int64, error) {
	var sb strings.Builder
	var syncIDs []int64

	for _, row := range rows {
		if row.MySotaRef == "" {
			continue
		}

		// Determine the station callsign (my callsign for the activation).
		myCallsign := ""
		if row.StationCallsign != nil && strings.TrimSpace(*row.StationCallsign) != "" {
			myCallsign = strings.TrimSpace(*row.StationCallsign)
		}
		// Callsign in pendingQSORow is the *other* station's callsign (CALL field).
		// We need the activator's callsign from station_callsign.
		// If station_callsign is empty, skip this row — we can't submit without it.
		if myCallsign == "" {
			continue
		}

		theirCallsign := strings.ToUpper(strings.TrimSpace(row.Callsign))
		if theirCallsign == "" {
			continue
		}

		date := sotapkg.FormatSOTADate(row.DatetimeOn)
		timeStr := sotapkg.FormatSOTATime(row.DatetimeOn)
		band := sotapkg.BandToSOTAMHz(row.Band)
		mode := strings.ToUpper(strings.TrimSpace(row.Mode))

		// their SOTA ref (summit-to-summit only; empty string is fine)
		theirSotaRef := ""
		if row.SotaRef != nil {
			theirSotaRef = strings.ToUpper(strings.TrimSpace(*row.SotaRef))
		}

		// Notes is optional. We leave it empty.
		notes := ""

		// V2 CSV format:
		// V2,{my_callsign},{my_sota_ref},{date},{time},{band},{mode},{their_callsign},{their_sota_ref},{notes}
		fmt.Fprintf(&sb, "V2,%s,%s,%s,%s,%s,%s,%s,%s,%s\n",
			myCallsign,
			row.MySotaRef,
			date,
			timeStr,
			band,
			mode,
			theirCallsign,
			theirSotaRef,
			notes,
		)
		syncIDs = append(syncIDs, row.SyncID)
	}

	return sb.String(), syncIDs, nil
}

// isSOTAAuthOrPermanentError returns true for error messages that indicate a
// permanent auth failure (invalid API key, unauthorized, etc.).
func isSOTAAuthOrPermanentError(errMsg string) bool {
	s := strings.ToLower(strings.TrimSpace(errMsg))
	return strings.Contains(s, "authentication failed") ||
		strings.Contains(s, "unauthorized") ||
		strings.Contains(s, "forbidden") ||
		strings.Contains(s, "invalid api key") ||
		strings.Contains(s, "invalid token") ||
		strings.Contains(s, "token is invalid") ||
		strings.Contains(s, "401") ||
		strings.Contains(s, "403")
}

// ──────────────────────────────────────────────────────────────────────────────
// SOTA enqueue helpers
// ──────────────────────────────────────────────────────────────────────────────

// EnqueueSOTAUpload enqueues a SOTA activation log upload job for the given user.
// Called when the user manually triggers sync or after QSO creation.
func EnqueueSOTAUpload(ctx context.Context, rc RiverInserter, userID int64) error {
	if rc == nil {
		return fmt.Errorf("river inserter is nil")
	}
	_, err := rc.Insert(ctx, SOTAUploadArgs{UserID: userID}, &river.InsertOpts{
		UniqueOpts: river.UniqueOpts{
			ByArgs:  true,
			ByState: []rivertype.JobState{rivertype.JobStateAvailable, rivertype.JobStateRunning, rivertype.JobStateScheduled, rivertype.JobStatePending},
		},
	})
	return err
}

// EncodeSOTACredentials is a convenience export for the credentials handler.
var EncodeSOTACredentials = func(apiKey string) ([]byte, error) {
	return sotapkg.EncodeCredentials(apiKey)
}

// Verify SOTAUploadWorker satisfies the River Worker interface at compile time.
var _ river.Worker[SOTAUploadArgs] = (*SOTAUploadWorker)(nil)
