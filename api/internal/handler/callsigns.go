package handler

// Package handler — callsign management endpoints.
//
// RadioLedger models operator identity and station callsigns as separate concepts:
//
//   - user_callsigns: personal callsign history for a user (home call, vanity call,
//     historical/expired calls). One callsign is marked is_primary.
//
//   - station_callsigns: station identities used for logging (personal, club, special
//     event, contest, guest). Supports the M:N operator model where multiple operators
//     can use one club callsign, and one operator can log under many callsigns.
//
// Endpoints:
//   POST   /v1/callsigns           — Register a user callsign
//   GET    /v1/callsigns           — List user's callsigns
//   PUT    /v1/callsigns/{uuid}    — Update callsign metadata
//   DELETE /v1/callsigns/{uuid}    — Remove callsign
//   POST   /v1/station-callsigns        — Register a station callsign
//   GET    /v1/station-callsigns        — List station callsigns
//   PUT    /v1/station-callsigns/{uuid} — Update station callsign
//   DELETE /v1/station-callsigns/{uuid} — Deactivate station callsign

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	db "github.com/FtlC-ian/radioledger/api/internal/database/sqlc"
)

// CallsignHandler handles callsign management endpoints.
type CallsignHandler struct {
	pool *pgxpool.Pool
}

// NewCallsignHandler creates a CallsignHandler with its database pool dependency.
func NewCallsignHandler(pool *pgxpool.Pool) *CallsignHandler {
	return &CallsignHandler{pool: pool}
}

// ─────────────────────────────────────────────────────────────────────────────
// user_callsigns — request/response types
// ─────────────────────────────────────────────────────────────────────────────

// userCallsignRequest is the JSON body for creating a user callsign.
type userCallsignRequest struct {
	// Callsign is stored uppercase. E.g. "W5ABC".
	Callsign string `json:"callsign"`
	// LicenseClass is the amateur license class. One of: novice, technician,
	// general, advanced, extra, other.
	LicenseClass *string `json:"license_class,omitempty"`
	// Country is the issuing country for this callsign.
	Country *string `json:"country,omitempty"`
	// DXCCEntity is the ARRL DXCC entity number for this callsign.
	DXCCEntity *int32 `json:"dxcc_entity,omitempty"`
	// IsPrimary marks this as the primary callsign used for new QSOs.
	// Only one callsign per user can be primary at a time.
	IsPrimary bool `json:"is_primary"`
	// ValidFrom and ValidTo are the validity period in YYYY-MM-DD format.
	// ValidTo omitted (or null) means currently active.
	ValidFrom *string `json:"valid_from,omitempty"`
	ValidTo   *string `json:"valid_to,omitempty"`
}

// userCallsignUpdateRequest is the JSON body for updating a user callsign.
// Callsign text is immutable; only metadata can be changed.
type userCallsignUpdateRequest struct {
	IsPrimary    bool    `json:"is_primary"`
	LicenseClass *string `json:"license_class,omitempty"`
	Country      *string `json:"country,omitempty"`
	DXCCEntity   *int32  `json:"dxcc_entity,omitempty"`
	ValidFrom    *string `json:"valid_from,omitempty"`
	ValidTo      *string `json:"valid_to,omitempty"`
}

