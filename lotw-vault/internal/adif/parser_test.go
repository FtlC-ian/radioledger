package adif_test

import (
	"strings"
	"testing"

	"github.com/FtlC-ian/radioledger/lotw-vault/internal/adif"
)

// ── Happy-path tests ──────────────────────────────────────────────────────────

func TestParseAll_BasicRecord(t *testing.T) {
	input := `<CALL:5>W1AW <BAND:3>20m <MODE:3>SSB <QSO_DATE:10>2024-01-15 <TIME_ON:6>143000 <EOR>`
	records, err := adif.ParseAll(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	r := records[0]
	if r["CALL"] != "W1AW" {
		t.Errorf("CALL = %q, want %q", r["CALL"], "W1AW")
	}
	if r["BAND"] != "20m" {
		t.Errorf("BAND = %q, want %q", r["BAND"], "20m")
	}
	if r["MODE"] != "SSB" {
		t.Errorf("MODE = %q, want %q", r["MODE"], "SSB")
	}
}

func TestParseAll_MultipleRecords(t *testing.T) {
	input := `<CALL:4>K1ZZ <BAND:2>2m <MODE:2>FM <QSO_DATE:10>2024-02-01 <TIME_ON:6>120000 <EOR>
<CALL:5>VE3XY <BAND:3>40m <MODE:3>CW  <QSO_DATE:10>2024-02-02 <TIME_ON:6>180000 <EOR>`

	records, err := adif.ParseAll(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}
	if records[0]["CALL"] != "K1ZZ" {
		t.Errorf("record 0 CALL = %q", records[0]["CALL"])
	}
	if records[1]["CALL"] != "VE3XY" {
		t.Errorf("record 1 CALL = %q", records[1]["CALL"])
	}
}

func TestParseAll_SkipsHeader(t *testing.T) {
	input := `ADIF log exported by test
<EOH>
<CALL:5>W1AW <BAND:3>20m <MODE:3>SSB <QSO_DATE:10>2024-01-15 <TIME_ON:6>143000 <EOR>`
	records, err := adif.ParseAll(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record after header, got %d", len(records))
	}
}

func TestParseAll_CaseInsensitiveFieldNames(t *testing.T) {
	input := `<call:5>W1AW <band:3>20m <mode:3>SSB <qso_date:10>2024-01-15 <time_on:6>143000 <eor>`
	records, err := adif.ParseAll(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0]["CALL"] != "W1AW" {
		t.Errorf("field name not uppercased: CALL = %q", records[0]["CALL"])
	}
}

func TestParseAll_EmptyValue(t *testing.T) {
	// A field with length 0 is valid ADIF and produces an empty value.
	input := `<CALL:5>W1AW <COMMENT:0> <BAND:3>20m <MODE:3>SSB <QSO_DATE:10>2024-01-15 <TIME_ON:6>143000 <EOR>`
	records, err := adif.ParseAll(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	// Empty value (length 0) is stored as empty string.
	if v, ok := records[0]["COMMENT"]; ok && v != "" {
		t.Errorf("COMMENT should be empty string, got %q", v)
	}
}

func TestParseAll_DuplicateFields(t *testing.T) {
	// Last write wins — ADIF doesn't forbid duplicates so we just store the last.
	input := `<CALL:4>W1AW <CALL:4>K2ZZ <BAND:3>20m <MODE:3>CW <QSO_DATE:10>2024-01-01 <TIME_ON:6>120000 <EOR>`
	records, err := adif.ParseAll(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	// Second CALL value should win.
	if records[0]["CALL"] != "K2ZZ" {
		t.Errorf("CALL = %q, want last-write value K2ZZ", records[0]["CALL"])
	}
}

func TestParseAll_JunkBetweenRecords(t *testing.T) {
	// ADIF allows free-form text between records (the parser skips to '<').
	input := `Some random junk here
<CALL:4>W1AW <BAND:3>20m <MODE:3>SSB <QSO_DATE:10>2024-01-15 <TIME_ON:6>143000 <EOR>
More junk
<CALL:4>K2ZZ <BAND:3>40m <MODE:2>CW <QSO_DATE:10>2024-01-16 <TIME_ON:6>150000 <EOR>`
	records, err := adif.ParseAll(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}
}

// ── Error cases — must return error, must NOT panic ───────────────────────────

func TestParseAll_NegativeLength(t *testing.T) {
	inputs := []string{
		`<CALL:-1>X<EOR>`,
		`<CALL:-100>SOME_DATA<EOR>`,
		`<BAND:3>20m <CALL:-1>W1AW <EOR>`,
	}
	for _, input := range inputs {
		t.Run(input, func(t *testing.T) {
			records, err := adif.ParseAll(input)
			if err == nil {
				t.Errorf("expected error for %q, got %d records", input, len(records))
			}
			// Must not panic — the error is the important thing.
		})
	}
}

func TestParseAll_NonNumericLength(t *testing.T) {
	inputs := []string{
		`<CALL:abc>W1AW<EOR>`,
		`<BAND:x>20m<EOR>`,
		`<MODE:->CW<EOR>`,
		`<CALL:1.5>W<EOR>`,
	}
	for _, input := range inputs {
		t.Run(input, func(t *testing.T) {
			records, err := adif.ParseAll(input)
			if err == nil {
				t.Errorf("expected error for %q, got %d records", input, len(records))
			}
		})
	}
}

func TestParseAll_AbsurdlyLargeLength(t *testing.T) {
	// Lengths larger than the remaining input must be rejected.
	inputs := []string{
		`<CALL:999999>W1AW<EOR>`,
		`<CALL:2147483647>W<EOR>`,
		// Exactly one byte over the remaining input.
		`<CALL:5>W1AW`,     // truncated — length 5 but only 4 chars left
		`<CALL:100>WA<EOR>`, // 100 > len("WA<EOR>")
	}
	for _, input := range inputs {
		t.Run(input, func(t *testing.T) {
			_, err := adif.ParseAll(input)
			if err == nil {
				t.Errorf("expected error for %q", input)
			}
		})
	}
}

func TestParseAll_TruncatedTag(t *testing.T) {
	// Tag opened with '<' but never closed with '>'.
	inputs := []string{
		`<CALL:4`,        // no closing >
		`<CALL:4>W1AW <BAND`, // second tag truncated
	}
	for _, input := range inputs {
		t.Run(input, func(t *testing.T) {
			_, err := adif.ParseAll(input)
			if err == nil {
				t.Errorf("expected error for truncated tag %q", input)
			}
		})
	}
}

func TestParseAll_IncompleteRecord_NoEOR(t *testing.T) {
	// Fields present but no <EOR> — end of input reached with partial record.
	input := `<CALL:5>W1AW <BAND:3>20m <MODE:3>SSB`
	_, err := adif.ParseAll(input)
	if err == nil {
		t.Error("expected error for record without <EOR>, got nil")
	}
	if !strings.Contains(err.Error(), "EOR") {
		t.Errorf("error should mention EOR, got: %v", err)
	}
}

func TestParseAll_NoPanic_OnAttackerInput(t *testing.T) {
	// Regression: these inputs panicked before the fix. Ensure no panic.
	inputs := []string{
		"<CALL:-1>X<EOR>",
		"<CALL:-2147483648>X<EOR>",
		"<CALL:99999999999>X<EOR>",
		"<:5>hello<EOR>",
		"<CALL:>data<EOR>",
		"",
		"<",
		"<<>>",
		strings.Repeat("<CALL:1>X", 1000) + "<EOR>",
		"<CALL:5>" + strings.Repeat("X", 4) + "<EOR>", // exactly 1 short
	}
	for _, input := range inputs {
		input := input
		t.Run("nopanic/"+input[:min(len(input), 30)], func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("panic on input %q: %v", input, r)
				}
			}()
			// Result doesn't matter — just must not panic.
			_, _ = adif.ParseAll(input)
		})
	}
}

// ── Encode helpers ────────────────────────────────────────────────────────────

func TestEncodeField(t *testing.T) {
	got := adif.EncodeField("CALL", "W1AW")
	want := "<CALL:4>W1AW\n"
	if got != want {
		t.Errorf("EncodeField = %q, want %q", got, want)
	}
}

func TestEncodeField_Empty(t *testing.T) {
	got := adif.EncodeField("BAND_RX", "")
	if got != "" {
		t.Errorf("EncodeField with empty value should return empty string, got %q", got)
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
