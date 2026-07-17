package handler_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// confirmationPayload represents a single item from the confirmations list.
type confirmationPayload struct {
	ID            int64   `json:"id"`
	OurCallsign   string  `json:"our_callsign"`
	TheirCallsign string  `json:"their_callsign"`
	Band          string  `json:"band"`
	Mode          string  `json:"mode"`
	Status        string  `json:"status"`
}

type confirmationListPayload struct {
	Items []confirmationPayload `json:"items"`
}

type confirmationStatsPayload struct {
	Total       int64   `json:"total"`
	Confirmed   int64   `json:"confirmed"`
	Matched     int64   `json:"matched"`
	Unconfirmed int64   `json:"unconfirmed"`
	Rate        float64 `json:"confirmation_rate"`
}

// createQSOWithConfirmation directly inserts a qso_confirmations row for testing,
// since the River worker runs async and isn't available in integration tests.
func createQSOConfirmationRow(t *testing.T, pool *pgxpool.Pool, qsoID int64, ourCallsign, theirCallsign, band, mode, status string, matchedQSOID *int64) int64 {
	t.Helper()
	var id int64
	dt := time.Now().UTC()
	err := pool.QueryRow(context.Background(), `
		INSERT INTO qso_confirmations (
			qso_id, matched_qso_id,
			our_callsign, their_callsign,
			band, mode, qso_date, qso_time,
			status
		) VALUES ($1, $2, $3, $4, $5, $6, ($7 AT TIME ZONE 'UTC')::date, ($7 AT TIME ZONE 'UTC')::time, $8)
		RETURNING id
	`, qsoID, matchedQSOID, ourCallsign, theirCallsign, band, mode, dt, status).Scan(&id)
	if err != nil {
		t.Fatalf("insert confirmation row: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM qso_confirmations WHERE id = $1`, id)
	})
	return id
}

func TestIntegration_ConfirmationsList(t *testing.T) {
	pool, h := setupIntegration(t)
	user := createTestUser(t, pool, "conf-list")

	lbUUID := createLogbookViaAPI(t, h, user.ID, "Test Logbook", true)

	// Create a QSO
	qso := createQSOViaAPI(t, h, user.ID, lbUUID, map[string]any{
		"callsign":    "W1ABC",
		"band":        "20m",
		"mode":        "FT8",
		"datetime_on": time.Now().UTC().Format(time.RFC3339),
	})

	// Look up the internal QSO ID
	var qsoID int64
	err := pool.QueryRow(context.Background(),
		`SELECT q.id FROM qsos q JOIN logbooks lb ON lb.id = q.logbook_id WHERE q.uuid = $1`,
		qso.UUID).Scan(&qsoID)
	if err != nil {
		t.Fatalf("look up qso id: %v", err)
	}

	// Create a confirmation record manually
	createQSOConfirmationRow(t, pool, qsoID, "KI5BRG", "W1ABC", "20m", "FT8", "unconfirmed", nil)

	// List confirmations
	status, env := doJSON(t, h, http.MethodGet, "/v1/confirmations", user.ID, nil)
	if status != http.StatusOK || !env.Success {
		t.Fatalf("list confirmations: status=%d success=%v error=%q", status, env.Success, env.Error)
	}

	var listData confirmationListPayload
	decodeData(t, env.Data, &listData)
	if len(listData.Items) == 0 {
		t.Fatal("expected at least one confirmation, got none")
	}

	found := false
	for _, item := range listData.Items {
		if item.TheirCallsign == "W1ABC" && item.Band == "20m" {
			found = true
			if item.Status != "unconfirmed" {
				t.Errorf("expected status=unconfirmed, got %q", item.Status)
			}
		}
	}
	if !found {
		t.Error("expected to find confirmation for W1ABC on 20m FT8")
	}
}

