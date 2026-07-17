// Package hamqth provides an HTTP client for the HamQTH XML API.
//
// # HamQTH API Overview
//
// HamQTH uses a session-key-based XML API:
//  1. Login: GET xml.php?u=USER&p=PASS → XML containing a <session_id>.
//  2. Upload: POST xml.php with id=SESSION_ID&adif=ADIF_DATA as form fields.
//  3. Session keys are valid for ~60 minutes of inactivity — this client caches
//     the session key and re-authenticates transparently on expiry.
//
// # Upload Format
//
// QSOs are uploaded as ADIF text in the "adif" form field. The XML response
// contains a <qso_upload> element with a <result> of OK or ERROR.
//
// # Rate Limiting
//
// HamQTH's homepage states "no limits", but best practice is to cache session
// keys and avoid hammering the endpoint. This client enforces a minimum of
// 500ms between API requests.
//
// References:
//   - https://www.hamqth.com/developers.php
//   - docs/api-research/HamQTH.md
//   - docs/SYNC_SERVICES.md § HamQTH
package hamqth

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	// xmlEndpoint is the HamQTH XML API base URL.
	xmlEndpoint = "https://www.hamqth.com/xml.php"

	// agentString identifies RadioLedger to HamQTH.
	agentString = "RadioLedger/1.0"

	// requestTimeout is the per-request HTTP timeout.
	requestTimeout = 30 * time.Second

	// minRequestInterval enforces a conservative rate limit.
	minRequestInterval = 500 * time.Millisecond

	// sessionTTL is how long to consider a cached session key valid before
	// proactively re-authenticating. HamQTH sessions last ~60 min of inactivity.
	sessionTTL = 45 * time.Minute
)

// Credentials holds HamQTH login credentials decoded from the encrypted store.
// These are decrypted in memory for API calls only; never logged or stored in plaintext.
type Credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// DecodeCredentials unmarshals a decrypted credential blob into Credentials.
func DecodeCredentials(plaintext []byte) (*Credentials, error) {
	var creds Credentials
	if err := json.Unmarshal(plaintext, &creds); err != nil {
		return nil, fmt.Errorf("hamqth: decode credentials: %w", err)
	}
	if creds.Username == "" || creds.Password == "" {
		return nil, fmt.Errorf("hamqth: credentials missing username or password")
	}
	return &creds, nil
}

// EncodeCredentials serializes Credentials to JSON for encrypted storage.
func EncodeCredentials(username, password string) ([]byte, error) {
	return json.Marshal(Credentials{Username: username, Password: password})
}

// UploadResult summarizes the outcome of an ADIF upload to HamQTH.
type UploadResult struct {
	// Count is the number of QSOs accepted by HamQTH.
	Count int

	// RawResponse is the full response body for debugging.
	RawResponse string
}

// hamqthXML is the root element of all HamQTH XML API responses.
type hamqthXML struct {
	XMLName   xml.Name       `xml:"HamQTH"`
	Session   *sessionResult `xml:"session"`
	QSOUpload *qsoUpload     `xml:"qso_upload"`
}

type sessionResult struct {
	SessionID string `xml:"session_id"`
	Error     string `xml:"error"`
}

type qsoUpload struct {
	Result string `xml:"result"`
	Count  int    `xml:"count"`
	Error  string `xml:"error"`
}

// Client is a thread-safe HamQTH XML API client with session caching.
// Construct via New; the zero value is not usable.
type Client struct {
	creds *Credentials

	mu          sync.Mutex
	sessionID   string
	sessionAt   time.Time
	lastRequest time.Time

	httpClient *http.Client
	logger     *slog.Logger
}

// New creates a HamQTH client for the given credentials.
// The client is thread-safe and should be reused within a single sync operation.
// Never log or persist the credentials after this call.
func New(creds *Credentials, logger *slog.Logger) *Client {
	if logger == nil {
		logger = slog.Default()
	}
	return &Client{
		creds: creds,
		httpClient: &http.Client{
			Timeout: requestTimeout,
		},
		logger: logger,
	}
}

