package callsign

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
)

const (
	upsertBatchSize = 5000
)

// ─────────────────────────────────────────────────────────────────────────────
// Job Args
// ─────────────────────────────────────────────────────────────────────────────

// FCCWeeklySyncArgs is the River job payload for a full FCC ULS dump sync.
// Runs every Sunday at 2am UTC.
type FCCWeeklySyncArgs struct{}

func (FCCWeeklySyncArgs) Kind() string { return "fcc_weekly_sync" }

// FCCDailySyncArgs is the River job payload for an FCC ULS daily diff sync.
// Runs Mon–Sat at 3am UTC. Day is e.g. "mon", "tue", …, "sat".
type FCCDailySyncArgs struct {
	Day string `json:"day,omitempty"` // overrides auto-detect when set (e.g. "mon")
}

func (FCCDailySyncArgs) Kind() string { return "fcc_daily_sync" }

// ─────────────────────────────────────────────────────────────────────────────
// FCCWeeklySyncWorker
// ─────────────────────────────────────────────────────────────────────────────

// FCCWeeklySyncWorker downloads the full FCC ULS amateur dump, parses it, and
// batch-UPSERTs all records into callsign_records. Schedule: Sunday 2am UTC.
type FCCWeeklySyncWorker struct {
	river.WorkerDefaults[FCCWeeklySyncArgs]
	Pool *pgxpool.Pool
}

// Timeout overrides River's default 60s job timeout.
// The full FCC dump is ~60MB and can take several minutes to download + parse + upsert.
func (w *FCCWeeklySyncWorker) Timeout(*river.Job[FCCWeeklySyncArgs]) time.Duration {
	return 30 * time.Minute
}

