package adif_test

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/FtlC-ian/radioledger/pkg/adif"
)

func TestParseSimpleFile(t *testing.T) {
	f, err := os.Open("testdata/simple.adi")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	ctx := context.Background()
	header, records, err := adif.ParseAll(ctx, f)
	if err != nil {
		t.Fatal(err)
	}

	if header.ADIFVersion() != "3.1.4" {
		t.Errorf("expected ADIF_VER 3.1.4, got %q", header.ADIFVersion())
	}
	if header.ProgramID() != "RadioLedger" {
		t.Errorf("expected PROGRAMID RadioLedger, got %q", header.ProgramID())
	}
	if len(records) != 3 {
		t.Fatalf("expected 3 records, got %d", len(records))
	}

	if call := records[0].Get("CALL"); call != "W1AW" {
		t.Errorf("record 0: expected CALL W1AW, got %q", call)
	}
	if band := records[0].Get("BAND"); band != "20m" {
		t.Errorf("record 0: expected BAND 20m, got %q", band)
	}
	if mode := records[0].Get("MODE"); mode != "SSB" {
		t.Errorf("record 0: expected MODE SSB, got %q", mode)
	}
}

func TestParseWsjtxFile(t *testing.T) {
	f, err := os.Open("testdata/wsjtx.adi")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	ctx := context.Background()
	_, records, err := adif.ParseAll(ctx, f)
	if err != nil {
		t.Fatal(err)
	}

	if len(records) != 3 {
		t.Fatalf("expected 3 records, got %d", len(records))
	}

	// WSJT-X uses lowercase tags — verify normalization to uppercase
	if call := records[0].Get("CALL"); call != "K1ABC" {
		t.Errorf("record 0: expected CALL K1ABC, got %q", call)
	}
	if mode := records[0].Get("MODE"); mode != "FT8" {
		t.Errorf("record 0: expected MODE FT8, got %q", mode)
	}
}

func TestParseComplexFile(t *testing.T) {
	f, err := os.Open("testdata/complex.adi")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	ctx := context.Background()
	_, records, err := adif.ParseAll(ctx, f)
	if err != nil {
		t.Fatal(err)
	}

	if len(records) != 3 {
		t.Fatalf("expected 3 records, got %d", len(records))
	}

	rec := records[0]
	tests := []struct {
		field string
		want  string
	}{
		{"CALL", "VK9/W5XXX"},
		{"BAND", "20m"},
		{"MODE", "SSB"},
		{"QSO_DATE", "20260228"},
		{"TIME_ON", "153000"},
		{"NAME", "PETER"},
		{"GRIDSQUARE", "QF89"},
		{"MY_STATE", "OK"},
		{"QSL_SENT", "Y"},
	}
	for _, tt := range tests {
		if got := rec.Get(tt.field); got != tt.want {
			t.Errorf("record 0: %s = %q, want %q", tt.field, got, tt.want)
		}
	}

	// Check APP_* field is preserved
	if app := records[1].Get("APP_WSJT_SNR"); app != "-15" {
		t.Errorf("record 1: expected APP_WSJT_SNR -15, got %q", app)
	}
}

func TestParseMalformedFile(t *testing.T) {
	f, err := os.Open("testdata/malformed.adi")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	ctx := context.Background()
	// Must not panic or crash. Should return at least the valid records.
	_, records, _ := adif.ParseAll(ctx, f)
	// Should get at least one valid record (the final K0ABC one).
	// The truncated/negative length records should be skipped.
	found := false
	for _, r := range records {
		if r.Get("CALL") == "K0ABC" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected to find K0ABC record after malformed input, records: %d", len(records))
	}
}

func TestParseUnicodeFile(t *testing.T) {
	f, err := os.Open("testdata/unicode.adi")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	ctx := context.Background()
	_, records, err := adif.ParseAll(ctx, f)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) == 0 {
		t.Fatal("expected at least one record from unicode.adi")
	}
}

