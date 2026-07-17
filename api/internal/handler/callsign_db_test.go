package handler_test

// Integration tests for the FCC callsign database endpoints.
//
// Tests cover:
//   - GET /v1/callsign/{call}         — 404 for unknown, 200 for seeded record
//   - GET /v1/callsign/search?q=...   — prefix match, empty results
//   - GET /v1/callsign/{call}/profile — unclaimed stub, upsert+fetch
//   - PUT /v1/callsign/{call}/profile — auth required, owns profile, foreign claim rejected

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

// (seedCallsignRecord and deleteCallsignRecord use pool directly in each test
// via pool.Exec to avoid interface complexity.)

// ─────────────────────────────────────────────────────────────────────────────
// GET /v1/callsign/{call}
// ─────────────────────────────────────────────────────────────────────────────

func TestCallsignDB_Lookup_NotFound(t *testing.T) {
	_, srv := setupIntegration(t)

	req := httptest.NewRequest(http.MethodGet, "/v1/callsign/ZZZZZ999", nil)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("status: got %d, want 404", rr.Code)
	}

	var env apiEnvelope
	if err := json.NewDecoder(rr.Body).Decode(&env); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if env.Success {
		t.Error("expected success=false for not found")
	}
}

func TestCallsignDB_Lookup_Found(t *testing.T) {
	pool, srv := setupIntegration(t)

	call := fmt.Sprintf("WX%dT", time.Now().UnixNano()%10000)
	_, err := pool.Exec(context.Background(), `
		INSERT INTO callsign_records
			(callsign, source, full_name, first_name, last_name,
			 city, state_province, country, status, license_class,
			 fetched_at, updated_at)
		VALUES ($1, 'fcc', 'Test Ham', 'Test', 'Ham', 'Testville', 'TX', 'US',
		        'active', 'extra', now(), now())
		ON CONFLICT (callsign, source) DO UPDATE SET updated_at = now()
	`, call)
	if err != nil {
		t.Fatalf("seed record: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM callsign_records WHERE callsign = $1`, call)
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/callsign/"+call, nil)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200 (body: %s)", rr.Code, rr.Body.String())
	}

	var env apiEnvelope
	if err := json.NewDecoder(rr.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !env.Success {
		t.Errorf("success=false: %s", env.Error)
	}

	var data map[string]any
	if err := json.Unmarshal(env.Data, &data); err != nil {
		t.Fatalf("decode data: %v", err)
	}
	if data["callsign"] != call {
		t.Errorf("callsign: got %v, want %s", data["callsign"], call)
	}
	if data["status"] != "active" {
		t.Errorf("status: got %v, want active", data["status"])
	}
	if data["license_class"] != "extra" {
		t.Errorf("license_class: got %v, want extra", data["license_class"])
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// GET /v1/callsign/search
// ─────────────────────────────────────────────────────────────────────────────

func TestCallsignDB_Search_EmptyQ(t *testing.T) {
	_, srv := setupIntegration(t)

	req := httptest.NewRequest(http.MethodGet, "/v1/callsign/search", nil)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want 400", rr.Code)
	}
}

func TestCallsignDB_Search_PrefixMatch(t *testing.T) {
	pool, srv := setupIntegration(t)

	// Seed a record with a distinctive prefix.
	prefix := fmt.Sprintf("KJ%d", time.Now().UnixNano()%10000)
	call := prefix + "X"
	_, err := pool.Exec(context.Background(), `
		INSERT INTO callsign_records
			(callsign, source, full_name, first_name, last_name,
			 city, state_province, country, status, license_class,
			 fetched_at, updated_at)
		VALUES ($1, 'fcc', 'Search Ham', 'Search', 'Ham', 'Searchville', 'CA', 'US',
		        'active', 'general', now(), now())
		ON CONFLICT (callsign, source) DO NOTHING
	`, call)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM callsign_records WHERE callsign = $1`, call)
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/callsign/search?q="+prefix, nil)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200 (body: %s)", rr.Code, rr.Body.String())
	}

	var env apiEnvelope
	if err := json.NewDecoder(rr.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !env.Success {
		t.Errorf("success=false: %s", env.Error)
	}

	var data struct {
		Results []map[string]any `json:"results"`
		Count   int              `json:"count"`
	}
	if err := json.Unmarshal(env.Data, &data); err != nil {
		t.Fatalf("decode data: %v", err)
	}
	if data.Count == 0 {
		t.Error("expected at least one search result")
	}
	// Verify our seeded record is in results.
	found := false
	for _, r := range data.Results {
		if r["callsign"] == call {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("seeded callsign %s not found in results", call)
	}
}

func TestCallsignDB_Search_NoResults(t *testing.T) {
	_, srv := setupIntegration(t)

	// A query that will match nothing.
	req := httptest.NewRequest(http.MethodGet, "/v1/callsign/search?q=ZZZZZNOTREAL99999", nil)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200", rr.Code)
	}

	var env apiEnvelope
	if err := json.NewDecoder(rr.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}

	var data struct {
		Results []any `json:"results"`
		Count   int   `json:"count"`
	}
	if err := json.Unmarshal(env.Data, &data); err != nil {
		t.Fatalf("decode data: %v", err)
	}
	if data.Count != 0 {
		t.Errorf("expected 0 results, got %d", data.Count)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// GET /v1/callsign/{call}/profile
// ─────────────────────────────────────────────────────────────────────────────

func TestCallsignDB_GetProfile_Unclaimed(t *testing.T) {
	_, srv := setupIntegration(t)

	// A callsign with no profile — returns unclaimed stub.
	req := httptest.NewRequest(http.MethodGet, "/v1/callsign/ZZZUNKNOWN/profile", nil)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200", rr.Code)
	}

	var env apiEnvelope
	if err := json.NewDecoder(rr.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !env.Success {
		t.Errorf("success=false: %s", env.Error)
	}

	var data map[string]any
	if err := json.Unmarshal(env.Data, &data); err != nil {
		t.Fatalf("decode data: %v", err)
	}
	if data["on_radioledger"] != false {
		t.Errorf("on_radioledger: got %v, want false", data["on_radioledger"])
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// PUT /v1/callsign/{call}/profile
// ─────────────────────────────────────────────────────────────────────────────

func TestCallsignDB_UpdateProfile_RequiresAuth(t *testing.T) {
	_, srv := setupIntegration(t)

	body := `{"bio":"Test bio"}`
	req := httptest.NewRequest(http.MethodPut, "/v1/callsign/W1TEST/profile",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want 401", rr.Code)
	}
}

func TestCallsignDB_UpdateProfile_AuthedUser(t *testing.T) {
	pool, srv := setupIntegration(t)

	user := createTestUser(t, pool, "profiletest")
	call := fmt.Sprintf("KG%dP", time.Now().UnixNano()%10000)

	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM operator_profiles WHERE callsign = $1`, call)
	})

	statusPut, envPut := doJSON(t, srv, http.MethodPut,
		"/v1/callsign/"+call+"/profile", user.ID, map[string]any{
			"bio":         "Testing RadioLedger profiles",
			"qsl_via":     "radioledger",
			"grid_square": "EM35",
			"antennas":    []string{"OCF-EFHW", "Vertical"},
			"rigs":        []string{"IC-7300"},
		})
	if statusPut != http.StatusOK {
		t.Errorf("status: got %d, want 200 (body: %s)", statusPut, string(envPut.Error))
	}
	if !envPut.Success {
		t.Errorf("success=false: %s", envPut.Error)
	}

	var data map[string]any
	if err := json.Unmarshal(envPut.Data, &data); err != nil {
		t.Fatalf("decode data: %v", err)
	}
	if data["bio"] != "Testing RadioLedger profiles" {
		t.Errorf("bio: got %v, want 'Testing RadioLedger profiles'", data["bio"])
	}
	if data["qsl_via"] != "radioledger" {
		t.Errorf("qsl_via: got %v", data["qsl_via"])
	}
	if data["on_radioledger"] != true {
		t.Errorf("on_radioledger: got %v, want true", data["on_radioledger"])
	}
}

func TestCallsignDB_UpdateProfile_CannotClaimOthers(t *testing.T) {
	pool, srv := setupIntegration(t)

	// Create two users and have user1 claim a callsign.
	user1 := createTestUser(t, pool, "profileowner")
	user2 := createTestUser(t, pool, "profilestealer")
	call := fmt.Sprintf("KG%dQ", time.Now().UnixNano()%10000)

	// User1 claims the profile.
	_, err := pool.Exec(context.Background(), `
		INSERT INTO operator_profiles (callsign, user_id, created_at, updated_at)
		VALUES ($1, $2, now(), now())
		ON CONFLICT (callsign) DO UPDATE SET user_id = EXCLUDED.user_id
	`, call, user1.ID)
	if err != nil {
		t.Fatalf("seed profile: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM operator_profiles WHERE callsign = $1`, call)
	})

	// User2 tries to update it — should be forbidden.
	statusForbid, _ := doJSON(t, srv, http.MethodPut,
		"/v1/callsign/"+call+"/profile", user2.ID, map[string]any{
			"bio": "Stealing this profile",
		})
	if statusForbid != http.StatusForbidden {
		t.Errorf("status: got %d, want 403", statusForbid)
	}
}
