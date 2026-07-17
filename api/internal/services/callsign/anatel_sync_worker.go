package callsign

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
)

// ANATELWeeklySyncArgs is the River job payload for a full ANATEL radioamador sync.
// ANATEL (Agência Nacional de Telecomunicações) is Brazil's telecom regulator.
// Runs once per week; the dataset is a full snapshot so each run replaces stale records
// via the ON CONFLICT … DO UPDATE upsert path.
type ANATELWeeklySyncArgs struct{}

func (ANATELWeeklySyncArgs) Kind() string { return "anatel_weekly_sync" }

// ANATELWeeklySyncWorker downloads the full ANATEL radioamador CSV, parses it,
// and batch-UPSERTs records into callsign_records.
type ANATELWeeklySyncWorker struct {
	river.WorkerDefaults[ANATELWeeklySyncArgs]
	Pool *pgxpool.Pool
}

// Timeout overrides River's default 60s job timeout.
// The ANATEL CSV is moderately large (tens of thousands of records); 30 minutes
// provides ample headroom for a slow upstream connection.
func (w *ANATELWeeklySyncWorker) Timeout(*river.Job[ANATELWeeklySyncArgs]) time.Duration {
	return 30 * time.Minute
}

// Work executes the full ANATEL radioamador import.
func (w *ANATELWeeklySyncWorker) Work(ctx context.Context, job *river.Job[ANATELWeeklySyncArgs]) error {
	log := slog.With(
		slog.String("job_kind", job.Args.Kind()),
		slog.Int64("job_id", job.ID),
	)
	log.Info("anatel_weekly_sync: started")

	runID, err := startSyncRun(ctx, w.Pool, "anatel", "full")
	if err != nil {
		return fmt.Errorf("anatel_weekly_sync: start run: %w", err)
	}

	result, parseErr := ParseANATELCSV(ctx, ANATELFullDumpURL)
	if parseErr != nil {
		_ = failSyncRun(ctx, w.Pool, runID, parseErr.Error())
		return fmt.Errorf("anatel_weekly_sync: parse: %w", parseErr)
	}

	added, updated, upsertErr := batchUpsertRecords(ctx, w.Pool, result.Records)
	if upsertErr != nil {
		_ = failSyncRun(ctx, w.Pool, runID, upsertErr.Error())
		return fmt.Errorf("anatel_weekly_sync: upsert: %w", upsertErr)
	}

	if err := completeSyncRun(ctx, w.Pool, runID, result.Processed, added, updated, 0); err != nil {
		log.Error("anatel_weekly_sync: complete run", slog.String("error", err.Error()))
	}

	log.Info("anatel_weekly_sync: complete",
		slog.Int("processed", result.Processed),
		slog.Int("added", added),
		slog.Int("updated", updated),
	)
	return nil
}