// userCallsignResponse is the outward-facing representation of a user callsign.
type userCallsignResponse struct {
	UUID         string    `json:"uuid"`
	Callsign     string    `json:"callsign"`
	LicenseClass *string   `json:"license_class,omitempty"`
	Country      *string   `json:"country,omitempty"`
	DXCCEntity   *int32    `json:"dxcc_entity,omitempty"`
	IsPrimary    bool      `json:"is_primary"`
	ValidFrom    *string   `json:"valid_from,omitempty"`
	ValidTo      *string   `json:"valid_to,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

// ─────────────────────────────────────────────────────────────────────────────
// user_callsigns — handlers
// ─────────────────────────────────────────────────────────────────────────────

// ListCallsigns handles GET /v1/callsigns.
// Returns all user callsigns, primary first.
func (h *CallsignHandler) ListCallsigns(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	tx, err := beginTenantTx(r.Context(), h.pool, userID)
	if err != nil {
		writeFailure(w, http.StatusServiceUnavailable, "database unavailable", "failed to prepare tenant context")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()

	queries := db.New(tx)
	rows, err := queries.ListUserCallsigns(r.Context(), userID)
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to list callsigns", "query failed")
		return
	}

	items := make([]userCallsignResponse, 0, len(rows))
	for _, row := range rows {
		items = append(items, userCallsignFromModel(row))
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to list callsigns", "transaction failed")
		return
	}

	writeSuccess(w, http.StatusOK, "callsigns listed", map[string]any{"items": items})
}

// CreateCallsign handles POST /v1/callsigns.
// Registers a new callsign for the authenticated user. If is_primary is set,
// the existing primary callsign has its flag cleared first.
func (h *CallsignHandler) CreateCallsign(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	var req userCallsignRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", "invalid JSON body")
		return
	}

	if strings.TrimSpace(req.Callsign) == "" {
		writeFailure(w, http.StatusBadRequest, "invalid request", "callsign is required")
		return
	}

	tx, err := beginTenantTx(r.Context(), h.pool, userID)
	if err != nil {
		writeFailure(w, http.StatusServiceUnavailable, "database unavailable", "failed to prepare tenant context")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()

	// If this callsign should be primary, clear the existing primary first.
	// The partial unique index on (user_id) WHERE is_primary=TRUE enforces single-primary
	// at the DB level; we do this proactively to avoid constraint errors.
	if req.IsPrimary {
		if _, err := tx.Exec(r.Context(), `
			UPDATE user_callsigns
			SET is_primary = FALSE
			WHERE user_id = $1 AND is_primary = TRUE
		`, userID); err != nil {
			writeFailure(w, http.StatusInternalServerError, "failed to create callsign", "failed to clear primary flag")
			return
		}
	}

	validFrom, err := parseOptionalDate(req.ValidFrom)
	if err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", fmt.Sprintf("valid_from: %v", err))
		return
	}
	validTo, err := parseOptionalDate(req.ValidTo)
	if err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", fmt.Sprintf("valid_to: %v", err))
		return
	}

	queries := db.New(tx)
	row, err := queries.CreateUserCallsign(r.Context(), db.CreateUserCallsignParams{
		UserID:       userID,
		Callsign:     strings.TrimSpace(req.Callsign),
		LicenseClass: normalizeOptional(req.LicenseClass),
		Country:      normalizeOptional(req.Country),
		DxccEntity:   req.DXCCEntity,
		IsPrimary:    req.IsPrimary,
		ValidFrom:    validFrom,
		ValidTo:      validTo,
	})
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to create callsign", "insert failed")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to create callsign", "transaction failed")
		return
	}

	writeSuccess(w, http.StatusCreated, "callsign registered", userCallsignFromModel(row))
}

// UpdateCallsign handles PUT /v1/callsigns/{uuid}.
// Updates the metadata of a user callsign. The callsign text itself is immutable.
func (h *CallsignHandler) UpdateCallsign(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	callsignUUID, err := callsignPathUUID(r, "callsignUUID")
	if err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", err.Error())
		return
	}

	var req userCallsignUpdateRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", "invalid JSON body")
		return
	}

	tx, err := beginTenantTx(r.Context(), h.pool, userID)
	if err != nil {
		writeFailure(w, http.StatusServiceUnavailable, "database unavailable", "failed to prepare tenant context")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()

	if req.IsPrimary {
		if _, err := tx.Exec(r.Context(), `
			UPDATE user_callsigns
			SET is_primary = FALSE
			WHERE user_id = $1 AND is_primary = TRUE
		`, userID); err != nil {
			writeFailure(w, http.StatusInternalServerError, "failed to update callsign", "failed to clear primary flag")
			return
		}
	}

	validFrom, err := parseOptionalDate(req.ValidFrom)
	if err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", fmt.Sprintf("valid_from: %v", err))
		return
	}
	validTo, err := parseOptionalDate(req.ValidTo)
	if err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", fmt.Sprintf("valid_to: %v", err))
		return
	}

	queries := db.New(tx)
	row, err := queries.UpdateUserCallsign(r.Context(), db.UpdateUserCallsignParams{
		CallsignUuid: callsignUUID,
		IsPrimary:    req.IsPrimary,
		LicenseClass: normalizeOptional(req.LicenseClass),
		Country:      normalizeOptional(req.Country),
		DxccEntity:   req.DXCCEntity,
		ValidFrom:    validFrom,
		ValidTo:      validTo,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeFailure(w, http.StatusOK, "callsign not found", "callsign not found")
		return
	}
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to update callsign", "update failed")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to update callsign", "transaction failed")
		return
	}

	writeSuccess(w, http.StatusOK, "callsign updated", userCallsignFromModel(row))
}

// DeleteCallsign handles DELETE /v1/callsigns/{uuid}.
// Removes a user callsign. Hard-delete is safe because user_callsigns has no
// downstream FK references from QSOs (QSOs use the TEXT callsign column directly).
func (h *CallsignHandler) DeleteCallsign(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	callsignUUID, err := callsignPathUUID(r, "callsignUUID")
	if err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", err.Error())
		return
	}

	tx, err := beginTenantTx(r.Context(), h.pool, userID)
	if err != nil {
		writeFailure(w, http.StatusServiceUnavailable, "database unavailable", "failed to prepare tenant context")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()

	queries := db.New(tx)
	if err := queries.DeleteUserCallsign(r.Context(), callsignUUID); err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to delete callsign", "delete failed")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to delete callsign", "transaction failed")
		return
	}

	writeSuccess(w, http.StatusOK, "callsign removed", nil)
}

// ─────────────────────────────────────────────────────────────────────────────
// station_callsigns — request/response types
// ─────────────────────────────────────────────────────────────────────────────

// stationCallsignRequest is the JSON body for creating a station callsign.
type stationCallsignRequest struct {
	// Callsign is the transmitting station identity. Stored uppercase.
	Callsign string `json:"callsign"`
	// CallsignType is one of: personal, club, special_event, contest, guest.
	CallsignType string `json:"callsign_type"`
	// Description is an optional human-readable note about this callsign.
	Description *string `json:"description,omitempty"`
	// ValidFrom and ValidTo are the validity period in YYYY-MM-DD format.
	ValidFrom *string `json:"valid_from,omitempty"`
	ValidTo   *string `json:"valid_to,omitempty"`
}

// stationCallsignUpdateRequest is the JSON body for updating a station callsign.
type stationCallsignUpdateRequest struct {
	CallsignType string  `json:"callsign_type"`
	Description  *string `json:"description,omitempty"`
	ValidFrom    *string `json:"valid_from,omitempty"`
	ValidTo      *string `json:"valid_to,omitempty"`
	// Active controls whether this callsign appears in logbook UI callsign selectors.
	// Set false to "archive" a callsign without losing QSO history.
	Active bool `json:"active"`
}

// stationCallsignResponse is the outward-facing representation of a station callsign.
type stationCallsignResponse struct {
	UUID         string    `json:"uuid"`
	Callsign     string    `json:"callsign"`
	CallsignType string    `json:"callsign_type"`
	Description  *string   `json:"description,omitempty"`
	ValidFrom    *string   `json:"valid_from,omitempty"`
	ValidTo      *string   `json:"valid_to,omitempty"`
	Active       bool      `json:"active"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// ─────────────────────────────────────────────────────────────────────────────
// station_callsigns — handlers
// ─────────────────────────────────────────────────────────────────────────────

// ListStationCallsigns handles GET /v1/station-callsigns.
// Returns all station callsigns for the authenticated user, active ones first.
func (h *CallsignHandler) ListStationCallsigns(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	tx, err := beginTenantTx(r.Context(), h.pool, userID)
	if err != nil {
		writeFailure(w, http.StatusServiceUnavailable, "database unavailable", "failed to prepare tenant context")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()

	queries := db.New(tx)
	rows, err := queries.ListStationCallsigns(r.Context(), userID)
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to list station callsigns", "query failed")
		return
	}

	items := make([]stationCallsignResponse, 0, len(rows))
	for _, row := range rows {
		items = append(items, stationCallsignFromModel(row))
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to list station callsigns", "transaction failed")
		return
	}

	writeSuccess(w, http.StatusOK, "station callsigns listed", map[string]any{"items": items})
}

// CreateStationCallsign handles POST /v1/station-callsigns.
// Creates a new station callsign identity entry.
func (h *CallsignHandler) CreateStationCallsign(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	var req stationCallsignRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", "invalid JSON body")
		return
	}

	if strings.TrimSpace(req.Callsign) == "" {
		writeFailure(w, http.StatusBadRequest, "invalid request", "callsign is required")
		return
	}
	if !isValidCallsignType(req.CallsignType) {
		writeFailure(w, http.StatusBadRequest, "invalid request",
			"callsign_type must be one of: personal, club, special_event, contest, guest")
		return
	}

	validFrom, err := parseOptionalDate(req.ValidFrom)
	if err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", fmt.Sprintf("valid_from: %v", err))
		return
	}
	validTo, err := parseOptionalDate(req.ValidTo)
	if err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", fmt.Sprintf("valid_to: %v", err))
		return
	}

	tx, err := beginTenantTx(r.Context(), h.pool, userID)
	if err != nil {
		writeFailure(w, http.StatusServiceUnavailable, "database unavailable", "failed to prepare tenant context")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()

	queries := db.New(tx)
	row, err := queries.CreateStationCallsign(r.Context(), db.CreateStationCallsignParams{
		UserID:       userID,
		Callsign:     strings.TrimSpace(req.Callsign),
		CallsignType: req.CallsignType,
		Description:  normalizeOptional(req.Description),
		ValidFrom:    validFrom,
		ValidTo:      validTo,
	})
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to create station callsign", "insert failed")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to create station callsign", "transaction failed")
		return
	}

	writeSuccess(w, http.StatusCreated, "station callsign created", stationCallsignFromModel(row))
}

