package handler_test

import (
	"context"
	"fmt"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
	"strings"
	"testing"
	"time"
)

type activationPayload struct {
	UUID           string `json:"uuid"`
	Reference      string `json:"reference"`
	Program        string `json:"program"`
	ActivationDate string `json:"activation_date"`
}

type activationStatusPayload struct {
	Program         string   `json:"program"`
	Reference       string   `json:"reference"`
	QSOCount        int64    `json:"qso_count"`
	UniqueCallsigns int64    `json:"unique_callsigns"`
	MinimumContacts int64    `json:"minimum_contacts"`
	MissingFields   []string `json:"missing_required_fields"`
	ReadyToSubmit   bool     `json:"ready_to_submit"`
}

type activationExportPayload struct {
	Filename           string   `json:"filename"`
	ADIF               string   `json:"adif"`
	UniqueCallsigns    int64    `json:"unique_callsigns"`
	ValidationWarnings []string `json:"validation_warnings"`
}

func createActivationTestLogbook(t *testing.T, pool *pgxpool.Pool, userID int64, name string) string {
	t.Helper()
	var logbookID int64
	var logbookUUID string
	err := pool.QueryRow(context.Background(), `
		INSERT INTO logbooks (user_id, name, callsign, is_default)
		VALUES ($1, $2, 'W1ABC', TRUE)
		RETURNING id, uuid::text
	`, userID, name).Scan(&logbookID, &logbookUUID)
	if err != nil {
		t.Fatalf("insert logbook: %v", err)
	}

	if _, err := pool.Exec(context.Background(), `
		INSERT INTO user_roles (logbook_id, user_id, role, invited_by)
		VALUES ($1, $2, 'owner', $2)
		ON CONFLICT (logbook_id, user_id)
		DO UPDATE SET role = 'owner', invited_by = EXCLUDED.invited_by, updated_at = NOW()
	`, logbookID, userID); err != nil {
		t.Fatalf("ensure user role: %v", err)
	}

	return logbookUUID
}

func TestIntegration_POTAActivationStatusCountsUniqueCallsigns(t *testing.T) {
	pool, h := setupIntegration(t)
	user := createTestUser(t, pool, "pota-activation-status")
	logbookUUID := createActivationTestLogbook(t, pool, user.ID, "POTA Log")

	activationDate := time.Now().UTC().Format("2006-01-02")
	status, env := doJSON(t, h, http.MethodPost, "/v1/activations/pota", user.ID, map[string]any{
		"reference":       "K-1234",
		"activation_date": activationDate,
		"logbook_uuid":    logbookUUID,
	})
	if status != http.StatusCreated || !env.Success {
		t.Fatalf("create activation failed: status=%d success=%v error=%q", status, env.Success, env.Error)
	}

	var created activationPayload
	decodeData(t, env.Data, &created)
	if created.UUID == "" {
		t.Fatal("expected activation UUID")
	}

	var logbookID int64
	if err := pool.QueryRow(context.Background(), `SELECT id FROM logbooks WHERE uuid = $1`, logbookUUID).Scan(&logbookID); err != nil {
		t.Fatalf("resolve logbook id: %v", err)
	}

	baseTime := time.Now().UTC().Truncate(time.Second)
	callsigns := []string{"K1AAA", "K1BBB", "K1CCC", "K1DDD", "K1EEE", "K1FFF", "K1GGG", "K1HHH", "K1III", "K1JJJ", "K1AAA"}
	for idx, callsign := range callsigns {
		_, err := pool.Exec(context.Background(), `
			INSERT INTO qsos (
				logbook_id,
				created_by_user_id,
				callsign,
				band,
				mode,
				datetime_on,
				my_pota_refs,
				station_callsign,
				my_gridsquare
			) VALUES ($1, $2, $3, '20m', 'SSB', $4, ARRAY['K-1234'], 'W1ABC', 'EM10')
		`, logbookID, user.ID, callsign, baseTime.Add(time.Duration(idx)*time.Minute))
		if err != nil {
			t.Fatalf("insert qso %d: %v", idx, err)
		}
	}

	status, env = doJSON(t, h, http.MethodGet, "/v1/activations/pota/"+created.UUID+"/status", user.ID, nil)
	if status != http.StatusOK || !env.Success {
		t.Fatalf("status endpoint failed: status=%d success=%v error=%q", status, env.Success, env.Error)
	}

	var validation activationStatusPayload
	decodeData(t, env.Data, &validation)
	if validation.UniqueCallsigns != 10 {
		t.Fatalf("expected 10 unique callsigns, got %d", validation.UniqueCallsigns)
	}
	if validation.MinimumContacts != 10 {
		t.Fatalf("expected minimum_contacts=10, got %d", validation.MinimumContacts)
	}
	if !validation.ReadyToSubmit {
		t.Fatalf("expected activation ready_to_submit=true, got false (missing=%v)", validation.MissingFields)
	}
}

