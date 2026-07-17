package jobs

import (
	"testing"

	adifpkg "github.com/FtlC-ian/radioledger/pkg/adif"
)

func TestMapADIFRecordNormalizesModes(t *testing.T) {
	tests := []struct {
		name        string
		mode        string
		submode     string
		wantMode    string
		wantSubmode *string
	}{
		{name: "canonical ft8 direct", mode: "FT8", wantMode: "FT8"},
		{name: "legacy ft8 mfsk pair", mode: "MFSK", submode: "FT8", wantMode: "FT8"},
		{name: "canonical ft4 pair", mode: "MFSK", submode: "FT4", wantMode: "MFSK", wantSubmode: strPtr("FT4")},
		{name: "legacy ft4 direct", mode: "FT4", wantMode: "MFSK", wantSubmode: strPtr("FT4")},
		{name: "legacy ft2 direct", mode: "FT2", wantMode: "MFSK", wantSubmode: strPtr("FT2")},
		{name: "legacy js8 direct", mode: "JS8", wantMode: "MFSK", wantSubmode: strPtr("JS8")},
		{name: "legacy q65 direct", mode: "Q65", wantMode: "MFSK", wantSubmode: strPtr("Q65")},
		{name: "legacy fst4 direct", mode: "FST4", wantMode: "MFSK", wantSubmode: strPtr("FST4")},
		{name: "legacy fst4w direct", mode: "FST4W", wantMode: "MFSK", wantSubmode: strPtr("FST4W")},
		{name: "legacy fsqcall direct", mode: "FSQCALL", wantMode: "MFSK", wantSubmode: strPtr("FSQCALL")},
		{name: "legacy jtms direct", mode: "JTMS", wantMode: "MFSK", wantSubmode: strPtr("JTMS")},
		{name: "legacy mfsk4 direct", mode: "MFSK4", wantMode: "MFSK", wantSubmode: strPtr("MFSK4")},
		{name: "legacy mfsk11 direct", mode: "MFSK11", wantMode: "MFSK", wantSubmode: strPtr("MFSK11")},
		{name: "legacy mfsk22 direct", mode: "MFSK22", wantMode: "MFSK", wantSubmode: strPtr("MFSK22")},
		{name: "legacy mfsk31 direct", mode: "MFSK31", wantMode: "MFSK", wantSubmode: strPtr("MFSK31")},
		{name: "legacy mfsk32 direct", mode: "MFSK32", wantMode: "MFSK", wantSubmode: strPtr("MFSK32")},
		{name: "legacy mfsk64 direct", mode: "MFSK64", wantMode: "MFSK", wantSubmode: strPtr("MFSK64")},
		{name: "legacy mfsk64l direct", mode: "MFSK64L", wantMode: "MFSK", wantSubmode: strPtr("MFSK64L")},
		{name: "legacy mfsk128 direct", mode: "MFSK128", wantMode: "MFSK", wantSubmode: strPtr("MFSK128")},
		{name: "legacy mfsk128l direct", mode: "MFSK128L", wantMode: "MFSK", wantSubmode: strPtr("MFSK128L")},
		{name: "canonical vara pair", mode: "DYNAMIC", submode: "VARA HF", wantMode: "DYNAMIC", wantSubmode: strPtr("VARA HF")},
		{name: "legacy packet alias", mode: "PACKET", wantMode: "PKT"},
		{name: "canonical pkt", mode: "PKT", wantMode: "PKT"},
		{name: "legacy dmr direct", mode: "DMR", wantMode: "DIGITALVOICE", wantSubmode: strPtr("DMR")},
		{name: "canonical digitalvoice pair", mode: "DIGITALVOICE", submode: "C4FM", wantMode: "DIGITALVOICE", wantSubmode: strPtr("C4FM")},
		{name: "legacy dstar direct", mode: "DSTAR", wantMode: "DIGITALVOICE", wantSubmode: strPtr("DSTAR")},
		{name: "legacy psk31 direct", mode: "PSK31", wantMode: "PSK", wantSubmode: strPtr("PSK31")},
		{name: "legacy usb direct", mode: "USB", wantMode: "SSB", wantSubmode: strPtr("USB")},
		{name: "canonical ssb usb pair", mode: "SSB", submode: "USB", wantMode: "SSB", wantSubmode: strPtr("USB")},
		{name: "canonical ssb", mode: "SSB", wantMode: "SSB"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := minimalADIFRecord(tt.mode, tt.submode)
			row, warnings, err := mapADIFRecord(rec, 1, 99)
			if err != nil {
				t.Fatalf("mapADIFRecord error: %v", err)
			}
			if len(warnings) != 0 {
				t.Fatalf("unexpected warnings: %+v", warnings)
			}
			if row.Mode != tt.wantMode {
				t.Fatalf("mode = %q, want %q", row.Mode, tt.wantMode)
			}
			if (row.Submode == nil) != (tt.wantSubmode == nil) {
				t.Fatalf("submode presence = %v, want %v", row.Submode != nil, tt.wantSubmode != nil)
			}
			if row.Submode != nil && *row.Submode != *tt.wantSubmode {
				t.Fatalf("submode = %q, want %q", *row.Submode, *tt.wantSubmode)
			}
		})
	}
}

