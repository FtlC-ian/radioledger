// Package sota provides an HTTP client for the SOTA (Summits on the Air)
// activation log upload API at api-db.sota.org.uk.
//
// # SOTA API Overview
//
// The SOTA database API (v2) accepts activator log uploads in a CSV format.
// Authentication uses a per-user API key obtained from the user's sotadata.org.uk
// profile page.
//
// # Upload Format (V2 CSV)
//
// Activator logs are submitted as a series of comma-separated lines in V2 format:
//
//	V2,{my_callsign},{my_sota_ref},{date},{time},{band},{mode},{their_callsign},{their_sota_ref},{notes}
//
// Where:
//   - date is YYYY/MM/DD in UTC
//   - time is HH:MM in UTC
//   - band uses MHz notation (e.g. "7MHz", "14MHz", "144MHz")
//   - their_sota_ref is optional (filled when the chaser is also activating a summit)
//
// # Authentication
//
// Credentials are stored per-user as JSON: {"api_key": "..."}.
// The API key is sent as a Bearer token in the Authorization header.
//
// # Endpoint
//
//	POST https://api-db.sota.org.uk/logs/activator/
//
// References:
//   - https://api-db.sota.org.uk/docs/
//   - docs/api-research/SOTA.md
//   - SCHEMA.md § sync_status, user_service_credentials
package sota

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	// uploadEndpoint is the URL for activator log uploads.
	uploadEndpoint = "https://api-db.sota.org.uk/logs/activator/"

	// requestTimeout is the per-request HTTP timeout.
	requestTimeout = 30 * time.Second

	// agentString identifies RadioLedger to the SOTA database.
	agentString = "RadioLedger/1.0"
)

// Credentials holds the per-user SOTA database API key.
// Credential type stored in user_service_credentials: "api_key"
type Credentials struct {
	APIKey string `json:"api_key"`
}

// DecodeCredentials parses the decrypted credential blob.
// Credentials are stored as JSON: {"api_key": "..."}
func DecodeCredentials(plaintext []byte) (*Credentials, error) {
	var creds Credentials
	if err := json.Unmarshal(plaintext, &creds); err != nil {
		return nil, fmt.Errorf("sota: decode credentials: %w", err)
	}
	if strings.TrimSpace(creds.APIKey) == "" {
		return nil, fmt.Errorf("sota: credentials missing api_key")
	}
	return &creds, nil
}

// EncodeCredentials serializes a SOTA API key to JSON for encrypted storage.
func EncodeCredentials(apiKey string) ([]byte, error) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, fmt.Errorf("sota: api_key must not be empty")
	}
	return json.Marshal(Credentials{APIKey: apiKey})
}

// Client is an HTTP client for the SOTA database activation log API.
type Client struct {
	apiKey     string
	httpClient *http.Client
}

// New creates a new SOTA API client with the given API key.
func New(apiKey string) *Client {
	return &Client{
		apiKey: strings.TrimSpace(apiKey),
		httpClient: &http.Client{
			Timeout: requestTimeout,
		},
	}
}

// UploadCSV uploads a SOTA V2 CSV log to the activator endpoint.
// csvData should be one or more lines in V2 CSV format.
// Returns a nil error on success (HTTP 200 or 204).
func (c *Client) UploadCSV(ctx context.Context, csvData string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, uploadEndpoint,
		strings.NewReader(csvData))
	if err != nil {
		return fmt.Errorf("sota upload: build request: %w", err)
	}

	req.Header.Set("Content-Type", "text/plain")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("User-Agent", agentString)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("sota upload: http POST: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 8192))
	if err != nil {
		return fmt.Errorf("sota upload: read body: %w", err)
	}

	switch resp.StatusCode {
	case http.StatusOK, http.StatusNoContent, http.StatusCreated:
		return nil
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("sota upload: authentication failed (HTTP %d): %s",
			resp.StatusCode, strings.TrimSpace(string(body)))
	case http.StatusBadRequest:
		return fmt.Errorf("sota upload: bad request (HTTP %d): %s",
			resp.StatusCode, strings.TrimSpace(string(body)))
	default:
		return fmt.Errorf("sota upload: unexpected status %d: %s",
			resp.StatusCode, strings.TrimSpace(string(body)))
	}
}

// BandToSOTAMHz converts an ADIF band name to the MHz notation expected by SOTA.
// Returns the band as-is (with "MHz" suffix attempt) if unrecognized, so the API
// can make its own determination.
func BandToSOTAMHz(band string) string {
	switch strings.ToLower(strings.TrimSpace(band)) {
	case "160m":
		return "1.8MHz"
	case "80m":
		return "3.5MHz"
	case "60m":
		return "5MHz"
	case "40m":
		return "7MHz"
	case "30m":
		return "10MHz"
	case "20m":
		return "14MHz"
	case "17m":
		return "18MHz"
	case "15m":
		return "21MHz"
	case "12m":
		return "24MHz"
	case "10m":
		return "28MHz"
	case "6m":
		return "50MHz"
	case "4m":
		return "70MHz"
	case "2m":
		return "144MHz"
	case "1.25m":
		return "222MHz"
	case "70cm":
		return "430MHz"
	case "33cm":
		return "900MHz"
	case "23cm":
		return "1240MHz"
	default:
		return band
	}
}

// FormatSOTADate formats a time.Time as SOTA's expected date string: YYYY/MM/DD.
func FormatSOTADate(t time.Time) string {
	return t.UTC().Format("2006/01/02")
}

// FormatSOTATime formats a time.Time as SOTA's expected time string: HH:MM (UTC).
func FormatSOTATime(t time.Time) string {
	return t.UTC().Format("15:04")
}
