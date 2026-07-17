package jobs

// AwardRefreshWorker is a River background job that recalculates award progress
// for a single user. It is enqueued after any QSO mutation (insert/update/delete)
// that may affect award standings.
//
// Architecture:
//   - Awards are computed live from the qsos table on every refresh. The
//     award_progress table acts as a read-through cache (dirty flag pattern).
//   - For each award type, the worker queries the relevant QSO data, groups
//     by the award key, and upserts rows into award_progress.
//   - Milestone notifications are fired when a threshold is crossed for the
//     first time (dedup by querying existing notification records).
//
// Scheduling: enqueued by the QSO handler after successful QSO mutations.

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	db "github.com/FtlC-ian/radioledger/api/internal/database/sqlc"
	"github.com/FtlC-ian/radioledger/api/internal/services/awards"
)

// AwardRefreshArgs is the River job payload for an award refresh.
// Passing AwardType="" refreshes all award types for the user.
type AwardRefreshArgs struct {
	// UserID is the internal users.id to refresh awards for.
	UserID int64 `json:"user_id"`
	// AwardType optionally limits the refresh to a single award. Empty = all.
	AwardType string `json:"award_type,omitempty"`
}

// Kind returns the unique River job kind identifier.
func (AwardRefreshArgs) Kind() string { return "award_refresh" }

// AwardRefreshWorker is the River worker that recalculates award progress.
type AwardRefreshWorker struct {
	river.WorkerDefaults[AwardRefreshArgs]
	Pool *pgxpool.Pool
}

// Work executes the award refresh for one user (or one award type within a user).
func (w *AwardRefreshWorker) Work(ctx context.Context, job *river.Job[AwardRefreshArgs]) error {
	log := slog.With(
		slog.String("job_kind", job.Args.Kind()),
		slog.Int64("job_id", job.ID),
		slog.Int64("user_id", job.Args.UserID),
		slog.String("award_type", job.Args.AwardType),
	)
	log.Info("award_refresh: started")
	if err := w.RefreshUser(ctx, job.Args.UserID, job.Args.AwardType, log); err != nil {
		return err
	}
	log.Info("award_refresh: complete")
	return nil
}

// RefreshUser recalculates award progress for one user (optionally a single type).
func (w *AwardRefreshWorker) RefreshUser(ctx context.Context, userID int64, awardType string, log *slog.Logger) error {
	conn, err := w.Pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("award_refresh: acquire conn: %w", err)
	}
	defer conn.Release()

	// Run as worker role to bypass RLS (cross-table selects + multi-type deletes).
	if _, err := conn.Exec(ctx, "SET ROLE radioledger_worker"); err != nil {
		return fmt.Errorf("award_refresh: set worker role: %w", err)
	}

	// Set RLS context so award_progress upserts are correctly scoped.
	if _, err := conn.Exec(ctx, "SELECT set_config('app.current_user_id', $1, true)",
		fmt.Sprintf("%d", userID)); err != nil {
		return fmt.Errorf("award_refresh: set user context: %w", err)
	}

	queries := db.New(conn)

	types := awards.ValidAwardTypes()
	if awardType != "" {
		types = []awards.AwardType{awards.AwardType(awardType)}
	}

	for _, at := range types {
		if err := w.refreshAwardType(ctx, conn.Conn(), queries, userID, at, log); err != nil {
			log.Error("award_refresh: type failed",
				slog.String("award_type", string(at)),
				slog.String("error", err.Error()),
			)
			// Continue with other types rather than aborting the whole job.
		}
	}

	return nil
}

// refreshAwardType recalculates progress for one award type and one user.
func (w *AwardRefreshWorker) refreshAwardType(
	ctx context.Context,
	conn *pgx.Conn,
	queries *db.Queries,
	userID int64,
	at awards.AwardType,
	log *slog.Logger,
) error {
	// Delete stale cached rows — we recalculate from scratch.
	if err := queries.DeleteAwardProgressByType(ctx, db.DeleteAwardProgressByTypeParams{
		UserID:    userID,
		AwardType: string(at),
	}); err != nil {
		return fmt.Errorf("delete existing %s rows: %w", at, err)
	}

	var worked int64
	var err error

	switch at {
	case awards.AwardDXCC:
		worked, err = w.refreshDXCC(ctx, queries, userID)
	case awards.AwardWAS:
		worked, err = w.refreshWAS(ctx, queries, userID)
	case awards.AwardVUCC:
		worked, err = w.refreshVUCC(ctx, queries, userID)
	case awards.AwardWAZ:
		worked, err = w.refreshWAZ(ctx, queries, userID)
	case awards.AwardWPX:
		worked, err = w.refreshWPX(ctx, queries, userID)
	case awards.AwardPOTAHunter:
		worked, err = w.refreshPOTAHunter(ctx, queries, userID)
	case awards.AwardPOTAActivator:
		worked, err = w.refreshPOTAActivator(ctx, queries, userID)
	case awards.AwardSOTAChaser:
		worked, err = w.refreshSOTAChaser(ctx, queries, userID)
	case awards.AwardSOTAActivator:
		worked, err = w.refreshSOTAActivator(ctx, queries, userID)
	default:
		return fmt.Errorf("unknown award type: %s", at)
	}

	if err != nil {
		return err
	}

	return w.checkMilestones(ctx, conn, queries, userID, at, worked, log)
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

// toTS converts a *time.Time to pgtype.Timestamptz for sqlc params.
func toTS(t *time.Time) pgtype.Timestamptz {
	if t == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: t.UTC(), Valid: true}
}

