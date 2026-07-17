// Package handler — sync_credentials.go implements HTTP handlers for the
// credential management endpoints under /v1/sync/credentials/.
//
// These endpoints replace the older /v1/credentials/* routes for credential
// management. Key differences:
//   - PUT /v1/sync/credentials/:service   — store + verify before saving
//   - GET /v1/sync/credentials            — list configured services (no secrets)
//   - POST /v1/sync/credentials/:service/verify — re-verify existing credentials
//   - DELETE /v1/sync/credentials/:service — remove credentials
//
// All responses use the JSend-style envelope (success/message/data).
// Plaintext credentials are NEVER returned in any response.
package handler

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/FtlC-ian/radioledger/api/internal/crypto"
	"github.com/FtlC-ian/radioledger/api/internal/middleware"
	"github.com/FtlC-ian/radioledger/api/internal/securitylog"
	"github.com/FtlC-ian/radioledger/api/internal/services/eqsl"
	syncsvc "github.com/FtlC-ian/radioledger/api/internal/services/sync"
)

// SyncCredentialHandler handles encrypted credential management for sync services.
// It delegates storage and verification to a CredentialStore.
type SyncCredentialHandler struct {
	store syncsvc.CredentialStore
	pool  *pgxpool.Pool
}

// NewSyncCredentialHandler creates a SyncCredentialHandler backed by PostgresStore.
func NewSyncCredentialHandler(pool *pgxpool.Pool, keyring *crypto.Keyring) *SyncCredentialHandler {
	store := syncsvc.NewPostgresStore(pool, keyring)
	return &SyncCredentialHandler{store: store, pool: pool}
}

// syncCredentialRequest is the request body for PUT /v1/sync/credentials/:service.
type syncCredentialRequest struct {
	CredentialType string     `json:"credential_type"`
	Value          string     `json:"value"`
	ExpiresAt      *time.Time `json:"expires_at,omitempty"`
}

