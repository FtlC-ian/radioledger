// Package pskreporter provides a client for querying reception reports from
// pskreporter.info.
//
// # PSK Reporter API Overview
//
// PSK Reporter aggregates reception reports from ham radio operators worldwide.
// Stations running digital-mode software (WSJT-X, JTDX, etc.) automatically
// upload spots of stations they decode; those spots are queryable via a public
// HTTP API.
//
// This client fetches reports where a specific callsign was heard (i.e., signals
// received by other stations). No API key is required — a descriptive User-Agent
// header identifying the application is sufficient and required by the service.
//
// Rate limit: do not poll more than once every 5 minutes per callsign.
//
// References:
//   - https://pskreporter.info/pskdev.html#URLParams
//   - docs/api-research/PSKReporter.md
package pskreporter

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// baseURL is the PSK Reporter query endpoint.
	baseURL = "https://pskreporter.info/query"

	// agentString identifies RadioLedger to the PSK Reporter service.
	// PSK Reporter requires a descriptive User-Agent.
	agentString = "RadioLedger/1.0 (https://radioledger.app)"

	// requestTimeout is the per-request HTTP timeout.
	requestTimeout = 30 * time.Second

	// MinPollInterval is the minimum time between queries for the same callsign.
	// PSK Reporter asks clients not to poll more than once every 5 minutes.
	MinPollInterval = 5 * time.Minute

	// defaultLookbackSeconds is the default window fetched per poll (-900 = last 15 min).
	defaultLookbackSeconds = -900
)

// ReceptionReport represents a single reception event reported to PSK Reporter.
// A receiver heard a sender on a given frequency/mode at a specific time.
type ReceptionReport struct {
	// SenderCallsign is the station that was heard (the station we queried for).
	SenderCallsign string

	// ReceiverCallsign is the station that reported hearing SenderCallsign.
	ReceiverCallsign string

	// FrequencyHz is the centre frequency in Hz (0 = unknown).
	FrequencyHz int64

	// Mode is the digital mode (e.g. "FT8", "FT4", "JS8", "PSK31").
	Mode string

	// SNR is the signal-to-noise ratio in dB as reported by the receiver.
	SNR *int16

	// Grid is the Maidenhead grid square reported by the receiver.
	Grid string

	// SpottedAt is the UTC time when the reception was observed.
	SpottedAt time.Time
}

// FrequencyKHz returns the frequency in kHz (nil if unknown).
func (r *ReceptionReport) FrequencyKHz() *float64 {
	if r.FrequencyHz <= 0 {
		return nil
	}
	f := float64(r.FrequencyHz) / 1000.0
	return &f
}

// Client queries pskreporter.info for reception reports.
// It enforces the rate limit (MinPollInterval per callsign) internally.
type Client struct {
	http *http.Client

	mu          sync.Mutex
	lastQueried map[string]time.Time // callsign → last query time
}

// New creates a new PSK Reporter client with default settings.
func New() *Client {
	return &Client{
		http: &http.Client{
			Timeout: requestTimeout,
		},
		lastQueried: make(map[string]time.Time),
	}
}

// NewWithHTTPClient creates a new PSK Reporter client using the provided
// *http.Client (useful for testing).
func NewWithHTTPClient(hc *http.Client) *Client {
	return &Client{
		http:        hc,
		lastQueried: make(map[string]time.Time),
	}
}

