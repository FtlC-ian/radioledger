// Package qrz — logbook.go provides a client for the QRZ Logbook HTTP API.
//
// # QRZ Logbook API Overview
//
// The QRZ Logbook API is separate from the QRZ XML callsign lookup API.
// Authentication uses an opaque API access key (obtained from the QRZ website
// under the user's logbook settings page).
//
// The API uses HTTP POST with URL-encoded name=value parameters. Responses are
// also URL-encoded key=value pairs. QSO data is conveyed in ADIF format.
//
// # Supported Actions
//
//   - INSERT: Upload a single QSO into the user's logbook (subscription required).
//     Returns a LOGID (integer string) stored as remote_id in sync_status for
//     later confirmation polling.
//   - DELETE: Remove one or more QSOs by LOGID.
//   - STATUS: Returns logbook health and statistics. Used to verify the API key
//     and, with the LOGIDS parameter, to poll per-QSO confirmation status.
//
// # Rate Limiting
//
// The RadioLedger sync infrastructure uses a global 2 RPS rate limiter for "qrz"
// (see internal/services/sync/infra.go). This client does not enforce its own
// throttle — rely on the caller's infra rate limiter.
//
// # Credential Format
//
// Logbook credentials are stored in user_service_credentials as JSON:
//
//	{"api_key": "XXXX-XXXX-XXXX-XXXX"}
//
// References:
//   - https://www.qrz.com/docs/logbook/QRZLogbookAPI.html
//   - docs/SYNC_SERVICES.md § QRZ Logbook
//   - SCHEMA.md § sync_status, user_service_credentials
package qrz

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// logbookEndpoint is the base URL for the QRZ Logbook API.
	logbookEndpoint = "https://logbook.qrz.com/api"

	// logbookRequestTimeout is the per-request HTTP timeout.
	logbookRequestTimeout = 30 * time.Second

	// logbookMaxResponseBytes caps QRZ response size we will parse.
	// FETCH responses can be large because ADIF payload is inlined in the URL-encoded body.
	logbookMaxResponseBytes = 600 * 1024 * 1024 // 600 MB

	// logbookAgentString identifies RadioLedger to QRZ Logbook.
	logbookAgentString = "RadioLedger/1.0"
)

// LogbookCredentials holds the opaque API access key for the QRZ Logbook.
// Credential type stored in user_service_credentials: "api_key"
type LogbookCredentials struct {
	APIKey string `json:"api_key"`
}

// DecodeLogbookCredentials parses the decrypted credential blob.
// Credentials are stored as JSON: {"api_key": "..."}
func DecodeLogbookCredentials(plaintext []byte) (*LogbookCredentials, error) {
	var creds LogbookCredentials
	if err := json.Unmarshal(plaintext, &creds); err != nil {
		return nil, fmt.Errorf("qrz logbook: decode credentials: %w", err)
	}
	if strings.TrimSpace(creds.APIKey) == "" {
		return nil, fmt.Errorf("qrz logbook: credentials missing api_key")
	}
	return &creds, nil
}

// EncodeLogbookCredentials serializes an API key to JSON for encrypted storage.
func EncodeLogbookCredentials(apiKey string) ([]byte, error) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, fmt.Errorf("qrz logbook: api_key must not be empty")
	}
	return json.Marshal(LogbookCredentials{APIKey: apiKey})
}

// IsLogbookCredential returns true if the plaintext bytes appear to be a logbook
// API key credential (JSON with "api_key"), rather than an XML "username:password".
// Used by the callsign_cache_warm job to skip XML lookups when only the logbook key is stored.
func IsLogbookCredential(plaintext []byte) bool {
	var probe map[string]interface{}
	if err := json.Unmarshal(plaintext, &probe); err != nil {
		return false
	}
	_, hasAPIKey := probe["api_key"]
	return hasAPIKey
}

