package handler_test

import (
	"net/http"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// /v1/stats/overview
// ---------------------------------------------------------------------------

func TestIntegration_StatsOverview(t *testing.T) {
	pool, h := setupIntegration(t)
	user := createTestUser(t, pool, "stats-overview")
	logbookUUID := createLogbookViaAPI(t, h, user.ID, "Overview Logbook", true)
	logbookID := lookupLogbookID(t, pool, logbookUUID)

	insertQSOForTests(t, pool, logbookID, qsoSeed{
		Callsign: "K1AAA",
		Band:     "20m",
		Mode:     "FT8",
		DateTime: time.Date(2024, 3, 1, 10, 0, 0, 0, time.UTC),
		Dxcc:     int32Ptr(291),
		Country:  strPtr("United States"),
		Grid:     strPtr("EM10"),
	})
	insertQSOForTests(t, pool, logbookID, qsoSeed{
		Callsign: "VE3BBB",
		Band:     "40m",
		Mode:     "CW",
		DateTime: time.Date(2024, 6, 15, 14, 0, 0, 0, time.UTC),
		Dxcc:     int32Ptr(1),
		Country:  strPtr("Canada"),
		Grid:     strPtr("FN03"),
	})

	status, env := doJSON(t, h, http.MethodGet, "/v1/stats/overview", user.ID, nil)
	if status != http.StatusOK || !env.Success {
		t.Fatalf("overview failed: status=%d success=%v error=%q", status, env.Success, env.Error)
	}

	type overviewResp struct {
		TotalQSOs       int64  `json:"total_qsos"`
		UniqueCallsigns int64  `json:"unique_callsigns"`
		UniqueCountries int64  `json:"unique_countries"`
		UniqueGrids     int64  `json:"unique_grids"`
		BandsUsed       int64  `json:"bands_used"`
		ModesUsed       int64  `json:"modes_used"`
		FirstQSO        string `json:"first_qso"`
	}

	var resp overviewResp
	decodeData(t, env.Data, &resp)

	if resp.TotalQSOs != 2 {
		t.Fatalf("expected total_qsos=2, got %d", resp.TotalQSOs)
	}
	if resp.UniqueCallsigns != 2 {
		t.Fatalf("expected unique_callsigns=2, got %d", resp.UniqueCallsigns)
	}
	if resp.UniqueCountries != 2 {
		t.Fatalf("expected unique_countries=2, got %d", resp.UniqueCountries)
	}
	if resp.UniqueGrids != 2 {
		t.Fatalf("expected unique_grids=2, got %d", resp.UniqueGrids)
	}
	if resp.BandsUsed != 2 {
		t.Fatalf("expected bands_used=2, got %d", resp.BandsUsed)
	}
	if resp.ModesUsed != 2 {
		t.Fatalf("expected modes_used=2, got %d", resp.ModesUsed)
	}
	if resp.FirstQSO != "2024-03-01T10:00:00Z" {
		t.Fatalf("expected first_qso=2024-03-01T10:00:00Z, got %q", resp.FirstQSO)
	}
}

// ---------------------------------------------------------------------------
// /v1/stats/by-band
// ---------------------------------------------------------------------------

func TestIntegration_StatsByBand(t *testing.T) {
	pool, h := setupIntegration(t)
	user := createTestUser(t, pool, "stats-by-band")
	logbookUUID := createLogbookViaAPI(t, h, user.ID, "Band Logbook", true)
	logbookID := lookupLogbookID(t, pool, logbookUUID)

	for i := 0; i < 3; i++ {
		insertQSOForTests(t, pool, logbookID, qsoSeed{
			Callsign: "W1AA",
			Band:     "20m",
			Mode:     "FT8",
			DateTime: time.Date(2024, 1, i+1, 10, 0, 0, 0, time.UTC),
		})
	}
	insertQSOForTests(t, pool, logbookID, qsoSeed{
		Callsign: "W1BB",
		Band:     "40m",
		Mode:     "CW",
		DateTime: time.Date(2024, 1, 10, 10, 0, 0, 0, time.UTC),
	})

	status, env := doJSON(t, h, http.MethodGet, "/v1/stats/by-band", user.ID, nil)
	if status != http.StatusOK || !env.Success {
		t.Fatalf("by-band failed: status=%d success=%v error=%q", status, env.Success, env.Error)
	}

	type bandEntry struct {
		Band  string `json:"band"`
		Count int64  `json:"count"`
	}
	var entries []bandEntry
	decodeData(t, env.Data, &entries)

	if len(entries) != 2 {
		t.Fatalf("expected 2 band entries, got %d", len(entries))
	}
	if entries[0].Band != "20m" || entries[0].Count != 3 {
		t.Fatalf("expected 20m:3, got %+v", entries[0])
	}
	if entries[1].Band != "40m" || entries[1].Count != 1 {
		t.Fatalf("expected 40m:1, got %+v", entries[1])
	}
}

// ---------------------------------------------------------------------------
// /v1/stats/by-mode
// ---------------------------------------------------------------------------

func TestIntegration_StatsByMode(t *testing.T) {
	pool, h := setupIntegration(t)
	user := createTestUser(t, pool, "stats-by-mode")
	logbookUUID := createLogbookViaAPI(t, h, user.ID, "Mode Logbook", true)
	logbookID := lookupLogbookID(t, pool, logbookUUID)

	for i := 0; i < 2; i++ {
		insertQSOForTests(t, pool, logbookID, qsoSeed{
			Callsign: "W1AA",
			Band:     "20m",
			Mode:     "FT8",
			DateTime: time.Date(2024, 1, i+1, 10, 0, 0, 0, time.UTC),
		})
	}
	insertQSOForTests(t, pool, logbookID, qsoSeed{
		Callsign: "W1BB",
		Band:     "40m",
		Mode:     "SSB",
		DateTime: time.Date(2024, 1, 5, 10, 0, 0, 0, time.UTC),
	})

	status, env := doJSON(t, h, http.MethodGet, "/v1/stats/by-mode", user.ID, nil)
	if status != http.StatusOK || !env.Success {
		t.Fatalf("by-mode failed: status=%d success=%v error=%q", status, env.Success, env.Error)
	}

	type modeEntry struct {
		Mode  string `json:"mode"`
		Count int64  `json:"count"`
	}
	var entries []modeEntry
	decodeData(t, env.Data, &entries)

	if len(entries) != 2 {
		t.Fatalf("expected 2 mode entries, got %d", len(entries))
	}
	if entries[0].Mode != "FT8" || entries[0].Count != 2 {
		t.Fatalf("expected FT8:2, got %+v", entries[0])
	}
	if entries[1].Mode != "SSB" || entries[1].Count != 1 {
		t.Fatalf("expected SSB:1, got %+v", entries[1])
	}
}

// ---------------------------------------------------------------------------
// /v1/stats/by-period
// ---------------------------------------------------------------------------

func TestIntegration_StatsByPeriod(t *testing.T) {
	pool, h := setupIntegration(t)
	user := createTestUser(t, pool, "stats-by-period")
	logbookUUID := createLogbookViaAPI(t, h, user.ID, "Period Logbook", true)
	logbookID := lookupLogbookID(t, pool, logbookUUID)

	insertQSOForTests(t, pool, logbookID, qsoSeed{
		Callsign: "K1AA",
		Band:     "20m",
		Mode:     "FT8",
		DateTime: time.Date(2024, 1, 10, 10, 0, 0, 0, time.UTC),
	})
	insertQSOForTests(t, pool, logbookID, qsoSeed{
		Callsign: "K1BB",
		Band:     "20m",
		Mode:     "FT8",
		DateTime: time.Date(2024, 3, 15, 10, 0, 0, 0, time.UTC),
	})
	insertQSOForTests(t, pool, logbookID, qsoSeed{
		Callsign: "K1CC",
		Band:     "20m",
		Mode:     "FT8",
		DateTime: time.Date(2025, 1, 5, 10, 0, 0, 0, time.UTC),
	})

	// Test month grouping.
	status, env := doJSON(t, h, http.MethodGet, "/v1/stats/by-period?period=month", user.ID, nil)
	if status != http.StatusOK || !env.Success {
		t.Fatalf("by-period month failed: status=%d success=%v error=%q", status, env.Success, env.Error)
	}

	type periodEntry struct {
		Period string `json:"period"`
		Count  int64  `json:"count"`
	}
	var monthEntries []periodEntry
	decodeData(t, env.Data, &monthEntries)

	if len(monthEntries) != 3 {
		t.Fatalf("expected 3 month entries, got %d", len(monthEntries))
	}
	if monthEntries[0].Period != "2024-01" || monthEntries[0].Count != 1 {
		t.Fatalf("expected 2024-01:1, got %+v", monthEntries[0])
	}

	// Test year grouping.
	status, env = doJSON(t, h, http.MethodGet, "/v1/stats/by-period?period=year", user.ID, nil)
	if status != http.StatusOK || !env.Success {
		t.Fatalf("by-period year failed: status=%d success=%v error=%q", status, env.Success, env.Error)
	}

	var yearEntries []periodEntry
	decodeData(t, env.Data, &yearEntries)

	if len(yearEntries) != 2 {
		t.Fatalf("expected 2 year entries, got %d", len(yearEntries))
	}
	if yearEntries[0].Period != "2024" || yearEntries[0].Count != 2 {
		t.Fatalf("expected 2024:2, got %+v", yearEntries[0])
	}
	if yearEntries[1].Period != "2025" || yearEntries[1].Count != 1 {
		t.Fatalf("expected 2025:1, got %+v", yearEntries[1])
	}

	// Test invalid group parameter.
	status, env = doJSON(t, h, http.MethodGet, "/v1/stats/by-period?period=day", user.ID, nil)
	if status != http.StatusBadRequest || env.Success {
		t.Fatalf("expected 400 for invalid group, got status=%d success=%v", status, env.Success)
	}
}

// ---------------------------------------------------------------------------
// /v1/stats/countries-over-time
// ---------------------------------------------------------------------------

func TestIntegration_StatsCountriesOverTime(t *testing.T) {
	pool, h := setupIntegration(t)
	user := createTestUser(t, pool, "stats-cot")
	logbookUUID := createLogbookViaAPI(t, h, user.ID, "COT Logbook", true)
	logbookID := lookupLogbookID(t, pool, logbookUUID)

	insertQSOForTests(t, pool, logbookID, qsoSeed{
		Callsign: "K1AA",
		Band:     "20m",
		Mode:     "FT8",
		DateTime: time.Date(2024, 1, 10, 10, 0, 0, 0, time.UTC),
		Country:  strPtr("United States"),
	})
	insertQSOForTests(t, pool, logbookID, qsoSeed{
		Callsign: "VE3BB",
		Band:     "20m",
		Mode:     "FT8",
		DateTime: time.Date(2024, 1, 20, 10, 0, 0, 0, time.UTC),
		Country:  strPtr("Canada"),
	})
	insertQSOForTests(t, pool, logbookID, qsoSeed{
		Callsign: "DL1CC",
		Band:     "20m",
		Mode:     "FT8",
		DateTime: time.Date(2024, 2, 5, 10, 0, 0, 0, time.UTC),
		Country:  strPtr("Germany"),
	})

	status, env := doJSON(t, h, http.MethodGet, "/v1/stats/countries-over-time", user.ID, nil)
	if status != http.StatusOK || !env.Success {
		t.Fatalf("countries-over-time failed: status=%d success=%v error=%q", status, env.Success, env.Error)
	}

	type cotEntry struct {
		Period          string `json:"period"`
		UniqueCountries int64  `json:"unique_countries"`
	}
	var entries []cotEntry
	decodeData(t, env.Data, &entries)

	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].Period != "2024-01" || entries[0].UniqueCountries != 2 {
		t.Fatalf("expected 2024-01:2 countries, got %+v", entries[0])
	}
	if entries[1].Period != "2024-02" || entries[1].UniqueCountries != 3 {
		t.Fatalf("expected 2024-02:3 countries (cumulative), got %+v", entries[1])
	}
}