func TestParseEmptyFile(t *testing.T) {
	ctx := context.Background()
	header, records, err := adif.ParseString(ctx, "")
	if err != nil {
		t.Fatalf("empty file: unexpected error: %v", err)
	}
	_ = header
	if len(records) != 0 {
		t.Errorf("expected 0 records from empty file, got %d", len(records))
	}
}

func TestParseHeaderOnly(t *testing.T) {
	ctx := context.Background()
	input := "<ADIF_VER:5>3.1.4\n<PROGRAMID:12>RadioLedger\n<EOH>\n"
	header, records, err := adif.ParseString(ctx, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if header.ADIFVersion() != "3.1.4" {
		t.Errorf("expected ADIF_VER 3.1.4, got %q", header.ADIFVersion())
	}
	if len(records) != 0 {
		t.Errorf("expected 0 records, got %d", len(records))
	}
}

func TestParseNoHeader(t *testing.T) {
	ctx := context.Background()
	// No EOH — everything is records
	input := "<CALL:4>W1AW <BAND:3>20m <MODE:3>SSB <QSO_DATE:8>20260228 <TIME_ON:6>153000 <EOR>\n"
	_, records, err := adif.ParseString(ctx, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record (no-header file), got %d", len(records))
	}
	if call := records[0].Get("CALL"); call != "W1AW" {
		t.Errorf("expected CALL W1AW, got %q", call)
	}
}

func TestParseCaseInsensitive(t *testing.T) {
	ctx := context.Background()
	// Mix of upper, lower, mixed case
	input := "<adif_ver:5>3.1.4\n<eoh>\n<call:4>W1AW <Band:3>20m <MODE:3>SSB <qso_date:8>20260228 <time_on:6>153000 <eor>\n"
	header, records, err := adif.ParseString(ctx, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if header.ADIFVersion() != "3.1.4" {
		t.Errorf("expected ADIF_VER 3.1.4, got %q", header.ADIFVersion())
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if call := records[0].Get("CALL"); call != "W1AW" {
		t.Errorf("expected CALL W1AW, got %q", call)
	}
	if band := records[0].Get("BAND"); band != "20m" {
		t.Errorf("expected BAND 20m, got %q", band)
	}
}

func TestParseCRLF(t *testing.T) {
	ctx := context.Background()
	input := "<ADIF_VER:5>3.1.4\r\n<EOH>\r\n<CALL:4>W1AW <BAND:3>20m <MODE:3>SSB <QSO_DATE:8>20260228 <TIME_ON:6>153000 <EOR>\r\n"
	_, records, err := adif.ParseString(ctx, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
}

func TestParseUTF8BOM(t *testing.T) {
	ctx := context.Background()
	// UTF-8 BOM followed by normal ADIF
	bom := "\xEF\xBB\xBF"
	input := bom + "<ADIF_VER:5>3.1.4\n<EOH>\n<CALL:4>W1AW <BAND:3>20m <MODE:3>SSB <QSO_DATE:8>20260228 <TIME_ON:6>153000 <EOR>\n"
	header, records, err := adif.ParseString(ctx, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if header.ADIFVersion() != "3.1.4" {
		t.Errorf("expected ADIF_VER 3.1.4, got %q", header.ADIFVersion())
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
}

func TestParseTruncatedTag(t *testing.T) {
	ctx := context.Background()
	// Truncated at different points — must not panic
	cases := []string{
		"<",
		"<CALL",
		"<CALL:",
		"<CALL:4",
		"<CALL:4>",
		"<CALL:4>W",
		"<CALL:99999>short",
		"<ADIF_VER:5>3.1.4\n<EOH>\n<CALL:4>W1AW",
	}
	for _, input := range cases {
		_, _, err := adif.ParseString(ctx, input)
		_ = err // may or may not error, but must not panic
	}
}

func TestParseNegativeLength(t *testing.T) {
	ctx := context.Background()
	// Negative length in tag should be skipped gracefully
	input := "<ADIF_VER:5>3.1.4\n<EOH>\n<CALL:-1>test <CALL:4>W1AW <BAND:3>20m <MODE:3>SSB <QSO_DATE:8>20260228 <TIME_ON:6>153000 <EOR>\n"
	_, records, err := adif.ParseString(ctx, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should still get the W1AW record
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if call := records[0].Get("CALL"); call != "W1AW" {
		t.Errorf("expected CALL W1AW, got %q", call)
	}
}

func TestParseMaxRecords(t *testing.T) {
	ctx := context.Background()

	var sb strings.Builder
	sb.WriteString("<ADIF_VER:5>3.1.4\n<EOH>\n")
	for i := 0; i < 5; i++ {
		fmt.Fprintf(&sb, "<CALL:4>W1AW <BAND:3>20m <MODE:3>SSB <QSO_DATE:8>20260228 <TIME_ON:6>153000 <EOR>\n")
	}

	opts := adif.ParserOptions{MaxRecords: 3}
	p := adif.NewParserWithOptions(strings.NewReader(sb.String()), opts)
	_, _ = p.Header(ctx)

	var count int
	for {
		rec, err := p.Next(ctx)
		if err == adif.ErrTooManyRecords {
			break
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		_ = rec
		count++
	}
	if count != 3 {
		t.Errorf("expected exactly 3 records before limit, got %d", count)
	}
}

func TestParseMaxFieldLen(t *testing.T) {
	ctx := context.Background()
	// Construct a field claiming to be 20MB (way over limit)
	input := "<ADIF_VER:5>3.1.4\n<EOH>\n<COMMENT:20971520>x <EOR>\n"
	opts := adif.ParserOptions{MaxFieldLen: 1024}
	p := adif.NewParserWithOptions(strings.NewReader(input), opts)
	_, _ = p.Header(ctx)
	_, err := p.Next(ctx)
	if err == nil {
		t.Error("expected error for oversized field, got nil")
	}
}

func TestParseSingleRecord(t *testing.T) {
	ctx := context.Background()
	input := "<ADIF_VER:5>3.1.4\n<EOH>\n<CALL:4>W1AW <BAND:3>20m <MODE:3>SSB <QSO_DATE:8>20260228 <TIME_ON:6>153000 <EOR>\n"
	_, records, err := adif.ParseString(ctx, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
}

func TestParseLargeFile(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping large file test in short mode")
	}
	ctx := context.Background()

	var sb strings.Builder
	sb.WriteString("<ADIF_VER:5>3.1.4\n<EOH>\n")
	const count = 10000
	for i := 0; i < count; i++ {
		fmt.Fprintf(&sb, "<CALL:4>W1AW <BAND:3>20m <MODE:3>SSB <QSO_DATE:8>20260228 <TIME_ON:6>153000 <EOR>\n")
	}

	_, records, err := adif.ParseString(ctx, sb.String())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != count {
		t.Errorf("expected %d records, got %d", count, len(records))
	}
}

func TestParseContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	var sb strings.Builder
	sb.WriteString("<ADIF_VER:5>3.1.4\n<EOH>\n")
	for i := 0; i < 100; i++ {
		fmt.Fprintf(&sb, "<CALL:4>W1AW <BAND:3>20m <MODE:3>SSB <QSO_DATE:8>20260228 <TIME_ON:6>153000 <EOR>\n")
	}

	p := adif.NewParser(strings.NewReader(sb.String()))
	_, _ = p.Header(ctx)

	cancel() // cancel immediately

	_, err := p.Next(ctx)
	if err == nil {
		t.Error("expected context cancellation error, got nil")
	}
}

func TestParseFieldWithTypeIndicator(t *testing.T) {
	ctx := context.Background()
	// ADIF type indicator: <FIELDNAME:LENGTH:TYPE>VALUE
	input := "<ADIF_VER:5>3.1.4\n<EOH>\n<CALL:4:S>W1AW <BAND:3:E>20m <MODE:3:E>SSB <QSO_DATE:8:D>20260228 <TIME_ON:6:T>153000 <EOR>\n"
	_, records, err := adif.ParseString(ctx, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	f, ok := records[0].GetField("QSO_DATE")
	if !ok {
		t.Fatal("expected QSO_DATE field")
	}
	if f.Type != "D" {
		t.Errorf("expected type indicator D, got %q", f.Type)
	}
}

func TestParseMultipleRecords(t *testing.T) {
	ctx := context.Background()
	input := `<ADIF_VER:5>3.1.4
<EOH>
<CALL:4>W1AW <BAND:3>20m <MODE:3>SSB <QSO_DATE:8>20260228 <TIME_ON:6>153000 <EOR>
<CALL:4>K5XX <BAND:3>40m <MODE:3>FT8 <QSO_DATE:8>20260228 <TIME_ON:6>160000 <EOR>
<CALL:5>VE3AB <BAND:2>2m <MODE:2>FM <QSO_DATE:8>20260228 <TIME_ON:6>170000 <EOR>
`
	_, records, err := adif.ParseString(ctx, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 3 {
		t.Fatalf("expected 3 records, got %d", len(records))
	}
	calls := []string{"W1AW", "K5XX", "VE3AB"}
	for i, want := range calls {
		if got := records[i].Get("CALL"); got != want {
			t.Errorf("record %d: CALL = %q, want %q", i, got, want)
		}
	}
}

func TestParseStreamingAPI(t *testing.T) {
	ctx := context.Background()
	input := "<ADIF_VER:5>3.1.4\n<EOH>\n<CALL:4>W1AW <BAND:3>20m <MODE:3>SSB <QSO_DATE:8>20260228 <TIME_ON:6>153000 <EOR>\n<CALL:4>K5XX <BAND:3>40m <MODE:3>FT8 <QSO_DATE:8>20260228 <TIME_ON:6>160000 <EOR>\n"

	p := adif.NewParser(strings.NewReader(input))
	header, err := p.Header(ctx)
	if err != nil {
		t.Fatalf("Header: %v", err)
	}
	if header.ADIFVersion() != "3.1.4" {
		t.Errorf("expected 3.1.4, got %q", header.ADIFVersion())
	}

	var count int
	for {
		rec, err := p.Next(ctx)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		_ = rec
		count++
	}
	if count != 2 {
		t.Errorf("expected 2 records, got %d", count)
	}
}

func TestParseTimeFormats(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name string
		time string
	}{
		{"HHMM", "1530"},
		{"HHMMSS", "153000"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			l := len(tt.time)
			input := fmt.Sprintf("<ADIF_VER:5>3.1.4\n<EOH>\n<CALL:4>W1AW <BAND:3>20m <MODE:3>SSB <QSO_DATE:8>20260228 <TIME_ON:%d>%s <EOR>\n", l, tt.time)
			_, records, err := adif.ParseString(ctx, input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(records) != 1 {
				t.Fatalf("expected 1 record, got %d", len(records))
			}
			if got := records[0].Get("TIME_ON"); got != tt.time {
				t.Errorf("TIME_ON = %q, want %q", got, tt.time)
			}
		})
	}
}

func TestParseAPPFields(t *testing.T) {
	ctx := context.Background()
	input := "<ADIF_VER:5>3.1.4\n<EOH>\n<CALL:4>W1AW <APP_WSJT_SNR:3>-15 <APP_CUSTOM_FIELD:5>hello <EOR>\n"
	_, records, err := adif.ParseString(ctx, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if v := records[0].Get("APP_WSJT_SNR"); v != "-15" {
		t.Errorf("APP_WSJT_SNR = %q, want -15", v)
	}
	if v := records[0].Get("APP_CUSTOM_FIELD"); v != "hello" {
		t.Errorf("APP_CUSTOM_FIELD = %q, want hello", v)
	}
}