// syncCredentialSummaryResponse is one item in the GET list response.
type syncCredentialSummaryResponse struct {
	Service        string     `json:"service"`
	CredentialType string     `json:"credential_type"`
	IsActive       bool       `json:"is_active"`
	Verified       bool       `json:"verified"`
	LastVerifiedAt *time.Time `json:"last_verified_at,omitempty"`
	LastUsedAt     *time.Time `json:"last_used_at,omitempty"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// StoreAndVerify handles PUT /v1/sync/credentials/{service}.
//
// Validates, stores, and attempts verification against the external service.
// Verification failures do not block save; the credential is persisted as
// unverified (last_verified_at = NULL).
// NEVER logs or returns the plaintext value.
func (h *SyncCredentialHandler) StoreAndVerify(w http.ResponseWriter, r *http.Request) {
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

	service := strings.ToLower(strings.TrimSpace(chi.URLParam(r, "service")))
	if !validServices[service] {
		writeFailure(w, http.StatusBadRequest, "invalid service",
			"service must be one of: qrz, eqsl, clublog, hamqth, pota, sota")
		return
	}

	var req syncCredentialRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}

	req.CredentialType = strings.ToLower(strings.TrimSpace(req.CredentialType))
	if !validCredentialTypes[req.CredentialType] {
		writeFailure(w, http.StatusBadRequest, "invalid credential_type",
			"must be one of: api_key, username_password, session, oauth_token")
		return
	}

	normalizedValue, err := normalizeCredentialValue(service, req.CredentialType, req.Value)
	if err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid value", err.Error())
		return
	}

	stored, err := h.store.Save(r.Context(), syncsvc.StoreParams{
		UserID:         userID,
		Service:        service,
		CredentialType: req.CredentialType,
		Plaintext:      []byte(normalizedValue),
		ExpiresAt:      req.ExpiresAt,
	})
	if err != nil {
		slog.ErrorContext(r.Context(), "sync_credentials: store failed",
			slog.String("service", service), slog.String("error", err.Error()))
		writeFailure(w, http.StatusInternalServerError, "store failed", "internal error")
		return
	}

	if req.CredentialType == "username_password" {
		securitylog.Event(r.Context(), "password_change",
			slog.Int64("user_id", userID),
			slog.String("service", service),
			slog.String("remote_ip", middleware.ClientIP(r)),
		)
	}

	// Backfill sync_status rows for existing QSOs so the dashboard shows accurate counts
	// immediately. Run in a background goroutine to avoid blocking the HTTP response.
	// Use context.WithoutCancel so the goroutine outlives the request while preserving
	// tracing/logging values; add a timeout so it doesn't run indefinitely.
	if h.pool != nil {
		go func(uid int64, svc string) {
			ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Minute)
			defer cancel()
			log := slog.With(slog.Int64("user_id", uid), slog.String("service", svc))
			counts, err := syncsvc.BackfillSyncStatusForService(ctx, h.pool, uid, svc)
			if err != nil {
				log.ErrorContext(ctx, "sync_credentials: backfill failed", slog.String("error", err.Error()))
				return
			}
			log.InfoContext(ctx, "sync_credentials: backfill complete",
				slog.Int64("confirmed", counts.Confirmed),
				slog.Int64("pending", counts.Pending),
			)
		}(userID, service)
	}

	resp := map[string]any{
		"service":            stored.Service,
		"credential_type":    stored.CredentialType,
		"key_version":        stored.KeyVersion,
		"is_active":          stored.IsActive,
		"verified":           stored.Verified,
		"verification_error": stored.VerificationError,
		"last_verified_at":   stored.LastVerifiedAt,
		"updated_at":         stored.UpdatedAt,
	}
	// Attach the eQSL password security warning so the UI can present it
	// immediately after the user saves their credentials.
	if service == "eqsl" {
		resp["warning"] = eqsl.PasswordWarning
	}

	message := "credential stored and verified"
	if !stored.Verified {
		message = "credential stored but not verified"
	}
	writeSuccess(w, http.StatusOK, message, resp)
}

// List handles GET /v1/sync/credentials.
//
// Returns metadata for all of the user's configured services.
// Plaintext credentials are NEVER included in the response.
func (h *SyncCredentialHandler) List(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	summaries, err := h.store.List(r.Context(), userID)
	if err != nil {
		slog.ErrorContext(r.Context(), "sync_credentials: list failed",
			slog.String("error", err.Error()))
		writeFailure(w, http.StatusInternalServerError, "list failed", "internal error")
		return
	}

	items := make([]syncCredentialSummaryResponse, 0, len(summaries))
	for _, s := range summaries {
		items = append(items, syncCredentialSummaryResponse{
			Service:        s.Service,
			CredentialType: s.CredentialType,
			IsActive:       s.IsActive,
			Verified:       s.LastVerifiedAt != nil,
			LastVerifiedAt: s.LastVerifiedAt,
			LastUsedAt:     s.LastUsedAt,
			UpdatedAt:      s.UpdatedAt,
		})
	}

	// Include per-service security warnings in the response so the UI can
	// display them without hardcoding them on the frontend.
	serviceWarnings := map[string]string{
		"eqsl": eqsl.PasswordWarning,
	}

	writeSuccess(w, http.StatusOK, "credentials listed", map[string]any{
		"items":            items,
		"count":            len(items),
		"service_warnings": serviceWarnings,
	})
}

// ReVerify handles POST /v1/sync/credentials/{service}/verify.
//
// Re-tests already-stored credentials against the external service.
// Updates last_verified_at on success.
func (h *SyncCredentialHandler) ReVerify(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	service := strings.ToLower(strings.TrimSpace(chi.URLParam(r, "service")))
	if !validServices[service] {
		writeFailure(w, http.StatusBadRequest, "invalid service",
			"service must be one of: qrz, eqsl, clublog, hamqth, pota, sota")
		return
	}

	if err := h.store.Verify(r.Context(), userID, service); err != nil {
		if strings.Contains(err.Error(), "no credentials") {
			writeFailure(w, http.StatusNotFound, "not found", "no credentials configured for "+service)
			return
		}
		if strings.Contains(err.Error(), "authentication failed") ||
			strings.Contains(err.Error(), "credential verification failed") {
			writeFailure(w, http.StatusUnprocessableEntity, "verification failed", err.Error())
			return
		}
		slog.ErrorContext(r.Context(), "sync_credentials: verify failed",
			slog.String("service", service), slog.String("error", err.Error()))
		writeFailure(w, http.StatusInternalServerError, "verification failed", "internal error")
		return
	}

	now := time.Now().UTC()
	writeSuccess(w, http.StatusOK, "credential verified", map[string]any{
		"service":          service,
		"last_verified_at": now,
	})
}

// Delete handles DELETE /v1/sync/credentials/{service}.
func (h *SyncCredentialHandler) Delete(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	service := strings.ToLower(strings.TrimSpace(chi.URLParam(r, "service")))
	if !validServices[service] {
		writeFailure(w, http.StatusBadRequest, "invalid service",
			"service must be one of: qrz, eqsl, clublog, hamqth, pota, sota")
		return
	}

	found, err := h.store.Delete(r.Context(), userID, service)
	if err != nil {
		slog.ErrorContext(r.Context(), "sync_credentials: delete failed",
			slog.String("service", service), slog.String("error", err.Error()))
		writeFailure(w, http.StatusInternalServerError, "delete failed", "internal error")
		return
	}
	if !found {
		writeFailure(w, http.StatusNotFound, "not found", "no credentials configured for "+service)
		return
	}

	writeSuccess(w, http.StatusOK, "credential deleted", nil)
}