// UpdateStationCallsign handles PUT /v1/station-callsigns/{uuid}.
// Updates metadata of a station callsign. Callsign text is immutable.
func (h *CallsignHandler) UpdateStationCallsign(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	callsignUUID, err := callsignPathUUID(r, "stationCallsignUUID")
	if err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", err.Error())
		return
	}

	var req stationCallsignUpdateRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", "invalid JSON body")
		return
	}

	if !isValidCallsignType(req.CallsignType) {
		writeFailure(w, http.StatusBadRequest, "invalid request",
			"callsign_type must be one of: personal, club, special_event, contest, guest")
		return
	}

	validFrom, err := parseOptionalDate(req.ValidFrom)
	if err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", fmt.Sprintf("valid_from: %v", err))
		return
	}
	validTo, err := parseOptionalDate(req.ValidTo)
	if err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", fmt.Sprintf("valid_to: %v", err))
		return
	}

	tx, err := beginTenantTx(r.Context(), h.pool, userID)
	if err != nil {
		writeFailure(w, http.StatusServiceUnavailable, "database unavailable", "failed to prepare tenant context")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()

	queries := db.New(tx)
	row, err := queries.UpdateStationCallsign(r.Context(), db.UpdateStationCallsignParams{
		CallsignUuid: callsignUUID,
		CallsignType: req.CallsignType,
		Description:  normalizeOptional(req.Description),
		ValidFrom:    validFrom,
		ValidTo:      validTo,
		Active:       req.Active,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeFailure(w, http.StatusOK, "station callsign not found", "station callsign not found")
		return
	}
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to update station callsign", "update failed")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to update station callsign", "transaction failed")
		return
	}

	writeSuccess(w, http.StatusOK, "station callsign updated", stationCallsignFromModel(row))
}

