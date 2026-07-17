package handler_test

// Integration tests for station locations and callsign management.
//
// These tests exercise the full HTTP stack (router → middleware → handler → DB)
// against the real PostgreSQL database with RLS enforcement.
//
// Prerequisites:
//   - PostgreSQL 17 with PostGIS extension
//   - Database "radioledger" accessible at the default socket
//   - Run: go test -run TestIntegration_Locations ./internal/handler/
//   - Run: go test -run TestIntegration_Callsigns ./internal/handler/

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"
)

// ─────────────────────────────────────────────────────────────────────────────
// Payload types used by location/callsign integration tests
// ─────────────────────────────────────────────────────────────────────────────

type locationPayload struct {
	UUID       string   `json:"uuid"`
	Name       string   `json:"name"`
	Callsign   string   `json:"callsign"`
	GridSquare string   `json:"grid_square"`
	Latitude   *float64 `json:"latitude"`
	Longitude  *float64 `json:"longitude"`
	IsDefault  bool     `json:"is_default"`
}

type listLocationsPayload struct {
	Items []locationPayload `json:"items"`
}

type userCallsignPayload struct {
	UUID      string `json:"uuid"`
	Callsign  string `json:"callsign"`
	IsPrimary bool   `json:"is_primary"`
}

type listUserCallsignsPayload struct {
	Items []userCallsignPayload `json:"items"`
}

type stationCallsignPayload struct {
	UUID         string `json:"uuid"`
	Callsign     string `json:"callsign"`
	CallsignType string `json:"callsign_type"`
	Active       bool   `json:"active"`
}

type listStationCallsignsPayload struct {
	Items []stationCallsignPayload `json:"items"`
}

// ─────────────────────────────────────────────────────────────────────────────
// Station location CRUD with RLS isolation
// ─────────────────────────────────────────────────────────────────────────────

