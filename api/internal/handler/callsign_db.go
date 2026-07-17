// Package handler — FCC callsign database lookup, search, and operator profile endpoints.
//
// Public endpoints (no auth required):
//   - GET /v1/callsign/{call}         — lookup a single callsign
//   - GET /v1/callsign/{call}/grid    — geocode/cache a Maidenhead grid for onboarding
//   - GET /v1/callsign/search?q=...   — prefix search
//   - GET /v1/callsign/{call}/profile — get operator profile
//
// Authenticated endpoints:
//   - PUT /v1/callsign/{call}/profile — update own profile
//
// Response format: JSend wrapper {success, message, data, error}.
package handler

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/FtlC-ian/radioledger/api/internal/geo"
)

// CallsignDBHandler handles FCC callsign database endpoints.
type CallsignDBHandler struct {
	pool           *pgxpool.Pool
	geocodeAddress func(ctx context.Context, street, city, state, zip string) (lat, lon float64, err error)
}

// NewCallsignDBHandler creates a CallsignDBHandler.
func NewCallsignDBHandler(pool *pgxpool.Pool) *CallsignDBHandler {
	return NewCallsignDBHandlerWithGeocoder(pool, geo.GeocodeAddress)
}

// NewCallsignDBHandlerWithGeocoder creates a CallsignDBHandler with an injected
// geocoder. Used by tests to avoid live Census API calls.
func NewCallsignDBHandlerWithGeocoder(pool *pgxpool.Pool, geocoder func(ctx context.Context, street, city, state, zip string) (lat, lon float64, err error)) *CallsignDBHandler {
	return &CallsignDBHandler{
		pool:           pool,
		geocodeAddress: geocoder,
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Response types
// ─────────────────────────────────────────────────────────────────────────────

type callsignDBRecord struct {
	Callsign      string   `json:"callsign"`
	Source        string   `json:"source"`
	FirstName     string   `json:"first_name,omitempty"`
	LastName      string   `json:"last_name,omitempty"`
	FullName      string   `json:"full_name,omitempty"`
	AddressLine1  string   `json:"address_line1,omitempty"`
	City          string   `json:"city,omitempty"`
	StateProvince string   `json:"state_province,omitempty"`
	PostalCode    string   `json:"postal_code,omitempty"`
	Country       string   `json:"country"`
	LicenseClass  string   `json:"license_class,omitempty"`
	GrantDate     *string  `json:"grant_date,omitempty"`
	ExpiryDate    *string  `json:"expiry_date,omitempty"`
	Status        string   `json:"status"`
	GridSquare    string   `json:"grid_square,omitempty"`
	Latitude      *float64 `json:"latitude,omitempty"`
	Longitude     *float64 `json:"longitude,omitempty"`
	UpdatedAt     string   `json:"updated_at"`
}

type callsignProfileResponse struct {
	Callsign           string   `json:"callsign"`
	UserID             *int64   `json:"user_id,omitempty"`
	DisplayName        string   `json:"display_name,omitempty"`
	Bio                string   `json:"bio,omitempty"`
	AvatarURL          string   `json:"avatar_url,omitempty"`
	Website            string   `json:"website,omitempty"`
	QRZPage            string   `json:"qrz_page,omitempty"`
	StationDescription string   `json:"station_description,omitempty"`
	Antennas           []string `json:"antennas,omitempty"`
	Rigs               []string `json:"rigs,omitempty"`
	GridSquare         string   `json:"grid_square,omitempty"`
	QSLVia             string   `json:"qsl_via,omitempty"`
	QSLMessage         string   `json:"qsl_message,omitempty"`
	Twitter            string   `json:"twitter,omitempty"`
	Mastodon           string   `json:"mastodon,omitempty"`
	YouTube            string   `json:"youtube,omitempty"`
	TotalQSOs          int      `json:"total_qsos"`
	UniqueDXCC         int      `json:"unique_dxcc"`
	UniqueGrids        int      `json:"unique_grids"`
	MemberSince        *string  `json:"member_since,omitempty"`
	LastActive         *string  `json:"last_active,omitempty"`
	OnRadioLedger      bool     `json:"on_radioledger"`
}

type callsignFullLookup struct {
	*callsignDBRecord
	Profile *callsignProfileResponse `json:"profile,omitempty"`
}

type callsignGridLookupResponse struct {
	Callsign   string  `json:"callsign"`
	GridSquare *string `json:"grid_square,omitempty"`
	Source     string  `json:"source"`
	City       string  `json:"city,omitempty"`
	State      string  `json:"state,omitempty"`
}

type updateCallsignProfileRequest struct {
	DisplayName        *string  `json:"display_name"`
	Bio                *string  `json:"bio"`
	AvatarURL          *string  `json:"avatar_url"`
	Website            *string  `json:"website"`
	QRZPage            *string  `json:"qrz_page"`
	StationDescription *string  `json:"station_description"`
	Antennas           []string `json:"antennas"`
	Rigs               []string `json:"rigs"`
	GridSquare         *string  `json:"grid_square"`
	QSLVia             *string  `json:"qsl_via"`
	QSLMessage         *string  `json:"qsl_message"`
	Twitter            *string  `json:"twitter"`
	Mastodon           *string  `json:"mastodon"`
	YouTube            *string  `json:"youtube"`
}

// ─────────────────────────────────────────────────────────────────────────────
// GET /v1/callsign/{call}/grid — PUBLIC
// ─────────────────────────────────────────────────────────────────────────────

// GridLookup handles GET /v1/callsign/{call}/grid. Public — no auth required.
// It returns a cached grid immediately when present, otherwise geocodes the FCC
// address, stores the result back into callsign_records, and returns it.
func (h *CallsignDBHandler) GridLookup(w http.ResponseWriter, r *http.Request) {
	call := strings.ToUpper(strings.TrimSpace(chi.URLParam(r, "call")))
	if call == "" {
		writeFailure(w, http.StatusBadRequest, "invalid request", "callsign is required")
		return
	}

	ctx := r.Context()
	rec, err := h.lookupRecord(ctx, call)
	if err != nil {
		if err == pgx.ErrNoRows {
			writeSuccess(w, http.StatusOK, "callsign not found", nil)
			return
		}
		slog.ErrorContext(ctx, "callsign_db: grid lookup failed",
			slog.String("callsign", call), slog.String("error", err.Error()))
		writeFailure(w, http.StatusInternalServerError, "grid lookup failed", "database error")
		return
	}

	resp := callsignGridLookupResponse{
		Callsign: call,
		Source:   rec.Source,
		City:     rec.City,
		State:    rec.StateProvince,
	}

	if strings.TrimSpace(rec.GridSquare) != "" {
		grid := normalizeGridValue(rec.GridSquare)
		resp.GridSquare = &grid
		resp.Source = "cached"
		writeSuccess(w, http.StatusOK, "callsign grid retrieved", resp)
		return
	}

	if rec.Latitude != nil && rec.Longitude != nil {
		if grid := geo.LatLonToGrid(*rec.Latitude, *rec.Longitude); grid != "" {
			normalized := normalizeGridValue(grid)
			resp.GridSquare = &normalized
			resp.Source = "coordinates"
			if err := h.cacheGridLookup(ctx, call, rec.Source, normalized, *rec.Latitude, *rec.Longitude); err != nil {
				slog.WarnContext(ctx, "callsign_db: failed to cache coordinate-derived grid",
					slog.String("callsign", call), slog.String("error", err.Error()))
			}
			writeSuccess(w, http.StatusOK, "callsign grid retrieved", resp)
			return
		}
	}

	// Try Census street geocoder when we have a usable street address.
	var lat, lon float64
	var geocodeSource string

	if h.geocodeAddress != nil && isUSCallsignRecord(rec) &&
		strings.TrimSpace(rec.AddressLine1) != "" && !geo.IsPOBox(rec.AddressLine1) {
		var geocodeErr error
		lat, lon, geocodeErr = h.geocodeAddress(ctx, rec.AddressLine1, rec.City, rec.StateProvince, rec.PostalCode)
		if geocodeErr == nil {
			geocodeSource = "geocoded"
		} else {
			slog.WarnContext(ctx, "callsign_db: census geocode failed, trying zip centroid",
				slog.String("callsign", call), slog.String("error", geocodeErr.Error()))
		}
	}

	// Fallback: zip centroid from local DB (zero external calls).
	if geocodeSource == "" && strings.TrimSpace(rec.PostalCode) != "" {
		var centroidErr error
		lat, lon, centroidErr = geo.GeocodeFromZipCentroid(ctx, h.pool, rec.PostalCode)
		if centroidErr == nil {
			geocodeSource = "zip_centroid"
		} else {
			slog.WarnContext(ctx, "callsign_db: zip centroid lookup failed",
				slog.String("callsign", call),
				slog.String("zip", rec.PostalCode),
				slog.String("error", centroidErr.Error()))
		}
	}

	if geocodeSource == "" {
		writeSuccess(w, http.StatusOK, "callsign grid unavailable", resp)
		return
	}

	grid := geo.LatLonToGrid(lat, lon)
	if grid == "" {
		writeSuccess(w, http.StatusOK, "callsign grid unavailable", resp)
		return
	}
	normalized := normalizeGridValue(grid)
	resp.GridSquare = &normalized
	resp.Source = geocodeSource

	if err := h.cacheGridLookup(ctx, call, rec.Source, normalized, lat, lon); err != nil {
		slog.WarnContext(ctx, "callsign_db: failed to cache geocoded grid",
			slog.String("callsign", call), slog.String("error", err.Error()))
	}

	writeSuccess(w, http.StatusOK, "callsign grid retrieved", resp)
}

// ─────────────────────────────────────────────────────────────────────────────
// GET /v1/callsign/{call} — PUBLIC
// ─────────────────────────────────────────────────────────────────────────────

// DBLookup handles GET /v1/callsign/{call}. Public — no auth required.
func (h *CallsignDBHandler) DBLookup(w http.ResponseWriter, r *http.Request) {
	call := strings.ToUpper(strings.TrimSpace(chi.URLParam(r, "call")))
	if call == "" {
		writeFailure(w, http.StatusBadRequest, "invalid request", "callsign is required")
		return
	}

	ctx := r.Context()

	rec, err := h.lookupRecord(ctx, call)
	if err != nil {
		if err == pgx.ErrNoRows {
			writeFailure(w, http.StatusNotFound, "callsign not found",
				"no record found for "+call)
			return
		}
		slog.ErrorContext(ctx, "callsign_db: lookup failed",
			slog.String("callsign", call), slog.String("error", err.Error()))
		writeFailure(w, http.StatusInternalServerError, "lookup failed", "database error")
		return
	}

	profile, _ := h.lookupProfile(ctx, call)

	writeSuccess(w, http.StatusOK, "callsign found", callsignFullLookup{
		callsignDBRecord: rec,
		Profile:          profile,
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// GET /v1/callsign/search?q=...&limit=20&offset=0 — PUBLIC
// ─────────────────────────────────────────────────────────────────────────────

// DBSearch handles GET /v1/callsign/search. Public — no auth required.
func (h *CallsignDBHandler) DBSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeFailure(w, http.StatusBadRequest, "invalid request", "q parameter is required")
		return
	}

	limit := 20
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}
	offset := 0
	if raw := r.URL.Query().Get("offset"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n >= 0 {
			offset = n
		}
	}

	ctx := r.Context()
	qUpper := strings.ToUpper(q)

	pgxRows, err := h.pool.Query(ctx, `
		SELECT
			callsign, source,
			COALESCE(first_name, ''),
			COALESCE(last_name, ''),
			COALESCE(full_name, ''),
			COALESCE(address_line1, ''),
			COALESCE(city, ''),
			COALESCE(state_province, ''),
			COALESCE(postal_code, ''),
			country,
			COALESCE(license_class, ''),
			grant_date, expiry_date,
			status,
			COALESCE(grid_square, ''),
			latitude, longitude,
			updated_at
		FROM callsign_records
		WHERE
			callsign LIKE $1
			OR last_name ILIKE $2
			OR full_name ILIKE $2
			OR city ILIKE $2
			OR grid_square LIKE $1
		ORDER BY
			CASE WHEN callsign LIKE $1 THEN 0 ELSE 1 END,
			callsign
		LIMIT $3 OFFSET $4
	`, qUpper+"%", q+"%", limit, offset)
	if err != nil {
		slog.ErrorContext(ctx, "callsign_db: search failed",
			slog.String("q", q), slog.String("error", err.Error()))
		writeFailure(w, http.StatusInternalServerError, "search failed", "database error")
		return
	}
	defer pgxRows.Close()

	var results []callsignDBRecord
	for pgxRows.Next() {
		rec, scanErr := scanDBRecord(pgxRows)
		if scanErr != nil {
			slog.ErrorContext(ctx, "callsign_db: scan search row", slog.String("error", scanErr.Error()))
			continue
		}
		results = append(results, *rec)
	}
	if err := pgxRows.Err(); err != nil {
		slog.ErrorContext(ctx, "callsign_db: search rows error", slog.String("error", err.Error()))
	}

	if results == nil {
		results = []callsignDBRecord{}
	}

	writeSuccess(w, http.StatusOK, "search results", map[string]any{
		"results": results,
		"count":   len(results),
		"query":   q,
		"limit":   limit,
		"offset":  offset,
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// GET /v1/callsign/{call}/profile — PUBLIC
// ─────────────────────────────────────────────────────────────────────────────

// GetCallsignProfile handles GET /v1/callsign/{call}/profile. Public.
func (h *CallsignDBHandler) GetCallsignProfile(w http.ResponseWriter, r *http.Request) {
	call := strings.ToUpper(strings.TrimSpace(chi.URLParam(r, "call")))
	if call == "" {
		writeFailure(w, http.StatusBadRequest, "invalid request", "callsign is required")
		return
	}

	profile, err := h.lookupProfile(r.Context(), call)
	if err != nil && err != pgx.ErrNoRows {
		slog.ErrorContext(r.Context(), "callsign_db: get profile",
			slog.String("callsign", call), slog.String("error", err.Error()))
		writeFailure(w, http.StatusInternalServerError, "profile lookup failed", "database error")
		return
	}

	if profile == nil {
		// Return unclaimed stub.
		writeSuccess(w, http.StatusOK, "profile retrieved", callsignProfileResponse{
			Callsign:      call,
			OnRadioLedger: false,
		})
		return
	}

	writeSuccess(w, http.StatusOK, "profile retrieved", profile)
}

// ─────────────────────────────────────────────────────────────────────────────
// PUT /v1/callsign/{call}/profile — AUTH REQUIRED
// ─────────────────────────────────────────────────────────────────────────────

// UpdateCallsignProfile handles PUT /v1/callsign/{call}/profile.
// Requires auth. User must be the profile owner or the profile must be unclaimed.
func (h *CallsignDBHandler) UpdateCallsignProfile(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	call := strings.ToUpper(strings.TrimSpace(chi.URLParam(r, "call")))
	if call == "" {
		writeFailure(w, http.StatusBadRequest, "invalid request", "callsign is required")
		return
	}

	var req updateCallsignProfileRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}

	ctx := r.Context()

	// Check existing ownership.
	var existingUserID *int64
	err = h.pool.QueryRow(ctx,
		`SELECT user_id FROM operator_profiles WHERE callsign = $1`, call,
	).Scan(&existingUserID)
	if err != nil && err != pgx.ErrNoRows {
		slog.ErrorContext(ctx, "callsign_db: ownership check",
			slog.String("callsign", call), slog.String("error", err.Error()))
		writeFailure(w, http.StatusInternalServerError, "profile update failed", "database error")
		return
	}

	// Reject if claimed by someone else.
	if existingUserID != nil && *existingUserID != userID {
		writeFailure(w, http.StatusForbidden, "forbidden",
			"this callsign profile is owned by another account")
		return
	}

	antennas := req.Antennas
	if antennas == nil {
		antennas = []string{}
	}
	rigs := req.Rigs
	if rigs == nil {
		rigs = []string{}
	}

	_, err = h.pool.Exec(ctx, `
		INSERT INTO operator_profiles (
			callsign, user_id,
			display_name, bio, avatar_url, website, qrz_page,
			station_description, antennas, rigs, grid_square,
			qsl_via, qsl_message,
			twitter, mastodon, youtube,
			updated_at
		) VALUES (
			$1, $2,
			$3, $4, $5, $6, $7,
			$8, $9, $10, $11,
			$12, $13,
			$14, $15, $16,
			now()
		)
		ON CONFLICT (callsign) DO UPDATE SET
			user_id             = EXCLUDED.user_id,
			display_name        = COALESCE(NULLIF(EXCLUDED.display_name,''), operator_profiles.display_name),
			bio                 = COALESCE(NULLIF(EXCLUDED.bio,''), operator_profiles.bio),
			avatar_url          = COALESCE(NULLIF(EXCLUDED.avatar_url,''), operator_profiles.avatar_url),
			website             = COALESCE(NULLIF(EXCLUDED.website,''), operator_profiles.website),
			qrz_page            = COALESCE(NULLIF(EXCLUDED.qrz_page,''), operator_profiles.qrz_page),
			station_description = COALESCE(NULLIF(EXCLUDED.station_description,''), operator_profiles.station_description),
			antennas            = EXCLUDED.antennas,
			rigs                = EXCLUDED.rigs,
			grid_square         = COALESCE(NULLIF(EXCLUDED.grid_square,''), operator_profiles.grid_square),
			qsl_via             = COALESCE(NULLIF(EXCLUDED.qsl_via,''), operator_profiles.qsl_via),
			qsl_message         = COALESCE(NULLIF(EXCLUDED.qsl_message,''), operator_profiles.qsl_message),
			twitter             = COALESCE(NULLIF(EXCLUDED.twitter,''), operator_profiles.twitter),
			mastodon            = COALESCE(NULLIF(EXCLUDED.mastodon,''), operator_profiles.mastodon),
			youtube             = COALESCE(NULLIF(EXCLUDED.youtube,''), operator_profiles.youtube),
			updated_at          = now()
	`,
		call, userID,
		derefStr(req.DisplayName), derefStr(req.Bio), derefStr(req.AvatarURL),
		derefStr(req.Website), derefStr(req.QRZPage),
		derefStr(req.StationDescription), antennas, rigs, derefStr(req.GridSquare),
		derefStr(req.QSLVia), derefStr(req.QSLMessage),
		derefStr(req.Twitter), derefStr(req.Mastodon), derefStr(req.YouTube),
	)
	if err != nil {
		slog.ErrorContext(ctx, "callsign_db: upsert profile",
			slog.String("callsign", call), slog.String("error", err.Error()))
		writeFailure(w, http.StatusInternalServerError, "profile update failed", "database error")
		return
	}

	profile, _ := h.lookupProfile(ctx, call)
	if profile == nil {
		writeSuccess(w, http.StatusOK, "profile updated", map[string]any{"callsign": call})
		return
	}
	writeSuccess(w, http.StatusOK, "profile updated", profile)
}

// ─────────────────────────────────────────────────────────────────────────────
// Internal query helpers
// ─────────────────────────────────────────────────────────────────────────────

const callsignRecordCols = `
	callsign, source,
	COALESCE(first_name, ''),
	COALESCE(last_name, ''),
	COALESCE(full_name, ''),
	COALESCE(address_line1, ''),
	COALESCE(city, ''),
	COALESCE(state_province, ''),
	COALESCE(postal_code, ''),
	country,
	COALESCE(license_class, ''),
	grant_date, expiry_date,
	status,
	COALESCE(grid_square, ''),
	latitude, longitude,
	updated_at`

func (h *CallsignDBHandler) lookupRecord(ctx context.Context, call string) (*callsignDBRecord, error) {
	row := h.pool.QueryRow(ctx, `
		SELECT `+callsignRecordCols+`
		FROM callsign_records
		WHERE callsign = $1
		ORDER BY
			CASE source
				WHEN 'fcc'   THEN 1
				WHEN 'ised'  THEN 2
				WHEN 'ofcom' THEN 3
				ELSE 9
			END
		LIMIT 1
	`, call)
	return scanDBRecord(row)
}

// scannerRow matches both *pgx.Row and pgx.Rows.
type scannerRow interface {
	Scan(dest ...any) error
}

func scanDBRecord(row scannerRow) (*callsignDBRecord, error) {
	var rec callsignDBRecord
	var grantDate, expiryDate *time.Time
	var lat, lon *float64
	var updatedAt time.Time

	if err := row.Scan(
		&rec.Callsign, &rec.Source,
		&rec.FirstName, &rec.LastName, &rec.FullName,
		&rec.AddressLine1, &rec.City, &rec.StateProvince, &rec.PostalCode,
		&rec.Country,
		&rec.LicenseClass,
		&grantDate, &expiryDate,
		&rec.Status,
		&rec.GridSquare,
		&lat, &lon,
		&updatedAt,
	); err != nil {
		return nil, err
	}

	if grantDate != nil {
		s := grantDate.UTC().Format("2006-01-02")
		rec.GrantDate = &s
	}
	if expiryDate != nil {
		s := expiryDate.UTC().Format("2006-01-02")
		rec.ExpiryDate = &s
	}
	rec.Latitude = lat
	rec.Longitude = lon
	rec.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)

	return &rec, nil
}

func (h *CallsignDBHandler) cacheGridLookup(ctx context.Context, call, source, grid string, lat, lon float64) error {
	_, err := h.pool.Exec(ctx, `
		UPDATE callsign_records
		SET grid_square = $3,
		    latitude = $4,
		    longitude = $5,
		    updated_at = now()
		WHERE callsign = $1
		  AND source = $2
	`, call, source, strings.ToUpper(strings.TrimSpace(grid)), lat, lon)
	return err
}

func normalizeGridValue(grid string) string {
	g := strings.TrimSpace(grid)
	if len(g) <= 4 {
		return strings.ToUpper(g)
	}
	return strings.ToUpper(g[:4]) + strings.ToLower(g[4:])
}

func isUSCallsignRecord(rec *callsignDBRecord) bool {
	country := strings.ToUpper(strings.TrimSpace(rec.Country))
	return country == "" || country == "US" || country == "USA" || country == "UNITED STATES"
}

func (h *CallsignDBHandler) lookupProfile(ctx context.Context, call string) (*callsignProfileResponse, error) {
	var p callsignProfileResponse
	var userID *int64
	var memberSince, lastActive *time.Time
	var antennas, rigs []string

	err := h.pool.QueryRow(ctx, `
		SELECT
			callsign, user_id,
			COALESCE(display_name, ''),
			COALESCE(bio, ''),
			COALESCE(avatar_url, ''),
			COALESCE(website, ''),
			COALESCE(qrz_page, ''),
			COALESCE(station_description, ''),
			COALESCE(antennas, ARRAY[]::text[]),
			COALESCE(rigs, ARRAY[]::text[]),
			COALESCE(grid_square, ''),
			COALESCE(qsl_via, ''),
			COALESCE(qsl_message, ''),
			COALESCE(twitter, ''),
			COALESCE(mastodon, ''),
			COALESCE(youtube, ''),
			total_qsos, unique_dxcc, unique_grids,
			member_since, last_active
		FROM operator_profiles
		WHERE callsign = $1
	`, call).Scan(
		&p.Callsign, &userID,
		&p.DisplayName, &p.Bio, &p.AvatarURL, &p.Website, &p.QRZPage,
		&p.StationDescription,
		&antennas, &rigs,
		&p.GridSquare,
		&p.QSLVia, &p.QSLMessage,
		&p.Twitter, &p.Mastodon, &p.YouTube,
		&p.TotalQSOs, &p.UniqueDXCC, &p.UniqueGrids,
		&memberSince, &lastActive,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, pgx.ErrNoRows
		}
		return nil, err
	}

	p.UserID = userID
	p.OnRadioLedger = userID != nil
	p.Antennas = antennas
	p.Rigs = rigs

	if memberSince != nil {
		s := memberSince.UTC().Format(time.RFC3339)
		p.MemberSince = &s
	}
	if lastActive != nil {
		s := lastActive.UTC().Format(time.RFC3339)
		p.LastActive = &s
	}

	return &p, nil
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