// DeleteStationCallsign handles DELETE /v1/station-callsigns/{uuid}.
// Soft-deactivates the station callsign (sets active=FALSE). QSOs that reference
// this callsign via station_callsign_id are preserved.
func (h *CallsignHandler) DeleteStationCallsign(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	callsignUUID, err := callsignPathUUID(r, "stationCallsignUUID")
	if err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", err.Error())
		return
	}

	tx, err := beginTenantTx(r.Context(), h.pool, userID)
	if err != nil {
		writeFailure(w, http.StatusServiceUnavailable, "database unavailable", "failed to prepare tenant context")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()

	queries := db.New(tx)
	if err := queries.DeleteStationCallsign(r.Context(), callsignUUID); err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to delete station callsign", "delete failed")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to delete station callsign", "transaction failed")
		return
	}

	writeSuccess(w, http.StatusOK, "station callsign deactivated", nil)
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

func callsignPathUUID(r *http.Request, paramName string) (uuid.UUID, error) {
	id, err := uuid.Parse(chi.URLParam(r, paramName))
	if err != nil {
		return uuid.Nil, fmt.Errorf("invalid UUID")
	}
	return id, nil
}

// isValidCallsignType checks against the CHECK constraint in the schema.
func isValidCallsignType(t string) bool {
	switch t {
	case "personal", "club", "special_event", "contest", "guest":
		return true
	}
	return false
}

// dateToStringPtr converts a pgtype.Date to an optional date string pointer.
func dateToStringPtr(d pgtype.Date) *string {
	if !d.Valid {
		return nil
	}
	s := d.Time.Format("2006-01-02")
	return &s
}

// ─────────────────────────────────────────────────────────────────────────────
// Response builders
// ─────────────────────────────────────────────────────────────────────────────

func userCallsignFromModel(row db.UserCallsign) userCallsignResponse {
	return userCallsignResponse{
		UUID:         row.Uuid.String(),
		Callsign:     row.Callsign,
		LicenseClass: row.LicenseClass,
		Country:      row.Country,
		DXCCEntity:   row.DxccEntity,
		IsPrimary:    row.IsPrimary,
		ValidFrom:    dateToStringPtr(row.ValidFrom),
		ValidTo:      dateToStringPtr(row.ValidTo),
		CreatedAt:    row.CreatedAt.Time.UTC(),
	}
}

func stationCallsignFromModel(row db.StationCallsign) stationCallsignResponse {
	return stationCallsignResponse{
		UUID:         row.Uuid.String(),
		Callsign:     row.Callsign,
		CallsignType: row.CallsignType,
		Description:  row.Description,
		ValidFrom:    dateToStringPtr(row.ValidFrom),
		ValidTo:      dateToStringPtr(row.ValidTo),
		Active:       row.Active,
		CreatedAt:    row.CreatedAt.Time.UTC(),
		UpdatedAt:    row.UpdatedAt.Time.UTC(),
	}
}