func TestIntegration_ConfirmationsPending(t *testing.T) {
	pool, h := setupIntegration(t)
	user := createTestUser(t, pool, "conf-pending")

	lbUUID := createLogbookViaAPI(t, h, user.ID, "Pending Test", true)

	qso := createQSOViaAPI(t, h, user.ID, lbUUID, map[string]any{
		"callsign":    "W2XYZ",
		"band":        "40m",
		"mode":        "SSB",
		"datetime_on": time.Now().UTC().Format(time.RFC3339),
	})

	var qsoID int64
	err := pool.QueryRow(context.Background(),
		`SELECT q.id FROM qsos q JOIN logbooks lb ON lb.id = q.logbook_id WHERE q.uuid = $1`,
		qso.UUID).Scan(&qsoID)
	if err != nil {
		t.Fatalf("look up qso id: %v", err)
	}

	// Create another user's QSO and link them
	user2 := createTestUser(t, pool, "conf-pending-2")
	lbUUID2 := createLogbookViaAPI(t, h, user2.ID, "Other Logbook", true)
	qso2 := createQSOViaAPI(t, h, user2.ID, lbUUID2, map[string]any{
		"callsign":    "KI5TEST",
		"band":        "40m",
		"mode":        "SSB",
		"datetime_on": time.Now().UTC().Format(time.RFC3339),
	})
	var qsoID2 int64
	err = pool.QueryRow(context.Background(),
		`SELECT q.id FROM qsos q JOIN logbooks lb ON lb.id = q.logbook_id WHERE q.uuid = $1`,
		qso2.UUID).Scan(&qsoID2)
	if err != nil {
		t.Fatalf("look up qso2 id: %v", err)
	}

	// Create matched confirmation
	createQSOConfirmationRow(t, pool, qsoID, "KI5TEST", "W2XYZ", "40m", "SSB", "matched", &qsoID2)

	status, env := doJSON(t, h, http.MethodGet, "/v1/confirmations/pending", user.ID, nil)
	if status != http.StatusOK || !env.Success {
		t.Fatalf("pending confirmations: status=%d success=%v error=%q", status, env.Success, env.Error)
	}

	var listData confirmationListPayload
	decodeData(t, env.Data, &listData)
	if len(listData.Items) == 0 {
		t.Fatal("expected at least one pending confirmation")
	}
}

func TestIntegration_ConfirmationsStats(t *testing.T) {
	pool, h := setupIntegration(t)
	user := createTestUser(t, pool, "conf-stats")

	lbUUID := createLogbookViaAPI(t, h, user.ID, "Stats Test", true)
	qso := createQSOViaAPI(t, h, user.ID, lbUUID, map[string]any{
		"callsign":    "N5XYZ",
		"band":        "15m",
		"mode":        "CW",
		"datetime_on": time.Now().UTC().Format(time.RFC3339),
	})

	var qsoID int64
	_ = pool.QueryRow(context.Background(),
		`SELECT q.id FROM qsos q JOIN logbooks lb ON lb.id = q.logbook_id WHERE q.uuid = $1`,
		qso.UUID).Scan(&qsoID)

	createQSOConfirmationRow(t, pool, qsoID, "W5ABC", "N5XYZ", "15m", "CW", "confirmed", nil)

	status, env := doJSON(t, h, http.MethodGet, "/v1/confirmations/stats", user.ID, nil)
	if status != http.StatusOK || !env.Success {
		t.Fatalf("confirmation stats: status=%d success=%v error=%q", status, env.Success, env.Error)
	}

	var stats confirmationStatsPayload
	decodeData(t, env.Data, &stats)
	if stats.Total == 0 {
		t.Error("expected total > 0")
	}
}

func TestIntegration_ConfirmationConfirmAction(t *testing.T) {
	pool, h := setupIntegration(t)
	user := createTestUser(t, pool, "conf-confirm")

	lbUUID := createLogbookViaAPI(t, h, user.ID, "Confirm Action Test", true)
	qso := createQSOViaAPI(t, h, user.ID, lbUUID, map[string]any{
		"callsign":    "VK2ABC",
		"band":        "10m",
		"mode":        "FT8",
		"datetime_on": time.Now().UTC().Format(time.RFC3339),
	})

	var qsoID int64
	_ = pool.QueryRow(context.Background(),
		`SELECT q.id FROM qsos q JOIN logbooks lb ON lb.id = q.logbook_id WHERE q.uuid = $1`,
		qso.UUID).Scan(&qsoID)

	// Need another QSO to match against
	user2 := createTestUser(t, pool, "conf-confirm-2")
	lbUUID2 := createLogbookViaAPI(t, h, user2.ID, "Other", true)
	qso2 := createQSOViaAPI(t, h, user2.ID, lbUUID2, map[string]any{
		"callsign":    "KI5BRG",
		"band":        "10m",
		"mode":        "FT8",
		"datetime_on": time.Now().UTC().Format(time.RFC3339),
	})
	var qsoID2 int64
	_ = pool.QueryRow(context.Background(),
		`SELECT q.id FROM qsos q JOIN logbooks lb ON lb.id = q.logbook_id WHERE q.uuid = $1`,
		qso2.UUID).Scan(&qsoID2)

	confID := createQSOConfirmationRow(t, pool, qsoID, "KI5BRG", "VK2ABC", "10m", "FT8", "matched", &qsoID2)

	// Confirm it
	path := fmt.Sprintf("/v1/confirmations/%d/confirm", confID)
	status, env := doJSON(t, h, http.MethodPost, path, user.ID, nil)
	if status != http.StatusOK || !env.Success {
		t.Fatalf("confirm action: status=%d success=%v error=%q", status, env.Success, env.Error)
	}

	// Verify the status was updated
	var newStatus string
	_ = pool.QueryRow(context.Background(),
		`SELECT status FROM qso_confirmations WHERE id = $1`, confID).Scan(&newStatus)
	if newStatus != "confirmed" {
		t.Errorf("expected status=confirmed after confirm action, got %q", newStatus)
	}
}

