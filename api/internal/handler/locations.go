package handler

// Package handler — station location CRUD endpoints.
//
// Station locations are tQSL "station locations" required for LoTW integration.
// One callsign can have multiple locations (Home, POTA Portable, DXpedition),
// each with its own tQSL certificate and possibly a different expiry date.
//
// Endpoints:
//   POST   /v1/locations         — Create station location
//   GET    /v1/locations         — List user's station locations
//   GET    /v1/locations/{uuid}  — Get location detail
//   PUT    /v1/locations/{uuid}  — Update location
//   DELETE /v1/locations/{uuid}  — Soft delete

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	db "github.com/FtlC-ian/radioledger/api/internal/database/sqlc"
)

// LocationHandler handles station location CRUD endpoints.
type LocationHandler struct {
	pool *pgxpool.Pool
}

// NewLocationHandler creates a LocationHandler with its database pool dependency.
func NewLocationHandler(pool *pgxpool.Pool) *LocationHandler {
	return &LocationHandler{pool: pool}
}

// locationRequest is the JSON body accepted by Create and Update.
type locationRequest struct {
	// Name is the human-readable label for this location, e.g. "Home Station" or "POTA Portable".
	Name string `json:"name"`
	// Callsign is the callsign operated from this location. Stored uppercase.
	Callsign string `json:"callsign"`
	// GridSquare is the 4 or 6-character Maidenhead grid locator. Used for PostGIS
	// point computation when lat/lng are not provided.
	GridSquare string `json:"grid_square"`
	// Latitude and Longitude are optional. When both are provided, the PostGIS
	// point is derived from them; otherwise it is derived from GridSquare.
	Latitude  *float64 `json:"latitude,omitempty"`
	Longitude *float64 `json:"longitude,omitempty"`
	// DXCCEntity is the ARRL DXCC entity number for this location.
	DXCCEntity *int32 `json:"dxcc_entity,omitempty"`
	// State, County, City, Country are address components for awards tracking.
	State   *string `json:"state,omitempty"`
	County  *string `json:"county,omitempty"`
	City    *string `json:"city,omitempty"`
	Country *string `json:"country,omitempty"`
	// LoTWLocationName is the exact tQSL station location string (case-sensitive).
	// Must match EXACTLY what is configured in tQSL for successful LoTW uploads.
	LoTWLocationName *string `json:"lotw_location_name,omitempty"`
	// LoTWCertExpiry is the date the tQSL certificate for this location expires.
	// Certificate renewal takes days to weeks; the system surfaces warnings at 60/30/7 days.
	LoTWCertExpiry *string `json:"lotw_cert_expiry,omitempty"` // ISO 8601 date: YYYY-MM-DD
	// IsDefault marks this as the default station location for new logbooks.
	IsDefault bool `json:"is_default"`
}

