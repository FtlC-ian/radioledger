package hamqth

import (
	"testing"
)

func TestDecodeCredentials_Valid(t *testing.T) {
	data, err := EncodeCredentials("W1ABC", "secret123")
	if err != nil {
		t.Fatalf("EncodeCredentials: %v", err)
	}
	creds, err := DecodeCredentials(data)
	if err != nil {
		t.Fatalf("DecodeCredentials: %v", err)
	}
	if creds.Username != "W1ABC" {
		t.Errorf("expected Username=W1ABC, got %q", creds.Username)
	}
	if creds.Password != "secret123" {
		t.Errorf("expected Password=secret123, got %q", creds.Password)
	}
}

func TestDecodeCredentials_MissingFields(t *testing.T) {
	data, _ := EncodeCredentials("", "")
	_, err := DecodeCredentials(data)
	if err == nil {
		t.Fatal("expected error for empty credentials")
	}
}

func TestIsSessionExpiredError(t *testing.T) {
	cases := []struct {
		msg      string
		expected bool
	}{
		{"Session does not exist or expired. Please login first.", true},
		{"session expired", true},
		{"Please login again", true},
		{"Wrong username or password", false},
		{"", false},
	}

	for _, tc := range cases {
		got := isSessionExpiredError(tc.msg)
		if got != tc.expected {
			t.Errorf("isSessionExpiredError(%q) = %v, want %v", tc.msg, got, tc.expected)
		}
	}
}

func TestIsAuthOrPermanentError(t *testing.T) {
	cases := []struct {
		msg      string
		expected bool
	}{
		{"Wrong username or password!", true},
		{"authentication failed", true},
		{"banned", true},
		{"suspended", true},
		{"credentials missing", true},
		{"session does not exist", false}, // session expiry ≠ auth failure
		{"timeout", false},
		{"", false},
	}

	for _, tc := range cases {
		got := IsAuthOrPermanentError(tc.msg)
		if got != tc.expected {
			t.Errorf("IsAuthOrPermanentError(%q) = %v, want %v", tc.msg, got, tc.expected)
		}
	}
}

func TestParseUploadResponse_OK(t *testing.T) {
	body := []byte(`<?xml version="1.0"?>
<HamQTH version="2.7" xmlns="https://www.hamqth.com">
<qso_upload>
<result>OK</result>
<count>3</count>
</qso_upload>
</HamQTH>`)

	result, expired, err := parseUploadResponse(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if expired {
		t.Fatal("expected expired=false")
	}
	if result.Count != 3 {
		t.Errorf("expected count=3, got %d", result.Count)
	}
}

func TestParseUploadResponse_SessionExpired(t *testing.T) {
	body := []byte(`<?xml version="1.0"?>
<HamQTH version="2.7" xmlns="https://www.hamqth.com">
<session>
<error>Session does not exist or expired. Please login first.</error>
</session>
</HamQTH>`)

	_, expired, err := parseUploadResponse(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !expired {
		t.Fatal("expected expired=true for session expiry")
	}
}

func TestParseUploadResponse_AuthError(t *testing.T) {
	body := []byte(`<?xml version="1.0"?>
<HamQTH version="2.7" xmlns="https://www.hamqth.com">
<session>
<error>Wrong username or password!</error>
</session>
</HamQTH>`)

	_, expired, err := parseUploadResponse(body)
	if err == nil {
		t.Fatal("expected error for auth failure")
	}
	if expired {
		t.Fatal("auth error should not be treated as session expiry")
	}
}

func TestParseUploadResponse_UploadError(t *testing.T) {
	body := []byte(`<?xml version="1.0"?>
<HamQTH version="2.7" xmlns="https://www.hamqth.com">
<qso_upload>
<result>ERROR</result>
<error>Invalid ADIF data</error>
</qso_upload>
</HamQTH>`)

	_, _, err := parseUploadResponse(body)
	if err == nil {
		t.Fatal("expected error for upload error result")
	}
}
