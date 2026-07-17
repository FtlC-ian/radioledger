// Package eqsl provides an HTTP client for the eQSL.cc API.
//
// # eQSL API Overview
//
// eQSL uses a simple HTTP form-POST API with HTTP Basic-style username/password
// parameters (not standard HTTP Basic Auth headers). All communication is over HTTPS.
//
// eQSL supports two primary operations:
//  1. Upload: POST QSOs in ADIF format to add them to the user's outbox.
//  2. Download: GET the user's inbox (eQSLs received from other stations).
//
// # Rate Limiting
//
// eQSL is aggressive about rate limiting. This client enforces a minimum of
// 2 seconds between requests (shared across upload and download). Exceeding this
// will result in temporary or permanent bans.
//
// # Authentication
//
// eQSL uses username + password submitted as form parameters. There is no OAuth
// or API key support. Plaintext credentials are never logged by this client.
//
// # Upload Format
//
// QSOs are uploaded as ADIF text in a form field named "ADIFData". The response
// is HTML text containing a result line like:
//
//	Result: 1 out of 1 records added
//	Error: No records were added
//
// # Download Format
//
// The inbox is downloaded as an ADIF file. Query parameters control date filtering.
// The special value "password=readonly" combined with "HamID=<callsign>" returns
// only records from a given callsign — we instead use authenticated inbox download.
//
// References:
//   - https://www.eqsl.cc/qslcard/ADIFContentSpecs.cfm
//   - docs/SYNC_SERVICES.md § eQSL.cc
//   - SCHEMA.md § sync_status, user_service_credentials
package eqsl

import (
	"context"
	"encoding/json"
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
	// uploadEndpoint is the URL for ADIF upload.
	uploadEndpoint = "https://www.eqsl.cc/qslcard/ImportADIF.cfm"

	// inboxEndpoint is the URL for inbox (received eQSL) download.
	inboxEndpoint = "https://www.eqsl.cc/qslcard/DownloadInBox.cfm"

	// agentString identifies RadioLedger to eQSL.
	agentString = "RadioLedger/1.0"

	// requestTimeout is the per-request HTTP timeout.
	requestTimeout = 30 * time.Second

	// minRequestInterval enforces eQSL rate limiting: max 1 request per 2 seconds.
	// eQSL will ban clients that exceed this — be conservative.
	minRequestInterval = 2 * time.Second
)

// UsernamePassword holds eQSL credentials decoded from the encrypted store.
// These are decrypted in memory for API calls only; never logged or stored in plaintext.
type UsernamePassword struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// DecodeCredentials unmarshals a decrypted credential blob into UsernamePassword.
func DecodeCredentials(plaintext []byte) (*UsernamePassword, error) {
	var creds UsernamePassword
	if err := json.Unmarshal(plaintext, &creds); err != nil {
		return nil, fmt.Errorf("eqsl: decode credentials: %w", err)
	}
	if creds.Username == "" || creds.Password == "" {
		return nil, fmt.Errorf("eqsl: credentials missing username or password")
	}
	return &creds, nil
}

// EncodeCredentials serializes a UsernamePassword to JSON for encrypted storage.
func EncodeCredentials(username, password string) ([]byte, error) {
	return json.Marshal(UsernamePassword{Username: username, Password: password})
}

// UploadResult summarizes the outcome of an ADIF upload to eQSL.
type UploadResult struct {
	// Accepted is the count of QSOs successfully added.
	Accepted int

	// Total is the count of QSOs submitted.
	Total int

	// RawResponse is the full response text for debugging.
	RawResponse string
}

// InboxRecord represents a single eQSL received from another station.
// These are parsed from the ADIF returned by the inbox download endpoint.
type InboxRecord struct {
	// TheirCallsign is the callsign of the station that sent this eQSL.
	TheirCallsign string

	// Band is the band as reported by the sender.
	Band string

	// Mode is the mode as reported by the sender.
	Mode string

	// DatetimeOn is the QSO date/time as reported by the sender (UTC).
	DatetimeOn time.Time

	// RstRcvd is the RST they gave us.
	RstRcvd string
}

// Client is a thread-safe eQSL HTTP API client.
// Construct via New; the zero value is not usable.
type Client struct {
	creds *UsernamePassword

	mu          sync.Mutex
	lastRequest time.Time

	httpClient *http.Client
	logger     *slog.Logger
}

