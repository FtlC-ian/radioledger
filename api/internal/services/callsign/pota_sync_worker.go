package callsign

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
)

const (
	// PotaCSVURL is the canonical POTA all-parks CSV endpoint.
	PotaCSVURL = "https://pota.app/all_parks.csv"

	potaUpsertBatchSize = 2000
)

// ─────────────────────────────────────────────────────────────────────────────
// Job Args
// ─────────────────────────────────────────────────────────────────────────────

// PotaSyncArgs is the River job payload for a full POTA parks CSV sync.
type PotaSyncArgs struct{}

func (PotaSyncArgs) Kind() string { return "pota_park_sync" }

// ─────────────────────────────────────────────────────────────────────────────
// PotaSyncWorker
// ─────────────────────────────────────────────────────────────────────────────

// PotaSyncWorker downloads the POTA all_parks.csv, parses it, and upserts every
// park into the pota_parks table.  Parks no longer present in the feed are
// marked inactive.  Sync progress is tracked in callsign_sync_runs.
//
// Schedule: weekly (e.g. every Sunday at 4am UTC).
type PotaSyncWorker struct {
	river.WorkerDefaults[PotaSyncArgs]
	Pool *pgxpool.Pool
}

// Timeout overrides River's default 60s job timeout.  The POTA CSV is ~3 MB
// and contains ~32k parks; parsing + upsert is typically under 2 minutes.
func (w *PotaSyncWorker) Timeout(*river.Job[PotaSyncArgs]) time.Duration {
	return 20 * time.Minute
}

// Work executes the POTA parks sync.
func (w *PotaSyncWorker) Work(ctx context.Context, job *river.Job[PotaSyncArgs]) error {
	log := slog.With(
		slog.String("job_kind", job.Args.Kind()),
		slog.Int64("job_id", job.ID),
	)
	log.Info("pota_park_sync: started")

	runID, err := startSyncRun(ctx, w.Pool, "pota_parks", "full")
	if err != nil {
		return fmt.Errorf("pota_park_sync: start run: %w", err)
	}

	parks, parseErr := parsePotaCSV(ctx, PotaCSVURL)
	if parseErr != nil {
		_ = failSyncRun(ctx, w.Pool, runID, parseErr.Error())
		return fmt.Errorf("pota_park_sync: parse: %w", parseErr)
	}

	added, updated, upsertErr := upsertPotaParks(ctx, w.Pool, parks)
	if upsertErr != nil {
		_ = failSyncRun(ctx, w.Pool, runID, upsertErr.Error())
		return fmt.Errorf("pota_park_sync: upsert: %w", upsertErr)
	}

	removed, deactivateErr := deactivateMissingParks(ctx, w.Pool, parks)
	if deactivateErr != nil {
		// Non-fatal: log but don't fail the run.
		log.Error("pota_park_sync: deactivate missing", slog.String("error", deactivateErr.Error()))
	}

	if err := completeSyncRun(ctx, w.Pool, runID, len(parks), added, updated, removed); err != nil {
		log.Error("pota_park_sync: complete run", slog.String("error", err.Error()))
	}

	log.Info("pota_park_sync: complete",
		slog.Int("processed", len(parks)),
		slog.Int("added", added),
		slog.Int("updated", updated),
		slog.Int("deactivated", removed),
	)
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// CSV parser
// ─────────────────────────────────────────────────────────────────────────────

// potaPark holds a single row from the POTA parks CSV.
type potaPark struct {
	Ref           string  // park_ref (e.g. "US-0001")
	Name          string  // park name
	Active        bool    // active flag
	Country       string  // derived from locationDesc prefix (e.g. "US")
	StateProvince string  // derived from locationDesc suffix (e.g. "ME")
	Latitude      *float64
	Longitude     *float64
}

// parsePotaCSV downloads and parses the POTA all_parks.csv.
// The CSV format is: reference,name,active,entityId,locationDesc
// locationDesc is a dash-separated code like "US-ME" or "CA-BC".
// lat/lng are not included in the bulk CSV; those fields will be NULL.
func parsePotaCSV(ctx context.Context, url string) ([]potaPark, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", "RadioLedger/1.0 (park-sync)")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download CSV: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected HTTP %d from %s", resp.StatusCode, url)
	}

	r := csv.NewReader(resp.Body)
	r.TrimLeadingSpace = true
	r.FieldsPerRecord = -1 // tolerate rows with extra fields

	// Read and validate header row.
	header, err := r.Read()
	if err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}
	colIdx := csvColumnIndex(header)
	refCol := colIdx["reference"]
	nameCol := colIdx["name"]
	activeCol := colIdx["active"]
	locCol := colIdx["locationDesc"]

	if refCol < 0 || nameCol < 0 {
		return nil, fmt.Errorf("unexpected CSV headers: %v", header)
	}

	var parks []potaPark
	lineNum := 1
	for {
		row, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("parse row %d: %w", lineNum, err)
		}
		lineNum++

		ref := strings.TrimSpace(safeCol(row, refCol))
		name := strings.TrimSpace(safeCol(row, nameCol))
		if ref == "" || name == "" {
			continue
		}

		active := true
		if activeCol >= 0 {
			v := strings.TrimSpace(safeCol(row, activeCol))
			active = v == "1" || strings.EqualFold(v, "true")
		}

		country, stateProv := "", ""
		if locCol >= 0 {
			loc := strings.TrimSpace(safeCol(row, locCol))
			country, stateProv = splitLocationDesc(loc)
		}
		if country == "" {
			// Fall back: use the prefix of the park ref itself.
			country, _ = splitLocationDesc(ref)
		}

		parks = append(parks, potaPark{
			Ref:           ref,
			Name:          name,
			Active:        active,
			Country:       country,
			StateProvince: stateProv,
		})
	}

	return parks, nil
}