// UploadADIF uploads QSOs in ADIF format to HamQTH.
// The adifData parameter is ADIF record(s) with no header required.
// Automatically handles session login and re-authentication on session expiry.
func (c *Client) UploadADIF(ctx context.Context, adifData string) (*UploadResult, error) {
	c.mu.Lock()
	sessionID, err := c.ensureSession(ctx)
	c.mu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("hamqth: login: %w", err)
	}

	c.mu.Lock()
	c.throttle()
	c.mu.Unlock()

	params := url.Values{}
	params.Set("id", sessionID)
	params.Set("adif", adifData)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, xmlEndpoint,
		strings.NewReader(params.Encode()))
	if err != nil {
		return nil, fmt.Errorf("hamqth upload: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", agentString)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("hamqth upload: http POST: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 256*1024)) // 256KB limit
	if err != nil {
		return nil, fmt.Errorf("hamqth upload: read response: %w", err)
	}

	result, sessionExpired, parseErr := parseUploadResponse(body)
	if parseErr != nil {
		return nil, fmt.Errorf("hamqth upload: parse response: %w", parseErr)
	}

	// If session expired, invalidate cached session and retry once.
	if sessionExpired {
		c.mu.Lock()
		c.sessionID = ""
		newSessionID, loginErr := c.ensureSession(ctx)
		c.mu.Unlock()
		if loginErr != nil {
			return nil, fmt.Errorf("hamqth upload: re-login after session expiry: %w", loginErr)
		}

		c.mu.Lock()
		c.throttle()
		c.mu.Unlock()

		params.Set("id", newSessionID)
		retryReq, err := http.NewRequestWithContext(ctx, http.MethodPost, xmlEndpoint,
			strings.NewReader(params.Encode()))
		if err != nil {
			return nil, fmt.Errorf("hamqth upload retry: build request: %w", err)
		}
		retryReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		retryReq.Header.Set("User-Agent", agentString)

		retryResp, err := c.httpClient.Do(retryReq)
		if err != nil {
			return nil, fmt.Errorf("hamqth upload retry: http POST: %w", err)
		}
		defer func() { _ = retryResp.Body.Close() }()

		retryBody, err := io.ReadAll(io.LimitReader(retryResp.Body, 256*1024)) // 256KB limit
		if err != nil {
			return nil, fmt.Errorf("hamqth upload retry: read response: %w", err)
		}
		result, _, parseErr = parseUploadResponse(retryBody)
		if parseErr != nil {
			return nil, fmt.Errorf("hamqth upload retry: parse response: %w", parseErr)
		}
	}

	c.logger.DebugContext(ctx, "hamqth upload complete",
		slog.Int("count", result.Count),
	)

	return result, nil
}

// ensureSession returns a valid session ID, logging in if needed.
// Must be called with c.mu held.
func (c *Client) ensureSession(ctx context.Context) (string, error) {
	if c.sessionID != "" && time.Since(c.sessionAt) < sessionTTL {
		return c.sessionID, nil
	}
	return c.login(ctx)
}

// login authenticates with HamQTH and caches the session ID.
// Must be called with c.mu held.
func (c *Client) login(ctx context.Context) (string, error) {
	c.throttle()

	loginURL := fmt.Sprintf("%s?u=%s&p=%s",
		xmlEndpoint,
		url.QueryEscape(c.creds.Username),
		url.QueryEscape(c.creds.Password),
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, loginURL, nil)
	if err != nil {
		return "", fmt.Errorf("build login request: %w", err)
	}
	req.Header.Set("User-Agent", agentString)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("http GET login: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 16*1024)) // 16KB limit
	if err != nil {
		return "", fmt.Errorf("read login response: %w", err)
	}

	var parsed hamqthXML
	if xmlErr := xml.Unmarshal(body, &parsed); xmlErr != nil {
		return "", fmt.Errorf("parse login XML: %w (body: %.200s)", xmlErr, string(body))
	}

	if parsed.Session == nil {
		return "", fmt.Errorf("hamqth login: unexpected response (body: %.200s)", string(body))
	}

	if parsed.Session.Error != "" {
		return "", fmt.Errorf("hamqth login failed: %s", parsed.Session.Error)
	}

	if parsed.Session.SessionID == "" {
		return "", fmt.Errorf("hamqth login: empty session_id in response")
	}

	c.sessionID = parsed.Session.SessionID
	c.sessionAt = time.Now()

	return c.sessionID, nil
}

// throttle sleeps until minRequestInterval has elapsed since the last request.
// Must be called with c.mu held.
func (c *Client) throttle() {
	now := time.Now()
	if elapsed := now.Sub(c.lastRequest); elapsed < minRequestInterval {
		time.Sleep(minRequestInterval - elapsed)
	}
	c.lastRequest = time.Now()
}

// parseUploadResponse parses a HamQTH XML upload response.
// Returns the result, whether the session expired (needs re-login), and any error.
func parseUploadResponse(body []byte) (*UploadResult, bool, error) {
	result := &UploadResult{RawResponse: string(body)}

	var parsed hamqthXML
	if err := xml.Unmarshal(body, &parsed); err != nil {
		// If we can't parse XML, treat it as a transient failure.
		return nil, false, fmt.Errorf("parse XML: %w (body: %.200s)", err, string(body))
	}

	// Session error (could be expiry or auth failure).
	if parsed.Session != nil && parsed.Session.Error != "" {
		errMsg := parsed.Session.Error
		if isSessionExpiredError(errMsg) {
			return result, true, nil // signal re-login needed
		}
		return nil, false, fmt.Errorf("hamqth session error: %s", errMsg)
	}

	// Upload result.
	if parsed.QSOUpload != nil {
		if strings.EqualFold(strings.TrimSpace(parsed.QSOUpload.Result), "OK") ||
			strings.EqualFold(strings.TrimSpace(parsed.QSOUpload.Result), "ok") {
			result.Count = parsed.QSOUpload.Count
			return result, false, nil
		}
		if parsed.QSOUpload.Error != "" {
			return nil, false, fmt.Errorf("hamqth upload error: %s", parsed.QSOUpload.Error)
		}
	}

	// Unknown/empty response — treat as success with 0 count.
	return result, false, nil
}

// isSessionExpiredError returns true if the error message indicates session expiry.
func isSessionExpiredError(msg string) bool {
	s := strings.ToLower(strings.TrimSpace(msg))
	return strings.Contains(s, "session does not exist") ||
		strings.Contains(s, "expired") ||
		strings.Contains(s, "please login")
}

// IsAuthOrPermanentError returns true if the error indicates bad credentials
// or a permanent condition that won't resolve by retrying.
func IsAuthOrPermanentError(errMsg string) bool {
	s := strings.ToLower(strings.TrimSpace(errMsg))
	return strings.Contains(s, "wrong username") ||
		strings.Contains(s, "wrong password") ||
		strings.Contains(s, "invalid user") ||
		strings.Contains(s, "authentication failed") ||
		strings.Contains(s, "incorrect") ||
		strings.Contains(s, "suspended") ||
		strings.Contains(s, "banned") ||
		strings.Contains(s, "credentials missing")
}

// PasswordWarning is the UI-facing security notice displayed when a user enters
// their HamQTH password. HamQTH does not support OAuth or API keys — RadioLedger
// must store the actual password (AES-256-GCM encrypted).
const PasswordWarning = "HamQTH requires storing your password encrypted. Use a password unique to HamQTH."
