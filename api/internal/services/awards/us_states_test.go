package awards

import "testing"

func TestNormalizeUSState(t *testing.T) {
	cases := []struct {
		input   string
		want    string
		wantOK  bool
	}{
		// Two-letter abbreviations — canonical form.
		{"AR", "AR", true},
		{"TX", "TX", true},
		{"CA", "CA", true},
		{"WY", "WY", true},
		// Lowercase abbreviations should be normalized to upper.
		{"ar", "AR", true},
		{"tx", "TX", true},
		// Full state names (upper).
		{"ARKANSAS", "AR", true},
		{"TEXAS", "TX", true},
		{"CALIFORNIA", "CA", true},
		{"WYOMING", "WY", true},
		// Full state names (mixed case / lower).
		{"Arkansas", "AR", true},
		{"arkansas", "AR", true},
		// Full state names with extra whitespace.
		{"  Arkansas  ", "AR", true},
		// Multi-word states.
		{"NEW YORK", "NY", true},
		{"New York", "NY", true},
		{"north dakota", "ND", true},
		{"WEST VIRGINIA", "WV", true},
		{"Rhode Island", "RI", true},
		// Period-separated abbreviations (e.g. "A.R." → "AR").
		{"A.R.", "AR", true},
		{"T.X.", "TX", true},
		// Empty and whitespace-only input.
		{"", "", false},
		{"   ", "", false},
		// Invalid abbreviations.
		{"XX", "", false},
		{"ZZ", "", false},
		// Invalid full name.
		{"NOTASTATE", "", false},
		// Territories not in the 50-state list.
		{"PUERTO RICO", "", false},
	}

	for _, tc := range cases {
		got, ok := NormalizeUSState(tc.input)
		if ok != tc.wantOK {
			t.Errorf("NormalizeUSState(%q): got ok=%v want ok=%v", tc.input, ok, tc.wantOK)
			continue
		}
		if ok && got != tc.want {
			t.Errorf("NormalizeUSState(%q): got %q want %q", tc.input, got, tc.want)
		}
	}
}

// TestNormalizeUSState_FullNameEqualsAbbrev verifies that a full state name and
// its two-letter abbreviation normalize to the same canonical code.  This is
// the core property that fixes the duplicate-entity-key bug in refreshWAS().
func TestNormalizeUSState_FullNameEqualsAbbrev(t *testing.T) {
	pairs := []struct{ full, abbrev string }{
		{"ARKANSAS", "AR"},
		{"TEXAS", "TX"},
		{"CALIFORNIA", "CA"},
		{"NEW YORK", "NY"},
		{"WEST VIRGINIA", "WV"},
		{"NORTH CAROLINA", "NC"},
		{"SOUTH DAKOTA", "SD"},
	}

	for _, p := range pairs {
		codeFromFull, okFull := NormalizeUSState(p.full)
		codeFromAbbrev, okAbbrev := NormalizeUSState(p.abbrev)
		if !okFull || !okAbbrev {
			t.Errorf("pair (%q, %q): one or both failed to normalize (okFull=%v okAbbrev=%v)",
				p.full, p.abbrev, okFull, okAbbrev)
			continue
		}
		if codeFromFull != codeFromAbbrev {
			t.Errorf("NormalizeUSState(%q)=%q != NormalizeUSState(%q)=%q — must be equal",
				p.full, codeFromFull, p.abbrev, codeFromAbbrev)
		}
	}
}
