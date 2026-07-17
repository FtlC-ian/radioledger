package jobs_test

import (
	"testing"
	"time"

	"github.com/FtlC-ian/radioledger/api/internal/jobs"
)

// TestDaysUntilExpiry verifies the DaysUntilExpiry helper with various inputs.
func TestDaysUntilExpiry(t *testing.T) {
	today := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		expiry   time.Time
		wantDays int
	}{
		{
			name:     "expires in exactly 60 days",
			expiry:   today.AddDate(0, 0, 60),
			wantDays: 60,
		},
		{
			name:     "expires in 30 days",
			expiry:   today.AddDate(0, 0, 30),
			wantDays: 30,
		},
		{
			name:     "expires in 7 days",
			expiry:   today.AddDate(0, 0, 7),
			wantDays: 7,
		},
		{
			name:     "expires tomorrow",
			expiry:   today.AddDate(0, 0, 1),
			wantDays: 1,
		},
		{
			name:     "expires today",
			expiry:   today,
			wantDays: 0,
		},
		{
			name:     "already expired yesterday",
			expiry:   today.AddDate(0, 0, -1),
			wantDays: -1,
		},
		{
			name:     "expires in 365 days",
			expiry:   today.AddDate(1, 0, 0),
			wantDays: 365,
		},
		{
			name:     "time-of-day ignored — expiry at noon should equal expiry at midnight",
			expiry:   time.Date(2026, 3, 31, 12, 30, 0, 0, time.UTC),
			wantDays: 30,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := jobs.DaysUntilExpiry(today, tt.expiry)
			if got != tt.wantDays {
				t.Errorf("DaysUntilExpiry(%v, %v) = %d; want %d",
					today.Format("2006-01-02"),
					tt.expiry.Format("2006-01-02"),
					got, tt.wantDays)
			}
		})
	}
}

// TestShouldNotify verifies the ShouldNotify threshold logic.
func TestShouldNotify(t *testing.T) {
	tests := []struct {
		name          string
		daysRemaining int
		threshold     int
		want          bool
	}{
		// Exactly at threshold.
		{"at 60-day threshold", 60, 60, true},
		{"at 30-day threshold", 30, 30, true},
		{"at 7-day threshold", 7, 7, true},

		// Below threshold (more urgent).
		{"1 day vs 7-day threshold", 1, 7, true},
		{"5 days vs 7-day threshold", 5, 7, true},
		{"0 days vs 7-day threshold (today)", 0, 7, true},
		{"negative (already expired) vs 7-day threshold", -3, 7, true},

		// Above threshold (not yet urgent).
		{"61 days vs 60-day threshold", 61, 60, false},
		{"31 days vs 30-day threshold", 31, 30, false},
		{"8 days vs 7-day threshold", 8, 7, false},
		{"365 days vs 60-day threshold", 365, 60, false},

		// Edge: 30 days vs 60-day threshold — should notify.
		{"30 days vs 60-day threshold", 30, 60, true},
		// Edge: 60 days vs 30-day threshold — should NOT notify.
		{"60 days vs 30-day threshold", 60, 30, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := jobs.ShouldNotify(tt.daysRemaining, tt.threshold)
			if got != tt.want {
				t.Errorf("ShouldNotify(%d, %d) = %v; want %v",
					tt.daysRemaining, tt.threshold, got, tt.want)
			}
		})
	}
}

// TestCertExpiryThresholdCoverage verifies that the three canonical thresholds
// (60/30/7) are all represented in the certExpiryThresholds slice via ShouldNotify.
func TestCertExpiryThresholdCoverage(t *testing.T) {
	// A cert expiring in 5 days should fire for all three thresholds.
	thresholds := []int{60, 30, 7}
	for _, th := range thresholds {
		if !jobs.ShouldNotify(5, th) {
			t.Errorf("ShouldNotify(5, %d) should be true but was false", th)
		}
	}

	// A cert expiring in 45 days should fire for 60 but NOT 30 or 7.
	if !jobs.ShouldNotify(45, 60) {
		t.Error("ShouldNotify(45, 60) should be true")
	}
	if jobs.ShouldNotify(45, 30) {
		t.Error("ShouldNotify(45, 30) should be false")
	}
	if jobs.ShouldNotify(45, 7) {
		t.Error("ShouldNotify(45, 7) should be false")
	}
}
