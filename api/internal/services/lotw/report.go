package lotw

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	adifpkg "github.com/FtlC-ian/radioledger/pkg/adif"
)

const (
	// ARRLReportURL is the LoTW report endpoint used to download confirmed QSOs.
	ARRLReportURL = "https://lotw.arrl.org/lotwuser/lotwreport.adi"
)

var (
	ErrReportAuthFailed = errors.New("lotw report authentication failed")
	ErrReportRateLimit  = errors.New("lotw report rate limited")
)

// StoredPasswords is the encrypted credential payload stored for service="lotw".
//
// Backwards compatibility: older rows stored only the vault password as raw text.
// DecodeStoredPasswords transparently handles both formats.
//
// For confirmation pulls, WebPassword is the important field because the ARRL
// report endpoint uses the user's LoTW web password rather than the vault
// password used for certificate signing.
type StoredPasswords struct {
	VaultPassword string `json:"vault_password,omitempty"`
	WebPassword   string `json:"web_password,omitempty"`
}

func EncodeStoredPasswords(p StoredPasswords) ([]byte, error) {
	if strings.TrimSpace(p.VaultPassword) == "" && strings.TrimSpace(p.WebPassword) == "" {
		return nil, fmt.Errorf("at least one lotw password must be set")
	}
	return json.Marshal(StoredPasswords{
		VaultPassword: strings.TrimSpace(p.VaultPassword),
		WebPassword:   strings.TrimSpace(p.WebPassword),
	})
}

func DecodeStoredPasswords(plaintext []byte) (StoredPasswords, error) {
	trimmed := strings.TrimSpace(string(plaintext))
	if trimmed == "" {
		return StoredPasswords{}, nil
	}
	if strings.HasPrefix(trimmed, "{") {
		var p StoredPasswords
		if err := json.Unmarshal([]byte(trimmed), &p); err != nil {
			return StoredPasswords{}, fmt.Errorf("decode lotw credential payload: %w", err)
		}
		p.VaultPassword = strings.TrimSpace(p.VaultPassword)
		p.WebPassword = strings.TrimSpace(p.WebPassword)
		return p, nil
	}
	// Legacy format: vault password only.
	return StoredPasswords{VaultPassword: trimmed}, nil
}

// ReportRecord is one confirmed QSO returned by LoTW's report endpoint.
type ReportRecord struct {
	Callsign   string
	Band       string
	Mode       string
	DatetimeOn time.Time
	QSLDate    time.Time
}

// ReportResult contains the parsed report contents and the checkpoint timestamp
// returned by LoTW for incremental polling.
type ReportResult struct {
	Records   []ReportRecord
	LastQSLAt *time.Time
	// RawResponse is intentionally omitted. The ARRL report URL contains the
	// user's LoTW web password as a query parameter (upstream API constraint).
	// Never store or log the request URL or raw response body for this endpoint.
}

// DownloadConfirmedReport downloads confirmed LoTW records for the given login.
//
// When since is non-nil, qso_qslsince is sent as a full timestamp so subsequent
// runs can use APP_LoTW_LASTQSL as a high-water mark.
func DownloadConfirmedReport(ctx context.Context, login, password string, since *time.Time) (*ReportResult, error) {
	login = strings.TrimSpace(login)
	password = strings.TrimSpace(password)
	if login == "" {
		return nil, fmt.Errorf("lotw report login must not be empty")
	}
	if password == "" {
		return nil, fmt.Errorf("lotw report password must not be empty")
	}

	params := url.Values{}
	params.Set("login", login)
	params.Set("password", password)
	params.Set("qso_query", "1")
	params.Set("qso_qsl", "yes")
	if since != nil && !since.IsZero() {
		params.Set("qso_qslsince", since.UTC().Format("2006-01-02 15:04:05"))
	}

	// NOTE: The ARRL LoTW report API requires the password as a GET query
	// parameter — this is an upstream API constraint that cannot be changed.
	// To prevent credential exposure:
	//   - The request URL is never logged or included in error messages.
	//   - The raw response body is never stored or logged.
	//   - RawResponse has been removed from ReportResult.
	requestURL := ARRLReportURL + "?" + params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		// Avoid wrapping err directly — it may contain the full URL with password.
		return nil, fmt.Errorf("build lotw report request: invalid parameters")
	}
	req.Header.Set("User-Agent", "RadioLedger/1.0")

	resp, err := (&http.Client{Timeout: 60 * time.Second}).Do(req)
	if err != nil {
		// Do not wrap err: Go's http error strings include the request URL.
		return nil, fmt.Errorf("lotw report request failed (network or TLS error)")
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("read lotw report response: %w", err)
	}
	bodyStr := string(body)

	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("%w: HTTP 429", ErrReportRateLimit)
	}
	if resp.StatusCode >= 500 {
		return nil, fmt.Errorf("lotw report HTTP %d", resp.StatusCode)
	}

	if !strings.Contains(strings.ToUpper(bodyStr), "<EOH>") {
		return nil, classifyReportFailure(bodyStr, resp.StatusCode)
	}

	result, err := parseReportResponse(ctx, bodyStr)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func classifyReportFailure(body string, statusCode int) error {
	msg := strings.TrimSpace(body)
	lower := strings.ToLower(msg)

	switch {
	case strings.Contains(lower, "password") && (strings.Contains(lower, "invalid") || strings.Contains(lower, "incorrect") || strings.Contains(lower, "failed")):
		return fmt.Errorf("%w: %s", ErrReportAuthFailed, truncateReportMessage(msg))
	case strings.Contains(lower, "login") && (strings.Contains(lower, "invalid") || strings.Contains(lower, "incorrect") || strings.Contains(lower, "failed")):
		return fmt.Errorf("%w: %s", ErrReportAuthFailed, truncateReportMessage(msg))
	case statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden:
		return fmt.Errorf("%w: HTTP %d", ErrReportAuthFailed, statusCode)
	case strings.Contains(lower, "rate limit") || strings.Contains(lower, "too many") || strings.Contains(lower, "try again later"):
		return fmt.Errorf("%w: %s", ErrReportRateLimit, truncateReportMessage(msg))
	default:
		return fmt.Errorf("lotw report failed: %s", truncateReportMessage(msg))
	}
}

