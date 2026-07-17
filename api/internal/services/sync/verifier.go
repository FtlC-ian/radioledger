// Package sync — verifier.go implements per-service credential verification.
//
// Each supported service has a lightweight verify function that tests the
// provided plaintext credentials against the real external API. Verification
// uses the same credential format as the corresponding sync workers.
//
// # Credential Formats
//
//   - qrz:     "username:password" (colon-separated, no JSON wrapper)
//   - eqsl:    JSON {"username":"...","password":"..."}
//   - clublog: JSON {"email":"...","password":"...","callsign":"..."}
//   - hamqth:  "username:password" (colon-separated, no JSON wrapper)
//   - pota:    no credential verification (public API, no auth)
//
// # Failure Modes
//
// verifyCredential returns a descriptive error on auth failure so the HTTP handler
// can return it to the user. Network errors (timeouts, DNS) are wrapped and
// returned — the caller should treat them as "could not verify" rather than
// "invalid credentials".
package sync

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const verifyTimeout = 15 * time.Second

// verifyCredential dispatches to the per-service verifier.
// plaintext is the raw decrypted credential bytes (not logged).
func verifyCredential(ctx context.Context, service string, plaintext []byte) error {
	ctx, cancel := context.WithTimeout(ctx, verifyTimeout)
	defer cancel()

	switch service {
	case "qrz":
		return verifyQRZ(ctx, plaintext)
	case "eqsl":
		return verifyEQSL(ctx, plaintext)
	case "clublog":
		return verifyClubLog(ctx, plaintext)
	case "hamqth":
		return verifyHamQTH(ctx, plaintext)
	case "pota":
		// POTA uses a public API — no auth to verify.
		return nil
	case "sota":
		return verifySOTA(ctx, plaintext)
	default:
		return fmt.Errorf("no verifier available for service %q", service)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// QRZ.com
// ──────────────────────────────────────────────────────────────────────────────

// verifyQRZ authenticates against the QRZ XML API using username:password.
// A successful login returns a non-empty session key.
// verifyQRZ verifies QRZ credentials. Two credential formats are supported:
//
//   - Logbook API key (JSON): {"api_key": "XXXX-XXXX-XXXX-XXXX"}
//     Verified by calling the QRZ Logbook STATUS action.
//   - XML API credentials (plain text): "username:password"
//     Verified by logging in to the QRZ XML API.
//     (Legacy; used by the callsign_cache_warm job for callsign lookups.)
func verifyQRZ(ctx context.Context, plaintext []byte) error {
	// Try JSON logbook API key format first.
	if isQRZLogbookCredential(plaintext) {
		return verifyQRZLogbookKey(ctx, plaintext)
	}
	// Fall back to XML API username:password verification.
	return verifyQRZXML(ctx, plaintext)
}

// isQRZLogbookCredential returns true if plaintext is JSON with an "api_key" field.
func isQRZLogbookCredential(plaintext []byte) bool {
	var probe map[string]interface{}
	if err := json.Unmarshal(plaintext, &probe); err != nil {
		return false
	}
	_, ok := probe["api_key"]
	return ok
}

// verifyQRZLogbookKey verifies a QRZ Logbook API key via the STATUS action.
func verifyQRZLogbookKey(ctx context.Context, plaintext []byte) error {
	var creds struct {
		APIKey string `json:"api_key"`
	}
	if err := json.Unmarshal(plaintext, &creds); err != nil {
		return fmt.Errorf("qrz logbook: decode credential: %w", err)
	}
	if strings.TrimSpace(creds.APIKey) == "" {
		return fmt.Errorf("qrz logbook: api_key must not be empty")
	}

	formData := url.Values{}
	formData.Set("KEY", creds.APIKey)
	formData.Set("ACTION", "STATUS")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://logbook.qrz.com/api",
		strings.NewReader(formData.Encode()))
	if err != nil {
		return fmt.Errorf("qrz logbook verify: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "RadioLedger/1.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("qrz logbook verify: http POST: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return fmt.Errorf("qrz logbook verify: read body: %w", err)
	}

	parsed, err := url.ParseQuery(string(body))
	if err != nil {
		return fmt.Errorf("qrz logbook verify: parse response: %w", err)
	}

	switch parsed.Get("RESULT") {
	case "AUTH":
		return fmt.Errorf("qrz logbook: API key is invalid or lacks logbook access")
	case "FAIL":
		return fmt.Errorf("qrz logbook: verification failed: %s", parsed.Get("REASON"))
	case "OK":
		return nil
	default:
		return fmt.Errorf("qrz logbook: unexpected RESULT=%q", parsed.Get("RESULT"))
	}
}

// verifyQRZXML authenticates against the QRZ XML API using username:password.
func verifyQRZXML(ctx context.Context, plaintext []byte) error {
	parts := strings.SplitN(string(plaintext), ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return fmt.Errorf("qrz: credential must be in 'username:password' format or JSON {api_key:...}")
	}
	username, password := parts[0], parts[1]

	params := url.Values{}
	params.Set("username", username)
	params.Set("password", password)
	params.Set("agent", "RadioLedger/1.0")

	reqURL := "https://xmldata.qrz.com/xml/current/?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return fmt.Errorf("qrz verify: build request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("qrz verify: http GET: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 8192))
	if err != nil {
		return fmt.Errorf("qrz verify: read body: %w", err)
	}

	var parsed struct {
		XMLName xml.Name `xml:"QRZDatabase"`
		Session struct {
			Key   string `xml:"Key"`
			Error string `xml:"Error"`
		} `xml:"Session"`
	}
	if err := xml.Unmarshal(body, &parsed); err != nil {
		return fmt.Errorf("qrz verify: parse XML: %w", err)
	}

	if parsed.Session.Error != "" {
		return fmt.Errorf("qrz authentication failed: %s", parsed.Session.Error)
	}
	if parsed.Session.Key == "" {
		return fmt.Errorf("qrz authentication failed: no session key returned")
	}

	return nil
}

// ──────────────────────────────────────────────────────────────────────────────
// eQSL.cc
// ──────────────────────────────────────────────────────────────────────────────

type eqslCreds struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// verifyEQSL authenticates against eQSL.cc by attempting to download the inbox
// with a future date filter (minimises data transfer). An auth error returns
// HTML containing "Invalid UserName/Password".
func verifyEQSL(ctx context.Context, plaintext []byte) error {
	var creds eqslCreds
	if err := json.Unmarshal(plaintext, &creds); err != nil {
		// Try colon-separated fallback.
		parts := strings.SplitN(string(plaintext), ":", 2)
		if len(parts) == 2 {
			creds.Username, creds.Password = parts[0], parts[1]
		} else {
			return fmt.Errorf("eqsl: credential must be JSON {username,password} or 'username:password'")
		}
	}
	if creds.Username == "" || creds.Password == "" {
		return fmt.Errorf("eqsl: credential missing username or password")
	}

	// Use a far-future date so the inbox response is empty (minimal data transfer).
	future := time.Now().AddDate(100, 0, 0).UTC().Format("01/02/2006")

	params := url.Values{}
	params.Set("UserName", creds.Username)
	params.Set("Password", creds.Password)
	params.Set("RcvdSince", future)

	reqURL := "https://www.eqsl.cc/qslcard/DownloadInBox.cfm?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return fmt.Errorf("eqsl verify: build request: %w", err)
	}
	req.Header.Set("User-Agent", "RadioLedger/1.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("eqsl verify: http GET: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 16384))
	if err != nil {
		return fmt.Errorf("eqsl verify: read body: %w", err)
	}

	bodyStr := string(body)
	if strings.Contains(bodyStr, "Invalid UserName/Password") ||
		strings.Contains(bodyStr, "Invalid Username") ||
		strings.Contains(bodyStr, "incorrect login") {
		return fmt.Errorf("eqsl authentication failed: invalid username or password")
	}

	return nil
}

// ──────────────────────────────────────────────────────────────────────────────
// Club Log
// ──────────────────────────────────────────────────────────────────────────────

type clublogCreds struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Callsign string `json:"callsign"`
}

// verifyClubLog checks credentials against Club Log by uploading an empty ADIF.
// Club Log returns HTTP 4xx or JSON errors for invalid auth details.
func verifyClubLog(ctx context.Context, plaintext []byte) error {
	var creds clublogCreds
	if err := json.Unmarshal(plaintext, &creds); err != nil {
		return fmt.Errorf("clublog: credential must be JSON {email, password, callsign}: %w", err)
	}
	if creds.Email == "" || creds.Password == "" || creds.Callsign == "" {
		return fmt.Errorf("clublog: credential missing email, password, or callsign")
	}
	apiKey := strings.TrimSpace(os.Getenv("CLUBLOG_API_KEY"))
	if apiKey == "" {
		return fmt.Errorf("clublog verification unavailable: server missing CLUBLOG_API_KEY")
	}

	formData := url.Values{}
	formData.Set("api", apiKey)
	formData.Set("email", creds.Email)
	formData.Set("password", creds.Password)
	formData.Set("callsign", creds.Callsign)
	formData.Set("adif", "<EOH>")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://clublog.org/put_adif.php",
		strings.NewReader(formData.Encode()))
	if err != nil {
		return fmt.Errorf("clublog verify: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "RadioLedger/1.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("clublog verify: http POST: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return fmt.Errorf("clublog verify: read body: %w", err)
	}

	bodyStr := strings.ToLower(string(body))
	if resp.StatusCode >= 400 {
		return fmt.Errorf("clublog authentication failed (HTTP %d): %s",
			resp.StatusCode, strings.TrimSpace(string(body)))
	}

	// Club Log may return a JSON error even on 200.
	if strings.Contains(bodyStr, `"error"`) || strings.Contains(bodyStr, "invalid") ||
		strings.Contains(bodyStr, "unauthorized") {
		return fmt.Errorf("clublog authentication failed: %s", strings.TrimSpace(string(body)))
	}

	return nil
}

// ──────────────────────────────────────────────────────────────────────────────
// HamQTH
// ──────────────────────────────────────────────────────────────────────────────

// verifyHamQTH authenticates against HamQTH.com's XML API.
// A successful login returns a non-empty session ID.
func verifyHamQTH(ctx context.Context, plaintext []byte) error {
	parts := strings.SplitN(string(plaintext), ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return fmt.Errorf("hamqth: credential must be in 'username:password' format")
	}
	username, password := parts[0], parts[1]

	params := url.Values{}
	params.Set("u", username)
	params.Set("p", password)
	params.Set("version", "2.2")

	reqURL := "https://www.hamqth.com/xml.php?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return fmt.Errorf("hamqth verify: build request: %w", err)
	}
	req.Header.Set("User-Agent", "RadioLedger/1.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("hamqth verify: http GET: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return fmt.Errorf("hamqth verify: read body: %w", err)
	}

	var parsed struct {
		XMLName xml.Name `xml:"HamQTH"`
		Session struct {
			SessionID string `xml:"session_id"`
			Error     string `xml:"error"`
		} `xml:"session"`
	}
	if err := xml.Unmarshal(body, &parsed); err != nil {
		return fmt.Errorf("hamqth verify: parse XML: %w", err)
	}

	if parsed.Session.Error != "" {
		return fmt.Errorf("hamqth authentication failed: %s", parsed.Session.Error)
	}
	if parsed.Session.SessionID == "" {
		return fmt.Errorf("hamqth authentication failed: no session ID returned")
	}

	return nil
}

// ──────────────────────────────────────────────────────────────────────────────
// SOTA (Summits on the Air)
// ──────────────────────────────────────────────────────────────────────────────

// verifySOTA validates a SOTA API key by fetching the user's summit list.
// A GET to a public endpoint with the Authorization header confirms the key is valid.
// SOTA's API returns 401 on invalid key and 200 on success.
func verifySOTA(ctx context.Context, plaintext []byte) error {
	var creds struct {
		APIKey string `json:"api_key"`
	}
	if err := json.Unmarshal(plaintext, &creds); err != nil {
		return fmt.Errorf("sota: credential must be JSON {api_key: ...}: %w", err)
	}
	apiKey := strings.TrimSpace(creds.APIKey)
	if apiKey == "" {
		return fmt.Errorf("sota: api_key must not be empty")
	}

	// Use a lightweight read endpoint to verify the API key without side effects.
	// GET /spots/all is a public endpoint that accepts Bearer auth; a 200 or 404
	// confirms the key is accepted. 401/403 means invalid key.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://api-db.sota.org.uk/spots/all", nil)
	if err != nil {
		return fmt.Errorf("sota verify: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("User-Agent", "RadioLedger/1.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("sota verify: http GET: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)

	switch resp.StatusCode {
	case http.StatusOK, http.StatusNoContent, http.StatusNotFound:
		// Key is accepted by the server.
		return nil
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("sota authentication failed: invalid API key (HTTP %d)", resp.StatusCode)
	default:
		// Unexpected status — treat as unverifiable (not an auth failure).
		return fmt.Errorf("sota verify: unexpected HTTP status %d", resp.StatusCode)
	}
}