// ─────────────────────────────────────────────────────────────────────────────
// Per-type refresh functions
// ─────────────────────────────────────────────────────────────────────────────

func (w *AwardRefreshWorker) refreshDXCC(ctx context.Context, q *db.Queries, userID int64) (int64, error) {
	rows, err := q.ListDXCCEntitiesProgress(ctx)
	if err != nil {
		return 0, fmt.Errorf("dxcc progress query: %w", err)
	}

	var worked int64
	for _, row := range rows {
		if !row.Worked {
			continue
		}
		worked++
		var lastQSOAt *time.Time
		if row.FirstQso.Valid {
			t := row.FirstQso.Time.UTC()
			lastQSOAt = &t
		}
		if err := q.UpsertAwardProgress(ctx, db.UpsertAwardProgressParams{
			UserID:    userID,
			AwardType: string(awards.AwardDXCC),
			EntityKey: fmt.Sprintf("%d", row.EntityID),
			Worked:    row.Worked,
			Confirmed: row.Confirmed,
			QsoCount:  row.QsoCount,
			LastQsoAt: toTS(lastQSOAt),
		}); err != nil {
			return 0, fmt.Errorf("upsert dxcc entity %d: %w", row.EntityID, err)
		}
	}
	return worked, nil
}

func (w *AwardRefreshWorker) refreshWAS(ctx context.Context, q *db.Queries, userID int64) (int64, error) {
	rows, err := q.ListWorkedStates(ctx)
	if err != nil {
		return 0, fmt.Errorf("was query: %w", err)
	}

	type stateAgg struct {
		count    int64
		firstQSO *time.Time
	}
	states := make(map[string]*stateAgg)

	for _, row := range rows {
		code, ok := awards.NormalizeUSState(row.StateValue)
		if !ok {
			continue
		}

		agg, exists := states[code]
		if !exists {
			agg = &stateAgg{}
			states[code] = agg
		}
		agg.count += row.QsoCount
		if row.FirstQso.Valid {
			t := row.FirstQso.Time.UTC()
			if agg.firstQSO == nil || t.Before(*agg.firstQSO) {
				tCopy := t
				agg.firstQSO = &tCopy
			}
		}
	}

	var worked int64
	for code, agg := range states {
		worked++
		if err := q.UpsertAwardProgress(ctx, db.UpsertAwardProgressParams{
			UserID:    userID,
			AwardType: string(awards.AwardWAS),
			EntityKey: code,
			Worked:    true,
			Confirmed: false,
			QsoCount:  agg.count,
			LastQsoAt: toTS(agg.firstQSO),
		}); err != nil {
			return 0, fmt.Errorf("upsert was state %s: %w", code, err)
		}
	}
	return worked, nil
}

func (w *AwardRefreshWorker) refreshVUCC(ctx context.Context, q *db.Queries, userID int64) (int64, error) {
	rows, err := q.ListWorkedGridSquares(ctx)
	if err != nil {
		return 0, fmt.Errorf("vucc query: %w", err)
	}

	var worked int64
	for _, row := range rows {
		worked++
		var lastQSOAt *time.Time
		if row.LastQso.Valid {
			t := row.LastQso.Time.UTC()
			lastQSOAt = &t
		}
		if err := q.UpsertAwardProgress(ctx, db.UpsertAwardProgressParams{
			UserID:    userID,
			AwardType: string(awards.AwardVUCC),
			EntityKey: row.GridSquare,
			Worked:    true,
			Confirmed: false,
			QsoCount:  row.QsoCount,
			LastQsoAt: toTS(lastQSOAt),
		}); err != nil {
			return 0, fmt.Errorf("upsert vucc grid %s: %w", row.GridSquare, err)
		}
	}
	return worked, nil
}

