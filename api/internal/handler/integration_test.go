package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"

	"github.com/FtlC-ian/radioledger/api/internal/config"
	"github.com/FtlC-ian/radioledger/api/internal/jobs"
	"github.com/FtlC-ian/radioledger/api/internal/router"
)

type apiEnvelope struct {
	Success bool            `json:"success"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
	Error   string          `json:"error"`
}

type testUser struct {
	ID   int64
	UUID uuid.UUID
}

type logbookPayload struct {
	UUID      string `json:"uuid"`
	Name      string `json:"name"`
	IsDefault bool   `json:"is_default"`
}

var (
	integrationRiverMigrationsOnce sync.Once
	integrationRiverMigrationsErr  error
)

type qsoPayload struct {
	UUID        string `json:"uuid"`
	LogbookUUID string `json:"logbook_uuid"`
	Callsign    string `json:"callsign"`
	Band        string `json:"band"`
	Mode        string `json:"mode"`
}

type listLogbooksPayload struct {
	Items []logbookPayload `json:"items"`
}

type listQSOsPayload struct {
	Items      []qsoPayload `json:"items"`
	NextCursor string       `json:"next_cursor"`
}

func TestIntegration_LogbookCRUD(t *testing.T) {
	pool, h := setupIntegration(t)
	user := createTestUser(t, pool, "logbook-a")

	status, env := doJSON(t, h, http.MethodPost, "/v1/logbooks", user.ID, map[string]any{
		"name":       "Home Station",
		"callsign":   "w1abc",
		"is_default": true,
	})
	if status != http.StatusCreated || !env.Success {
		t.Fatalf("create logbook failed: status=%d success=%v error=%q", status, env.Success, env.Error)
	}
	assertNoIDKey(t, env.Data)

	var created logbookPayload
	decodeData(t, env.Data, &created)
	if _, err := uuid.Parse(created.UUID); err != nil {
		t.Fatalf("create logbook returned invalid uuid: %q", created.UUID)
	}
	if created.Name != "Home Station" {
		t.Fatalf("unexpected logbook name: %q", created.Name)
	}
	if !created.IsDefault {
		t.Fatal("expected created logbook to be default")
	}

	status, env = doJSON(t, h, http.MethodGet, "/v1/logbooks/default", user.ID, nil)
	if status != http.StatusOK || !env.Success {
		t.Fatalf("get default logbook failed: status=%d success=%v error=%q", status, env.Success, env.Error)
	}
	assertNoIDKey(t, env.Data)

	var currentDefault logbookPayload
	decodeData(t, env.Data, &currentDefault)
	if currentDefault.UUID != created.UUID {
		t.Fatalf("expected default %s, got %s", created.UUID, currentDefault.UUID)
	}

	status, env = doJSON(t, h, http.MethodPost, "/v1/logbooks", user.ID, map[string]any{
		"name":       "Portable",
		"callsign":   "w1abc/p",
		"is_default": false,
	})
	if status != http.StatusCreated || !env.Success {
		t.Fatalf("create second logbook failed: status=%d success=%v error=%q", status, env.Success, env.Error)
	}
	assertNoIDKey(t, env.Data)

	var second logbookPayload
	decodeData(t, env.Data, &second)

	status, env = doJSON(t, h, http.MethodPut, "/v1/logbooks/"+second.UUID, user.ID, map[string]any{
		"name":       "Portable Updated",
		"callsign":   "W1ABC/P",
		"is_default": true,
	})
	if status != http.StatusOK || !env.Success {
		t.Fatalf("update logbook failed: status=%d success=%v error=%q", status, env.Success, env.Error)
	}
	assertNoIDKey(t, env.Data)

	status, env = doJSON(t, h, http.MethodGet, "/v1/logbooks/default", user.ID, nil)
	if status != http.StatusOK || !env.Success {
		t.Fatalf("get default after update failed: status=%d success=%v error=%q", status, env.Success, env.Error)
	}
	decodeData(t, env.Data, &currentDefault)
	if currentDefault.UUID != second.UUID {
		t.Fatalf("expected default %s, got %s", second.UUID, currentDefault.UUID)
	}

	status, env = doJSON(t, h, http.MethodGet, "/v1/logbooks", user.ID, nil)
	if status != http.StatusOK || !env.Success {
		t.Fatalf("list logbooks failed: status=%d success=%v error=%q", status, env.Success, env.Error)
	}
	assertNoIDKey(t, env.Data)

	var listed listLogbooksPayload
	decodeData(t, env.Data, &listed)
	if len(listed.Items) != 2 {
		t.Fatalf("expected 2 logbooks, got %d", len(listed.Items))
	}
}

func TestIntegration_QSOCRUDSearchPaginationAndRLS(t *testing.T) {
	pool, h := setupIntegration(t)
	userA := createTestUser(t, pool, "qso-a")
	userB := createTestUser(t, pool, "qso-b")

	logbookA := createLogbookViaAPI(t, h, userA.ID, "A Logbook", true)
	logbookB := createLogbookViaAPI(t, h, userB.ID, "B Logbook", true)

	now := time.Now().UTC().Truncate(time.Second)
	qso1 := createQSOViaAPI(t, h, userA.ID, logbookA, map[string]any{
		"callsign":    "K1AAA",
		"band":        "20m",
		"mode":        "CW",
		"datetime_on": now.Add(-3 * time.Hour).Format(time.RFC3339),
		"dxcc":        291,
		"gridsquare":  "EM10",
		"comment":     "first",
	})
	qso2 := createQSOViaAPI(t, h, userA.ID, logbookA, map[string]any{
		"callsign":    "W1BBB",
		"band":        "40m",
		"mode":        "SSB",
		"datetime_on": now.Add(-2 * time.Hour).Format(time.RFC3339),
		"dxcc":        291,
		"gridsquare":  "EM12",
		"comment":     "second",
	})
	qso3 := createQSOViaAPI(t, h, userA.ID, logbookA, map[string]any{
		"callsign":    "DL1CCC",
		"band":        "20m",
		"mode":        "FT8",
		"datetime_on": now.Add(-1 * time.Hour).Format(time.RFC3339),
		"dxcc":        230,
		"gridsquare":  "JO62",
		"comment":     "third",
	})
	_ = qso2

	createQSOViaAPI(t, h, userB.ID, logbookB, map[string]any{
		"callsign":    "JA1ZZZ",
		"band":        "15m",
		"mode":        "CW",
		"datetime_on": now.Add(-30 * time.Minute).Format(time.RFC3339),
	})

	status, env := doJSON(t, h, http.MethodGet, "/v1/logbooks/"+logbookA+"/qsos/"+qso1.UUID, userA.ID, nil)
	if status != http.StatusOK || !env.Success {
		t.Fatalf("get qso failed: status=%d success=%v error=%q", status, env.Success, env.Error)
	}
	assertNoIDKey(t, env.Data)

	status, env = doJSON(t, h, http.MethodPut, "/v1/logbooks/"+logbookA+"/qsos/"+qso1.UUID, userA.ID, map[string]any{
		"callsign":    "K1AAA",
		"band":        "20m",
		"mode":        "CW",
		"datetime_on": now.Add(-3 * time.Hour).Format(time.RFC3339),
		"dxcc":        291,
		"gridsquare":  "EM10",
		"comment":     "updated",
	})
	if status != http.StatusOK || !env.Success {
		t.Fatalf("update qso failed: status=%d success=%v error=%q", status, env.Success, env.Error)
	}

	status, env = doJSON(t, h, http.MethodPatch, "/v1/logbooks/"+logbookA+"/qsos/"+qso1.UUID, userA.ID, map[string]any{
		"notes": "patched",
	})
	if status != http.StatusOK || !env.Success {
		t.Fatalf("patch qso failed: status=%d success=%v error=%q", status, env.Success, env.Error)
	}

	searchPath := fmt.Sprintf(
		"/v1/logbooks/%s/qsos?callsign=DL1&band=20m&mode=FT8&dxcc=230&gridsquare=JO&date_from=%s&date_to=%s",
		logbookA,
		now.Add(-90*time.Minute).Format(time.RFC3339),
		now.Format(time.RFC3339),
	)
	status, env = doJSON(t, h, http.MethodGet, searchPath, userA.ID, nil)
	if status != http.StatusOK || !env.Success {
		t.Fatalf("search qsos failed: status=%d success=%v error=%q", status, env.Success, env.Error)
	}
	assertNoIDKey(t, env.Data)

	var searched listQSOsPayload
	decodeData(t, env.Data, &searched)
	if len(searched.Items) != 1 || searched.Items[0].UUID != qso3.UUID {
		t.Fatalf("search expected only %s, got %+v", qso3.UUID, searched.Items)
	}

	status, env = doJSON(t, h, http.MethodGet, "/v1/logbooks/"+logbookA+"/qsos?limit=2", userA.ID, nil)
	if status != http.StatusOK || !env.Success {
		t.Fatalf("list page1 failed: status=%d success=%v error=%q", status, env.Success, env.Error)
	}
	decodeData(t, env.Data, &searched)
	if len(searched.Items) != 2 {
		t.Fatalf("expected 2 items on first page, got %d", len(searched.Items))
	}
	if searched.NextCursor == "" {
		t.Fatal("expected next_cursor on first page")
	}

	status, env = doJSON(t, h, http.MethodGet, "/v1/logbooks/"+logbookA+"/qsos?limit=2&after="+searched.NextCursor, userA.ID, nil)
	if status != http.StatusOK || !env.Success {
		t.Fatalf("list page2 failed: status=%d success=%v error=%q", status, env.Success, env.Error)
	}
	decodeData(t, env.Data, &searched)
	if len(searched.Items) != 1 {
		t.Fatalf("expected 1 item on second page, got %d", len(searched.Items))
	}

	// RBAC: user B has no role in logbook A, so requests are blocked at the
	// permission layer before reaching the database-level RLS filter.
	status, env = doJSON(t, h, http.MethodGet, "/v1/logbooks/"+logbookA+"/qsos/"+qso1.UUID, userB.ID, nil)
	if status != http.StatusForbidden || env.Success {
		t.Fatalf("RBAC get should be forbidden: status=%d success=%v error=%q", status, env.Success, env.Error)
	}

	status, env = doJSON(t, h, http.MethodGet, "/v1/logbooks/"+logbookA+"/qsos", userB.ID, nil)
	if status != http.StatusForbidden || env.Success {
		t.Fatalf("RBAC list should be forbidden: status=%d success=%v error=%q", status, env.Success, env.Error)
	}

	status, env = doJSON(t, h, http.MethodDelete, "/v1/logbooks/"+logbookA+"/qsos/"+qso1.UUID, userA.ID, nil)
	if status != http.StatusOK || !env.Success {
		t.Fatalf("delete qso failed: status=%d success=%v error=%q", status, env.Success, env.Error)
	}
	assertNoIDKey(t, env.Data)

	status, env = doJSON(t, h, http.MethodGet, "/v1/logbooks/"+logbookA+"/qsos/"+qso1.UUID, userA.ID, nil)
	if status != http.StatusOK || env.Success {
		t.Fatalf("deleted qso should return success=false envelope: status=%d success=%v", status, env.Success)
	}
}

func TestIntegration_QSOEditResetsSyncStatus_DeleteCleansSync_AndMarksAwardsDirty(t *testing.T) {
	pool, h := setupIntegration(t)
	user := createTestUser(t, pool, "qso-edit-sync")
	logbookUUID := createLogbookViaAPI(t, h, user.ID, "Edit Sync Logbook", true)

	now := time.Now().UTC().Truncate(time.Second)
	created := createQSOViaAPI(t, h, user.ID, logbookUUID, map[string]any{
		"callsign":    "K1AAA",
		"band":        "20m",
		"mode":        "CW",
		"datetime_on": now.Format(time.RFC3339),
		"dxcc":        291,
	})

	ctx := context.Background()
	var qsoID int64
	if err := pool.QueryRow(ctx, `SELECT id FROM qsos WHERE uuid = $1::uuid`, created.UUID).Scan(&qsoID); err != nil {
		t.Fatalf("lookup qso id: %v", err)
	}

	if _, err := pool.Exec(ctx, `
		INSERT INTO sync_status
			(qso_id, service, status, last_synced_at, remote_id, error_message, last_error_code, retry_count, next_retry_at, updated_at)
		VALUES
			($1, 'eqsl', 'uploaded', NOW(), 'eqsl-123', 'old err', 'E_OLD', 3, NOW() + INTERVAL '30 minutes', NOW()),
			($1, 'clublog', 'error', NOW(), 'clublog-456', 'old err', 'E_OLD', 2, NOW() + INTERVAL '15 minutes', NOW())
		ON CONFLICT (qso_id, service) DO UPDATE
		SET status = EXCLUDED.status,
			last_synced_at = EXCLUDED.last_synced_at,
			remote_id = EXCLUDED.remote_id,
			error_message = EXCLUDED.error_message,
			last_error_code = EXCLUDED.last_error_code,
			retry_count = EXCLUDED.retry_count,
			next_retry_at = EXCLUDED.next_retry_at,
			updated_at = NOW()
	`, qsoID); err != nil {
		t.Fatalf("seed sync_status rows: %v", err)
	}

	if _, err := pool.Exec(ctx, `
		DELETE FROM award_progress
		WHERE user_id = $1 AND award_type = 'dxcc' AND entity_key = '291'
	`, user.ID); err != nil {
		t.Fatalf("clear existing award_progress seed row: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO award_progress (user_id, award_type, entity_key, worked, confirmed, dirty, qso_count)
		VALUES ($1, 'dxcc', '291', TRUE, FALSE, FALSE, 1)
	`, user.ID); err != nil {
		t.Fatalf("seed award_progress: %v", err)
	}

	status, env := doJSON(t, h, http.MethodPut, "/v1/logbooks/"+logbookUUID+"/qsos/"+created.UUID, user.ID, map[string]any{
		"callsign":    "K1AAA",
		"band":        "20m",
		"mode":        "CW",
		"datetime_on": now.Add(1 * time.Minute).Format(time.RFC3339),
		"dxcc":        291,
		"comment":     "edited",
	})
	if status != http.StatusOK || !env.Success {
		t.Fatalf("update qso failed: status=%d success=%v error=%q", status, env.Success, env.Error)
	}

	rows, err := pool.Query(ctx, `
		SELECT service, status, retry_count, error_message, last_error_code, last_synced_at, remote_id, next_retry_at
		FROM sync_status
		WHERE qso_id = $1
		ORDER BY service
	`, qsoID)
	if err != nil {
		t.Fatalf("query sync_status after edit: %v", err)
	}
	defer rows.Close()

	var seen int
	for rows.Next() {
		var service, syncState string
		var retryCount int16
		var errMsg, errCode, remoteID *string
		var lastSyncedAt, nextRetryAt *time.Time
		if err := rows.Scan(&service, &syncState, &retryCount, &errMsg, &errCode, &lastSyncedAt, &remoteID, &nextRetryAt); err != nil {
			t.Fatalf("scan sync row: %v", err)
		}
		seen++
		if syncState != "pending" {
			t.Fatalf("service %s expected pending, got %s", service, syncState)
		}
		if retryCount != 0 {
			t.Fatalf("service %s expected retry_count=0, got %d", service, retryCount)
		}
		if errMsg != nil || errCode != nil || lastSyncedAt != nil || remoteID != nil || nextRetryAt != nil {
			t.Fatalf("service %s expected reset nullable sync fields, got errMsg=%v errCode=%v lastSyncedAt=%v remoteID=%v nextRetryAt=%v", service, errMsg, errCode, lastSyncedAt, remoteID, nextRetryAt)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate sync rows: %v", err)
	}
	if seen != 2 {
		t.Fatalf("expected 2 sync rows after edit, got %d", seen)
	}

	var dirty bool
	if err := pool.QueryRow(ctx, `SELECT dirty FROM award_progress WHERE user_id = $1 AND award_type = 'dxcc' AND entity_key = '291'`, user.ID).Scan(&dirty); err != nil {
		t.Fatalf("query award_progress after edit: %v", err)
	}
	if !dirty {
		t.Fatalf("expected award_progress dirty=true after qso edit")
	}

	if _, err := pool.Exec(ctx, `
		UPDATE award_progress
		SET dirty = FALSE, updated_at = NOW()
		WHERE user_id = $1 AND award_type = 'dxcc' AND entity_key = '291'
	`, user.ID); err != nil {
		t.Fatalf("reset award_progress dirty flag: %v", err)
	}

	status, env = doJSON(t, h, http.MethodDelete, "/v1/logbooks/"+logbookUUID+"/qsos/"+created.UUID, user.ID, nil)
	if status != http.StatusOK || !env.Success {
		t.Fatalf("delete qso failed: status=%d success=%v error=%q", status, env.Success, env.Error)
	}

	var syncCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM sync_status WHERE qso_id = $1`, qsoID).Scan(&syncCount); err != nil {
		t.Fatalf("count sync_status after delete: %v", err)
	}
	if syncCount != 0 {
		t.Fatalf("expected sync_status cleanup on delete, got %d rows", syncCount)
	}

	if err := pool.QueryRow(ctx, `SELECT dirty FROM award_progress WHERE user_id = $1 AND award_type = 'dxcc' AND entity_key = '291'`, user.ID).Scan(&dirty); err != nil {
		t.Fatalf("query award_progress after delete: %v", err)
	}
	if !dirty {
		t.Fatalf("expected award_progress dirty=true after qso delete")
	}
}

func TestIntegration_QSOAutoEnrichFromCallsignRecords(t *testing.T) {
	pool, h := setupIntegration(t)
	user := createTestUser(t, pool, "qso-enrich")
	logbookUUID := createLogbookViaAPI(t, h, user.ID, "Enrich Logbook", true)

	ctx := context.Background()
	_, err := pool.Exec(ctx, `
		INSERT INTO callsign_records (
			callsign, source, first_name, last_name, full_name,
			state_province, country, grid_square, dxcc_entity_id
		)
		VALUES ($1, 'fcc', $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (callsign, source) DO UPDATE SET
			first_name = EXCLUDED.first_name,
			last_name = EXCLUDED.last_name,
			full_name = EXCLUDED.full_name,
			state_province = EXCLUDED.state_province,
			country = EXCLUDED.country,
			grid_square = EXCLUDED.grid_square,
			dxcc_entity_id = EXCLUDED.dxcc_entity_id,
			updated_at = NOW()
	`, "W1AW", "Hiram", "Maxim", "Hiram Percy Maxim", "CT", "US", "FN31", 291)
	if err != nil {
		t.Fatalf("seed callsign_records: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM callsign_records WHERE callsign = $1 AND source = 'fcc'`, "W1AW")
	})

	now := time.Now().UTC().Truncate(time.Second)
	created := createQSOViaAPI(t, h, user.ID, logbookUUID, map[string]any{
		"callsign":    "w1aw",
		"band":        "20m",
		"mode":        "CW",
		"datetime_on": now.Format(time.RFC3339),
	})

	var state, country, gridsquare, name *string
	var cqZone, ituZone *int16
	err = pool.QueryRow(ctx, `
		SELECT state, country, cq_zone, itu_zone, gridsquare, name
		FROM qsos
		WHERE uuid = $1::uuid
	`, created.UUID).Scan(&state, &country, &cqZone, &ituZone, &gridsquare, &name)
	if err != nil {
		t.Fatalf("query enriched qso: %v", err)
	}
	if state == nil || *state != "CT" {
		t.Fatalf("expected state CT, got %v", state)
	}
	if country == nil || *country != "US" {
		t.Fatalf("expected country US, got %v", country)
	}
	if cqZone == nil || *cqZone != 3 {
		t.Fatalf("expected cq_zone 3, got %v", cqZone)
	}
	if ituZone == nil || *ituZone != 6 {
		t.Fatalf("expected itu_zone 6, got %v", ituZone)
	}
	if gridsquare == nil || *gridsquare != "FN31" {
		t.Fatalf("expected gridsquare FN31, got %v", gridsquare)
	}
	if name == nil || *name != "Hiram Percy Maxim" {
		t.Fatalf("expected name Hiram Percy Maxim, got %v", name)
	}

	_, err = pool.Exec(ctx, `
		UPDATE qsos
		SET state = 'TX', country = 'CA', cq_zone = 4, itu_zone = 7, gridsquare = 'EM12', name = 'Custom Name'
		WHERE uuid = $1::uuid
	`, created.UUID)
	if err != nil {
		t.Fatalf("seed custom qso values: %v", err)
	}

	status, env := doJSON(t, h, http.MethodPut, "/v1/logbooks/"+logbookUUID+"/qsos/"+created.UUID, user.ID, map[string]any{
		"callsign":    "W1AW",
		"band":        "20m",
		"mode":        "CW",
		"datetime_on": now.Add(1 * time.Minute).Format(time.RFC3339),
		"gridsquare":  "EM12",
	})
	if status != http.StatusOK || !env.Success {
		t.Fatalf("update qso failed: status=%d success=%v error=%q", status, env.Success, env.Error)
	}

	err = pool.QueryRow(ctx, `
		SELECT state, country, cq_zone, itu_zone, gridsquare, name
		FROM qsos
		WHERE uuid = $1::uuid
	`, created.UUID).Scan(&state, &country, &cqZone, &ituZone, &gridsquare, &name)
	if err != nil {
		t.Fatalf("query qso after update: %v", err)
	}
	if state == nil || *state != "TX" {
		t.Fatalf("expected state TX to remain user-entered, got %v", state)
	}
	if country == nil || *country != "CA" {
		t.Fatalf("expected country CA to remain user-entered, got %v", country)
	}
	if cqZone == nil || *cqZone != 4 {
		t.Fatalf("expected cq_zone 4 to remain user-entered, got %v", cqZone)
	}
	if ituZone == nil || *ituZone != 7 {
		t.Fatalf("expected itu_zone 7 to remain user-entered, got %v", ituZone)
	}
	if gridsquare == nil || *gridsquare != "EM12" {
		t.Fatalf("expected gridsquare EM12 to remain user-entered, got %v", gridsquare)
	}
	if name == nil || *name != "Custom Name" {
		t.Fatalf("expected name Custom Name to remain user-entered, got %v", name)
	}
}

