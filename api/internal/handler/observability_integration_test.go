package handler_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/FtlC-ian/radioledger/api/internal/logging"
)

// TestIntegration_ServiceStatusEndpoint verifies the public /v1/status/services
// endpoint is reachable without authentication and returns well-formed JSON with
// known service names.
func TestIntegration_ServiceStatusEndpoint(t *testing.T) {
	_, h := setupIntegration(t)

	req := httptest.NewRequest(http.MethodGet, "/v1/status/services", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /v1/status/services expected 200, got %d (body: %s)", rec.Code, rec.Body.String())
	}

	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Fatalf("expected application/json content-type, got %q", ct)
	}

	var payload struct {
		Services []struct {
			Service string `json:"service"`
			Status  string `json:"status"`
		} `json:"services"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("failed to decode /v1/status/services response: %v", err)
	}

	if len(payload.Services) == 0 {
		t.Fatal("expected at least one service in status response")
	}

	knownServices := map[string]bool{"eqsl": true, "clublog": true, "lotw": true, "qrz": true}
	validStatuses := map[string]bool{"ok": true, "circuit_open": true, "rate_limited": true}

	for _, s := range payload.Services {
		if s.Service == "" {
			t.Errorf("found service entry with empty name: %+v", s)
		}
		if !knownServices[s.Service] {
			t.Errorf("unexpected service %q in status response", s.Service)
		}
		if !validStatuses[s.Status] {
			t.Errorf("unexpected status %q for service %q", s.Status, s.Service)
		}
	}
}

// TestIntegration_ServiceStatusNoAuthRequired verifies that /v1/status/services
// returns 200 even without an Authorization header (truly public endpoint).
func TestIntegration_ServiceStatusNoAuthRequired(t *testing.T) {
	_, h := setupIntegration(t)

	// Deliberately send a request with no auth headers.
	req := httptest.NewRequest(http.MethodGet, "/v1/status/services", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unauthenticated GET /v1/status/services expected 200, got %d", rec.Code)
	}
}

func TestIntegration_MetricsEndpointPrometheusFormat(t *testing.T) {
	_, h := setupIntegration(t)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /metrics expected 200, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/plain") {
		t.Fatalf("expected Prometheus content type, got %q", ct)
	}

	body := rec.Body.String()
	for _, needle := range []string{
		"# HELP radioledger_http_requests_total",
		"# HELP radioledger_http_request_duration_seconds",
		"# HELP radioledger_qsos_logged_total",
		"# HELP radioledger_adif_import_duration_seconds",
	} {
		if !strings.Contains(body, needle) {
			t.Fatalf("/metrics missing expected line: %s", needle)
		}
	}
}

func TestIntegration_MetricsIncrementOnAPICalls(t *testing.T) {
	pool, h := setupIntegration(t)
	user := createTestUser(t, pool, "metrics")
	logbookUUID := createLogbookViaAPI(t, h, user.ID, "Metrics Log", true)

	before := scrapeMetrics(t, h)
	beforeHTTP := metricValue(before, "radioledger_http_requests_total", map[string]string{
		"method": "GET",
		"path":   "/health",
		"status": "200",
	})
	beforeQSO := metricValue(before, "radioledger_qsos_logged_total", nil)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /health expected 200, got %d", rec.Code)
	}

	createQSOViaAPI(t, h, user.ID, logbookUUID, map[string]any{
		"callsign":    "K1OBS",
		"band":        "20m",
		"mode":        "CW",
		"datetime_on": time.Now().UTC().Format(time.RFC3339),
	})

	after := scrapeMetrics(t, h)
	afterHTTP := metricValue(after, "radioledger_http_requests_total", map[string]string{
		"method": "GET",
		"path":   "/health",
		"status": "200",
	})
	afterQSO := metricValue(after, "radioledger_qsos_logged_total", nil)

	if afterHTTP < beforeHTTP+1 {
		t.Fatalf("expected http request counter to increment: before=%v after=%v", beforeHTTP, afterHTTP)
	}
	if afterQSO < beforeQSO+1 {
		t.Fatalf("expected qso logged counter to increment: before=%v after=%v", beforeQSO, afterQSO)
	}
}

func TestIntegration_StructuredRequestLogFields(t *testing.T) {
	var buf bytes.Buffer
	old := slog.Default()
	logging.Setup(logging.Config{
		Env:    "development",
		Level:  "debug",
		Format: "json",
		Out:    &buf,
	})
	defer slog.SetDefault(old)

	pool, h := setupIntegration(t)
	user := createTestUser(t, pool, "logs")

	status, env := doJSON(t, h, http.MethodGet, "/v1/logbooks", user.ID, nil)
	if status != http.StatusOK || !env.Success {
		t.Fatalf("list logbooks failed: status=%d success=%v error=%q", status, env.Success, env.Error)
	}

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) == 0 {
		t.Fatal("expected log output, got empty buffer")
	}

	var httpLog map[string]any
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var row map[string]any
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			continue
		}
		if msg, _ := row["msg"].(string); msg == "http_request" {
			httpLog = row
			break
		}
	}

	if httpLog == nil {
		t.Fatalf("expected at least one http_request log line, got: %s", buf.String())
	}

	for _, key := range []string{"request_id", "user_id", "duration_ms", "method", "path", "status", "request_size", "response_size", "remote_ip"} {
		if _, ok := httpLog[key]; !ok {
			t.Fatalf("missing %q in structured log: %+v", key, httpLog)
		}
	}
}

func scrapeMetrics(t *testing.T, h http.Handler) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /metrics expected 200, got %d", rec.Code)
	}
	return rec.Body.String()
}

func metricValue(metrics, name string, labels map[string]string) float64 {
	for _, line := range strings.Split(metrics, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || !strings.HasPrefix(line, name) {
			continue
		}

		if labels != nil {
			ok := true
			for k, v := range labels {
				needle := fmt.Sprintf(`%s="%s"`, k, v)
				if !strings.Contains(line, needle) {
					ok = false
					break
				}
			}
			if !ok {
				continue
			}
		}

		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		val, err := strconv.ParseFloat(fields[len(fields)-1], 64)
		if err == nil {
			return val
		}
	}
	return 0
}