// Work executes the full FCC dump import.
func (w *FCCWeeklySyncWorker) Work(ctx context.Context, job *river.Job[FCCWeeklySyncArgs]) error {
	log := slog.With(
		slog.String("job_kind", job.Args.Kind()),
		slog.Int64("job_id", job.ID),
	)
	log.Info("fcc_weekly_sync: started")

	runID, err := startSyncRun(ctx, w.Pool, "fcc", "full")
	if err != nil {
		return fmt.Errorf("fcc_weekly_sync: start run: %w", err)
	}

	result, parseErr := ParseFCCZip(ctx, FCCFullDumpURL)
	if parseErr != nil {
		_ = failSyncRun(ctx, w.Pool, runID, parseErr.Error())
		return fmt.Errorf("fcc_weekly_sync: parse: %w", parseErr)
	}

	added, updated, upsertErr := batchUpsertRecords(ctx, w.Pool, result.Records)
	if upsertErr != nil {
		_ = failSyncRun(ctx, w.Pool, runID, upsertErr.Error())
		return fmt.Errorf("fcc_weekly_sync: upsert: %w", upsertErr)
	}

	if err := completeSyncRun(ctx, w.Pool, runID, result.Processed, added, updated, 0); err != nil {
		log.Error("fcc_weekly_sync: complete run", slog.String("error", err.Error()))
	}

	log.Info("fcc_weekly_sync: complete",
		slog.Int("processed", result.Processed),
		slog.Int("added", added),
		slog.Int("updated", updated),
	)
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// FCCDailySyncWorker
// ─────────────────────────────────────────────────────────────────────────────

// FCCDailySyncWorker downloads the daily FCC ULS diff and applies changes.
// Schedule: Mon–Sat at 3am UTC.
type FCCDailySyncWorker struct {
	river.WorkerDefaults[FCCDailySyncArgs]
	Pool *pgxpool.Pool
}

// Timeout overrides River's default 60s timeout for the daily diff import.
func (w *FCCDailySyncWorker) Timeout(*river.Job[FCCDailySyncArgs]) time.Duration {
	return 10 * time.Minute
}

// Work executes the daily FCC diff import.
func (w *FCCDailySyncWorker) Work(ctx context.Context, job *river.Job[FCCDailySyncArgs]) error {
	log := slog.With(
		slog.String("job_kind", job.Args.Kind()),
		slog.Int64("job_id", job.ID),
	)

	day := job.Args.Day
	if day == "" {
		// Auto-detect: "Mon" → "mon", etc.
		day = strings.ToLower(time.Now().UTC().Format("Mon"))
	}
	log = log.With(slog.String("day", day))
	log.Info("fcc_daily_sync: started")

	url := fmt.Sprintf(FCCDailyDumpURLFmt, day)

	runID, err := startSyncRun(ctx, w.Pool, "fcc", "daily")
	if err != nil {
		return fmt.Errorf("fcc_daily_sync: start run: %w", err)
	}

	result, parseErr := ParseFCCZip(ctx, url)
	if parseErr != nil {
		_ = failSyncRun(ctx, w.Pool, runID, parseErr.Error())
		return fmt.Errorf("fcc_daily_sync: parse %s: %w", url, parseErr)
	}

	added, updated, upsertErr := batchUpsertRecords(ctx, w.Pool, result.Records)
	if upsertErr != nil {
		_ = failSyncRun(ctx, w.Pool, runID, upsertErr.Error())
		return fmt.Errorf("fcc_daily_sync: upsert: %w", upsertErr)
	}

	if err := completeSyncRun(ctx, w.Pool, runID, result.Processed, added, updated, 0); err != nil {
		log.Error("fcc_daily_sync: complete run", slog.String("error", err.Error()))
	}

	log.Info("fcc_daily_sync: complete",
		slog.Int("processed", result.Processed),
		slog.Int("added", added),
		slog.Int("updated", updated),
	)
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Batch upsert
// ─────────────────────────────────────────────────────────────────────────────

// batchUpsertRecords inserts/updates callsign_records in chunks of upsertBatchSize.
// Returns (added, updated, error). Uses unnest for high-throughput bulk upserts.
func batchUpsertRecords(ctx context.Context, pool *pgxpool.Pool, records []NormalizedRecord) (added, updated int, err error) {
	if len(records) == 0 {
		return 0, 0, nil
	}

	conn, err := pool.Acquire(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("acquire conn: %w", err)
	}
	defer conn.Release()
	// Reset role before returning connection to pool so River and other
	// pool users don't inherit the restricted worker role.
	defer func() { _, _ = conn.Exec(ctx, "RESET ROLE") }()

	// Run as worker role (no RLS on callsign_records).
	if _, err := conn.Exec(ctx, "SET ROLE radioledger_worker"); err != nil {
		return 0, 0, fmt.Errorf("set role: %w", err)
	}

	// Deduplicate by (source, callsign), keeping the last occurrence.
	// FCC dumps may contain multiple records per callsign (e.g. expired + active).
	seen := make(map[string]int, len(records))
	deduped := make([]NormalizedRecord, 0, len(records))
	for _, r := range records {
		key := r.Source + "\x00" + r.Callsign
		if idx, ok := seen[key]; ok {
			deduped[idx] = r // overwrite with later entry
		} else {
			seen[key] = len(deduped)
			deduped = append(deduped, r)
		}
	}

	for i := 0; i < len(deduped); i += upsertBatchSize {
		end := i + upsertBatchSize
		if end > len(deduped) {
			end = len(deduped)
		}
		batch := deduped[i:end]

		bAdded, bUpdated, bErr := upsertBatch(ctx, conn.Conn(), batch)
		if bErr != nil {
			return added, updated, fmt.Errorf("batch [%d:%d]: %w", i, end, bErr)
		}
		added += bAdded
		updated += bUpdated
	}

	return added, updated, nil
}

// upsertBatch upserts one chunk using pgx unnest for bulk throughput.
// ON CONFLICT (callsign, source) DO UPDATE — safe for both full and daily diff.
func upsertBatch(ctx context.Context, conn *pgx.Conn, batch []NormalizedRecord) (added, updated int, err error) {
	// Build parallel arrays for unnest.
	callsigns := make([]string, len(batch))
	sources := make([]string, len(batch))
	sourceIDs := make([]*string, len(batch))
	firstNames := make([]*string, len(batch))
	lastNames := make([]*string, len(batch))
	fullNames := make([]*string, len(batch))
	addr1s := make([]*string, len(batch))
	cities := make([]*string, len(batch))
	states := make([]*string, len(batch))
	postalCodes := make([]*string, len(batch))
	countries := make([]string, len(batch))
	licenseClasses := make([]*string, len(batch))
	grantDates := make([]pgtype.Date, len(batch))
	expiryDates := make([]pgtype.Date, len(batch))
	statuses := make([]string, len(batch))
	latitudes := make([]*float64, len(batch))
	longitudes := make([]*float64, len(batch))

	for i, r := range batch {
		callsigns[i] = r.Callsign
		sources[i] = r.Source
		sourceIDs[i] = nullStr(r.SourceID)
		firstNames[i] = nullStr(r.FirstName)
		lastNames[i] = nullStr(r.LastName)
		fullNames[i] = nullStr(r.FullName)
		addr1s[i] = nullStr(r.AddressLine1)
		cities[i] = nullStr(r.City)
		states[i] = nullStr(r.StateProvince)
		postalCodes[i] = nullStr(r.PostalCode)
		countries[i] = r.Country
		licenseClasses[i] = nullStr(r.LicenseClass)
		grantDates[i] = toDate(r.GrantDate)
		expiryDates[i] = toDate(r.ExpiryDate)
		statuses[i] = r.Status
		latitudes[i] = r.Latitude
		longitudes[i] = r.Longitude
	}

	tag, err := conn.Exec(ctx, `
		INSERT INTO callsign_records (
			callsign, source, source_id,
			first_name, last_name, full_name,
			address_line1, city, state_province, postal_code, country,
			license_class, grant_date, expiry_date, status,
			latitude, longitude,
			fetched_at, updated_at
		)
		SELECT
			unnest($1::text[]),
			unnest($2::text[]),
			unnest($3::text[]),
			unnest($4::text[]),
			unnest($5::text[]),
			unnest($6::text[]),
			unnest($7::text[]),
			unnest($8::text[]),
			unnest($9::text[]),
			unnest($10::text[]),
			unnest($11::text[]),
			unnest($12::text[]),
			unnest($13::date[]),
			unnest($14::date[]),
			unnest($15::text[]),
			unnest($16::float8[]),
			unnest($17::float8[]),
			now(), now()
		ON CONFLICT (callsign, source) DO UPDATE SET
			source_id      = EXCLUDED.source_id,
			first_name     = EXCLUDED.first_name,
			last_name      = EXCLUDED.last_name,
			full_name      = EXCLUDED.full_name,
			address_line1  = EXCLUDED.address_line1,
			city           = EXCLUDED.city,
			state_province = EXCLUDED.state_province,
			postal_code    = EXCLUDED.postal_code,
			license_class  = EXCLUDED.license_class,
			grant_date     = EXCLUDED.grant_date,
			expiry_date    = EXCLUDED.expiry_date,
			status         = EXCLUDED.status,
			latitude       = EXCLUDED.latitude,
			longitude      = EXCLUDED.longitude,
			updated_at     = now()
	`,
		callsigns, sources, sourceIDs,
		firstNames, lastNames, fullNames,
		addr1s, cities, states, postalCodes, countries,
		licenseClasses, grantDates, expiryDates, statuses,
		latitudes, longitudes,
	)
	if err != nil {
		return 0, 0, err
	}

	// PostgreSQL counts all affected rows (inserts + updates) in RowsAffected.
	// We report them all as "updated" since we can't easily distinguish.
	return 0, int(tag.RowsAffected()), nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Sync run tracking
// ─────────────────────────────────────────────────────────────────────────────

func startSyncRun(ctx context.Context, pool *pgxpool.Pool, source, runType string) (int64, error) {
	var id int64
	err := pool.QueryRow(ctx, `
		INSERT INTO callsign_sync_runs (source, run_type, status, started_at)
		VALUES ($1, $2, 'running', now())
		RETURNING id
	`, source, runType).Scan(&id)
	return id, err
}

func failSyncRun(ctx context.Context, pool *pgxpool.Pool, id int64, errMsg string) error {
	_, err := pool.Exec(ctx, `
		UPDATE callsign_sync_runs
		SET status = 'failed', error = $2, completed_at = now()
		WHERE id = $1
	`, id, errMsg)
	return err
}

func completeSyncRun(ctx context.Context, pool *pgxpool.Pool, id int64, processed, added, updated, removed int) error {
	_, err := pool.Exec(ctx, `
		UPDATE callsign_sync_runs
		SET status           = 'completed',
		    completed_at     = now(),
		    records_processed = $2,
		    records_added     = $3,
		    records_updated   = $4,
		    records_removed   = $5
		WHERE id = $1
	`, id, processed, added, updated, removed)
	return err
}

// ─────────────────────────────────────────────────────────────────────────────
// Type utilities
// ─────────────────────────────────────────────────────────────────────────────

func nullStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func toDate(t *time.Time) pgtype.Date {
	if t == nil {
		return pgtype.Date{}
	}
	return pgtype.Date{Time: *t, Valid: true}
}
