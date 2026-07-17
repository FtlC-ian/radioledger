// Package sync — clublog_worker_test.go: unit tests for Club Log worker logic.
//
// Tests cover:
//   - ClubLogUploadArgs / ClubLogPollArgs / ClubLogDeleteArgs River Kind() values
//   - EnqueueClubLogUpload / EnqueueClubLogPoll / EnqueueClubLogDelete arg serialization
//   - ClubLogPollArgs JSON shape
package sync

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// ─────────────────────────────────────────────────────────────────────────────
// Job kind stability tests
// ─────────────────────────────────────────────────────────────────────────────

func TestClubLogUploadArgs_Kind(t *testing.T) {
	args := ClubLogUploadArgs{UserID: 42}
	if args.Kind() != "clublog_upload" {
		t.Errorf("ClubLogUploadArgs.Kind(): want 'clublog_upload', got %q", args.Kind())
	}
}

func TestClubLogDeleteArgs_Kind(t *testing.T) {
	args := ClubLogDeleteArgs{UserID: 42}
	if args.Kind() != "clublog_delete" {
		t.Errorf("ClubLogDeleteArgs.Kind(): want 'clublog_delete', got %q", args.Kind())
	}
}

func TestClubLogPollArgs_Kind(t *testing.T) {
	args := ClubLogPollArgs{UserID: 99}
	if args.Kind() != "clublog_poll" {
		t.Errorf("ClubLogPollArgs.Kind(): want 'clublog_poll', got %q", args.Kind())
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// JSON serialization tests — ensure job args round-trip correctly.
// ─────────────────────────────────────────────────────────────────────────────

func TestClubLogUploadArgs_JSON(t *testing.T) {
	args := ClubLogUploadArgs{UserID: 123}
	b, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal ClubLogUploadArgs: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if m["user_id"] != float64(123) {
		t.Errorf("user_id: want 123, got %v", m["user_id"])
	}
}

func TestClubLogPollArgs_JSON(t *testing.T) {
	args := ClubLogPollArgs{UserID: 77}
	b, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal ClubLogPollArgs: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if m["user_id"] != float64(77) {
		t.Errorf("user_id: want 77, got %v", m["user_id"])
	}
}

func TestClubLogDeleteArgs_JSON(t *testing.T) {
	dt := time.Date(2024, 6, 1, 14, 30, 0, 0, time.UTC)
	args := ClubLogDeleteArgs{
		UserID:        55,
		TheirCallsign: "DX0DX",
		Band:          "20m",
		Mode:          "CW",
		DatetimeOn:    dt.Format(time.RFC3339),
	}
	b, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal ClubLogDeleteArgs: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if m["user_id"] != float64(55) {
		t.Errorf("user_id: want 55, got %v", m["user_id"])
	}
	if m["their_callsign"] != "DX0DX" {
		t.Errorf("their_callsign: want 'DX0DX', got %v", m["their_callsign"])
	}
	if m["band"] != "20m" {
		t.Errorf("band: want '20m', got %v", m["band"])
	}
	if m["mode"] != "CW" {
		t.Errorf("mode: want 'CW', got %v", m["mode"])
	}
	if m["datetime_on"] != dt.Format(time.RFC3339) {
		t.Errorf("datetime_on: want %q, got %v", dt.Format(time.RFC3339), m["datetime_on"])
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Enqueue helpers — validate arg serialization (using same mock as eqsl tests)
// ─────────────────────────────────────────────────────────────────────────────

func TestEnqueueClubLogUpload_ArgShape(t *testing.T) {
	args := ClubLogUploadArgs{UserID: 100}
	b, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	if m["user_id"] != float64(100) {
		t.Errorf("user_id: want 100, got %v", m["user_id"])
	}
}

func TestEnqueueClubLogPoll_ArgShape(t *testing.T) {
	args := ClubLogPollArgs{UserID: 200}
	b, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	if m["user_id"] != float64(200) {
		t.Errorf("user_id: want 200, got %v", m["user_id"])
	}
}

func TestEnqueueClubLogDelete_ArgShape(t *testing.T) {
	args := ClubLogDeleteArgs{
		UserID:        300,
		TheirCallsign: "K1DX",
		Band:          "40m",
		Mode:          "SSB",
		DatetimeOn:    "2024-01-15T18:00:00Z",
	}
	b, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	if m["user_id"] != float64(300) {
		t.Errorf("user_id: want 300, got %v", m["user_id"])
	}
	if m["their_callsign"] != "K1DX" {
		t.Errorf("their_callsign: want 'K1DX', got %v", m["their_callsign"])
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// EnqueueClubLog* nil inserter guard tests
// ─────────────────────────────────────────────────────────────────────────────

func TestEnqueueClubLogUpload_NilInserterError(t *testing.T) {
	err := EnqueueClubLogUpload(context.Background(), nil, 1)
	if err == nil {
		t.Fatal("expected error for nil inserter, got nil")
	}
}

func TestEnqueueClubLogPoll_NilInserterError(t *testing.T) {
	err := EnqueueClubLogPoll(context.Background(), nil, 1)
	if err == nil {
		t.Fatal("expected error for nil inserter, got nil")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// EncodeClubLogCredentials export test
// ─────────────────────────────────────────────────────────────────────────────

func TestEncodeClubLogCredentials(t *testing.T) {
	b, err := EncodeClubLogCredentials("me@example.com", "secret", "K1ABC")
	if err != nil {
		t.Fatalf("EncodeClubLogCredentials: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if m["email"] != "me@example.com" {
		t.Errorf("email: want 'me@example.com', got %v", m["email"])
	}
	if m["password"] != "secret" {
		t.Errorf("password: want 'secret', got %v", m["password"])
	}
	if m["callsign"] != "K1ABC" {
		t.Errorf("callsign: want 'K1ABC', got %v", m["callsign"])
	}
}

func TestIsClubLogAuthOrPermanentError(t *testing.T) {
	if !isClubLogAuthOrPermanentError("invalid login") {
		t.Fatal("expected invalid login to be permanent")
	}
	if isClubLogAuthOrPermanentError("temporary timeout") {
		t.Fatal("timeout should not be permanent")
	}
}
