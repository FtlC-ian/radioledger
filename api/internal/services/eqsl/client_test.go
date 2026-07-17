// Package eqsl — client_test.go: unit tests for the eQSL HTTP client.
//
// All tests use an in-process httptest.Server to mock eQSL responses.
// No real network calls are made.
package eqsl

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// rewriteHostTransport is a test-only RoundTripper that rewrites the request URL
// host to a given test server URL while preserving the path and query string.
type rewriteHostTransport struct {
	base string // e.g. "http://127.0.0.1:12345"
}

func (t rewriteHostTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	cloned := req.Clone(req.Context())
	cloned.URL.Scheme = "http"
	base := strings.TrimPrefix(t.base, "http://")
	base = strings.TrimPrefix(base, "https://")
	cloned.URL.Host = base
	return http.DefaultTransport.RoundTrip(cloned)
}

// newTestClient creates an eQSL client with a custom http.Client pointing at the test server.
func newTestClientWithURLs(srv *httptest.Server) *Client {
	creds := &UsernamePassword{
		Username: "testuser",
		Password: "testpass",
	}
	c := New(creds, nil)
	c.httpClient = &http.Client{
		Transport: rewriteHostTransport{base: srv.URL},
		Timeout:   5 * time.Second,
	}
	return c
}

// ──────────────────────────────────────────────────────────────────────────────
// UploadADIF tests
// ──────────────────────────────────────────────────────────────────────────────

func TestUploadADIF_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
		}
		if r.FormValue("EQSL_USER") != "testuser" {
			t.Errorf("EQSL_USER: expected 'testuser', got %q", r.FormValue("EQSL_USER"))
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`<html><body>Result: 2 out of 2 records added</body></html>`))
	}))
	defer srv.Close()

	c := newTestClientWithURLs(srv)
	result, err := c.UploadADIF(context.Background(), "<EOH><EOR><EOR>")
	if err != nil {
		t.Fatalf("UploadADIF: %v", err)
	}
	if result.Accepted != 2 {
		t.Errorf("expected Accepted=2, got %d", result.Accepted)
	}
	if result.Total != 2 {
		t.Errorf("expected Total=2, got %d", result.Total)
	}
}

func TestUploadADIF_ErrorResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`<html><body>Error: Invalid credentials. No records were added</body></html>`))
	}))
	defer srv.Close()

	c := newTestClientWithURLs(srv)
	_, err := c.UploadADIF(context.Background(), "<EOH>")
	if err == nil {
		t.Fatal("expected error for Error response, got nil")
	}
	if !strings.Contains(err.Error(), "eqsl rejected upload") {
		t.Errorf("expected 'eqsl rejected upload' in error, got: %v", err)
	}
}

func TestUploadADIF_ZeroRecords(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`<html><body>Thank you for submitting.</body></html>`))
	}))
	defer srv.Close()

	c := newTestClientWithURLs(srv)
	result, err := c.UploadADIF(context.Background(), "<EOH>")
	if err != nil {
		t.Fatalf("UploadADIF unexpected error: %v", err)
	}
	if result.Accepted != 0 {
		t.Errorf("expected Accepted=0, got %d", result.Accepted)
	}
}

