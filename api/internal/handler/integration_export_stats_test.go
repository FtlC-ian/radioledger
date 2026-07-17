package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/FtlC-ian/radioledger/api/internal/jobs"
	adifpkg "github.com/FtlC-ian/radioledger/pkg/adif"
)

type statsPayload struct {
	TotalQSOs       int64             `json:"total_qsos"`
	UniqueCallsigns int64             `json:"unique_callsigns"`
	UniqueCountries int64             `json:"unique_countries"`
	UniqueGrids     int64             `json:"unique_grids"`
	Bands           map[string]int64  `json:"bands"`
	Modes           map[string]int64  `json:"modes"`
	TopCountries    []countryCountDTO `json:"top_countries"`
	QSOsByYear      map[string]int64  `json:"qsos_by_year"`
	FirstQSO        string            `json:"first_qso"`
	LastQSO         string            `json:"last_qso"`
}

type countryCountDTO struct {
	Name  string `json:"name"`
	Count int64  `json:"count"`
}

func TestIntegration_ADIFExport_AllAndBandFilter(t *testing.T) {
	pool, h := setupIntegration(t)
	user := createTestUser(t, pool, "adif-export")
	logbookUUID := createLogbookViaAPI(t, h, user.ID, "Export Logbook", true)
	logbookID := lookupLogbookID(t, pool, logbookUUID)

	insertQSOForTests(t, pool, logbookID, qsoSeed{
		Callsign: "K1AAA",
		Band:     "20m",
		Mode:     "FT8",
		DateTime: time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
		Dxcc:     int32Ptr(291),
		Country:  strPtr("United States"),
		Grid:     strPtr("EM10"),
		Extra: map[string]string{
			"APP_TEST_TAG": "ALPHA",
			"X_NOTE":       "hello",
		},
	})
	insertQSOForTests(t, pool, logbookID, qsoSeed{
		Callsign: "DL1BBB",
		Band:     "40m",
		Mode:     "CW",
		DateTime: time.Date(2024, 2, 11, 12, 0, 0, 0, time.UTC),
		Dxcc:     int32Ptr(230),
		Country:  strPtr("Germany"),
		Grid:     strPtr("JO62"),
	})
	insertQSOForTests(t, pool, logbookID, qsoSeed{
		Callsign: "JA1CCC",
		Band:     "20m",
		Mode:     "SSB",
		DateTime: time.Date(2025, 1, 3, 18, 15, 0, 0, time.UTC),
		Dxcc:     int32Ptr(339),
		Country:  strPtr("Japan"),
		Grid:     strPtr("PM95"),
	})

	status, headers, body := exportADIF(t, h, user.ID, "/v1/export/adif?logbook_uuid="+logbookUUID)
	if status != http.StatusOK {
		t.Fatalf("export all failed: status=%d body=%s", status, string(body))
	}
	if disp := headers.Get("Content-Disposition"); !strings.Contains(disp, "attachment") || !strings.Contains(disp, ".adi") {
		t.Fatalf("expected attachment content-disposition, got %q", disp)
	}

	header, records, err := adifpkg.ParseBytes(context.Background(), body)
	if err != nil {
		t.Fatalf("parse exported adif: %v\nbody:\n%s", err, string(body))
	}
	if header.ADIFVersion() != "3.1.7" {
		t.Fatalf("expected ADIF_VER=3.1.7, got %q", header.ADIFVersion())
	}
	if header.ProgramID() != "RadioLedger" {
		t.Fatalf("expected PROGRAMID=RadioLedger, got %q", header.ProgramID())
	}
	if header.ProgramVersion() != "0.1.0" {
		t.Fatalf("expected PROGRAMVERSION=0.1.0, got %q", header.ProgramVersion())
	}
	if len(records) != 3 {
		t.Fatalf("expected 3 exported records, got %d", len(records))
	}

	var foundExtra bool
	for _, rec := range records {
		if rec.Get("APP_TEST_TAG") == "ALPHA" && rec.Get("X_NOTE") == "hello" {
			foundExtra = true
			break
		}
	}
	if !foundExtra {
		t.Fatal("expected APP_TEST_TAG/X_NOTE from extra JSONB in exported ADIF")
	}

	params := url.Values{}
	params.Set("logbook_uuid", logbookUUID)
	params.Set("band", "20m")
	status, _, body = exportADIF(t, h, user.ID, "/v1/export/adif?"+params.Encode())
	if status != http.StatusOK {
		t.Fatalf("band-filter export failed: status=%d body=%s", status, string(body))
	}

	_, filtered, err := adifpkg.ParseBytes(context.Background(), body)
	if err != nil {
		t.Fatalf("parse filtered adif: %v", err)
	}
	if len(filtered) != 2 {
		t.Fatalf("expected 2 filtered records, got %d", len(filtered))
	}
	for _, rec := range filtered {
		if rec.Get("BAND") != "20m" {
			t.Fatalf("expected BAND=20m, got %q", rec.Get("BAND"))
		}
	}
}

