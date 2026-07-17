package confirmation

import (
	"context"
	"strings"
	"testing"
)

// TestUpsertConfirmationParams verifies DetermineConfirmationStatus logic
// used inside the worker during upsert.
func TestMatchWorker_StatusTransitions(t *testing.T) {
	tests := []struct {
		name       string
		ourV       string
		theirV     string
		matched    bool
		wantStatus string
	}{
		{"no match, no verification", "none", "none", false, "unconfirmed"},
		{"match, no verification", "none", "none", true, "matched"},
		{"match, one side verified", "email", "none", true, "matched"},
		{"match, both sides verified", "email", "email", true, "confirmed"},
		{"match, cross-verified", "cross_verified", "cross_verified", true, "confirmed"},
		{"no match, both verified", "email", "email", false, "unconfirmed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetermineConfirmationStatus(tt.ourV, tt.theirV, tt.matched)
			if got != tt.wantStatus {
				t.Errorf("DetermineConfirmationStatus(%q, %q, %v) = %q, want %q",
					tt.ourV, tt.theirV, tt.matched, got, tt.wantStatus)
			}
		})
	}
}

// TestLoadQSOForConfirmation_NilOnMissingStationCallsign verifies that when a
// QSO has no station_callsign and no primary callsign set, loadQSOForConfirmation
// returns nil (can't match without knowing who we are).
//
// This is a unit test — no DB required; we test the nil return path directly
// via the exported helper logic in this package.
func TestMatchArgs_Kind(t *testing.T) {
	args := QSOMatchArgs{QSOID: 1, UserID: 2}
	if args.Kind() != "qso_match" {
		t.Errorf("expected kind=qso_match, got %q", args.Kind())
	}
}

func TestCascadeArgs_Kind(t *testing.T) {
	args := CascadeConfirmationArgs{UserID: 1, Callsign: "W1ABC"}
	if args.Kind() != "cascade_confirmation" {
		t.Errorf("expected kind=cascade_confirmation, got %q", args.Kind())
	}
}

// TestEnqueueQSOMatch_NilClient ensures EnqueueQSOMatch returns an error when
// the River client is nil (e.g. in configs without a worker pool).
func TestEnqueueQSOMatch_NilClient(t *testing.T) {
	err := EnqueueQSOMatch(context.Background(), nil, 1, 1)
	if err == nil {
		t.Error("expected error for nil river client")
	}
}

func TestEnqueueCascadeConfirmation_NilClient(t *testing.T) {
	err := EnqueueCascadeConfirmation(context.Background(), nil, 1, "W1ABC")
	if err == nil {
		t.Error("expected error for nil river client")
	}
}

// TestModeFuzzyMatchGroup verifies that the mode groups used by the SQL function
// match what NormalizeModeGroup produces for both sides of a potential match.
func TestLoadQSOForConfirmationSQL_UsesUserCallsignsTable(t *testing.T) {
	if !strings.Contains(loadQSOForConfirmationSQL, "FROM user_callsigns") {
		t.Fatalf("expected fallback query to use user_callsigns table")
	}
	if strings.Contains(loadQSOForConfirmationSQL, "FROM callsigns") {
		t.Fatalf("fallback query should not reference legacy callsigns table")
	}
	if !strings.Contains(loadQSOForConfirmationSQL, "uc.user_id") {
		t.Fatalf("expected fallback query to join on user_callsigns.user_id")
	}
	if !strings.Contains(loadQSOForConfirmationSQL, "uc.is_primary") {
		t.Fatalf("expected fallback query to filter on user_callsigns.is_primary")
	}
	if !strings.Contains(loadQSOForConfirmationSQL, "uc.valid_to") {
		t.Fatalf("expected fallback query to filter on active user_callsigns rows via valid_to")
	}
}

func TestModeFuzzyMatch_BothSidesNormalize(t *testing.T) {
	// USB and LSB should both normalize to SSB and therefore match
	a := NormalizeModeGroup("USB")
	b := NormalizeModeGroup("LSB")
	if a != b {
		t.Errorf("USB and LSB should normalize to same group: %q vs %q", a, b)
	}

	// CW and CWR should both normalize to CW
	c := NormalizeModeGroup("CW")
	d := NormalizeModeGroup("CWR")
	if c != d {
		t.Errorf("CW and CWR should normalize to same group: %q vs %q", c, d)
	}

	// RTTY and BAUDOT should both normalize to RTTY
	e := NormalizeModeGroup("RTTY")
	f := NormalizeModeGroup("BAUDOT")
	if e != f {
		t.Errorf("RTTY and BAUDOT should normalize to same group: %q vs %q", e, f)
	}

	// FT8 should NOT equal FT4 (exact match modes)
	g := NormalizeModeGroup("FT8")
	h := NormalizeModeGroup("FT4")
	if g == h {
		t.Errorf("FT8 and FT4 should NOT normalize to same group")
	}
}