func TestUploadADIF_ContextCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	c := newTestClientWithURLs(srv)
	_, err := c.UploadADIF(ctx, "<EOH>")
	if err == nil {
		t.Fatal("expected context cancellation error, got nil")
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// DownloadInbox tests
// ──────────────────────────────────────────────────────────────────────────────

const sampleInboxADIF = `ADIF Export from eQSL.cc
<ADIF_VER:5>3.0.4
<CREATED_TIMESTAMP:15>20240101 120000
<PROGRAMID:6>eQSL
<EOH>
<CALL:5>W1ABC<BAND:3>40m<MODE:3>SSB<QSO_DATE:8>20240115<TIME_ON:4>1430<RST_RCVD:2>59<EOR>
<CALL:5>K9XYZ<BAND:2>2m<MODE:2>FM<QSO_DATE:8>20240116<TIME_ON:4>2000<EOR>
`

func TestDownloadInbox_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		q := r.URL.Query()
		if q.Get("UserName") != "testuser" {
			t.Errorf("UserName: expected 'testuser', got %q", q.Get("UserName"))
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sampleInboxADIF))
	}))
	defer srv.Close()

	c := newTestClientWithURLs(srv)
	records, err := c.DownloadInbox(context.Background(), time.Time{})
	if err != nil {
		t.Fatalf("DownloadInbox: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}

	r0 := records[0]
	if r0.TheirCallsign != "W1ABC" {
		t.Errorf("record 0 callsign: expected 'W1ABC', got %q", r0.TheirCallsign)
	}
	if r0.Band != "40m" {
		t.Errorf("record 0 band: expected '40m', got %q", r0.Band)
	}
	if r0.Mode != "SSB" {
		t.Errorf("record 0 mode: expected 'SSB', got %q", r0.Mode)
	}
	if r0.DatetimeOn.IsZero() {
		t.Error("record 0: DatetimeOn should not be zero")
	}
	if r0.RstRcvd != "59" {
		t.Errorf("record 0 RST_RCVD: expected '59', got %q", r0.RstRcvd)
	}

	r1 := records[1]
	if r1.TheirCallsign != "K9XYZ" {
		t.Errorf("record 1 callsign: expected 'K9XYZ', got %q", r1.TheirCallsign)
	}
}

func TestDownloadInbox_AuthError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`<html><body>Invalid UserName/Password combination</body></html>`))
	}))
	defer srv.Close()

	c := newTestClientWithURLs(srv)
	_, err := c.DownloadInbox(context.Background(), time.Time{})
	if err == nil {
		t.Fatal("expected auth error, got nil")
	}
	if !strings.Contains(err.Error(), "authentication failed") {
		t.Errorf("expected 'authentication failed' in error, got: %v", err)
	}
}

func TestDownloadInbox_SinceDateParam(t *testing.T) {
	var capturedRcvdSince string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedRcvdSince = r.URL.Query().Get("RcvdSince")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<EOH>"))
	}))
	defer srv.Close()

	c := newTestClientWithURLs(srv)
	since, _ := time.Parse("2006-01-02", "2024-03-01")
	_, err := c.DownloadInbox(context.Background(), since)
	if err != nil {
		t.Fatalf("DownloadInbox with since date: %v", err)
	}

	// eQSL expects MM/DD/YYYY format.
	if capturedRcvdSince != "03/01/2024" {
		t.Errorf("RcvdSince: expected '03/01/2024', got %q", capturedRcvdSince)
	}
}

func TestDownloadInbox_EmptyInbox(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<ADIF_VER:5>3.0.4<EOH>"))
	}))
	defer srv.Close()

	c := newTestClientWithURLs(srv)
	records, err := c.DownloadInbox(context.Background(), time.Time{})
	if err != nil {
		t.Fatalf("empty inbox: %v", err)
	}
	if len(records) != 0 {
		t.Errorf("expected 0 records for empty inbox, got %d", len(records))
	}
}

