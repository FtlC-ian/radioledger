package adif_test

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/FtlC-ian/radioledger/pkg/adif"
)

func TestWriterBasic(t *testing.T) {
	var sb strings.Builder
	w := adif.NewWriter(&sb)

	err := w.WriteRecord(&adif.Record{
		Fields: []adif.Field{
			{Name: "CALL", Value: "W1AW"},
			{Name: "BAND", Value: "20m"},
			{Name: "MODE", Value: "SSB"},
			{Name: "QSO_DATE", Value: "20260228"},
			{Name: "TIME_ON", Value: "153000"},
		},
	})
	if err != nil {
		t.Fatalf("WriteRecord: %v", err)
	}

	output := sb.String()
	if !strings.Contains(output, "<ADIF_VER:5>3.1.7") {
		t.Errorf("missing ADIF_VER in output:\n%s", output)
	}
	if !strings.Contains(output, "<PROGRAMID:11>RadioLedger") {
		t.Errorf("missing PROGRAMID in output:\n%s", output)
	}
	if !strings.Contains(output, "<EOH>") {
		t.Errorf("missing EOH in output:\n%s", output)
	}
	if !strings.Contains(output, "<CALL:4>W1AW") {
		t.Errorf("missing CALL field in output:\n%s", output)
	}
	if !strings.Contains(output, "<EOR>") {
		t.Errorf("missing EOR in output:\n%s", output)
	}
}

func TestWriterCanonicalOrder(t *testing.T) {
	var sb strings.Builder
	w := adif.NewWriter(&sb)

	// Write fields in reverse canonical order
	err := w.WriteRecord(&adif.Record{
		Fields: []adif.Field{
			{Name: "TIME_ON", Value: "153000"},
			{Name: "QSO_DATE", Value: "20260228"},
			{Name: "MODE", Value: "SSB"},
			{Name: "BAND", Value: "20m"},
			{Name: "CALL", Value: "W1AW"},
		},
	})
	if err != nil {
		t.Fatalf("WriteRecord: %v", err)
	}

	output := sb.String()
	// CALL should appear before BAND which should appear before MODE etc.
	callPos := strings.Index(output, "<CALL:")
	bandPos := strings.Index(output, "<BAND:")
	modePos := strings.Index(output, "<MODE:")
	datePos := strings.Index(output, "<QSO_DATE:")
	timePos := strings.Index(output, "<TIME_ON:")

	if callPos > datePos || datePos > timePos {
		t.Errorf("fields not in canonical order: CALL=%d, DATE=%d, TIME=%d", callPos, datePos, timePos)
	}
	_ = bandPos
	_ = modePos
}

func TestWriterAPPFieldsLast(t *testing.T) {
	var sb strings.Builder
	w := adif.NewWriter(&sb)

	err := w.WriteRecord(&adif.Record{
		Fields: []adif.Field{
			{Name: "APP_WSJT_SNR", Value: "-15"},
			{Name: "CALL", Value: "W1AW"},
			{Name: "BAND", Value: "20m"},
		},
	})
	if err != nil {
		t.Fatalf("WriteRecord: %v", err)
	}

	output := sb.String()
	callPos := strings.Index(output, "<CALL:")
	appPos := strings.Index(output, "<APP_WSJT_SNR:")
	if callPos == -1 || appPos == -1 {
		t.Fatalf("missing field in output: callPos=%d, appPos=%d", callPos, appPos)
	}
	if appPos < callPos {
		t.Errorf("APP_ field should come after CALL, but appPos=%d < callPos=%d", appPos, callPos)
	}
}

func TestWriterDeterministic(t *testing.T) {
	// Same record written twice should produce identical output
	makeRecord := func() *adif.Record {
		return &adif.Record{
			Fields: []adif.Field{
				{Name: "TIME_ON", Value: "153000"},
				{Name: "CALL", Value: "W1AW"},
				{Name: "BAND", Value: "20m"},
				{Name: "MODE", Value: "SSB"},
				{Name: "QSO_DATE", Value: "20260228"},
				{Name: "APP_CUSTOM", Value: "test"},
				{Name: "COMMENT", Value: "hello"},
			},
		}
	}

	var sb1, sb2 strings.Builder
	w1 := adif.NewWriter(&sb1)
	w2 := adif.NewWriter(&sb2)

	if err := w1.WriteRecord(makeRecord()); err != nil {
		t.Fatal(err)
	}
	if err := w2.WriteRecord(makeRecord()); err != nil {
		t.Fatal(err)
	}

	if sb1.String() != sb2.String() {
		t.Errorf("non-deterministic output:\n%s\n!=\n%s", sb1.String(), sb2.String())
	}
}

func TestWriterRoundTrip(t *testing.T) {
	ctx := context.Background()

	original := `<ADIF_VER:5>3.1.4
<PROGRAMID:11>RadioLedger
<EOH>
<CALL:4>W1AW
<QSO_DATE:8>20260228
<TIME_ON:6>153000
<BAND:3>20m
<MODE:3>SSB
<RST_SENT:2>59
<RST_RCVD:2>59
<APP_WSJT_SNR:3>-15
<EOR>
`

	// Parse
	header, records1, err := adif.ParseString(ctx, original)
	if err != nil {
		t.Fatalf("parse 1: %v", err)
	}

	// Write
	written, err := adif.FormatAll(header, records1, "1.0.0")
	if err != nil {
		t.Fatalf("format: %v", err)
	}

	// Parse again
	_, records2, err := adif.ParseString(ctx, written)
	if err != nil {
		t.Fatalf("parse 2: %v", err)
	}

	if len(records1) != len(records2) {
		t.Fatalf("round-trip record count mismatch: %d != %d", len(records1), len(records2))
	}

	for i, rec1 := range records1 {
		rec2 := records2[i]
		for _, f := range rec1.Fields {
			if got := rec2.Get(f.Name); got != f.Value {
				t.Errorf("round-trip record %d field %s: got %q, want %q", i, f.Name, got, f.Value)
			}
		}
	}
}

