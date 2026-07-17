package handler_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func ensureSyncConflictsTable(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		CREATE TABLE IF NOT EXISTS sync_conflicts (
			id BIGSERIAL PRIMARY KEY,
			qso_id BIGINT NOT NULL REFERENCES qsos(id) ON DELETE CASCADE,
			service_a TEXT NOT NULL,
			service_b TEXT NOT NULL,
			field_conflicts JSONB NOT NULL DEFAULT '{}'::jsonb,
			status TEXT NOT NULL DEFAULT 'open',
			resolution JSONB,
			resolved_by_service TEXT,
			resolved_at TIMESTAMPTZ,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`)
	if err != nil {
		t.Fatalf("ensure sync_conflicts table: %v", err)
	}
}

func TestIntegration_SyncStatusRequiresAuth(t *testing.T) {
	pool, h := setupIntegration(t)
	ensureSyncConflictsTable(t, pool)
	status, env := doJSON(t, h, http.MethodGet, "/v1/sync/status", 0, nil)
	if status != http.StatusUnauthorized && env.Success {
		t.Fatalf("expected unauthorized, got status=%d success=%v", status, env.Success)
	}
}

func TestIntegration_SyncStatusListAndFilters(t *testing.T) {
	pool, h := setupIntegration(t)
	ensureSyncConflictsTable(t, pool)
	user := createTestUser(t, pool, "sync-status-list")
	logbook := createLogbookViaAPI(t, h, user.ID, "Sync logbook", true)

	now := time.Now().UTC().Truncate(time.Second)
	qso1 := createQSOViaAPI(t, h, user.ID, logbook, map[string]any{
		"callsign":    "K1AAA",
		"band":        "20m",
		"mode":        "CW",
		"datetime_on": now.Add(-2 * time.Hour).Format(time.RFC3339),
	})
	qso2 := createQSOViaAPI(t, h, user.ID, logbook, map[string]any{
		"callsign":    "W1BBB",
		"band":        "40m",
		"mode":        "SSB",
		"datetime_on": now.Add(-1 * time.Hour).Format(time.RFC3339),
	})

	_, err := pool.Exec(context.Background(), `
		INSERT INTO sync_status (qso_id, service, status, updated_at)
		SELECT id, 'eqsl', 'pending', NOW() FROM qsos WHERE uuid = $1::uuid
		ON CONFLICT (qso_id, service) DO UPDATE SET status = EXCLUDED.status
	`, qso1.UUID)
	if err != nil {
		t.Fatalf("insert sync status qso1: %v", err)
	}

	_, err = pool.Exec(context.Background(), `
		INSERT INTO sync_status (qso_id, service, status, error_message, last_error_code, updated_at)
		SELECT id, 'qrz', 'error', 'boom', 'permanent_failure', NOW() FROM qsos WHERE uuid = $1::uuid
		ON CONFLICT (qso_id, service) DO UPDATE
		SET status = EXCLUDED.status,
			error_message = EXCLUDED.error_message,
			last_error_code = EXCLUDED.last_error_code
	`, qso2.UUID)
	if err != nil {
		t.Fatalf("insert sync status qso2: %v", err)
	}

	status, env := doJSON(t, h, http.MethodGet, "/v1/sync/status?page=1&page_size=10", user.ID, nil)
	if status != http.StatusOK || !env.Success {
		t.Fatalf("sync status list: status=%d success=%v error=%q", status, env.Success, env.Error)
	}
	var payload struct {
		Items []struct {
			QSOUUID string `json:"qso_uuid"`
		} `json:"items"`
		Services map[string]struct {
			PendingCount      int64   `json:"pending_count"`
			UploadedCount     int64   `json:"uploaded_count"`
			FailedCount       int64   `json:"failed_count"`
			TotalCount        int64   `json:"total_count"`
			ErrorMessage      *string `json:"error_message"`
			HasPermanentError bool    `json:"has_permanent_error"`
			IsRunning         bool    `json:"is_running"`
		} `json:"services"`
		Pagination struct {
			Total int64 `json:"total"`
		} `json:"pagination"`
	}
	decodeData(t, env.Data, &payload)
	if payload.Pagination.Total < 2 {
		t.Fatalf("expected at least 2 rows, got %d", payload.Pagination.Total)
	}
	if payload.Services["eqsl"].PendingCount < 1 {
		t.Fatalf("expected eqsl pending_count >= 1, got %+v", payload.Services["eqsl"])
	}
	if payload.Services["qrz"].FailedCount < 1 {
		t.Fatalf("expected qrz failed_count >= 1, got %+v", payload.Services["qrz"])
	}
	if payload.Services["qrz"].TotalCount < payload.Services["qrz"].FailedCount {
		t.Fatalf("expected qrz total_count >= failed_count, got %+v", payload.Services["qrz"])
	}
	if payload.Services["qrz"].ErrorMessage == nil || *payload.Services["qrz"].ErrorMessage == "" {
		t.Fatalf("expected qrz error_message, got %+v", payload.Services["qrz"])
	}
	if !payload.Services["qrz"].HasPermanentError {
		t.Fatalf("expected qrz has_permanent_error=true, got %+v", payload.Services["qrz"])
	}

	status, env = doJSON(t, h, http.MethodGet, "/v1/sync/status?service=qrz&status=error", user.ID, nil)
	if status != http.StatusOK || !env.Success {
		t.Fatalf("sync status filter: status=%d success=%v error=%q", status, env.Success, env.Error)
	}
	decodeData(t, env.Data, &payload)
	if len(payload.Items) == 0 {
		t.Fatal("expected filtered rows for qrz error")
	}
}

func TestIntegration_SyncBulkUploadAndRetry(t *testing.T) {
	pool, h := setupIntegration(t)
	ensureSyncConflictsTable(t, pool)
	user := createTestUser(t, pool, "sync-bulk-retry")
	logbook := createLogbookViaAPI(t, h, user.ID, "Bulk logbook", true)

	now := time.Now().UTC().Truncate(time.Second)
	qso := createQSOViaAPI(t, h, user.ID, logbook, map[string]any{
		"callsign":    "N0CALL",
		"band":        "20m",
		"mode":        "FT8",
		"datetime_on": now.Format(time.RFC3339),
	})

	_, err := pool.Exec(context.Background(), `
		INSERT INTO sync_status (qso_id, service, status, error_message, retry_count, updated_at)
		SELECT id, 'eqsl', 'error', 'upload failed', 3, NOW() FROM qsos WHERE uuid = $1::uuid
		ON CONFLICT (qso_id, service) DO UPDATE
		SET status = EXCLUDED.status,
			error_message = EXCLUDED.error_message,
			retry_count = EXCLUDED.retry_count
	`, qso.UUID)
	if err != nil {
		t.Fatalf("insert error sync row: %v", err)
	}

	status, env := doJSON(t, h, http.MethodPost, "/v1/sync/bulk-upload", user.ID, map[string]any{"service": "eqsl"})
	if status != http.StatusAccepted && status != http.StatusServiceUnavailable {
		t.Fatalf("bulk upload: unexpected status=%d success=%v error=%q", status, env.Success, env.Error)
	}

	var current string
	if err := pool.QueryRow(context.Background(), `
		SELECT ss.status
		FROM sync_status ss
		JOIN qsos q ON q.id = ss.qso_id
		WHERE q.uuid = $1::uuid AND ss.service = 'eqsl'
	`, qso.UUID).Scan(&current); err != nil {
		t.Fatalf("query updated status: %v", err)
	}
	if current != "pending" {
		t.Fatalf("expected pending after bulk upload, got %q", current)
	}

	_, err = pool.Exec(context.Background(), `
		UPDATE sync_status ss
		SET status = 'error', retry_count = 3, error_message = 'upload failed', updated_at = NOW()
		FROM qsos q
		WHERE ss.qso_id = q.id
		  AND q.uuid = $1::uuid
		  AND ss.service = 'eqsl'
	`, qso.UUID)
	if err != nil {
		t.Fatalf("reseed error sync row: %v", err)
	}

	status, env = doJSON(t, h, http.MethodPost, "/v1/sync/retry", user.ID, map[string]any{"service": "eqsl"})
	if status != http.StatusAccepted || !env.Success {
		t.Fatalf("retry endpoint: status=%d success=%v error=%q", status, env.Success, env.Error)
	}

	var retryCount int16
	if err := pool.QueryRow(context.Background(), `
		SELECT ss.retry_count
		FROM sync_status ss
		JOIN qsos q ON q.id = ss.qso_id
		WHERE q.uuid = $1::uuid AND ss.service = 'eqsl'
	`, qso.UUID).Scan(&retryCount); err != nil {
		t.Fatalf("query retry_count: %v", err)
	}
	if retryCount != 0 {
		t.Fatalf("expected retry_count reset to 0 after retry, got %d", retryCount)
	}
}

func TestIntegration_SyncCancelPending(t *testing.T) {
	pool, h := setupIntegration(t)
	ensureSyncConflictsTable(t, pool)
	user := createTestUser(t, pool, "sync-cancel")
	logbook := createLogbookViaAPI(t, h, user.ID, "Cancel logbook", true)

	now := time.Now().UTC().Truncate(time.Second)
	qso := createQSOViaAPI(t, h, user.ID, logbook, map[string]any{
		"callsign":    "K9CNL",
		"band":        "20m",
		"mode":        "SSB",
		"datetime_on": now.Format(time.RFC3339),
	})

	_, err := pool.Exec(context.Background(), `
		INSERT INTO sync_status (qso_id, service, status, updated_at)
		SELECT id, 'qrz', 'pending', NOW() FROM qsos WHERE uuid = $1::uuid
		ON CONFLICT (qso_id, service) DO UPDATE SET status = EXCLUDED.status
	`, qso.UUID)
	if err != nil {
		t.Fatalf("insert pending sync row: %v", err)
	}

	status, env := doJSON(t, h, http.MethodPost, "/v1/sync/cancel", user.ID, map[string]any{"service": "qrz"})
	if status != http.StatusOK || !env.Success {
		t.Fatalf("cancel endpoint: status=%d success=%v error=%q", status, env.Success, env.Error)
	}

	var current string
	if err := pool.QueryRow(context.Background(), `
		SELECT ss.status
		FROM sync_status ss
		JOIN qsos q ON q.id = ss.qso_id
		WHERE q.uuid = $1::uuid AND ss.service = 'qrz'
	`, qso.UUID).Scan(&current); err != nil {
		t.Fatalf("query cancelled status: %v", err)
	}
	if current != "skipped" {
		t.Fatalf("expected skipped (cancelled by user) after cancel endpoint, got %q", current)
	}
}

func TestIntegration_SyncConflictsResolve(t *testing.T) {
	pool, h := setupIntegration(t)
	ensureSyncConflictsTable(t, pool)
	user := createTestUser(t, pool, "sync-conflicts")
	logbook := createLogbookViaAPI(t, h, user.ID, "Conflict logbook", true)

	now := time.Now().UTC().Truncate(time.Second)
	qso := createQSOViaAPI(t, h, user.ID, logbook, map[string]any{
		"callsign":    "DL1XYZ",
		"band":        "17m",
		"mode":        "SSB",
		"datetime_on": now.Format(time.RFC3339),
	})

	var conflictID int64
	err := pool.QueryRow(context.Background(), `
		INSERT INTO sync_conflicts (qso_id, service_a, service_b, field_conflicts, status)
		SELECT id, 'eqsl', 'qrz', '{"band": {"eqsl":"17m", "qrz":"20m"}}'::jsonb, 'open'
		FROM qsos WHERE uuid = $1::uuid
		RETURNING id
	`, qso.UUID).Scan(&conflictID)
	if err != nil {
		t.Fatalf("insert conflict: %v", err)
	}

	status, env := doJSON(t, h, http.MethodGet, "/v1/sync/conflicts", user.ID, nil)
	if status != http.StatusOK || !env.Success {
		t.Fatalf("conflicts list: status=%d success=%v error=%q", status, env.Success, env.Error)
	}

	status, env = doJSON(t, h, http.MethodPost, "/v1/sync/conflicts/"+itoa(conflictID)+"/resolve", user.ID, map[string]any{
		"fields": map[string]any{"band": "eqsl"},
	})
	if status != http.StatusOK || !env.Success {
		t.Fatalf("resolve conflict: status=%d success=%v error=%q", status, env.Success, env.Error)
	}

	var state string
	if err := pool.QueryRow(context.Background(), `SELECT status FROM sync_conflicts WHERE id = $1`, conflictID).Scan(&state); err != nil {
		t.Fatalf("query conflict status: %v", err)
	}
	if state != "resolved" {
		t.Fatalf("expected resolved conflict, got %q", state)
	}
}

func TestIntegration_SyncVerifyCredentialsUnavailableWithoutKeyring(t *testing.T) {
	pool, h := setupIntegration(t)
	ensureSyncConflictsTable(t, pool)
	user := createTestUser(t, pool, "sync-verify-credentials")

	status, env := doJSON(t, h, http.MethodPost, "/v1/sync/verify-credentials", user.ID, map[string]any{"service": "qrz"})
	if status != http.StatusServiceUnavailable || env.Success {
		t.Fatalf("expected 503 when keyring is unavailable, got status=%d success=%v error=%q", status, env.Success, env.Error)
	}
}

func itoa(v int64) string { return fmt.Sprintf("%d", v) }
