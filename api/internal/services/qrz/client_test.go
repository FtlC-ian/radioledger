package qrz

// Unit tests for the QRZ XML API client.
//
// All tests use an httptest.Server to mock the QRZ XML API.
// No real network calls are made.

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// buildMockServer creates an httptest.Server that routes requests based on query params.
// Handlers must be provided as a map of callsign → XML response body.
// If callsign is not in the map, qrzXMLNotFoundFixture is returned.
func buildMockServer(t *testing.T, loginXML string, lookupXMLByCall map[string]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		w.Header().Set("Content-Type", "text/xml; charset=utf-8")
		w.WriteHeader(http.StatusOK)

		if q.Get("username") != "" {
			// Login request
			_, _ = fmt.Fprint(w, loginXML)
			return
		}
		// Lookup request
		callsign := strings.ToUpper(q.Get("callsign"))
		if body, ok := lookupXMLByCall[callsign]; ok {
			_, _ = fmt.Fprint(w, body)
			return
		}
		_, _ = fmt.Fprintf(w, `<?xml version="1.0" ?><QRZDatabase><Session><Error>Not found: %s</Error></Session></QRZDatabase>`, callsign)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// patchEndpoint replaces the package-level endpoint const with the mock server URL.
// Returns a cleanup function. This is a test-only approach to inject the mock URL.
func clientWithEndpoint(username, password, endpoint string) *Client {
	c := New(username, password)
	// We intercept by pointing the httpClient's transport to a custom roundtripper
	// that rewrites the host to our test server.
	c.httpClient.Transport = &endpointRewriter{
		base:        http.DefaultTransport,
		mockBaseURL: endpoint,
	}
	return c
}

// endpointRewriter is a test-only http.RoundTripper that rewrites the request URL
// to point at the mock server instead of qrzXMLEndpoint.
type endpointRewriter struct {
	base        http.RoundTripper
	mockBaseURL string
}

func (e *endpointRewriter) RoundTrip(r *http.Request) (*http.Response, error) {
	// Rewrite the host and scheme to the mock server.
	r2 := r.Clone(r.Context())
	// Parse the mock URL
	mock := e.mockBaseURL
	// Strip trailing slash from mock URL
	mock = strings.TrimSuffix(mock, "/")
	// Rewrite URL: keep path and query, replace scheme+host
	r2.URL.Scheme = "http"
	r2.URL.Host = strings.TrimPrefix(strings.TrimPrefix(mock, "https://"), "http://")
	return e.base.RoundTrip(r2)
}

// ─────────────────────────────────────────────────────────────────────────────

const testLoginXML = `<?xml version="1.0" ?>
<QRZDatabase version="1.34">
<Session>
  <Key>test-session-abc123</Key>
  <SubExp>Mon Dec 31 2029</SubExp>
</Session>
</QRZDatabase>`

const testCallsignXML = `<?xml version="1.0" ?>
<QRZDatabase version="1.34">
<Callsign>
  <call>W5ABC</call>
  <fname>John</fname>
  <lname>Smith</lname>
  <addr1>123 Main St</addr1>
  <state>TX</state>
  <zip>75001</zip>
  <country>United States</country>
  <lat>32.946</lat>
  <lon>-97.034</lon>
  <grid>EM12</grid>
  <dxcc>291</dxcc>
  <land>United States</land>
  <cq_zone>4</cq_zone>
  <itu_zone>7</itu_zone>
  <email>w5abc@example.com</email>
  <class>E</class>
  <expdate>2029-12-31</expdate>
</Callsign>
<Session>
  <Key>test-session-abc123</Key>
</Session>
</QRZDatabase>`

const testSessionTimeoutXML = `<?xml version="1.0" ?>
<QRZDatabase version="1.34">
<Session>
  <Error>Session Timeout</Error>
</Session>
</QRZDatabase>`

func TestQRZClient_Lookup_Success(t *testing.T) {
	srv := buildMockServer(t, testLoginXML, map[string]string{
		"W5ABC": testCallsignXML,
	})
	client := clientWithEndpoint("testuser", "testpass", srv.URL)

	info, err := client.LookupCallsign(context.Background(), "W5ABC")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.Callsign != "W5ABC" {
		t.Errorf("expected callsign W5ABC, got %s", info.Callsign)
	}
	if info.FName != "John" {
		t.Errorf("expected fname John, got %s", info.FName)
	}
	if info.LName != "Smith" {
		t.Errorf("expected lname Smith, got %s", info.LName)
	}
	if info.FullName != "John Smith" {
		t.Errorf("expected full_name 'John Smith', got %q", info.FullName)
	}
	if info.Grid != "EM12" {
		t.Errorf("expected grid EM12, got %s", info.Grid)
	}
	if info.Class != "E" {
		t.Errorf("expected class E, got %s", info.Class)
	}
	if info.Country != "United States" {
		t.Errorf("expected country 'United States', got %s", info.Country)
	}
	if info.DXCC != 291 {
		t.Errorf("expected dxcc 291, got %d", info.DXCC)
	}
}

func TestQRZClient_Lookup_NotFound(t *testing.T) {
	srv := buildMockServer(t, testLoginXML, map[string]string{})
	client := clientWithEndpoint("testuser", "testpass", srv.URL)

	_, err := client.LookupCallsign(context.Background(), "ZZ9ZZZ")
	if err == nil {
		t.Fatal("expected error for unknown callsign, got nil")
	}
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestQRZClient_Lookup_NormalizeCallsignUppercase(t *testing.T) {
	srv := buildMockServer(t, testLoginXML, map[string]string{
		"W5ABC": testCallsignXML,
	})
	client := clientWithEndpoint("testuser", "testpass", srv.URL)

	// Lowercase callsign should be normalized.
	info, err := client.LookupCallsign(context.Background(), "w5abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.Callsign != "W5ABC" {
		t.Errorf("expected callsign W5ABC, got %s", info.Callsign)
	}
}

func TestQRZClient_Lookup_SessionCached(t *testing.T) {
	loginCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		w.Header().Set("Content-Type", "text/xml")
		if q.Get("username") != "" {
			loginCount++
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprint(w, testLoginXML)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, testCallsignXML)
	}))
	t.Cleanup(srv.Close)
	client := clientWithEndpoint("u", "p", srv.URL)

	// Two lookups should only trigger one login.
	if _, err := client.LookupCallsign(context.Background(), "W5ABC"); err != nil {
		t.Fatalf("first lookup: %v", err)
	}
	if _, err := client.LookupCallsign(context.Background(), "W5ABC"); err != nil {
		t.Fatalf("second lookup: %v", err)
	}
	if loginCount != 1 {
		t.Errorf("expected 1 login, got %d", loginCount)
	}
}

