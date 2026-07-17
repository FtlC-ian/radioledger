package callsign

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
)

// SdppiSyncArgs is the River job payload for an SDPPI manual CSV import.
//
// SDPPI (Sumber Daya dan Perangkat Pos dan Informatika) is Indonesia's telecom
// regulator responsible for amateur radio licensing.  Indonesian amateur
// callsigns use the prefixes YB, YC, YD, YE, YF, YG, and YH.
//
// The public lookup portal at https://iar-ikrap.postel.go.id supports
// individual callsign lookups only — there is no bulk CSV/API export.  The
// older SIARAS system (siaras.postel.go.id) no longer resolves.  Until SDPPI
// publishes a bulk data feed, imports are performed from a user-supplied CSV
// file (see ParseSdppiCSVData for the expected column layout).
//
// To trigger a one-off import, enqueue this job via the River admin API with
// a non-empty FilePath field pointing to an uploaded CSV, or via the
// management CLI:
//
//	INSERT INTO river_job (kind, args, state, …)
//	VALUES ('sdppi_sync', '{"file_path":"/data/yd-callsigns.csv"}', 'available', …);
type SdppiSyncArgs struct {
	// FilePath is the absolute path to a CSV file containing SDPPI callsign
	// records.  If empty the worker exits successfully without processing any
	// records (no-op, safe to enqueue as a placeholder).
	FilePath string `json:"file_path"`
}

func (SdppiSyncArgs) Kind() string { return "sdppi_sync" }

// SdppiSyncWorker reads a user-supplied SDPPI callsign CSV and batch-UPSERTs
// the records into callsign_records.
type SdppiSyncWorker struct {
	river.WorkerDefaults[SdppiSyncArgs]
	Pool *pgxpool.Pool
}

// Timeout overrides River's default 60s job timeout.
// CSV files can be large; 30 minutes provides headroom for very large imports.
func (w *SdppiSyncWorker) Timeout(*river.Job[SdppiSyncArgs]) time.Duration {
	return 30 * time.Minute
}

// Work executes the SDPPI CSV import.
func (w *SdppiSyncWorker) Work(ctx context.Context, job *river.Job[SdppiSyncArgs]) error {
	log := slog.With(
		slog.String("job_kind", job.Args.Kind()),
		slog.Int64("job_id", job.ID),
		slog.String("file_path", job.Args.FilePath),
	)
	log.Info("sdppi_sync: started")

	if job.Args.FilePath == "" {
		log.Info("sdppi_sync: no file_path provided, skipping (no-op)")
		return nil
	}

	runID, err := startSyncRun(ctx, w.Pool, "sdppi", "manual")
	if err != nil {
		return fmt.Errorf("sdppi_sync: start run: %w", err)
	}

	result, parseErr := ParseSdppiCSVFile(ctx, job.Args.FilePath)
	if parseErr != nil {
		_ = failSyncRun(ctx, w.Pool, runID, parseErr.Error())
		return fmt.Errorf("sdppi_sync: parse: %w", parseErr)
	}

	added, updated, upsertErr := batchUpsertRecords(ctx, w.Pool, result.Records)
	if upsertErr != nil {
		_ = failSyncRun(ctx, w.Pool, runID, upsertErr.Error())
		return fmt.Errorf("sdppi_sync: upsert: %w", upsertErr)
	}

	if err := completeSyncRun(ctx, w.Pool, runID, result.Processed, added, updated, 0); err != nil {
		log.Error("sdppi_sync: complete run", slog.String("error", err.Error()))
	}

	log.Info("sdppi_sync: complete",
		slog.Int("processed", result.Processed),
		slog.Int("added", added),
		slog.Int("updated", updated),
	)
	return nil
}