func parseReportResponse(ctx context.Context, adifText string) (*ReportResult, error) {
	parser := adifpkg.NewParser(bytes.NewReader([]byte(adifText)))
	header, err := parser.Header(ctx)
	if err != nil {
		return nil, fmt.Errorf("parse lotw report header: %w", err)
	}

	result := &ReportResult{}
	if t := parseLoTWTimestamp(header.Get("APP_LOTW_LASTQSL")); t != nil {
		result.LastQSLAt = t
	}

	for {
		rec, err := parser.Next(ctx)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("parse lotw report record: %w", err)
		}

		qslRcvd := strings.ToUpper(strings.TrimSpace(rec.Get("QSL_RCVD")))
		if qslRcvd != "" && qslRcvd != "Y" {
			continue
		}

		callsign := strings.ToUpper(strings.TrimSpace(rec.Get("CALL")))
		band := strings.ToLower(strings.TrimSpace(rec.Get("BAND")))
		mode := strings.ToUpper(strings.TrimSpace(rec.Get("MODE")))
		if mode == "" {
			mode = strings.ToUpper(strings.TrimSpace(rec.Get("APP_LOTW_MODE")))
		}
		dt := parseReportDateTime(rec.Get("QSO_DATE"), rec.Get("TIME_ON"))
		qslDate := parseReportDate(rec.Get("QSLRDATE"))
		if qslDate == nil {
			qslDate = parseLoTWTimestamp(rec.Get("APP_LOTW_RXQSL"))
		}

		if callsign == "" || band == "" || mode == "" || dt.IsZero() || qslDate == nil {
			continue
		}

		result.Records = append(result.Records, ReportRecord{
			Callsign:   callsign,
			Band:       band,
			Mode:       mode,
			DatetimeOn: dt,
			QSLDate:    qslDate.UTC(),
		})
	}

	return result, nil
}

func parseReportDateTime(qsoDate, timeOn string) time.Time {
	qsoDate = strings.TrimSpace(qsoDate)
	timeOn = strings.TrimSpace(timeOn)
	if len(qsoDate) != 8 {
		return time.Time{}
	}
	for len(timeOn) < 4 {
		timeOn += "0"
	}
	if len(timeOn) > 6 {
		timeOn = timeOn[:6]
	}
	if len(timeOn) == 4 {
		timeOn += "00"
	}
	t, err := time.Parse("20060102150405", qsoDate+timeOn)
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}

func parseReportDate(raw string) *time.Time {
	raw = strings.TrimSpace(raw)
	if len(raw) != 8 {
		return nil
	}
	t, err := time.Parse("20060102", raw)
	if err != nil {
		return nil
	}
	t = t.UTC()
	return &t
}

func parseLoTWTimestamp(raw string) *time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	layouts := []string{
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
		time.RFC3339,
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, raw); err == nil {
			t = t.UTC()
			return &t
		}
	}
	return nil
}

func truncateReportMessage(s string) string {
	s = strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
	if len(s) <= 240 {
		return s
	}
	return s[:240] + "…"
}