func (w *AwardRefreshWorker) refreshWAZ(ctx context.Context, q *db.Queries, userID int64) (int64, error) {
	rows, err := q.ListWorkedZones(ctx)
	if err != nil {
		return 0, fmt.Errorf("waz query: %w", err)
	}

	var worked int64
	for _, row := range rows {
		worked++
		var lastQSOAt *time.Time
		if row.FirstQso.Valid {
			t := row.FirstQso.Time.UTC()
			lastQSOAt = &t
		}
		confirmed, _ := row.Confirmed.(bool)
		if err := q.UpsertAwardProgress(ctx, db.UpsertAwardProgressParams{
			UserID:    userID,
			AwardType: string(awards.AwardWAZ),
			EntityKey: fmt.Sprintf("%d", row.Zone),
			Worked:    true,
			Confirmed: confirmed,
			QsoCount:  row.QsoCount,
			LastQsoAt: toTS(lastQSOAt),
		}); err != nil {
			return 0, fmt.Errorf("upsert waz zone %d: %w", row.Zone, err)
		}
	}
	return worked, nil
}

func (w *AwardRefreshWorker) refreshWPX(ctx context.Context, q *db.Queries, userID int64) (int64, error) {
	rows, err := q.ListWorkedWPXPrefixes(ctx)
	if err != nil {
		return 0, fmt.Errorf("wpx query: %w", err)
	}

	// Aggregate by WPX prefix (multiple callsigns may share a prefix).
	type prefixAgg struct {
		count     int64
		confirmed bool
		firstQSO  *time.Time
	}
	prefixes := make(map[string]*prefixAgg)

	for _, row := range rows {
		prefix := awards.WPXPrefix(row.Callsign)
		if prefix == "" {
			continue
		}
		agg, ok := prefixes[prefix]
		if !ok {
			agg = &prefixAgg{}
			prefixes[prefix] = agg
		}
		agg.count += row.QsoCount
		confirmed, _ := row.Confirmed.(bool)
		if confirmed {
			agg.confirmed = true
		}
		if row.FirstQso.Valid {
			t := row.FirstQso.Time.UTC()
			if agg.firstQSO == nil || t.Before(*agg.firstQSO) {
				agg.firstQSO = &t
			}
		}
	}

	var worked int64
	for prefix, agg := range prefixes {
		worked++
		if err := q.UpsertAwardProgress(ctx, db.UpsertAwardProgressParams{
			UserID:    userID,
			AwardType: string(awards.AwardWPX),
			EntityKey: prefix,
			Worked:    true,
			Confirmed: agg.confirmed,
			QsoCount:  agg.count,
			LastQsoAt: toTS(agg.firstQSO),
		}); err != nil {
			return 0, fmt.Errorf("upsert wpx prefix %s: %w", prefix, err)
		}
	}
	return worked, nil
}

func (w *AwardRefreshWorker) refreshPOTAHunter(ctx context.Context, q *db.Queries, userID int64) (int64, error) {
	rows, err := q.ListHuntedPOTAParks(ctx)
	if err != nil {
		return 0, fmt.Errorf("pota_hunter query: %w", err)
	}

	var worked int64
	for _, row := range rows {
		worked++
		var lastQSOAt *time.Time
		if row.FirstQso.Valid {
			t := row.FirstQso.Time.UTC()
			lastQSOAt = &t
		}
		if err := q.UpsertAwardProgress(ctx, db.UpsertAwardProgressParams{
			UserID:    userID,
			AwardType: string(awards.AwardPOTAHunter),
			EntityKey: row.ParkRef,
			Worked:    true,
			Confirmed: row.Confirmed,
			QsoCount:  row.QsoCount,
			LastQsoAt: toTS(lastQSOAt),
		}); err != nil {
			return 0, fmt.Errorf("upsert pota_hunter %s: %w", row.ParkRef, err)
		}
	}
	return worked, nil
}

func (w *AwardRefreshWorker) refreshPOTAActivator(ctx context.Context, q *db.Queries, userID int64) (int64, error) {
	rows, err := q.ListActivatedPOTAParks(ctx)
	if err != nil {
		return 0, fmt.Errorf("pota_activator query: %w", err)
	}

	var worked int64
	for _, row := range rows {
		worked++
		var lastQSOAt *time.Time
		if row.FirstQso.Valid {
			t := row.FirstQso.Time.UTC()
			lastQSOAt = &t
		}
		if err := q.UpsertAwardProgress(ctx, db.UpsertAwardProgressParams{
			UserID:    userID,
			AwardType: string(awards.AwardPOTAActivator),
			EntityKey: row.ParkRef,
			Worked:    true,
			Confirmed: false,
			QsoCount:  row.QsoCount,
			LastQsoAt: toTS(lastQSOAt),
		}); err != nil {
			return 0, fmt.Errorf("upsert pota_activator %s: %w", row.ParkRef, err)
		}
	}
	return worked, nil
}

