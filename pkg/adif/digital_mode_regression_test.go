package adif_test

import (
	"testing"

	"github.com/FtlC-ian/radioledger/pkg/adif"
)

func TestDigitalModeRegressionNormalizeModePair(t *testing.T) {
	tests := []struct {
		name        string
		mode        string
		submode     string
		wantMode    string
		wantSubmode string
		wantOK      bool
	}{
		{name: "ft2 alias", mode: "FT2", wantMode: "MFSK", wantSubmode: "FT2", wantOK: true},
		{name: "ft2 canonical pair", mode: "MFSK", submode: "FT2", wantMode: "MFSK", wantSubmode: "FT2", wantOK: true},
		{name: "ft4 alias", mode: "FT4", wantMode: "MFSK", wantSubmode: "FT4", wantOK: true},
		{name: "js8 alias", mode: "JS8", wantMode: "MFSK", wantSubmode: "JS8", wantOK: true},
		{name: "q65 alias", mode: "Q65", wantMode: "MFSK", wantSubmode: "Q65", wantOK: true},
		{name: "ft8 top level", mode: "FT8", wantMode: "FT8", wantOK: true},
		{name: "legacy mfsk ft8 pair", mode: "MFSK", submode: "FT8", wantMode: "FT8", wantOK: true},
		{name: "dmr alias", mode: "DMR", wantMode: "DIGITALVOICE", wantSubmode: "DMR", wantOK: true},
		{name: "digitalvoice dmr pair", mode: "DIGITALVOICE", submode: "DMR", wantMode: "DIGITALVOICE", wantSubmode: "DMR", wantOK: true},
		{name: "packet alias", mode: "PACKET", wantMode: "PKT", wantOK: true},
		{name: "pkt canonical", mode: "PKT", wantMode: "PKT", wantOK: true},
		{name: "invalid ft2 parent", mode: "PKT", submode: "FT2", wantOK: false},
		{name: "invalid q65 parent", mode: "PSK", submode: "Q65", wantOK: false},
		{name: "invalid digitalvoice ft4", mode: "DIGITALVOICE", submode: "FT4", wantOK: false},
		{name: "invalid ft8 submode", mode: "FT8", submode: "FT8", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := adif.NormalizeModePair(tt.mode, tt.submode)
			if ok != tt.wantOK {
				t.Fatalf("NormalizeModePair(%q, %q) ok=%v, want %v", tt.mode, tt.submode, ok, tt.wantOK)
			}
			if !tt.wantOK {
				return
			}
			if got.Mode != tt.wantMode || got.Submode != tt.wantSubmode {
				t.Fatalf("NormalizeModePair(%q, %q)=(%q, %q), want (%q, %q)", tt.mode, tt.submode, got.Mode, got.Submode, tt.wantMode, tt.wantSubmode)
			}
		})
	}
}

func TestDigitalModeRegressionResolveModeAliases(t *testing.T) {
	tests := []struct {
		input       string
		wantMode    string
		wantSubmode string
	}{
		{input: "FT2", wantMode: "MFSK", wantSubmode: "FT2"},
		{input: "FT4", wantMode: "MFSK", wantSubmode: "FT4"},
		{input: "JS8", wantMode: "MFSK", wantSubmode: "JS8"},
		{input: "Q65", wantMode: "MFSK", wantSubmode: "Q65"},
		{input: "DMR", wantMode: "DIGITALVOICE", wantSubmode: "DMR"},
		{input: "PACKET", wantMode: "PKT"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, ok := adif.ResolveMode(tt.input)
			if !ok {
				t.Fatalf("ResolveMode(%q) returned false", tt.input)
			}
			if got.Mode != tt.wantMode || got.Submode != tt.wantSubmode {
				t.Fatalf("ResolveMode(%q)=(%q, %q), want (%q, %q)", tt.input, got.Mode, got.Submode, tt.wantMode, tt.wantSubmode)
			}
		})
	}
}

func TestDigitalModeRegressionCanonicalizeRecordMode(t *testing.T) {
	tests := []struct {
		name        string
		mode        string
		submode     string
		wantMode    string
		wantSubmode string
	}{
		{name: "ft2 alias", mode: "FT2", wantMode: "MFSK", wantSubmode: "FT2"},
		{name: "ft8 direct", mode: "FT8", wantMode: "FT8"},
		{name: "packet alias", mode: "PACKET", wantMode: "PKT"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := &adif.Record{}
			rec.Set("MODE", tt.mode)
			if tt.submode != "" {
				rec.Set("SUBMODE", tt.submode)
			}

			if changed := adif.CanonicalizeRecordMode(rec); !changed {
				t.Fatal("expected canonicalization to succeed")
			}
			if got := rec.Get("MODE"); got != tt.wantMode {
				t.Fatalf("MODE=%q, want %q", got, tt.wantMode)
			}
			if got := rec.Get("SUBMODE"); got != tt.wantSubmode {
				t.Fatalf("SUBMODE=%q, want %q", got, tt.wantSubmode)
			}
		})
	}
}
