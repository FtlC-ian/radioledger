package handler

import "testing"

func TestNormalizeCredentialValue_QRZAPIKeyRaw(t *testing.T) {
	got, err := normalizeCredentialValue("qrz", "api_key", "  TEST-KEY  ")
	if err != nil {
		t.Fatalf("normalizeCredentialValue returned error: %v", err)
	}
	want := `{"api_key":"TEST-KEY"}`
	if got != want {
		t.Fatalf("expected %s, got %s", want, got)
	}
}

func TestNormalizeCredentialValue_QRZAPIKeyJSON(t *testing.T) {
	got, err := normalizeCredentialValue("qrz", "api_key", `{"api_key":"JSON-KEY"}`)
	if err != nil {
		t.Fatalf("normalizeCredentialValue returned error: %v", err)
	}
	want := `{"api_key":"JSON-KEY"}`
	if got != want {
		t.Fatalf("expected %s, got %s", want, got)
	}
}

func TestNormalizeCredentialValue_QRZUsernamePasswordJSON(t *testing.T) {
	got, err := normalizeCredentialValue("qrz", "username_password", `{"username":"ian","password":"secret"}`)
	if err != nil {
		t.Fatalf("normalizeCredentialValue returned error: %v", err)
	}
	want := "ian:secret"
	if got != want {
		t.Fatalf("expected %s, got %s", want, got)
	}
}

func TestNormalizeCredentialValue_QRZUsernamePasswordRaw(t *testing.T) {
	got, err := normalizeCredentialValue("qrz", "username_password", "ian:secret")
	if err != nil {
		t.Fatalf("normalizeCredentialValue returned error: %v", err)
	}
	want := "ian:secret"
	if got != want {
		t.Fatalf("expected %s, got %s", want, got)
	}
}

func TestNormalizeCredentialValue_NonQRZUnchanged(t *testing.T) {
	got, err := normalizeCredentialValue("clublog", "api_key", "  abc123  ")
	if err != nil {
		t.Fatalf("normalizeCredentialValue returned error: %v", err)
	}
	if got != "abc123" {
		t.Fatalf("expected trimmed value, got %s", got)
	}
}
