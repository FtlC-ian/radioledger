package callsign

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
)

// ACMAWeeklySyncArgs is the River job payload for a full ACMA dump sync.
type ACMAWeeklySyncArgs struct{}

func (ACMAWeeklySyncArgs) Kind() string { return "acma_weekly_sync" }

// ACMAWeeklySyncWorker downloads the full ACMA amateur dump, parses it, and
// batch-UPSERTs records into callsign_records.
type ACMAWeeklySyncWorker struct {
	river.WorkerDefaults[ACMAWeeklySyncArgs]
	Pool *pgxpool.Pool
}

// Timeout overrides River's default 60s job timeout.
func (w *ACMAWeeklySyncWorker) Timeout(*river.Job[ACMAWeeklySyncArgs]) time.Duration {
	return 30 * time.Minute
}

// Work executes the full ACMA dump import.
func (w *ACMAWeeklySyncWorker) Work(ctx context.Context, job *river.Job[ACMAWeeklySyncArgs]) error {
	log := slog.With(
		slog.String("job_kind", job.Args.Kind()),
		slog.Int64("job_id", job.ID),
	)
	log.Info("acma_weekly_sync: started")

	runID, err := startSyncRun(ctx, w.Pool, "acma", "full")
	if err != nil {
		return fmt.Errorf("acma_weekly_sync: start run: %w", err)
	}

	result, parseErr := ParseACMAZip(ctx, ACMAFullDumpURL)
	if parseErr != nil {
		_ = failSyncRun(ctx, w.Pool, runID, parseErr.Error())
		return fmt.Errorf("acma_weekly_sync: parse: %w", parseErr)
	}

	added, updated, upsertErr := batchUpsertRecords(ctx, w.Pool, result.Records)
	if upsertErr != nil {
		_ = failSyncRun(ctx, w.Pool, runID, upsertErr.Error())
		return fmt.Errorf("acma_weekly_sync: upsert: %w", upsertErr)
	}

	if err := completeSyncRun(ctx, w.Pool, runID, result.Processed, added, updated, 0); err != nil {
		log.Error("acma_weekly_sync: complete run", slog.String("error", err.Error()))
	}

	log.Info("acma_weekly_sync: complete",
		slog.Int("processed", result.Processed),
		slog.Int("added", added),
		slog.Int("updated", updated),
	)
	return nil
}
