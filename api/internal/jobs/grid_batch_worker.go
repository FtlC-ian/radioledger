package jobs

// GridBatchWorker is a River background job that geocodes all callsign records
// that are missing a Maidenhead grid square. It runs daily and uses a
// three-tier lookup strategy:
//
//  1. Existing lat/lon on the record — instant, no external calls.
//  2. Census street geocoder — real-time HTTP call to a US government service.
//  3. Zip centroid from local zip_centroids table — zero external calls.
//
// Results are cached back into callsign_records.grid_square (and lat/lon).
// Progress is logged every 500 records.

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/FtlC-ian/radioledger/api/internal/geo"
)

const (
	// gridBatchSize is the number of callsign records processed per DB query.
	gridBatchSize = 100

	// gridBatchLogInterval controls how often progress is logged.
	gridBatchLogInterval = 500

	// gridBatchCensusDelay is a courtesy pause between Census requests to avoid
	// hammering the geocoder (it has no documented rate limit, but we're polite).
	gridBatchCensusDelay = 100 * time.Millisecond
)

// ──────────────────────────────────────────────────────────────────────────────
// GridBatchArgs
// ──────────────────────────────────────────────────────────────────────────────

// GridBatchArgs holds the (empty) arguments for the grid batch job.
type GridBatchArgs struct{}

// Kind returns the unique River job kind identifier.
func (GridBatchArgs) Kind() string { return "grid_batch" }

// ──────────────────────────────────────────────────────────────────────────────
// GridBatchWorker
// ──────────────────────────────────────────────────────────────────────────────

// GridBatchWorker geocodes callsign records that are missing a grid square.
type GridBatchWorker struct {
	river.WorkerDefaults[GridBatchArgs]
	Pool *pgxpool.Pool
}

// pendingGridRecord holds the data needed to geocode one callsign record.
type pendingGridRecord struct {
	Callsign     string
	Source       string
	AddressLine1 string
	City         string
	State        string
	PostalCode   string
	Country      string
	Latitude     *float64
	Longitude    *float64
}

// Work runs the batch geocoding sweep for all callsign records without a grid.
func (w *GridBatchWorker) Work(ctx context.Context, job *river.Job[GridBatchArgs]) error {
	log := slog.With(slog.String("job_kind", job.Args.Kind()), slog.Int64("job_id", job.ID))
	log.Info("grid_batch: starting sweep")

	var (
		offset    int
		processed int
		updated   int
		skipped   int
	)

	for {
		records, err := w.fetchPending(ctx, offset)
		if err != nil {
			return fmt.Errorf("grid_batch: fetch pending at offset %d: %w", offset, err)
		}
		if len(records) == 0 {
			break
		}

		for _, rec := range records {
			lat, lon, source, ok := w.resolve(ctx, log, rec)
			if !ok {
				skipped++
				processed++
				continue
			}

			grid := geo.LatLonToGrid(lat, lon)
			if grid == "" {
				skipped++
				processed++
				continue
			}
			grid = strings.ToUpper(strings.TrimSpace(grid))

			if err := w.cacheGrid(ctx, rec.Callsign, rec.Source, grid, lat, lon); err != nil {
				log.WarnContext(ctx, "grid_batch: failed to cache grid",
					slog.String("callsign", rec.Callsign),
					slog.String("error", err.Error()))
				skipped++
			} else {
				log.DebugContext(ctx, "grid_batch: cached grid",
					slog.String("callsign", rec.Callsign),
					slog.String("grid", grid),
					slog.String("source", source))
				updated++
			}

			processed++
			if processed%gridBatchLogInterval == 0 {
				log.Info("grid_batch: progress",
					slog.Int("processed", processed),
					slog.Int("updated", updated),
					slog.Int("skipped", skipped))
			}
		}

		offset += len(records)
		if len(records) < gridBatchSize {
			break
		}
	}

	log.Info("grid_batch: sweep complete",
		slog.Int("processed", processed),
		slog.Int("updated", updated),
		slog.Int("skipped", skipped))
	return nil
}

