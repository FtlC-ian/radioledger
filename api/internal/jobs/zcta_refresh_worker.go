package jobs

// ZCTARefreshWorker downloads the US Census ZCTA5 centroid file and upserts
// all zip code centroids into the local zip_centroids table.
//
// This worker eliminates the need for any external geocoding service at
// request time. The centroid data is used by GridBatchWorker and the
// GridLookup handler as a last-resort fallback after the Census street
// geocoder.
//
// Data source (zip archive, tab-separated inside):
//
//	https://www2.census.gov/geo/docs/maps-data/data/gazetteer/2025_Gazetteer/2025_Gaz_zcta_national.zip
//
// The file inside the zip is tab-separated with columns:
//
//	GEOID, ALAND, AWATER, ALAND_SQMI, AWATER_SQMI, INTPTLAT, INTPTLONG
//
// Scheduling: registered as a monthly periodic job in appserver/server.go.

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
)

const (
	// zctaURL points to the latest Census ZCTA5 gazetteer zip.
	// The file inside the archive is tab-separated; GEOID is the ZIP code,
	// INTPTLAT/INTPTLONG are the internal point lat/lon.
	zctaURL = "https://www2.census.gov/geo/docs/maps-data/data/gazetteer/2025_Gazetteer/2025_Gaz_zcta_national.zip"

	// zctaBatchSize is the number of rows to upsert per transaction.
	zctaBatchSize = 500
)

// ──────────────────────────────────────────────────────────────────────────────
// ZCTARefreshArgs
// ──────────────────────────────────────────────────────────────────────────────

// ZCTARefreshArgs holds the (empty) arguments for the ZCTA refresh job.
type ZCTARefreshArgs struct{}

// Kind returns the unique River job kind identifier.
func (ZCTARefreshArgs) Kind() string { return "zcta_refresh" }

// ──────────────────────────────────────────────────────────────────────────────
// ZCTARefreshWorker
// ──────────────────────────────────────────────────────────────────────────────

// ZCTARefreshWorker is the River worker that downloads and upserts ZCTA centroids.
type ZCTARefreshWorker struct {
	river.WorkerDefaults[ZCTARefreshArgs]
	Pool *pgxpool.Pool

	// OverrideURL is used by tests to inject a mock server URL.
	OverrideURL string
}

// zctaRow represents one parsed row from the ZCTA5 gazetteer file.
type zctaRow struct {
	ZIP string
	Lat float64
	Lon float64
}

// Work downloads the ZCTA file and upserts all centroids into zip_centroids.
func (w *ZCTARefreshWorker) Work(ctx context.Context, job *river.Job[ZCTARefreshArgs]) error {
	log := slog.With(slog.String("job_kind", job.Args.Kind()), slog.Int64("job_id", job.ID))
	log.Info("zcta_refresh: starting download")

	rows, err := w.downloadAndParse(ctx, log)
	if err != nil {
		return fmt.Errorf("zcta_refresh: download/parse: %w", err)
	}
	log.Info("zcta_refresh: parsed rows", slog.Int("count", len(rows)))

	upserted, err := w.upsertBatched(ctx, rows, log)
	if err != nil {
		return fmt.Errorf("zcta_refresh: upsert: %w", err)
	}

	log.Info("zcta_refresh: complete", slog.Int("upserted", upserted))
	return nil
}

// downloadAndParse fetches the ZCTA zip archive and returns parsed rows.
// The Census file is now distributed as a zip containing a tab-separated text file.
func (w *ZCTARefreshWorker) downloadAndParse(ctx context.Context, log *slog.Logger) ([]zctaRow, error) {
	targetURL := zctaURL
	if w.OverrideURL != "" {
		targetURL = w.OverrideURL
	}

	client := &http.Client{Timeout: 5 * time.Minute}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", "RadioLedger/1.0 (https://radioledger.com)")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", targetURL, err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			log.Warn("zcta_refresh: close response body", slog.Any("error", closeErr))
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s returned %s", targetURL, resp.Status)
	}

	// Read the entire response into memory so we can use zip.NewReader (needs io.ReaderAt).
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}

	// Detect zip vs plain text by magic bytes (PK\x03\x04).
	var scanner *bufio.Scanner
	if len(body) >= 4 && body[0] == 'P' && body[1] == 'K' {
		zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
		if err != nil {
			return nil, fmt.Errorf("open zip: %w", err)
		}
		// Find the first .txt file in the archive.
		var txtFile *zip.File
		for _, f := range zr.File {
			if strings.HasSuffix(strings.ToLower(f.Name), ".txt") {
				txtFile = f
				break
			}
		}
		if txtFile == nil {
			return nil, fmt.Errorf("no .txt file found in zip archive")
		}
		rc, err := txtFile.Open()
		if err != nil {
			return nil, fmt.Errorf("open zip entry %s: %w", txtFile.Name, err)
		}
		defer func() {
			if closeErr := rc.Close(); closeErr != nil {
				log.Warn("zcta_refresh: close zip entry", slog.Any("error", closeErr))
			}
		}()
		scanner = bufio.NewScanner(rc)
	} else {
		scanner = bufio.NewScanner(bytes.NewReader(body))
	}

	var rows []zctaRow

	// The Census gazetteer file has a header line; skip it.
	headerSkipped := false
	for scanner.Scan() {
		line := scanner.Text()
		if !headerSkipped {
			headerSkipped = true
			continue
		}

		row, ok := parseZCTALine(line)
		if !ok {
			log.Debug("zcta_refresh: skipping malformed line", slog.String("line", line))
			continue
		}
		rows = append(rows, row)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan response body: %w", err)
	}
	return rows, nil
}

