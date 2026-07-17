// Package eqsl — inbox.go: eQSL inbox downloader for confirmation pulling.
//
// This file implements the two-step download process for the eQSL inbox:
//  1. POST/GET to DownloadInBox.cfm — returns an HTML page with ADIF file links.
//  2. Follow the ADIF link — download the actual ADIF file.
//
// The returned records include APP_EQSL_AG (Authenticity Guaranteed) status,
// used to populate the eqsl_ag column in qso_confirmations.
//
// API reference: https://www.eqsl.cc/qslcard/DownloadInBox.txt
//
// # Format
//
// The response page contains a string "Your ADIF log file has been built",
// followed by two A HREF links — one .adi, one .txt — pointing to the ADIF data.
// We follow the first link to retrieve the actual ADIF.
//
// # Fields of interest
//
//	CALL          — callsign of the station that sent the eQSL
//	QSO_DATE      — YYYYMMDD
//	TIME_ON       — HHMMSS (or HHMM)
//	BAND          — band string (e.g. "40m")
//	MODE          — mode string (e.g. "CW", "SSB", "FT8")
//	APP_EQSL_AG   — present and "Y" when sender has Authenticity Guaranteed status
//	EQSL_AG       — present in confirmed records: "Y" if AG, "N" if not
package eqsl

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	adifpkg "github.com/FtlC-ian/radioledger/pkg/adif"
)

const (
	// inboxDownloadTimeout is the per-request HTTP timeout for inbox download operations.
	// eQSL file generation can be slow — 60 s is generous.
	inboxDownloadTimeout = 60 * time.Second

	// inboxMaxBytes limits how much ADIF we read from eQSL (10 MB).
	inboxMaxBytes = 10 * 1024 * 1024
)

// hrefPattern matches an A HREF link in eQSL's response HTML.
// eQSL emits something like:
//
//	<A HREF="https://www.eQSL.cc/DownloadedFiles/abcdef.adi">
var hrefPattern = regexp.MustCompile(`(?i)<a\s+href="([^"]*\.(?:adi|txt))"`)

// InboxRecordAG is an eQSL inbox record enriched with AG and EQSL_AG status.
// It extends the basic InboxRecord with the AG authentication fields returned
// by the DownloadInBox endpoint.
type InboxRecordAG struct {
	// TheirCallsign is the callsign of the station that sent this eQSL.
	TheirCallsign string

	// Band is the band as reported by the sender (lowercase, e.g. "40m").
	Band string

	// Mode is the mode as reported by the sender (uppercase, e.g. "FT8").
	Mode string

	// DatetimeOn is the QSO date/time as reported by the sender (UTC).
	DatetimeOn time.Time

	// AppEQSLAG is true when the sender of this eQSL has Authenticity Guaranteed
	// status (APP_EQSL_AG field = "Y").
	AppEQSLAG bool

	// EQSLQSLRCVD is true when the EQSL_QSL_RCVD field is "Y" (confirmed).
	EQSLQSLRCVD bool

	// EQSLQSLRDate is the date the eQSL was received into the database (EQSL_QSLRDATE).
	EQSLQSLRDate *time.Time

	// QSLRDate is the date parsed from QSLRDATE.
	QSLRDate *time.Time
}

// DownloadInboxAG downloads the user's eQSL inbox and returns enriched records
// including AG (Authenticity Guaranteed) status.
//
// sinceDate, if non-zero, limits the download to eQSLs received on or after
// that date (RcvdSince parameter, format YYYYMMDDHHMM per eQSL docs).
//
// The function performs two HTTP requests:
//  1. To DownloadInBox.cfm to trigger file generation.
//  2. To the ADIF file URL returned in the HTML response.
func DownloadInboxAG(ctx context.Context, creds *UsernamePassword, sinceDate time.Time) ([]InboxRecordAG, error) {
	if creds == nil || creds.Username == "" || creds.Password == "" {
		return nil, fmt.Errorf("eqsl inbox: credentials are required")
	}

	client := &http.Client{Timeout: inboxDownloadTimeout}

	// Build the request URL.
	params := url.Values{}
	params.Set("UserName", creds.Username)
	params.Set("Password", creds.Password)
	params.Set("ConfirmedOnly", "1")
	if !sinceDate.IsZero() {
		// RcvdSince format: YYYYMMDDHHMM
		params.Set("RcvdSince", sinceDate.UTC().Format("200601021504"))
	}

	reqURL := inboxEndpoint + "?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("eqsl inbox: build request: %w", err)
	}
	req.Header.Set("User-Agent", agentString)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("eqsl inbox: HTTP GET: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("eqsl inbox: unexpected HTTP status %d", resp.StatusCode)
	}

	pageBody, err := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if err != nil {
		return nil, fmt.Errorf("eqsl inbox: read HTML response: %w", err)
	}
	pageStr := string(pageBody)

	// Check for auth failure.
	if isEQSLAuthError(pageStr) {
		return nil, fmt.Errorf("eqsl inbox: authentication failed — check credentials")
	}

	// Check for success indicator.
	if !strings.Contains(pageStr, "Your ADIF log file has been built") {
		// No records — empty inbox for this date range.
		if strings.Contains(strings.ToLower(pageStr), "no records") ||
			strings.Contains(strings.ToLower(pageStr), "0 records") {
			return nil, nil
		}
		return nil, fmt.Errorf("eqsl inbox: unexpected response (missing success indicator): %.300s", pageStr)
	}

	// Extract the ADIF file URL from the HTML page.
	adifURL := extractADIFLink(pageStr)
	if adifURL == "" {
		return nil, fmt.Errorf("eqsl inbox: no ADIF download link found in response")
	}

	// Fetch the ADIF file.
	adifData, err := fetchADIFFile(ctx, client, adifURL)
	if err != nil {
		return nil, fmt.Errorf("eqsl inbox: fetch ADIF file: %w", err)
	}

	// Parse the ADIF records.
	records, err := parseInboxADIFAG(ctx, adifData)
	if err != nil {
		return nil, fmt.Errorf("eqsl inbox: parse ADIF: %w", err)
	}

	return records, nil
}

