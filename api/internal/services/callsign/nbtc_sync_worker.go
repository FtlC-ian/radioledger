package callsign

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
)

// NbtcSyncArgs is the River job payload for a full NBTC dump sync.
type NbtcSyncArgs struct{}

func (NbtcSyncArgs) Kind() string { return "nbtc_sync" }

// NbtcSyncWorker downloads the full NBTC amateur dump, parses it, and
// batch-UPSERTs records into callsign_records.
type NbtcSyncWorker struct {
	river.WorkerDefaults[NbtcSyncArgs]
	Pool *pgxpool.Pool
}

// Timeout overrides River's default 60s job timeout.
func (w *NbtcSyncWorker) Timeout(*river.Job[NbtcSyncArgs]) time.Duration {
	return 20 * time.Minute
}

// Work executes the full NBTC dump import.
func (w *NbtcSyncWorker) Work(ctx context.Context, job *river.Job[NbtcSyncArgs]) error {
	log := slog.With(
		slog.String("job_kind", job.Args.Kind()),
		slog.Int64("job_id", job.ID),
	)
	log.Info("nbtc_sync: started")

	runID, err := startSyncRun(ctx, w.Pool, nbtcSource, "full")
	if err != nil {
		return fmt.Errorf("nbtc_sync: start run: %w", err)
	}

	result, parseErr := ParseNBTC(ctx, NBTCFullDumpURL)
	if parseErr != nil {
		_ = failSyncRun(ctx, w.Pool, runID, parseErr.Error())
		return fmt.Errorf("nbtc_sync: parse: %w", parseErr)
	}

	added, updated, upsertErr := batchUpsertRecords(ctx, w.Pool, result.Records)
	if upsertErr != nil {
		_ = failSyncRun(ctx, w.Pool, runID, upsertErr.Error())
		return fmt.Errorf("nbtc_sync: upsert: %w", upsertErr)
	}

	if err := completeSyncRun(ctx, w.Pool, runID, result.Processed, added, updated, 0); err != nil {
		log.Error("nbtc_sync: complete run", slog.String("error", err.Error()))
	}

	log.Info("nbtc_sync: complete",
		slog.Int("processed", result.Processed),
		slog.Int("added", added),
		slog.Int("updated", updated),
	)
	return nil
}
