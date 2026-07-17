package callsign

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
)

const (
	// SotaCSVURL is the canonical SOTA summits list CSV endpoint.
	// The URL redirects to storage.sota.org.uk; the HTTP client follows the redirect.
	SotaCSVURL = "https://www.sotadata.org.uk/summitslist.csv"

	sotaUpsertBatchSize = 2000
)

// ─────────────────────────────────────────────────────────────────────────────
// Job Args
// ─────────────────────────────────────────────────────────────────────────────

// SotaSyncArgs is the River job payload for a full SOTA summits CSV sync.
type SotaSyncArgs struct{}

func (SotaSyncArgs) Kind() string { return "sota_summit_sync" }

// ─────────────────────────────────────────────────────────────────────────────
// SotaSyncWorker
// ─────────────────────────────────────────────────────────────────────────────

// SotaSyncWorker downloads the SOTA summitslist.csv, parses it, and upserts
// every summit into the sota_summits table.  Summits no longer in the feed
// (or with a past valid_to date) are marked inactive.  Sync progress is tracked
// in callsign_sync_runs with source="sota_summits".
//
// Schedule: weekly (e.g. every Sunday at 4–6am UTC).
type SotaSyncWorker struct {
	river.WorkerDefaults[SotaSyncArgs]
	Pool *pgxpool.Pool
}

// Timeout overrides River's default 60s job timeout.  The SOTA CSV is ~10 MB
// and contains ~130k summits; the full run typically takes 3–5 minutes.
func (w *SotaSyncWorker) Timeout(*river.Job[SotaSyncArgs]) time.Duration {
	return 30 * time.Minute
}

