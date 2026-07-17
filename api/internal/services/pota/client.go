package pota

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	defaultAPIBase = "https://api.pota.app"
	uploadPath     = "/log/"
)

// Credentials supports either a direct JWT token or username/password.
// If JWT is empty and Username/Password are set, the client attempts a token exchange.
type Credentials struct {
	JWT         string `json:"jwt,omitempty"`
	AccessToken string `json:"access_token,omitempty"`
	Token       string `json:"token,omitempty"`
	Username    string `json:"username,omitempty"`
	Password    string `json:"password,omitempty"`
}

func (c *Credentials) bearerToken() string {
	if c == nil {
		return ""
	}
	if strings.TrimSpace(c.JWT) != "" {
		return strings.TrimSpace(c.JWT)
	}
	if strings.TrimSpace(c.AccessToken) != "" {
		return strings.TrimSpace(c.AccessToken)
	}
	return strings.TrimSpace(c.Token)
}

func (c *Credentials) hasUsernamePassword() bool {
	return strings.TrimSpace(c.Username) != "" && strings.TrimSpace(c.Password) != ""
}

// DecodeCredentials accepts JSON credentials, username:password, or raw JWT.
func DecodeCredentials(plaintext []byte) (*Credentials, error) {
	raw := strings.TrimSpace(string(plaintext))
	if raw == "" {
		return nil, fmt.Errorf("pota: credentials are empty")
	}

	// JSON is preferred because it can carry either token or username/password.
	if strings.HasPrefix(raw, "{") {
		var creds Credentials
		if err := json.Unmarshal([]byte(raw), &creds); err != nil {
			return nil, fmt.Errorf("pota: decode credentials: %w", err)
		}
		if creds.bearerToken() == "" && !creds.hasUsernamePassword() {
			return nil, fmt.Errorf("pota: credentials must include jwt/access_token/token or username/password")
		}
		return &creds, nil
	}

	if parts := strings.SplitN(raw, ":", 2); len(parts) == 2 && strings.TrimSpace(parts[0]) != "" && strings.TrimSpace(parts[1]) != "" {
		return &Credentials{Username: strings.TrimSpace(parts[0]), Password: strings.TrimSpace(parts[1])}, nil
	}

	// Treat any other non-empty plaintext as a bearer token.
	return &Credentials{JWT: raw}, nil
}

// EncodeTokenCredentials stores a token-oriented credential payload.
func EncodeTokenCredentials(token string) ([]byte, error) {
	return json.Marshal(Credentials{JWT: strings.TrimSpace(token)})
}

// EncodeUsernamePasswordCredentials stores username/password credentials.
func EncodeUsernamePasswordCredentials(username, password string) ([]byte, error) {
	return json.Marshal(Credentials{Username: strings.TrimSpace(username), Password: strings.TrimSpace(password)})
}

type UploadResult struct {
	StatusCode int
	Response   string
}

type authResponse struct {
	JWT         string `json:"jwt"`
	AccessToken string `json:"access_token"`
	Token       string `json:"token"`
}

type ClientConfig struct {
	APIBaseURL string
	AuthURL    string
}

type Client struct {
	apiBase    string
	authURL    string
	httpClient *http.Client
}

func New() *Client {
	return NewWithConfig(ClientConfig{})
}

func NewWithConfig(cfg ClientConfig) *Client {
	base := strings.TrimRight(strings.TrimSpace(cfg.APIBaseURL), "/")
	if base == "" {
		base = defaultAPIBase
	}
	return &Client{
		apiBase: base,
		authURL: strings.TrimSpace(cfg.AuthURL),
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (c *Client) resolveToken(ctx context.Context, creds *Credentials) (string, error) {
	if tok := creds.bearerToken(); tok != "" {
		return tok, nil
	}
	if !creds.hasUsernamePassword() {
		return "", fmt.Errorf("pota: no JWT token configured; save a POTA token in settings")
	}

	authURL := strings.TrimSpace(c.authURL)
	if authURL == "" {
		return "", fmt.Errorf("pota: username/password provided but POTA_AUTH_URL is not configured; save a JWT token instead")
	}

	payload, _ := json.Marshal(map[string]string{
		"username": creds.Username,
		"password": creds.Password,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, authURL, bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("pota auth: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "RadioLedger/1.0")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("pota auth: request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 16*1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("pota auth failed (HTTP %d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var parsed authResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("pota auth: parse response: %w", err)
	}
	if tok := strings.TrimSpace(parsed.JWT); tok != "" {
		return tok, nil
	}
	if tok := strings.TrimSpace(parsed.AccessToken); tok != "" {
		return tok, nil
	}
	if tok := strings.TrimSpace(parsed.Token); tok != "" {
		return tok, nil
	}
	return "", fmt.Errorf("pota auth: response missing token")
}

func (c *Client) UploadADIF(ctx context.Context, creds *Credentials, adif string) (*UploadResult, error) {
	token, err := c.resolveToken(ctx, creds)
	if err != nil {
		return nil, err
	}

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreateFormFile("file", "radioledger.adi")
	if err != nil {
		return nil, fmt.Errorf("pota upload: form file: %w", err)
	}
	if _, err := io.WriteString(part, adif); err != nil {
		return nil, fmt.Errorf("pota upload: write adif: %w", err)
	}
	_ = mw.WriteField("name", "radioledger")
	if err := mw.Close(); err != nil {
		return nil, fmt.Errorf("pota upload: close multipart: %w", err)
	}

	u, err := url.JoinPath(c.apiBase, uploadPath)
	if err != nil {
		u = c.apiBase + uploadPath
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, &body)
	if err != nil {
		return nil, fmt.Errorf("pota upload: build request: %w", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", "RadioLedger/1.0")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("pota upload: request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	bodyStr := strings.TrimSpace(string(respBody))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("pota upload failed (HTTP %d): %s", resp.StatusCode, bodyStr)
	}

	return &UploadResult{StatusCode: resp.StatusCode, Response: bodyStr}, nil
}