func (w *AwardRefreshWorker) refreshSOTAChaser(ctx context.Context, q *db.Queries, userID int64) (int64, error) {
	rows, err := q.ListWorkedSOTASummits(ctx)
	if err != nil {
		return 0, fmt.Errorf("sota_chaser query: %w", err)
	}

	var worked int64
	for _, row := range rows {
		worked++
		var lastQSOAt *time.Time
		if row.FirstQso.Valid {
			t := row.FirstQso.Time.UTC()
			lastQSOAt = &t
		}
		if err := q.UpsertAwardProgress(ctx, db.UpsertAwardProgressParams{
			UserID:    userID,
			AwardType: string(awards.AwardSOTAChaser),
			EntityKey: row.SummitRef,
			Worked:    true,
			Confirmed: row.Confirmed,
			QsoCount:  row.QsoCount,
			LastQsoAt: toTS(lastQSOAt),
		}); err != nil {
			return 0, fmt.Errorf("upsert sota_chaser %s: %w", row.SummitRef, err)
		}
	}
	return worked, nil
}

func (w *AwardRefreshWorker) refreshSOTAActivator(ctx context.Context, q *db.Queries, userID int64) (int64, error) {
	rows, err := q.ListActivatedSOTASummits(ctx)
	if err != nil {
		return 0, fmt.Errorf("sota_activator query: %w", err)
	}

	var worked int64
	for _, row := range rows {
		worked++
		var lastQSOAt *time.Time
		if row.FirstQso.Valid {
			t := row.FirstQso.Time.UTC()
			lastQSOAt = &t
		}
		if err := q.UpsertAwardProgress(ctx, db.UpsertAwardProgressParams{
			UserID:    userID,
			AwardType: string(awards.AwardSOTAActivator),
			EntityKey: row.SummitRef,
			Worked:    true,
			Confirmed: false, // SOTA activator confirmation is via the SOTA database, not QSL
			QsoCount:  row.QsoCount,
			LastQsoAt: toTS(lastQSOAt),
		}); err != nil {
			return 0, fmt.Errorf("upsert sota_activator %s: %w", row.SummitRef, err)
		}
	}
	return worked, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Milestone notification
// ─────────────────────────────────────────────────────────────────────────────

// checkMilestones fires an award_milestone notification when the worked count
// crosses a threshold for the first time. Duplicate suppression prevents
// re-firing the same milestone within 365 days.
func (w *AwardRefreshWorker) checkMilestones(
	ctx context.Context,
	conn *pgx.Conn,
	queries *db.Queries,
	userID int64,
	at awards.AwardType,
	worked int64,
	log *slog.Logger,
) error {
	milestones := awards.MilestonesFor(at)
	for _, m := range milestones {
		if worked < m.Count {
			continue
		}

		// Dedup: skip if we already fired this milestone notification.
		var alreadySent int64
		err := conn.QueryRow(ctx,
			`SELECT COUNT(*)::bigint FROM notifications
             WHERE user_id = $1
               AND type = 'award_milestone'
               AND payload->>'award_type' = $2
               AND (payload->>'milestone')::bigint = $3
               AND created_at > NOW() - INTERVAL '365 days'`,
			userID, string(at), m.Count,
		).Scan(&alreadySent)
		if err != nil || alreadySent > 0 {
			continue
		}

		payload, marshalErr := json.Marshal(map[string]any{
			"award_type": string(at),
			"milestone":  m.Count,
			"label":      m.Label,
			"worked":     worked,
		})
		if marshalErr != nil {
			log.Warn("award_refresh: marshal milestone payload", slog.String("error", marshalErr.Error()))
			continue
		}

		if _, notifErr := queries.CreateWorkerNotification(ctx, db.CreateWorkerNotificationParams{
			UserID:  userID,
			Type:    "award_milestone",
			Payload: payload,
		}); notifErr != nil {
			log.Warn("award_refresh: create milestone notification",
				slog.String("award_type", string(at)),
				slog.Int64("milestone", m.Count),
				slog.String("error", notifErr.Error()),
			)
		}
	}
	return nil
}
