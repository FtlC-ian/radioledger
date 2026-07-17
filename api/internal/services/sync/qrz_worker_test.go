// Package sync — qrz_worker_test.go: unit tests for QRZ Logbook sync workers.
//
// Tests cover:
//   - QRZUploadArgs / QRZPollArgs River Kind() values
//   - qsoRowToSingleADIF formatting (required fields, optional fields, frequency)
//   - EnqueueQRZUpload / EnqueueQRZPoll job arg serialization
//   - EncodeQRZLogbookCredentials round-trip
package sync

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// ──────────────────────────────────────────────────────────────────────────────
// River job kind stability
// ──────────────────────────────────────────────────────────────────────────────

func TestQRZUploadArgs_Kind(t *testing.T) {
	args := QRZUploadArgs{UserID: 42}
	if args.Kind() != "qrz_upload" {
		t.Errorf("QRZUploadArgs.Kind(): expected 'qrz_upload', got %q", args.Kind())
	}
}

func TestQRZPollArgs_Kind(t *testing.T) {
	args := QRZPollArgs{UserID: 42}
	if args.Kind() != "qrz_poll" {
		t.Errorf("QRZPollArgs.Kind(): expected 'qrz_poll', got %q", args.Kind())
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// qsoRowToSingleADIF
// ──────────────────────────────────────────────────────────────────────────────

func TestQSORowToSingleADIF_RequiredFields(t *testing.T) {
	dt := time.Date(2024, 3, 15, 14, 30, 45, 0, time.UTC)
	row := pendingQSORow{
		SyncID:     1,
		QSOID:      100,
		Callsign:   "W5ABC",
		Band:       "40m",
		Mode:       "SSB",
		DatetimeOn: dt,
	}

	adif, err := qsoRowToSingleADIF(row)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	checks := map[string]string{
		"CALL":     "W5ABC",
		"BAND":     "40m",
		"MODE":     "SSB",
		"QSO_DATE": "20240315",
		"TIME_ON":  "143045",
	}
	for field, want := range checks {
		tag := "<" + field + ":"
		if !strings.Contains(adif, tag) {
			t.Errorf("expected ADIF field %s in output:\n%s", field, adif)
			continue
		}
		if !strings.Contains(adif, want) {
			t.Errorf("expected value %q for field %s in:\n%s", want, field, adif)
		}
	}

	if !strings.HasSuffix(strings.TrimSpace(adif), "<eor>") {
		t.Errorf("ADIF record should end with <eor>, got:\n%s", adif)
	}
}

func TestQSORowToSingleADIF_OptionalFields(t *testing.T) {
	dt := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	freq := int64(7_150_000) // 7.150 MHz
	stationCall := "K7ABC"
	rst59 := "59"
	rst57 := "57"
	grid1 := "DM41"
	grid2 := "DM42"
	row := pendingQSORow{
		Callsign:        "K7XYZ",
		Band:            "40m",
		Mode:            "CW",
		DatetimeOn:      dt,
		RstSent:         &rst59,
		RstRcvd:         &rst57,
		Gridsquare:      &grid1,
		MyGridsquare:    &grid2,
		StationCallsign: &stationCall,
		FrequencyHz:     &freq,
	}

	adif, err := qsoRowToSingleADIF(row)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, field := range []string{"RST_SENT", "RST_RCVD", "GRIDSQUARE", "MY_GRIDSQUARE", "STATION_CALLSIGN", "FREQ"} {
		if !strings.Contains(adif, "<"+field+":") {
			t.Errorf("expected optional field %s in ADIF:\n%s", field, adif)
		}
	}

	// 7150000 Hz → 7.150000 MHz
	if !strings.Contains(adif, "7.150000") {
		t.Errorf("expected frequency '7.150000' in ADIF:\n%s", adif)
	}
}

func TestQSORowToSingleADIF_CanonicalizesModeSubmode(t *testing.T) {
	dt := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	submode := "FT2"
	row := pendingQSORow{
		Callsign:   "N0CALL",
		Band:       "20m",
		Mode:       "MFSK",
		Submode:    &submode,
		DatetimeOn: dt,
	}

	adif, err := qsoRowToSingleADIF(row)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(adif, "<MODE:4>MFSK") || !strings.Contains(adif, "<SUBMODE:3>FT2") {
		t.Fatalf("expected canonical MFSK/FT2 export, got:\n%s", adif)
	}
}

func TestQSORowToSingleADIF_NilOptionalFields(t *testing.T) {
	dt := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	row := pendingQSORow{
		Callsign:   "N0CALL",
		Band:       "20m",
		Mode:       "FT8",
		DatetimeOn: dt,
	}

	adif, err := qsoRowToSingleADIF(row)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, field := range []string{"RST_SENT", "RST_RCVD", "GRIDSQUARE", "MY_GRIDSQUARE", "STATION_CALLSIGN", "FREQ"} {
		if strings.Contains(adif, "<"+field+":") {
			t.Errorf("unexpected optional field %s when nil:\n%s", field, adif)
		}
	}
}

func TestQSORowToSingleADIF_FieldLengthPrefixes(t *testing.T) {
	dt := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	row := pendingQSORow{
		Callsign:   "W1AW",
		Band:       "40m",
		Mode:       "SSB",
		DatetimeOn: dt,
	}

	adif, err := qsoRowToSingleADIF(row)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// CALL:4 — "W1AW" is 4 chars.
	if !strings.Contains(adif, "<CALL:4>W1AW") {
		t.Errorf("expected <CALL:4>W1AW in:\n%s", adif)
	}
	// BAND:3 — "40m" is 3 chars.
	if !strings.Contains(adif, "<BAND:3>40m") {
		t.Errorf("expected <BAND:3>40m in:\n%s", adif)
	}
	// QSO_DATE:8 — "20240101" is 8 chars.
	if !strings.Contains(adif, "<QSO_DATE:8>20240101") {
		t.Errorf("expected <QSO_DATE:8>20240101 in:\n%s", adif)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Enqueue helpers — validate job arg JSON shape
// ──────────────────────────────────────────────────────────────────────────────

func TestEnqueueQRZUpload_ArgShape(t *testing.T) {
	args := QRZUploadArgs{UserID: 777}
	b, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal QRZUploadArgs: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	uid, ok := m["user_id"]
	if !ok {
		t.Fatal("QRZUploadArgs JSON missing 'user_id'")
	}
	if uid != float64(777) {
		t.Errorf("user_id: expected 777, got %v", uid)
	}
}

func TestEnqueueQRZPoll_ArgShape(t *testing.T) {
	args := QRZPollArgs{UserID: 888}
	b, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal QRZPollArgs: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	uid, ok := m["user_id"]
	if !ok {
		t.Fatal("QRZPollArgs JSON missing 'user_id'")
	}
	if uid != float64(888) {
		t.Errorf("user_id: expected 888, got %v", uid)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Credential encoding helper
// ──────────────────────────────────────────────────────────────────────────────

func TestEncodeQRZLogbookCredentials_RoundTrip(t *testing.T) {
	raw, err := EncodeQRZLogbookCredentials("TEST-API-KEY-9999")
	if err != nil {
		t.Fatalf("EncodeQRZLogbookCredentials: %v", err)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded["api_key"] != "TEST-API-KEY-9999" {
		t.Errorf("expected api_key 'TEST-API-KEY-9999', got %v", decoded["api_key"])
	}
}

func TestEncodeQRZLogbookCredentials_RejectsEmpty(t *testing.T) {
	_, err := EncodeQRZLogbookCredentials("")
	if err == nil {
		t.Error("expected error for empty api_key, got nil")
	}
}

func TestIsQRZAuthOrPermanentError(t *testing.T) {
	if !isQRZAuthOrPermanentError("qrz logbook: API key is invalid") {
		t.Fatal("expected auth error to be classified as permanent")
	}
	if isQRZAuthOrPermanentError("qrz logbook insert failed: duplicate qso") {
		t.Fatal("duplicate qso should not be classified as auth/permanent")
	}
}

func TestIsTransientSyncError(t *testing.T) {
	if !isTransientSyncError("http POST: context deadline exceeded") {
		t.Fatal("expected deadline exceeded to be transient")
	}
	if !isTransientSyncError("eqsl upload: unexpected status 503") {
		t.Fatal("expected 5xx to be transient")
	}
	if isTransientSyncError("authentication failed") {
		t.Fatal("authentication failure should not be transient")
	}
}

func TestParseQRZConfirmationADIF(t *testing.T) {
	adif := "<CALL:4>W1AW<BAND:3>20m<MODE:2>CW<QSO_DATE:8>20240315<TIME_ON:4>1230<LOTW_QSL_RCVD:1>Y<LOTW_QSLRDATE:8>20240320<EQSL_QSL_RCVD:1>Y<EQSL_QSLRDATE:8>20240321<QSL_RCVD:1>Y<QSLRDATE:8>20240322<EOR>"
	recs, err := parseQRZConfirmationADIF(adif)
	if err != nil {
		t.Fatalf("parseQRZConfirmationADIF: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("expected 1 record, got %d", len(recs))
	}
	r := recs[0]
	if r.Callsign != "W1AW" || r.Band != "20m" || r.Mode != "CW" {
		t.Fatalf("unexpected normalized fields: %+v", r)
	}
	if !r.HasAnyConfirmation() {
		t.Fatal("expected confirmation flags to be detected")
	}
	if r.LotwQSLDate == nil || r.EQSLQSLDate == nil || r.PaperQSLDate == nil {
		t.Fatal("expected confirmation dates to be parsed")
	}
}

func TestParseQRZConfirmationADIF_Empty(t *testing.T) {
	recs, err := parseQRZConfirmationADIF("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(recs) != 0 {
		t.Fatalf("expected zero records, got %d", len(recs))
	}
}
