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
	lotwsvc "github.com/FtlC-ian/radioledger/api/internal/services/lotw"
)

// LoTWConfirmationPullArgs is the River payload for downloading newly confirmed
// LoTW QSLs and matching them back to local QSOs.
type LoTWConfirmationPullArgs struct {
	UserID       int64  `json:"user_id"`
	Callsign     string `json:"callsign"`
	LastPullDate string `json:"last_pull_date,omitempty"`
}

func (LoTWConfirmationPullArgs) Kind() string { return "lotw_pull_confirmations" }

// LoTWConfirmationPullWorker downloads confirmations from ARRL and applies them
// to local QSOs and qso_confirmations rows.
type LoTWConfirmationPullWorker struct {
	river.WorkerDefaults[LoTWConfirmationPullArgs]
	Pool    *pgxpool.Pool
	Keyring *crypto.Keyring
}

func (w *LoTWConfirmationPullWorker) Work(ctx context.Context, job *river.Job[LoTWConfirmationPullArgs]) error {
	userID := job.Args.UserID
	log := slog.With(slog.Int64("user_id", userID), slog.String("service", "lotw"))

	infra := infraOrFallback(w.Pool)
	allowed, retryAfter, err := infra.AllowCircuit(ctx, "lotw")
	if err != nil {
		return fmt.Errorf("lotw pull circuit check: %w", err)
	}
	if !allowed {
		log.WarnContext(ctx, "lotw circuit breaker open (pull), requeueing", slog.Duration("retry_after", retryAfter))
		return river.JobSnooze(retryAfter)
	}

	plaintext, err := decryptServiceCredentials(ctx, w.Pool, w.Keyring, userID, "lotw")
	if err != nil {
		return fmt.Errorf("lotw pull credentials: %w", err)
	}
	if len(plaintext) == 0 {
		log.DebugContext(ctx, "no lotw credentials configured, skipping confirmation pull")
		return nil
	}

	passwords, err := lotwsvc.DecodeStoredPasswords(plaintext)
	if err != nil {
		return fmt.Errorf("decode lotw credentials: %w", err)
	}
	if strings.TrimSpace(passwords.WebPassword) == "" {
		log.WarnContext(ctx, "lotw confirmation pull skipped: no web password configured")
		return nil
	}

	callsign := strings.ToUpper(strings.TrimSpace(job.Args.Callsign))
	if callsign == "" {
		callsign, err = loadUserCallsign(ctx, w.Pool, userID)
		if err != nil {
			return fmt.Errorf("load lotw callsign: %w", err)
		}
	}
	if callsign == "" {
		log.WarnContext(ctx, "lotw confirmation pull skipped: no callsign configured")
		return nil
	}

	since, err := w.resolveLastPull(ctx, userID, job.Args.LastPullDate)
	if err != nil {
		return fmt.Errorf("resolve lotw last pull: %w", err)
	}

	allowedRate, rateErr := infra.ConsumeRateLimit(ctx, "lotw")
	if rateErr != nil {
		return fmt.Errorf("lotw pull rate limit check: %w", rateErr)
	}
	if !allowedRate {
		return river.JobSnooze(30 * time.Second)
	}

	result, err := lotwsvc.DownloadConfirmedReport(ctx, callsign, passwords.WebPassword, since)
	if err != nil {
		switch {
		case errors.Is(err, lotwsvc.ErrReportAuthFailed):
			log.ErrorContext(ctx, "lotw confirmation pull authentication failed", slog.String("error", err.Error()))
			return nil
		case errors.Is(err, lotwsvc.ErrReportRateLimit):
			log.WarnContext(ctx, "lotw confirmation pull rate limited", slog.String("error", err.Error()))
			return river.JobSnooze(30 * time.Second)
		default:
			_, _ = infra.RecordFailure(ctx, "lotw", err.Error())
			return fmt.Errorf("download lotw confirmations: %w", err)
		}
	}
	if err := infra.RecordSuccess(ctx, "lotw"); err != nil {
		log.WarnContext(ctx, "failed to record lotw circuit success", slog.String("error", err.Error()))
	}

	matched := 0
	unmatched := 0
	for _, rec := range result.Records {
		candidate, err := w.findBestMatch(ctx, userID, rec)
		if err != nil {
			log.WarnContext(ctx, "lotw confirmation match failed",
				slog.String("callsign", rec.Callsign),
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
			log.WarnContext(ctx, "lotw confirmation apply failed",
				slog.Int64("qso_id", candidate.QSOID),
				slog.String("error", err.Error()))
			unmatched++
			continue
		}
		matched++
	}

	checkpoint := time.Now().UTC()
	if result.LastQSLAt != nil && !result.LastQSLAt.IsZero() {
		checkpoint = result.LastQSLAt.UTC()
	}
	if err := w.updateLastPull(ctx, userID, checkpoint); err != nil {
		log.WarnContext(ctx, "failed to update lotw last_pull_at", slog.String("error", err.Error()))
	}

	log.InfoContext(ctx, "lotw confirmation pull complete",
		slog.String("callsign", callsign),
		slog.Int("received", len(result.Records)),
		slog.Int("matched", matched),
		slog.Int("unmatched", unmatched),
		slog.Time("last_pull_at", checkpoint),
	)
	return nil
}

type lotwMatchCandidate struct {
	QSOID         int64
	OurCallsign   string
	TheirCallsign string
	Band          string
	Mode          string
	DatetimeOn    time.Time
}

func (w *LoTWConfirmationPullWorker) findBestMatch(ctx context.Context, userID int64, rec lotwsvc.ReportRecord) (*lotwMatchCandidate, error) {
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
	`, userID, rec.Callsign, rec.Band, rec.DatetimeOn)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	wantedMode := confirmsvc.NormalizeModeGroup(rec.Mode)
	for rows.Next() {
		var c lotwMatchCandidate
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
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return nil, nil
}

func (w *LoTWConfirmationPullWorker) applyConfirmation(ctx context.Context, candidate lotwMatchCandidate, rec lotwsvc.ReportRecord) error {
	tx, err := w.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	confirmedAt := rec.QSLDate.UTC()
	modeGroup := confirmsvc.NormalizeModeGroup(candidate.Mode)

	if _, err := tx.Exec(ctx, `
		UPDATE qsos SET
			lotw_qsl_rcvd = 'Y',
			lotw_qsl_rcvd_date = COALESCE(lotw_qsl_rcvd_date, $2::date),
			updated_at = NOW()
		WHERE id = $1
	`, candidate.QSOID, confirmedAt); err != nil {
		return fmt.Errorf("update qso lotw confirmation: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO sync_status (qso_id, service, status, last_synced_at, error_message, last_error_code, retry_count, next_retry_at, updated_at)
		VALUES ($1, 'lotw', 'confirmed', $2, NULL, NULL, 0, NULL, NOW())
		ON CONFLICT (qso_id, service) DO UPDATE SET
			status = 'confirmed',
			last_synced_at = EXCLUDED.last_synced_at,
			error_message = NULL,
			last_error_code = NULL,
			retry_count = 0,
			next_retry_at = NULL,
			updated_at = NOW()
	`, candidate.QSOID, confirmedAt); err != nil {
		return fmt.Errorf("upsert lotw sync_status: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO qso_confirmations (
			qso_id, matched_qso_id,
			our_callsign, their_callsign,
			band, mode,
			qso_date, qso_time,
			status,
			our_verification, their_verification,
			lotw_confirmed, lotw_confirmed_at,
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
			$7,
			NOW(), NOW()
		)
		ON CONFLICT (qso_id) DO UPDATE SET
			status = 'confirmed',
			lotw_confirmed = TRUE,
			lotw_confirmed_at = COALESCE(qso_confirmations.lotw_confirmed_at, EXCLUDED.lotw_confirmed_at),
			confirmed_at = COALESCE(qso_confirmations.confirmed_at, EXCLUDED.confirmed_at),
			our_verification = CASE
				WHEN qso_confirmations.our_verification = 'none' THEN 'cross_verified'
				ELSE qso_confirmations.our_verification
			END,
			their_verification = CASE
				WHEN qso_confirmations.their_verification = 'none' THEN 'cross_verified'
				ELSE qso_confirmations.their_verification
			END,
			updated_at = NOW()
	`, candidate.QSOID, candidate.OurCallsign, candidate.TheirCallsign, candidate.Band, modeGroup, candidate.DatetimeOn.UTC(), confirmedAt); err != nil {
		return fmt.Errorf("upsert qso_confirmation: %w", err)
	}

	return tx.Commit(ctx)
}

func (w *LoTWConfirmationPullWorker) resolveLastPull(ctx context.Context, userID int64, raw string) (*time.Time, error) {
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
	err := w.Pool.QueryRow(ctx, `SELECT last_pull_at FROM lotw_sync_status WHERE user_id = $1`, userID).Scan(&lastPull)
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

func (w *LoTWConfirmationPullWorker) updateLastPull(ctx context.Context, userID int64, pulledAt time.Time) error {
	_, err := w.Pool.Exec(ctx, `
		INSERT INTO lotw_sync_status (user_id, last_pull_at, updated_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (user_id) DO UPDATE SET
			last_pull_at = GREATEST(COALESCE(lotw_sync_status.last_pull_at, '-infinity'::timestamptz), EXCLUDED.last_pull_at),
			updated_at = NOW()
	`, userID, pulledAt.UTC())
	return err
}

func EnqueueLoTWConfirmationPull(ctx context.Context, rc RiverInserter, userID int64, callsign string, lastPullAt *time.Time, delay time.Duration) (int64, error) {
	if rc == nil {
		return 0, fmt.Errorf("river inserter is nil")
	}
	args := LoTWConfirmationPullArgs{UserID: userID, Callsign: strings.ToUpper(strings.TrimSpace(callsign))}
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

func loadUserCallsign(ctx context.Context, pool *pgxpool.Pool, userID int64) (string, error) {
	var callsign *string
	err := pool.QueryRow(ctx, `SELECT callsign FROM users WHERE id = $1`, userID).Scan(&callsign)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if callsign == nil {
		return "", nil
	}
	return strings.ToUpper(strings.TrimSpace(*callsign)), nil
}

var _ river.Worker[LoTWConfirmationPullArgs] = (*LoTWConfirmationPullWorker)(nil)
