package awards_test

import (
	"testing"

	"github.com/FtlC-ian/radioledger/api/internal/services/awards"
)

func TestWPXPrefix(t *testing.T) {
	tests := []struct {
		callsign string
		want     string
	}{
		// Standard North American calls
		{"W1AW", "W1"},
		{"K5ABC", "K5"},
		{"N7XY", "N7"},
		{"WB4GHN", "WB4"},
		{"KA9XYZ", "KA9"},

		// DX calls
		{"DL1ABC", "DL1"},
		{"G3ZZZ", "G3"},
		{"JA1XYZ", "JA1"},
		{"VK2ABC", "VK2"},
		{"ZL3XYZ", "ZL3"},
		{"PY5ABC", "PY5"},
		{"OH2BH", "OH2"},
		{"SM5ABC", "SM5"},
		{"F5UII", "F5"},

		// Portable calls — /P suffix
		{"W1AW/P", "W1"},
		{"DL1ABC/P", "DL1"},
		{"G3ZZZ/M", "G3"},

		// Portable from foreign entity — use left part
		{"W6/DL1ABC", "W6"},
		{"KH6/W1AW", "KH6"},
		{"VK9X/W1AW", "VK9"},

		// District indicator
		{"W1AW/6", "W1"},

		// Edge cases
		{"", ""},
		{"VK9X", "VK9"},
		{"A61ZX", "A6"},  // A=letter, 6=first digit → A6

		// Unusual no-digit calls (treated as full prefix)
		{"PA", "PA"},
	}

	for _, tt := range tests {
		t.Run(tt.callsign, func(t *testing.T) {
			got := awards.WPXPrefix(tt.callsign)
			if got != tt.want {
				t.Errorf("WPXPrefix(%q) = %q, want %q", tt.callsign, got, tt.want)
			}
		})
	}
}