func ensureRiverSchemaForIntegrationTests(pool *pgxpool.Pool) error {
	integrationRiverMigrationsOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		integrationRiverMigrationsErr = runRiverMigrations(ctx, pool)
	})

	return integrationRiverMigrationsErr
}

func runRiverMigrations(ctx context.Context, pool *pgxpool.Pool) error {
	migrator, err := rivermigrate.New(riverpgxv5.New(pool), nil)
	if err != nil {
		return fmt.Errorf("creating river migrator: %w", err)
	}

	if _, err := migrator.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
		return fmt.Errorf("applying river migrations: %w", err)
	}

	return applyRiverRoleGrants(ctx, pool)
}

func applyRiverRoleGrants(ctx context.Context, pool *pgxpool.Pool) error {
	const grantSQL = `
DO $$
DECLARE
	has_api_role BOOLEAN := EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'radioledger_api');
	has_worker_role BOOLEAN := EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'radioledger_worker');
	t RECORD;
	s RECORD;
BEGIN
	FOR t IN
		SELECT schemaname, tablename
		FROM pg_tables
		WHERE schemaname = 'public' AND tablename LIKE 'river\_%' ESCAPE '\'
	LOOP
		IF has_api_role THEN
			EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE %I.%I TO radioledger_api', t.schemaname, t.tablename);
		END IF;
		IF has_worker_role THEN
			EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE %I.%I TO radioledger_worker', t.schemaname, t.tablename);
		END IF;
	END LOOP;

	FOR s IN
		SELECT sequence_schema, sequence_name
		FROM information_schema.sequences
		WHERE sequence_schema = 'public' AND sequence_name LIKE 'river\_%' ESCAPE '\'
	LOOP
		IF has_api_role THEN
			EXECUTE format('GRANT USAGE, SELECT ON SEQUENCE %I.%I TO radioledger_api', s.sequence_schema, s.sequence_name);
		END IF;
		IF has_worker_role THEN
			EXECUTE format('GRANT USAGE, SELECT ON SEQUENCE %I.%I TO radioledger_worker', s.sequence_schema, s.sequence_name);
		END IF;
	END LOOP;
END
$$;
`

	if _, err := pool.Exec(ctx, grantSQL); err != nil {
		return fmt.Errorf("apply river grants: %w", err)
	}

	return nil
}

