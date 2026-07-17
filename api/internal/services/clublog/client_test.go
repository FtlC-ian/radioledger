// Package clublog — client_test.go: unit tests for the Club Log API client.
//
// All tests use an in-process httptest.Server to mock Club Log responses.
// No real network calls are made.
package clublog

import (
	"context"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// rewriteHostTransport rewrites every request URL host to the test server host.
type rewriteHostTransport struct {
	testServerHost string // e.g. "127.0.0.1:12345"
}

func (t rewriteHostTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	cloned := req.Clone(req.Context())
	cloned.URL.Scheme = "http"
	cloned.URL.Host = t.testServerHost
	return http.DefaultTransport.RoundTrip(cloned)
}

// newTestClient returns a Club Log client with HTTP calls redirected to srv.
func newTestClient(srv *httptest.Server) *Client {
	host := strings.TrimPrefix(srv.URL, "http://")
	creds := &Credentials{
		Email:    "test@example.com",
		Password: "test-password",
		Callsign: "W1TEST",
	}
	c := New("test-api-key", creds, nil)
	c.httpClient = &http.Client{
		Transport: rewriteHostTransport{testServerHost: host},
		Timeout:   5 * time.Second,
	}
	return c
}

// parseMultipartFields reads all fields from a multipart request body.
func parseMultipartFields(r *http.Request) map[string]string {
	ct := r.Header.Get("Content-Type")
	_, params, _ := mime.ParseMediaType(ct)
	mr := multipart.NewReader(r.Body, params["boundary"])
	fields := make(map[string]string)
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}
		b, _ := io.ReadAll(p)
		fields[p.FormName()] = string(b)
	}
	return fields
}

// ─────────────────────────────────────────────────────────────────────────────
// Credentials
// ─────────────────────────────────────────────────────────────────────────────

func TestDecodeCredentials_Valid(t *testing.T) {
	b, _ := EncodeCredentials("user@ham.radio", "secret", "W1AW")
	creds, err := DecodeCredentials(b)
	if err != nil {
		t.Fatalf("DecodeCredentials: %v", err)
	}
	if creds.Email != "user@ham.radio" {
		t.Errorf("Email: want 'user@ham.radio', got %q", creds.Email)
	}
	if creds.Password != "secret" {
		t.Errorf("Password: want 'secret', got %q", creds.Password)
	}
	if creds.Callsign != "W1AW" {
		t.Errorf("Callsign: want 'W1AW', got %q", creds.Callsign)
	}
}

func TestDecodeCredentials_MissingPassword(t *testing.T) {
	b, _ := json.Marshal(Credentials{Email: "e@e.com", Callsign: "W1AW"})
	_, err := DecodeCredentials(b)
	if err == nil {
		t.Fatal("expected error for missing password, got nil")
	}
}

func TestDecodeCredentials_MissingEmail(t *testing.T) {
	b, _ := json.Marshal(Credentials{Password: "secret", Callsign: "W1AW"})
	_, err := DecodeCredentials(b)
	if err == nil {
		t.Fatal("expected error for missing email, got nil")
	}
}

func TestDecodeCredentials_InvalidJSON(t *testing.T) {
	_, err := DecodeCredentials([]byte("not-json"))
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// UploadADIF
// ─────────────────────────────────────────────────────────────────────────────

func TestUploadADIF_Success(t *testing.T) {
	var capturedFields map[string]string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		capturedFields = parseMultipartFields(r)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"count": 3}`))
	}))
	defer srv.Close()

	c := newTestClient(srv)
	result, err := c.UploadADIF(context.Background(), "<EOH><EOR><EOR><EOR>")
	if err != nil {
		t.Fatalf("UploadADIF: %v", err)
	}
	if result.Count != 3 {
		t.Errorf("Count: want 3, got %d", result.Count)
	}
	if capturedFields["api"] != "test-api-key" {
		t.Errorf("api field: want 'test-api-key', got %q", capturedFields["api"])
	}
	if capturedFields["email"] != "test@example.com" {
		t.Errorf("email field: want 'test@example.com', got %q", capturedFields["email"])
	}
	if capturedFields["password"] != "test-password" {
		t.Errorf("password field: want 'test-password', got %q", capturedFields["password"])
	}
	if capturedFields["callsign"] != "W1TEST" {
		t.Errorf("callsign field: want 'W1TEST', got %q", capturedFields["callsign"])
	}
}

func TestUploadADIF_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"error": "Invalid API key"}`))
	}))
	defer srv.Close()

	c := newTestClient(srv)
	_, err := c.UploadADIF(context.Background(), "<EOH><EOR>")
	if err == nil {
		t.Fatal("expected error for API error response, got nil")
	}
	if !strings.Contains(err.Error(), "Invalid API key") {
		t.Errorf("error should mention API key issue, got: %v", err)
	}
}

