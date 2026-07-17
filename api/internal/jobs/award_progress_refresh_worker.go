package jobs

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
)

// AwardProgressRefreshArgs is a periodic River job payload that scans for
// dirty award_progress rows and refreshes those users' award caches.
type AwardProgressRefreshArgs struct{}

// Kind returns the unique River job kind identifier.
func (AwardProgressRefreshArgs) Kind() string { return "award_progress_refresh" }

// AwardProgressRefreshWorker scans for users with dirty award rows, then
// recalculates award progress for each user.
type AwardProgressRefreshWorker struct {
	river.WorkerDefaults[AwardProgressRefreshArgs]
	Pool *pgxpool.Pool
}

func (w *AwardProgressRefreshWorker) Work(ctx context.Context, job *river.Job[AwardProgressRefreshArgs]) error {
	log := slog.With(
		slog.String("job_kind", job.Args.Kind()),
		slog.Int64("job_id", job.ID),
	)

	conn, err := w.Pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("award_progress_refresh: acquire conn: %w", err)
	}
	defer conn.Release()

	// Worker role can inspect all users' dirty flags.
	if _, err := conn.Exec(ctx, "SET ROLE radioledger_worker"); err != nil {
		return fmt.Errorf("award_progress_refresh: set worker role: %w", err)
	}

	rows, err := conn.Query(ctx, `
		SELECT DISTINCT user_id
		FROM award_progress
		WHERE dirty = TRUE
		ORDER BY user_id ASC
		LIMIT 200`)
	if err != nil {
		return fmt.Errorf("award_progress_refresh: list dirty users: %w", err)
	}
	defer rows.Close()

	var userIDs []int64
	for rows.Next() {
		var userID int64
		if err := rows.Scan(&userID); err != nil {
			return fmt.Errorf("award_progress_refresh: scan user id: %w", err)
		}
		userIDs = append(userIDs, userID)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("award_progress_refresh: iterate dirty users: %w", err)
	}

	if len(userIDs) == 0 {
		log.Debug("award_progress_refresh: no dirty users")
		return nil
	}

	refreshWorker := &AwardRefreshWorker{Pool: w.Pool}
	for _, userID := range userIDs {
		userLog := log.With(slog.Int64("user_id", userID))
		if err := refreshWorker.RefreshUser(ctx, userID, "", userLog); err != nil {
			userLog.Error("award_progress_refresh: user refresh failed", slog.String("error", err.Error()))
			continue
		}
		userLog.Info("award_progress_refresh: refreshed user awards")
	}

	return nil
}