// ---------------------------------------------------------------------------
// /v1/stats/top-callsigns
// ---------------------------------------------------------------------------

func TestIntegration_StatsTopCallsigns(t *testing.T) {
	pool, h := setupIntegration(t)
	user := createTestUser(t, pool, "stats-top-cs")
	logbookUUID := createLogbookViaAPI(t, h, user.ID, "TopCS Logbook", true)
	logbookID := lookupLogbookID(t, pool, logbookUUID)

	for i := 0; i < 3; i++ {
		insertQSOForTests(t, pool, logbookID, qsoSeed{
			Callsign: "K1AAA",
			Band:     "20m",
			Mode:     "FT8",
			DateTime: time.Date(2024, 1, i+1, 10, 0, 0, 0, time.UTC),
		})
	}
	insertQSOForTests(t, pool, logbookID, qsoSeed{
		Callsign: "VE3BBB",
		Band:     "20m",
		Mode:     "FT8",
		DateTime: time.Date(2024, 1, 10, 10, 0, 0, 0, time.UTC),
	})

	status, env := doJSON(t, h, http.MethodGet, "/v1/stats/top-callsigns?limit=5", user.ID, nil)
	if status != http.StatusOK || !env.Success {
		t.Fatalf("top-callsigns failed: status=%d success=%v error=%q", status, env.Success, env.Error)
	}

	type csEntry struct {
		Callsign string `json:"callsign"`
		Count    int64  `json:"count"`
	}
	var entries []csEntry
	decodeData(t, env.Data, &entries)

	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].Callsign != "K1AAA" || entries[0].Count != 3 {
		t.Fatalf("expected K1AAA:3, got %+v", entries[0])
	}

	// Test invalid limit.
	status, env = doJSON(t, h, http.MethodGet, "/v1/stats/top-callsigns?limit=999", user.ID, nil)
	if status != http.StatusBadRequest || env.Success {
		t.Fatalf("expected 400 for limit=999, got status=%d success=%v", status, env.Success)
	}
}