func TestMapADIFRecordRejectsInvalidModeInputs(t *testing.T) {
	tests := []struct {
		name     string
		mode     string
		submode  string
		wantCode string
	}{
		{name: "unknown mode", mode: "BANANA", wantCode: "INVALID_MODE"},
		{name: "mismatched pair", mode: "DIGITALVOICE", submode: "FT4", wantCode: "INVALID_MODE_SUBMODE"},
		{name: "invalid ft8 pair", mode: "PSK", submode: "FT8", wantCode: "INVALID_MODE_SUBMODE"},
		{name: "FT2 with wrong parent", mode: "PKT", submode: "FT2", wantCode: "INVALID_MODE_SUBMODE"},
		{name: "Q65 with wrong parent", mode: "PSK", submode: "Q65", wantCode: "INVALID_MODE_SUBMODE"},
		{name: "JS8 with wrong parent", mode: "CW", submode: "JS8", wantCode: "INVALID_MODE_SUBMODE"},
		{name: "DMR with wrong parent", mode: "MFSK", submode: "DMR", wantCode: "INVALID_MODE_SUBMODE"},
		{name: "DSTAR with wrong parent", mode: "RTTY", submode: "DSTAR", wantCode: "INVALID_MODE_SUBMODE"},
		{name: "PSK31 with wrong parent", mode: "MFSK", submode: "PSK31", wantCode: "INVALID_MODE_SUBMODE"},
		{name: "USB with wrong parent", mode: "CW", submode: "USB", wantCode: "INVALID_MODE_SUBMODE"},
		{name: "LSB with wrong parent", mode: "FM", submode: "LSB", wantCode: "INVALID_MODE_SUBMODE"},
		{name: "FT8 should not accept submode", mode: "FT8", submode: "FT8", wantCode: "INVALID_MODE_SUBMODE"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := minimalADIFRecord(tt.mode, tt.submode)
			_, _, err := mapADIFRecord(rec, 7, 99)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if err.Code != tt.wantCode {
				t.Fatalf("error code = %q, want %q", err.Code, tt.wantCode)
			}
		})
	}
}

func minimalADIFRecord(mode, submode string) *adifpkg.Record {
	fields := []adifpkg.Field{
		{Name: "CALL", Value: "K1ABC"},
		{Name: "QSO_DATE", Value: "20260413"},
		{Name: "TIME_ON", Value: "123456"},
		{Name: "BAND", Value: "20m"},
		{Name: "MODE", Value: mode},
	}
	if submode != "" {
		fields = append(fields, adifpkg.Field{Name: "SUBMODE", Value: submode})
	}
	return &adifpkg.Record{Fields: fields}
}
