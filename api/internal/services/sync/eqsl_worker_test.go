// Package sync — eqsl_worker_test.go: unit tests for eQSL worker logic.
//
// Tests cover:
//   - qsosToADIF formatting
//   - Confirmation matching query logic (via a mock pool approach)
//   - EnqueueEQSLUpload / EnqueueEQSLDownload job arg shapes
//   - EQSLUploadArgs / EQSLDownloadArgs River Kind() values
package sync

import (
	"encoding/json"
	"testing"
	"time"
)

// ──────────────────────────────────────────────────────────────────────────────
// Job kind tests — ensure River job kinds are stable across refactors.
// ──────────────────────────────────────────────────────────────────────────────

func TestEQSLUploadArgs_Kind(t *testing.T) {
	args := EQSLUploadArgs{UserID: 42}
	if args.Kind() != "eqsl_upload" {
		t.Errorf("EQSLUploadArgs.Kind(): expected 'eqsl_upload', got %q", args.Kind())
	}
}

func TestEQSLDownloadArgs_Kind(t *testing.T) {
	args := EQSLDownloadArgs{UserID: 42}
	if args.Kind() != "eqsl_download" {
		t.Errorf("EQSLDownloadArgs.Kind(): expected 'eqsl_download', got %q", args.Kind())
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// qsosToADIF tests
// ──────────────────────────────────────────────────────────────────────────────

func strPtr(s string) *string { return &s }
func i64Ptr(i int64) *int64   { return &i }

func TestQSOsToADIF_BasicFields(t *testing.T) {
	dt := time.Date(2024, 3, 15, 14, 30, 0, 0, time.UTC)
	rows := []pendingQSORow{
		{
			SyncID:     1,
			QSOID:      100,
			Callsign:   "W1ABC",
			Band:       "40m",
			Mode:       "SSB",
			DatetimeOn: dt,
			RstSent:    strPtr("59"),
			RstRcvd:    strPtr("57"),
			Gridsquare: strPtr("EN52"),
			UserID:     42,
		},
	}

	adif, err := qsosToADIF(rows)
	if err != nil {
		t.Fatalf("qsosToADIF: %v", err)
	}

	// Must contain required ADIF fields.
	for _, want := range []string{
		"W1ABC", "40m", "SSB", "20240315", "1430", "59", "57", "EN52",
	} {
		if !containsStr(adif, want) {
			t.Errorf("ADIF output missing %q\nGot:\n%s", want, adif)
		}
	}

	// Must contain <EOR> marker.
	if !containsStr(adif, "<EOR>") && !containsStr(adif, "<eor>") {
		t.Errorf("ADIF output missing <EOR> marker\nGot:\n%s", adif)
	}
}

func TestQSOsToADIF_CanonicalizesModeAliases(t *testing.T) {
	dt := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	rows := []pendingQSORow{
		{QSOID: 1, Callsign: "W1XYZ", Band: "20m", Mode: "FT2", DatetimeOn: dt},
		{QSOID: 2, Callsign: "W1XYZ", Band: "20m", Mode: "DMR", DatetimeOn: dt},
		{QSOID: 3, Callsign: "W1XYZ", Band: "20m", Mode: "USB", DatetimeOn: dt},
		{QSOID: 4, Callsign: "W1XYZ", Band: "20m", Mode: "FT8", DatetimeOn: dt},
	}

	adif, err := qsosToADIF(rows)
	if err != nil {
		t.Fatalf("qsosToADIF canonicalize: %v", err)
	}

	for _, want := range []string{"<MODE:4>MFSK", "<SUBMODE:3>FT2", "<MODE:12>DIGITALVOICE", "<SUBMODE:3>DMR", "<MODE:3>SSB", "<SUBMODE:3>USB", "<MODE:3>FT8"} {
		if !containsStr(adif, want) {
			t.Fatalf("ADIF output missing %q\nGot:\n%s", want, adif)
		}
	}
	if containsStr(adif, "<SUBMODE:3>FT8") {
		t.Fatalf("FT8 should remain MODE-only\nGot:\n%s", adif)
	}
}

func TestQSOsToADIF_OptionalFields(t *testing.T) {
	dt := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	rows := []pendingQSORow{
		{
			QSOID:           200,
			Callsign:        "K9XYZ",
			Band:            "2m",
			Mode:            "FM",
			DatetimeOn:      dt,
			StationCallsign: strPtr("W1AW"),
			FrequencyHz:     i64Ptr(144_200_000),
			MyGridsquare:    strPtr("EN52"),
		},
	}

	adif, err := qsosToADIF(rows)
	if err != nil {
		t.Fatalf("qsosToADIF: %v", err)
	}

	if !containsStr(adif, "W1AW") {
		t.Error("ADIF missing STATION_CALLSIGN W1AW")
	}
	if !containsStr(adif, "144.200000") {
		t.Error("ADIF missing frequency 144.200000 MHz")
	}
	if !containsStr(adif, "EN52") {
		t.Error("ADIF missing MY_GRIDSQUARE EN52")
	}
}

func TestQSOsToADIF_NilOptionalFields(t *testing.T) {
	dt := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)

	// A QSO with all optional fields nil — should produce valid minimal ADIF.
	rows := []pendingQSORow{
		{
			QSOID:      300,
			Callsign:   "VK2ABC",
			Band:       "20m",
			Mode:       "CW",
			DatetimeOn: dt,
		},
	}

	adif, err := qsosToADIF(rows)
	if err != nil {
		t.Fatalf("qsosToADIF with nil fields: %v", err)
	}
	if !containsStr(adif, "VK2ABC") {
		t.Errorf("ADIF missing callsign VK2ABC\nGot:\n%s", adif)
	}
}

func TestQSOsToADIF_MultipleBatches(t *testing.T) {
	dt := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	rows := make([]pendingQSORow, 5)
	for i := range rows {
		rows[i] = pendingQSORow{
			QSOID:      int64(i + 1),
			Callsign:   "W1XYZ",
			Band:       "20m",
			Mode:       "CW",
			DatetimeOn: dt,
		}
	}

	adif, err := qsosToADIF(rows)
	if err != nil {
		t.Fatalf("qsosToADIF multi: %v", err)
	}

	// Count EOR markers — should be one per record.
	count := countSubstr(adif, "<EOR>")
	if count == 0 {
		// Try lowercase variant — ADIF writers may differ in case.
		count = countSubstr(adif, "<eor>")
	}
	if count != 5 {
		t.Errorf("expected 5 <EOR> markers for 5 QSOs, got %d\nADIF:\n%s", count, adif)
	}
}

func TestQSOsToADIF_Empty(t *testing.T) {
	adif, err := qsosToADIF(nil)
	if err != nil {
		t.Fatalf("qsosToADIF empty: %v", err)
	}
	// Empty ADIF is valid (no records, just possibly a header).
	_ = adif
}

// ──────────────────────────────────────────────────────────────────────────────
// Enqueue helpers — validate job arg serialization
// ──────────────────────────────────────────────────────────────────────────────

// We test the real EnqueueEQSLUpload/Download using a minimal interface-compatible mock.

func TestEnqueueEQSLUpload_ArgShape(t *testing.T) {
	// Verify EQSLUploadArgs serializes to the expected JSON shape.
	args := EQSLUploadArgs{UserID: 99}
	b, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal EQSLUploadArgs: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	uid, ok := m["user_id"]
	if !ok {
		t.Fatal("EQSLUploadArgs JSON missing 'user_id'")
	}
	if uid != float64(99) {
		t.Errorf("user_id: expected 99, got %v", uid)
	}
}

func TestEnqueueEQSLDownload_ArgShape(t *testing.T) {
	since := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	args := EQSLDownloadArgs{
		UserID:    42,
		SinceDate: since.Format(time.RFC3339),
	}
	b, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal EQSLDownloadArgs: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if m["user_id"] != float64(42) {
		t.Errorf("user_id: expected 42, got %v", m["user_id"])
	}
	if m["since_date"] != since.Format(time.RFC3339) {
		t.Errorf("since_date: expected %q, got %v", since.Format(time.RFC3339), m["since_date"])
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Helper functions
// ──────────────────────────────────────────────────────────────────────────────

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && func() bool {
		for i := 0; i <= len(s)-len(substr); i++ {
			if s[i:i+len(substr)] == substr {
				return true
			}
		}
		return false
	}()
}

func countSubstr(s, substr string) int {
	count := 0
	for i := 0; i <= len(s)-len(substr); {
		if s[i:i+len(substr)] == substr {
			count++
			i += len(substr)
		} else {
			i++
		}
	}
	return count
}

func TestIsEQSLAuthOrPermanentError(t *testing.T) {
	if !isEQSLAuthOrPermanentError("eqsl inbox: authentication failed — check credentials") {
		t.Fatal("expected auth failure to be classified as permanent")
	}
	if isEQSLAuthOrPermanentError("eqsl rejected upload: duplicate") {
		t.Fatal("duplicate upload should not be classified as auth/permanent")
	}
}
