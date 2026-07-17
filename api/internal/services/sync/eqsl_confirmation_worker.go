package sync

import (
	"context"
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
	confirmsvc "github.com/FtlC-ian/radioledger/api/internal/services/confirmation"
	"github.com/FtlC-ian/radioledger/api/internal/services/eqsl"
)

// ──────────────────────────────────────────────────────────────────────────────
// EQSLConfirmationPullArgs
// ──────────────────────────────────────────────────────────────────────────────

// EQSLConfirmationPullArgs is the River job payload for pulling eQSL inbox
// confirmations and matching them back to local QSOs.
type EQSLConfirmationPullArgs struct {
	UserID       int64  `json:"user_id"`
	LastPullDate string `json:"last_pull_date,omitempty"` // RFC3339; empty = load from DB
}

// Kind returns the unique River job kind for eQSL confirmation pulls.
func (EQSLConfirmationPullArgs) Kind() string { return "eqsl_pull_confirmations" }

// ──────────────────────────────────────────────────────────────────────────────
// EQSLConfirmationPullWorker
// ──────────────────────────────────────────────────────────────────────────────

// EQSLConfirmationPullWorker is a River worker that downloads the eQSL inbox for
// a user and matches incoming confirmations to local QSOs.
//
// For each matched QSO it:
//   - Sets eqsl_qsl_rcvd = 'Y' on the qsos row.
//   - Upserts a qso_confirmations row with status='confirmed', eqsl_confirmed=TRUE.
//   - Records eqsl_ag (Authenticity Guaranteed) boolean.
//   - Updates sync_status to confirmed for service='eqsl'.
//   - Records the pull timestamp in eqsl_sync_status for incremental future pulls.
type EQSLConfirmationPullWorker struct {
	river.WorkerDefaults[EQSLConfirmationPullArgs]
	Pool    *pgxpool.Pool
	Keyring *crypto.Keyring
}