func TestUploadADIF_HTTP400(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("bad request"))
	}))
	defer srv.Close()

	c := newTestClient(srv)
	_, err := c.UploadADIF(context.Background(), "<EOH><EOR>")
	if err == nil {
		t.Fatal("expected error for HTTP 400, got nil")
	}
	if !strings.Contains(err.Error(), "HTTP 400") {
		t.Errorf("error should mention HTTP 400, got: %v", err)
	}
}

func TestUploadADIF_PlainTextResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("1 record added"))
	}))
	defer srv.Close()

	c := newTestClient(srv)
	result, err := c.UploadADIF(context.Background(), "<EOH><EOR>")
	if err != nil {
		t.Fatalf("UploadADIF plain text: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result for plain text response")
	}
}

func TestUploadADIF_ADIFInBody(t *testing.T) {
	var capturedADIF string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fields := parseMultipartFields(r)
		capturedADIF = fields["adif"]
		_, _ = w.Write([]byte(`{"count": 1}`))
	}))
	defer srv.Close()

	adif := "<CALL:5>W1AW<BAND:3>20m<MODE:2>CW<EOR>"
	c := newTestClient(srv)
	_, err := c.UploadADIF(context.Background(), adif)
	if err != nil {
		t.Fatalf("UploadADIF: %v", err)
	}
	if capturedADIF != adif {
		t.Errorf("adif field: want %q, got %q", adif, capturedADIF)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// DeleteQSO
// ─────────────────────────────────────────────────────────────────────────────

func TestDeleteQSO_Success(t *testing.T) {
	var capturedADIF string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fields := parseMultipartFields(r)
		capturedADIF = fields["adif"]
		_, _ = w.Write([]byte("1 record deleted"))
	}))
	defer srv.Close()

	c := newTestClient(srv)
	dt := time.Date(2024, 3, 15, 14, 30, 0, 0, time.UTC)
	err := c.DeleteQSO(context.Background(), "DX0DX", "20m", "CW", dt)
	if err != nil {
		t.Fatalf("DeleteQSO: %v", err)
	}
	for _, want := range []string{"DX0DX", "20m", "CW", "20240315", "1430"} {
		if !strings.Contains(capturedADIF, want) {
			t.Errorf("delete ADIF missing %q: got %s", want, capturedADIF)
		}
	}
	if !strings.Contains(capturedADIF, "<EOR>") {
		t.Errorf("delete ADIF missing <EOR>: got %s", capturedADIF)
	}
}

func TestDeleteQSO_HTTP500(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("server error"))
	}))
	defer srv.Close()

	c := newTestClient(srv)
	err := c.DeleteQSO(context.Background(), "DX0DX", "20m", "CW", time.Now().UTC())
	if err == nil {
		t.Fatal("expected error for HTTP 500, got nil")
	}
}

func TestDeleteQSO_CredentialsIncluded(t *testing.T) {
	var capturedFields map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedFields = parseMultipartFields(r)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	c := newTestClient(srv)
	err := c.DeleteQSO(context.Background(), "W1AW", "40m", "SSB", time.Now().UTC())
	if err != nil {
		t.Fatalf("DeleteQSO: %v", err)
	}
	if capturedFields["api"] != "test-api-key" {
		t.Errorf("api field missing or wrong: %q", capturedFields["api"])
	}
	if capturedFields["callsign"] != "W1TEST" {
		t.Errorf("callsign field missing or wrong: %q", capturedFields["callsign"])
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// GetDXCCStatus
// ─────────────────────────────────────────────────────────────────────────────

func TestGetDXCCStatus_Success(t *testing.T) {
	responseJSON := `{
		"191": {"mixed": true, "confirmed": true, "cw": true},
		"3":   {"mixed": true, "confirmed": false},
		"1":   {"mixed": false}
	}`

	var capturedParams map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedParams = map[string]string{
			"api":      r.URL.Query().Get("api"),
			"email":    r.URL.Query().Get("email"),
			"callsign": r.URL.Query().Get("callsign"),
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(responseJSON))
	}))
	defer srv.Close()

	c := newTestClient(srv)
	status, err := c.GetDXCCStatus(context.Background())
	if err != nil {
		t.Fatalf("GetDXCCStatus: %v", err)
	}

	if capturedParams["api"] != "test-api-key" {
		t.Errorf("api param: want 'test-api-key', got %q", capturedParams["api"])
	}
	if capturedParams["callsign"] != "W1TEST" {
		t.Errorf("callsign param: want 'W1TEST', got %q", capturedParams["callsign"])
	}

	e191, ok := status[191]
	if !ok {
		t.Fatal("entity 191 missing from result")
	}
	if !e191.Worked {
		t.Error("entity 191 should be Worked=true")
	}
	if !e191.Confirmed {
		t.Error("entity 191 should be Confirmed=true")
	}
	if e191.EntityID != 191 {
		t.Errorf("entity 191 EntityID: want 191, got %d", e191.EntityID)
	}

	e3, ok := status[3]
	if !ok {
		t.Fatal("entity 3 missing from result")
	}
	if !e3.Worked {
		t.Error("entity 3 should be Worked=true")
	}
	if e3.Confirmed {
		t.Error("entity 3 should be Confirmed=false")
	}
}

