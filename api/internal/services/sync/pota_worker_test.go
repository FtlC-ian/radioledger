package sync

import (
	"strings"
	"testing"
	"time"
)

func TestPotaQSOsToADIF_CanonicalizesModeAliases(t *testing.T) {
	dt := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	rows := []potaPendingQSORow{
		{QSOID: 1, Callsign: "K1ABC", Band: "20m", Mode: "FT2", DatetimeOn: dt, MyPotaRefs: []string{"K-1234"}},
		{QSOID: 2, Callsign: "K1ABC", Band: "20m", Mode: "DMR", DatetimeOn: dt, MyPotaRefs: []string{"K-1234"}},
		{QSOID: 3, Callsign: "K1ABC", Band: "20m", Mode: "USB", DatetimeOn: dt, MyPotaRefs: []string{"K-1234"}},
		{QSOID: 4, Callsign: "K1ABC", Band: "20m", Mode: "FT8", DatetimeOn: dt, MyPotaRefs: []string{"K-1234"}},
	}

	adif, err := potaQSOsToADIF(rows)
	if err != nil {
		t.Fatalf("potaQSOsToADIF: %v", err)
	}

	for _, want := range []string{"<MODE:4>MFSK", "<SUBMODE:3>FT2", "<MODE:12>DIGITALVOICE", "<SUBMODE:3>DMR", "<MODE:3>SSB", "<SUBMODE:3>USB", "<MODE:3>FT8"} {
		if !strings.Contains(adif, want) {
			t.Fatalf("ADIF output missing %q\n%s", want, adif)
		}
	}
	if strings.Contains(adif, "<SUBMODE:3>FT8") {
		t.Fatalf("FT8 should remain MODE-only\n%s", adif)
	}
}
