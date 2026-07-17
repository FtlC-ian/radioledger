package callsign

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
)

// BNetzAWeeklySyncArgs is the River job payload for a full BNetzA PDF sync.
type BNetzAWeeklySyncArgs struct{}

func (BNetzAWeeklySyncArgs) Kind() string { return "bnetza_weekly_sync" }

// BNetzAWeeklySyncWorker downloads the full BNetzA PDF, parses it, and
// batch-UPSERTs records into callsign_records.
type BNetzAWeeklySyncWorker struct {
	river.WorkerDefaults[BNetzAWeeklySyncArgs]
	Pool *pgxpool.Pool
}

// Timeout overrides River's default 60s job timeout.
func (w *BNetzAWeeklySyncWorker) Timeout(*river.Job[BNetzAWeeklySyncArgs]) time.Duration {
	return 40 * time.Minute
}

// Work executes the full BNetzA PDF import.
func (w *BNetzAWeeklySyncWorker) Work(ctx context.Context, job *river.Job[BNetzAWeeklySyncArgs]) error {
	log := slog.With(
		slog.String("job_kind", job.Args.Kind()),
		slog.Int64("job_id", job.ID),
	)
	log.Info("bnetza_weekly_sync: started")

	runID, err := startSyncRun(ctx, w.Pool, "bnetza", "full")
	if err != nil {
		return fmt.Errorf("bnetza_weekly_sync: start run: %w", err)
	}

	result, parseErr := ParseBNetzA(ctx, BNetzAFullDumpURL)
	if parseErr != nil {
		_ = failSyncRun(ctx, w.Pool, runID, parseErr.Error())
		return fmt.Errorf("bnetza_weekly_sync: parse: %w", parseErr)
	}

	added, updated, upsertErr := batchUpsertRecords(ctx, w.Pool, result.Records)
	if upsertErr != nil {
		_ = failSyncRun(ctx, w.Pool, runID, upsertErr.Error())
		return fmt.Errorf("bnetza_weekly_sync: upsert: %w", upsertErr)
	}

	if err := completeSyncRun(ctx, w.Pool, runID, result.Processed, added, updated, 0); err != nil {
		log.Error("bnetza_weekly_sync: complete run", slog.String("error", err.Error()))
	}

	log.Info("bnetza_weekly_sync: complete",
		slog.Int("processed", result.Processed),
		slog.Int("added", added),
		slog.Int("updated", updated),
	)
	return nil
}
