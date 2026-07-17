package jobs

import (
	"testing"
	"time"
)

func TestSyncTierInterval(t *testing.T) {
	tests := []struct {
		tier     string
		expected time.Duration
	}{
		{"free", 7 * 24 * time.Hour},
		{"standard", 24 * time.Hour},
		{"premium", 1 * time.Hour},
		{"club", 1 * time.Hour},
		{"", 7 * 24 * time.Hour},         // unknown → weekly
		{"enterprise", 7 * 24 * time.Hour}, // unknown → weekly
	}

	for _, tc := range tests {
		t.Run(tc.tier, func(t *testing.T) {
			got := syncTierInterval(tc.tier)
			if got != tc.expected {
				t.Errorf("syncTierInterval(%q) = %v; want %v", tc.tier, got, tc.expected)
			}
		})
	}
}

func TestAutoSyncSchedulerArgs_Kind(t *testing.T) {
	args := AutoSyncSchedulerArgs{}
	if args.Kind() != "auto_sync_scheduler" {
		t.Errorf("unexpected Kind: %q", args.Kind())
	}
}
