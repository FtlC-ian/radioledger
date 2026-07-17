// Package sync — hamqth_worker_test.go: unit tests for HamQTH sync worker logic.
//
// Tests cover:
//   - HamQTHUploadArgs / HamQTHPollArgs River Kind() values
//   - EncodeHamQTHCredentials round-trip
//   - EnqueueHamQTHUpload / EnqueueHamQTHPoll job arg shapes
//   - isHamQTHAuthOrPermanentError classifier
package sync

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/FtlC-ian/radioledger/api/internal/services/hamqth"
)

// ──────────────────────────────────────────────────────────────────────────────
// River job kind stability
// ──────────────────────────────────────────────────────────────────────────────

func TestHamQTHUploadArgs_Kind(t *testing.T) {
	args := HamQTHUploadArgs{UserID: 42}
	if args.Kind() != "hamqth_upload" {
		t.Errorf("HamQTHUploadArgs.Kind(): expected 'hamqth_upload', got %q", args.Kind())
	}
}

func TestHamQTHPollArgs_Kind(t *testing.T) {
	args := HamQTHPollArgs{UserID: 42}
	if args.Kind() != "hamqth_poll" {
		t.Errorf("HamQTHPollArgs.Kind(): expected 'hamqth_poll', got %q", args.Kind())
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Credentials round-trip
// ──────────────────────────────────────────────────────────────────────────────

func TestEncodeHamQTHCredentials_RoundTrip(t *testing.T) {
	encoded, err := EncodeHamQTHCredentials("W1TEST", "s3cr3t!")
	if err != nil {
		t.Fatalf("EncodeHamQTHCredentials: %v", err)
	}
	creds, err := hamqth.DecodeCredentials(encoded)
	if err != nil {
		t.Fatalf("DecodeCredentials: %v", err)
	}
	if creds.Username != "W1TEST" {
		t.Errorf("Username: expected W1TEST, got %q", creds.Username)
	}
	if creds.Password != "s3cr3t!" {
		t.Errorf("Password: expected s3cr3t!, got %q", creds.Password)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Job arg serialization
// ──────────────────────────────────────────────────────────────────────────────

func TestHamQTHUploadArgs_JSONShape(t *testing.T) {
	args := HamQTHUploadArgs{UserID: 99}
	b, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	uid, ok := m["user_id"]
	if !ok {
		t.Fatal("HamQTHUploadArgs JSON missing 'user_id'")
	}
	if uid != float64(99) {
		t.Errorf("user_id: expected 99, got %v", uid)
	}
}

func TestHamQTHPollArgs_JSONShape(t *testing.T) {
	args := HamQTHPollArgs{UserID: 77}
	b, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if m["user_id"] != float64(77) {
		t.Errorf("user_id: expected 77, got %v", m["user_id"])
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Error classifier
// ──────────────────────────────────────────────────────────────────────────────

func TestIsHamQTHAuthOrPermanentError(t *testing.T) {
	cases := []struct {
		msg      string
		expected bool
	}{
		{"Wrong username or password!", true},
		{"authentication failed", true},
		{"suspended", true},
		{"banned", true},
		{"credentials missing", true},
		{"Invalid user", true},
		{"incorrect", true},
		{"timeout", false},
		{"connection refused", false},
		{"status 503", false},
		{"", false},
	}

	for _, tc := range cases {
		got := isHamQTHAuthOrPermanentError(tc.msg)
		if got != tc.expected {
			t.Errorf("isHamQTHAuthOrPermanentError(%q) = %v, want %v", tc.msg, got, tc.expected)
		}
	}
}

func TestEnqueueHamQTHUpload_NilInserter(t *testing.T) {
	err := EnqueueHamQTHUpload(context.Background(), nil, 1)
	if err == nil {
		t.Fatal("expected error for nil RiverInserter")
	}
}

func TestEnqueueHamQTHPoll_NilInserter(t *testing.T) {
	err := EnqueueHamQTHPoll(context.Background(), nil, 1)
	if err == nil {
		t.Fatal("expected error for nil RiverInserter")
	}
}