func TestQRZClient_Lookup_SessionExpiry_ReAuth(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		w.Header().Set("Content-Type", "text/xml")
		callCount++

		if q.Get("username") != "" {
			// Login
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprint(w, testLoginXML)
			return
		}

		// First lookup: session timeout; second: success.
		if callCount == 2 {
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprint(w, testSessionTimeoutXML)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, testCallsignXML)
	}))
	t.Cleanup(srv.Close)
	client := clientWithEndpoint("u", "p", srv.URL)

	info, err := client.LookupCallsign(context.Background(), "W5ABC")
	if err != nil {
		t.Fatalf("expected successful re-auth retry, got error: %v", err)
	}
	if info.Callsign != "W5ABC" {
		t.Errorf("expected W5ABC, got %s", info.Callsign)
	}
	if callCount < 3 {
		t.Errorf("expected at least 3 calls (login, timeout, re-login, lookup), got %d", callCount)
	}
}

func TestQRZClient_Lookup_LoginError(t *testing.T) {
	srv := buildMockServer(t, `<?xml version="1.0" ?>
<QRZDatabase><Session><Error>Username/Password incorrect</Error></Session></QRZDatabase>`,
		map[string]string{})
	client := clientWithEndpoint("baduser", "badpass", srv.URL)

	_, err := client.LookupCallsign(context.Background(), "W5ABC")
	if err == nil {
		t.Fatal("expected login error, got nil")
	}
	if !strings.Contains(err.Error(), "Username/Password incorrect") {
		t.Errorf("expected username/password error message, got %v", err)
	}
}

func TestQRZClient_RateLimiting_ThrottlesRequests(t *testing.T) {
	// Verify that the client enforces ~1 second between requests.
	// We make 2 requests and check that elapsed time >= 1 second.
	srv := buildMockServer(t, testLoginXML, map[string]string{
		"W5ABC": testCallsignXML,
	})
	client := clientWithEndpoint("u", "p", srv.URL)

	start := time.Now()

	if _, err := client.LookupCallsign(context.Background(), "W5ABC"); err != nil {
		t.Fatalf("first lookup: %v", err)
	}
	if _, err := client.LookupCallsign(context.Background(), "W5ABC"); err != nil {
		t.Fatalf("second lookup: %v", err)
	}

	elapsed := time.Since(start)
	// Two lookups + one login = at least 2 throttle cycles = 2 seconds.
	// Be generous with the lower bound to avoid flakiness.
	if elapsed < 900*time.Millisecond {
		t.Errorf("expected at least 900ms for 2 throttled requests, got %v", elapsed)
	}
}
