package confirmation

import (
	"testing"
)

func TestNormalizeModeGroup(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"USB", "SSB"},
		{"LSB", "SSB"},
		{"AM",  "SSB"},
		{"SSB", "SSB"},
		{"usb", "SSB"},
		{"lsb", "SSB"},
		{"CW",  "CW"},
		{"CWR", "CW"},
		{"cw",  "CW"},
		{"cwr", "CW"},
		{"RTTY",   "RTTY"},
		{"BAUDOT", "RTTY"},
		{"rtty",   "RTTY"},
		{"FT8",  "FT8"},
		{"FT4",  "FT4"},
		{"JS8",  "JS8"},
		{"ft8",  "FT8"},
		{"DMR",  "DMR"},
		{"",     ""},
		{"  SSB  ", "SSB"},
	}

	for _, tt := range tests {
		got := NormalizeModeGroup(tt.input)
		if got != tt.expected {
			t.Errorf("NormalizeModeGroup(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestDetermineConfirmationStatus(t *testing.T) {
	tests := []struct {
		ourV     string
		theirV   string
		matched  bool
		expected string
	}{
		{"none",  "none",  false, "unconfirmed"},
		{"email", "email", false, "unconfirmed"},
		{"none",  "none",  true,  "matched"},
		{"email", "none",  true,  "matched"},
		{"none",  "email", true,  "matched"},
		{"email", "email", true,  "confirmed"},
		{"address", "email", true, "confirmed"},
		{"cross_verified", "cross_verified", true, "confirmed"},
	}

	for _, tt := range tests {
		got := DetermineConfirmationStatus(tt.ourV, tt.theirV, tt.matched)
		if got != tt.expected {
			t.Errorf("DetermineConfirmationStatus(%q, %q, %v) = %q, want %q",
				tt.ourV, tt.theirV, tt.matched, got, tt.expected)
		}
	}
}

func TestBestMatch(t *testing.T) {
	t.Run("empty slice returns nil", func(t *testing.T) {
		result := BestMatch(nil)
		if result != nil {
			t.Errorf("expected nil, got %+v", result)
		}
	})

	t.Run("single candidate", func(t *testing.T) {
		candidates := []MatchCandidate{{QSOID: 1, Confidence: 0.9}}
		result := BestMatch(candidates)
		if result == nil || result.QSOID != 1 {
			t.Errorf("expected qso_id=1, got %+v", result)
		}
	})

	t.Run("picks highest confidence", func(t *testing.T) {
		candidates := []MatchCandidate{
			{QSOID: 1, Confidence: 0.9},
			{QSOID: 2, Confidence: 1.0},
			{QSOID: 3, Confidence: 0.8},
		}
		result := BestMatch(candidates)
		if result == nil || result.QSOID != 2 {
			t.Errorf("expected qso_id=2 (highest confidence), got %+v", result)
		}
	})

	t.Run("equal confidence picks first", func(t *testing.T) {
		candidates := []MatchCandidate{
			{QSOID: 10, Confidence: 1.0},
			{QSOID: 20, Confidence: 1.0},
		}
		result := BestMatch(candidates)
		if result == nil || result.QSOID != 10 {
			t.Errorf("expected qso_id=10 (first with equal confidence), got %+v", result)
		}
	})
}
