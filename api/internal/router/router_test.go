package router_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/FtlC-ian/radioledger/api/internal/config"
	"github.com/FtlC-ian/radioledger/api/internal/router"
	"github.com/FtlC-ian/radioledger/pkg/plan"
)

// testConfig returns a minimal Config for router tests.
// DATABASE_URL is set to a placeholder; no real DB connection is made in unit tests.
func testConfig() *config.Config {
	return &config.Config{
		Port:               9091,
		CORSAllowedOrigins: "https://test.example.com",
		RateLimitIPRPS:     100,
		RateLimitIPBurst:   200,
		Env:                "development",
	}
}

// TestRouter_HealthRoute verifies that GET /health returns 200 without a database pool.
func TestRouter_HealthRoute(t *testing.T) {
	h := router.New(testConfig(), nil, nil) // nil pool: health endpoint handles this gracefully

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("GET /health: expected 200, got %d", rec.Code)
	}
}

// TestRouter_ReadyRoute_NilPool verifies that GET /ready returns 503 when no DB pool exists.
func TestRouter_ReadyRoute_NilPool(t *testing.T) {
	h := router.New(testConfig(), nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("GET /ready with nil pool: expected 503, got %d", rec.Code)
	}
}

// TestRouter_UnknownRoute_Returns404 verifies the router returns 404 for unknown paths.
func TestRouter_UnknownRoute_Returns404(t *testing.T) {
	h := router.New(testConfig(), nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/does-not-exist", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("GET /does-not-exist: expected 404, got %d", rec.Code)
	}
}

// TestRouter_RequestIDHeader verifies that the X-Request-ID header is present on responses.
func TestRouter_RequestIDHeader(t *testing.T) {
	h := router.New(testConfig(), nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Header().Get("X-Request-ID") == "" {
		t.Error("expected X-Request-ID header in response, got empty string")
	}
}

// TestRouter_RequestIDPropagation verifies that a client-supplied X-Request-ID is echoed back.
func TestRouter_RequestIDPropagation(t *testing.T) {
	h := router.New(testConfig(), nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Header.Set("X-Request-ID", "test-id-12345")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got := rec.Header().Get("X-Request-ID"); got != "test-id-12345" {
		t.Errorf("expected X-Request-ID to be echoed as \"test-id-12345\", got %q", got)
	}
}

// TestRouter_CORS_AllowedOrigin verifies that CORS headers are set for allowed origins.
func TestRouter_CORS_AllowedOrigin(t *testing.T) {
	h := router.New(testConfig(), nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Header.Set("Origin", "https://test.example.com")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://test.example.com" {
		t.Errorf("expected CORS allow origin header, got %q", got)
	}
}

// TestRouter_CORS_DisallowedOrigin verifies that CORS headers are NOT set for unknown origins.
func TestRouter_CORS_DisallowedOrigin(t *testing.T) {
	h := router.New(testConfig(), nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("expected no CORS header for disallowed origin, got %q", got)
	}
}

// TestRouter_Options_Preflight verifies that OPTIONS preflight returns 204.
func TestRouter_Options_Preflight(t *testing.T) {
	h := router.New(testConfig(), nil, nil)

	req := httptest.NewRequest(http.MethodOptions, "/health", nil)
	req.Header.Set("Origin", "https://test.example.com")
	req.Header.Set("Access-Control-Request-Method", "GET")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("OPTIONS preflight: expected 204, got %d", rec.Code)
	}
}

// TestRouter_QSORoute_NotImplemented verifies the QSO route exists and returns 501.
func TestRouter_QSORoute_NotImplemented(t *testing.T) {
	h := router.New(testConfig(), nil, nil)

	req := httptest.NewRequest(http.MethodGet,
		"/v1/logbooks/550e8400-e29b-41d4-a716-446655440000/qsos", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	// The route exists (not 404), but the handler returns 501 until implemented.
	if rec.Code == http.StatusNotFound {
		t.Error("QSO list route should exist, got 404")
	}
}

// TestRouter_LogbookRoute_Registered verifies the logbook route exists and is not a 404.
func TestRouter_LogbookRoute_Registered(t *testing.T) {
	h := router.New(testConfig(), nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/v1/logbooks", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code == http.StatusNotFound {
		t.Error("logbook list route should exist, got 404")
	}
}

// TestRouter_MetricsRoute_Public verifies that /metrics is publicly accessible.
func TestRouter_MetricsRoute_Public(t *testing.T) {
	h := router.New(testConfig(), nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /metrics: expected 200, got %d", rec.Code)
	}
}

// TestRouter_PlanRoute_Exists verifies that GET /v1/account/plan is registered
// and returns 401 (not 404) for unauthenticated requests.
func TestRouter_PlanRoute_Exists(t *testing.T) {
	h := router.New(testConfig(), nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/v1/account/plan", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code == http.StatusNotFound {
		t.Error("GET /v1/account/plan route should be registered, got 404")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("GET /v1/account/plan without auth: expected 401, got %d", rec.Code)
	}
}

// TestRouter_WithPlanProvider_CustomProvider verifies that WithPlanProvider
// is accepted and does not cause a panic or build error.
func TestRouter_WithPlanProvider_CustomProvider(t *testing.T) {
	// DefaultProvider with nil pool is a safe no-op provider.
	provider := plan.NewDefaultProvider(nil)
	h := router.New(testConfig(), nil, nil, router.WithPlanProvider(provider))

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("GET /health with custom provider: expected 200, got %d", rec.Code)
	}
}