// InsertResult is the parsed response from an INSERT action.
type InsertResult struct {
	// LogID is the QRZ-assigned integer identifier for the uploaded QSO.
	// Store this as remote_id in sync_status for later confirmation polling.
	LogID string

	// Result is the raw RESULT field ("OK", "REPLACE", "FAIL").
	Result string

	// Count is the number of records inserted (always 0 or 1 for INSERT).
	Count int

	// Reason is the failure reason when Result == "FAIL".
	Reason string
}

// StatusResult is the parsed response from a STATUS action.
type StatusResult struct {
	// Result is "OK" or "FAIL".
	Result string

	// Reason is set when Result == "FAIL".
	Reason string

	// Confirmed is the total number of confirmed QSOs in the logbook (aggregate).
	Confirmed int

	// Total is the total number of QSOs in the logbook (aggregate).
	Total int

	// PerLogID maps logid → confirmed (true/false).
	// Populated when LOGIDS were included in the STATUS request.
	PerLogID map[string]bool

	// RawData is the raw DATA field value from the response, for debugging.
	RawData string
}

// FetchResult is the parsed response from a FETCH action.
type FetchResult struct {
	Result string
	Reason string
	Count  int
	ADIF   string
}

// DeleteResult summarizes the outcome of a DELETE action.
type DeleteResult struct {
	Result string
	Count  int
	Reason string
}

// LogbookClient is a QRZ Logbook API client.
// It is safe for sequential use; rate limiting is the caller's responsibility
// (use the sync infra rate limiter, not an internal throttle here).
type LogbookClient struct {
	apiKey     string
	httpClient *http.Client
}

// NewLogbookClient constructs a LogbookClient for the given API key.
func NewLogbookClient(apiKey string) *LogbookClient {
	return &LogbookClient{
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: logbookRequestTimeout,
		},
	}
}

// WithHTTPClient replaces the default HTTP client. Used in tests to inject a mock server.
func (c *LogbookClient) WithHTTPClient(hc *http.Client) *LogbookClient {
	c.httpClient = hc
	return c
}

// withEndpoint returns a client that rewrites API calls to the given base URL.
// For testing only.
func (c *LogbookClient) withEndpointRewriter(mockURL string) *LogbookClient {
	c.httpClient = &http.Client{
		Timeout: logbookRequestTimeout,
		Transport: &logbookEndpointRewriter{
			base:    http.DefaultTransport,
			mockURL: mockURL,
		},
	}
	return c
}

// logbookEndpointRewriter rewrites the target URL for tests.
type logbookEndpointRewriter struct {
	base    http.RoundTripper
	mockURL string
}

func (e *logbookEndpointRewriter) RoundTrip(r *http.Request) (*http.Response, error) {
	r2 := r.Clone(r.Context())
	mock := strings.TrimSuffix(e.mockURL, "/")
	r2.URL.Scheme = "http"
	r2.URL.Host = strings.TrimPrefix(strings.TrimPrefix(mock, "https://"), "http://")
	r2.URL.Path = ""
	return e.base.RoundTrip(r2)
}

// VerifyKey calls STATUS to verify that the API key is valid.
// Returns nil if the key is accepted, or an error describing the failure.
func (c *LogbookClient) VerifyKey(ctx context.Context) error {
	params := url.Values{}
	params.Set("KEY", c.apiKey)
	params.Set("ACTION", "STATUS")

	resp, err := c.post(ctx, params)
	if err != nil {
		return fmt.Errorf("qrz logbook verify: %w", err)
	}

	switch resp.Get("RESULT") {
	case "AUTH":
		return fmt.Errorf("qrz logbook: API key is invalid or lacks logbook access")
	case "FAIL":
		return fmt.Errorf("qrz logbook: STATUS failed: %s", resp.Get("REASON"))
	}
	return nil
}

