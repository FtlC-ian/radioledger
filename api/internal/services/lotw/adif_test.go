package lotw

import (
	"strings"
	"testing"
	"time"
)

func TestBuildADIF_CanonicalizesModeAliases(t *testing.T) {
	dt := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	rows := []LoTWQSORow{
		{QSOID: 1, Callsign: "W1AW", Band: "20m", Mode: "FT2", DatetimeOn: dt},
		{QSOID: 2, Callsign: "W1AW", Band: "20m", Mode: "DMR", DatetimeOn: dt},
		{QSOID: 3, Callsign: "W1AW", Band: "20m", Mode: "USB", DatetimeOn: dt},
		{QSOID: 4, Callsign: "W1AW", Band: "20m", Mode: "FT8", DatetimeOn: dt},
	}

	adif, err := BuildADIF(rows)
	if err != nil {
		t.Fatalf("BuildADIF: %v", err)
	}

	for _, want := range []string{
		"<MODE:4>MFSK",
		"<SUBMODE:3>FT2",
		"<MODE:12>DIGITALVOICE",
		"<SUBMODE:3>DMR",
		"<MODE:3>SSB",
		"<SUBMODE:3>USB",
		"<MODE:3>FT8",
	} {
		if !strings.Contains(adif, want) {
			t.Fatalf("ADIF missing %q\n%s", want, adif)
		}
	}
	if strings.Contains(adif, "<SUBMODE:3>FT8") {
		t.Fatalf("FT8 should remain MODE-only\n%s", adif)
	}
}