// parseZCTALine parses a single line from the ZCTA5 gazetteer.
// Supports both tab-separated (2023 and earlier) and pipe-separated (2024+) formats.
//
// 2023 layout (tab-separated, 7 columns):
//
//	0: GEOID  1: ALAND  2: AWATER  3: ALAND_SQMI  4: AWATER_SQMI  5: INTPTLAT  6: INTPTLONG
//
// 2024+ layout (pipe-separated, 8 columns):
//
//	0: GEOID  1: GEOIDFQ  2: ALAND  3: AWATER  4: ALAND_SQMI  5: AWATER_SQMI  6: INTPTLAT  7: INTPTLONG
func parseZCTALine(line string) (zctaRow, bool) {
	// Auto-detect delimiter: pipe-separated (2024+) or tab-separated (2023)
	sep := "\t"
	if strings.Contains(line, "|") {
		sep = "|"
	}
	fields := strings.Split(line, sep)

	// Determine lat/lon column indices based on field count
	var latIdx, lonIdx int
	switch {
	case len(fields) >= 8: // 2024+ format with GEOIDFQ
		latIdx, lonIdx = 6, 7
	case len(fields) >= 7: // 2023 format
		latIdx, lonIdx = 5, 6
	default:
		return zctaRow{}, false
	}

	zip := strings.TrimSpace(fields[0])
	if len(zip) < 3 { // sanity check
		return zctaRow{}, false
	}

	lat, err := strconv.ParseFloat(strings.TrimSpace(fields[latIdx]), 64)
	if err != nil {
		return zctaRow{}, false
	}
	lon, err := strconv.ParseFloat(strings.TrimSpace(fields[lonIdx]), 64)
	if err != nil {
		return zctaRow{}, false
	}

	return zctaRow{ZIP: zip, Lat: lat, Lon: lon}, true
}

// upsertBatched inserts/updates zip_centroids in batches of zctaBatchSize.
// Uses SET LOCAL ROLE radioledger_worker to bypass RLS (inside tx).
func (w *ZCTARefreshWorker) upsertBatched(ctx context.Context, rows []zctaRow, log *slog.Logger) (int, error) {
	total := 0

	for i := 0; i < len(rows); i += zctaBatchSize {
		end := i + zctaBatchSize
		if end > len(rows) {
			end = len(rows)
		}
		batch := rows[i:end]

		conn, err := w.Pool.Acquire(ctx)
		if err != nil {
			return total, fmt.Errorf("acquire conn: %w", err)
		}

		tx, err := conn.Begin(ctx)
		if err != nil {
			conn.Release()
			return total, fmt.Errorf("begin tx: %w", err)
		}

		if _, err := tx.Exec(ctx, "SET LOCAL ROLE radioledger_worker"); err != nil {
			_ = tx.Rollback(ctx)
			conn.Release()
			return total, fmt.Errorf("set worker role: %w", err)
		}

		for _, row := range batch {
			_, err := tx.Exec(ctx, `
				INSERT INTO zip_centroids (zip_code, latitude, longitude, updated_at)
				VALUES ($1, $2, $3, now())
				ON CONFLICT (zip_code) DO UPDATE
				SET latitude   = EXCLUDED.latitude,
				    longitude  = EXCLUDED.longitude,
				    updated_at = EXCLUDED.updated_at
			`, row.ZIP, row.Lat, row.Lon)
			if err != nil {
				_ = tx.Rollback(ctx)
				conn.Release()
				return total, fmt.Errorf("upsert zip %s: %w", row.ZIP, err)
			}
		}

		if err := tx.Commit(ctx); err != nil {
			conn.Release()
			return total, fmt.Errorf("commit batch: %w", err)
		}
		conn.Release()

		total += len(batch)
		if total%5000 == 0 {
			log.Info("zcta_refresh: upsert progress", slog.Int("upserted", total), slog.Int("total", len(rows)))
		}
	}

	return total, nil
}