func setupIntegration(t *testing.T) (*pgxpool.Pool, http.Handler) {
	return setupIntegrationWithConfig(t, func(cfg *config.Config) {})
}

func setupIntegrationWithConfig(t *testing.T, mutate func(*config.Config)) (*pgxpool.Pool, http.Handler) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	dbURL := os.Getenv("RADIOLEDGER_TEST_DATABASE_URL")
	if strings.TrimSpace(dbURL) == "" {
		t.Skip("RADIOLEDGER_TEST_DATABASE_URL is required for integration tests")
	}

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Skipf("skipping integration tests: cannot create pool: %v", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("skipping integration tests: cannot connect to db: %v", err)
	}

	if err := ensureRiverSchemaForIntegrationTests(pool); err != nil {
		pool.Close()
		t.Fatalf("setup integration db: %v", err)
	}

	t.Cleanup(pool.Close)

	cfg := &config.Config{
		CORSAllowedOrigins: "https://integration.test",
		RateLimitIPRPS:     1000,
		RateLimitIPBurst:   2000,
		AuthMode:           "dev",
		Env:                "development",
	}
	mutate(cfg)

	return pool, router.New(cfg, pool, nil)
}

func createTestUser(t *testing.T, pool *pgxpool.Pool, label string) testUser {
	t.Helper()

	email := fmt.Sprintf("it_%s_%d@example.test", label, time.Now().UnixNano())
	var u testUser
	err := pool.QueryRow(context.Background(), `
		INSERT INTO users (email, timezone, display_name)
		VALUES ($1, 'UTC', $2)
		RETURNING id, uuid
	`, email, "Integration "+label).Scan(&u.ID, &u.UUID)
	if err != nil {
		t.Fatalf("create test user: %v", err)
	}

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = pool.Exec(ctx, `DELETE FROM qsos WHERE logbook_id IN (SELECT id FROM logbooks WHERE user_id = $1)`, u.ID)
		_, _ = pool.Exec(ctx, `DELETE FROM logbooks WHERE user_id = $1`, u.ID)
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, u.ID)
	})

	return u
}