// extractADIFLink finds the first .adi or .txt hyperlink in the eQSL HTML response.
func extractADIFLink(html string) string {
	matches := hrefPattern.FindStringSubmatch(html)
	if len(matches) < 2 {
		return ""
	}
	link := strings.TrimSpace(matches[1])
	// eQSL may return a relative or absolute URL. Ensure it's absolute.
	if !strings.HasPrefix(strings.ToLower(link), "http") {
		link = "https://www.eQSL.cc" + link
	}
	return link
}

// fetchADIFFile downloads the ADIF content from the given URL.
func fetchADIFFile(ctx context.Context, client *http.Client, adifURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, adifURL, nil)
	if err != nil {
		return "", fmt.Errorf("build ADIF file request: %w", err)
	}
	req.Header.Set("User-Agent", agentString)

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("HTTP GET ADIF file: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected HTTP status %d for ADIF file", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, inboxMaxBytes))
	if err != nil {
		return "", fmt.Errorf("read ADIF file: %w", err)
	}
	return string(body), nil
}

// parseInboxADIFAG parses eQSL inbox ADIF using the pkg/adif streaming parser.
// It extracts QSO fields plus the APP_EQSL_AG and EQSL_QSL_RCVD fields.
func parseInboxADIFAG(ctx context.Context, adifText string) ([]InboxRecordAG, error) {
	parser := adifpkg.NewParser(bytes.NewReader([]byte(adifText)))

	// Consume the header (ignore its contents — eQSL header has no useful data for us).
	if _, err := parser.Header(ctx); err != nil {
		// Some eQSL responses have no header (no <EOH>). The parser handles this.
		_ = err
	}

	var records []InboxRecordAG

	for {
		rec, err := parser.Next(ctx)
		if err == io.EOF {
			break
		}
		if err != nil {
			// Non-fatal: skip bad records, continue parsing.
			continue
		}

		callsign := strings.ToUpper(strings.TrimSpace(rec.Get("CALL")))
		band := strings.ToLower(strings.TrimSpace(rec.Get("BAND")))
		mode := strings.ToUpper(strings.TrimSpace(rec.Get("MODE")))
		if callsign == "" || band == "" || mode == "" {
			continue
		}

		dt := parseEQSLDateTime(rec.Get("QSO_DATE"), rec.Get("TIME_ON"))
		if dt.IsZero() {
			continue
		}

		appAG := strings.ToUpper(strings.TrimSpace(rec.Get("APP_EQSL_AG"))) == "Y"

		// EQSL_AG (added Oct 2025): Y = AG confirmed, N = not AG, absent = old record.
		eqslAG := strings.ToUpper(strings.TrimSpace(rec.Get("EQSL_AG"))) == "Y"

		// EQSL_QSL_RCVD: Y = confirmed.
		qslRcvd := strings.ToUpper(strings.TrimSpace(rec.Get("EQSL_QSL_RCVD"))) == "Y"

		var rcvdDate *time.Time
		if raw := strings.TrimSpace(rec.Get("EQSL_QSLRDATE")); raw != "" {
			if t, err := time.Parse("20060102", raw); err == nil {
				t = t.UTC()
				rcvdDate = &t
			}
		}

		var qslRDate *time.Time
		if raw := strings.TrimSpace(rec.Get("QSLRDATE")); raw != "" {
			if t, err := time.Parse("20060102", raw); err == nil {
				t = t.UTC()
				qslRDate = &t
			}
		}

		records = append(records, InboxRecordAG{
			TheirCallsign: callsign,
			Band:          band,
			Mode:          mode,
			DatetimeOn:    dt,
			AppEQSLAG:     appAG || eqslAG,
			EQSLQSLRCVD:   qslRcvd,
			EQSLQSLRDate:  rcvdDate,
			QSLRDate:      qslRDate,
		})
	}

	return records, nil
}

// parseEQSLDateTime parses eQSL's QSO_DATE (YYYYMMDD) and TIME_ON (HHMM or HHMMSS).
func parseEQSLDateTime(qsoDate, timeOn string) time.Time {
	qsoDate = strings.TrimSpace(qsoDate)
	timeOn = strings.TrimSpace(timeOn)

	if len(qsoDate) != 8 {
		return time.Time{}
	}

	// Normalise timeOn to exactly 6 digits.
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

// isEQSLAuthError reports whether an eQSL HTML response indicates an auth failure.
func isEQSLAuthError(body string) bool {
	lower := strings.ToLower(body)
	return strings.Contains(lower, "invalid username") ||
		strings.Contains(lower, "invalid username/password") ||
		strings.Contains(lower, "invalid user name") ||
		strings.Contains(lower, "authentication failed") ||
		strings.Contains(lower, "login failed")
}