// Work executes the eQSL confirmation pull job for a specific user.
//
// Flow:
//  1. Load and decrypt eQSL credentials.
//  2. Resolve last-pull timestamp (from job args or DB).
//  3. Download inbox from eQSL (incremental via RcvdSince).
//  4. For each inbox record, find the best-matching local QSO (±1 day, mode group).
//  5. Apply confirmation: update qsos, qso_confirmations, sync_status.
//  6. Persist new last_pull_at checkpoint in eqsl_sync_status.
func (w *EQSLConfirmationPullWorker) Work(ctx context.Context, job *river.Job[EQSLConfirmationPullArgs]) error {
	userID := job.Args.UserID
	log := slog.With(slog.Int64("user_id", userID), slog.String("service", "eqsl"))

	infra := infraOrFallback(w.Pool)
	allowed, retryAfter, err := infra.AllowCircuit(ctx, "eqsl")
	if err != nil {
		return fmt.Errorf("eqsl pull circuit check: %w", err)
	}
	if !allowed {
		log.WarnContext(ctx, "eqsl circuit breaker open (pull), requeueing", slog.Duration("retry_after", retryAfter))
		return river.JobSnooze(retryAfter)
	}

	// Load and decrypt eQSL credentials.
	plaintext, err := decryptServiceCredentials(ctx, w.Pool, w.Keyring, userID, "eqsl")
	if err != nil {
		log.ErrorContext(ctx, "failed to load eqsl credentials for confirmation pull", slog.String("error", err.Error()))
		return fmt.Errorf("eqsl pull credentials: %w", err)
	}
	if len(plaintext) == 0 {
		log.DebugContext(ctx, "no eqsl credentials configured, skipping confirmation pull")
		return nil
	}

	creds, err := eqsl.DecodeCredentials(plaintext)
	if err != nil {
		log.ErrorContext(ctx, "failed to decode eqsl credentials for confirmation pull", slog.String("error", err.Error()))
		return fmt.Errorf("decode eqsl credentials: %w", err)
	}

	// Resolve the since-date for incremental polling.
	since, err := w.resolveLastPull(ctx, userID, job.Args.LastPullDate)
	if err != nil {
		return fmt.Errorf("resolve eqsl last pull: %w", err)
	}

	// Consume rate limit token.
	allowedRate, rateErr := infra.ConsumeRateLimit(ctx, "eqsl")
	if rateErr != nil {
		return fmt.Errorf("eqsl pull rate limit check: %w", rateErr)
	}
	if !allowedRate {
		return river.JobSnooze(2 * time.Second)
	}

	// Download the inbox.
	sinceTime := time.Time{}
	if since != nil {
		sinceTime = *since
	}

	start := time.Now()
	records, err := eqsl.DownloadInboxAG(ctx, creds, sinceTime)
	elapsed := time.Since(start)

	if err != nil {
		errText := err.Error()
		if isEQSLAuthOrPermanentError(errText) {
			log.ErrorContext(ctx, "eqsl confirmation pull authentication failed", slog.String("error", errText))
			return nil // permanent failure — do not retry
		}
		if _, cbErr := infra.RecordFailure(ctx, "eqsl", errText); cbErr != nil {
			log.WarnContext(ctx, "failed to record eqsl circuit failure", slog.String("error", cbErr.Error()))
		}
		return fmt.Errorf("eqsl inbox download: %w", err)
	}
	if err := infra.RecordSuccess(ctx, "eqsl"); err != nil {
		log.WarnContext(ctx, "failed to record eqsl circuit success", slog.String("error", err.Error()))
	}

	log.InfoContext(ctx, "eqsl inbox downloaded",
		slog.Int("records", len(records)),
		slog.Duration("elapsed", elapsed),
	)

	matched := 0
	unmatched := 0
	for _, rec := range records {
		if rec.DatetimeOn.IsZero() {
			unmatched++
			continue
		}

		candidate, err := w.findBestMatch(ctx, userID, rec)
		if err != nil {
			log.WarnContext(ctx, "eqsl confirmation match failed",
				slog.String("callsign", rec.TheirCallsign),
				slog.String("band", rec.Band),
				slog.String("mode", rec.Mode),
				slog.String("error", err.Error()))
			unmatched++
			continue
		}
		if candidate == nil {
			unmatched++
			continue
		}

		if err := w.applyConfirmation(ctx, *candidate, rec); err != nil {
			log.WarnContext(ctx, "eqsl confirmation apply failed",
				slog.Int64("qso_id", candidate.QSOID),
				slog.String("error", err.Error()))
			unmatched++
			continue
		}
		matched++
	}

	// Checkpoint: record last pull at NOW() (or latest QSL date if records are available).
	checkpoint := time.Now().UTC()
	if err := w.updateLastPull(ctx, userID, checkpoint); err != nil {
		log.WarnContext(ctx, "failed to update eqsl last_pull_at", slog.String("error", err.Error()))
	}

	log.InfoContext(ctx, "eqsl confirmation pull complete",
		slog.Int("received", len(records)),
		slog.Int("matched", matched),
		slog.Int("unmatched", unmatched),
		slog.Time("last_pull_at", checkpoint),
	)
	return nil
}

// eqslMatchCandidate holds local QSO data used for matching eQSL confirmations.
type eqslMatchCandidate struct {
	QSOID         int64
	OurCallsign   string
	TheirCallsign string
	Band          string
	Mode          string
	DatetimeOn    time.Time
}

// findBestMatch finds the best local QSO matching an incoming eQSL record.
//
// Matching criteria (same as LoTW confirmation worker):
//   - Their callsign (CALL in eQSL = the station that sent us a card)
//   - Band (case-insensitive)
//   - QSO datetime within ±1 day
//   - Mode group normalized (SSB/LSB/USB → SSB, CW/CWR → CW, etc.)
//
// Returns the closest-in-time match, or nil if none found.
func (w *EQSLConfirmationPullWorker) findBestMatch(ctx context.Context, userID int64, rec eqsl.InboxRecordAG) (*eqslMatchCandidate, error) {
	rows, err := w.Pool.Query(ctx, `
		SELECT
			q.id,
			upper(COALESCE(NULLIF(q.station_callsign, ''), u.callsign)),
			upper(q.callsign),
			lower(q.band),
			upper(q.mode),
			q.datetime_on
		FROM qsos q
		JOIN logbooks lb ON lb.id = q.logbook_id
		JOIN users u ON u.id = lb.user_id
		WHERE lb.user_id = $1
		  AND upper(q.callsign) = upper($2)
		  AND lower(q.band) = lower($3)
		  AND q.datetime_on BETWEEN ($4::timestamptz - INTERVAL '1 day')
		                        AND ($4::timestamptz + INTERVAL '1 day')
		  AND q.deleted_at IS NULL
		ORDER BY ABS(EXTRACT(EPOCH FROM (q.datetime_on - $4::timestamptz))) ASC
		LIMIT 25
	`, userID, rec.TheirCallsign, rec.Band, rec.DatetimeOn)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	wantedMode := confirmsvc.NormalizeModeGroup(rec.Mode)
	for rows.Next() {
		var c eqslMatchCandidate
		var dt pgtype.Timestamptz
		if err := rows.Scan(&c.QSOID, &c.OurCallsign, &c.TheirCallsign, &c.Band, &c.Mode, &dt); err != nil {
			return nil, err
		}
		if dt.Valid {
			c.DatetimeOn = dt.Time.UTC()
		}
		if confirmsvc.NormalizeModeGroup(c.Mode) != wantedMode {
			continue
		}
		return &c, nil
	}
	return nil, rows.Err()
}