func createLogbookViaAPI(t *testing.T, h http.Handler, userID int64, name string, isDefault bool) string {
	t.Helper()
	status, env := doJSON(t, h, http.MethodPost, "/v1/logbooks", userID, map[string]any{
		"name":       name,
		"callsign":   "W1ABC",
		"is_default": isDefault,
	})
	if status != http.StatusCreated || !env.Success {
		t.Fatalf("create logbook via api failed: status=%d success=%v error=%q", status, env.Success, env.Error)
	}
	assertNoIDKey(t, env.Data)

	var payload logbookPayload
	decodeData(t, env.Data, &payload)
	return payload.UUID
}

func createQSOViaAPI(t *testing.T, h http.Handler, userID int64, logbookUUID string, body map[string]any) qsoPayload {
	t.Helper()
	status, env := doJSON(t, h, http.MethodPost, "/v1/logbooks/"+logbookUUID+"/qsos", userID, body)
	if status != http.StatusCreated || !env.Success {
		t.Fatalf("create qso via api failed: status=%d success=%v error=%q", status, env.Success, env.Error)
	}
	assertNoIDKey(t, env.Data)

	var payload qsoPayload
	decodeData(t, env.Data, &payload)
	return payload
}

func issueTestLocalJWT(t *testing.T, userID int64) string {
	t.Helper()

	now := time.Now()
	claims := jwt.MapClaims{
		"iss":   "radioledger-local",
		"sub":   fmt.Sprintf("%d", userID),
		"uid":   userID,
		"email": fmt.Sprintf("it-user-%d@example.test", userID),
		"iat":   now.Unix(),
		"exp":   now.Add(30 * 24 * time.Hour).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte("radioledger-local-dev-jwt-secret!"))
	if err != nil {
		t.Fatalf("issue test local jwt: %v", err)
	}
	return signed
}