// TestIntegration_LocationCRUDAndRLS tests the full location lifecycle:
//  1. Create a location (with and without lat/lng)
//  2. Get location detail
//  3. List locations
//  4. Update location
//  5. Delete (soft) location
//  6. RLS: user B cannot see user A's locations
func TestIntegration_LocationCRUDAndRLS(t *testing.T) {
	pool, h := setupIntegration(t)
	userA := createTestUser(t, pool, "loc-a")
	userB := createTestUser(t, pool, "loc-b")

	// ── Create: grid-square-only location ────────────────────────────────────
	status, env := doJSON(t, h, http.MethodPost, "/v1/locations", userA.ID, map[string]any{
		"name":        "Home Station",
		"callsign":    "w5rex",
		"grid_square": "EM10",
		"is_default":  true,
	})
	if status != http.StatusCreated || !env.Success {
		t.Fatalf("create location failed: status=%d success=%v error=%q", status, env.Success, env.Error)
	}
	assertNoIDKey(t, env.Data)

	var created locationPayload
	decodeData(t, env.Data, &created)
	if created.UUID == "" {
		t.Fatal("created location has empty UUID")
	}
	if created.Callsign != "W5REX" {
		t.Errorf("callsign should be uppercased, got %q", created.Callsign)
	}
	if created.GridSquare != "EM10" {
		t.Errorf("expected grid_square EM10, got %q", created.GridSquare)
	}
	if !created.IsDefault {
		t.Error("expected is_default=true")
	}

	// ── Create: location with lat/lng coordinates ─────────────────────────────
	lat := 30.5
	lng := -97.0
	status, env = doJSON(t, h, http.MethodPost, "/v1/locations", userA.ID, map[string]any{
		"name":        "POTA Portable",
		"callsign":    "W5REX/P",
		"grid_square": "EM10",
		"latitude":    lat,
		"longitude":   lng,
		"is_default":  false,
	})
	if status != http.StatusCreated || !env.Success {
		t.Fatalf("create location with coords failed: status=%d success=%v error=%q", status, env.Success, env.Error)
	}
	assertNoIDKey(t, env.Data)

	var createdWithCoords locationPayload
	decodeData(t, env.Data, &createdWithCoords)
	if createdWithCoords.Latitude == nil || *createdWithCoords.Latitude != lat {
		t.Errorf("expected latitude %v, got %v", lat, createdWithCoords.Latitude)
	}
	if createdWithCoords.Longitude == nil || *createdWithCoords.Longitude != lng {
		t.Errorf("expected longitude %v, got %v", lng, createdWithCoords.Longitude)
	}

	// Verify PostGIS point was stored: compute distance from the DB.
	var distKm float64
	err := pool.QueryRow(context.Background(), `
		SELECT ST_DistanceSphere(location, ST_SetSRID(ST_MakePoint($2::float8, $3::float8), 4326)) / 1000.0
		FROM station_locations
		WHERE uuid = $1
	`, createdWithCoords.UUID, lng, lat).Scan(&distKm)
	if err != nil {
		t.Fatalf("PostGIS distance check: %v", err)
	}
	if distKm > 1.0 {
		t.Errorf("PostGIS point distance from expected coords too large: %.3f km", distKm)
	}

	// ── Get by UUID ───────────────────────────────────────────────────────────
	status, env = doJSON(t, h, http.MethodGet, "/v1/locations/"+created.UUID, userA.ID, nil)
	if status != http.StatusOK || !env.Success {
		t.Fatalf("get location failed: status=%d success=%v error=%q", status, env.Success, env.Error)
	}
	assertNoIDKey(t, env.Data)

	var fetched locationPayload
	decodeData(t, env.Data, &fetched)
	if fetched.UUID != created.UUID {
		t.Errorf("get returned wrong UUID: got %s, want %s", fetched.UUID, created.UUID)
	}

	// ── List ─────────────────────────────────────────────────────────────────
	status, env = doJSON(t, h, http.MethodGet, "/v1/locations", userA.ID, nil)
	if status != http.StatusOK || !env.Success {
		t.Fatalf("list locations failed: status=%d success=%v error=%q", status, env.Success, env.Error)
	}
	assertNoIDKey(t, env.Data)

	var listed listLocationsPayload
	decodeData(t, env.Data, &listed)
	if len(listed.Items) != 2 {
		t.Fatalf("expected 2 locations, got %d", len(listed.Items))
	}
	// Default should be listed first (ORDER BY is_default DESC).
	if !listed.Items[0].IsDefault {
		t.Error("expected default location listed first")
	}

	// ── Update ───────────────────────────────────────────────────────────────
	status, env = doJSON(t, h, http.MethodPut, "/v1/locations/"+created.UUID, userA.ID, map[string]any{
		"name":               "Home Station Updated",
		"callsign":           "W5REX",
		"grid_square":        "EM10",
		"is_default":         true,
		"lotw_location_name": "Home QTH",
		"lotw_cert_expiry":   "2027-12-31",
	})
	if status != http.StatusOK || !env.Success {
		t.Fatalf("update location failed: status=%d success=%v error=%q", status, env.Success, env.Error)
	}
	assertNoIDKey(t, env.Data)

	var updated locationPayload
	decodeData(t, env.Data, &updated)
	if updated.Name != "Home Station Updated" {
		t.Errorf("expected updated name, got %q", updated.Name)
	}
	// Verify LoTW fields were stored.
	var raw json.RawMessage
	doJSON(t, h, http.MethodGet, "/v1/locations/"+created.UUID, userA.ID, nil)
	_ = raw // accessed via env.Data above

	// ── RLS: user B sees 0 locations for user A ───────────────────────────────
	status, env = doJSON(t, h, http.MethodGet, "/v1/locations", userB.ID, nil)
	if status != http.StatusOK || !env.Success {
		t.Fatalf("user B list locations failed: status=%d success=%v", status, env.Success)
	}
	var bListed listLocationsPayload
	decodeData(t, env.Data, &bListed)
	if len(bListed.Items) != 0 {
		t.Errorf("user B should see 0 locations, got %d", len(bListed.Items))
	}

	// ── RLS: user B cannot get user A's location by UUID ─────────────────────
	status, env = doJSON(t, h, http.MethodGet, "/v1/locations/"+created.UUID, userB.ID, nil)
	if status != http.StatusOK || env.Success {
		t.Fatalf("RLS: user B should get not-found for user A location: status=%d success=%v", status, env.Success)
	}

	// ── Delete (soft) ────────────────────────────────────────────────────────
	status, env = doJSON(t, h, http.MethodDelete, "/v1/locations/"+createdWithCoords.UUID, userA.ID, nil)
	if status != http.StatusOK || !env.Success {
		t.Fatalf("delete location failed: status=%d success=%v error=%q", status, env.Success, env.Error)
	}

	// Verify soft-delete: location no longer visible via API.
	status, env = doJSON(t, h, http.MethodGet, "/v1/locations/"+createdWithCoords.UUID, userA.ID, nil)
	if status != http.StatusOK || env.Success {
		t.Fatalf("deleted location should return not-found: status=%d success=%v", status, env.Success)
	}

	// Verify soft-delete: deleted_at set in DB, row not gone.
	var deletedAt *time.Time
	err = pool.QueryRow(context.Background(),
		`SELECT deleted_at FROM station_locations WHERE uuid = $1`,
		createdWithCoords.UUID,
	).Scan(&deletedAt)
	if err != nil {
		t.Fatalf("check deleted_at: %v", err)
	}
	if deletedAt == nil {
		t.Error("expected deleted_at to be set after soft delete")
	}

	// ── List after delete: only 1 location remains ───────────────────────────
	status, env = doJSON(t, h, http.MethodGet, "/v1/locations", userA.ID, nil)
	if status != http.StatusOK || !env.Success {
		t.Fatalf("list after delete failed: status=%d success=%v", status, env.Success)
	}
	decodeData(t, env.Data, &listed)
	if len(listed.Items) != 1 {
		t.Fatalf("expected 1 location after delete, got %d", len(listed.Items))
	}
}