// applyConfirmation persists an eQSL confirmation to the database.
//
// In a single transaction it:
//  1. Sets eqsl_qsl_rcvd = 'Y' and eqsl_qsl_rcvd_date on the qsos row.
//  2. Upserts the qso_confirmations row with eqsl_confirmed=TRUE (and eqsl_ag if AG).
//  3. Upserts sync_status to confirmed for service='eqsl'.
func (w *EQSLConfirmationPullWorker) applyConfirmation(ctx context.Context, candidate eqslMatchCandidate, rec eqsl.InboxRecordAG) error {
	tx, err := w.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Use the QSL receipt date if available; otherwise fall back to the QSO datetime.
	confirmedAt := rec.DatetimeOn.UTC()
	if rec.QSLRDate != nil {
		confirmedAt = rec.QSLRDate.UTC()
	} else if rec.EQSLQSLRDate != nil {
		confirmedAt = rec.EQSLQSLRDate.UTC()
	}

	modeGroup := confirmsvc.NormalizeModeGroup(candidate.Mode)

	// 1. Update qsos row.
	if _, err := tx.Exec(ctx, `
		UPDATE qsos SET
			eqsl_qsl_rcvd      = 'Y',
			eqsl_qsl_rcvd_date = COALESCE(eqsl_qsl_rcvd_date, $2::date),
			updated_at         = NOW()
		WHERE id = $1
	`, candidate.QSOID, confirmedAt); err != nil {
		return fmt.Errorf("update qso eqsl confirmation: %w", err)
	}

	// 2. Upsert sync_status.
	if _, err := tx.Exec(ctx, `
		INSERT INTO sync_status (qso_id, service, status, last_synced_at, error_message, last_error_code, retry_count, next_retry_at, updated_at)
		VALUES ($1, 'eqsl', 'confirmed', $2, NULL, NULL, 0, NULL, NOW())
		ON CONFLICT (qso_id, service) DO UPDATE SET
			status         = 'confirmed',
			last_synced_at = EXCLUDED.last_synced_at,
			error_message  = NULL,
			last_error_code = NULL,
			retry_count    = 0,
			next_retry_at  = NULL,
			updated_at     = NOW()
	`, candidate.QSOID, confirmedAt); err != nil {
		return fmt.Errorf("upsert eqsl sync_status: %w", err)
	}

	// 3. Upsert qso_confirmations.
	if _, err := tx.Exec(ctx, `
		INSERT INTO qso_confirmations (
			qso_id, matched_qso_id,
			our_callsign, their_callsign,
			band, mode,
			qso_date, qso_time,
			status,
			our_verification, their_verification,
			eqsl_confirmed, eqsl_confirmed_at,
			eqsl_ag,
			confirmed_at,
			created_at, updated_at
		) VALUES (
			$1, NULL,
			upper($2), upper($3),
			$4, $5,
			$6::date, $6::time,
			'confirmed',
			'cross_verified', 'cross_verified',
			TRUE, $7,
			$8,
			$7,
			NOW(), NOW()
		)
		ON CONFLICT (qso_id) DO UPDATE SET
			status              = 'confirmed',
			eqsl_confirmed      = TRUE,
			eqsl_confirmed_at   = COALESCE(qso_confirmations.eqsl_confirmed_at, EXCLUDED.eqsl_confirmed_at),
			eqsl_ag             = CASE
				WHEN EXCLUDED.eqsl_ag THEN TRUE
				ELSE qso_confirmations.eqsl_ag
			END,
			confirmed_at        = COALESCE(qso_confirmations.confirmed_at, EXCLUDED.confirmed_at),
			our_verification    = CASE
				WHEN qso_confirmations.our_verification = 'none' THEN 'cross_verified'
				ELSE qso_confirmations.our_verification
			END,
			their_verification  = CASE
				WHEN qso_confirmations.their_verification = 'none' THEN 'cross_verified'
				ELSE qso_confirmations.their_verification
			END,
			updated_at          = NOW()
	`, candidate.QSOID, candidate.OurCallsign, candidate.TheirCallsign,
		candidate.Band, modeGroup,
		candidate.DatetimeOn.UTC(), confirmedAt,
		rec.AppEQSLAG,
	); err != nil {
		return fmt.Errorf("upsert qso_confirmation: %w", err)
	}

	return tx.Commit(ctx)
}

