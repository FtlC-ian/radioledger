package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestZitadelResolveEmail_UsesJWTClaimWhenPresent(t *testing.T) {
	z := &ZitadelAuth{}

	email, err := z.resolveEmail(context.Background(), "ignored-token", "  User@Example.COM ")
	if err != nil {
		t.Fatalf("resolveEmail returned error: %v", err)
	}
	if want := "user@example.com"; email != want {
		t.Fatalf("resolveEmail email = %q, want %q", email, want)
	}
}

func TestZitadelResolveEmail_FallsBackToUserInfo(t *testing.T) {
	var authHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"sub":"123","email":"user@example.com","name":"Example User"}`))
	}))
	defer server.Close()

	z := &ZitadelAuth{ZitadelURL: server.URL}

	email, err := z.resolveEmail(context.Background(), "access-token", "")
	if err != nil {
		t.Fatalf("resolveEmail returned error: %v", err)
	}
	if got, want := authHeader, "Bearer access-token"; got != want {
		t.Fatalf("Authorization header = %q, want %q", got, want)
	}
	if want := "user@example.com"; email != want {
		t.Fatalf("resolveEmail email = %q, want %q", email, want)
	}
}

func TestZitadelResolveEmail_ErrorsWhenUserInfoHasNoEmail(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"sub":"123","name":"Example User"}`))
	}))
	defer server.Close()

	z := &ZitadelAuth{ZitadelURL: server.URL}

	_, err := z.resolveEmail(context.Background(), "access-token", "")
	if err == nil {
		t.Fatal("resolveEmail returned nil error")
	}
	if !strings.Contains(err.Error(), "userinfo response missing email") {
		t.Fatalf("resolveEmail error = %q", err)
	}
}
