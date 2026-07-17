package pota

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeCredentials(t *testing.T) {
	t.Run("token json", func(t *testing.T) {
		creds, err := DecodeCredentials([]byte(`{"jwt":"abc123"}`))
		if err != nil {
			t.Fatalf("DecodeCredentials error: %v", err)
		}
		if got := creds.bearerToken(); got != "abc123" {
			t.Fatalf("token = %q, want abc123", got)
		}
	})

	t.Run("username password colon", func(t *testing.T) {
		creds, err := DecodeCredentials([]byte("user:pass"))
		if err != nil {
			t.Fatalf("DecodeCredentials error: %v", err)
		}
		if creds.Username != "user" || creds.Password != "pass" {
			t.Fatalf("got username/password %q/%q", creds.Username, creds.Password)
		}
	})

	t.Run("raw token", func(t *testing.T) {
		creds, err := DecodeCredentials([]byte("token-value"))
		if err != nil {
			t.Fatalf("DecodeCredentials error: %v", err)
		}
		if creds.JWT != "token-value" {
			t.Fatalf("jwt = %q", creds.JWT)
		}
	})
}

func TestUploadADIF(t *testing.T) {
	var authHeader string
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := New()
	c.apiBase = srv.URL
	res, err := c.UploadADIF(context.Background(), &Credentials{JWT: "abc"}, "<EOH><EOR>")
	if err != nil {
		t.Fatalf("UploadADIF error: %v", err)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", res.StatusCode)
	}
	if authHeader != "Bearer abc" {
		t.Fatalf("Authorization header = %q", authHeader)
	}
	if !strings.Contains(body, "radioledger.adi") {
		t.Fatalf("multipart payload missing filename")
	}
	if !strings.Contains(body, "<EOH><EOR>") {
		t.Fatalf("multipart payload missing ADIF content")
	}
}
