package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/FtlC-ian/radioledger/api/internal/handler"
)

// TestHealth_ReturnsOK verifies that the liveness endpoint always returns
// 200 with {"status":"ok"}, regardless of database state.
func TestHealth_ReturnsOK(t *testing.T) {
	h := handler.NewHealthHandler(nil) // nil pool: health does not check DB

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	h.Health(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}

	if body["status"] != "ok" {
		t.Errorf("expected body.status = \"ok\", got %q", body["status"])
	}
}

// TestHealth_ContentType verifies the response is application/json.
func TestHealth_ContentType(t *testing.T) {
	h := handler.NewHealthHandler(nil)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	h.Health(rec, req)

	ct := rec.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %q", ct)
	}
}

// TestReady_NilPool_Returns503 verifies that /ready returns 503 when the
// database pool has not been initialized (e.g. startup failure or nil injection).
func TestReady_NilPool_Returns503(t *testing.T) {
	h := handler.NewHealthHandler(nil) // nil pool simulates uninitialized state

	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	rec := httptest.NewRecorder()

	h.Ready(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status 503, got %d", rec.Code)
	}

	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}

	if body["status"] != "unavailable" {
		t.Errorf("expected body.status = \"unavailable\", got %q", body["status"])
	}
	if body["reason"] == "" {
		t.Error("expected non-empty reason in unavailable response")
	}
}