func setTestAuthHeader(t *testing.T, req *http.Request, userID int64) {
	t.Helper()
	req.Header.Set("Authorization", "Bearer "+issueTestLocalJWT(t, userID))
}

// doJSON performs an authenticated HTTP request against the test handler.
// When userID > 0, sets the Authorization header with a local-mode JWT.
// When userID == 0, sends an unauthenticated request (no Authorization header).
func doJSON(t *testing.T, h http.Handler, method, path string, userID int64, body any) (int, apiEnvelope) {
	t.Helper()

	var reqBody io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request body: %v", err)
		}
		reqBody = bytes.NewReader(raw)
	}

	req := httptest.NewRequest(method, path, reqBody)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if userID > 0 {
		setTestAuthHeader(t, req, userID)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	var env apiEnvelope
	if strings.Contains(rec.Header().Get("Content-Type"), "application/json") && rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
			t.Fatalf("decode api envelope (%s %s): %v body=%s", method, path, err, rec.Body.String())
		}
	}

	return rec.Code, env
}

func decodeData(t *testing.T, raw json.RawMessage, dst any) {
	t.Helper()
	if len(raw) == 0 {
		t.Fatalf("expected non-empty data payload")
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		t.Fatalf("decode data payload: %v payload=%s", err, string(raw))
	}
}

