// Package qrz provides a QRZ.com XML API client for callsign lookups.
//
// # QRZ XML API Overview
//
// Authentication uses a session-based flow:
//  1. POST a login request with username and password → receive a session key.
//  2. Use the session key for subsequent callsign lookups.
//  3. If the session key expires (Error="Session Timeout"), re-authenticate automatically.
//
// Rate limiting: QRZ enforces rate limits aggressively. This client allows at most
// 1 request per second (shared across login and lookup). Exceeding this will result
// in temporary or permanent bans.
//
// QRZ XML API requires a QRZ.com subscription for programmatic access.
// Users who have not subscribed receive lookup responses without name/address data.
//
// References:
//   - https://www.qrz.com/XML/current_spec.html
//   - SCHEMA.md § callsign_cache
//   - docs/SYNC_SERVICES.md § QRZ.com Logbook
package qrz

import (
	"context"
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
	// qrzXMLEndpoint is the base URL for the QRZ XML API.
	qrzXMLEndpoint = "https://xmldata.qrz.com/xml/current/"

	// agentString identifies our application to QRZ.
	agentString = "RadioLedger/1.0"

	// requestTimeout is the per-request HTTP timeout.
	requestTimeout = 10 * time.Second

	// minRequestInterval enforces the rate limit: max 1 request per second.
	// QRZ will ban clients that exceed this without warning.
	minRequestInterval = time.Second

	// CacheTTL is the duration before a callsign_cache entry is considered stale.
	CacheTTL = 30 * 24 * time.Hour // 30 days
)

// CallsignInfo contains the structured data returned from a QRZ callsign lookup.
// All fields are optional — QRZ may not have data for every field for every callsign.
type CallsignInfo struct {
	// Callsign is the queried callsign, normalized to uppercase.
	Callsign string `json:"callsign"`

	// Name fields
	FName string `json:"fname,omitempty"`   // first name
	LName string `json:"lname,omitempty"`   // last name
	Alias string `json:"aliases,omitempty"` // comma-separated list of other callsigns

	// Address
	Addr1   string `json:"addr1,omitempty"`
	Addr2   string `json:"addr2,omitempty"`
	State   string `json:"state,omitempty"`
	Zip     string `json:"zip,omitempty"`
	Country string `json:"country,omitempty"`

	// Location
	Lat  float64 `json:"lat,omitempty"`
	Lon  float64 `json:"lon,omitempty"`
	Grid string  `json:"grid,omitempty"` // Maidenhead grid square

	// License
	Class   string `json:"class,omitempty"`   // license class (T, G, E, etc.)
	Expires string `json:"expires,omitempty"` // license expiry date

	// DXCC / entity
	DXCC      int    `json:"dxcc,omitempty"`
	Land      string `json:"land,omitempty"` // DXCC entity name
	CQZone    int    `json:"cq_zone,omitempty"`
	ITUZone   int    `json:"itu_zone,omitempty"`
	TimeZone  string `json:"time_zone,omitempty"`
	GMTOffset int    `json:"gmt_offset,omitempty"`
	DST       string `json:"dst,omitempty"`

	// QSL / contact
	QSLMgr string `json:"qsl_mgr,omitempty"`
	Email  string `json:"email,omitempty"`
	URL    string `json:"url,omitempty"`
	Image  string `json:"image,omitempty"` // profile photo URL

	// Computed from FName + LName
	FullName string `json:"full_name,omitempty"`
}

// qrzSessionResponse is the XML structure returned by the QRZ login endpoint.
type qrzSessionResponse struct {
	XMLName xml.Name `xml:"QRZDatabase"`
	Session struct {
		Key    string `xml:"Key"`
		Error  string `xml:"Error"`
		SubExp string `xml:"SubExp"`
		GMTime string `xml:"GMTime"`
	} `xml:"Session"`
}

// qrzCallsignResponse is the XML structure returned by a QRZ callsign lookup.
type qrzCallsignResponse struct {
	XMLName  xml.Name `xml:"QRZDatabase"`
	Callsign struct {
		Call    string  `xml:"call"`
		Aliases string  `xml:"aliases"`
		FName   string  `xml:"fname"`
		LName   string  `xml:"lname"`
		Addr1   string  `xml:"addr1"`
		Addr2   string  `xml:"addr2"`
		State   string  `xml:"state"`
		Zip     string  `xml:"zip"`
		Country string  `xml:"country"`
		Lat     float64 `xml:"lat"`
		Lon     float64 `xml:"lon"`
		Grid    string  `xml:"grid"`
		DXCC    int     `xml:"dxcc"`
		Land    string  `xml:"land"`
		CQZone  int     `xml:"cq_zone"`
		ITUZone int     `xml:"itu_zone"`
		// Note: QRZ XML uses CamelCase for these fields
		TimeZone string `xml:"TimeZone"`
		GMTOff   int    `xml:"GMTOff"`
		DST      string `xml:"DST"`
		QSLMgr   string `xml:"qslmgr"`
		Email    string `xml:"email"`
		URL      string `xml:"url"`
		Image    string `xml:"image"`
		Class    string `xml:"class"`
		Expires  string `xml:"expdate"`
	} `xml:"Callsign"`
	Session struct {
		Key   string `xml:"Key"`
		Error string `xml:"Error"`
	} `xml:"Session"`
}

// Client is a thread-safe QRZ XML API client.
// Construct via New; the zero value is not usable.
type Client struct {
	username string
	password string

	mu          sync.Mutex
	sessionKey  string
	lastRequest time.Time

	httpClient *http.Client
}