// Work executes the SOTA summits sync.
func (w *SotaSyncWorker) Work(ctx context.Context, job *river.Job[SotaSyncArgs]) error {
	log := slog.With(
		slog.String("job_kind", job.Args.Kind()),
		slog.Int64("job_id", job.ID),
	)
	log.Info("sota_summit_sync: started")

	runID, err := startSyncRun(ctx, w.Pool, "sota_summits", "full")
	if err != nil {
		return fmt.Errorf("sota_summit_sync: start run: %w", err)
	}

	summits, parseErr := parseSotaCSV(ctx, SotaCSVURL)
	if parseErr != nil {
		_ = failSyncRun(ctx, w.Pool, runID, parseErr.Error())
		return fmt.Errorf("sota_summit_sync: parse: %w", parseErr)
	}

	added, updated, upsertErr := upsertSotaSummits(ctx, w.Pool, summits)
	if upsertErr != nil {
		_ = failSyncRun(ctx, w.Pool, runID, upsertErr.Error())
		return fmt.Errorf("sota_summit_sync: upsert: %w", upsertErr)
	}

	removed, deactivateErr := deactivateMissingSummits(ctx, w.Pool, summits)
	if deactivateErr != nil {
		// Non-fatal: log but don't fail the run.
		log.Error("sota_summit_sync: deactivate missing", slog.String("error", deactivateErr.Error()))
	}

	if err := completeSyncRun(ctx, w.Pool, runID, len(summits), added, updated, removed); err != nil {
		log.Error("sota_summit_sync: complete run", slog.String("error", err.Error()))
	}

	log.Info("sota_summit_sync: complete",
		slog.Int("processed", len(summits)),
		slog.Int("added", added),
		slog.Int("updated", updated),
		slog.Int("deactivated", removed),
	)
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// CSV parser
// ─────────────────────────────────────────────────────────────────────────────

// sotaSummit holds a single row from the SOTA summits CSV.
type sotaSummit struct {
	Ref         string  // summit_ref (e.g. "W7A/SN-001")
	Name        string  // summit name
	Association string  // association name (e.g. "W7A - Arizona")
	Region      string  // region name (e.g. "Sierra Nevada")
	ElevationM  *int    // altitude in metres
	Points      *int16  // activation points
	BonusPoints int16   // winter bonus points
	Latitude    *float64
	Longitude   *float64
	ValidFrom   pgtype.Date
	ValidTo     pgtype.Date
	Active      bool // derived from valid_to (31/12/2099 = still active)
}

// sotaDateLayout is the date format used in the SOTA CSV ("DD/MM/YYYY").
const sotaDateLayout = "02/01/2006"

// sotaRetiredYear is the year used by SOTA to represent "still active" summits.
// Summits with valid_to year < 2099 are considered retired.
const sotaRetiredYear = 2099

// parseSotaCSV downloads and parses the SOTA summitslist.csv.
//
// The CSV has a metadata line on row 1 ("SOTA Summits List (Date=...)"), then
// the column header on row 2, followed by data rows.
//
// Columns: SummitCode, AssociationName, RegionName, SummitName, AltM, AltFt,
//
//	GridRef1, GridRef2, Longitude, Latitude, Points, BonusPoints,
//	ValidFrom, ValidTo, ActivationCount, ActivationDate, ActivationCall
func parseSotaCSV(ctx context.Context, url string) ([]sotaSummit, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", "RadioLedger/1.0 (summit-sync)")

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
	r.FieldsPerRecord = -1 // tolerate variable field counts
	r.LazyQuotes = true    // SOTA CSV may have unquoted fields with commas in names

	// Row 1: metadata line like "SOTA Summits List (Date=12/03/2026)".
	// Row 2: column headers.
	// Read up to 5 rows searching for the header row.
	var header []string
	var lineNum int
	for {
		row, err := r.Read()
		if err == io.EOF {
			return nil, fmt.Errorf("CSV is empty or has no header row")
		}
		if err != nil {
			return nil, fmt.Errorf("read header area: %w", err)
		}
		lineNum++
		// Detect the header row by looking for "SummitCode" in the first column.
		if len(row) > 0 && strings.EqualFold(strings.TrimSpace(row[0]), "summitcode") {
			header = row
			break
		}
		if lineNum > 5 {
			return nil, fmt.Errorf("could not locate SOTA CSV header (gave up after %d lines)", lineNum)
		}
	}

	colIdx := csvColumnIndex(header)
	refCol := colIdx["summitcode"]
	assocCol := colIdx["associationname"]
	regionCol := colIdx["regionname"]
	nameCol := colIdx["summitname"]
	altMCol := colIdx["altm"]
	lonCol := colIdx["longitude"]
	latCol := colIdx["latitude"]
	pointsCol := colIdx["points"]
	bonusCol := colIdx["bonuspoints"]
	validFromCol := colIdx["validfrom"]
	validToCol := colIdx["validto"]

	if refCol < 0 || nameCol < 0 {
		return nil, fmt.Errorf("unexpected SOTA CSV headers: %v", header)
	}

	var summits []sotaSummit
	for {
		row, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			lineNum++
			// Skip malformed rows but keep going.
			slog.Warn("sota_summit_sync: skipping malformed row",
				slog.Int("line", lineNum),
				slog.String("error", err.Error()),
			)
			continue
		}
		lineNum++

		ref := strings.TrimSpace(safeCol(row, refCol))
		name := strings.TrimSpace(safeCol(row, nameCol))
		if ref == "" || name == "" {
			continue
		}

		s := sotaSummit{
			Ref:         ref,
			Name:        name,
			Association: strings.TrimSpace(safeCol(row, assocCol)),
			Region:      strings.TrimSpace(safeCol(row, regionCol)),
			Active:      true,
		}

		if altMCol >= 0 {
			if v, err := strconv.Atoi(strings.TrimSpace(safeCol(row, altMCol))); err == nil {
				s.ElevationM = &v
			}
		}
		if latCol >= 0 {
			if v, err := strconv.ParseFloat(strings.TrimSpace(safeCol(row, latCol)), 64); err == nil {
				s.Latitude = &v
			}
		}
		if lonCol >= 0 {
			if v, err := strconv.ParseFloat(strings.TrimSpace(safeCol(row, lonCol)), 64); err == nil {
				s.Longitude = &v
			}
		}
		if pointsCol >= 0 {
			if v, err := strconv.ParseInt(strings.TrimSpace(safeCol(row, pointsCol)), 10, 16); err == nil {
				p := int16(v)
				s.Points = &p
			}
		}
		if bonusCol >= 0 {
			if v, err := strconv.ParseInt(strings.TrimSpace(safeCol(row, bonusCol)), 10, 16); err == nil {
				s.BonusPoints = int16(v)
			}
		}
		if validFromCol >= 0 {
			s.ValidFrom = parseSotaDate(strings.TrimSpace(safeCol(row, validFromCol)))
		}
		if validToCol >= 0 {
			raw := strings.TrimSpace(safeCol(row, validToCol))
			s.ValidTo = parseSotaDate(raw)
			// Mark retired summits: valid_to year < sotaRetiredYear.
			if s.ValidTo.Valid && s.ValidTo.Time.Year() < sotaRetiredYear {
				s.Active = false
			}
		}

		summits = append(summits, s)
	}

	return summits, nil
}

// parseSotaDate parses a SOTA-format date string ("DD/MM/YYYY") into a pgtype.Date.
// Returns an invalid (zero) pgtype.Date on parse failure.
func parseSotaDate(s string) pgtype.Date {
	if s == "" {
		return pgtype.Date{}
	}
	t, err := time.Parse(sotaDateLayout, s)
	if err != nil {
		return pgtype.Date{}
	}
	return pgtype.Date{Time: t, Valid: true}
}

// ─────────────────────────────────────────────────────────────────────────────
// Upsert
// ─────────────────────────────────────────────────────────────────────────────