// FetchReports fetches recent FT8 reception reports for callsign.
// The mode filter defaults to "FT8" if empty.
// Returns (nil, nil) if the rate limit has not yet expired for this callsign.
func (c *Client) FetchReports(ctx context.Context, callsign, mode string) ([]ReceptionReport, error) {
	callsign = strings.ToUpper(strings.TrimSpace(callsign))
	if callsign == "" {
		return nil, fmt.Errorf("pskreporter: callsign must not be empty")
	}
	if mode == "" {
		mode = "FT8"
	}

	// Enforce rate limit.
	c.mu.Lock()
	last, seen := c.lastQueried[callsign]
	if seen && time.Since(last) < MinPollInterval {
		c.mu.Unlock()
		slog.Debug("pskreporter: rate limit active, skipping query",
			slog.String("callsign", callsign),
			slog.Duration("cooldown_remaining", MinPollInterval-time.Since(last)),
		)
		return nil, nil
	}
	c.lastQueried[callsign] = time.Now()
	c.mu.Unlock()

	params := url.Values{}
	params.Set("senderCallsign", callsign)
	params.Set("mode", mode)
	params.Set("flowStartSeconds", strconv.Itoa(defaultLookbackSeconds))
	params.Set("noactive", "1")   // skip active reporters list
	params.Set("nolocator", "1") // skip locator map data

	reqURL := baseURL + "?" + params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("pskreporter: build request: %w", err)
	}
	req.Header.Set("User-Agent", agentString)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("pskreporter: http request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("pskreporter: unexpected status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024)) // 10 MB limit
	if err != nil {
		return nil, fmt.Errorf("pskreporter: read body: %w", err)
	}

	return parseXML(callsign, body)
}

// ─── XML parsing ─────────────────────────────────────────────────────────────

// pskXMLResponse is the top-level XML envelope returned by pskreporter.info.
type pskXMLResponse struct {
	XMLName      xml.Name     `xml:"receptionReports"`
	ActiveCalls  []activeCall `xml:"activeReceiver"`
	ReceptionRow []receptionRow `xml:"receptionReport"`
}

type activeCall struct {
	Callsign string `xml:"callsign,attr"`
	Locator  string `xml:"locator,attr"`
}

type receptionRow struct {
	SenderCallsign   string `xml:"senderCallsign,attr"`
	ReceiverCallsign string `xml:"receiverCallsign,attr"`
	Frequency        string `xml:"frequency,attr"`
	FlowStartSeconds string `xml:"flowStartSeconds,attr"`
	Mode             string `xml:"mode,attr"`
	SenderLocator    string `xml:"senderLocator,attr"`
	ReceiverLocator  string `xml:"receiverLocator,attr"`
	SNR              string `xml:"sNR,attr"`
}

func parseXML(queriedCallsign string, data []byte) ([]ReceptionReport, error) {
	var envelope pskXMLResponse
	if err := xml.Unmarshal(data, &envelope); err != nil {
		return nil, fmt.Errorf("pskreporter: parse XML: %w", err)
	}

	reports := make([]ReceptionReport, 0, len(envelope.ReceptionRow))
	for _, row := range envelope.ReceptionRow {
		sender := strings.ToUpper(strings.TrimSpace(row.SenderCallsign))
		receiver := strings.ToUpper(strings.TrimSpace(row.ReceiverCallsign))
		if sender == "" || receiver == "" {
			continue
		}

		var freqHz int64
		if f, err := strconv.ParseInt(strings.TrimSpace(row.Frequency), 10, 64); err == nil && f > 0 {
			freqHz = f
		}

		spottedAt := parseFlowStart(row.FlowStartSeconds)

		var snrPtr *int16
		if row.SNR != "" {
			if v, err := strconv.ParseInt(strings.TrimSpace(row.SNR), 10, 16); err == nil {
				s := int16(v)
				snrPtr = &s
			}
		}

		grid := strings.ToUpper(strings.TrimSpace(row.SenderLocator))

		reports = append(reports, ReceptionReport{
			SenderCallsign:   sender,
			ReceiverCallsign: receiver,
			FrequencyHz:      freqHz,
			Mode:             strings.ToUpper(strings.TrimSpace(row.Mode)),
			SNR:              snrPtr,
			Grid:             grid,
			SpottedAt:        spottedAt,
		})
	}

	slog.Debug("pskreporter: parsed reception reports",
		slog.String("callsign", queriedCallsign),
		slog.Int("count", len(reports)),
	)
	return reports, nil
}

// parseFlowStart converts the flowStartSeconds attribute to a UTC time.
// PSK Reporter encodes the observation time as a Unix timestamp (seconds since
// epoch) in the flowStartSeconds attribute.
func parseFlowStart(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Now().UTC()
	}
	if ts, err := strconv.ParseInt(raw, 10, 64); err == nil && ts > 0 {
		return time.Unix(ts, 0).UTC()
	}
	return time.Now().UTC()
}