func TestGetDXCCStatus_HTTP403(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("invalid api key"))
	}))
	defer srv.Close()

	c := newTestClient(srv)
	_, err := c.GetDXCCStatus(context.Background())
	if err == nil {
		t.Fatal("expected error for HTTP 403, got nil")
	}
	if !strings.Contains(err.Error(), "HTTP 403") {
		t.Errorf("error should mention HTTP 403, got: %v", err)
	}
}

func TestGetDXCCStatus_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not-json"))
	}))
	defer srv.Close()

	c := newTestClient(srv)
	_, err := c.GetDXCCStatus(context.Background())
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestGetDXCCStatus_EmptyResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := newTestClient(srv)
	status, err := c.GetDXCCStatus(context.Background())
	if err != nil {
		t.Fatalf("GetDXCCStatus empty: %v", err)
	}
	if len(status) != 0 {
		t.Errorf("expected 0 entities, got %d", len(status))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// parseDXCCStatusResponse unit tests
// ─────────────────────────────────────────────────────────────────────────────

func TestParseDXCCStatusResponse_BandStatus(t *testing.T) {
	body := []byte(`{"191": {"cw": true, "ssb": false, "mixed": true, "confirmed": false}}`)

	status, err := parseDXCCStatusResponse(body)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	e, ok := status[191]
	if !ok {
		t.Fatal("entity 191 missing")
	}
	if !e.Worked {
		t.Error("entity 191 should be Worked=true (cw=true or mixed=true)")
	}
	cwBand, ok := e.Bands["cw"]
	if !ok {
		t.Fatal("band 'cw' missing from entity 191")
	}
	if !cwBand.Worked {
		t.Error("cw band should be Worked=true")
	}
}

func TestParseDXCCStatusResponse_SkipsNonNumericKeys(t *testing.T) {
	body := []byte(`{
		"191":     {"mixed": true},
		"meta":    {"info": "should be skipped"},
		"version": {"num": 2}
	}`)

	status, err := parseDXCCStatusResponse(body)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, ok := status[191]; !ok {
		t.Error("entity 191 should be present")
	}
	if len(status) != 1 {
		t.Errorf("expected 1 entity (non-numeric skipped), got %d", len(status))
	}
}

func TestParseDXCCStatusResponse_MultipleEntities(t *testing.T) {
	body := []byte(`{
		"1":   {"mixed": true, "confirmed": true},
		"3":   {"mixed": true, "confirmed": false},
		"100": {"mixed": false, "confirmed": false}
	}`)

	status, err := parseDXCCStatusResponse(body)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(status) != 3 {
		t.Errorf("expected 3 entities, got %d", len(status))
	}
	if !status[1].Confirmed {
		t.Error("entity 1 should be confirmed")
	}
	if status[3].Confirmed {
		t.Error("entity 3 should not be confirmed")
	}
}

func TestParseDXCCStatusResponse_WorkedSetFromBands(t *testing.T) {
	// Entity has no top-level "worked" key — should derive Worked from band values.
	body := []byte(`{"50": {"20m": true, "40m": false}}`)

	status, err := parseDXCCStatusResponse(body)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	e, ok := status[50]
	if !ok {
		t.Fatal("entity 50 missing")
	}
	if !e.Worked {
		t.Error("entity 50 should be Worked=true (20m worked)")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// asBool unit tests
// ─────────────────────────────────────────────────────────────────────────────

func TestAsBool(t *testing.T) {
	cases := []struct {
		input interface{}
		want  bool
	}{
		{true, true},
		{false, false},
		{float64(1), true},
		{float64(0), false},
		{"1", true},
		{"true", true},
		{"True", true},
		{"false", false},
		{"0", false},
		{nil, false},
	}
	for _, tc := range cases {
		got := asBool(tc.input)
		if got != tc.want {
			t.Errorf("asBool(%v) = %v, want %v", tc.input, got, tc.want)
		}
	}
}