// InsertQSO uploads a single QSO in ADIF format.
// Returns the assigned LogID on success. The caller should store this as
// remote_id in sync_status for later confirmation polling.
//
// The adif parameter should be a single ADIF record without header, e.g.:
//
//	<call:4>W5AB<band:3>40m<mode:3>SSB<qso_date:8>20240101<time_on:4>1234<eor>
func (c *LogbookClient) InsertQSO(ctx context.Context, adif string) (*InsertResult, error) {
	return c.upsertQSO(ctx, "INSERT", "", adif)
}

// ReplaceQSO updates an existing QRZ logbook record by LOGID using ADIF data.
func (c *LogbookClient) ReplaceQSO(ctx context.Context, logID, adif string) (*InsertResult, error) {
	return c.upsertQSO(ctx, "REPLACE", logID, adif)
}

func (c *LogbookClient) upsertQSO(ctx context.Context, action, logID, adif string) (*InsertResult, error) {
	params := url.Values{}
	params.Set("KEY", c.apiKey)
	params.Set("ACTION", action)
	params.Set("ADIF", adif)
	if action == "REPLACE" {
		trimmed := strings.TrimSpace(logID)
		if trimmed == "" {
			return nil, fmt.Errorf("qrz logbook replace: missing LOGID")
		}
		params.Set("LOGID", trimmed)
	}

	raw, err := c.post(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("qrz logbook %s: %w", strings.ToLower(action), err)
	}

	result := &InsertResult{
		Result: raw.Get("RESULT"),
		LogID:  raw.Get("LOGID"),
		Reason: raw.Get("REASON"),
	}
	if count := raw.Get("COUNT"); count != "" {
		if _, err := fmt.Sscan(count, &result.Count); err != nil {
			return nil, fmt.Errorf("qrz logbook %s: parse COUNT %q: %w", strings.ToLower(action), count, err)
		}
	}

	switch result.Result {
	case "OK", "REPLACE":
		if result.LogID == "" {
			return nil, fmt.Errorf("qrz logbook %s: no LOGID in response (RESULT=%s)", strings.ToLower(action), result.Result)
		}
		return result, nil
	case "AUTH":
		return nil, fmt.Errorf("qrz logbook: API key is invalid or lacks INSERT privileges (subscription required)")
	case "FAIL":
		return nil, fmt.Errorf("qrz logbook %s failed: %s", strings.ToLower(action), result.Reason)
	default:
		return nil, fmt.Errorf("qrz logbook %s: unexpected RESULT=%q", strings.ToLower(action), result.Result)
	}
}

// CheckStatus returns aggregate logbook statistics (total QSOs, confirmed count).
// Used by the poll worker to check overall confirmation progress and logbook health.
func (c *LogbookClient) CheckStatus(ctx context.Context) (*StatusResult, error) {
	params := url.Values{}
	params.Set("KEY", c.apiKey)
	params.Set("ACTION", "STATUS")

	raw, err := c.post(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("qrz logbook status: %w", err)
	}

	sr := &StatusResult{
		Result:   raw.Get("RESULT"),
		Reason:   raw.Get("REASON"),
		RawData:  raw.Get("DATA"),
		PerLogID: make(map[string]bool),
	}

	switch sr.Result {
	case "FAIL":
		return nil, fmt.Errorf("qrz logbook status failed: %s", sr.Reason)
	case "AUTH":
		return nil, fmt.Errorf("qrz logbook: API key is invalid")
	}

	parseStatusData(sr.RawData, sr)
	return sr, nil
}