// ---------------------------------------------------------------------------
// /v1/stats/top-countries
// ---------------------------------------------------------------------------

func TestIntegration_StatsTopCountries(t *testing.T) {
	pool, h := setupIntegration(t)
	user := createTestUser(t, pool, "stats-top-ctry")
	logbookUUID := createLogbookViaAPI(t, h, user.ID, "TopCtry Logbook", true)
	logbookID := lookupLogbookID(t, pool, logbookUUID)

	for i := 0; i < 5; i++ {
		insertQSOForTests(t, pool, logbookID, qsoSeed{
			Callsign: "K1AA",
			Band:     "20m",
			Mode:     "FT8",
			DateTime: time.Date(2024, 1, i+1, 10, 0, 0, 0, time.UTC),
			Country:  strPtr("United States"),
		})
	}
	insertQSOForTests(t, pool, logbookID, qsoSeed{
		Callsign: "DL1BB",
		Band:     "20m",
		Mode:     "FT8",
		DateTime: time.Date(2024, 2, 1, 10, 0, 0, 0, time.UTC),
		Country:  strPtr("Germany"),
	})

	status, env := doJSON(t, h, http.MethodGet, "/v1/stats/top-countries?limit=10", user.ID, nil)
	if status != http.StatusOK || !env.Success {
		t.Fatalf("top-countries failed: status=%d success=%v error=%q", status, env.Success, env.Error)
	}

	type ctryEntry struct {
		Name  string `json:"name"`
		Count int64  `json:"count"`
	}
	var entries []ctryEntry
	decodeData(t, env.Data, &entries)

	if len(entries) != 2 {
		t.Fatalf("expected 2 country entries, got %d", len(entries))
	}
	if entries[0].Name != "United States" || entries[0].Count != 5 {
		t.Fatalf("expected United States:5, got %+v", entries[0])
	}
}

