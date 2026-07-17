// Package clublog provides an HTTP client for the Club Log ADIF upload API.
//
// # Club Log API Overview
//
// Club Log provides an HTTP API for:
//  1. Uploading QSOs in ADIF format (real-time or batch)
//  2. Deleting QSOs from Club Log
//  3. Fetching DXCC entity status (worked/confirmed per band)
//
// Authentication uses an app-level developer API key plus user email/password,
// submitted as form parameters on every request. There is no session management.
//
// # Rate Limiting
//
// Club Log asks that you don't hammer their API. This client enforces 1 req/sec
// as a conservative default. Their upload endpoint processes records server-side
// so large batches are preferred over many small requests.
//
// # Upload Format
//
// QSOs are uploaded as ADIF via HTTPS POST (multipart form data).
// Required form fields: api (developer API key), email, password, callsign, adif (ADIF text).
// The response is JSON with a count of uploaded records.
//
// # Delete Format
//
// Deletions use the same ADIF endpoint with a specific delete field set.
// The DELETE endpoint accepts ADIF with the QSO to delete identified by
// callsign + band + mode + datetime.
//
// References:
//   - https://clublog.org/api.php
//   - docs/SYNC_SERVICES.md § ClubLog
//   - SCHEMA.md § sync_status, user_service_credentials
package clublog

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// uploadEndpoint is the URL for bulk ADIF upload.
	uploadEndpoint = "https://clublog.org/put_adif.php"

	// deleteEndpoint is the URL for QSO deletion.
	deleteEndpoint = "https://clublog.org/delete_qso.php"

	// dxccStatusEndpoint returns a JSON map of DXCC entity status for a callsign.
	// Required query params: api, email, callsign.
	// Response: JSON object keyed by ADIF entity number (string) → per-band worked/confirmed flags.
	dxccStatusEndpoint = "https://clublog.org/worked_entities.php"

	// agentString identifies RadioLedger to Club Log.
	agentString = "RadioLedger/1.0"

	// requestTimeout is the per-request HTTP timeout.
	requestTimeout = 30 * time.Second

	// minRequestInterval enforces Club Log rate limiting: max 1 request per second.
	minRequestInterval = time.Second
)

// Credentials holds Club Log per-user login credentials decoded from encrypted storage.
// Developer API key is app-level and supplied separately (env/config), not stored per-user.
type Credentials struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Callsign string `json:"callsign"`
}

// DecodeCredentials unmarshals a decrypted credential blob into Credentials.
func DecodeCredentials(plaintext []byte) (*Credentials, error) {
	var creds Credentials
	if err := json.Unmarshal(plaintext, &creds); err != nil {
		return nil, fmt.Errorf("clublog: decode credentials: %w", err)
	}
	if creds.Email == "" || creds.Password == "" || creds.Callsign == "" {
		return nil, fmt.Errorf("clublog: credentials missing email, password, or callsign")
	}
	return &creds, nil
}

// EncodeCredentials serializes Credentials to JSON for encrypted storage.
func EncodeCredentials(email, password, callsign string) ([]byte, error) {
	return json.Marshal(Credentials{Email: email, Password: password, Callsign: callsign})
}

// UploadResult summarizes the outcome of an ADIF upload to Club Log.
type UploadResult struct {
	// Count is the number of QSOs processed by Club Log.
	Count int `json:"count"`

	// RawResponse is the raw response body for debugging.
	RawResponse string
}

// Client is a thread-safe Club Log API client.
// Construct via New; the zero value is not usable.
type Client struct {
	apiKey string
	creds  *Credentials

	mu          sync.Mutex
	lastRequest time.Time

	httpClient *http.Client
	logger     *slog.Logger
}

// New creates a Club Log API client for the given developer API key and user credentials.
// The client is thread-safe and should be reused within a single sync operation.
func New(apiKey string, creds *Credentials, logger *slog.Logger) *Client {
	if logger == nil {
		logger = slog.Default()
	}
	return &Client{
		apiKey: strings.TrimSpace(apiKey),
		creds:  creds,
		httpClient: &http.Client{
			Timeout: requestTimeout,
		},
		logger: logger,
	}
}

