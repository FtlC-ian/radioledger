package adif_test

import (
	"testing"

	"github.com/FtlC-ian/radioledger/pkg/adif"
)

func TestParseCallsignSimple(t *testing.T) {
	tests := []struct {
		input          string
		wantBase       string
		wantPrefix     string
		wantSuffix     string
		wantWPX        string
	}{
		// Simple calls
		{"W1AW",    "W1AW",    "",    "",    "W1"},
		{"K5XX",    "K5XX",    "",    "",    "K5"},
		{"VE3XYZ",  "VE3XYZ",  "",    "",    "VE3"},
		{"DL1ABC",  "DL1ABC",  "",    "",    "DL1"},
		{"JA1TEST", "JA1TEST", "",    "",    "JA1"},
		{"3W3RR",   "3W3RR",   "",    "",    "3W3"},
		{"9A1AA",   "9A1AA",   "",    "",    "9A1"},
		// Suffix modifiers
		{"W5XXX/P",  "W5XXX",  "",    "P",   "W5"},
		{"W5XXX/M",  "W5XXX",  "",    "M",   "W5"},
		{"W5XXX/MM", "W5XXX",  "",    "MM",  "W5"},
		{"W5XXX/AM", "W5XXX",  "",    "AM",  "W5"},
		{"W5XXX/QRP","W5XXX",  "",    "QRP", "W5"},
		// Prefix overrides
		{"VK9/W5XXX",   "W5XXX", "VK9", "",  "VK9"},
		{"KH0/W5XXX",   "W5XXX", "KH0", "",  "KH0"},
		{"5B4/G3ZZZ",   "G3ZZZ", "5B4", "",  "5B4"},
		// Both prefix and suffix
		{"VK9/W5XXX/P", "W5XXX", "VK9", "P", "VK9"},
		// Edge cases
		{"",        "",     "",    "",    ""},
		{"W5XXX",   "W5XXX", "",   "",    "W5"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			pc := adif.ParseCallsign(tt.input)
			if pc.Base != tt.wantBase {
				t.Errorf("Base: got %q, want %q", pc.Base, tt.wantBase)
			}
			if pc.PrefixOverride != tt.wantPrefix {
				t.Errorf("PrefixOverride: got %q, want %q", pc.PrefixOverride, tt.wantPrefix)
			}
			if pc.Suffix != tt.wantSuffix {
				t.Errorf("Suffix: got %q, want %q", pc.Suffix, tt.wantSuffix)
			}
			if pc.WPXPrefix != tt.wantWPX {
				t.Errorf("WPXPrefix: got %q, want %q", pc.WPXPrefix, tt.wantWPX)
			}
		})
	}
}

func TestParseCallsignSuffixInfo(t *testing.T) {
	tests := []struct {
		input       string
		wantHasSuffix bool
		wantDesc    string
	}{
		{"W5XXX/P",  true,  "Portable"},
		{"W5XXX/M",  true,  "Mobile"},
		{"W5XXX/MM", true,  "Maritime Mobile"},
		{"W5XXX/AM", true,  "Aeronautical Mobile"},
		{"W5XXX/QRP",true,  "Low power (5W or less)"},
		{"W5XXX/X",  false, ""},  // Unknown suffix
		{"W5XXX",    false, ""},  // No suffix
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			pc := adif.ParseCallsign(tt.input)
			hasSuffix := pc.SuffixInfo != nil
			if hasSuffix != tt.wantHasSuffix {
				t.Errorf("SuffixInfo: hasSuffix=%v, want %v", hasSuffix, tt.wantHasSuffix)
			}
			if tt.wantHasSuffix && pc.SuffixInfo.Description != tt.wantDesc {
				t.Errorf("SuffixInfo.Description: got %q, want %q", pc.SuffixInfo.Description, tt.wantDesc)
			}
		})
	}
}

func TestParseCallsignUppercase(t *testing.T) {
	// Input is normalized to uppercase
	pc := adif.ParseCallsign("w5xxx/p")
	if pc.Raw != "W5XXX/P" {
		t.Errorf("Raw: got %q, want W5XXX/P", pc.Raw)
	}
	if pc.Base != "W5XXX" {
		t.Errorf("Base: got %q, want W5XXX", pc.Base)
	}
	if pc.Suffix != "P" {
		t.Errorf("Suffix: got %q, want P", pc.Suffix)
	}
}

func TestNormalizeCallsign(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"w1aw",    "W1AW"},
		{"  K5XX  ", "K5XX"},
		{"VE3XYZ",  "VE3XYZ"},
		{"",        ""},
	}
	for _, tt := range tests {
		if got := adif.NormalizeCallsign(tt.input); got != tt.want {
			t.Errorf("NormalizeCallsign(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestWPXPrefixExamples(t *testing.T) {
	// WPX prefix examples from ADIF/contest documentation
	tests := []struct {
		callsign string
		wantWPX  string
	}{
		{"W1AW",   "W1"},
		{"W5XXX",  "W5"},
		{"K5REX",  "K5"},
		{"N6ABC",  "N6"},
		{"VE3XYZ", "VE3"},
		{"VK2ABC", "VK2"},
		{"DL1ABC", "DL1"},
		{"G3ZZZ",  "G3"},
		{"JA1ABC", "JA1"},
		{"9A1AA",  "9A1"},
		{"3W3RR",  "3W3"},
	}
	for _, tt := range tests {
		t.Run(tt.callsign, func(t *testing.T) {
			pc := adif.ParseCallsign(tt.callsign)
			if pc.WPXPrefix != tt.wantWPX {
				t.Errorf("WPXPrefix for %q: got %q, want %q", tt.callsign, pc.WPXPrefix, tt.wantWPX)
			}
		})
	}
}

func TestParseCallsignNoDXCCHardcoding(t *testing.T) {
	// Verify that ParseCallsign does NOT return DXCC entity numbers.
	// DXCC resolution requires a database lookup (this package doesn't do that).
	pc := adif.ParseCallsign("VK9/W5XXX")
	// We should only have component parts, not DXCC entity numbers.
	if pc.PrefixOverride != "VK9" {
		t.Errorf("PrefixOverride: got %q, want VK9", pc.PrefixOverride)
	}
	// No DXCC entity number field on ParsedCallsign (intentional — requires DB lookup)
}
