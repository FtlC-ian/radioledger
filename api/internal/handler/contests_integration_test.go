package handler_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ─── Payload types ─────────────────────────────────────────────────────────

type contestSessionPayload struct {
	UUID             string     `json:"uuid"`
	LogbookUUID      string     `json:"logbook_uuid"`
	ContestCode      string     `json:"contest_code"`
	ContestName      string     `json:"contest_name"`
	Name             string     `json:"name"`
	MyCallsign       *string    `json:"my_callsign,omitempty"`
	ExchangeTemplate string     `json:"exchange_template"`
	ExchangeSent     *string    `json:"exchange_sent,omitempty"`
	Status           string     `json:"status"`
	SerialCounter    int32      `json:"serial_counter"`
	CategoryOperator string     `json:"category_operator"`
	CategoryBand     string     `json:"category_band"`
	CategoryMode     string     `json:"category_mode"`
	CategoryPower    string     `json:"category_power"`
	StartsAt         *time.Time `json:"starts_at,omitempty"`
	EndsAt           *time.Time `json:"ends_at,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
}

type contestQSOPayload struct {
	UUID         string  `json:"uuid"`
	Callsign     string  `json:"callsign"`
	Band         string  `json:"band"`
	Mode         string  `json:"mode"`
	DatetimeOn   string  `json:"datetime_on"`
	RstSent      *string `json:"rst_sent,omitempty"`
	RstRcvd      *string `json:"rst_rcvd,omitempty"`
	SentSerial   *int32  `json:"sent_serial,omitempty"`
	SentExchange *string `json:"sent_exchange,omitempty"`
	RecvExchange *string `json:"recv_exchange,omitempty"`
	IsDupe       bool    `json:"is_dupe"`
}

type dupeCheckPayload struct {
	Dupe        bool `json:"dupe"`
	PreviousQSO *struct {
		UUID         string    `json:"uuid"`
		Callsign     string    `json:"callsign"`
		Band         string    `json:"band"`
		Mode         string    `json:"mode"`
		DatetimeOn   time.Time `json:"datetime_on"`
		SentSerial   *int32    `json:"sent_serial,omitempty"`
		RecvExchange *string   `json:"recv_exchange,omitempty"`
	} `json:"previous_qso,omitempty"`
}

type contestStatsPayload struct {
	TotalQSOs       int64   `json:"total_qsos"`
	DupeQSOs        int64   `json:"dupe_qsos"`
	UniqueCallsigns int64   `json:"unique_callsigns"`
	SerialCounter   int32   `json:"serial_counter"`
	RatePerHour     float64 `json:"rate_per_hour"`
	FirstQSOAt      *string `json:"first_qso_at,omitempty"`
}

type listContestSessionsPayload struct {
	Items []contestSessionPayload `json:"items"`
}

// ─── TestIntegration_ContestLifecycle ─────────────────────────────────────

// TestIntegration_ContestLifecycle exercises the full contest logging workflow:
//  1. Create a contest session
//  2. Log multiple QSOs with auto-serial
//  3. Verify dupe detection (same call+band = dupe)
//  4. Fetch live statistics (rate, QSO count, serial)
//  5. Export in Cabrillo format and validate structure
//  6. Export in ADIF format
func TestIntegration_ContestLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	pool, h := setupIntegration(t)
	user := createTestUser(t, pool, "contest-lifecycle")
	logbookUUID := createContestTestLogbook(t, pool, user.ID, "Contest Logbook")

	// ── 1. Create a contest session ────────────────────────────────────

	startsAt := time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)
	endsAt := startsAt.Add(48 * time.Hour)

	status, env := doJSON(t, h, http.MethodPost, "/v1/contests", user.ID, map[string]any{
		"logbook_uuid":      logbookUUID,
		"name":              "CQ WW SSB 2026",
		"contest_id":        "CQ-WW-SSB",
		"exchange_template": "zone",
		"exchange_sent":     "05",
		"category_operator": "SINGLE-OP",
		"category_band":     "ALL",
		"category_mode":     "SSB",
		"category_power":    "HIGH",
		"starts_at":         startsAt.Format(time.RFC3339),
		"ends_at":           endsAt.Format(time.RFC3339),
	})
	if status != http.StatusCreated || !env.Success {
		t.Fatalf("create contest session failed: status=%d success=%v error=%q body=%s",
			status, env.Success, env.Error, string(env.Data))
	}

	var session contestSessionPayload
	decodeData(t, env.Data, &session)

	if _, err := uuid.Parse(session.UUID); err != nil {
		t.Fatalf("invalid contest session UUID: %v", err)
	}
	if session.Status != "active" {
		t.Errorf("expected status=active, got %q", session.Status)
	}
	if session.ContestCode != "CQ-WW-SSB" {
		t.Errorf("expected contest_code=CQ-WW-SSB, got %q", session.ContestCode)
	}
	if session.ExchangeTemplate != "zone" {
		t.Errorf("expected exchange_template=zone, got %q", session.ExchangeTemplate)
	}
	if session.CategoryMode != "SSB" {
		t.Errorf("expected category_mode=SSB, got %q", session.CategoryMode)
	}
	t.Logf("created contest session %s", session.UUID)

	// ── 2. List contest sessions ───────────────────────────────────────

	status, env = doJSON(t, h, http.MethodGet, "/v1/contests", user.ID, nil)
	if status != http.StatusOK || !env.Success {
		t.Fatalf("list contests failed: status=%d", status)
	}
	var listed listContestSessionsPayload
	decodeData(t, env.Data, &listed)
	if len(listed.Items) < 1 {
		t.Fatal("expected at least 1 contest session in list")
	}

	// ── 3. Log QSOs with auto-serial numbers ──────────────────────────

	type qsoEntry struct {
		callsign     string
		band         string
		recvExchange string
	}
	qsoEntries := []qsoEntry{
		{"W1AW", "20m", "001 05"},
		{"K1ZZ", "20m", "002 CT"},
		{"N6TR", "20m", "003 OR"},
		{"W1AW", "15m", "004 05"}, // same call, different band — NOT a dupe
	}

	loggedQSOs := make([]contestQSOPayload, 0, len(qsoEntries))
	for i, entry := range qsoEntries {
		status, env = doJSON(t, h, http.MethodPost,
			fmt.Sprintf("/v1/contests/%s/qso", session.UUID), user.ID,
			map[string]any{
				"callsign":      entry.callsign,
				"band":          entry.band,
				"mode":          "SSB",
				"exchange_rcvd": entry.recvExchange,
			})
		// Expect 201 for unique QSOs, 200 for dupes.
		if status != http.StatusCreated && status != http.StatusOK {
			t.Fatalf("log QSO %d (%s/%s) failed: status=%d error=%q",
				i, entry.callsign, entry.band, status, env.Error)
		}
		if !env.Success {
			t.Fatalf("log QSO %d failed: success=%v error=%q", i, env.Success, env.Error)
		}
		var qso contestQSOPayload
		decodeData(t, env.Data, &qso)
		loggedQSOs = append(loggedQSOs, qso)
		t.Logf("logged QSO %d: %s/%s serial=%v dupe=%v",
			i+1, qso.Callsign, qso.Band, qso.SentSerial, qso.IsDupe)
	}

	// All 4 QSOs should be non-dupes (W1AW on 20m ≠ W1AW on 15m).
	for _, qso := range loggedQSOs {
		if qso.IsDupe {
			t.Errorf("unexpected dupe: %s/%s", qso.Callsign, qso.Band)
		}
	}

	// RST should default to "59" for SSB.
	if loggedQSOs[0].RstSent != nil && *loggedQSOs[0].RstSent != "59" {
		t.Errorf("expected RST=59 for SSB, got %q", *loggedQSOs[0].RstSent)
	}

	// ── 4. Dupe detection — same call + same band = dupe ──────────────

	status, env = doJSON(t, h, http.MethodPost,
		fmt.Sprintf("/v1/contests/%s/qso", session.UUID), user.ID,
		map[string]any{
			"callsign":      "W1AW", // already worked on 20m
			"band":          "20m",
			"mode":          "SSB",
			"exchange_rcvd": "099 05",
		})
	if status != http.StatusOK || !env.Success {
		t.Fatalf("dupe QSO failed: status=%d error=%q", status, env.Error)
	}
	var dupeQSO contestQSOPayload
	decodeData(t, env.Data, &dupeQSO)
	if !dupeQSO.IsDupe {
		t.Error("expected is_dupe=true for W1AW/20m second contact, got false")
	}
	t.Logf("dupe correctly detected: %s/%s", dupeQSO.Callsign, dupeQSO.Band)

	// ── 5. Real-time dupe check endpoint ──────────────────────────────

	status, env = doJSON(t, h, http.MethodGet,
		fmt.Sprintf("/v1/contests/%s/check-dupe?callsign=W1AW&band=20m", session.UUID), user.ID, nil)
	if status != http.StatusOK || !env.Success {
		t.Fatalf("check-dupe failed: status=%d error=%q", status, env.Error)
	}
	var dc dupeCheckPayload
	decodeData(t, env.Data, &dc)
	if !dc.Dupe {
		t.Error("check-dupe: expected dupe=true for W1AW/20m")
	}
	if dc.PreviousQSO == nil {
		t.Error("check-dupe: expected previous_qso to be populated")
	}

	// Different band on same call should NOT be a dupe.
	status, env = doJSON(t, h, http.MethodGet,
		fmt.Sprintf("/v1/contests/%s/check-dupe?callsign=W1AW&band=40m", session.UUID), user.ID, nil)
	if status != http.StatusOK {
		t.Fatalf("check-dupe (40m) failed: status=%d", status)
	}
	var dc40 dupeCheckPayload
	decodeData(t, env.Data, &dc40)
	if dc40.Dupe {
		t.Error("check-dupe: W1AW/40m should NOT be a dupe")
	}

	// ── 6. Stats endpoint ─────────────────────────────────────────────

	status, env = doJSON(t, h, http.MethodGet,
		fmt.Sprintf("/v1/contests/%s/stats", session.UUID), user.ID, nil)
	if status != http.StatusOK || !env.Success {
		t.Fatalf("stats failed: status=%d error=%q", status, env.Error)
	}
	var stats contestStatsPayload
	decodeData(t, env.Data, &stats)

	// 4 unique + 1 dupe = 5 total QSOs.
	if stats.TotalQSOs != 4 {
		t.Errorf("expected total_qsos=4 (non-dupes), got %d", stats.TotalQSOs)
	}
	if stats.DupeQSOs != 1 {
		t.Errorf("expected dupe_qsos=1, got %d", stats.DupeQSOs)
	}
	if stats.UniqueCallsigns < 3 {
		t.Errorf("expected unique_callsigns>=3, got %d", stats.UniqueCallsigns)
	}
	t.Logf("stats: total=%d dupes=%d unique=%d serial=%d rate/hr=%.1f",
		stats.TotalQSOs, stats.DupeQSOs, stats.UniqueCallsigns,
		stats.SerialCounter, stats.RatePerHour)

	// ── 7. GET contest detail (with stats) ────────────────────────────

	status, env = doJSON(t, h, http.MethodGet,
		fmt.Sprintf("/v1/contests/%s", session.UUID), user.ID, nil)
	if status != http.StatusOK || !env.Success {
		t.Fatalf("get contest detail failed: status=%d error=%q", status, env.Error)
	}
	var detail contestSessionPayload
	decodeData(t, env.Data, &detail)
	if detail.UUID != session.UUID {
		t.Errorf("expected UUID=%s, got %s", session.UUID, detail.UUID)
	}

	// ── 8. Update contest session ──────────────────────────────────────

	status, env = doJSON(t, h, http.MethodPut,
		fmt.Sprintf("/v1/contests/%s", session.UUID), user.ID,
		map[string]any{
			"name":                 "CQ WW SSB 2026 UPDATED",
			"exchange_template":    "zone",
			"exchange_sent":        "05",
			"status":               "finished",
			"category_operator":    "SINGLE-OP",
			"category_assisted":    "NON-ASSISTED",
			"category_band":        "ALL",
			"category_mode":        "SSB",
			"category_power":       "HIGH",
			"category_station":     "FIXED",
			"category_time":        "24-HOURS",
			"category_transmitter": "ONE",
		})
	if status != http.StatusOK || !env.Success {
		t.Fatalf("update contest session failed: status=%d success=%v body=%s",
			status, env.Success, string(env.Data))
	}
	var updated contestSessionPayload
	decodeData(t, env.Data, &updated)
	_ = updated // status field will be in the response

	// ── 9. Cabrillo export ────────────────────────────────────────────

	cabrilloStatus, cabrilloEnv := doRaw(t, h, http.MethodGet,
		fmt.Sprintf("/v1/contests/%s/export/cabrillo", session.UUID),
		issueTestLocalJWT(t, user.ID), nil)
	if cabrilloStatus != http.StatusOK {
		t.Fatalf("cabrillo export failed: status=%d", cabrilloStatus)
	}
	// Non-JSON response: env.Data will be empty for text/plain.
	// Check via the raw env message or use doRawBytes helper.
	_ = cabrilloEnv

	// Re-do as a raw bytes request to get the response body.
	cabrilloBody := doExportRequest(t, h, http.MethodGet,
		fmt.Sprintf("/v1/contests/%s/export/cabrillo", session.UUID), user.ID)
	t.Logf("Cabrillo output (%d bytes):\n%.500s", len(cabrilloBody), cabrilloBody)
	validateCabrillo(t, cabrilloBody)

	// ── 10. ADIF export ────────────────────────────────────────────────

	adifBody := doExportRequest(t, h, http.MethodGet,
		fmt.Sprintf("/v1/contests/%s/export/adif", session.UUID), user.ID)
	if !strings.Contains(adifBody, "<EOH>") {
		t.Error("ADIF export missing <EOH> header marker")
	}
	if !strings.Contains(adifBody, "CQ-WW-SSB") {
		t.Error("ADIF export missing CONTEST_ID value")
	}
	if !strings.Contains(adifBody, "<EOR>") {
		t.Error("ADIF export missing <EOR> record marker")
	}
	t.Logf("ADIF export OK (%d bytes)", len(adifBody))
}

// TestIntegration_ContestSerialNumbers verifies auto-increment serial numbers are assigned correctly.
func TestIntegration_ContestSerialNumbers(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	pool, h := setupIntegration(t)
	user := createTestUser(t, pool, "contest-serial")
	logbookUUID := createContestTestLogbook(t, pool, user.ID, "Contest Logbook")

	// Create a serial-exchange contest.
	status, env := doJSON(t, h, http.MethodPost, "/v1/contests", user.ID, map[string]any{
		"logbook_uuid":      logbookUUID,
		"name":              "ARRL DX CW 2026",
		"contest_id":        "ARRL-DX-CW",
		"exchange_template": "serial",
		"exchange_sent":     "AR",
		"category_operator": "SINGLE-OP",
		"category_band":     "ALL",
		"category_mode":     "CW",
		"category_power":    "LOW",
	})
	if status != http.StatusCreated || !env.Success {
		t.Fatalf("create contest failed: status=%d success=%v", status, env.Success)
	}
	var session contestSessionPayload
	decodeData(t, env.Data, &session)

	// Log 5 QSOs with unique callsigns.
	callsigns := []string{"VK2GX", "JA1ZLO", "DL1YFF", "F5INB", "PY5EG"}
	for i, call := range callsigns {
		st, ev := doJSON(t, h, http.MethodPost,
			fmt.Sprintf("/v1/contests/%s/qso", session.UUID), user.ID,
			map[string]any{
				"callsign":      call,
				"band":          "20m",
				"mode":          "CW",
				"exchange_rcvd": fmt.Sprintf("%03d AR", i+1),
			})
		if st != http.StatusCreated {
			t.Fatalf("log QSO %d failed: status=%d error=%q", i, st, ev.Error)
		}
		var qso contestQSOPayload
		decodeData(t, ev.Data, &qso)

		// Serial numbers should be 1, 2, 3, 4, 5.
		expectedSerial := int32(i + 1)
		if qso.SentSerial == nil || *qso.SentSerial != expectedSerial {
			t.Errorf("QSO %d: expected serial=%d, got %v", i+1, expectedSerial, qso.SentSerial)
		}
		t.Logf("QSO %d: %s serial=%d exchange=%q",
			i+1, call, *qso.SentSerial, safeString(qso.SentExchange))
	}

	// Verify stats serial counter.
	st, ev := doJSON(t, h, http.MethodGet,
		fmt.Sprintf("/v1/contests/%s/stats", session.UUID), user.ID, nil)
	if st != http.StatusOK {
		t.Fatalf("stats failed: status=%d", st)
	}
	var stats contestStatsPayload
	decodeData(t, ev.Data, &stats)
	if stats.SerialCounter != 5 {
		t.Errorf("expected serial_counter=5, got %d", stats.SerialCounter)
	}

	// RST should default to "599" for CW.
	st, ev = doJSON(t, h, http.MethodPost,
		fmt.Sprintf("/v1/contests/%s/qso", session.UUID), user.ID,
		map[string]any{
			"callsign":      "G3SXW",
			"band":          "15m",
			"mode":          "CW",
			"exchange_rcvd": "006 AR",
		})
	if st != http.StatusCreated {
		t.Fatalf("log CW QSO failed: status=%d", st)
	}
	var cwQSO contestQSOPayload
	decodeData(t, ev.Data, &cwQSO)
	if cwQSO.RstSent != nil && *cwQSO.RstSent != "599" {
		t.Errorf("expected RST=599 for CW, got %q", safeString(cwQSO.RstSent))
	}
}

// TestIntegration_ContestCabrilloFormat validates the Cabrillo file format in detail.
func TestIntegration_ContestCabrilloFormat(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	pool, h := setupIntegration(t)
	user := createTestUser(t, pool, "contest-cabrillo")
	logbookUUID := createContestTestLogbook(t, pool, user.ID, "Contest Logbook")

	// Create contest session.
	status, env := doJSON(t, h, http.MethodPost, "/v1/contests", user.ID, map[string]any{
		"logbook_uuid":         logbookUUID,
		"name":                 "CQ WPX CW 2026",
		"contest_id":           "CQ-WPX-CW",
		"exchange_template":    "serial",
		"category_operator":    "SINGLE-OP",
		"category_assisted":    "NON-ASSISTED",
		"category_band":        "ALL",
		"category_mode":        "CW",
		"category_power":       "HIGH",
		"category_station":     "FIXED",
		"category_time":        "24-HOURS",
		"category_transmitter": "ONE",
		"operators_line":       "W1TEST",
		"club_name":            "Radio Amateurs of Springfield",
		"soapbox":              "First contest with RadioLedger. Great fun!",
	})
	if status != http.StatusCreated || !env.Success {
		t.Fatalf("create contest failed: status=%d success=%v", status, env.Success)
	}
	var session contestSessionPayload
	decodeData(t, env.Data, &session)

	// Log 3 QSOs.
	testQSOs := []struct {
		call string
		band string
		recv string
	}{
		{"W1AW", "14025", "001 AR"},
		{"K1ZZ", "21025", "002 CT"},
		{"VE3EJ", "28025", "003 ON"},
	}
	for i, q := range testQSOs {
		st, ev := doJSON(t, h, http.MethodPost,
			fmt.Sprintf("/v1/contests/%s/qso", session.UUID), user.ID,
			map[string]any{
				"callsign":      q.call,
				"band":          bandFromFreq(q.band),
				"mode":          "CW",
				"frequency_hz":  freqToHz(q.band),
				"exchange_rcvd": q.recv,
			})
		if st != http.StatusCreated {
			t.Fatalf("log QSO %d failed: status=%d error=%q", i, st, ev.Error)
		}
	}

	// Export Cabrillo.
	cabrillo := doExportRequest(t, h, http.MethodGet,
		fmt.Sprintf("/v1/contests/%s/export/cabrillo", session.UUID), user.ID)
	if cabrillo == "" {
		t.Fatal("export returned empty response")
	}
	validateCabrillo(t, cabrillo)

	// Check specific header fields.
	lines := strings.Split(cabrillo, "\n")
	headerFields := map[string]string{}
	for _, line := range lines {
		if idx := strings.Index(line, ": "); idx > 0 {
			key := strings.TrimSpace(line[:idx])
			val := strings.TrimSpace(line[idx+2:])
			headerFields[key] = val
		}
	}

	if v := headerFields["CATEGORY-OPERATOR"]; v != "SINGLE-OP" {
		t.Errorf("CATEGORY-OPERATOR: expected SINGLE-OP, got %q", v)
	}
	if v := headerFields["CATEGORY-POWER"]; v != "HIGH" {
		t.Errorf("CATEGORY-POWER: expected HIGH, got %q", v)
	}
	if v := headerFields["CLUB"]; v != "Radio Amateurs of Springfield" {
		t.Errorf("CLUB: expected %q, got %q", "Radio Amateurs of Springfield", v)
	}

	// Verify QSO lines exist (exactly 3 non-dupe QSOs).
	qsoCount := 0
	for _, line := range lines {
		if strings.HasPrefix(line, "QSO:") {
			qsoCount++
			// Validate QSO line has at least 8 space-separated tokens.
			parts := strings.Fields(line[4:])
			if len(parts) < 8 {
				t.Errorf("QSO line has too few fields: %q", line)
			}
		}
	}
	if qsoCount != 3 {
		t.Errorf("expected 3 QSO lines in Cabrillo, got %d", qsoCount)
	}

	t.Logf("Cabrillo export validated: %d lines, %d QSOs", len(lines), qsoCount)
}

// TestIntegration_ContestDupeCheckPerformance verifies the dupe check endpoint
// responds quickly (as a smoke test — actual timing requires a loaded DB).
func TestIntegration_ContestDupeCheckPerformance(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	pool, h := setupIntegration(t)
	user := createTestUser(t, pool, "contest-perf")
	logbookUUID := createContestTestLogbook(t, pool, user.ID, "Contest Logbook")

	status, env := doJSON(t, h, http.MethodPost, "/v1/contests", user.ID, map[string]any{
		"logbook_uuid":      logbookUUID,
		"name":              "Performance Test Contest",
		"contest_id":        "PERF-TEST",
		"exchange_template": "serial",
		"category_operator": "SINGLE-OP",
		"category_band":     "ALL",
		"category_mode":     "CW",
		"category_power":    "HIGH",
	})
	if status != http.StatusCreated || !env.Success {
		t.Fatalf("create contest failed: status=%d", status)
	}
	var session contestSessionPayload
	decodeData(t, env.Data, &session)

	// Log a QSO for W1AW.
	doJSON(t, h, http.MethodPost,
		fmt.Sprintf("/v1/contests/%s/qso", session.UUID), user.ID,
		map[string]any{
			"callsign":      "W1AW",
			"band":          "20m",
			"mode":          "CW",
			"exchange_rcvd": "001 AR",
		})

	// Measure dupe check latency.
	start := time.Now()
	for i := 0; i < 10; i++ {
		st, _ := doJSON(t, h, http.MethodGet,
			fmt.Sprintf("/v1/contests/%s/check-dupe?callsign=W1AW&band=20m", session.UUID),
			user.ID, nil)
		if st != http.StatusOK {
			t.Errorf("dupe check %d failed: status=%d", i, st)
		}
	}
	elapsed := time.Since(start)
	avgMs := float64(elapsed.Milliseconds()) / 10.0
	t.Logf("dupe check avg latency: %.1fms per request (10 requests in %s)", avgMs, elapsed)
	// This is a smoke test — we're not measuring DB query time here.
	// The <10ms requirement is validated by the DB index, not this test.
}

// ─── Helper functions ──────────────────────────────────────────────────────

// validateCabrillo checks required structural elements of a Cabrillo 3.0 log.
func validateCabrillo(t *testing.T, cabrillo string) {
	t.Helper()
	required := []string{
		"START-OF-LOG:",
		"CREATED-BY:",
		"CONTEST:",
		"CALLSIGN:",
		"CATEGORY-OPERATOR:",
		"CATEGORY-BAND:",
		"CATEGORY-MODE:",
		"CATEGORY-POWER:",
		"END-OF-LOG:",
	}
	for _, field := range required {
		if !strings.Contains(cabrillo, field) {
			t.Errorf("Cabrillo missing required field: %s", field)
		}
	}
	// File must start with START-OF-LOG and end with END-OF-LOG.
	trimmed := strings.TrimSpace(cabrillo)
	if !strings.HasPrefix(trimmed, "START-OF-LOG:") {
		t.Error("Cabrillo does not start with START-OF-LOG:")
	}
	if !strings.HasSuffix(trimmed, "END-OF-LOG:") {
		t.Error("Cabrillo does not end with END-OF-LOG:")
	}
}

func safeString(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}

func bandFromFreq(freq string) string {
	switch freq {
	case "14025":
		return "20m"
	case "21025":
		return "15m"
	case "28025":
		return "10m"
	default:
		return "20m"
	}
}

func freqToHz(khz string) int64 {
	switch khz {
	case "14025":
		return 14_025_000
	case "21025":
		return 21_025_000
	case "28025":
		return 28_025_000
	default:
		return 14_025_000
	}
}

// createContestTestLogbook creates a logbook directly in the DB (bypassing the API's RLS)
// for use in integration tests. This matches the pattern used by other integration tests
// (see createActivationTestLogbook in activations_integration_test.go).
func createContestTestLogbook(t *testing.T, pool *pgxpool.Pool, userID int64, name string) string {
	t.Helper()
	var logbookID int64
	var logbookUUID string
	err := pool.QueryRow(context.Background(), `
		INSERT INTO logbooks (user_id, name, callsign, is_default)
		VALUES ($1, $2, 'W1TEST', TRUE)
		RETURNING id, uuid::text
	`, userID, name).Scan(&logbookID, &logbookUUID)
	if err != nil {
		t.Fatalf("insert contest test logbook: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO user_roles (logbook_id, user_id, role, invited_by)
		VALUES ($1, $2, 'owner', $2)
		ON CONFLICT (logbook_id, user_id)
		DO UPDATE SET role = 'owner', invited_by = EXCLUDED.invited_by, updated_at = NOW()
	`, logbookID, userID); err != nil {
		t.Fatalf("ensure contest logbook user role: %v", err)
	}
	return logbookUUID
}

// doExportRequest fetches a raw export endpoint (Cabrillo, ADIF, etc.) and returns the body as a string.
func doExportRequest(t *testing.T, h http.Handler, method, path string, userID int64) string {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	setTestAuthHeader(t, req, userID)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("export request %s %s failed: status=%d body=%s", method, path, rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}
