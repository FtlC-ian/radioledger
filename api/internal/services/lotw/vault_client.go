// Package lotw provides a client for the lotw-vault microservice and the ARRL
// LoTW upload endpoint.
//
// The vault is a security boundary: it holds encrypted private keys and is the
// only service that ever touches plaintext key material. This client talks to the
// vault over the Docker-internal vault-internal network and never exposes
// credentials beyond in-memory use.
//
// References:
//   - lotw-vault/README.md     — vault API spec
//   - docs/LOTW_INTEGRATION_PLAN.md — architecture
package lotw

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"
)

// Sentinel errors mapped from vault HTTP status codes.
// Callers use errors.Is to distinguish error categories.
var (
	// ErrInvalidCert is returned when the vault rejects the .p12 as malformed (HTTP 400).
	ErrInvalidCert = errors.New("invalid certificate")
	// ErrWrongPassword is returned when the vault rejects the supplied password (HTTP 401).
	ErrWrongPassword = errors.New("wrong password")
	// ErrNoCert is returned when no certificate is stored for the user (HTTP 404).
	ErrNoCert = errors.New("no certificate found")
	// ErrCertAlreadyExists is returned when a cert is already stored for the user (HTTP 409).
	ErrCertAlreadyExists = errors.New("certificate already exists")
	// ErrVaultInternal is returned for unexpected vault errors (HTTP 5xx).
	ErrVaultInternal = errors.New("vault internal error")
)

// CertInfo holds public certificate metadata returned by the vault.
// The vault never returns private key material.
type CertInfo struct {
	UserID        string `json:"user_id"`
	Callsign      string `json:"callsign"`
	DXCC          string `json:"dxcc"`
	Gridsquare    string `json:"gridsquare"`
	CQZ           string `json:"cqz"`
	ITUZ          string `json:"ituz"`
	QSOStart      string `json:"qso_start"`
	QSOEnd        string `json:"qso_end"`
	CertNotBefore string `json:"cert_not_before"`
	CertNotAfter  string `json:"cert_not_after"`
	Expired       bool   `json:"expired"`
}

// StationInfo is the per-QSL station metadata embedded in the vault /sign request.
// Values typically come from the imported certificate via GetCertInfo.
type StationInfo struct {
	Callsign   string `json:"callsign"`
	DXCC       string `json:"dxcc"`
	Gridsquare string `json:"gridsquare"`
	CQZ        string `json:"cqz"`
	ITUZ       string `json:"ituz"`
}