func assertNoIDKey(t *testing.T, raw json.RawMessage) {
	t.Helper()
	if len(raw) == 0 {
		return
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("decode json for id check: %v", err)
	}
	if hasIDKey(v) {
		t.Fatalf("response data exposed internal id field: %s", string(raw))
	}
}

func hasIDKey(v any) bool {
	switch vv := v.(type) {
	case map[string]any:
		for k, child := range vv {
			if strings.EqualFold(k, "id") {
				return true
			}
			if hasIDKey(child) {
				return true
			}
		}
	case []any:
		for _, child := range vv {
			if hasIDKey(child) {
				return true
			}
		}
	}
	return false
}

// ---- ADIF Import Integration Tests ----

type importJobPayload struct {
	JobUUID   string `json:"job_uuid"`
	Status    string `json:"status"`
	StatusURL string `json:"status_url"`
}

type importStatusPayload struct {
	UUID         string  `json:"uuid"`
	LogbookUUID  string  `json:"logbook_uuid"`
	Status       string  `json:"status"`
	TotalRecords *int32  `json:"total_records"`
	Imported     int32   `json:"imported"`
	Duplicate    int32   `json:"duplicate"`
	Skipped      int32   `json:"skipped"`
	Errors       int32   `json:"errors"`
	Warnings     int32   `json:"warnings"`
	PctComplete  float64 `json:"pct_complete"`
}

