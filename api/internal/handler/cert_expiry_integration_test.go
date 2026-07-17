package handler_test

// Integration tests for:
//   POST /v1/desktop/cert-expiry      (DesktopHandler.CertExpiry)
//   GET  /v1/notifications            (NotificationHandler.List)
//   PUT  /v1/notifications/:id/read   (NotificationHandler.MarkRead)
//
// These tests require a live PostgreSQL database and are skipped when
// RADIOLEDGER_TEST_DATABASE_URL is not set (or the DB cannot be reached).

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
)

// locationCertPayload is a minimal representation of a station location with cert expiry.
type locationCertPayload struct {
	UUID           string  `json:"uuid"`
	Callsign       string  `json:"callsign"`
	Name           string  `json:"name"`
	LoTWCertExpiry *string `json:"lotw_cert_expiry,omitempty"`
}

// TestIntegration_CertExpiryPush verifies the full desktop-to-server cert expiry push flow.
func TestIntegration_CertExpiryPush(t *testing.T) {
	pool, h := setupIntegration(t)
	user := createTestUser(t, pool, "certexpiry")

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = pool.Exec(ctx, `DELETE FROM station_locations WHERE user_id = $1`, user.ID)
	})

	// Create a station location so there is a callsign to match.
	createStatus, createEnv := doJSON(t, h, http.MethodPost, "/v1/locations", user.ID, map[string]any{
		"name":        "Home Station",
		"callsign":    "W1TEST",
		"grid_square": "FN31",
		"is_default":  true,
	})
	if createStatus != http.StatusCreated || !createEnv.Success {
		t.Fatalf("create location failed: status=%d success=%v error=%q",
			createStatus, createEnv.Success, createEnv.Error)
	}
	var loc locationCertPayload
	decodeData(t, createEnv.Data, &loc)
	if loc.UUID == "" {
		t.Fatal("expected location uuid in response")
	}

	// Push cert expiry.
	expiryDate := time.Now().AddDate(0, 0, 25).Format("2006-01-02")
	pushStatus, pushEnv := doJSON(t, h, http.MethodPost, "/v1/desktop/cert-expiry", user.ID, map[string]any{
		"station_callsign": "W1TEST",
		"expires_at":       expiryDate,
	})
	if pushStatus != http.StatusOK || !pushEnv.Success {
		t.Fatalf("cert expiry push failed: status=%d success=%v error=%q",
			pushStatus, pushEnv.Success, pushEnv.Error)
	}

	type certExpiryResp struct {
		Callsign       string `json:"callsign"`
		ExpiresAt      string `json:"expires_at"`
		LocationsFound int64  `json:"locations_found"`
	}
	var resp certExpiryResp
	decodeData(t, pushEnv.Data, &resp)
	if resp.Callsign != "W1TEST" {
		t.Errorf("expected callsign W1TEST, got %q", resp.Callsign)
	}
	if resp.ExpiresAt != expiryDate {
		t.Errorf("expected expires_at %q, got %q", expiryDate, resp.ExpiresAt)
	}
	if resp.LocationsFound != 1 {
		t.Errorf("expected locations_found=1, got %d", resp.LocationsFound)
	}

	// Verify the update persisted by fetching the location.
	getStatus, getEnv := doJSON(t, h, http.MethodGet, "/v1/locations/"+loc.UUID, user.ID, nil)
	if getStatus != http.StatusOK || !getEnv.Success {
		t.Fatalf("get location failed: status=%d success=%v", getStatus, getEnv.Success)
	}
	var updatedLoc locationCertPayload
	decodeData(t, getEnv.Data, &updatedLoc)
	if updatedLoc.LoTWCertExpiry == nil || *updatedLoc.LoTWCertExpiry != expiryDate {
		t.Errorf("expected lotw_cert_expiry=%q after push, got %v", expiryDate, updatedLoc.LoTWCertExpiry)
	}
}

// TestIntegration_CertExpiryPush_UnknownCallsign verifies that pushing an expiry
// for a callsign with no station_location returns 200 with locations_found=0.
func TestIntegration_CertExpiryPush_UnknownCallsign(t *testing.T) {
	pool, h := setupIntegration(t)
	user := createTestUser(t, pool, "certexpiry-unknown")

	pushStatus, pushEnv := doJSON(t, h, http.MethodPost, "/v1/desktop/cert-expiry", user.ID, map[string]any{
		"station_callsign": "XZ9NOTEXIST",
		"expires_at":       time.Now().AddDate(1, 0, 0).Format("2006-01-02"),
	})
	if pushStatus != http.StatusOK || !pushEnv.Success {
		t.Fatalf("expected 200 success for unknown callsign, got status=%d success=%v",
			pushStatus, pushEnv.Success)
	}

	type certExpiryResp struct {
		LocationsFound int64 `json:"locations_found"`
	}
	var resp certExpiryResp
	decodeData(t, pushEnv.Data, &resp)
	if resp.LocationsFound != 0 {
		t.Errorf("expected locations_found=0 for unknown callsign, got %d", resp.LocationsFound)
	}
}