// VaultClient is an HTTP client for the lotw-vault microservice.
// It is safe for concurrent use.
type VaultClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewVaultClient creates a VaultClient that calls the vault at baseURL.
// baseURL should be the full base URL, e.g. "http://lotw-vault:8081".
func NewVaultClient(baseURL string) *VaultClient {
	return &VaultClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// ImportCert uploads a .p12 certificate to the vault.
// The vault encrypts the private key under userPassword using Argon2id + AES-256-GCM.
// Returns the public certificate metadata on success.
// Returns ErrCertAlreadyExists if a cert is already registered for userID.
// Returns ErrInvalidCert if the .p12 is malformed or p12Password is wrong.
func (c *VaultClient) ImportCert(ctx context.Context, userID string, p12Data []byte, p12Password, userPassword string) (*CertInfo, error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if err := mw.WriteField("user_id", userID); err != nil {
		return nil, fmt.Errorf("write user_id field: %w", err)
	}
	if err := mw.WriteField("p12_password", p12Password); err != nil {
		return nil, fmt.Errorf("write p12_password field: %w", err)
	}
	if err := mw.WriteField("user_password", userPassword); err != nil {
		return nil, fmt.Errorf("write user_password field: %w", err)
	}
	fw, err := mw.CreateFormFile("p12_file", "cert.p12")
	if err != nil {
		return nil, fmt.Errorf("create form file: %w", err)
	}
	if _, err := fw.Write(p12Data); err != nil {
		return nil, fmt.Errorf("write p12 data: %w", err)
	}
	if err := mw.Close(); err != nil {
		return nil, fmt.Errorf("close multipart writer: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/import-cert", &buf)
	if err != nil {
		return nil, fmt.Errorf("build import-cert request: %w", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("vault import-cert: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if err := checkVaultStatus(resp); err != nil {
		return nil, err
	}

	var info CertInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("decode cert info response: %w", err)
	}
	return &info, nil
}

// GetCertInfo returns public certificate metadata for userID.
// No password is required — the vault never returns private key material.
// Returns ErrNoCert if no certificate is registered for userID.
func (c *VaultClient) GetCertInfo(ctx context.Context, userID string) (*CertInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.baseURL+"/cert-info?user_id="+userID, nil)
	if err != nil {
		return nil, fmt.Errorf("build cert-info request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("vault cert-info: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if err := checkVaultStatus(resp); err != nil {
		return nil, err
	}

	var info CertInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("decode cert info response: %w", err)
	}
	return &info, nil
}

// DeleteCert removes a stored certificate from the vault.
// The user's password is verified before deletion.
// Returns ErrNoCert if no certificate is registered.
// Returns ErrWrongPassword if password verification fails.
func (c *VaultClient) DeleteCert(ctx context.Context, userID string, password string) error {
	body, err := json.Marshal(map[string]string{
		"user_id":       userID,
		"user_password": password,
	})
	if err != nil {
		return fmt.Errorf("marshal delete request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.baseURL+"/cert", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build delete request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("vault delete cert: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	return checkVaultStatus(resp)
}

// RotatePassword re-encrypts the stored private key under a new password.
// A new Argon2 salt is generated; the old password is verified before any changes.
// Returns ErrWrongPassword if oldPassword is incorrect.
func (c *VaultClient) RotatePassword(ctx context.Context, userID string, oldPassword, newPassword string) error {
	body, err := json.Marshal(map[string]string{
		"user_id":      userID,
		"old_password": oldPassword,
		"new_password": newPassword,
	})
	if err != nil {
		return fmt.Errorf("marshal rotate-password request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/rotate-password", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build rotate-password request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("vault rotate-password: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	return checkVaultStatus(resp)
}

// signRequest is the JSON body for the vault /sign endpoint.
type signRequest struct {
	UserID       string      `json:"user_id"`
	UserPassword string      `json:"user_password"`
	ADIFData     string      `json:"adif_data"`
	Station      StationInfo `json:"station"`
}

// Sign sends ADIF data to the vault for signing, returning a .tq8 binary blob.
// The .tq8 is a gzip-compressed ADIF file with ARRL-compatible digital signatures.
// Returns ErrNoCert if no certificate is registered.
// Returns ErrWrongPassword if the password is incorrect.
func (c *VaultClient) Sign(ctx context.Context, userID string, password string, adifData string, station StationInfo) ([]byte, error) {
	payload := signRequest{
		UserID:       userID,
		UserPassword: password,
		ADIFData:     adifData,
		Station:      station,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal sign request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/sign", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build sign request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("vault sign: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if err := checkVaultStatus(resp); err != nil {
		return nil, err
	}

	const maxTQ8 = 10 * 1024 * 1024 // 10 MB should cover any realistic upload
	tq8Data, err := io.ReadAll(io.LimitReader(resp.Body, maxTQ8))
	if err != nil {
		return nil, fmt.Errorf("read tq8 response body: %w", err)
	}
	if len(tq8Data) == 0 {
		return nil, fmt.Errorf("vault returned empty tq8 response")
	}
	return tq8Data, nil
}

// checkVaultStatus maps vault HTTP status codes to typed sentinel errors.
// Returns nil for 2xx responses. The response body is consumed and attached to the error.
func checkVaultStatus(resp *http.Response) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	msg := strings.TrimSpace(string(body))
	switch resp.StatusCode {
	case http.StatusBadRequest:
		return fmt.Errorf("%w: %s", ErrInvalidCert, msg)
	case http.StatusUnauthorized:
		return fmt.Errorf("%w: %s", ErrWrongPassword, msg)
	case http.StatusNotFound:
		return fmt.Errorf("%w: %s", ErrNoCert, msg)
	case http.StatusConflict:
		return fmt.Errorf("%w: %s", ErrCertAlreadyExists, msg)
	default:
		return fmt.Errorf("%w: HTTP %d: %s", ErrVaultInternal, resp.StatusCode, msg)
	}
}