func TestIntegration_POTAActivationExportAddsMySigFields(t *testing.T) {
	pool, h := setupIntegration(t)
	user := createTestUser(t, pool, "pota-export")
	logbookUUID := createActivationTestLogbook(t, pool, user.ID, "POTA Export Log")

	activationDate := time.Now().UTC().Format("2006-01-02")
	status, env := doJSON(t, h, http.MethodPost, "/v1/activations/pota", user.ID, map[string]any{
		"reference":       "K-5678",
		"activation_date": activationDate,
		"logbook_uuid":    logbookUUID,
	})
	if status != http.StatusCreated || !env.Success {
		t.Fatalf("create activation failed: status=%d success=%v error=%q", status, env.Success, env.Error)
	}

	var created activationPayload
	decodeData(t, env.Data, &created)

	var logbookID int64
	if err := pool.QueryRow(context.Background(), `SELECT id FROM logbooks WHERE uuid = $1`, logbookUUID).Scan(&logbookID); err != nil {
		t.Fatalf("resolve logbook id: %v", err)
	}

	for idx, callsign := range []string{"W1AW", "K7ABC"} {
		_, err := pool.Exec(context.Background(), `
			INSERT INTO qsos (
				logbook_id,
				created_by_user_id,
				callsign,
				band,
				mode,
				datetime_on,
				my_pota_refs,
				station_callsign,
				my_gridsquare,
				rst_sent,
				rst_rcvd
			) VALUES ($1, $2, $3, '20m', 'SSB', $4, ARRAY['K-5678'], 'W1ABC', 'EM10', '59', '59')
		`, logbookID, user.ID, callsign, time.Now().UTC().Add(time.Duration(idx)*time.Minute))
		if err != nil {
			t.Fatalf("insert qso %d: %v", idx, err)
		}
	}

	status, env = doJSON(t, h, http.MethodPost, "/v1/activations/pota/"+created.UUID+"/export", user.ID, map[string]any{})
	if status != http.StatusOK || !env.Success {
		t.Fatalf("export endpoint failed: status=%d success=%v error=%q", status, env.Success, env.Error)
	}

	var exportPayload activationExportPayload
	decodeData(t, env.Data, &exportPayload)
	if exportPayload.Filename == "" {
		t.Fatal("expected export filename")
	}
	if strings.Count(exportPayload.ADIF, "<MY_SIG:4>POTA") < 2 {
		t.Fatalf("expected MY_SIG=POTA in every record, adif=%s", exportPayload.ADIF)
	}
	if strings.Count(exportPayload.ADIF, "<MY_SIG_INFO:6>K-5678") < 2 {
		t.Fatalf("expected MY_SIG_INFO=K-5678 in every record, adif=%s", exportPayload.ADIF)
	}
	if !strings.Contains(exportPayload.ADIF, "STATION_CALLSIGN") {
		t.Fatalf("expected STATION_CALLSIGN field in export, adif=%s", exportPayload.ADIF)
	}
	if !strings.Contains(exportPayload.ADIF, "MY_GRIDSQUARE") {
		t.Fatalf("expected MY_GRIDSQUARE field in export, adif=%s", exportPayload.ADIF)
	}
}

