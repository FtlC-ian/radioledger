// Package handler provides HTTP handlers for the RadioLedger API.
// This file implements encrypted credential storage and retrieval.
package handler

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/FtlC-ian/radioledger/api/internal/crypto"
	"github.com/FtlC-ian/radioledger/api/internal/middleware"
	"github.com/FtlC-ian/radioledger/api/internal/securitylog"
	syncsvc "github.com/FtlC-ian/radioledger/api/internal/services/sync"
)

var validServices = map[string]bool{
	"qrz":     true,
	"eqsl":    true,
	"clublog": true,
	"hamqth":  true,
	"pota":    true,
	"sota":    true,
}

var validCredentialTypes = map[string]bool{
	"api_key":           true,
	"username_password": true,
	"session":           true,
	"oauth_token":       true,
}

// CredentialHandler handles encrypted credential management endpoints.
type CredentialHandler struct {
	store syncsvc.CredentialStore
}

// NewCredentialHandler creates a CredentialHandler.
func NewCredentialHandler(pool *pgxpool.Pool, keyring *crypto.Keyring) *CredentialHandler {
	var store syncsvc.CredentialStore
	if keyring != nil {
		store = syncsvc.NewPostgresStore(pool, keyring)
	}
	return &CredentialHandler{store: store}
}

type credentialRequest struct {
	Service        string     `json:"service"`
	CredentialType string     `json:"credential_type"`
	Value          string     `json:"value"`
	ExpiresAt      *time.Time `json:"expires_at,omitempty"`
}

type credentialResponse struct {
	Service        string     `json:"service"`
	CredentialType string     `json:"credential_type"`
	KeyVersion     int32      `json:"key_version"`
	IsActive       bool       `json:"is_active"`
	ExpiresAt      *time.Time `json:"expires_at,omitempty"`
	LastUsedAt     *time.Time `json:"last_used_at,omitempty"`
	LastVerifiedAt *time.Time `json:"last_verified_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// Store handles POST /v1/credentials.
// Encrypts the value and upserts the row. NEVER logs the plaintext.
func (h *CredentialHandler) Store(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	if h.store == nil {
		writeFailure(w, http.StatusServiceUnavailable, "encryption not configured",
			"RADIOLEDGER_MASTER_KEY is not set")
		return
	}
	var req credentialRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}
	req.Service = strings.ToLower(strings.TrimSpace(req.Service))
	req.CredentialType = strings.ToLower(strings.TrimSpace(req.CredentialType))
	if !validServices[req.Service] {
		writeFailure(w, http.StatusBadRequest, "invalid service",
			"service must be one of: qrz, eqsl, clublog, hamqth, pota")
		return
	}
	if !validCredentialTypes[req.CredentialType] {
		writeFailure(w, http.StatusBadRequest, "invalid credential_type",
			"must be one of: api_key, username_password, session, oauth_token")
		return
	}
	normalizedValue, err := normalizeCredentialValue(req.Service, req.CredentialType, req.Value)
	if err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid value", err.Error())
		return
	}

	plaintext := []byte(normalizedValue)

	stored, err := h.store.Save(r.Context(), syncsvc.StoreParams{
		UserID:         userID,
		Service:        req.Service,
		CredentialType: req.CredentialType,
		Plaintext:      plaintext,
		ExpiresAt:      req.ExpiresAt,
	})
	if err != nil {
		slog.ErrorContext(r.Context(), "credentials: save failed",
			slog.String("service", req.Service), slog.String("error", err.Error()))
		writeFailure(w, http.StatusInternalServerError, "store failed", "internal error")
		return
	}
	resp := credentialResponse{
		Service:        stored.Service,
		CredentialType: stored.CredentialType,
		KeyVersion:     stored.KeyVersion,
		IsActive:       stored.IsActive,
		LastVerifiedAt: stored.LastVerifiedAt,
		CreatedAt:      stored.CreatedAt,
		UpdatedAt:      stored.UpdatedAt,
	}

	if req.CredentialType == "username_password" {
		securitylog.Event(r.Context(), "password_change",
			slog.Int64("user_id", userID),
			slog.String("service", req.Service),
			slog.String("remote_ip", middleware.ClientIP(r)),
		)
	}

	writeSuccess(w, http.StatusOK, "credential stored", resp)
}

// List handles GET /v1/credentials.
func (h *CredentialHandler) List(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	if h.store == nil {
		writeFailure(w, http.StatusServiceUnavailable, "encryption not configured",
			"RADIOLEDGER_MASTER_KEY is not set")
		return
	}

	summaries, err := h.store.List(r.Context(), userID)
	if err != nil {
		slog.ErrorContext(r.Context(), "credentials: list failed", slog.String("error", err.Error()))
		writeFailure(w, http.StatusInternalServerError, "database error", "internal error")
		return
	}
	items := make([]credentialResponse, 0, len(summaries))
	for _, row := range summaries {
		cr := credentialResponse{
			Service:        row.Service,
			CredentialType: row.CredentialType,
			KeyVersion:     row.KeyVersion,
			IsActive:       row.IsActive,
			LastUsedAt:     row.LastUsedAt,
			LastVerifiedAt: row.LastVerifiedAt,
			CreatedAt:      row.CreatedAt,
			UpdatedAt:      row.UpdatedAt,
		}
		items = append(items, cr)
	}
	writeSuccess(w, http.StatusOK, "credentials retrieved", map[string]any{
		"items": items,
		"count": len(items),
	})
}

// Delete handles DELETE /v1/credentials/{service}.
func (h *CredentialHandler) Delete(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	service := strings.ToLower(strings.TrimSpace(chi.URLParam(r, "service")))
	if !validServices[service] {
		writeFailure(w, http.StatusBadRequest, "invalid service",
			"service must be one of: qrz, eqsl, clublog, hamqth, pota")
		return
	}
	if h.store == nil {
		writeFailure(w, http.StatusServiceUnavailable, "encryption not configured",
			"RADIOLEDGER_MASTER_KEY is not set")
		return
	}

	deleted, err := h.store.Delete(r.Context(), userID, service)
	if err != nil {
		slog.ErrorContext(r.Context(), "credentials: delete failed",
			slog.String("service", service), slog.String("error", err.Error()))
		writeFailure(w, http.StatusInternalServerError, "database error", "internal error")
		return
	}
	if !deleted {
		writeFailure(w, http.StatusNotFound, "not found", "no credential found for that service")
		return
	}
	writeSuccess(w, http.StatusOK, "credential deleted", nil)
}