// CheckLogIDStatus polls per-QSO confirmation status for specific QRZ LOGIDs.
// Returns a StatusResult with PerLogID populated where data is available.
// QRZ may return confirmation status as logid=Y/N pairs in the DATA field.
// Missing logids should be treated as not-yet-confirmed.
func (c *LogbookClient) CheckLogIDStatus(ctx context.Context, logids []string) (*StatusResult, error) {
	if len(logids) == 0 {
		return &StatusResult{Result: "OK", PerLogID: map[string]bool{}}, nil
	}

	params := url.Values{}
	params.Set("KEY", c.apiKey)
	params.Set("ACTION", "STATUS")
	params.Set("LOGIDS", strings.Join(logids, ","))

	raw, err := c.post(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("qrz logbook status(logids): %w", err)
	}

	sr := &StatusResult{
		Result:   raw.Get("RESULT"),
		Reason:   raw.Get("REASON"),
		RawData:  raw.Get("DATA"),
		PerLogID: make(map[string]bool),
	}

	switch sr.Result {
	case "FAIL":
		return nil, fmt.Errorf("qrz logbook status(logids) failed: %s", sr.Reason)
	case "AUTH":
		return nil, fmt.Errorf("qrz logbook: API key is invalid")
	}

	parseStatusData(sr.RawData, sr)
	return sr, nil
}

// FetchConfirmedQSOs fetches confirmed/modified QSOs as ADIF.
// since controls MODSINCE date filtering; max controls page size (default 250).
func (c *LogbookClient) FetchConfirmedQSOs(ctx context.Context, since time.Time, max int) (*FetchResult, error) {
	if max <= 0 {
		max = 250
	}

	optionParts := []string{"STATUS:CONFIRMED"}
	if !since.IsZero() {
		optionParts = append(optionParts, "MODSINCE:"+since.UTC().Format("2006-01-02"))
	}
	optionParts = append(optionParts, fmt.Sprintf("MAX:%d", max))

	params := url.Values{}
	params.Set("KEY", c.apiKey)
	params.Set("ACTION", "FETCH")
	params.Set("OPTION", strings.Join(optionParts, ","))

	return c.fetch(ctx, params)
}

// FetchAllQSOs downloads the full QRZ logbook as ADIF.
// This is used for one-time migration imports.
func (c *LogbookClient) FetchAllQSOs(ctx context.Context) (*FetchResult, error) {
	params := url.Values{}
	params.Set("KEY", c.apiKey)
	params.Set("ACTION", "FETCH")
	return c.fetch(ctx, params)
}

func (c *LogbookClient) fetch(ctx context.Context, params url.Values) (*FetchResult, error) {
	raw, err := c.post(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("qrz logbook fetch: %w", err)
	}

	res := &FetchResult{
		Result: raw.Get("RESULT"),
		Reason: raw.Get("REASON"),
		ADIF:   raw.Get("ADIF"),
	}
	if count := raw.Get("COUNT"); count != "" {
		if _, err := fmt.Sscan(count, &res.Count); err != nil {
			return nil, fmt.Errorf("qrz logbook fetch: parse COUNT %q: %w", count, err)
		}
	}

	switch res.Result {
	case "AUTH":
		return nil, fmt.Errorf("qrz logbook: API key is invalid")
	case "FAIL":
		return nil, fmt.Errorf("qrz logbook fetch failed: %s", res.Reason)
	}
	return res, nil
}

// DeleteQSOs removes QSOs from the logbook by their QRZ LOGIDs.
// Used when a QSO is deleted locally. Partial deletion is possible; check result.
func (c *LogbookClient) DeleteQSOs(ctx context.Context, logids []string) (*DeleteResult, error) {
	if len(logids) == 0 {
		return &DeleteResult{Result: "OK"}, nil
	}

	params := url.Values{}
	params.Set("KEY", c.apiKey)
	params.Set("ACTION", "DELETE")
	params.Set("LOGIDS", strings.Join(logids, ","))

	raw, err := c.post(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("qrz logbook delete: %w", err)
	}

	result := &DeleteResult{
		Result: raw.Get("RESULT"),
		Reason: raw.Get("REASON"),
	}
	if count := raw.Get("COUNT"); count != "" {
		if _, err := fmt.Sscan(count, &result.Count); err != nil {
			return nil, fmt.Errorf("qrz logbook delete: parse COUNT %q: %w", count, err)
		}
	}

	return result, nil
}

// ──────────────────────────────────────────────────────────────────────────────
// Internal helpers
// ──────────────────────────────────────────────────────────────────────────────