// New creates an eQSL client for the given credentials.
// The client is thread-safe and should be reused within a single sync operation.
// Never log or persist the credentials after this call.
func New(creds *UsernamePassword, logger *slog.Logger) *Client {
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

// UploadADIF uploads QSOs in ADIF format to the eQSL outbox.
// The adifData parameter is the full ADIF document (header + records) as a string.
// Returns the number of records accepted and the total submitted.
//
// The caller is responsible for formatting adifData as valid ADIF.
// Large batches should be split if eQSL rejects them (no documented limit, use ≤500).
func (c *Client) UploadADIF(ctx context.Context, adifData string) (*UploadResult, error) {
	c.mu.Lock()
	c.throttle()
	c.mu.Unlock()

	params := url.Values{}
	params.Set("EQSL_USER", c.creds.Username)
	params.Set("EQSL_PSWD", c.creds.Password)
	params.Set("ADIFData", adifData)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, uploadEndpoint,
		strings.NewReader(params.Encode()))
	if err != nil {
		return nil, fmt.Errorf("eqsl upload: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", agentString)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("eqsl upload: http POST: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("eqsl upload: read response: %w", err)
	}

	bodyStr := string(body)

	// eQSL returns HTML; parse the result line.
	// Success looks like: "Result: 1 out of 1 records added"
	// Error looks like:   "Error: No records were added" or specific error text.
	result, err := parseUploadResponse(bodyStr)
	if err != nil {
		return nil, fmt.Errorf("eqsl upload: parse response: %w", err)
	}

	c.logger.DebugContext(ctx, "eqsl upload complete",
		slog.Int("accepted", result.Accepted),
		slog.Int("total", result.Total),
	)

	return result, nil
}

// DownloadInbox downloads the eQSL inbox (QSLs received from other stations).
// If sinceDate is non-zero, only records received after that date are returned.
// Returns a slice of InboxRecord parsed from the ADIF response.
//
// The inbox returns eQSLs that OTHER stations sent TO us, which we use to update
// qsl_rcvd status on our logged QSOs.
func (c *Client) DownloadInbox(ctx context.Context, sinceDate time.Time) ([]InboxRecord, error) {
	c.mu.Lock()
	c.throttle()
	c.mu.Unlock()

	params := url.Values{}
	params.Set("UserName", c.creds.Username)
	params.Set("Password", c.creds.Password)
	params.Set("RcvdSince", "")

	if !sinceDate.IsZero() {
		// eQSL expects dates in MM/DD/YYYY format
		params.Set("RcvdSince", sinceDate.UTC().Format("01/02/2006"))
	}

	reqURL := inboxEndpoint + "?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("eqsl inbox: build request: %w", err)
	}
	req.Header.Set("User-Agent", agentString)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("eqsl inbox: http GET: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("eqsl inbox: unexpected status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("eqsl inbox: read response: %w", err)
	}

	bodyStr := string(body)

	// Check for auth error (eQSL returns HTML error page on bad credentials)
	if strings.Contains(bodyStr, "Invalid UserName/Password") ||
		strings.Contains(bodyStr, "Invalid Username") {
		return nil, fmt.Errorf("eqsl inbox: authentication failed — check credentials")
	}

	records, err := parseInboxADIF(bodyStr)
	if err != nil {
		return nil, fmt.Errorf("eqsl inbox: parse ADIF: %w", err)
	}

	c.logger.DebugContext(ctx, "eqsl inbox downloaded",
		slog.Int("record_count", len(records)),
	)

	return records, nil
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

// parseUploadResponse extracts accepted/total counts from eQSL's HTML response.
// eQSL embeds the result in an HTML page as a text snippet — we search for it.
func parseUploadResponse(body string) (*UploadResult, error) {
	result := &UploadResult{RawResponse: body}

	// Look for "Result: N out of M records added"
	if idx := strings.Index(body, "Result:"); idx >= 0 {
		segment := body[idx:]
		var accepted, total int
		if n, _ := fmt.Sscanf(segment, "Result: %d out of %d records added", &accepted, &total); n == 2 {
			result.Accepted = accepted
			result.Total = total
			return result, nil
		}
	}

	// Look for explicit error markers
	if strings.Contains(body, "Error:") || strings.Contains(body, "No records were added") {
		// Extract error text for better diagnostics
		if idx := strings.Index(body, "Error:"); idx >= 0 {
			line := strings.SplitN(body[idx:], "\n", 2)[0]
			return nil, fmt.Errorf("eqsl rejected upload: %s", strings.TrimSpace(line))
		}
		return nil, fmt.Errorf("eqsl rejected upload: no records added")
	}

	// Response didn't match known patterns — return what we got (0 accepted, log for debugging).
	result.Total = 0
	result.Accepted = 0
	return result, nil
}

// parseInboxADIF parses the ADIF content returned by the eQSL inbox download.
// eQSL uses a simple ADIF format with CALL, BAND, MODE, QSO_DATE, TIME_ON fields.
func parseInboxADIF(adifText string) ([]InboxRecord, error) {
	var records []InboxRecord

	// Find the start of ADIF records (after the <EOH> header)
	// eQSL may or may not include a header.
	text := adifText
	if eohIdx := strings.Index(strings.ToUpper(adifText), "<EOH>"); eohIdx >= 0 {
		text = adifText[eohIdx+5:]
	}

	// Parse each record terminated by <EOR>
	for {
		eorIdx := strings.Index(strings.ToUpper(text), "<EOR>")
		if eorIdx < 0 {
			break
		}
		recordText := text[:eorIdx]
		text = text[eorIdx+5:]

		rec, ok := parseADIFRecord(recordText)
		if ok {
			records = append(records, rec)
		}
	}

	return records, nil
}

// parseADIFRecord parses a single ADIF record snippet into an InboxRecord.
// Returns (record, true) on success, (zero, false) if the record is missing required fields.
func parseADIFRecord(text string) (InboxRecord, bool) {
	fields := extractADIFFields(text)

	callsign, hasCall := fields["CALL"]
	band, hasBand := fields["BAND"]
	mode, hasMode := fields["MODE"]
	if !hasCall || !hasBand || !hasMode {
		return InboxRecord{}, false
	}

	// Parse QSO_DATE + TIME_ON into a UTC timestamp.
	// eQSL uses YYYYMMDD and HHMMSS.
	qsoDate := fields["QSO_DATE"]
	timeOn := fields["TIME_ON"]
	if len(timeOn) < 4 {
		timeOn += "00"
	}
	var dt time.Time
	if qsoDate != "" {
		parsed, err := time.Parse("20060102 1504", qsoDate+" "+timeOn[:4])
		if err == nil {
			dt = parsed.UTC()
		}
	}

	return InboxRecord{
		TheirCallsign: strings.ToUpper(strings.TrimSpace(callsign)),
		Band:          strings.ToLower(strings.TrimSpace(band)),
		Mode:          strings.ToUpper(strings.TrimSpace(mode)),
		DatetimeOn:    dt,
		RstRcvd:       fields["RST_RCVD"],
	}, true
}

// extractADIFFields extracts all field values from an ADIF record text segment.
// Returns a map of uppercase field name → value.
func extractADIFFields(text string) map[string]string {
	fields := make(map[string]string)
	upper := strings.ToUpper(text)

	for i := 0; i < len(text); {
		// Find next <FIELD:LEN> or <FIELD:LEN:TYPE> tag
		start := strings.Index(upper[i:], "<")
		if start < 0 {
			break
		}
		start += i
		end := strings.Index(upper[start:], ">")
		if end < 0 {
			break
		}
		end += start + 1 // end points past the '>'

		tag := upper[start+1 : end-1] // content between < and >
		parts := strings.SplitN(tag, ":", 3)
		if len(parts) < 2 {
			i = end
			continue
		}

		name := parts[0]
		var length int
		if _, err := fmt.Sscanf(parts[1], "%d", &length); err != nil || length <= 0 {
			i = end
			continue
		}

		if end+length > len(text) {
			break
		}
		value := text[end : end+length]
		fields[name] = value
		i = end + length
	}

	return fields
}

// PasswordWarning is the UI-facing security notice that should be displayed
// whenever a user is prompted to enter their eQSL password.
//
// eQSL does not support OAuth or API keys — RadioLedger must store the actual
// password (AES-256-GCM encrypted). Users should use a password unique to eQSL
// so that a compromise of this credential does not affect other services.
//
// This constant is exported so it can be included in API responses (e.g. the
// GET /v1/sync/credentials endpoint) without duplicating the string in handlers.
const PasswordWarning = "eQSL requires storing your password encrypted. Use a password unique to eQSL."
