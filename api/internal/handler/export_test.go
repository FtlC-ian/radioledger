package handler

import (
	"testing"
	"time"
)

func TestExportQSOToADIFRecord_CanonicalizesModeAliases(t *testing.T) {
	dt := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	cases := []struct {
		name        string
		qso         exportQSO
		wantMode    string
		wantSubmode string
	}{
		{name: "ft2", qso: exportQSO{Callsign: "W1AW", Band: "20m", Mode: "FT2", DatetimeOn: dt}, wantMode: "MFSK", wantSubmode: "FT2"},
		{name: "dmr", qso: exportQSO{Callsign: "W1AW", Band: "20m", Mode: "DMR", DatetimeOn: dt}, wantMode: "DIGITALVOICE", wantSubmode: "DMR"},
		{name: "usb", qso: exportQSO{Callsign: "W1AW", Band: "20m", Mode: "USB", DatetimeOn: dt}, wantMode: "SSB", wantSubmode: "USB"},
		{name: "ft8", qso: exportQSO{Callsign: "W1AW", Band: "20m", Mode: "FT8", DatetimeOn: dt}, wantMode: "FT8", wantSubmode: ""},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			rec := tt.qso.toADIFRecord()
			if got := rec.Get("MODE"); got != tt.wantMode {
				t.Fatalf("MODE=%q want %q", got, tt.wantMode)
			}
			if got := rec.Get("SUBMODE"); got != tt.wantSubmode {
				t.Fatalf("SUBMODE=%q want %q", got, tt.wantSubmode)
			}
		})
	}
}