// ---------------------------------------------------------------------------
// /v1/stats/operating-patterns
// ---------------------------------------------------------------------------

func TestIntegration_StatsOperatingPatterns(t *testing.T) {
	pool, h := setupIntegration(t)
	user := createTestUser(t, pool, "stats-patterns")
	logbookUUID := createLogbookViaAPI(t, h, user.ID, "Patterns Logbook", true)
	logbookID := lookupLogbookID(t, pool, logbookUUID)

	// Saturday (DOW=6) at 14:00 UTC.
	insertQSOForTests(t, pool, logbookID, qsoSeed{
		Callsign: "K1AA",
		Band:     "20m",
		Mode:     "FT8",
		DateTime: time.Date(2024, 1, 6, 14, 0, 0, 0, time.UTC), // Sat
	})
	// Saturday (DOW=6) at 14:00 UTC.
	insertQSOForTests(t, pool, logbookID, qsoSeed{
		Callsign: "W1BB",
		Band:     "20m",
		Mode:     "FT8",
		DateTime: time.Date(2024, 1, 13, 14, 0, 0, 0, time.UTC), // Sat
	})
	// Sunday (DOW=0) at 10:00 UTC.
	insertQSOForTests(t, pool, logbookID, qsoSeed{
		Callsign: "VE3CC",
		Band:     "40m",
		Mode:     "CW",
		DateTime: time.Date(2024, 1, 7, 10, 0, 0, 0, time.UTC), // Sun
	})

	status, env := doJSON(t, h, http.MethodGet, "/v1/stats/operating-patterns", user.ID, nil)
	if status != http.StatusOK || !env.Success {
		t.Fatalf("operating-patterns failed: status=%d success=%v error=%q", status, env.Success, env.Error)
	}

	type patternEntry struct {
		DayOfWeek int32 `json:"day_of_week"`
		HourOfDay int32 `json:"hour_of_day"`
		Count     int64 `json:"count"`
	}
	var entries []patternEntry
	decodeData(t, env.Data, &entries)

	if len(entries) != 2 {
		t.Fatalf("expected 2 pattern entries (Sun@10 and Sat@14), got %d", len(entries))
	}

	// DOW=0 (Sun) at hour 10 with count 1.
	found0 := false
	// DOW=6 (Sat) at hour 14 with count 2.
	found6 := false
	for _, e := range entries {
		if e.DayOfWeek == 0 && e.HourOfDay == 10 && e.Count == 1 {
			found0 = true
		}
		if e.DayOfWeek == 6 && e.HourOfDay == 14 && e.Count == 2 {
			found6 = true
		}
	}
	if !found0 {
		t.Fatalf("missing Sun@10:1 entry; entries=%+v", entries)
	}
	if !found6 {
		t.Fatalf("missing Sat@14:2 entry; entries=%+v", entries)
	}

	status, env = doJSON(t, h, http.MethodGet, "/v1/stats/activity-heatmap", user.ID, nil)
	if status != http.StatusOK || !env.Success {
		t.Fatalf("activity-heatmap failed: status=%d success=%v error=%q", status, env.Success, env.Error)
	}

	var aliasEntries []patternEntry
	decodeData(t, env.Data, &aliasEntries)
	if len(aliasEntries) != len(entries) {
		t.Fatalf("expected alias endpoint to return %d entries, got %d", len(entries), len(aliasEntries))
	}
}