// splitLocationDesc splits "US-ME" into ("US", "ME"), "CA-BC" into ("CA", "BC"),
// and "AQ-AQ" into ("AQ", "AQ").  Single-segment codes return (code, "").
func splitLocationDesc(loc string) (country, region string) {
	if idx := strings.Index(loc, "-"); idx >= 0 {
		return loc[:idx], loc[idx+1:]
	}
	return loc, ""
}

// ─────────────────────────────────────────────────────────────────────────────
// Upsert
// ─────────────────────────────────────────────────────────────────────────────

// upsertPotaParks inserts/updates pota_parks in batches.
// Returns (added, updated, error).  Uses unnest for bulk throughput.
func upsertPotaParks(ctx context.Context, pool *pgxpool.Pool, parks []potaPark) (added, updated int, err error) {
	if len(parks) == 0 {
		return 0, 0, nil
	}

	for i := 0; i < len(parks); i += potaUpsertBatchSize {
		end := i + potaUpsertBatchSize
		if end > len(parks) {
			end = len(parks)
		}
		a, u, bErr := upsertPotaBatch(ctx, pool, parks[i:end])
		if bErr != nil {
			return added, updated, fmt.Errorf("batch [%d:%d]: %w", i, end, bErr)
		}
		added += a
		updated += u
	}
	return added, updated, nil
}

// upsertPotaBatch upserts one chunk of pota_parks rows.
func upsertPotaBatch(ctx context.Context, pool *pgxpool.Pool, batch []potaPark) (added, updated int, err error) {
	refs := make([]string, len(batch))
	names := make([]string, len(batch))
	countries := make([]string, len(batch))
	stateProvs := make([]*string, len(batch))
	lats := make([]*float64, len(batch))
	lons := make([]*float64, len(batch))
	actives := make([]bool, len(batch))

	for i, p := range batch {
		refs[i] = p.Ref
		names[i] = p.Name
		countries[i] = p.Country
		if p.Country == "" {
			countries[i] = "??" // NOT NULL column; use placeholder if unknown
		}
		if p.StateProvince != "" {
			s := p.StateProvince
			stateProvs[i] = &s
		}
		lats[i] = p.Latitude
		lons[i] = p.Longitude
		actives[i] = p.Active
	}

	// Use a subquery with named columns so the CASE for PostGIS geometry is clean.
	tag, err := pool.Exec(ctx, `
		INSERT INTO pota_parks (
			park_ref, name, country, state_province,
			latitude, longitude, location,
			active, updated_at
		)
		SELECT
			t.ref,
			t.name,
			t.country,
			t.state_prov,
			t.lat,
			t.lon,
			CASE WHEN t.lat IS NOT NULL AND t.lon IS NOT NULL
			     THEN ST_SetSRID(ST_MakePoint(t.lon, t.lat), 4326)
			     ELSE NULL
			END,
			t.active,
			now()
		FROM unnest(
			$1::text[], $2::text[], $3::text[], $4::text[],
			$5::float8[], $6::float8[], $7::bool[]
		) AS t(ref, name, country, state_prov, lat, lon, active)
		ON CONFLICT (park_ref) DO UPDATE SET
			name           = EXCLUDED.name,
			country        = EXCLUDED.country,
			state_province = EXCLUDED.state_province,
			latitude       = COALESCE(EXCLUDED.latitude, pota_parks.latitude),
			longitude      = COALESCE(EXCLUDED.longitude, pota_parks.longitude),
			location       = COALESCE(EXCLUDED.location, pota_parks.location),
			active         = EXCLUDED.active,
			updated_at     = now()
	`,
		refs, names, countries, stateProvs,
		lats, lons, actives,
	)
	if err != nil {
		return 0, 0, err
	}
	return 0, int(tag.RowsAffected()), nil
}

// deactivateMissingParks marks any park not present in the latest CSV as inactive.
// Returns the number of rows deactivated.
func deactivateMissingParks(ctx context.Context, pool *pgxpool.Pool, parks []potaPark) (int, error) {
	if len(parks) == 0 {
		return 0, nil
	}

	// Collect the full set of refs from the latest feed.
	refs := make([]string, len(parks))
	for i, p := range parks {
		refs[i] = p.Ref
	}

	tag, err := pool.Exec(ctx, `
		UPDATE pota_parks
		SET active = FALSE, updated_at = now()
		WHERE park_ref != ALL($1::text[])
		  AND active = TRUE
	`, refs)
	if err != nil {
		return 0, fmt.Errorf("deactivate missing parks: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Compile-time interface check
// ─────────────────────────────────────────────────────────────────────────────

var _ river.Worker[PotaSyncArgs] = (*PotaSyncWorker)(nil)
