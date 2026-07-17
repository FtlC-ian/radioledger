package middleware

import (
	"testing"
	"time"
)

func TestStreamToken_OneTimeUse(t *testing.T) {
	path := "/v1/import/11111111-1111-1111-1111-111111111111/stream"
	token, err := IssueStreamToken(42, path, 30*time.Second)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}

	userID, err := ConsumeStreamToken(token, path, time.Now())
	if err != nil {
		t.Fatalf("consume token first use: %v", err)
	}
	if userID != 42 {
		t.Fatalf("expected user id 42 to round-trip, got %d", userID)
	}

	_, err = ConsumeStreamToken(token, path, time.Now())
	if err == nil {
		t.Fatal("expected second consume to fail")
	}
}

func TestStreamToken_RejectsWrongPath(t *testing.T) {
	token, err := IssueStreamToken(7, "/v1/import/aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa/stream", 30*time.Second)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}

	_, err = ConsumeStreamToken(token, "/v1/import/bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb/stream", time.Now())
	if err == nil {
		t.Fatal("expected consume to fail for mismatched stream path")
	}
}

func TestStreamToken_Expires(t *testing.T) {
	token, err := IssueStreamToken(99, "/v1/import/cccccccc-cccc-cccc-cccc-cccccccccccc/stream", 10*time.Millisecond)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}

	_, err = ConsumeStreamToken(token, "/v1/import/cccccccc-cccc-cccc-cccc-cccccccccccc/stream", time.Now().Add(50*time.Millisecond))
	if err == nil {
		t.Fatal("expected expired token to fail")
	}
}