// ---------------------------------------------------------------------------
// RLS isolation — stats endpoints must not leak data across users
// ---------------------------------------------------------------------------

func TestIntegration_StatsIsolation(t *testing.T) {
	pool, h := setupIntegration(t)
	userA := createTestUser(t, pool, "stats-iso-a")
	userB := createTestUser(t, pool, "stats-iso-b")

	logbookA := createLogbookViaAPI(t, h, userA.ID, "User A Logbook", true)
	logbookAID := lookupLogbookID(t, pool, logbookA)
	logbookB := createLogbookViaAPI(t, h, userB.ID, "User B Logbook", true)
	logbookBID := lookupLogbookID(t, pool, logbookB)

	for i := 0; i < 5; i++ {
		insertQSOForTests(t, pool, logbookAID, qsoSeed{
			Callsign: "K1AA",
			Band:     "20m",
			Mode:     "FT8",
			DateTime: time.Date(2024, 1, i+1, 10, 0, 0, 0, time.UTC),
			Country:  strPtr("United States"),
		})
	}
	for i := 0; i < 2; i++ {
		insertQSOForTests(t, pool, logbookBID, qsoSeed{
			Callsign: "DL1BB",
			Band:     "40m",
			Mode:     "CW",
			DateTime: time.Date(2024, 1, i+1, 12, 0, 0, 0, time.UTC),
			Country:  strPtr("Germany"),
		})
	}

	// User A should see 5 QSOs.
	statusA, envA := doJSON(t, h, http.MethodGet, "/v1/stats/overview", userA.ID, nil)
	if statusA != http.StatusOK || !envA.Success {
		t.Fatalf("overview for user A failed: status=%d", statusA)
	}
	type overviewResp struct {
		TotalQSOs int64 `json:"total_qsos"`
	}
	var respA overviewResp
	decodeData(t, envA.Data, &respA)
	if respA.TotalQSOs != 5 {
		t.Fatalf("user A: expected 5 QSOs, got %d", respA.TotalQSOs)
	}

	// User B should see 2 QSOs.
	statusB, envB := doJSON(t, h, http.MethodGet, "/v1/stats/overview", userB.ID, nil)
	if statusB != http.StatusOK || !envB.Success {
		t.Fatalf("overview for user B failed: status=%d", statusB)
	}
	var respB overviewResp
	decodeData(t, envB.Data, &respB)
	if respB.TotalQSOs != 2 {
		t.Fatalf("user B: expected 2 QSOs, got %d", respB.TotalQSOs)
	}
}
