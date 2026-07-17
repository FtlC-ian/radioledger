package handler_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/FtlC-ian/radioledger/api/internal/handler"
)

func TestCallsignGridLookup_NotFound(t *testing.T) {
	pool, _ := setupIntegration(t)

	r := chi.NewRouter()
	r.Get("/v1/callsign/{call}/grid", handler.NewCallsignDBHandler(pool).GridLookup)

	status, env := doJSON(t, r, http.MethodGet, "/v1/callsign/ZZZNOPE123/grid", 0, nil)
	if status != http.StatusOK {
		t.Fatalf("status: got %d want 200", status)
	}
	if !env.Success {
		t.Fatalf("expected success=true, got error=%q", env.Error)
	}
}

func TestCallsignGridLookup_ReturnsCachedGrid(t *testing.T) {
	pool, _ := setupIntegration(t)

	call := "W1GRIDCACHED"
	_, err := pool.Exec(context.Background(), `
		INSERT INTO callsign_records
			(callsign, source, full_name, city, state_province, postal_code, country, status, grid_square, fetched_at, updated_at)
		VALUES ($1, 'fcc', 'Cached Grid', 'Newington', 'CT', '06111', 'US', 'active', 'FN31PR', now(), now())
		ON CONFLICT (callsign, source) DO UPDATE SET grid_square = EXCLUDED.grid_square, updated_at = now()
	`, call)
	if err != nil {
		t.Fatalf("seed record: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM callsign_records WHERE callsign = $1`, call)
	})

	r := chi.NewRouter()
	r.Get("/v1/callsign/{call}/grid", handler.NewCallsignDBHandler(pool).GridLookup)

	status, env := doJSON(t, r, http.MethodGet, "/v1/callsign/"+call+"/grid", 0, nil)
	if status != http.StatusOK || !env.Success {
		t.Fatalf("grid lookup failed: status=%d success=%v error=%q", status, env.Success, env.Error)
	}

	var data struct {
		Callsign   string `json:"callsign"`
		GridSquare string `json:"grid_square"`
		Source     string `json:"source"`
		City       string `json:"city"`
		State      string `json:"state"`
	}
	decodeData(t, env.Data, &data)

	if data.Callsign != call {
		t.Fatalf("callsign: got %q want %q", data.Callsign, call)
	}
	if data.GridSquare != "FN31pr" {
		t.Fatalf("grid_square: got %q want %q", data.GridSquare, "FN31pr")
	}
	if data.Source != "cached" {
		t.Fatalf("source: got %q want %q", data.Source, "cached")
	}
}