// TestIntegration_ADIFImport tests the full ADIF import pipeline:
//  1. Upload backup.adi through the POST /v1/import/adif endpoint
//  2. Run the River worker directly (synchronous for test determinism)
//  3. Verify all QSOs are in the database
//  4. Verify import_jobs counters are correct
//  5. Verify duplicate detection on re-import
func TestIntegration_ADIFImport(t *testing.T) {
	adifPath := os.Getenv("ADIF_TEST_FILE")
	if adifPath == "" {
		t.Skip("ADIF_TEST_FILE is required for the optional personal-log integration test")
	}
	if _, err := os.Stat(adifPath); os.IsNotExist(err) {
		t.Skipf("skipping ADIF import test: test file not found at %s", adifPath)
	}

	pool, h := setupIntegration(t)
	user := createTestUser(t, pool, "adif-import")
	logbookUUID := createLogbookViaAPI(t, h, user.ID, "Ian's Log", true)

	// Get the logbook internal ID (needed for the worker).
	var logbookID int64
	err := pool.QueryRow(context.Background(),
		`SELECT id FROM logbooks WHERE uuid = $1`, logbookUUID,
	).Scan(&logbookID)
	if err != nil {
		t.Fatalf("get logbook id: %v", err)
	}

	// --- 1. Upload backup.adi via the HTTP endpoint ---
	status, env := uploadADIF(t, h, user.ID, logbookUUID, adifPath)
	if status != http.StatusAccepted || !env.Success {
		t.Fatalf("upload adif failed: status=%d success=%v error=%q", status, env.Success, env.Error)
	}

	var uploadResp importJobPayload
	decodeData(t, env.Data, &uploadResp)
	if uploadResp.JobUUID == "" {
		t.Fatal("expected non-empty job_uuid in upload response")
	}
	if uploadResp.Status != "pending" {
		t.Fatalf("expected status=pending, got %q", uploadResp.Status)
	}
	t.Logf("import job created: %s", uploadResp.JobUUID)

	// Get the internal import job ID.
	var importJobID int64
	err = pool.QueryRow(context.Background(),
		`SELECT id FROM import_jobs WHERE uuid = $1`, uploadResp.JobUUID,
	).Scan(&importJobID)
	if err != nil {
		t.Fatalf("get import job id: %v", err)
	}

	// --- 2. Copy the ADIF file to a temp location for the worker ---
	workerTmpFile, err := os.CreateTemp("", "test-import-*.adif")
	if err != nil {
		t.Fatalf("create worker temp file: %v", err)
	}
	workerTmpPath := workerTmpFile.Name()

	src, err := os.Open(adifPath)
	if err != nil {
		_ = os.Remove(workerTmpPath)
		t.Fatalf("open source adif: %v", err)
	}
	_, err = io.Copy(workerTmpFile, src)
	_ = src.Close()
	_ = workerTmpFile.Close()
	if err != nil {
		_ = os.Remove(workerTmpPath)
		t.Fatalf("copy adif to temp: %v", err)
	}

	// --- 3. Run the import worker directly ---
	w := &jobs.ADIFImportWorker{Pool: pool}
	riverJob := &river.Job[jobs.ADIFImportArgs]{
		Args: jobs.ADIFImportArgs{
			ImportJobID: importJobID,
			FilePath:    workerTmpPath,
			LogbookID:   logbookID,
			UserID:      user.ID,
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	start := time.Now()
	if err := w.Work(ctx, riverJob); err != nil {
		t.Fatalf("worker failed: %v", err)
	}
	elapsed := time.Since(start)
	t.Logf("import completed in %v", elapsed)

	// Performance assertion: 2354 QSOs should import in < 15 seconds.
	if elapsed > 15*time.Second {
		t.Errorf("import too slow: %v (expected < 15s for 2354 QSOs)", elapsed)
	}

	// --- 4. Verify import_jobs counters ---
	var imported, duplicate, errors int32
	var jobStatus string
	err = pool.QueryRow(context.Background(),
		`SELECT status, imported, duplicate, errors FROM import_jobs WHERE uuid = $1`,
		uploadResp.JobUUID,
	).Scan(&jobStatus, &imported, &duplicate, &errors)
	if err != nil {
		t.Fatalf("query import job: %v", err)
	}

	t.Logf("import result: status=%s imported=%d duplicate=%d errors=%d", jobStatus, imported, duplicate, errors)

	if jobStatus != "complete" {
		t.Errorf("expected job status=complete, got %q", jobStatus)
	}
	if errors > 100 {
		t.Errorf("too many import errors: %d (expected < 100)", errors)
	}
	if imported < 2300 {
		t.Errorf("expected at least 2300 imported QSOs, got %d", imported)
	}

	// --- 5. Verify QSOs are in the database ---
	var qsoCount int64
	err = pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM qsos WHERE logbook_id = $1`, logbookID,
	).Scan(&qsoCount)
	if err != nil {
		t.Fatalf("count qsos: %v", err)
	}
	t.Logf("QSOs in database: %d", qsoCount)
	if qsoCount < 2300 {
		t.Errorf("expected at least 2300 QSOs in db, got %d", qsoCount)
	}

	// --- 6. Poll the status endpoint ---
	statusCode, statusEnv := doJSON(t, h, http.MethodGet, "/v1/import/"+uploadResp.JobUUID, user.ID, nil)
	if statusCode != http.StatusOK || !statusEnv.Success {
		t.Fatalf("get import status failed: status=%d success=%v", statusCode, statusEnv.Success)
	}
	var statusPayload importStatusPayload
	decodeData(t, statusEnv.Data, &statusPayload)
	if statusPayload.Status != "complete" {
		t.Errorf("expected status=complete from API, got %q", statusPayload.Status)
	}
	if statusPayload.PctComplete != 100 {
		t.Errorf("expected pct_complete=100, got %f", statusPayload.PctComplete)
	}
	if statusPayload.Imported < 2300 {
		t.Errorf("API: expected imported >= 2300, got %d", statusPayload.Imported)
	}

	// --- 7. Duplicate detection: upload the same file again ---
	t.Log("testing duplicate detection with second upload...")

	tmpFile2, err := os.CreateTemp("", "test-import2-*.adif")
	if err != nil {
		t.Fatalf("create second temp: %v", err)
	}
	tmpPath2 := tmpFile2.Name()
	src2, err := os.Open(adifPath)
	if err != nil {
		_ = os.Remove(tmpPath2)
		t.Fatalf("open second source: %v", err)
	}
	_, err = io.Copy(tmpFile2, src2)
	_ = src2.Close()
	_ = tmpFile2.Close()
	if err != nil {
		_ = os.Remove(tmpPath2)
		t.Fatalf("copy second adif: %v", err)
	}

	// Create a second import job via HTTP.
	status2, env2 := uploadADIF(t, h, user.ID, logbookUUID, adifPath)
	if status2 != http.StatusAccepted || !env2.Success {
		t.Fatalf("second upload failed: status=%d success=%v error=%q", status2, env2.Success, env2.Error)
	}
	var upload2Resp importJobPayload
	decodeData(t, env2.Data, &upload2Resp)

	var importJobID2 int64
	err = pool.QueryRow(context.Background(),
		`SELECT id FROM import_jobs WHERE uuid = $1`, upload2Resp.JobUUID,
	).Scan(&importJobID2)
	if err != nil {
		t.Fatalf("get second import job id: %v", err)
	}

	// Run second import directly.
	riverJob2 := &river.Job[jobs.ADIFImportArgs]{
		Args: jobs.ADIFImportArgs{
			ImportJobID: importJobID2,
			FilePath:    tmpPath2,
			LogbookID:   logbookID,
			UserID:      user.ID,
		},
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel2()
	if err := w.Work(ctx2, riverJob2); err != nil {
		t.Fatalf("second worker failed: %v", err)
	}

	var imported2, duplicate2 int32
	err = pool.QueryRow(context.Background(),
		`SELECT imported, duplicate FROM import_jobs WHERE uuid = $1`, upload2Resp.JobUUID,
	).Scan(&imported2, &duplicate2)
	if err != nil {
		t.Fatalf("query second import job: %v", err)
	}

	t.Logf("second import: imported=%d duplicate=%d", imported2, duplicate2)
	if duplicate2 < 2300 {
		t.Errorf("expected second import to detect >= 2300 duplicates, got %d", duplicate2)
	}
	if imported2 > 50 {
		t.Errorf("expected second import to insert < 50 new QSOs, got %d", imported2)
	}
}

// uploadADIF submits a multipart ADIF upload to POST /v1/import/adif.
func uploadADIF(t *testing.T, h http.Handler, userID int64, logbookUUID, filePath string) (int, apiEnvelope) {
	t.Helper()

	f, err := os.Open(filePath)
	if err != nil {
		t.Fatalf("open adif file %s: %v", filePath, err)
	}
	defer func() { _ = f.Close() }()

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)

	if err := mw.WriteField("logbook_uuid", logbookUUID); err != nil {
		t.Fatalf("write field: %v", err)
	}

	part, err := mw.CreateFormFile("file", filepath.Base(filePath))
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := io.Copy(part, f); err != nil {
		t.Fatalf("copy file: %v", err)
	}
	_ = mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/v1/import/adif", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	setTestAuthHeader(t, req, userID)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	var env apiEnvelope
	if strings.Contains(rec.Header().Get("Content-Type"), "application/json") && rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
			t.Fatalf("decode api envelope: %v body=%s", err, rec.Body.String())
		}
	}
	return rec.Code, env
}