// fetchPending returns a batch of callsign records that need geocoding.
func (w *GridBatchWorker) fetchPending(ctx context.Context, offset int) ([]pendingGridRecord, error) {
	rows, err := w.Pool.Query(ctx, `
		SELECT
			callsign, source,
			COALESCE(address_line1, ''),
			COALESCE(city, ''),
			COALESCE(state_province, ''),
			COALESCE(postal_code, ''),
			COALESCE(country, ''),
			latitude,
			longitude
		FROM callsign_records
		WHERE (grid_square IS NULL OR grid_square = '')
		ORDER BY callsign, source
		LIMIT $1 OFFSET $2
	`, gridBatchSize, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []pendingGridRecord
	for rows.Next() {
		var r pendingGridRecord
		if err := rows.Scan(
			&r.Callsign, &r.Source,
			&r.AddressLine1, &r.City, &r.State, &r.PostalCode, &r.Country,
			&r.Latitude, &r.Longitude,
		); err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}
		records = append(records, r)
	}
	return records, rows.Err()
}

// resolve attempts to determine lat/lon for a record using the available data.
// Returns the coordinates, a source description, and whether resolution succeeded.
func (w *GridBatchWorker) resolve(ctx context.Context, log *slog.Logger, rec pendingGridRecord) (lat, lon float64, source string, ok bool) {
	// 1. Existing coordinates — no external call needed.
	if rec.Latitude != nil && rec.Longitude != nil {
		return *rec.Latitude, *rec.Longitude, "coordinates", true
	}

	// Only geocode US records further (Census and zip centroid data are US-only).
	if !isUSCountry(rec.Country) {
		return 0, 0, "", false
	}

	// 2. Census street geocoder — for records with a usable street address.
	street := strings.TrimSpace(rec.AddressLine1)
	if street != "" && !geo.IsPOBox(street) {
		// Brief pause to avoid hammering Census.
		select {
		case <-ctx.Done():
			return 0, 0, "", false
		case <-time.After(gridBatchCensusDelay):
		}

		clat, clon, err := geo.GeocodeAddress(ctx, street, rec.City, rec.State, rec.PostalCode)
		if err == nil {
			return clat, clon, "census", true
		}
		log.DebugContext(ctx, "grid_batch: census failed, trying zip centroid",
			slog.String("callsign", rec.Callsign),
			slog.String("error", err.Error()))
	}

	// 3. Zip centroid — local DB lookup, zero external calls.
	if zip := strings.TrimSpace(rec.PostalCode); zip != "" {
		zlat, zlon, err := geo.GeocodeFromZipCentroid(ctx, w.Pool, zip)
		if err == nil {
			return zlat, zlon, "zip_centroid", true
		}
		log.DebugContext(ctx, "grid_batch: zip centroid lookup failed",
			slog.String("callsign", rec.Callsign),
			slog.String("zip", zip),
			slog.String("error", err.Error()))
	}

	return 0, 0, "", false
}

// cacheGrid persists the derived grid square and coordinates back into
// callsign_records.
func (w *GridBatchWorker) cacheGrid(ctx context.Context, callsign, source, grid string, lat, lon float64) error {
	_, err := w.Pool.Exec(ctx, `
		UPDATE callsign_records
		SET grid_square = $3,
		    latitude    = $4,
		    longitude   = $5,
		    updated_at  = now()
		WHERE callsign = $1
		  AND source   = $2
	`, callsign, source, grid, lat, lon)
	return err
}

// isUSCountry returns true for records that appear to be US-licensed.
func isUSCountry(country string) bool {
	c := strings.ToUpper(strings.TrimSpace(country))
	return c == "" || c == "US" || c == "USA" || c == "UNITED STATES"
}
