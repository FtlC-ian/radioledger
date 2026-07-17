package callsign

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
)

// ANFRWeeklySyncArgs is the River job payload for a full ANFR dump sync.
type ANFRWeeklySyncArgs struct{}

func (ANFRWeeklySyncArgs) Kind() string { return "anfr_weekly_sync" }

// ANFRWeeklySyncWorker downloads the full ANFR amateur dump, parses it, and
// batch-UPSERTs records into callsign_records.
type ANFRWeeklySyncWorker struct {
	river.WorkerDefaults[ANFRWeeklySyncArgs]
	Pool *pgxpool.Pool
}

// Timeout overrides River's default 60s job timeout.
func (w *ANFRWeeklySyncWorker) Timeout(*river.Job[ANFRWeeklySyncArgs]) time.Duration {
	return 20 * time.Minute
}

// Work executes the full ANFR dump import.
func (w *ANFRWeeklySyncWorker) Work(ctx context.Context, job *river.Job[ANFRWeeklySyncArgs]) error {
	log := slog.With(
		slog.String("job_kind", job.Args.Kind()),
		slog.Int64("job_id", job.ID),
	)
	log.Info("anfr_weekly_sync: started")

	runID, err := startSyncRun(ctx, w.Pool, "anfr", "full")
	if err != nil {
		return fmt.Errorf("anfr_weekly_sync: start run: %w", err)
	}

	result, parseErr := ParseANFRCSV(ctx, ANFRFullDumpURL)
	if parseErr != nil {
		_ = failSyncRun(ctx, w.Pool, runID, parseErr.Error())
		return fmt.Errorf("anfr_weekly_sync: parse: %w", parseErr)
	}

	added, updated, upsertErr := batchUpsertRecords(ctx, w.Pool, result.Records)
	if upsertErr != nil {
		_ = failSyncRun(ctx, w.Pool, runID, upsertErr.Error())
		return fmt.Errorf("anfr_weekly_sync: upsert: %w", upsertErr)
	}

	if err := completeSyncRun(ctx, w.Pool, runID, result.Processed, added, updated, 0); err != nil {
		log.Error("anfr_weekly_sync: complete run", slog.String("error", err.Error()))
	}

	log.Info("anfr_weekly_sync: complete",
		slog.Int("processed", result.Processed),
		slog.Int("added", added),
		slog.Int("updated", updated),
	)
	return nil
}
