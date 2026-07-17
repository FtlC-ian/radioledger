package handler

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/FtlC-ian/radioledger/api/internal/database/sqlc"
	adifpkg "github.com/FtlC-ian/radioledger/pkg/adif"
)

func TestExportQSOToADIFRecordCanonicalizesModePair(t *testing.T) {
	tests := []struct {
		name        string
		mode        string
		submode     *string
		wantMode    string
		wantSubmode string
	}{
		{name: "ft2 alias", mode: "FT2", wantMode: "MFSK", wantSubmode: "FT2"},
		{name: "ft4 alias", mode: "FT4", wantMode: "MFSK", wantSubmode: "FT4"},
		{name: "js8 alias", mode: "JS8", wantMode: "MFSK", wantSubmode: "JS8"},
		{name: "q65 alias", mode: "Q65", wantMode: "MFSK", wantSubmode: "Q65"},
		{name: "ft8 top level", mode: "FT8", wantMode: "FT8"},
		{name: "ft8 legacy pair", mode: "MFSK", submode: strPtr("FT8"), wantMode: "FT8"},
		{name: "dmr alias", mode: "DMR", wantMode: "DIGITALVOICE", wantSubmode: "DMR"},
		{name: "packet alias", mode: "PACKET", wantMode: "PKT"},
		{name: "canonical digitalvoice pair", mode: "DIGITALVOICE", submode: strPtr("DMR"), wantMode: "DIGITALVOICE", wantSubmode: "DMR"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := (exportQSO{
				Callsign:   "K1ABC",
				Band:       "20m",
				Mode:       tt.mode,
				Submode:    tt.submode,
				DatetimeOn: time.Date(2024, 4, 13, 12, 0, 0, 0, time.UTC),
			}).toADIFRecord()

			if got := rec.Get("MODE"); got != tt.wantMode {
				t.Fatalf("MODE=%q, want %q", got, tt.wantMode)
			}
			if got := rec.Get("SUBMODE"); got != tt.wantSubmode {
				t.Fatalf("SUBMODE=%q, want %q", got, tt.wantSubmode)
			}
		})
	}
}

func TestBuildContestADIFCanonicalizesAliasModes(t *testing.T) {
	myCall := "W1AW"
	body := buildContestADIF(
		db.GetContestForExportRow{ContestCode: "CQ-WW-SSB", MyCallsign: &myCall},
		[]db.GetContestQSOsForExportRow{
			{
				Callsign:   "K1DMR",
				Band:       "70cm",
				Mode:       "DMR",
				DatetimeOn: pgTime(time.Date(2024, 4, 13, 13, 0, 0, 0, time.UTC)),
			},
			{
				Callsign:   "K1FT2",
				Band:       "20m",
				Mode:       "FT2",
				DatetimeOn: pgTime(time.Date(2024, 4, 13, 13, 2, 0, 0, time.UTC)),
			},
			{
				Callsign:   "K1PKT",
				Band:       "2m",
				Mode:       "PACKET",
				DatetimeOn: pgTime(time.Date(2024, 4, 13, 13, 5, 0, 0, time.UTC)),
			},
			{
				Callsign:   "K1CANON1",
				Band:       "70cm",
				Mode:       "DIGITALVOICE",
				Submode:    strPtr("DMR"),
				DatetimeOn: pgTime(time.Date(2024, 4, 13, 13, 7, 0, 0, time.UTC)),
			},
			{
				Callsign:   "K1CANON2",
				Band:       "20m",
				Mode:       "MFSK",
				Submode:    strPtr("FT2"),
				DatetimeOn: pgTime(time.Date(2024, 4, 13, 13, 9, 0, 0, time.UTC)),
			},
		},
	)

	header, records, err := adifpkg.ParseString(context.Background(), body)
	if err != nil {
		t.Fatalf("parse contest export: %v\nbody:\n%s", err, body)
	}
	if got := header.ADIFVersion(); got != adifpkg.ADIFVersion {
		t.Fatalf("ADIF_VER=%q, want %q", got, adifpkg.ADIFVersion)
	}
	if len(records) != 5 {
		t.Fatalf("record count=%d, want 5", len(records))
	}
	if got := records[0].Get("MODE"); got != "DIGITALVOICE" {
		t.Fatalf("record 0 MODE=%q, want DIGITALVOICE", got)
	}
	if got := records[0].Get("SUBMODE"); got != "DMR" {
		t.Fatalf("record 0 SUBMODE=%q, want DMR", got)
	}
	if got := records[1].Get("MODE"); got != "MFSK" {
		t.Fatalf("record 1 MODE=%q, want MFSK", got)
	}
	if got := records[1].Get("SUBMODE"); got != "FT2" {
		t.Fatalf("record 1 SUBMODE=%q, want FT2", got)
	}
	if got := records[2].Get("MODE"); got != "PKT" {
		t.Fatalf("record 2 MODE=%q, want PKT", got)
	}
	if got := records[2].Get("SUBMODE"); got != "" {
		t.Fatalf("record 2 SUBMODE=%q, want empty", got)
	}
	if got := records[3].Get("MODE"); got != "DIGITALVOICE" {
		t.Fatalf("record 3 MODE=%q, want DIGITALVOICE", got)
	}
	if got := records[3].Get("SUBMODE"); got != "DMR" {
		t.Fatalf("record 3 SUBMODE=%q, want DMR", got)
	}
	if got := records[4].Get("MODE"); got != "MFSK" {
		t.Fatalf("record 4 MODE=%q, want MFSK", got)
	}
	if got := records[4].Get("SUBMODE"); got != "FT2" {
		t.Fatalf("record 4 SUBMODE=%q, want FT2", got)
	}
}

func pgTime(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true}
}

func strPtr(s string) *string {
	return &s
}
