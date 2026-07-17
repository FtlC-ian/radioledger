package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/FtlC-ian/radioledger/api/internal/services/awards"
)

func setupAwardRefreshIntegration(t *testing.T) *pgxpool.Pool {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	dbURL := os.Getenv("RADIOLEDGER_TEST_DATABASE_URL")
	if strings.TrimSpace(dbURL) == "" {
		t.Skip("RADIOLEDGER_TEST_DATABASE_URL is required for award refresh integration tests")
	}

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Skipf("skipping integration test: cannot create pool: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("skipping integration test: cannot connect to db: %v", err)
	}

	var hasAwardProgressConstraint bool
	if err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM pg_constraint
			WHERE conname = 'uq_award_progress_nulls_not_distinct'
		)
	`).Scan(&hasAwardProgressConstraint); err != nil {
		pool.Close()
		t.Skipf("skipping integration test: cannot inspect schema: %v", err)
	}
	if !hasAwardProgressConstraint {
		pool.Close()
		t.Skip("skipping integration test: test database is missing uq_award_progress_nulls_not_distinct")
	}

	t.Cleanup(pool.Close)
	return pool
}

func TestAwardRefreshWorker_POTAActivatorUsesMyPOTARefs(t *testing.T) {
	pool := setupAwardRefreshIntegration(t)
	ctx := context.Background()

	label := time.Now().UnixNano()
	var userID int64
	var logbookID int64
	if err := pool.QueryRow(ctx, `
		INSERT INTO users (email, timezone, display_name)
		VALUES ($1, 'UTC', 'POTA Activator Test')
		RETURNING id
	`, fmt.Sprintf("pota-activator-%d@example.test", label)).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO logbooks (user_id, name, callsign, is_default)
		VALUES ($1, 'POTA Test Log', 'W1POTA', true)
		RETURNING id
	`, userID).Scan(&logbookID); err != nil {
		t.Fatalf("create logbook: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM award_progress WHERE user_id = $1`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM qsos WHERE logbook_id = $1`, logbookID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM logbooks WHERE id = $1`, logbookID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, userID)
	})

	if _, err := pool.Exec(ctx, `
		INSERT INTO qsos (logbook_id, callsign, datetime_on, band, mode, my_pota_refs)
		VALUES
			($1, 'K1AAA', '2026-06-02T20:00:00Z', '20m', 'SSB', ARRAY['us-1234', 'US-1234', 'bad-ref']),
			($1, 'K1BBB', '2026-06-02T20:10:00Z', '20m', 'SSB', ARRAY['US-5678'])
	`, logbookID); err != nil {
		t.Fatalf("seed qsos: %v", err)
	}

	worker := &AwardRefreshWorker{Pool: pool}
	if err := worker.RefreshUser(ctx, userID, string(awards.AwardPOTAActivator), slog.Default()); err != nil {
		t.Fatalf("refresh pota activator: %v", err)
	}

	rows, err := pool.Query(ctx, `
		SELECT entity_key, qso_count, worked, confirmed
		FROM award_progress
		WHERE user_id = $1 AND award_type = 'pota_activator'
		ORDER BY entity_key
	`, userID)
	if err != nil {
		t.Fatalf("query award progress: %v", err)
	}
	defer rows.Close()

	type progressRow struct {
		key       string
		qsoCount  int64
		worked    bool
		confirmed bool
	}
	var got []progressRow
	for rows.Next() {
		var row progressRow
		if err := rows.Scan(&row.key, &row.qsoCount, &row.worked, &row.confirmed); err != nil {
			t.Fatalf("scan award progress: %v", err)
		}
		got = append(got, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate award progress: %v", err)
	}

	want := []progressRow{
		{key: "US-1234", qsoCount: 2, worked: true, confirmed: false},
		{key: "US-5678", qsoCount: 1, worked: true, confirmed: false},
	}
	if fmt.Sprintf("%#v", got) != fmt.Sprintf("%#v", want) {
		t.Fatalf("award progress mismatch\ngot:  %#v\nwant: %#v", got, want)
	}
}