// New creates a QRZ XML API client for the given credentials.
// The client is thread-safe and should be reused across requests.
func New(username, password string) *Client {
	return &Client{
		username: username,
		password: password,
		httpClient: &http.Client{
			Timeout: requestTimeout,
		},
	}
}

// ErrNotFound is returned when QRZ has no record for the queried callsign.
var ErrNotFound = fmt.Errorf("qrz: callsign not found")

// ErrNotSubscribed is returned when the QRZ account does not have API access.
var ErrNotSubscribed = fmt.Errorf("qrz: account not subscribed for XML API access")

// errSessionTimeout is an internal sentinel triggering re-authentication.
var errSessionTimeout = fmt.Errorf("qrz: session timeout")

// LookupCallsign looks up a callsign on QRZ and returns structured info.
// It handles session management automatically:
//   - If no session key exists, it logs in first.
//   - If the session expires, it re-authenticates and retries once.
//
// Returns ErrNotFound if QRZ has no record for the callsign.
// Returns ErrNotSubscribed if the QRZ account lacks API subscription.
func (c *Client) LookupCallsign(ctx context.Context, callsign string) (*CallsignInfo, error) {
	callsign = strings.ToUpper(strings.TrimSpace(callsign))
	if callsign == "" {
		return nil, fmt.Errorf("qrz: callsign must not be empty")
	}

	if err := c.ensureSession(ctx); err != nil {
		return nil, fmt.Errorf("qrz: session setup: %w", err)
	}

	info, err := c.doLookup(ctx, callsign)
	if err == nil {
		return info, nil
	}

	// Session expired — re-authenticate once and retry.
	if err == errSessionTimeout {
		slog.DebugContext(ctx, "qrz: session expired, re-authenticating")
		c.mu.Lock()
		c.sessionKey = ""
		c.mu.Unlock()

		if authErr := c.ensureSession(ctx); authErr != nil {
			return nil, fmt.Errorf("qrz: re-auth after session expiry: %w", authErr)
		}
		return c.doLookup(ctx, callsign)
	}

	return nil, err
}

// ensureSession acquires a session key if one is not already cached.
func (c *Client) ensureSession(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.sessionKey != "" {
		return nil
	}
	return c.login(ctx)
}

// login performs the QRZ login flow. Must be called with c.mu held.
func (c *Client) login(ctx context.Context) error {
	c.throttle()

	params := url.Values{}
	params.Set("username", c.username)
	params.Set("password", c.password)
	params.Set("agent", agentString)

	reqURL := qrzXMLEndpoint + "?" + params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return fmt.Errorf("build login request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http GET login: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read login body: %w", err)
	}

	var parsed qrzSessionResponse
	if err := xml.Unmarshal(body, &parsed); err != nil {
		return fmt.Errorf("parse login XML: %w", err)
	}

	if parsed.Session.Error != "" {
		return fmt.Errorf("qrz login: %s", parsed.Session.Error)
	}
	if parsed.Session.Key == "" {
		return fmt.Errorf("qrz: login succeeded but returned empty session key")
	}

	c.sessionKey = parsed.Session.Key
	slog.Debug("qrz: session established", slog.String("sub_exp", parsed.Session.SubExp))
	return nil
}

// doLookup performs a callsign lookup using the current session key.
func (c *Client) doLookup(ctx context.Context, callsign string) (*CallsignInfo, error) {
	c.mu.Lock()
	sessionKey := c.sessionKey
	c.throttle()
	c.mu.Unlock()

	params := url.Values{}
	params.Set("s", sessionKey)
	params.Set("callsign", callsign)

	reqURL := qrzXMLEndpoint + "?" + params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build lookup request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http GET lookup: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read lookup body: %w", err)
	}

	var parsed qrzCallsignResponse
	if err := xml.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("parse lookup XML: %w", err)
	}

	// Check for session or lookup errors.
	if parsed.Session.Error != "" {
		lower := strings.ToLower(parsed.Session.Error)
		if strings.Contains(lower, "session timeout") ||
			strings.Contains(lower, "invalid session") ||
			strings.Contains(lower, "session not found") {
			return nil, errSessionTimeout
		}
		if strings.Contains(lower, "not found") {
			return nil, ErrNotFound
		}
		if strings.Contains(lower, "subscription") {
			return nil, ErrNotSubscribed
		}
		return nil, fmt.Errorf("qrz error: %s", parsed.Session.Error)
	}

	cs := parsed.Callsign
	if cs.Call == "" {
		return nil, ErrNotFound
	}

	info := &CallsignInfo{
		Callsign:  strings.ToUpper(cs.Call),
		Alias:     cs.Aliases,
		FName:     cs.FName,
		LName:     cs.LName,
		Addr1:     cs.Addr1,
		Addr2:     cs.Addr2,
		State:     cs.State,
		Zip:       cs.Zip,
		Country:   cs.Country,
		Lat:       cs.Lat,
		Lon:       cs.Lon,
		Grid:      cs.Grid,
		DXCC:      cs.DXCC,
		Land:      cs.Land,
		CQZone:    cs.CQZone,
		ITUZone:   cs.ITUZone,
		TimeZone:  cs.TimeZone,
		GMTOffset: cs.GMTOff,
		DST:       cs.DST,
		QSLMgr:    cs.QSLMgr,
		Email:     cs.Email,
		URL:       cs.URL,
		Image:     cs.Image,
		Class:     cs.Class,
		Expires:   cs.Expires,
	}

	// Build human-friendly full name.
	parts := make([]string, 0, 2)
	if cs.FName != "" {
		parts = append(parts, cs.FName)
	}
	if cs.LName != "" {
		parts = append(parts, cs.LName)
	}
	info.FullName = strings.Join(parts, " ")

	return info, nil
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