// locationResponse is the outward-facing representation of a station location.
// Internal ID is never exposed; UUID is used in all API URLs.
type locationResponse struct {
	UUID             string    `json:"uuid"`
	Name             string    `json:"name"`
	Callsign         string    `json:"callsign"`
	GridSquare       string    `json:"grid_square"`
	Latitude         *float64  `json:"latitude,omitempty"`
	Longitude        *float64  `json:"longitude,omitempty"`
	DXCCEntity       *int32    `json:"dxcc_entity,omitempty"`
	State            *string   `json:"state,omitempty"`
	County           *string   `json:"county,omitempty"`
	City             *string   `json:"city,omitempty"`
	Country          *string   `json:"country,omitempty"`
	LoTWLocationName *string   `json:"lotw_location_name,omitempty"`
	LoTWCertExpiry   *string   `json:"lotw_cert_expiry,omitempty"`
	IsDefault        bool      `json:"is_default"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// List handles GET /v1/locations.
// Returns all active (non-deleted) station locations for the authenticated user.
func (h *LocationHandler) List(w http.ResponseWriter, r *http.Request) {
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
	rows, err := queries.ListStationLocations(r.Context(), userID)
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to list locations", "query failed")
		return
	}

	items := make([]locationResponse, 0, len(rows))
	for _, row := range rows {
		items = append(items, locationFromListRow(row))
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to list locations", "transaction failed")
		return
	}

	writeSuccess(w, http.StatusOK, "locations listed", map[string]any{"items": items})
}

// Get handles GET /v1/locations/{uuid}.
// Returns a single station location by UUID for the authenticated user (RLS enforces ownership).
func (h *LocationHandler) Get(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	locationUUID, err := locationPathUUID(r)
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
	row, err := queries.GetStationLocationByUUID(r.Context(), locationUUID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeFailure(w, http.StatusOK, "location not found", "location not found")
		return
	}
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to fetch location", "query failed")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to fetch location", "transaction failed")
		return
	}

	writeSuccess(w, http.StatusOK, "location retrieved", locationFromGetRow(row))
}

// Create handles POST /v1/locations.
// Creates a new station location for the authenticated user.
func (h *LocationHandler) Create(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	var req locationRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", "invalid JSON body")
		return
	}

	if err := validateLocationRequest(&req); err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", err.Error())
		return
	}

	tx, err := beginTenantTx(r.Context(), h.pool, userID)
	if err != nil {
		writeFailure(w, http.StatusServiceUnavailable, "database unavailable", "failed to prepare tenant context")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	// If this location should be the default, clear any existing default first.
	// The partial unique index on (user_id) WHERE is_default = TRUE AND deleted_at IS NULL
	// enforces single-default at the DB level; we do this proactively to avoid constraint errors.
	if req.IsDefault {
		if _, err := tx.Exec(r.Context(), `
			UPDATE station_locations
			SET is_default = FALSE, updated_at = NOW()
			WHERE user_id = $1 AND deleted_at IS NULL
		`, userID); err != nil {
			writeFailure(w, http.StatusInternalServerError, "failed to create location", "failed to clear default flag")
			return
		}
	}

	queries := db.New(tx)
	params, err := buildCreateLocationParams(userID, &req)
	if err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", err.Error())
		return
	}

	row, err := queries.CreateStationLocation(r.Context(), params)
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to create location", "insert failed")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to create location", "transaction failed")
		return
	}

	writeSuccess(w, http.StatusCreated, "location created", locationFromCreateRow(row))
}

// Update handles PUT /v1/locations/{uuid}.
// Replaces all mutable fields of a station location.
func (h *LocationHandler) Update(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	locationUUID, err := locationPathUUID(r)
	if err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", err.Error())
		return
	}

	var req locationRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", "invalid JSON body")
		return
	}

	if err := validateLocationRequest(&req); err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", err.Error())
		return
	}

	tx, err := beginTenantTx(r.Context(), h.pool, userID)
	if err != nil {
		writeFailure(w, http.StatusServiceUnavailable, "database unavailable", "failed to prepare tenant context")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	if req.IsDefault {
		if _, err := tx.Exec(r.Context(), `
			UPDATE station_locations
			SET is_default = FALSE, updated_at = NOW()
			WHERE user_id = $1 AND uuid <> $2 AND deleted_at IS NULL
		`, userID, locationUUID); err != nil {
			writeFailure(w, http.StatusInternalServerError, "failed to update location", "failed to clear default flag")
			return
		}
	}

	queries := db.New(tx)
	params, err := buildUpdateLocationParams(locationUUID, &req)
	if err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", err.Error())
		return
	}

	row, err := queries.UpdateStationLocation(r.Context(), params)
	if errors.Is(err, pgx.ErrNoRows) {
		writeFailure(w, http.StatusOK, "location not found", "location not found")
		return
	}
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to update location", "update failed")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to update location", "transaction failed")
		return
	}

	writeSuccess(w, http.StatusOK, "location updated", locationFromUpdateRow(row))
}

// Delete handles DELETE /v1/locations/{uuid}.
// Soft-deletes the location by setting deleted_at. Logbooks that reference this
// location via station_location_id are not affected (FK uses ON DELETE SET NULL).
func (h *LocationHandler) Delete(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	locationUUID, err := locationPathUUID(r)
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
	if err := queries.DeleteStationLocation(r.Context(), locationUUID); err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to delete location", "delete failed")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to delete location", "transaction failed")
		return
	}

	writeSuccess(w, http.StatusOK, "location deleted", nil)
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

func locationPathUUID(r *http.Request) (uuid.UUID, error) {
	id, err := uuid.Parse(chi.URLParam(r, "locationUUID"))
	if err != nil {
		return uuid.Nil, fmt.Errorf("invalid location UUID")
	}
	return id, nil
}

// validateLocationRequest checks required fields and basic format rules.
func validateLocationRequest(req *locationRequest) error {
	if strings.TrimSpace(req.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if strings.TrimSpace(req.Callsign) == "" {
		return fmt.Errorf("callsign is required")
	}
	if strings.TrimSpace(req.GridSquare) == "" {
		return fmt.Errorf("grid_square is required")
	}
	// Latitude/longitude range check when provided.
	if req.Latitude != nil && (*req.Latitude < -90 || *req.Latitude > 90) {
		return fmt.Errorf("latitude must be between -90 and 90")
	}
	if req.Longitude != nil && (*req.Longitude < -180 || *req.Longitude > 180) {
		return fmt.Errorf("longitude must be between -180 and 180")
	}
	return nil
}

// buildCreateLocationParams converts a validated locationRequest into sqlc params.
func buildCreateLocationParams(userID int64, req *locationRequest) (db.CreateStationLocationParams, error) {
	certExpiry, err := parseOptionalDate(req.LoTWCertExpiry)
	if err != nil {
		return db.CreateStationLocationParams{}, fmt.Errorf("lotw_cert_expiry: %w", err)
	}

	return db.CreateStationLocationParams{
		UserID:           userID,
		Name:             strings.TrimSpace(req.Name),
		Callsign:         strings.TrimSpace(req.Callsign),
		GridSquare:       strings.TrimSpace(req.GridSquare),
		Latitude:         float64ToNumeric(req.Latitude),
		Longitude:        float64ToNumeric(req.Longitude),
		DxccEntity:       req.DXCCEntity,
		State:            normalizeOptional(req.State),
		County:           normalizeOptional(req.County),
		City:             normalizeOptional(req.City),
		Country:          normalizeOptional(req.Country),
		LotwLocationName: normalizeOptional(req.LoTWLocationName),
		LotwCertExpiry:   certExpiry,
		IsDefault:        req.IsDefault,
	}, nil
}

// buildUpdateLocationParams converts a validated locationRequest into sqlc update params.
func buildUpdateLocationParams(locationUUID uuid.UUID, req *locationRequest) (db.UpdateStationLocationParams, error) {
	certExpiry, err := parseOptionalDate(req.LoTWCertExpiry)
	if err != nil {
		return db.UpdateStationLocationParams{}, fmt.Errorf("lotw_cert_expiry: %w", err)
	}

	return db.UpdateStationLocationParams{
		LocationUuid:     locationUUID,
		Name:             strings.TrimSpace(req.Name),
		Callsign:         strings.TrimSpace(req.Callsign),
		GridSquare:       strings.TrimSpace(req.GridSquare),
		Latitude:         float64ToNumeric(req.Latitude),
		Longitude:        float64ToNumeric(req.Longitude),
		DxccEntity:       req.DXCCEntity,
		State:            normalizeOptional(req.State),
		County:           normalizeOptional(req.County),
		City:             normalizeOptional(req.City),
		Country:          normalizeOptional(req.Country),
		LotwLocationName: normalizeOptional(req.LoTWLocationName),
		LotwCertExpiry:   certExpiry,
		IsDefault:        req.IsDefault,
	}, nil
}

// parseOptionalDate parses an optional YYYY-MM-DD date string into pgtype.Date.
func parseOptionalDate(s *string) (pgtype.Date, error) {
	if s == nil || strings.TrimSpace(*s) == "" {
		return pgtype.Date{}, nil
	}
	t, err := time.Parse("2006-01-02", strings.TrimSpace(*s))
	if err != nil {
		return pgtype.Date{}, fmt.Errorf("invalid date format, expected YYYY-MM-DD: %w", err)
	}
	return pgtype.Date{Time: t, Valid: true}, nil
}

// float64ToNumeric converts an optional float64 pointer to pgtype.Numeric.
// pgtype.Numeric.Scan only accepts string/[]byte, so we convert via FormatFloat.
func float64ToNumeric(v *float64) pgtype.Numeric {
	if v == nil {
		return pgtype.Numeric{}
	}
	var n pgtype.Numeric
	s := strconv.FormatFloat(*v, 'f', -1, 64)
	if err := n.Scan(s); err != nil {
		return pgtype.Numeric{}
	}
	return n
}

// numericToFloat64 converts a pgtype.Numeric to an optional float64 pointer.
// Returns nil when the value is invalid (NULL in the database).
func numericToFloat64(n pgtype.Numeric) *float64 {
	if !n.Valid {
		return nil
	}
	f, err := n.Float64Value()
	if err != nil || !f.Valid {
		return nil
	}
	return &f.Float64
}

// dateToString converts a pgtype.Date to an optional YYYY-MM-DD string pointer.
func dateToString(d pgtype.Date) *string {
	if !d.Valid {
		return nil
	}
	s := d.Time.Format("2006-01-02")
	return &s
}

// ─────────────────────────────────────────────────────────────────────────────
// Response builders
// ─────────────────────────────────────────────────────────────────────────────

func locationFromCreateRow(row db.CreateStationLocationRow) locationResponse {
	return locationResponse{
		UUID:             row.Uuid.String(),
		Name:             row.Name,
		Callsign:         row.Callsign,
		GridSquare:       row.GridSquare,
		Latitude:         numericToFloat64(row.Latitude),
		Longitude:        numericToFloat64(row.Longitude),
		DXCCEntity:       row.DxccEntity,
		State:            row.State,
		County:           row.County,
		City:             row.City,
		Country:          row.Country,
		LoTWLocationName: row.LotwLocationName,
		LoTWCertExpiry:   dateToString(row.LotwCertExpiry),
		IsDefault:        row.IsDefault,
		CreatedAt:        row.CreatedAt.Time.UTC(),
		UpdatedAt:        row.UpdatedAt.Time.UTC(),
	}
}

func locationFromGetRow(row db.GetStationLocationByUUIDRow) locationResponse {
	return locationResponse{
		UUID:             row.Uuid.String(),
		Name:             row.Name,
		Callsign:         row.Callsign,
		GridSquare:       row.GridSquare,
		Latitude:         numericToFloat64(row.Latitude),
		Longitude:        numericToFloat64(row.Longitude),
		DXCCEntity:       row.DxccEntity,
		State:            row.State,
		County:           row.County,
		City:             row.City,
		Country:          row.Country,
		LoTWLocationName: row.LotwLocationName,
		LoTWCertExpiry:   dateToString(row.LotwCertExpiry),
		IsDefault:        row.IsDefault,
		CreatedAt:        row.CreatedAt.Time.UTC(),
		UpdatedAt:        row.UpdatedAt.Time.UTC(),
	}
}

func locationFromListRow(row db.ListStationLocationsRow) locationResponse {
	return locationResponse{
		UUID:             row.Uuid.String(),
		Name:             row.Name,
		Callsign:         row.Callsign,
		GridSquare:       row.GridSquare,
		Latitude:         numericToFloat64(row.Latitude),
		Longitude:        numericToFloat64(row.Longitude),
		DXCCEntity:       row.DxccEntity,
		State:            row.State,
		County:           row.County,
		City:             row.City,
		Country:          row.Country,
		LoTWLocationName: row.LotwLocationName,
		LoTWCertExpiry:   dateToString(row.LotwCertExpiry),
		IsDefault:        row.IsDefault,
		CreatedAt:        row.CreatedAt.Time.UTC(),
		UpdatedAt:        row.UpdatedAt.Time.UTC(),
	}
}

func locationFromUpdateRow(row db.UpdateStationLocationRow) locationResponse {
	return locationResponse{
		UUID:             row.Uuid.String(),
		Name:             row.Name,
		Callsign:         row.Callsign,
		GridSquare:       row.GridSquare,
		Latitude:         numericToFloat64(row.Latitude),
		Longitude:        numericToFloat64(row.Longitude),
		DXCCEntity:       row.DxccEntity,
		State:            row.State,
		County:           row.County,
		City:             row.City,
		Country:          row.Country,
		LoTWLocationName: row.LotwLocationName,
		LoTWCertExpiry:   dateToString(row.LotwCertExpiry),
		IsDefault:        row.IsDefault,
		CreatedAt:        row.CreatedAt.Time.UTC(),
		UpdatedAt:        row.UpdatedAt.Time.UTC(),
	}
}
