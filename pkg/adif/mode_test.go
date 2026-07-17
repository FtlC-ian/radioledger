package adif_test

import (
	"testing"

	"github.com/FtlC-ian/radioledger/pkg/adif"
)

func TestResolveMode(t *testing.T) {
	tests := []struct {
		input       string
		wantMode    string
		wantSubmode string
		wantOK      bool
	}{
		{"FT8", "FT8", "", true},
		{"FT2", "MFSK", "FT2", true},
		{"OFDM", "OFDM", "", true},
		{"C4FM", "DIGITALVOICE", "C4FM", true},
		{"DSTAR", "DIGITALVOICE", "DSTAR", true},
		{"PSK31", "PSK", "PSK31", true},
		{"USB", "SSB", "USB", true},
		{"PACKET", "PKT", "", true},
		{"VARAHF", "DYNAMIC", "VARA HF", true},
		{"FREEDATA", "DYNAMIC", "FREEDATA", true},
		{"SCAMP_FAST", "FSK", "SCAMP_FAST", true},
		{"SCAMP_OO", "MTONE", "SCAMP_OO", true},
		{"RIBBIT_PIX", "OFDM", "RIBBIT_PIX", true},
		{"", "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, ok := adif.ResolveMode(tt.input)
			if ok != tt.wantOK {
				t.Fatalf("ResolveMode(%q) ok=%v, want %v", tt.input, ok, tt.wantOK)
			}
			if !tt.wantOK {
				return
			}
			if got.Mode != tt.wantMode || got.Submode != tt.wantSubmode {
				t.Fatalf("ResolveMode(%q) = (%q,%q), want (%q,%q)", tt.input, got.Mode, got.Submode, tt.wantMode, tt.wantSubmode)
			}
		})
	}
}

func TestNormalizeModePair(t *testing.T) {
	tests := []struct {
		name        string
		mode        string
		submode     string
		wantMode    string
		wantSubmode string
		wantOK      bool
	}{
		{"canonical pair", "DIGITALVOICE", "DMR", "DIGITALVOICE", "DMR", true},
		{"legacy direct mode", "DMR", "", "DIGITALVOICE", "DMR", true},
		{"canonical dynamic pair", "DYNAMIC", "VARA HF", "DYNAMIC", "VARA HF", true},
		{"legacy vara alias", "VARAHF", "", "DYNAMIC", "VARA HF", true},
		{"canonical mfsk pair", "MFSK", "FT4", "MFSK", "FT4", true},
		{"canonical ft8 no submode", "FT8", "", "FT8", "", true},
		{"legacy mfsk alias", "FT4", "", "MFSK", "FT4", true},
		{"legacy ft2 alias", "FT2", "", "MFSK", "FT2", true},
		{"legacy dmr alias", "DMR", "", "DIGITALVOICE", "DMR", true},
		{"legacy usb alias", "USB", "", "SSB", "USB", true},
		{"canonical jt65 pair", "JT65", "JT65A", "JT65", "JT65A", true},
		{"canonical jt4 pair", "JT4", "JT4G", "JT4", "JT4G", true},
		{"canonical pac pair", "PAC", "PAC3", "PAC", "PAC3", true},
		{"canonical pax pair", "PAX", "PAX2", "PAX", "PAX2", true},
		{"canonical tor pair", "TOR", "AMTORFEC", "TOR", "AMTORFEC", true},
		{"canonical tor gtor pair", "TOR", "GTOR", "TOR", "GTOR", true},
		{"canonical chip pair", "CHIP", "CHIP128", "CHIP", "CHIP128", true},
		{"canonical domino pair", "DOMINO", "DOMINOF", "DOMINO", "DOMINOF", true},
		{"canonical hell pair", "HELL", "FMHELL", "HELL", "FMHELL", true},
		{"canonical hell pskhell pair", "HELL", "PSKHELL", "HELL", "PSKHELL", true},
		{"canonical thrb pair", "THRB", "THRBX", "THRB", "THRBX", true},
		{"canonical psk pair", "PSK", "FSK31", "PSK", "FSK31", true},
		{"canonical packet", "PKT", "", "PKT", "", true},
		{"ui packet alias", "PACKET", "", "PKT", "", true},
		{"mismatched pair", "DIGITALVOICE", "FT4", "", "", false},
		{"submode alias mismatch", "JT65", "JT4A", "", "", false},
		// Auto-derived aliases for non-MFSK families
		{"legacy scamp_fast alias", "SCAMP_FAST", "", "FSK", "SCAMP_FAST", true},
		{"legacy scamp_oo alias", "SCAMP_OO", "", "MTONE", "SCAMP_OO", true},
		{"legacy ribbit_pix alias", "RIBBIT_PIX", "", "OFDM", "RIBBIT_PIX", true},
		{"legacy ribbit_sms alias", "RIBBIT_SMS", "", "OFDM", "RIBBIT_SMS", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := adif.NormalizeModePair(tt.mode, tt.submode)
			if ok != tt.wantOK {
				t.Fatalf("NormalizeModePair(%q,%q) ok=%v, want %v", tt.mode, tt.submode, ok, tt.wantOK)
			}
			if !tt.wantOK {
				return
			}
			if got.Mode != tt.wantMode || got.Submode != tt.wantSubmode {
				t.Fatalf("NormalizeModePair(%q,%q) = (%q,%q), want (%q,%q)", tt.mode, tt.submode, got.Mode, got.Submode, tt.wantMode, tt.wantSubmode)
			}
		})
	}
}

func TestCanonicalizeRecordMode(t *testing.T) {
	tests := []struct {
		name        string
		mode        string
		submode     string
		wantMode    string
		wantSubmode string
	}{
		{"ft2 alias", "FT2", "", "MFSK", "FT2"},
		{"dmr alias", "DMR", "", "DIGITALVOICE", "DMR"},
		{"usb alias", "USB", "", "SSB", "USB"},
		{"ft8 unchanged", "FT8", "", "FT8", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := &adif.Record{}
			rec.Set("MODE", tt.mode)
			if tt.submode != "" {
				rec.Set("SUBMODE", tt.submode)
			}

			if !adif.CanonicalizeRecordMode(rec) {
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

// TestAutoDerivationCoversAllSubmodes verifies that every entry in
// CanonicalADIFSubmodes is reachable as a bare MODE import alias.
// This is the property guarantee for the auto-derivation loop in buildModeAliases:
// adding a new submode to CanonicalADIFSubmodes must make it automatically
// available as an import alias without any manual ModeAliases entry.
func TestAutoDerivationCoversAllSubmodes(t *testing.T) {
	for submode, wantMode := range adif.CanonicalADIFSubmodes {
		t.Run(submode, func(t *testing.T) {
			got, ok := adif.ResolveMode(submode)
			if !ok {
				t.Fatalf("ResolveMode(%q) returned false; submode not reachable as bare import alias", submode)
			}
			if got.Mode != wantMode {
				t.Fatalf("ResolveMode(%q).Mode=%q, want %q", submode, got.Mode, wantMode)
			}
			if got.Submode != submode {
				t.Fatalf("ResolveMode(%q).Submode=%q, want %q", submode, got.Submode, submode)
			}
		})
	}
}
