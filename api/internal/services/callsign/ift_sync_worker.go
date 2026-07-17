package callsign

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
)

// IFTWeeklySyncArgs is the River job payload for a full IFT dump sync.
type IFTWeeklySyncArgs struct{}

func (IFTWeeklySyncArgs) Kind() string { return "ift_weekly_sync" }

// IFTWeeklySyncWorker downloads the full IFT amateur dump, parses it, and
// batch-UPSERTs records into callsign_records.
type IFTWeeklySyncWorker struct {
	river.WorkerDefaults[IFTWeeklySyncArgs]
	Pool *pgxpool.Pool
}

// Timeout overrides River's default 60s job timeout.
func (w *IFTWeeklySyncWorker) Timeout(*river.Job[IFTWeeklySyncArgs]) time.Duration {
	return 20 * time.Minute
}

// Work executes the full IFT dump import.
func (w *IFTWeeklySyncWorker) Work(ctx context.Context, job *river.Job[IFTWeeklySyncArgs]) error {
	log := slog.With(
		slog.String("job_kind", job.Args.Kind()),
		slog.Int64("job_id", job.ID),
	)
	log.Info("ift_weekly_sync: started")

	runID, err := startSyncRun(ctx, w.Pool, "ift", "full")
	if err != nil {
		return fmt.Errorf("ift_weekly_sync: start run: %w", err)
	}

	result, parseErr := ParseIFTCSV(ctx, IFTFullDumpURL)
	if parseErr != nil {
		_ = failSyncRun(ctx, w.Pool, runID, parseErr.Error())
		return fmt.Errorf("ift_weekly_sync: parse: %w", parseErr)
	}

	added, updated, upsertErr := batchUpsertRecords(ctx, w.Pool, result.Records)
	if upsertErr != nil {
		_ = failSyncRun(ctx, w.Pool, runID, upsertErr.Error())
		return fmt.Errorf("ift_weekly_sync: upsert: %w", upsertErr)
	}

	if err := completeSyncRun(ctx, w.Pool, runID, result.Processed, added, updated, 0); err != nil {
		log.Error("ift_weekly_sync: complete run", slog.String("error", err.Error()))
	}

	log.Info("ift_weekly_sync: complete",
		slog.Int("processed", result.Processed),
		slog.Int("added", added),
		slog.Int("updated", updated),
	)
	return nil
}