func TestDownloadInbox_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := newTestClientWithURLs(srv)
	_, err := c.DownloadInbox(context.Background(), time.Time{})
	if err == nil {
		t.Fatal("expected error for HTTP 500, got nil")
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// parseUploadResponse unit tests
// ──────────────────────────────────────────────────────────────────────────────

func TestParseUploadResponse(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		wantAccept  int
		wantTotal   int
		wantErr     bool
		errContains string
	}{
		{
			name:       "standard success",
			body:       "<html>Result: 5 out of 5 records added</html>",
			wantAccept: 5, wantTotal: 5,
		},
		{
			name:       "partial success",
			body:       "<html>Result: 3 out of 5 records added</html>",
			wantAccept: 3, wantTotal: 5,
		},
		{
			name:       "single record",
			body:       "Result: 1 out of 1 records added",
			wantAccept: 1, wantTotal: 1,
		},
		{
			name:        "error line",
			body:        "<html>Error: ADIF format invalid</html>",
			wantErr:     true,
			errContains: "eqsl rejected upload",
		},
		{
			name:        "no records added text",
			body:        "No records were added due to duplicate check.",
			wantErr:     true,
			errContains: "eqsl rejected upload",
		},
		{
			name:       "unknown page layout",
			body:       "<html>Thank you for visiting eQSL!</html>",
			wantAccept: 0, wantTotal: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseUploadResponse(tt.body)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("error %q should contain %q", err.Error(), tt.errContains)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.Accepted != tt.wantAccept {
				t.Errorf("Accepted: expected %d, got %d", tt.wantAccept, result.Accepted)
			}
			if result.Total != tt.wantTotal {
				t.Errorf("Total: expected %d, got %d", tt.wantTotal, result.Total)
			}
		})
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// parseInboxADIF unit tests
// ──────────────────────────────────────────────────────────────────────────────

func TestParseInboxADIF(t *testing.T) {
	tests := []struct {
		name      string
		adif      string
		wantCount int
		wantFirst InboxRecord
	}{
		{
			name:      "empty string",
			adif:      "",
			wantCount: 0,
		},
		{
			name:      "only header",
			adif:      "<ADIF_VER:5>3.0.4<EOH>",
			wantCount: 0,
		},
		{
			name:      "single complete record",
			adif:      "<EOH><CALL:5>W1ABC<BAND:3>40m<MODE:3>SSB<QSO_DATE:8>20240115<TIME_ON:4>1430<EOR>",
			wantCount: 1,
			wantFirst: InboxRecord{TheirCallsign: "W1ABC", Band: "40m", Mode: "SSB"},
		},
		{
			name:      "record missing CALL — skipped",
			adif:      "<EOH><BAND:3>40m<MODE:3>SSB<QSO_DATE:8>20240115<EOR>",
			wantCount: 0,
		},
		{
			name:      "record missing BAND — skipped",
			adif:      "<EOH><CALL:5>W1ABC<MODE:3>SSB<QSO_DATE:8>20240115<EOR>",
			wantCount: 0,
		},
		{
			name:      "multiple records",
			adif:      sampleInboxADIF,
			wantCount: 2,
			wantFirst: InboxRecord{TheirCallsign: "W1ABC", Band: "40m", Mode: "SSB"},
		},
		{
			name:      "no EOH marker — records still parsed",
			adif:      "<CALL:5>W1ABC<BAND:3>40m<MODE:2>CW<QSO_DATE:8>20240101<TIME_ON:4>1200<EOR>",
			wantCount: 1,
			wantFirst: InboxRecord{TheirCallsign: "W1ABC", Band: "40m", Mode: "CW"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			records, err := parseInboxADIF(tt.adif)
			if err != nil {
				t.Fatalf("parseInboxADIF: %v", err)
			}
			if len(records) != tt.wantCount {
				t.Fatalf("expected %d records, got %d", tt.wantCount, len(records))
			}
			if tt.wantCount > 0 {
				r := records[0]
				if r.TheirCallsign != tt.wantFirst.TheirCallsign {
					t.Errorf("callsign: expected %q, got %q", tt.wantFirst.TheirCallsign, r.TheirCallsign)
				}
				if r.Band != tt.wantFirst.Band {
					t.Errorf("band: expected %q, got %q", tt.wantFirst.Band, r.Band)
				}
				if r.Mode != tt.wantFirst.Mode {
					t.Errorf("mode: expected %q, got %q", tt.wantFirst.Mode, r.Mode)
				}
			}
		})
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// DecodeCredentials / EncodeCredentials tests
// ──────────────────────────────────────────────────────────────────────────────

func TestEncodeDecodeCredentials(t *testing.T) {
	encoded, err := EncodeCredentials("W1AW", "supersecret")
	if err != nil {
		t.Fatalf("EncodeCredentials: %v", err)
	}
	if len(encoded) == 0 {
		t.Fatal("encoded credentials should not be empty")
	}

	decoded, err := DecodeCredentials(encoded)
	if err != nil {
		t.Fatalf("DecodeCredentials: %v", err)
	}
	if decoded.Username != "W1AW" {
		t.Errorf("Username: expected 'W1AW', got %q", decoded.Username)
	}
	if decoded.Password != "supersecret" {
		t.Errorf("Password mismatch")
	}
}

func TestDecodeCredentials_InvalidJSON(t *testing.T) {
	_, err := DecodeCredentials([]byte("not json at all"))
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestDecodeCredentials_EmptyFields(t *testing.T) {
	_, err := DecodeCredentials([]byte(`{"username":"","password":""}`))
	if err == nil {
		t.Fatal("expected error for empty username/password, got nil")
	}
}
