package callsign

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
)

// RDIWeeklySyncArgs is the River job payload for a full RDI dump sync.
type RDIWeeklySyncArgs struct{}

func (RDIWeeklySyncArgs) Kind() string { return "rdi_weekly_sync" }

// RDIWeeklySyncWorker downloads the full RDI amateur dump, parses it, and
// batch-UPSERTs records into callsign_records.
type RDIWeeklySyncWorker struct {
	river.WorkerDefaults[RDIWeeklySyncArgs]
	Pool *pgxpool.Pool
}

// Timeout overrides River's default 60s job timeout.
func (w *RDIWeeklySyncWorker) Timeout(*river.Job[RDIWeeklySyncArgs]) time.Duration {
	return 20 * time.Minute
}

// Work executes the full RDI dump import.
func (w *RDIWeeklySyncWorker) Work(ctx context.Context, job *river.Job[RDIWeeklySyncArgs]) error {
	log := slog.With(
		slog.String("job_kind", job.Args.Kind()),
		slog.Int64("job_id", job.ID),
	)
	log.Info("rdi_weekly_sync: started")

	runID, err := startSyncRun(ctx, w.Pool, "rdi", "full")
	if err != nil {
		return fmt.Errorf("rdi_weekly_sync: start run: %w", err)
	}

	result, parseErr := ParseRDI(ctx, RDIFullDumpURL)
	if parseErr != nil {
		_ = failSyncRun(ctx, w.Pool, runID, parseErr.Error())
		return fmt.Errorf("rdi_weekly_sync: parse: %w", parseErr)
	}

	added, updated, upsertErr := batchUpsertRecords(ctx, w.Pool, result.Records)
	if upsertErr != nil {
		_ = failSyncRun(ctx, w.Pool, runID, upsertErr.Error())
		return fmt.Errorf("rdi_weekly_sync: upsert: %w", upsertErr)
	}

	if err := completeSyncRun(ctx, w.Pool, runID, result.Processed, added, updated, 0); err != nil {
		log.Error("rdi_weekly_sync: complete run", slog.String("error", err.Error()))
	}

	log.Info("rdi_weekly_sync: complete",
		slog.Int("processed", result.Processed),
		slog.Int("added", added),
		slog.Int("updated", updated),
	)
	return nil
}
