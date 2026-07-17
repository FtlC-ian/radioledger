package confirmation

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

// ─────────────────────────────────────────────────────────────────────────────
// QSOMatchWorker
// Triggered when a new QSO is created. Looks for a matching QSO from the
// other side and creates/updates a qso_confirmations record.
// ─────────────────────────────────────────────────────────────────────────────

// QSOMatchArgs is the River job payload for matching a single QSO.
type QSOMatchArgs struct {
	QSOID  int64 `json:"qso_id"`
	UserID int64 `json:"user_id"`
}

// Kind returns the unique River job kind for QSO matching.
func (QSOMatchArgs) Kind() string { return "qso_match" }

// QSOMatchWorker is the River worker that matches a QSO against other users' logs.
type QSOMatchWorker struct {
	river.WorkerDefaults[QSOMatchArgs]
	Pool *pgxpool.Pool
}

// Work executes the QSO match job.
//
// Flow:
//  1. Load QSO data for the given qso_id.
//  2. Look for matching QSOs from other users via find_qso_matches().
//  3. Look up verification levels for both sides.
//  4. Upsert a qso_confirmations record with the appropriate status.
//  5. If a match was found, also upsert the matched side's confirmation record.
func (w *QSOMatchWorker) Work(ctx context.Context, job *river.Job[QSOMatchArgs]) error {
	qsoID := job.Args.QSOID
	userID := job.Args.UserID
	log := slog.With(slog.Int64("qso_id", qsoID), slog.Int64("user_id", userID))

	// Load the QSO.
	qso, err := loadQSOForConfirmation(ctx, w.Pool, qsoID)
	if err != nil {
		return fmt.Errorf("qso_match: load qso: %w", err)
	}
	if qso == nil {
		log.WarnContext(ctx, "qso_match: qso not found or deleted, skipping")
		return nil
	}

	// Find candidates from the other side.
	candidates, err := FindMatches(ctx, w.Pool, MatchRequest{
		QSOID:         qsoID,
		UserID:        userID,
		OurCallsign:   qso.StationCallsign,
		TheirCallsign: qso.Callsign,
		Band:          qso.Band,
		Mode:          qso.Mode,
		DatetimeOn:    qso.DatetimeOn,
	})
	if err != nil {
		return fmt.Errorf("qso_match: find matches: %w", err)
	}

	best := BestMatch(candidates)

	// Determine our verification level.
	ourVerification, err := OperatorVerificationLevel(ctx, w.Pool, userID, qso.StationCallsign)
	if err != nil {
		log.WarnContext(ctx, "qso_match: failed to get our verification level", slog.String("error", err.Error()))
		ourVerification = "none"
	}

	var (
		matchedQSOID      *int64
		theirVerification = "none"
		status            string
	)

	if best != nil {
		matchedQSOID = &best.QSOID
		theirVerification, err = OperatorVerificationLevel(ctx, w.Pool, best.UserID, qso.Callsign)
		if err != nil {
			log.WarnContext(ctx, "qso_match: failed to get their verification level", slog.String("error", err.Error()))
			theirVerification = "none"
		}
		status = DetermineConfirmationStatus(ourVerification, theirVerification, true)
		log.InfoContext(ctx, "qso_match: match found",
			slog.Int64("matched_qso_id", best.QSOID),
			slog.Float64("confidence", best.Confidence),
			slog.String("status", status),
		)
	} else {
		status = "unconfirmed"
		log.InfoContext(ctx, "qso_match: no match found")
	}

	// Upsert our confirmation record.
	if err := upsertConfirmation(ctx, w.Pool, upsertConfirmationParams{
		QSOID:             qsoID,
		MatchedQSOID:      matchedQSOID,
		OurCallsign:       qso.StationCallsign,
		TheirCallsign:     qso.Callsign,
		Band:              qso.Band,
		Mode:              qso.Mode,
		QSODate:           qso.DatetimeOn,
		Status:            status,
		OurVerification:   ourVerification,
		TheirVerification: theirVerification,
	}); err != nil {
		return fmt.Errorf("qso_match: upsert our confirmation: %w", err)
	}

	// If we found a match on the other side, also create/update their record.
	if best != nil {
		theirStatus := DetermineConfirmationStatus(theirVerification, ourVerification, true)
		if err := upsertConfirmation(ctx, w.Pool, upsertConfirmationParams{
			QSOID:             best.QSOID,
			MatchedQSOID:      &qsoID,
			OurCallsign:       qso.Callsign, // from their perspective: they worked us
			TheirCallsign:     qso.StationCallsign,
			Band:              qso.Band,
			Mode:              qso.Mode,
			QSODate:           best.DatetimeOn,
			Status:            theirStatus,
			OurVerification:   theirVerification,
			TheirVerification: ourVerification,
		}); err != nil {
			log.WarnContext(ctx, "qso_match: failed to upsert their confirmation",
				slog.Int64("their_qso_id", best.QSOID),
				slog.String("error", err.Error()),
			)
			// Don't fail the whole job for this — our side is already saved
		}
	}

	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// CascadeConfirmationWorker
// Triggered when a user claims a callsign and uploads logs.
// Re-runs matching for ALL QSOs mentioning that callsign (on both sides).
// ─────────────────────────────────────────────────────────────────────────────

// CascadeConfirmationArgs is the River job payload for cascading confirmations.
type CascadeConfirmationArgs struct {
	// UserID is the user who just claimed/verified the callsign.
	UserID int64 `json:"user_id"`
	// Callsign is the callsign that was just verified.
	Callsign string `json:"callsign"`
}

// Kind returns the unique River job kind for cascade confirmation.
func (CascadeConfirmationArgs) Kind() string { return "cascade_confirmation" }

// CascadeConfirmationWorker re-runs matching for all QSOs mentioning a callsign.
// This is idempotent — re-running it just refreshes confirmation statuses.
type CascadeConfirmationWorker struct {
	river.WorkerDefaults[CascadeConfirmationArgs]
	Pool        *pgxpool.Pool
	RiverClient RiverInserter
}

// Work executes the cascade confirmation for a newly claimed callsign.
//
// Flow:
//  1. Find all QSOs that mention this callsign (either as station_callsign or as callsign).
//  2. For each: enqueue a QSOMatchWorker job to re-run matching.
//
// We enqueue individual jobs rather than doing inline to keep each unit of work
// small and retryable, and to respect River's concurrency limits.
func (w *CascadeConfirmationWorker) Work(ctx context.Context, job *river.Job[CascadeConfirmationArgs]) error {
	callsign := job.Args.Callsign
	userID := job.Args.UserID
	log := slog.With(slog.String("callsign", callsign), slog.Int64("user_id", userID))

	log.InfoContext(ctx, "cascade_confirmation: starting cascade for callsign")

	// Find all QSOs that mention this callsign (as either side).
	// We look at:
	//  1. QSOs owned by this user (their logs now that they're verified)
	//  2. QSOs by others that logged this callsign as a contact
	rows, err := w.Pool.Query(ctx, `
		SELECT q.id, lb.user_id
		FROM qsos q
		JOIN logbooks lb ON lb.id = q.logbook_id
		WHERE (
			upper(q.station_callsign) = upper($1)
			OR upper(q.callsign) = upper($1)
		)
		AND q.deleted_at IS NULL
		ORDER BY q.datetime_on DESC
		LIMIT 5000
	`, callsign)
	if err != nil {
		return fmt.Errorf("cascade_confirmation: query qsos: %w", err)
	}
	defer rows.Close()

	type qsoRef struct {
		ID     int64
		UserID int64
	}
	var qsos []qsoRef
	for rows.Next() {
		var ref qsoRef
		if err := rows.Scan(&ref.ID, &ref.UserID); err != nil {
			return fmt.Errorf("cascade_confirmation: scan: %w", err)
		}
		qsos = append(qsos, ref)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("cascade_confirmation: rows err: %w", err)
	}

	log.InfoContext(ctx, "cascade_confirmation: found QSOs to reprocess",
		slog.Int("count", len(qsos)),
	)

	// Enqueue a QSOMatchWorker for each. Use InsertMany for efficiency.
	enqueued := 0
	for _, ref := range qsos {
		if w.RiverClient == nil {
			// Fallback: run matching inline (useful in tests)
			worker := &QSOMatchWorker{Pool: w.Pool}
			if err := worker.Work(ctx, &river.Job[QSOMatchArgs]{
				Args: QSOMatchArgs{QSOID: ref.ID, UserID: ref.UserID},
			}); err != nil {
				log.WarnContext(ctx, "cascade_confirmation: inline match failed",
					slog.Int64("qso_id", ref.ID),
					slog.String("error", err.Error()),
				)
			}
		} else {
			if _, err := w.RiverClient.Insert(ctx, QSOMatchArgs{
				QSOID:  ref.ID,
				UserID: ref.UserID,
			}, nil); err != nil {
				log.WarnContext(ctx, "cascade_confirmation: enqueue failed",
					slog.Int64("qso_id", ref.ID),
					slog.String("error", err.Error()),
				)
				continue
			}
		}
		enqueued++
	}

	log.InfoContext(ctx, "cascade_confirmation: complete",
		slog.Int("enqueued", enqueued),
		slog.Int("total", len(qsos)),
	)
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Enqueue helpers (called from handlers and other workers)
// ─────────────────────────────────────────────────────────────────────────────

// RiverInserter is the interface satisfied by *river.Client[pgx.Tx].
type RiverInserter interface {
	Insert(ctx context.Context, args river.JobArgs, opts *river.InsertOpts) (*rivertype.JobInsertResult, error)
}

// EnqueueQSOMatch enqueues a QSOMatchWorker job for the given QSO.
func EnqueueQSOMatch(ctx context.Context, rc RiverInserter, qsoID, userID int64) error {
	if rc == nil {
		return fmt.Errorf("river client is nil")
	}
	_, err := rc.Insert(ctx, QSOMatchArgs{QSOID: qsoID, UserID: userID}, nil)
	return err
}

// EnqueueCascadeConfirmation enqueues a cascade job for a newly verified callsign.
func EnqueueCascadeConfirmation(ctx context.Context, rc RiverInserter, userID int64, callsign string) error {
	if rc == nil {
		return fmt.Errorf("river client is nil")
	}
	_, err := rc.Insert(ctx, CascadeConfirmationArgs{UserID: userID, Callsign: callsign}, nil)
	return err
}

// ─────────────────────────────────────────────────────────────────────────────
// DB helpers
// ─────────────────────────────────────────────────────────────────────────────

type qsoForConfirmation struct {
	ID              int64
	StationCallsign string
	Callsign        string
	Band            string
	Mode            string
	DatetimeOn      time.Time
}

const loadQSOForConfirmationSQL = `
	SELECT
		q.id,
		COALESCE(q.station_callsign, (
			SELECT uc.callsign
			FROM user_callsigns uc
			WHERE uc.user_id = lb.user_id
			  AND uc.is_primary = TRUE
			  AND uc.valid_to IS NULL
			LIMIT 1
		), '') AS station_callsign,
		q.callsign,
		q.band,
		q.mode,
		q.datetime_on
	FROM qsos q
	JOIN logbooks lb ON lb.id = q.logbook_id
	WHERE q.id = $1
	  AND q.deleted_at IS NULL
`

func loadQSOForConfirmation(ctx context.Context, pool *pgxpool.Pool, qsoID int64) (*qsoForConfirmation, error) {
	row := pool.QueryRow(ctx, loadQSOForConfirmationSQL, qsoID)

	var q qsoForConfirmation
	var dtOn interface{}
	if err := row.Scan(&q.ID, &q.StationCallsign, &q.Callsign, &q.Band, &q.Mode, &dtOn); err != nil {
		return nil, err
	}

	switch v := dtOn.(type) {
	case time.Time:
		q.DatetimeOn = v
	}

	if q.StationCallsign == "" {
		// Can't match without knowing who we are
		return nil, nil
	}
	return &q, nil
}

type upsertConfirmationParams struct {
	QSOID             int64
	MatchedQSOID      *int64
	OurCallsign       string
	TheirCallsign     string
	Band              string
	Mode              string
	QSODate           time.Time
	Status            string
	OurVerification   string
	TheirVerification string
}

func upsertConfirmation(ctx context.Context, pool *pgxpool.Pool, p upsertConfirmationParams) error {
	modeGroup := NormalizeModeGroup(p.Mode)

	var confirmedAt interface{} = nil
	if p.Status == "confirmed" {
		confirmedAt = time.Now()
	}

	_, err := pool.Exec(ctx, `
		INSERT INTO qso_confirmations (
			qso_id, matched_qso_id,
			our_callsign, their_callsign,
			band, mode,
			qso_date, qso_time,
			status,
			our_verification, their_verification,
			confirmed_at,
			created_at, updated_at
		) VALUES (
			$1, $2,
			upper($3), upper($4),
			$5, $6,
			$7::date, $7::time,
			$8,
			$9, $10,
			$11,
			now(), now()
		)
		ON CONFLICT (qso_id) DO UPDATE SET
			matched_qso_id      = EXCLUDED.matched_qso_id,
			status              = CASE
				-- Only allow status transitions forward (never downgrade confirmed → matched)
				WHEN qso_confirmations.status = 'confirmed' THEN 'confirmed'
				WHEN EXCLUDED.status = 'confirmed' THEN 'confirmed'
				ELSE EXCLUDED.status
			END,
			our_verification    = EXCLUDED.our_verification,
			their_verification  = EXCLUDED.their_verification,
			confirmed_at        = COALESCE(qso_confirmations.confirmed_at, EXCLUDED.confirmed_at),
			updated_at          = now()
	`,
		p.QSOID,
		p.MatchedQSOID,
		p.OurCallsign,
		p.TheirCallsign,
		p.Band,
		modeGroup,
		p.QSODate.UTC(),
		p.Status,
		p.OurVerification,
		p.TheirVerification,
		confirmedAt,
	)
	return err
}

// Compile-time interface checks.
var _ river.Worker[QSOMatchArgs] = (*QSOMatchWorker)(nil)
var _ river.Worker[CascadeConfirmationArgs] = (*CascadeConfirmationWorker)(nil)