// resolveLastPull returns the since-date for this pull run.
//
// Priority:
//  1. Explicit LastPullDate from job args.
//  2. last_pull_at from eqsl_sync_status.
//  3. nil (full pull from the beginning).
func (w *EQSLConfirmationPullWorker) resolveLastPull(ctx context.Context, userID int64, raw string) (*time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw != "" {
		layouts := []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02"}
		for _, layout := range layouts {
			if t, err := time.Parse(layout, raw); err == nil {
				t = t.UTC()
				return &t, nil
			}
		}
		return nil, fmt.Errorf("invalid last_pull_date %q", raw)
	}

	var lastPull pgtype.Timestamptz
	err := w.Pool.QueryRow(ctx, `SELECT last_pull_at FROM eqsl_sync_status WHERE user_id = $1`, userID).Scan(&lastPull)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !lastPull.Valid {
		return nil, nil
	}
	t := lastPull.Time.UTC()
	return &t, nil
}

// updateLastPull records the last successful pull timestamp in eqsl_sync_status.
func (w *EQSLConfirmationPullWorker) updateLastPull(ctx context.Context, userID int64, pulledAt time.Time) error {
	_, err := w.Pool.Exec(ctx, `
		INSERT INTO eqsl_sync_status (user_id, last_pull_at, updated_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (user_id) DO UPDATE SET
			last_pull_at = GREATEST(COALESCE(eqsl_sync_status.last_pull_at, '-infinity'::timestamptz), EXCLUDED.last_pull_at),
			updated_at   = NOW()
	`, userID, pulledAt.UTC())
	return err
}

// ──────────────────────────────────────────────────────────────────────────────
// EnqueueEQSLConfirmationPull helper
// ──────────────────────────────────────────────────────────────────────────────

// EnqueueEQSLConfirmationPull enqueues an eQSL confirmation pull job for the
// given user. If lastPullAt is provided it is embedded in the job args for the
// worker to use as the RcvdSince parameter. delay schedules the job in the future
// (useful when called immediately after an upload to give eQSL time to process).
func EnqueueEQSLConfirmationPull(ctx context.Context, rc RiverInserter, userID int64, lastPullAt *time.Time, delay time.Duration) (int64, error) {
	if rc == nil {
		return 0, fmt.Errorf("river inserter is nil")
	}
	args := EQSLConfirmationPullArgs{UserID: userID}
	if lastPullAt != nil && !lastPullAt.IsZero() {
		args.LastPullDate = lastPullAt.UTC().Format(time.RFC3339)
	}

	opts := &river.InsertOpts{
		UniqueOpts: river.UniqueOpts{
			ByArgs:  true,
			ByState: []rivertype.JobState{rivertype.JobStateAvailable, rivertype.JobStateRunning, rivertype.JobStateScheduled, rivertype.JobStatePending},
		},
	}
	if delay > 0 {
		opts.ScheduledAt = time.Now().UTC().Add(delay)
	}

	res, err := rc.Insert(ctx, args, opts)
	if err != nil {
		return 0, err
	}
	if res == nil || res.Job == nil {
		return 0, nil
	}
	return res.Job.ID, nil
}

var _ river.Worker[EQSLConfirmationPullArgs] = (*EQSLConfirmationPullWorker)(nil)