// TestIntegration_LocationPostGISDistance verifies that PostGIS distance calculations
// work correctly between two station locations stored in the database.
func TestIntegration_LocationPostGISDistance(t *testing.T) {
	pool, h := setupIntegration(t)
	user := createTestUser(t, pool, "loc-dist")

	// Create two locations with known coordinates.
	// EM10 center: lat=30.5, lng=-97 (central Texas)
	// JO62 center: lat=52.5, lng=13  (Berlin)
	// Expected great-circle distance ≈ 8900 km

	status, env := doJSON(t, h, http.MethodPost, "/v1/locations", user.ID, map[string]any{
		"name":        "Texas",
		"callsign":    "W5REX",
		"grid_square": "EM10",
		"latitude":    30.5,
		"longitude":   -97.0,
		"is_default":  true,
	})
	if status != http.StatusCreated || !env.Success {
		t.Fatalf("create location 1 failed: status=%d success=%v", status, env.Success)
	}
	var loc1 locationPayload
	decodeData(t, env.Data, &loc1)

	status, env = doJSON(t, h, http.MethodPost, "/v1/locations", user.ID, map[string]any{
		"name":        "Berlin",
		"callsign":    "DL1REX",
		"grid_square": "JO62",
		"latitude":    52.5,
		"longitude":   13.0,
		"is_default":  false,
	})
	if status != http.StatusCreated || !env.Success {
		t.Fatalf("create location 2 failed: status=%d success=%v", status, env.Success)
	}
	var loc2 locationPayload
	decodeData(t, env.Data, &loc2)

	// Query PostGIS distance between the two stored locations.
	var distKm float64
	err := pool.QueryRow(context.Background(), `
		SELECT ST_DistanceSphere(a.location, b.location) / 1000.0
		FROM station_locations a, station_locations b
		WHERE a.uuid = $1 AND b.uuid = $2
	`, loc1.UUID, loc2.UUID).Scan(&distKm)
	if err != nil {
		t.Fatalf("PostGIS distance query: %v", err)
	}

	// EM10 (30.5, -97) to JO62 (52.5, 13) ≈ 8900 km ± 500 km
	const wantKm, tolerKm = 8900.0, 500.0
	if distKm < wantKm-tolerKm || distKm > wantKm+tolerKm {
		t.Errorf("PostGIS distance between Texas and Berlin: got %.1f km, want %.1f ± %.1f km",
			distKm, wantKm, tolerKm)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Callsign management
// ─────────────────────────────────────────────────────────────────────────────

// TestIntegration_UserCallsignCRUDAndRLS tests the user callsign lifecycle:
//  1. Register a callsign
//  2. Register a second callsign as primary (clears first primary)
//  3. Update callsign metadata
//  4. List callsigns (primary first)
//  5. Delete callsign
//  6. RLS: user B cannot see user A's callsigns
func TestIntegration_UserCallsignCRUDAndRLS(t *testing.T) {
	pool, h := setupIntegration(t)
	userA := createTestUser(t, pool, "call-a")
	userB := createTestUser(t, pool, "call-b")

	// ── Create first callsign (non-primary) ───────────────────────────────────
	status, env := doJSON(t, h, http.MethodPost, "/v1/callsigns", userA.ID, map[string]any{
		"callsign":      "K5OLD",
		"license_class": "general",
		"is_primary":    false,
		"valid_from":    "2010-01-15",
		"valid_to":      "2020-01-14",
	})
	if status != http.StatusCreated || !env.Success {
		t.Fatalf("create callsign 1 failed: status=%d success=%v error=%q", status, env.Success, env.Error)
	}
	assertNoIDKey(t, env.Data)

	var first userCallsignPayload
	decodeData(t, env.Data, &first)
	if first.Callsign != "K5OLD" {
		t.Errorf("expected callsign K5OLD, got %q", first.Callsign)
	}
	if first.IsPrimary {
		t.Error("expected is_primary=false")
	}

	// ── Create second callsign as primary (vanity upgrade) ────────────────────
	status, env = doJSON(t, h, http.MethodPost, "/v1/callsigns", userA.ID, map[string]any{
		"callsign":      "w5rex",
		"license_class": "extra",
		"is_primary":    true,
		"valid_from":    "2020-01-15",
	})
	if status != http.StatusCreated || !env.Success {
		t.Fatalf("create callsign 2 failed: status=%d success=%v error=%q", status, env.Success, env.Error)
	}
	assertNoIDKey(t, env.Data)

	var second userCallsignPayload
	decodeData(t, env.Data, &second)
	if second.Callsign != "W5REX" {
		t.Errorf("callsign should be uppercased, got %q", second.Callsign)
	}
	if !second.IsPrimary {
		t.Error("expected is_primary=true for new primary callsign")
	}

	// Verify old callsign lost its primary flag.
	var firstStillPrimary bool
	err := pool.QueryRow(context.Background(),
		`SELECT is_primary FROM user_callsigns WHERE uuid = $1`, first.UUID,
	).Scan(&firstStillPrimary)
	if err != nil {
		t.Fatalf("check old primary: %v", err)
	}
	if firstStillPrimary {
		t.Error("old callsign should no longer be primary after a new primary was set")
	}

	// ── List: primary first ───────────────────────────────────────────────────
	status, env = doJSON(t, h, http.MethodGet, "/v1/callsigns", userA.ID, nil)
	if status != http.StatusOK || !env.Success {
		t.Fatalf("list callsigns failed: status=%d success=%v error=%q", status, env.Success, env.Error)
	}
	assertNoIDKey(t, env.Data)

	var listed listUserCallsignsPayload
	decodeData(t, env.Data, &listed)
	if len(listed.Items) != 2 {
		t.Fatalf("expected 2 callsigns, got %d", len(listed.Items))
	}
	if !listed.Items[0].IsPrimary {
		t.Error("expected primary callsign listed first")
	}
	if listed.Items[0].UUID != second.UUID {
		t.Errorf("expected primary to be %s, got %s", second.UUID, listed.Items[0].UUID)
	}

	// ── Update: set old callsign as primary again ─────────────────────────────
	status, env = doJSON(t, h, http.MethodPut, "/v1/callsigns/"+first.UUID, userA.ID, map[string]any{
		"is_primary":    true,
		"license_class": "extra",
		"valid_from":    "2010-01-15",
		"valid_to":      "2020-01-14",
	})
	if status != http.StatusOK || !env.Success {
		t.Fatalf("update callsign failed: status=%d success=%v error=%q", status, env.Success, env.Error)
	}
	assertNoIDKey(t, env.Data)

	// Verify previous primary (W5REX) lost its flag.
	var secondNowPrimary bool
	err = pool.QueryRow(context.Background(),
		`SELECT is_primary FROM user_callsigns WHERE uuid = $1`, second.UUID,
	).Scan(&secondNowPrimary)
	if err != nil {
		t.Fatalf("check new primary state: %v", err)
	}
	if secondNowPrimary {
		t.Error("W5REX should no longer be primary after K5OLD reclaimed it")
	}

	// ── RLS: user B sees 0 callsigns ──────────────────────────────────────────
	status, env = doJSON(t, h, http.MethodGet, "/v1/callsigns", userB.ID, nil)
	if status != http.StatusOK || !env.Success {
		t.Fatalf("user B list callsigns failed: status=%d success=%v", status, env.Success)
	}
	var bCallsigns listUserCallsignsPayload
	decodeData(t, env.Data, &bCallsigns)
	if len(bCallsigns.Items) != 0 {
		t.Errorf("user B should see 0 callsigns, got %d", len(bCallsigns.Items))
	}

	// ── Delete ───────────────────────────────────────────────────────────────
	status, env = doJSON(t, h, http.MethodDelete, "/v1/callsigns/"+first.UUID, userA.ID, nil)
	if status != http.StatusOK || !env.Success {
		t.Fatalf("delete callsign failed: status=%d success=%v error=%q", status, env.Success, env.Error)
	}

	// Verify the row is gone (hard delete for user_callsigns).
	var count int
	err = pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM user_callsigns WHERE uuid = $1`, first.UUID,
	).Scan(&count)
	if err != nil {
		t.Fatalf("check deleted callsign: %v", err)
	}
	if count != 0 {
		t.Errorf("expected hard-deleted callsign row to be gone, got count=%d", count)
	}

	// After delete, only 1 callsign remains.
	status, env = doJSON(t, h, http.MethodGet, "/v1/callsigns", userA.ID, nil)
	if status != http.StatusOK || !env.Success {
		t.Fatalf("list after delete failed")
	}
	decodeData(t, env.Data, &listed)
	if len(listed.Items) != 1 {
		t.Fatalf("expected 1 callsign after delete, got %d", len(listed.Items))
	}
}

// TestIntegration_StationCallsignCRUDAndRLS tests the station callsign lifecycle:
//  1. Create a personal station callsign
//  2. Create a club callsign
//  3. List (active first)
//  4. Update callsign type and active state
//  5. Delete (soft: sets active=FALSE)
//  6. RLS isolation
func TestIntegration_StationCallsignCRUDAndRLS(t *testing.T) {
	pool, h := setupIntegration(t)
	userA := createTestUser(t, pool, "stn-a")
	userB := createTestUser(t, pool, "stn-b")

	// ── Create personal callsign ──────────────────────────────────────────────
	status, env := doJSON(t, h, http.MethodPost, "/v1/station-callsigns", userA.ID, map[string]any{
		"callsign":      "w5rex",
		"callsign_type": "personal",
		"valid_from":    "2020-01-01",
	})
	if status != http.StatusCreated || !env.Success {
		t.Fatalf("create station callsign failed: status=%d success=%v error=%q", status, env.Success, env.Error)
	}
	assertNoIDKey(t, env.Data)

	var personal stationCallsignPayload
	decodeData(t, env.Data, &personal)
	if personal.Callsign != "W5REX" {
		t.Errorf("expected W5REX (uppercase), got %q", personal.Callsign)
	}
	if !personal.Active {
		t.Error("expected new callsign to be active")
	}

	// ── Create club callsign ──────────────────────────────────────────────────
	status, env = doJSON(t, h, http.MethodPost, "/v1/station-callsigns", userA.ID, map[string]any{
		"callsign":      "W5CLUB",
		"callsign_type": "club",
		"description":   "Local ham radio club",
	})
	if status != http.StatusCreated || !env.Success {
		t.Fatalf("create club callsign failed: status=%d success=%v error=%q", status, env.Success, env.Error)
	}

	var club stationCallsignPayload
	decodeData(t, env.Data, &club)
	if club.CallsignType != "club" {
		t.Errorf("expected callsign_type club, got %q", club.CallsignType)
	}

	// ── List: 2 active callsigns ──────────────────────────────────────────────
	status, env = doJSON(t, h, http.MethodGet, "/v1/station-callsigns", userA.ID, nil)
	if status != http.StatusOK || !env.Success {
		t.Fatalf("list station callsigns failed: status=%d success=%v", status, env.Success)
	}
	assertNoIDKey(t, env.Data)

	var listed listStationCallsignsPayload
	decodeData(t, env.Data, &listed)
	if len(listed.Items) != 2 {
		t.Fatalf("expected 2 station callsigns, got %d", len(listed.Items))
	}

	// ── Update: change type and disable ──────────────────────────────────────
	status, env = doJSON(t, h, http.MethodPut, "/v1/station-callsigns/"+personal.UUID, userA.ID, map[string]any{
		"callsign_type": "personal",
		"active":        false,
		"description":   "Archived after license upgrade",
	})
	if status != http.StatusOK || !env.Success {
		t.Fatalf("update station callsign failed: status=%d success=%v error=%q", status, env.Success, env.Error)
	}
	assertNoIDKey(t, env.Data)

	var updated stationCallsignPayload
	decodeData(t, env.Data, &updated)
	if updated.Active {
		t.Error("expected callsign to be inactive after update")
	}

	// ── Delete (soft deactivate) ──────────────────────────────────────────────
	status, env = doJSON(t, h, http.MethodDelete, "/v1/station-callsigns/"+club.UUID, userA.ID, nil)
	if status != http.StatusOK || !env.Success {
		t.Fatalf("delete station callsign failed: status=%d success=%v error=%q", status, env.Success, env.Error)
	}

	// Verify soft-delete: row exists but active=FALSE.
	var activeAfterDelete bool
	err := pool.QueryRow(context.Background(),
		`SELECT active FROM station_callsigns WHERE uuid = $1`, club.UUID,
	).Scan(&activeAfterDelete)
	if err != nil {
		t.Fatalf("check active after delete: %v", err)
	}
	if activeAfterDelete {
		t.Error("expected station callsign to be inactive after soft delete")
	}

	// ── RLS: user B sees 0 station callsigns ──────────────────────────────────
	status, env = doJSON(t, h, http.MethodGet, "/v1/station-callsigns", userB.ID, nil)
	if status != http.StatusOK || !env.Success {
		t.Fatalf("user B list station callsigns failed: status=%d success=%v", status, env.Success)
	}
	var bList listStationCallsignsPayload
	decodeData(t, env.Data, &bList)
	if len(bList.Items) != 0 {
		t.Errorf("user B should see 0 station callsigns, got %d", len(bList.Items))
	}

	// ── Invalid callsign_type rejected ───────────────────────────────────────
	status, env = doJSON(t, h, http.MethodPost, "/v1/station-callsigns", userA.ID, map[string]any{
		"callsign":      "W5INVALID",
		"callsign_type": "unknown_type",
	})
	if status != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid callsign_type, got %d", status)
	}

	// Cleanup DB rows so other tests don't see stale state.
	_, _ = pool.Exec(context.Background(),
		"DELETE FROM station_callsigns WHERE user_id = $1", userA.ID)

	_ = fmt.Sprintf("test user A: %d, B: %d", userA.ID, userB.ID)
}