// post sends a POST request to the QRZ Logbook API and returns the parsed
// response as url.Values. QRZ responses are URL-encoded key=value pairs.
func (c *LogbookClient) post(ctx context.Context, params url.Values) (url.Values, error) {
	body := params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, logbookEndpoint,
		strings.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", logbookAgentString)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http POST: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	rawBody, err := io.ReadAll(io.LimitReader(resp.Body, logbookMaxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	// QRZ Logbook API returns URL-encoded key=value pairs.
	// Example: RESULT=OK&LOGID=130877825&COUNT=1
	//
	// FETCH responses include an ADIF field whose value contains raw ADIF text
	// with newlines, angle brackets, and potentially '&' characters. Using
	// url.ParseQuery on the full body fails because Go's parser treats every
	// '&' as a parameter separator, exceeding the default 1000-parameter limit
	// when many QSOs are returned.
	//
	// Strategy: find ADIF= (case-insensitive) and split there. Parse only the
	// prefix with url.ParseQuery (which has very few params), then attach the
	// raw ADIF value directly.
	parsed := parseQRZResponse(string(rawBody))
	return parsed, nil
}

// parseQRZResponse handles the QRZ logbook response format, correctly
// separating the ADIF payload (which may contain '&') from the other
// URL-encoded key=value pairs.
func parseQRZResponse(body string) url.Values {
	// Look for &ADIF= (case-insensitive) to split off the ADIF payload.
	bodyUpper := strings.ToUpper(body)
	adifIdx := strings.Index(bodyUpper, "&ADIF=")
	if adifIdx == -1 {
		// Also check if it starts with ADIF= (no leading &).
		if strings.HasPrefix(bodyUpper, "ADIF=") {
			result := make(url.Values)
			adifDecoded, _ := url.QueryUnescape(body[5:])
			result.Set("ADIF", adifDecoded)
			return result
		}
		// No ADIF field — safe to use standard parser (few params).
		parsed, err := url.ParseQuery(body)
		if err != nil {
			// Fall back: return what we can.
			result := make(url.Values)
			result.Set("_raw", body)
			return result
		}
		return parsed
	}

	// Everything before &ADIF= is normal URL-encoded params.
	prefix := body[:adifIdx]
	adifRaw := body[adifIdx+6:] // skip "&ADIF="

	parsed, _ := url.ParseQuery(prefix)
	if parsed == nil {
		parsed = make(url.Values)
	}
	// Decode the ADIF value (it may contain %3C, %3E, %0A, etc.).
	adifDecoded, _ := url.QueryUnescape(adifRaw)
	parsed.Set("ADIF", adifDecoded)
	return parsed
}

// parseStatusData parses the DATA field from a STATUS response into sr.
// The DATA field contains "&"-separated name=value pairs.
// Known aggregate keys: BOOK, TOTAL, CONFIRMED, DXCC.
// Per-logid confirmation: numeric keys mapping to "Y"/"N" values.
func parseStatusData(data string, sr *StatusResult) {
	if data == "" {
		return
	}

	// Split on "&" — this is already URL-decoded by ParseQuery at the outer level.
	parts := strings.Split(data, "&")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		k := strings.TrimSpace(kv[0])
		v := strings.TrimSpace(kv[1])

		switch strings.ToUpper(k) {
		case "TOTAL":
			if _, err := fmt.Sscan(v, &sr.Total); err != nil {
				continue
			}
		case "CONFIRMED":
			if _, err := fmt.Sscan(v, &sr.Confirmed); err != nil {
				continue
			}
		default:
			// Numeric key → per-logid confirmation status (value: "Y"/"N"/"1"/"0").
			if isNumeric(k) {
				if sr.PerLogID == nil {
					sr.PerLogID = make(map[string]bool)
				}
				confirmed := strings.EqualFold(v, "Y") || v == "1"
				sr.PerLogID[k] = confirmed
			}
		}
	}
}

// isNumeric returns true if s consists entirely of ASCII digits.
func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