// TestCallsignGridLookup_ZipCentroidFallback verifies that when census geocoding
// fails (or there's no street address), the handler falls back to the local
// zip_centroids table and returns source="zip_centroid".
func TestCallsignGridLookup_ZipCentroidFallback(t *testing.T) {
	pool, _ := setupIntegration(t)

	// Ensure zip_centroids table exists — it is created by migration 008 but
	// may not be present in CI/local environments that haven't run the latest
	// migrations yet.
	_, err := pool.Exec(context.Background(), `
		CREATE TABLE IF NOT EXISTS zip_centroids (
			zip_code   TEXT             PRIMARY KEY,
			latitude   DOUBLE PRECISION NOT NULL,
			longitude  DOUBLE PRECISION NOT NULL,
			city       TEXT,
			state      TEXT,
			updated_at TIMESTAMPTZ      NOT NULL DEFAULT now()
		)
	`)
	if err != nil {
		t.Fatalf("ensure zip_centroids table: %v", err)
	}

	call := "W1GRIDZIP"

	// Seed a record with no street address and a known zip, so census can't help.
	_, err = pool.Exec(context.Background(), `
		INSERT INTO callsign_records
			(callsign, source, full_name, city, state_province, postal_code, country, status, fetched_at, updated_at)
		VALUES ($1, 'fcc', 'Zip Grid', 'Newington', 'CT', '06111', 'US', 'active', now(), now())
		ON CONFLICT (callsign, source) DO UPDATE SET
			address_line1  = NULL,
			grid_square    = NULL,
			latitude       = NULL,
			longitude      = NULL,
			updated_at     = now()
	`, call)
	if err != nil {
		t.Fatalf("seed record: %v", err)
	}

	// Seed zip centroid for 06111.
	_, err = pool.Exec(context.Background(), `
		INSERT INTO zip_centroids (zip_code, latitude, longitude, city, state, updated_at)
		VALUES ('06111', 41.7148, -72.7272, 'Newington', 'CT', now())
		ON CONFLICT (zip_code) DO UPDATE SET
			latitude   = EXCLUDED.latitude,
			longitude  = EXCLUDED.longitude,
			updated_at = EXCLUDED.updated_at
	`)
	if err != nil {
		t.Fatalf("seed zip centroid: %v", err)
	}

	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM callsign_records WHERE callsign = $1`, call)
		_, _ = pool.Exec(context.Background(), `DELETE FROM zip_centroids WHERE zip_code = '06111'`)
	})

	// Use handler with a geocoder that always fails (no street address anyway).
	h := handler.NewCallsignDBHandlerWithGeocoder(pool, func(ctx context.Context, street, city, state, zip string) (float64, float64, error) {
		return 0, 0, fmt.Errorf("census unavailable")
	})

	r := chi.NewRouter()
	r.Get("/v1/callsign/{call}/grid", h.GridLookup)

	status, env := doJSON(t, r, http.MethodGet, "/v1/callsign/"+call+"/grid", 0, nil)
	if status != http.StatusOK || !env.Success {
		t.Fatalf("grid lookup failed: status=%d success=%v error=%q", status, env.Success, env.Error)
	}

	var data struct {
		Callsign   string `json:"callsign"`
		GridSquare string `json:"grid_square"`
		Source     string `json:"source"`
	}
	decodeData(t, env.Data, &data)

	if data.Callsign != call {
		t.Fatalf("callsign: got %q want %q", data.Callsign, call)
	}
	if data.GridSquare == "" {
		t.Fatal("expected non-empty grid_square from zip centroid fallback")
	}
	if data.Source != "zip_centroid" {
		t.Fatalf("source: got %q want %q", data.Source, "zip_centroid")
	}
}

func TestCallsignGridLookup_GeocodesAndCaches(t *testing.T) {
	pool, _ := setupIntegration(t)

	call := "W1GRIDGEO"
	_, err := pool.Exec(context.Background(), `
		INSERT INTO callsign_records
			(callsign, source, full_name, address_line1, city, state_province, postal_code, country, status, fetched_at, updated_at)
		VALUES ($1, 'fcc', 'Geo Grid', '225 Main St', 'Newington', 'CT', '06111', 'US', 'active', now(), now())
		ON CONFLICT (callsign, source) DO UPDATE SET
			address_line1 = EXCLUDED.address_line1,
			city = EXCLUDED.city,
			state_province = EXCLUDED.state_province,
			postal_code = EXCLUDED.postal_code,
			grid_square = NULL,
			latitude = NULL,
			longitude = NULL,
			updated_at = now()
	`, call)
	if err != nil {
		t.Fatalf("seed record: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM callsign_records WHERE callsign = $1`, call)
	})

	h := handler.NewCallsignDBHandlerWithGeocoder(pool, func(ctx context.Context, street, city, state, zip string) (float64, float64, error) {
		if street != "225 Main St" || city != "Newington" || state != "CT" || zip != "06111" {
			t.Fatalf("unexpected geocoder input: street=%q city=%q state=%q zip=%q", street, city, state, zip)
		}
		return 41.7148, -72.7272, nil
	})

	r := chi.NewRouter()
	r.Get("/v1/callsign/{call}/grid", h.GridLookup)

	status, env := doJSON(t, r, http.MethodGet, "/v1/callsign/"+call+"/grid", 0, nil)
	if status != http.StatusOK || !env.Success {
		t.Fatalf("grid lookup failed: status=%d success=%v error=%q", status, env.Success, env.Error)
	}

	var data struct {
		Callsign   string `json:"callsign"`
		GridSquare string `json:"grid_square"`
		Source     string `json:"source"`
		City       string `json:"city"`
		State      string `json:"state"`
	}
	decodeData(t, env.Data, &data)

	if data.Callsign != call {
		t.Fatalf("callsign: got %q want %q", data.Callsign, call)
	}
	if data.GridSquare != "FN31pr" {
		t.Fatalf("grid_square: got %q want %q", data.GridSquare, "FN31pr")
	}
	if data.Source != "geocoded" {
		t.Fatalf("source: got %q want %q", data.Source, "geocoded")
	}
	if data.City != "Newington" || data.State != "CT" {
		t.Fatalf("location hint fields: got city=%q state=%q", data.City, data.State)
	}

	var storedGrid *string
	var storedLat, storedLon *float64
	err = pool.QueryRow(context.Background(), `
		SELECT grid_square, latitude, longitude
		FROM callsign_records
		WHERE callsign = $1 AND source = 'fcc'
	`, call).Scan(&storedGrid, &storedLat, &storedLon)
	if err != nil {
		t.Fatalf("query cached values: %v", err)
	}
	if storedGrid == nil || *storedGrid != "FN31PR" {
		t.Fatalf("stored grid_square: got %#v want %q", storedGrid, "FN31PR")
	}
	if storedLat == nil || storedLon == nil {
		t.Fatalf("expected cached coordinates, got lat=%v lon=%v", storedLat, storedLon)
	}
}
