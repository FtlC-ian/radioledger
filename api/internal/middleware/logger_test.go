package middleware

import (
	"net/http/httptest"
	"testing"
)

func TestSanitizedPathForLogs_RedactsAccessToken(t *testing.T) {
	req := httptest.NewRequest("GET", "/v1/import/123/stream?access_token=secret&foo=bar", nil)

	got := sanitizedPathForLogs(req)
	want := "/v1/import/123/stream?access_token=%5BREDACTED%5D&foo=bar"
	if got != want {
		t.Fatalf("sanitized path mismatch: got %q want %q", got, want)
	}
}

func TestSanitizedPathForLogs_OnlyAccessTokenQuery(t *testing.T) {
	req := httptest.NewRequest("GET", "/v1/import/123/stream?access_token=secret", nil)

	got := sanitizedPathForLogs(req)
	want := "/v1/import/123/stream?access_token=%5BREDACTED%5D"
	if got != want {
		t.Fatalf("sanitized path mismatch: got %q want %q", got, want)
	}
}

func TestSanitizedPathForLogs_RedactsStreamToken(t *testing.T) {
	req := httptest.NewRequest("GET", "/v1/import/123/stream?stream_token=topsecret&foo=bar", nil)

	got := sanitizedPathForLogs(req)
	want := "/v1/import/123/stream?foo=bar&stream_token=%5BREDACTED%5D"
	if got != want {
		t.Fatalf("sanitized path mismatch: got %q want %q", got, want)
	}
}
