package handler

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/FtlC-ian/radioledger/api/internal/database/sqlc"
)

func TestBuildContestADIF_CanonicalizesModeAliases(t *testing.T) {
	dt := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	callsign := "W1AW"
	h := db.GetContestForExportRow{ContestCode: "ARRL-TEST", MyCallsign: &callsign}
	qsos := []db.GetContestQSOsForExportRow{
		{QsoUuid: uuid.New(), Callsign: "K1ABC", Band: "20m", Mode: "FT2", DatetimeOn: pgtype.Timestamptz{Time: dt, Valid: true}},
		{QsoUuid: uuid.New(), Callsign: "K1ABC", Band: "20m", Mode: "DMR", DatetimeOn: pgtype.Timestamptz{Time: dt, Valid: true}},
		{QsoUuid: uuid.New(), Callsign: "K1ABC", Band: "20m", Mode: "USB", DatetimeOn: pgtype.Timestamptz{Time: dt, Valid: true}},
		{QsoUuid: uuid.New(), Callsign: "K1ABC", Band: "20m", Mode: "FT8", DatetimeOn: pgtype.Timestamptz{Time: dt, Valid: true}},
	}

	adif := buildContestADIF(h, qsos)
	for _, want := range []string{"<MODE:4>MFSK", "<SUBMODE:3>FT2", "<MODE:12>DIGITALVOICE", "<SUBMODE:3>DMR", "<MODE:3>SSB", "<SUBMODE:3>USB", "<MODE:3>FT8"} {
		if !strings.Contains(adif, want) {
			t.Fatalf("ADIF missing %q\n%s", want, adif)
		}
	}
	if strings.Contains(adif, "<SUBMODE:3>FT8") {
		t.Fatalf("FT8 should remain MODE-only\n%s", adif)
	}
}