func TestIntegration_POTAActivationRejectsInvalidReference(t *testing.T) {
	pool, h := setupIntegration(t)
	user := createTestUser(t, pool, "pota-invalid-ref")
	logbookUUID := createActivationTestLogbook(t, pool, user.ID, "POTA Invalid")

	status, env := doJSON(t, h, http.MethodPost, "/v1/activations/pota", user.ID, map[string]any{
		"reference":       "BADREF",
		"activation_date": time.Now().UTC().Format("2006-01-02"),
		"logbook_uuid":    logbookUUID,
	})
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid ref, got %d", status)
	}
	if env.Success {
		t.Fatalf("expected success=false for invalid ref, got true")
	}
	if !strings.Contains(strings.ToLower(env.Error), "invalid") {
		t.Fatalf("expected validation error message, got %q", env.Error)
	}
}

func TestIntegration_POTAAwardsSummaryIncludesActivatedAndHunted(t *testing.T) {
	pool, h := setupIntegration(t)
	user := createTestUser(t, pool, "pota-awards")
	logbookUUID := createActivationTestLogbook(t, pool, user.ID, "POTA Awards")

	activationDate := time.Now().UTC().Format("2006-01-02")
	status, env := doJSON(t, h, http.MethodPost, "/v1/activations/pota", user.ID, map[string]any{
		"reference":       "K-9012",
		"activation_date": activationDate,
		"logbook_uuid":    logbookUUID,
	})
	if status != http.StatusCreated || !env.Success {
		t.Fatalf("create activation failed: status=%d success=%v error=%q", status, env.Success, env.Error)
	}
	var logbookID int64
	if err := pool.QueryRow(context.Background(), `SELECT id FROM logbooks WHERE uuid = $1`, logbookUUID).Scan(&logbookID); err != nil {
		t.Fatalf("resolve logbook id: %v", err)
	}

	for i := 0; i < 10; i++ {
		callsign := fmt.Sprintf("K1A%02d", i)
		_, err := pool.Exec(context.Background(), `
			INSERT INTO qsos (
				logbook_id,
				created_by_user_id,
				callsign,
				band,
				mode,
				datetime_on,
				my_pota_refs,
				station_callsign,
				my_gridsquare,
				sig,
				sig_info
			) VALUES ($1, $2, $3, '20m', 'SSB', $4, ARRAY['K-9012'], 'W1ABC', 'EM10', 'POTA', 'K-1111')
		`, logbookID, user.ID, callsign, time.Now().UTC().Add(time.Duration(i)*time.Minute))
		if err != nil {
			t.Fatalf("insert qso %d: %v", i, err)
		}
	}

	status, env = doJSON(t, h, http.MethodGet, "/v1/awards/pota", user.ID, nil)
	if status != http.StatusOK || !env.Success {
		t.Fatalf("awards endpoint failed: status=%d success=%v error=%q", status, env.Success, env.Error)
	}

	var payload struct {
		ParksActivated   int64 `json:"parks_activated"`
		ParksHunted      int64 `json:"parks_hunted"`
		ActivationsTotal int64 `json:"activations_total"`
		ValidActivations int64 `json:"valid_activations"`
	}
	decodeData(t, env.Data, &payload)

	if payload.ParksActivated < 1 {
		t.Fatalf("expected parks_activated >= 1, got %d", payload.ParksActivated)
	}
	if payload.ParksHunted < 1 {
		t.Fatalf("expected parks_hunted >= 1, got %d", payload.ParksHunted)
	}
	if payload.ActivationsTotal < 1 {
		t.Fatalf("expected activations_total >= 1, got %d", payload.ActivationsTotal)
	}
	if payload.ValidActivations < 1 {
		t.Fatalf("expected valid_activations >= 1, got %d", payload.ValidActivations)
	}
}
