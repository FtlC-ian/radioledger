package callsign

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
)

// JJ1WTLMonthlySyncArgs is the River job payload for a full JJ1WTL MIC-derived dump sync.
type JJ1WTLMonthlySyncArgs struct{}

func (JJ1WTLMonthlySyncArgs) Kind() string { return "jj1wtl_monthly_sync" }

// JJ1WTLMonthlySyncWorker downloads the latest annual JJ1WTL CSV export,
// parses it, and batch-UPSERTs records into callsign_records.
type JJ1WTLMonthlySyncWorker struct {
	river.WorkerDefaults[JJ1WTLMonthlySyncArgs]
	Pool *pgxpool.Pool
}

// Timeout overrides River's default 60s job timeout.
func (w *JJ1WTLMonthlySyncWorker) Timeout(*river.Job[JJ1WTLMonthlySyncArgs]) time.Duration {
	return 45 * time.Minute
}

// Work executes the full JJ1WTL dump import.
func (w *JJ1WTLMonthlySyncWorker) Work(ctx context.Context, job *river.Job[JJ1WTLMonthlySyncArgs]) error {
	log := slog.With(
		slog.String("job_kind", job.Args.Kind()),
		slog.Int64("job_id", job.ID),
	)
	log.Info("jj1wtl_monthly_sync: started")

	runID, err := startSyncRun(ctx, w.Pool, "jj1wtl", "full")
	if err != nil {
		return fmt.Errorf("jj1wtl_monthly_sync: start run: %w", err)
	}

	result, parseErr := ParseJJ1WTL(ctx, JJ1WTLLicenseSearchURL)
	if parseErr != nil {
		_ = failSyncRun(ctx, w.Pool, runID, parseErr.Error())
		return fmt.Errorf("jj1wtl_monthly_sync: parse: %w", parseErr)
	}

	added, updated, upsertErr := batchUpsertRecords(ctx, w.Pool, result.Records)
	if upsertErr != nil {
		_ = failSyncRun(ctx, w.Pool, runID, upsertErr.Error())
		return fmt.Errorf("jj1wtl_monthly_sync: upsert: %w", upsertErr)
	}

	if err := completeSyncRun(ctx, w.Pool, runID, result.Processed, added, updated, 0); err != nil {
		log.Error("jj1wtl_monthly_sync: complete run", slog.String("error", err.Error()))
	}

	log.Info("jj1wtl_monthly_sync: complete",
		slog.Int("processed", result.Processed),
		slog.Int("added", added),
		slog.Int("updated", updated),
	)
	return nil
}