// upsertSotaSummits inserts/updates sota_summits in batches.
func upsertSotaSummits(ctx context.Context, pool *pgxpool.Pool, summits []sotaSummit) (added, updated int, err error) {
	if len(summits) == 0 {
		return 0, 0, nil
	}

	for i := 0; i < len(summits); i += sotaUpsertBatchSize {
		end := i + sotaUpsertBatchSize
		if end > len(summits) {
			end = len(summits)
		}
		a, u, bErr := upsertSotaBatch(ctx, pool, summits[i:end])
		if bErr != nil {
			return added, updated, fmt.Errorf("batch [%d:%d]: %w", i, end, bErr)
		}
		added += a
		updated += u
	}
	return added, updated, nil
}

// upsertSotaBatch upserts one chunk of sota_summits rows.
func upsertSotaBatch(ctx context.Context, pool *pgxpool.Pool, batch []sotaSummit) (added, updated int, err error) {
	refs := make([]string, len(batch))
	names := make([]string, len(batch))
	assocs := make([]string, len(batch))
	regions := make([]string, len(batch))
	elevs := make([]*int, len(batch))
	points := make([]*int16, len(batch))
	bonuses := make([]int16, len(batch))
	lats := make([]*float64, len(batch))
	lons := make([]*float64, len(batch))
	validFroms := make([]pgtype.Date, len(batch))
	validTos := make([]pgtype.Date, len(batch))
	actives := make([]bool, len(batch))

	for i, s := range batch {
		refs[i] = s.Ref
		names[i] = s.Name
		assocs[i] = s.Association
		regions[i] = s.Region
		elevs[i] = s.ElevationM
		points[i] = s.Points
		bonuses[i] = s.BonusPoints
		lats[i] = s.Latitude
		lons[i] = s.Longitude
		validFroms[i] = s.ValidFrom
		validTos[i] = s.ValidTo
		actives[i] = s.Active
	}

	tag, err := pool.Exec(ctx, `
		INSERT INTO sota_summits (
			summit_ref, name, association, region,
			elevation_m, points, bonus_points,
			latitude, longitude, location,
			valid_from, valid_to,
			active
		)
		SELECT
			t.ref,
			t.name,
			t.association,
			t.region,
			t.elev,
			t.pts,
			t.bonus,
			t.lat,
			t.lon,
			CASE WHEN t.lat IS NOT NULL AND t.lon IS NOT NULL
			     THEN ST_SetSRID(ST_MakePoint(t.lon, t.lat), 4326)
			     ELSE NULL
			END,
			t.valid_from,
			t.valid_to,
			t.active
		FROM unnest(
			$1::text[], $2::text[], $3::text[], $4::text[],
			$5::int[], $6::smallint[], $7::smallint[],
			$8::float8[], $9::float8[],
			$10::date[], $11::date[],
			$12::bool[]
		) AS t(ref, name, association, region,
		       elev, pts, bonus,
		       lat, lon,
		       valid_from, valid_to,
		       active)
		ON CONFLICT (summit_ref) DO UPDATE SET
			name         = EXCLUDED.name,
			association  = EXCLUDED.association,
			region       = EXCLUDED.region,
			elevation_m  = EXCLUDED.elevation_m,
			points       = EXCLUDED.points,
			bonus_points = EXCLUDED.bonus_points,
			latitude     = COALESCE(EXCLUDED.latitude, sota_summits.latitude),
			longitude    = COALESCE(EXCLUDED.longitude, sota_summits.longitude),
			location     = COALESCE(EXCLUDED.location, sota_summits.location),
			valid_from   = EXCLUDED.valid_from,
			valid_to     = EXCLUDED.valid_to,
			active       = EXCLUDED.active
	`,
		refs, names, assocs, regions,
		elevs, points, bonuses,
		lats, lons,
		validFroms, validTos,
		actives,
	)
	if err != nil {
		return 0, 0, err
	}
	return 0, int(tag.RowsAffected()), nil
}

// deactivateMissingSummits marks any summit not present in the latest CSV as inactive.
// Returns the number of rows deactivated.
func deactivateMissingSummits(ctx context.Context, pool *pgxpool.Pool, summits []sotaSummit) (int, error) {
	if len(summits) == 0 {
		return 0, nil
	}

	refs := make([]string, len(summits))
	for i, s := range summits {
		refs[i] = s.Ref
	}

	tag, err := pool.Exec(ctx, `
		UPDATE sota_summits
		SET active = FALSE
		WHERE summit_ref != ALL($1::text[])
		  AND active = TRUE
	`, refs)
	if err != nil {
		return 0, fmt.Errorf("deactivate missing summits: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Compile-time interface check
// ─────────────────────────────────────────────────────────────────────────────

var _ river.Worker[SotaSyncArgs] = (*SotaSyncWorker)(nil)