// TestIntegration_CertExpiryPush_Validation tests input validation.
func TestIntegration_CertExpiryPush_Validation(t *testing.T) {
	pool, h := setupIntegration(t)
	user := createTestUser(t, pool, "certexpiry-val")

	tests := []struct {
		name       string
		userID     int64
		body       map[string]any
		wantStatus int
	}{
		{
			name:       "missing station_callsign",
			userID:     user.ID,
			body:       map[string]any{"expires_at": "2027-01-01"},
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name:       "missing expires_at",
			userID:     user.ID,
			body:       map[string]any{"station_callsign": "W1ABC"},
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name:       "invalid expires_at format",
			userID:     user.ID,
			body:       map[string]any{"station_callsign": "W1ABC", "expires_at": "01/01/2027"},
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name:       "unauthenticated",
			userID:     0,
			body:       map[string]any{"station_callsign": "W1ABC", "expires_at": "2027-01-01"},
			wantStatus: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, env := doJSON(t, h, http.MethodPost, "/v1/desktop/cert-expiry", tt.userID, tt.body)
			if status != tt.wantStatus {
				t.Errorf("expected status %d, got %d (success=%v error=%q)",
					tt.wantStatus, status, env.Success, env.Error)
			}
		})
	}
}

// TestIntegration_NotificationsListAndRead verifies GET /v1/notifications and
// PUT /v1/notifications/:id/read using a directly-inserted cert_expiry notification.
func TestIntegration_NotificationsListAndRead(t *testing.T) {
	pool, h := setupIntegration(t)
	user := createTestUser(t, pool, "notif-read")

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = pool.Exec(ctx, `DELETE FROM notifications WHERE user_id = $1`, user.ID)
	})

	// Insert a cert_expiry notification directly (simulating the daily job output).
	payload := map[string]any{
		"callsign":       "W1NOTIF",
		"location_name":  "Test Location",
		"expires_at":     "2026-04-01",
		"days_remaining": 30,
		"threshold_days": "30",
		"renewal_url":    "https://lotw.arrl.org/lotw-help/creating-a-certificate/",
	}
	payloadJSON, _ := json.Marshal(payload)
	var notifUUID uuid.UUID
	err := pool.QueryRow(context.Background(),
		`INSERT INTO notifications (user_id, type, payload)
		 VALUES ($1, 'cert_expiry', $2::jsonb)
		 RETURNING uuid`,
		user.ID, string(payloadJSON),
	).Scan(&notifUUID)
	if err != nil {
		t.Fatalf("insert test notification: %v", err)
	}

	// List notifications — should include the one we just inserted.
	listStatus, listEnv := doJSON(t, h, http.MethodGet, "/v1/notifications", user.ID, nil)
	if listStatus != http.StatusOK || !listEnv.Success {
		t.Fatalf("list notifications failed: status=%d success=%v error=%q",
			listStatus, listEnv.Success, listEnv.Error)
	}
	assertNoIDKey(t, listEnv.Data)

	var notifList notificationListPayload
	decodeData(t, listEnv.Data, &notifList)
	if notifList.Count == 0 {
		t.Fatal("expected at least one notification in list")
	}

	var found *notificationItem
	for i := range notifList.Items {
		if notifList.Items[i].UUID == notifUUID.String() {
			found = &notifList.Items[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("notification %s not found in list", notifUUID)
	}
	if found.Type != "cert_expiry" {
		t.Errorf("expected type cert_expiry, got %q", found.Type)
	}
	if found.IsRead {
		t.Error("notification should be unread initially")
	}
	callsignVal, _ := found.Payload["callsign"].(string)
	if callsignVal != "W1NOTIF" {
		t.Errorf("unexpected callsign in payload: %v", found.Payload["callsign"])
	}

	// Mark the notification as read.
	readPath := fmt.Sprintf("/v1/notifications/%s/read", notifUUID)
	readStatus, readEnv := doJSON(t, h, http.MethodPut, readPath, user.ID, nil)
	if readStatus != http.StatusOK || !readEnv.Success {
		t.Fatalf("mark notification read failed: status=%d success=%v error=%q",
			readStatus, readEnv.Success, readEnv.Error)
	}

	// Re-fetch with unread filter and confirm the notification is no longer included.
	listStatus2, listEnv2 := doJSON(t, h, http.MethodGet,
		"/v1/notifications?unread=true", user.ID, nil)
	if listStatus2 != http.StatusOK || !listEnv2.Success {
		t.Fatalf("list unread notifications failed: status=%d success=%v", listStatus2, listEnv2.Success)
	}

	var unreadList notificationListPayload
	decodeData(t, listEnv2.Data, &unreadList)
	for _, item := range unreadList.Items {
		if item.UUID == notifUUID.String() {
			t.Errorf("notification %s should not appear in unread list after being marked read", notifUUID)
		}
	}
}
