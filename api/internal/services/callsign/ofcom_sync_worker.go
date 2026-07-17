package callsign

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
)

// OfcomWeeklySyncArgs is the River job payload for a full Ofcom dump sync.
type OfcomWeeklySyncArgs struct{}

func (OfcomWeeklySyncArgs) Kind() string { return "ofcom_weekly_sync" }

// OfcomWeeklySyncWorker downloads the full Ofcom amateur dump, parses it, and
// batch-UPSERTs records into callsign_records.
type OfcomWeeklySyncWorker struct {
	river.WorkerDefaults[OfcomWeeklySyncArgs]
	Pool *pgxpool.Pool
}

// Timeout overrides River's default 60s job timeout.
func (w *OfcomWeeklySyncWorker) Timeout(*river.Job[OfcomWeeklySyncArgs]) time.Duration {
	return 20 * time.Minute
}

// Work executes the full Ofcom dump import.
func (w *OfcomWeeklySyncWorker) Work(ctx context.Context, job *river.Job[OfcomWeeklySyncArgs]) error {
	log := slog.With(
		slog.String("job_kind", job.Args.Kind()),
		slog.Int64("job_id", job.ID),
	)
	log.Info("ofcom_weekly_sync: started")

	runID, err := startSyncRun(ctx, w.Pool, "ofcom", "full")
	if err != nil {
		return fmt.Errorf("ofcom_weekly_sync: start run: %w", err)
	}

	result, parseErr := ParseOfcom(ctx, OfcomFullDumpURL)
	if parseErr != nil {
		_ = failSyncRun(ctx, w.Pool, runID, parseErr.Error())
		return fmt.Errorf("ofcom_weekly_sync: parse: %w", parseErr)
	}

	added, updated, upsertErr := batchUpsertRecords(ctx, w.Pool, result.Records)
	if upsertErr != nil {
		_ = failSyncRun(ctx, w.Pool, runID, upsertErr.Error())
		return fmt.Errorf("ofcom_weekly_sync: upsert: %w", upsertErr)
	}

	if err := completeSyncRun(ctx, w.Pool, runID, result.Processed, added, updated, 0); err != nil {
		log.Error("ofcom_weekly_sync: complete run", slog.String("error", err.Error()))
	}

	log.Info("ofcom_weekly_sync: complete",
		slog.Int("processed", result.Processed),
		slog.Int("added", added),
		slog.Int("updated", updated),
	)
	return nil
}