func TestIntegration_ADIFExport_RoundTripReimport(t *testing.T) {
	pool, h := setupIntegration(t)
	user := createTestUser(t, pool, "adif-roundtrip")
	sourceLogbook := createLogbookViaAPI(t, h, user.ID, "Roundtrip Source", true)
	targetLogbook := createLogbookViaAPI(t, h, user.ID, "Roundtrip Target", false)

	sourceLogbookID := lookupLogbookID(t, pool, sourceLogbook)
	targetLogbookID := lookupLogbookID(t, pool, targetLogbook)

	insertQSOForTests(t, pool, sourceLogbookID, qsoSeed{
		Callsign: "W1AAA",
		Band:     "10m",
		Mode:     "FT8",
		DateTime: time.Date(2024, 5, 5, 1, 2, 3, 0, time.UTC),
		Dxcc:     int32Ptr(291),
		Country:  strPtr("United States"),
		Grid:     strPtr("EM10"),
		Extra: map[string]string{
			"APP_CUSTOM": "VALUE-1",
		},
	})
	insertQSOForTests(t, pool, sourceLogbookID, qsoSeed{
		Callsign: "VE3BBB",
		Band:     "20m",
		Mode:     "CW",
		DateTime: time.Date(2024, 6, 6, 2, 3, 4, 0, time.UTC),
		Dxcc:     int32Ptr(1),
		Country:  strPtr("Canada"),
		Grid:     strPtr("FN03"),
	})

	status, _, exported := exportADIF(t, h, user.ID, "/v1/export/adif?logbook_uuid="+sourceLogbook)
	if status != http.StatusOK {
		t.Fatalf("roundtrip export failed: status=%d body=%s", status, string(exported))
	}

	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "roundtrip.adi")
	if err := os.WriteFile(tmpFile, exported, 0o600); err != nil {
		t.Fatalf("write temp export file: %v", err)
	}

	status, env := uploadADIF(t, h, user.ID, targetLogbook, tmpFile)
	if status != http.StatusAccepted || !env.Success {
		t.Fatalf("upload exported adif failed: status=%d success=%v error=%q", status, env.Success, env.Error)
	}

	var uploadResp importJobPayload
	decodeData(t, env.Data, &uploadResp)

	var importJobID int64
	err := pool.QueryRow(context.Background(), `SELECT id FROM import_jobs WHERE uuid = $1`, uploadResp.JobUUID).Scan(&importJobID)
	if err != nil {
		t.Fatalf("lookup import job id: %v", err)
	}

	w := &jobs.ADIFImportWorker{Pool: pool}
	riverJob := &river.Job[jobs.ADIFImportArgs]{
		Args: jobs.ADIFImportArgs{
			ImportJobID: importJobID,
			FilePath:    tmpFile,
			LogbookID:   targetLogbookID,
			UserID:      user.ID,
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := w.Work(ctx, riverJob); err != nil {
		t.Fatalf("run roundtrip worker: %v", err)
	}

	sourceCount := countQSOsByLogbookID(t, pool, sourceLogbookID)
	targetCount := countQSOsByLogbookID(t, pool, targetLogbookID)
	if sourceCount != targetCount {
		t.Fatalf("roundtrip count mismatch: source=%d target=%d", sourceCount, targetCount)
	}

	var appCustom string
	err = pool.QueryRow(context.Background(), `
		SELECT extra->>'APP_CUSTOM'
		FROM qsos
		WHERE logbook_id = $1
		ORDER BY datetime_on ASC
		LIMIT 1
	`, targetLogbookID).Scan(&appCustom)
	if err != nil {
		t.Fatalf("query roundtrip extra field: %v", err)
	}
	if appCustom != "VALUE-1" {
		t.Fatalf("expected APP_CUSTOM to roundtrip, got %q", appCustom)
	}
}

func TestIntegration_StatsEndpoint(t *testing.T) {
	pool, h := setupIntegration(t)
	user := createTestUser(t, pool, "stats")
	logbookUUID := createLogbookViaAPI(t, h, user.ID, "Stats Logbook", true)
	logbookID := lookupLogbookID(t, pool, logbookUUID)

	insertQSOForTests(t, pool, logbookID, qsoSeed{
		Callsign: "K1AAA",
		Band:     "20m",
		Mode:     "FT8",
		DateTime: time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
		Dxcc:     int32Ptr(291),
		Country:  strPtr("United States"),
		Grid:     strPtr("EM10"),
	})
	insertQSOForTests(t, pool, logbookID, qsoSeed{
		Callsign: "VE3BBB",
		Band:     "20m",
		Mode:     "CW",
		DateTime: time.Date(2024, 7, 20, 12, 0, 0, 0, time.UTC),
		Dxcc:     int32Ptr(1),
		Country:  strPtr("Canada"),
		Grid:     strPtr("FN03"),
	})
	insertQSOForTests(t, pool, logbookID, qsoSeed{
		Callsign: "K1AAA",
		Band:     "10m",
		Mode:     "FT8",
		DateTime: time.Date(2025, 3, 10, 6, 30, 0, 0, time.UTC),
		Dxcc:     int32Ptr(291),
		Country:  strPtr("United States"),
		Grid:     strPtr("EM11"),
	})
	insertQSOForTests(t, pool, logbookID, qsoSeed{
		Callsign: "DL1CCC",
		Band:     "40m",
		Mode:     "FT8",
		DateTime: time.Date(2025, 12, 11, 22, 45, 0, 0, time.UTC),
		Dxcc:     int32Ptr(230),
		Country:  strPtr("Germany"),
		Grid:     strPtr("JO62"),
	})

	status, env := doJSON(t, h, http.MethodGet, "/v1/stats", user.ID, nil)
	if status != http.StatusOK || !env.Success {
		t.Fatalf("stats endpoint failed: status=%d success=%v error=%q", status, env.Success, env.Error)
	}

	var payload statsPayload
	decodeData(t, env.Data, &payload)

	if payload.TotalQSOs != 4 {
		t.Fatalf("expected total_qsos=4, got %d", payload.TotalQSOs)
	}
	if payload.UniqueCallsigns != 3 {
		t.Fatalf("expected unique_callsigns=3, got %d", payload.UniqueCallsigns)
	}
	if payload.UniqueCountries != 3 {
		t.Fatalf("expected unique_countries=3, got %d", payload.UniqueCountries)
	}
	if payload.UniqueGrids != 4 {
		t.Fatalf("expected unique_grids=4, got %d", payload.UniqueGrids)
	}

	if payload.Bands["20m"] != 2 || payload.Bands["10m"] != 1 || payload.Bands["40m"] != 1 {
		t.Fatalf("unexpected band counts: %+v", payload.Bands)
	}
	if payload.Modes["FT8"] != 3 || payload.Modes["CW"] != 1 {
		t.Fatalf("unexpected mode counts: %+v", payload.Modes)
	}

	if len(payload.TopCountries) == 0 || payload.TopCountries[0].Name != "United States" || payload.TopCountries[0].Count != 2 {
		t.Fatalf("unexpected top countries: %+v", payload.TopCountries)
	}
	if payload.QSOsByYear["2024"] != 2 || payload.QSOsByYear["2025"] != 2 {
		t.Fatalf("unexpected qsos_by_year: %+v", payload.QSOsByYear)
	}

	if payload.FirstQSO != "2024-01-15T00:00:00Z" {
		t.Fatalf("expected first_qso=2024-01-15T00:00:00Z, got %q", payload.FirstQSO)
	}
	if payload.LastQSO != "2025-12-11T22:45:00Z" {
		t.Fatalf("expected last_qso=2025-12-11T22:45:00Z, got %q", payload.LastQSO)
	}
}

type qsoSeed struct {
	Callsign string
	Band     string
	Mode     string
	Submode  *string
	DateTime time.Time
	Dxcc     *int32
	Country  *string
	Grid     *string
	Extra    map[string]string
}

func insertQSOForTests(t *testing.T, pool *pgxpool.Pool, logbookID int64, seed qsoSeed) {
	t.Helper()

	extra := seed.Extra
	if extra == nil {
		extra = map[string]string{}
	}
	extraJSON, err := json.Marshal(extra)
	if err != nil {
		t.Fatalf("marshal extra json: %v", err)
	}

	_, err = pool.Exec(context.Background(), `
		INSERT INTO qsos (
			logbook_id,
			callsign,
			band,
			mode,
			submode,
			datetime_on,
			dxcc,
			country,
			gridsquare,
			extra
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::jsonb)
	`,
		logbookID,
		strings.ToUpper(strings.TrimSpace(seed.Callsign)),
		seed.Band,
		seed.Mode,
		seed.Submode,
		seed.DateTime.UTC(),
		seed.Dxcc,
		seed.Country,
		seed.Grid,
		extraJSON,
	)
	if err != nil {
		t.Fatalf("insert qso seed: %v", err)
	}
}

func lookupLogbookID(t *testing.T, pool *pgxpool.Pool, logbookUUID string) int64 {
	t.Helper()

	var logbookID int64
	err := pool.QueryRow(context.Background(), `SELECT id FROM logbooks WHERE uuid = $1`, logbookUUID).Scan(&logbookID)
	if err != nil {
		t.Fatalf("lookup logbook id: %v", err)
	}
	return logbookID
}

func countQSOsByLogbookID(t *testing.T, pool *pgxpool.Pool, logbookID int64) int64 {
	t.Helper()

	var count int64
	err := pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM qsos WHERE logbook_id = $1 AND deleted_at IS NULL`, logbookID).Scan(&count)
	if err != nil {
		t.Fatalf("count qsos: %v", err)
	}
	return count
}

func exportADIF(t *testing.T, h http.Handler, userID int64, path string) (int, http.Header, []byte) {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, path, nil)
	setTestAuthHeader(t, req, userID)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	return rec.Code, rec.Header(), rec.Body.Bytes()
}

func strPtr(v string) *string {
	return &v
}

func int32Ptr(v int32) *int32 {
	return &v
}
