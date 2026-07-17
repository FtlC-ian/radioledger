package handler

// Package handler — desktop client integration endpoints.
//
// These endpoints accept push data from the RadioLedger desktop (Tauri) client.
// The security model is intentional: sensitive artifacts (private keys, raw cert
// files) stay on the operator's machine; only derived metadata is sent here.
//
// Endpoints:
//   POST /v1/desktop/cert-expiry  — Push LoTW certificate expiry date from desktop

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	db "github.com/FtlC-ian/radioledger/api/internal/database/sqlc"
)

// DesktopHandler handles endpoints that accept push data from the desktop client.
type DesktopHandler struct {
	pool *pgxpool.Pool
}

// NewDesktopHandler creates a DesktopHandler with its database pool dependency.
func NewDesktopHandler(pool *pgxpool.Pool) *DesktopHandler {
	return &DesktopHandler{pool: pool}
}

// certExpiryRequest is the JSON body accepted by CertExpiry.
// The desktop client reads the tQSL certificate files locally and pushes
// only the expiry DATE — no private key material is ever transmitted.
type certExpiryRequest struct {
	// StationCallsign is the callsign the tQSL certificate is issued to.
	// Stored uppercase; matched against station_locations.callsign.
	StationCallsign string `json:"station_callsign"`

	// LocationName is the tQSL "station location" label (e.g. "Home Station").
	// Optional: if empty the update applies to all locations for the callsign.
	LocationName string `json:"location_name,omitempty"`

	// ExpiresAt is the certificate expiry date in ISO 8601 format (YYYY-MM-DD).
	ExpiresAt string `json:"expires_at"`
}

// certExpiryResponse is returned on success.
type certExpiryResponse struct {
	Callsign       string `json:"callsign"`
	ExpiresAt      string `json:"expires_at"`
	LocationsFound int64  `json:"locations_found"`
}

// CertExpiry handles POST /v1/desktop/cert-expiry.
//
// Accepts a certificate expiry date from the desktop client and stores it on
// the matching station location(s). The desktop identifies the cert by callsign
// (and optionally location name); the server updates lotw_cert_expiry in
// station_locations so the daily CertExpiryCheckJob can fire timely alerts.
//
// The update is scoped to the authenticated user via RLS — a user can only
// update their own station locations.
func (h *DesktopHandler) CertExpiry(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	var req certExpiryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", "request body must be valid JSON")
		return
	}

	req.StationCallsign = strings.TrimSpace(strings.ToUpper(req.StationCallsign))
	if req.StationCallsign == "" {
		writeFailure(w, http.StatusUnprocessableEntity, "validation failed", "station_callsign is required")
		return
	}

	expiresAt := strings.TrimSpace(req.ExpiresAt)
	if expiresAt == "" {
		writeFailure(w, http.StatusUnprocessableEntity, "validation failed", "expires_at is required")
		return
	}
	expiry, err := time.Parse("2006-01-02", expiresAt)
	if err != nil {
		writeFailure(w, http.StatusUnprocessableEntity, "validation failed",
			"expires_at must be in YYYY-MM-DD format")
		return
	}

	tx, err := beginTenantTx(r.Context(), h.pool, userID)
	if err != nil {
		writeFailure(w, http.StatusServiceUnavailable, "database unavailable", "failed to prepare tenant context")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	queries := db.New(tx)

	certExpiryDate := pgtype.Date{Time: expiry, Valid: true}

	updated, err := queries.UpdateCertExpiryByCallsign(r.Context(), db.UpdateCertExpiryByCallsignParams{
		LotwCertExpiry: certExpiryDate,
		UserID:         userID,
		Callsign:       req.StationCallsign,
	})
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "database error", "failed to update cert expiry")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "database error", "failed to commit update")
		return
	}

	writeSuccess(w, http.StatusOK, "cert expiry updated", certExpiryResponse{
		Callsign:       req.StationCallsign,
		ExpiresAt:      expiresAt,
		LocationsFound: updated,
	})
}