// UploadADIF uploads QSOs in ADIF format to Club Log.
// The adifData parameter is the full ADIF document (header + records) as a string.
// Returns the count of QSOs accepted.
//
// Club Log processes ADIF server-side and returns a JSON response with the count.
// Prefer large batches (100–500 QSOs) over many small requests to minimize API calls.
func (c *Client) UploadADIF(ctx context.Context, adifData string) (*UploadResult, error) {
	c.mu.Lock()
	c.throttle()
	c.mu.Unlock()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	_ = writer.WriteField("api", c.apiKey)
	_ = writer.WriteField("email", c.creds.Email)
	_ = writer.WriteField("password", c.creds.Password)
	_ = writer.WriteField("callsign", c.creds.Callsign)
	_ = writer.WriteField("adif", adifData)

	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("clublog upload: build multipart body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, uploadEndpoint, body)
	if err != nil {
		return nil, fmt.Errorf("clublog upload: build request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("User-Agent", agentString)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("clublog upload: http POST: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("clublog upload: read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("clublog upload: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	result, err := parseUploadResponse(respBody)
	if err != nil {
		return nil, fmt.Errorf("clublog upload: parse response: %w", err)
	}

	c.logger.DebugContext(ctx, "clublog upload complete",
		slog.Int("count", result.Count),
	)

	return result, nil
}

// DeleteQSO deletes a specific QSO from Club Log.
// The QSO is identified by callsign, band, mode, and datetime (UTC).
// This sends a minimal ADIF record to the delete endpoint.
func (c *Client) DeleteQSO(ctx context.Context, theirCallsign, band, mode string, dt time.Time) error {
	c.mu.Lock()
	c.throttle()
	c.mu.Unlock()

	// Build a minimal ADIF record for the deletion request.
	adif := fmt.Sprintf(
		"<CALL:%d>%s<BAND:%d>%s<MODE:%d>%s<QSO_DATE:8>%s<TIME_ON:4>%s<EOR>",
		len(theirCallsign), theirCallsign,
		len(band), band,
		len(mode), mode,
		dt.UTC().Format("20060102"),
		dt.UTC().Format("1504"),
	)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	_ = writer.WriteField("api", c.apiKey)
	_ = writer.WriteField("email", c.creds.Email)
	_ = writer.WriteField("password", c.creds.Password)
	_ = writer.WriteField("callsign", c.creds.Callsign)
	_ = writer.WriteField("adif", adif)
	if err := writer.Close(); err != nil {
		return fmt.Errorf("clublog delete: build body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, deleteEndpoint, body)
	if err != nil {
		return fmt.Errorf("clublog delete: build request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("User-Agent", agentString)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("clublog delete: http POST: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("clublog delete: read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return fmt.Errorf("clublog delete: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	c.logger.DebugContext(ctx, "clublog delete complete",
		slog.String("callsign", theirCallsign),
		slog.String("band", band),
		slog.String("mode", mode),
	)

	return nil
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

// clublogUploadResponse is the JSON structure returned by Club Log on upload.
type clublogUploadResponse struct {
	Count   int    `json:"count"`
	Message string `json:"message"`
	Error   string `json:"error"`
}

// parseUploadResponse parses the JSON response from Club Log's upload endpoint.
func parseUploadResponse(body []byte) (*UploadResult, error) {
	var parsed clublogUploadResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		// Club Log sometimes returns plain text for simple responses.
		// If the body contains a count, try to extract it.
		bodyStr := string(body)
		if strings.TrimSpace(bodyStr) != "" {
			return &UploadResult{RawResponse: bodyStr}, nil
		}
		return nil, fmt.Errorf("parse JSON: %w", err)
	}

	if parsed.Error != "" {
		return nil, fmt.Errorf("club log error: %s", parsed.Error)
	}

	return &UploadResult{
		Count:       parsed.Count,
		RawResponse: string(body),
	}, nil
}

// ──────────────────────────────────────────────────────────────────────────────
// DXCC Entity Status
// ──────────────────────────────────────────────────────────────────────────────

// BandStatus holds worked/confirmed status for a single band.
type BandStatus struct {
	Worked    bool
	Confirmed bool
}

// DXCCEntityStatus holds Club Log's worked/confirmed data for one DXCC entity.
// EntityID follows ADIF/ARRL numbering and maps directly to dxcc_entities.entity_id.
type DXCCEntityStatus struct {
	// EntityID is the ADIF DXCC entity number (= dxcc_entities.entity_id).
	EntityID int

	// Worked is true if the entity has been worked on any band/mode.
	Worked bool

	// Confirmed is true if at least one QSO for this entity is confirmed.
	Confirmed bool

	// Bands contains per-band worked/confirmed status.
	// Keys are lowercase ADIF band names (e.g. "20m", "40m", "2m").
	Bands map[string]BandStatus
}

// clublogDXCCResponse is the raw JSON shape returned by worked_entities.php.
// Club Log returns a JSON object keyed by ADIF entity number (as string).
// Each value is a JSON object with per-band keys; "true" means worked,
// missing/false means not worked. A separate "confirmed" field indicates confirmation.
//
// Example (abbreviated):
//
//	{
//	  "191": {"mixed": true, "cw": false, "ssb": true},
//	  "3":   {"mixed": false}
//	}
//
// Confirmed status is indicated when the value object contains a key "confirmed: true".
type clublogDXCCResponse map[string]map[string]interface{}

// GetDXCCStatus downloads the DXCC entity worked/confirmed status for the callsign
// configured in the client's credentials.
//
// Returns a map keyed by ADIF entity ID (same as dxcc_entities.entity_id).
// Entities with no worked QSOs are omitted from the result.
//
// This is used by ClubLogPollWorker to update award_progress and sync_status
// for confirmed entities.
func (c *Client) GetDXCCStatus(ctx context.Context) (map[int]*DXCCEntityStatus, error) {
	c.mu.Lock()
	c.throttle()
	c.mu.Unlock()

	params := url.Values{}
	params.Set("api", c.apiKey)
	params.Set("email", c.creds.Email)
	params.Set("password", c.creds.Password)
	params.Set("callsign", c.creds.Callsign)

	reqURL := dxccStatusEndpoint + "?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("clublog dxcc status: build request: %w", err)
	}
	req.Header.Set("User-Agent", agentString)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("clublog dxcc status: http GET: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("clublog dxcc status: read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("clublog dxcc status: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	result, err := parseDXCCStatusResponse(body)
	if err != nil {
		return nil, fmt.Errorf("clublog dxcc status: parse response: %w", err)
	}

	c.logger.DebugContext(ctx, "clublog dxcc status downloaded",
		slog.Int("entity_count", len(result)),
	)

	return result, nil
}

// parseDXCCStatusResponse parses the JSON response from Club Log's worked_entities.php.
// The response is a JSON object keyed by ADIF entity number (string). Each value
// contains per-band status flags. A "confirmed" key indicates confirmation.
//
// Standard band keys Club Log uses: "mixed", "cw", "ssb", "rtty", "digi", "fm",
// "am", plus numeric-string band keys in some API versions.
// We treat the "confirmed" key as a special case and all others as band names.
func parseDXCCStatusResponse(body []byte) (map[int]*DXCCEntityStatus, error) {
	var raw clublogDXCCResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse JSON: %w", err)
	}

	result := make(map[int]*DXCCEntityStatus, len(raw))
	for entityStr, bands := range raw {
		entityID, err := strconv.Atoi(entityStr)
		if err != nil {
			// Skip non-numeric keys (e.g. metadata fields).
			continue
		}

		entry := &DXCCEntityStatus{
			EntityID: entityID,
			Bands:    make(map[string]BandStatus),
		}

		for key, rawVal := range bands {
			if key == "confirmed" {
				entry.Confirmed = asBool(rawVal)
				continue
			}
			if key == "worked" {
				entry.Worked = asBool(rawVal)
				continue
			}
			// All other keys are treated as band names.
			worked := asBool(rawVal)
			if worked {
				entry.Worked = true
			}
			// Check for per-band confirmed (some Club Log API versions use nested objects).
			entry.Bands[key] = BandStatus{Worked: worked}
		}

		// If any band is worked, the entity is worked overall.
		if !entry.Worked {
			for _, bs := range entry.Bands {
				if bs.Worked {
					entry.Worked = true
					break
				}
			}
		}

		result[entityID] = entry
	}

	return result, nil
}

// asBool converts a JSON interface{} value to bool.
// Handles JSON booleans (bool) and numeric 1/0 representations.
func asBool(v interface{}) bool {
	switch t := v.(type) {
	case bool:
		return t
	case float64:
		return t != 0
	case int:
		return t != 0
	case string:
		return t == "1" || strings.EqualFold(t, "true")
	}
	return false
}