func TestIntegration_ConfirmationRejectAction(t *testing.T) {
	pool, h := setupIntegration(t)
	user := createTestUser(t, pool, "conf-reject")

	lbUUID := createLogbookViaAPI(t, h, user.ID, "Reject Test", true)
	qso := createQSOViaAPI(t, h, user.ID, lbUUID, map[string]any{
		"callsign":    "DL5ABC",
		"band":        "17m",
		"mode":        "SSB",
		"datetime_on": time.Now().UTC().Format(time.RFC3339),
	})

	var qsoID int64
	_ = pool.QueryRow(context.Background(),
		`SELECT q.id FROM qsos q JOIN logbooks lb ON lb.id = q.logbook_id WHERE q.uuid = $1`,
		qso.UUID).Scan(&qsoID)

	user2 := createTestUser(t, pool, "conf-reject-2")
	lbUUID2 := createLogbookViaAPI(t, h, user2.ID, "Other", true)
	qso2 := createQSOViaAPI(t, h, user2.ID, lbUUID2, map[string]any{
		"callsign":    "W1TEST",
		"band":        "17m",
		"mode":        "SSB",
		"datetime_on": time.Now().UTC().Format(time.RFC3339),
	})
	var qsoID2 int64
	_ = pool.QueryRow(context.Background(),
		`SELECT q.id FROM qsos q JOIN logbooks lb ON lb.id = q.logbook_id WHERE q.uuid = $1`,
		qso2.UUID).Scan(&qsoID2)

	confID := createQSOConfirmationRow(t, pool, qsoID, "W1TEST", "DL5ABC", "17m", "SSB", "matched", &qsoID2)

	path := fmt.Sprintf("/v1/confirmations/%d/reject", confID)
	status, env := doJSON(t, h, http.MethodPost, path, user.ID, nil)
	if status != http.StatusOK || !env.Success {
		t.Fatalf("reject action: status=%d success=%v error=%q", status, env.Success, env.Error)
	}

	var newStatus string
	_ = pool.QueryRow(context.Background(),
		`SELECT status FROM qso_confirmations WHERE id = $1`, confID).Scan(&newStatus)
	if newStatus != "rejected" {
		t.Errorf("expected status=rejected, got %q", newStatus)
	}
}

func TestIntegration_ConfirmationFilterByStatus(t *testing.T) {
	pool, h := setupIntegration(t)
	user := createTestUser(t, pool, "conf-filter")

	lbUUID := createLogbookViaAPI(t, h, user.ID, "Filter Test", true)

	// Create QSOs with different confirmation statuses
	for _, tc := range []struct {
		callsign string
		band     string
		status   string
	}{
		{"AA1A", "20m", "unconfirmed"},
		{"BB2B", "40m", "matched"},
		{"CC3C", "15m", "confirmed"},
	} {
		qso := createQSOViaAPI(t, h, user.ID, lbUUID, map[string]any{
			"callsign":    tc.callsign,
			"band":        tc.band,
			"mode":        "FT8",
			"datetime_on": time.Now().UTC().Format(time.RFC3339),
		})
		var qsoID int64
		_ = pool.QueryRow(context.Background(),
			`SELECT q.id FROM qsos q JOIN logbooks lb ON lb.id = q.logbook_id WHERE q.uuid = $1`,
			qso.UUID).Scan(&qsoID)
		createQSOConfirmationRow(t, pool, qsoID, "W1FILTER", tc.callsign, tc.band, "FT8", tc.status, nil)
	}

	// Filter by status=confirmed
	status, env := doJSON(t, h, http.MethodGet, "/v1/confirmations?status=confirmed", user.ID, nil)
	if status != http.StatusOK || !env.Success {
		t.Fatalf("filter confirmations: status=%d success=%v error=%q", status, env.Success, env.Error)
	}

	var listData confirmationListPayload
	decodeData(t, env.Data, &listData)
	for _, item := range listData.Items {
		if item.Status != "confirmed" {
			t.Errorf("expected only confirmed items, got %q for callsign %q", item.Status, item.TheirCallsign)
		}
	}
}

func TestIntegration_ConfirmationUnauthorized(t *testing.T) {
	_, h := setupIntegration(t)

	// No auth
	status, env := doJSON(t, h, http.MethodGet, "/v1/confirmations", 0, nil)
	if status == http.StatusOK && env.Success {
		t.Fatal("expected unauthorized response, got success")
	}
}