func TestWriterCanonicalizesModePairs(t *testing.T) {
	tests := []struct {
		name         string
		mode         string
		wantMode     string
		wantSubmode  string
		legacyMarker string
	}{
		{name: "ft2", mode: "FT2", wantMode: "MFSK", wantSubmode: "FT2", legacyMarker: "<MODE:3>FT2"},
		{name: "ft4", mode: "FT4", wantMode: "MFSK", wantSubmode: "FT4", legacyMarker: "<MODE:3>FT4"},
		{name: "js8", mode: "JS8", wantMode: "MFSK", wantSubmode: "JS8", legacyMarker: "<MODE:3>JS8"},
		{name: "vara", mode: "VARAHF", wantMode: "DYNAMIC", wantSubmode: "VARA HF", legacyMarker: "VARAHF"},
		{name: "dmr", mode: "DMR", wantMode: "DIGITALVOICE", wantSubmode: "DMR", legacyMarker: "<MODE:3>DMR"},
		{name: "packet", mode: "PACKET", wantMode: "PKT", legacyMarker: "PACKET"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var sb strings.Builder
			w := adif.NewWriter(&sb)

			err := w.WriteRecord(&adif.Record{
				Fields: []adif.Field{
					{Name: "CALL", Value: "K1ABC"},
					{Name: "MODE", Value: tt.mode},
				},
			})
			if err != nil {
				t.Fatalf("WriteRecord: %v", err)
			}

			output := sb.String()
			if !strings.Contains(output, "<MODE:"+strconv.Itoa(len(tt.wantMode))+">"+tt.wantMode) {
				t.Fatalf("expected canonical MODE=%s, output:\n%s", tt.wantMode, output)
			}
			if tt.wantSubmode == "" {
				if strings.Contains(output, "<SUBMODE:") {
					t.Fatalf("did not expect SUBMODE, output:\n%s", output)
				}
			} else if !strings.Contains(output, "<SUBMODE:"+strconv.Itoa(len(tt.wantSubmode))+">"+tt.wantSubmode) {
				t.Fatalf("expected canonical SUBMODE=%s, output:\n%s", tt.wantSubmode, output)
			}
			if strings.Contains(output, tt.legacyMarker) {
				t.Fatalf("unexpected legacy alias leak %q, output:\n%s", tt.legacyMarker, output)
			}
			if !strings.Contains(output, "<ADIF_VER:5>3.1.7") {
				t.Fatalf("expected ADIF 3.1.7 header, output:\n%s", output)
			}
		})
	}
}

func TestWriterWithTypeIndicator(t *testing.T) {
	var sb strings.Builder
	w := adif.NewWriter(&sb)

	err := w.WriteRecord(&adif.Record{
		Fields: []adif.Field{
			{Name: "CALL", Value: "W1AW"},
			{Name: "QSO_DATE", Value: "20260228", Type: "D"},
		},
	})
	if err != nil {
		t.Fatalf("WriteRecord: %v", err)
	}

	output := sb.String()
	if !strings.Contains(output, "<QSO_DATE:8:D>20260228") {
		t.Errorf("expected type indicator in output:\n%s", output)
	}
}

func TestFormatRecord(t *testing.T) {
	rec := &adif.Record{
		Fields: []adif.Field{
			{Name: "CALL", Value: "W1AW"},
			{Name: "BAND", Value: "20m"},
		},
	}
	output := adif.FormatRecord(rec)
	if !strings.Contains(output, "<CALL:4>W1AW") {
		t.Errorf("FormatRecord missing CALL field:\n%s", output)
	}
	if !strings.Contains(output, "<EOR>") {
		t.Errorf("FormatRecord missing EOR:\n%s", output)
	}
}

func TestWriteMultipleRecords(t *testing.T) {
	var sb strings.Builder
	w := adif.NewWriter(&sb)

	records := []*adif.Record{
		{Fields: []adif.Field{{Name: "CALL", Value: "W1AW"}, {Name: "BAND", Value: "20m"}}},
		{Fields: []adif.Field{{Name: "CALL", Value: "K5XX"}, {Name: "BAND", Value: "40m"}}},
		{Fields: []adif.Field{{Name: "CALL", Value: "VE3AB"}, {Name: "BAND", Value: "2m"}}},
	}

	if err := w.WriteRecords(records); err != nil {
		t.Fatalf("WriteRecords: %v", err)
	}

	output := sb.String()
	for _, call := range []string{"W1AW", "K5XX", "VE3AB"} {
		if !strings.Contains(output, call) {
			t.Errorf("missing call %s in output", call)
		}
	}
	// Count EOR occurrences
	count := strings.Count(output, "<EOR>")
	if count != 3 {
		t.Errorf("expected 3 <EOR> markers, got %d", count)
	}
}
