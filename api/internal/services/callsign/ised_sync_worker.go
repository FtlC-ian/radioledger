package callsign

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
)

// ISEDWeeklySyncArgs is the River job payload for a full ISED dump sync.
type ISEDWeeklySyncArgs struct{}

func (ISEDWeeklySyncArgs) Kind() string { return "ised_weekly_sync" }

// ISEDWeeklySyncWorker downloads the full ISED amateur dump, parses it, and
// batch-UPSERTs records into callsign_records.
type ISEDWeeklySyncWorker struct {
	river.WorkerDefaults[ISEDWeeklySyncArgs]
	Pool *pgxpool.Pool
}

// Timeout overrides River's default 60s job timeout.
func (w *ISEDWeeklySyncWorker) Timeout(*river.Job[ISEDWeeklySyncArgs]) time.Duration {
	return 20 * time.Minute
}

// Work executes the full ISED dump import.
func (w *ISEDWeeklySyncWorker) Work(ctx context.Context, job *river.Job[ISEDWeeklySyncArgs]) error {
	log := slog.With(
		slog.String("job_kind", job.Args.Kind()),
		slog.Int64("job_id", job.ID),
	)
	log.Info("ised_weekly_sync: started")

	runID, err := startSyncRun(ctx, w.Pool, "ised", "full")
	if err != nil {
		return fmt.Errorf("ised_weekly_sync: start run: %w", err)
	}

	result, parseErr := ParseISEDZip(ctx, ISEDFullDumpURL)
	if parseErr != nil {
		_ = failSyncRun(ctx, w.Pool, runID, parseErr.Error())
		return fmt.Errorf("ised_weekly_sync: parse: %w", parseErr)
	}

	added, updated, upsertErr := batchUpsertRecords(ctx, w.Pool, result.Records)
	if upsertErr != nil {
		_ = failSyncRun(ctx, w.Pool, runID, upsertErr.Error())
		return fmt.Errorf("ised_weekly_sync: upsert: %w", upsertErr)
	}

	if err := completeSyncRun(ctx, w.Pool, runID, result.Processed, added, updated, 0); err != nil {
		log.Error("ised_weekly_sync: complete run", slog.String("error", err.Error()))
	}

	log.Info("ised_weekly_sync: complete",
		slog.Int("processed", result.Processed),
		slog.Int("added", added),
		slog.Int("updated", updated),
	)
	return nil
}
