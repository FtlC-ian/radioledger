package handler

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
	auth "github.com/FtlC-ian/radioledger/api/internal/auth"

	db "github.com/FtlC-ian/radioledger/api/internal/database/sqlc"
)

// ContestHandler handles HTTP requests for contest session resources.
type ContestHandler struct {
	pool *pgxpool.Pool
}

// NewContestHandler creates a ContestHandler with its dependencies.
func NewContestHandler(pool *pgxpool.Pool) *ContestHandler {
	return &ContestHandler{pool: pool}
}

// ─── Request/Response types ────────────────────────────────────────────────

type contestCreateRequest struct {
	// Human-readable name, e.g. "CQ WW SSB 2026".
	Name string `json:"name"`

	// ADIF contest identifier, e.g. "CQ-WW-SSB".
	ContestCode string `json:"contest_id"`

	// UUID of the logbook to log QSOs into. Defaults to user's default logbook.
	LogbookUUID *string `json:"logbook_uuid,omitempty"`

	// UUID of the station callsign (from /v1/station-callsigns).
	StationCallsignUUID *string `json:"station_callsign_uuid,omitempty"`

	// Contest operating window.
	StartsAt *time.Time `json:"starts_at,omitempty"`
	EndsAt   *time.Time `json:"ends_at,omitempty"`

	// Exchange template type: serial | grid | state | zone | custom.
	ExchangeTemplate string `json:"exchange_template"`

	// Static portion of the sent exchange (e.g., "AR" for serial+state).
	ExchangeSent *string `json:"exchange_sent,omitempty"`

	// Cabrillo category fields.
	CategoryOperator    string  `json:"category_operator"`
	CategoryAssisted    string  `json:"category_assisted"`
	CategoryBand        string  `json:"category_band"`
	CategoryMode        string  `json:"category_mode"`
	CategoryPower       string  `json:"category_power"`
	CategoryStation     string  `json:"category_station"`
	CategoryTime        string  `json:"category_time"`
	CategoryTransmitter string  `json:"category_transmitter"`
	CategoryOverlay     *string `json:"category_overlay,omitempty"`

	OperatorsLine *string `json:"operators_line,omitempty"`
	ClubName      *string `json:"club_name,omitempty"`
	Location      *string `json:"location,omitempty"`
	Soapbox       *string `json:"soapbox,omitempty"`
}

type contestUpdateRequest struct {
	Name             string     `json:"name"`
	StartsAt         *time.Time `json:"starts_at,omitempty"`
	EndsAt           *time.Time `json:"ends_at,omitempty"`
	ExchangeTemplate string     `json:"exchange_template"`
	ExchangeSent     *string    `json:"exchange_sent,omitempty"`
	Status           string     `json:"status"`

	CategoryOperator    string  `json:"category_operator"`
	CategoryAssisted    string  `json:"category_assisted"`
	CategoryBand        string  `json:"category_band"`
	CategoryMode        string  `json:"category_mode"`
	CategoryPower       string  `json:"category_power"`
	CategoryStation     string  `json:"category_station"`
	CategoryTime        string  `json:"category_time"`
	CategoryTransmitter string  `json:"category_transmitter"`
	CategoryOverlay     *string `json:"category_overlay,omitempty"`

	OperatorsLine *string `json:"operators_line,omitempty"`
	ClubName      *string `json:"club_name,omitempty"`
	Location      *string `json:"location,omitempty"`
	Soapbox       *string `json:"soapbox,omitempty"`
}

// contestQSORequest is the minimal quick-entry QSO form for contest logging.
type contestQSORequest struct {
	Callsign string `json:"callsign"`

	// Exchange received from the other station.
	ExchangeRcvd string `json:"exchange_rcvd"`

	// Band and mode. Mode defaults to the session's category_mode.
	Band string `json:"band"`
	Mode string `json:"mode"`

	// Optional frequency in Hz (e.g., 14025000).
	FrequencyHz *int64 `json:"frequency_hz,omitempty"`

	// Override RST (default: 59 for SSB/AM/FM, 599 for CW/RTTY/FT8/FT4/DIGI).
	RstSent *string `json:"rst_sent,omitempty"`
	RstRcvd *string `json:"rst_rcvd,omitempty"`

	// Override datetime (defaults to now). ISO 8601.
	DatetimeOn *time.Time `json:"datetime_on,omitempty"`
}

// contestSessionResponse is the outbound representation of a contest session.
type contestSessionResponse struct {
	UUID             string  `json:"uuid"`
	LogbookUUID      string  `json:"logbook_uuid"`
	ContestCode      string  `json:"contest_code"`
	ContestName      string  `json:"contest_name"`
	Name             string  `json:"name"`
	MyCallsign       *string `json:"my_callsign,omitempty"`
	ExchangeTemplate string  `json:"exchange_template"`
	ExchangeSent     *string `json:"exchange_sent,omitempty"`
	Status           string  `json:"status"`
	SerialCounter    int32   `json:"serial_counter"`

	StartsAt *time.Time `json:"starts_at,omitempty"`
	EndsAt   *time.Time `json:"ends_at,omitempty"`

	CategoryOperator    string  `json:"category_operator"`
	CategoryAssisted    string  `json:"category_assisted"`
	CategoryBand        string  `json:"category_band"`
	CategoryMode        string  `json:"category_mode"`
	CategoryPower       string  `json:"category_power"`
	CategoryStation     string  `json:"category_station"`
	CategoryTime        string  `json:"category_time"`
	CategoryTransmitter string  `json:"category_transmitter"`
	CategoryOverlay     *string `json:"category_overlay,omitempty"`

	OperatorsLine *string `json:"operators_line,omitempty"`
	ClubName      *string `json:"club_name,omitempty"`
	Location      *string `json:"location,omitempty"`
	Soapbox       *string `json:"soapbox,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// contestQSOResponse is the response after logging a contest QSO.
type contestQSOResponse struct {
	UUID         string    `json:"uuid"`
	Callsign     string    `json:"callsign"`
	Band         string    `json:"band"`
	Mode         string    `json:"mode"`
	DatetimeOn   time.Time `json:"datetime_on"`
	RstSent      *string   `json:"rst_sent,omitempty"`
	RstRcvd      *string   `json:"rst_rcvd,omitempty"`
	FrequencyHz  *int64    `json:"frequency_hz,omitempty"`
	SentSerial   *int32    `json:"sent_serial,omitempty"`
	SentExchange *string   `json:"sent_exchange,omitempty"`
	RecvExchange *string   `json:"recv_exchange,omitempty"`
	IsDupe       bool      `json:"is_dupe"`
}

type contestStatsResponse struct {
	TotalQSOs       int64      `json:"total_qsos"`
	DupeQSOs        int64      `json:"dupe_qsos"`
	UniqueCallsigns int64      `json:"unique_callsigns"`
	SerialCounter   int32      `json:"serial_counter"`
	RatePerHour     float64    `json:"rate_per_hour"`
	RatePer10Min    float64    `json:"rate_per_10min"`
	RatePerMin      float64    `json:"rate_per_min"`
	FirstQSOAt      *time.Time `json:"first_qso_at,omitempty"`
	LastQSOAt       *time.Time `json:"last_qso_at,omitempty"`
}

type dupeCheckResponse struct {
	Dupe        bool               `json:"dupe"`
	PreviousQSO *dupeCheckPrevious `json:"previous_qso,omitempty"`
}

type dupeCheckPrevious struct {
	UUID         string    `json:"uuid"`
	Callsign     string    `json:"callsign"`
	Band         string    `json:"band"`
	Mode         string    `json:"mode"`
	DatetimeOn   time.Time `json:"datetime_on"`
	SentSerial   *int32    `json:"sent_serial,omitempty"`
	RecvExchange *string   `json:"recv_exchange,omitempty"`
}

// ─── Handlers ──────────────────────────────────────────────────────────────

// Create handles POST /v1/contests — start a contest session.
func (h *ContestHandler) Create(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	var req contestCreateRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", "invalid JSON body")
		return
	}
	if err := validateContestCreate(req); err != nil {
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

	// Resolve logbook (default if not specified).
	logbookID, logbookUUID, err := resolveActivationLogbook(r.Context(), queries, userID, req.LogbookUUID, auth.PermissionQSOCreate)
	if err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", err.Error())
		return
	}

	// Upsert the contest catalog entry.
	contestCode := strings.ToUpper(strings.TrimSpace(req.ContestCode))
	contestRow, err := queries.UpsertContest(r.Context(), db.UpsertContestParams{
		ContestCode: contestCode,
		Name:        req.Name,
	})
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to create contest session", "contest upsert failed")
		return
	}

	// Optionally resolve station callsign.
	var stationCallsignID *int64
	if req.StationCallsignUUID != nil && strings.TrimSpace(*req.StationCallsignUUID) != "" {
		scUUID, err := uuid.Parse(strings.TrimSpace(*req.StationCallsignUUID))
		if err != nil {
			writeFailure(w, http.StatusBadRequest, "invalid request", "invalid station_callsign_uuid")
			return
		}
		sc, err := queries.GetStationCallsignByUUID(r.Context(), scUUID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeFailure(w, http.StatusBadRequest, "invalid request", "station callsign not found")
				return
			}
			writeFailure(w, http.StatusInternalServerError, "failed to create contest session", "callsign lookup failed")
			return
		}
		stationCallsignID = &sc.ID
	}

	template := coalesceString(req.ExchangeTemplate, "serial")
	catOp := coalesceString(req.CategoryOperator, "SINGLE-OP")
	catAssisted := coalesceString(req.CategoryAssisted, "NON-ASSISTED")
	catBand := coalesceString(req.CategoryBand, "ALL")
	catMode := coalesceString(req.CategoryMode, "MIXED")
	catPower := coalesceString(req.CategoryPower, "HIGH")
	catStation := coalesceString(req.CategoryStation, "FIXED")
	catTime := coalesceString(req.CategoryTime, "24-HOURS")
	catTx := coalesceString(req.CategoryTransmitter, "ONE")

	params := db.CreateContestSessionParams{
		UserID:              userID,
		LogbookID:           logbookID,
		ContestID:           contestRow.ID,
		StationCallsignID:   stationCallsignID,
		Name:                req.Name,
		StartsAt:            toOptTimestamptz(req.StartsAt),
		EndsAt:              toOptTimestamptz(req.EndsAt),
		CategoryOperator:    catOp,
		CategoryAssisted:    catAssisted,
		CategoryBand:        catBand,
		CategoryMode:        catMode,
		CategoryPower:       catPower,
		CategoryStation:     catStation,
		CategoryTime:        catTime,
		CategoryTransmitter: catTx,
		CategoryOverlay:     normalizeOptional(req.CategoryOverlay),
		OperatorsLine:       normalizeOptional(req.OperatorsLine),
		ClubName:            normalizeOptional(req.ClubName),
		Location:            normalizeOptional(req.Location),
		Soapbox:             normalizeOptional(req.Soapbox),
		ExchangeSent:        normalizeOptional(req.ExchangeSent),
		ExchangeTemplate:    template,
		Status:              "active",
		CabrilloVersion:     "3.0",
	}

	row, err := queries.CreateContestSession(r.Context(), params)
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to create contest session", "insert failed")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to create contest session", "transaction failed")
		return
	}

	resp := contestSessionResponseFromCreate(row, logbookUUID, contestCode, req.Name)
	writeSuccess(w, http.StatusCreated, "contest session created", resp)
}

// List handles GET /v1/contests.
func (h *ContestHandler) List(w http.ResponseWriter, r *http.Request) {
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
	rows, err := queries.ListContestSessions(r.Context(), userID)
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to list contest sessions", "query failed")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to list contest sessions", "transaction failed")
		return
	}

	items := make([]contestSessionResponse, 0, len(rows))
	for _, row := range rows {
		items = append(items, contestSessionResponseFromList(row))
	}
	writeSuccess(w, http.StatusOK, "contest sessions listed", map[string]any{"items": items})
}

// Get handles GET /v1/contests/{uuid}.
func (h *ContestHandler) Get(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	sessionUUID, err := parseContestUUID(r)
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
	row, err := queries.GetContestSessionByUUID(r.Context(), sessionUUID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeFailure(w, http.StatusOK, "contest session not found", "not found")
		return
	}
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to get contest session", "query failed")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to get contest session", "transaction failed")
		return
	}

	writeSuccess(w, http.StatusOK, "contest session retrieved", contestSessionResponseFromGet(row))
}

// Update handles PUT /v1/contests/{uuid}.
func (h *ContestHandler) Update(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	sessionUUID, err := parseContestUUID(r)
	if err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", err.Error())
		return
	}

	var req contestUpdateRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", "invalid JSON body")
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		writeFailure(w, http.StatusBadRequest, "invalid request", "name is required")
		return
	}

	tx, err := beginTenantTx(r.Context(), h.pool, userID)
	if err != nil {
		writeFailure(w, http.StatusServiceUnavailable, "database unavailable", "failed to prepare tenant context")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	queries := db.New(tx)
	template := coalesceString(req.ExchangeTemplate, "serial")
	status := coalesceString(req.Status, "active")

	params := db.UpdateContestSessionParams{
		SessionUuid:         sessionUUID,
		Name:                req.Name,
		StartsAt:            toOptTimestamptz(req.StartsAt),
		EndsAt:              toOptTimestamptz(req.EndsAt),
		CategoryOperator:    coalesceString(req.CategoryOperator, "SINGLE-OP"),
		CategoryAssisted:    coalesceString(req.CategoryAssisted, "NON-ASSISTED"),
		CategoryBand:        coalesceString(req.CategoryBand, "ALL"),
		CategoryMode:        coalesceString(req.CategoryMode, "MIXED"),
		CategoryPower:       coalesceString(req.CategoryPower, "HIGH"),
		CategoryStation:     coalesceString(req.CategoryStation, "FIXED"),
		CategoryTime:        coalesceString(req.CategoryTime, "24-HOURS"),
		CategoryTransmitter: coalesceString(req.CategoryTransmitter, "ONE"),
		CategoryOverlay:     normalizeOptional(req.CategoryOverlay),
		OperatorsLine:       normalizeOptional(req.OperatorsLine),
		ClubName:            normalizeOptional(req.ClubName),
		Location:            normalizeOptional(req.Location),
		Soapbox:             normalizeOptional(req.Soapbox),
		ExchangeSent:        normalizeOptional(req.ExchangeSent),
		ExchangeTemplate:    template,
		Status:              status,
	}

	row, err := queries.UpdateContestSession(r.Context(), params)
	if errors.Is(err, pgx.ErrNoRows) {
		writeFailure(w, http.StatusOK, "contest session not found", "not found")
		return
	}
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to update contest session", "update failed")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to update contest session", "transaction failed")
		return
	}

	writeSuccess(w, http.StatusOK, "contest session updated", contestSessionResponseFromUpdate(row))
}

// LogQSO handles POST /v1/contests/{uuid}/qso — speed-optimized contest QSO entry.
func (h *ContestHandler) LogQSO(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	sessionUUID, err := parseContestUUID(r)
	if err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", err.Error())
		return
	}

	var req contestQSORequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", "invalid JSON body")
		return
	}
	if err := validateContestQSORequest(req); err != nil {
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

	// Load the contest session (validates ownership via RLS, gets exchange_template).
	session, err := queries.GetContestSessionByUUID(r.Context(), sessionUUID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeFailure(w, http.StatusOK, "contest session not found", "not found")
		return
	}
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to log contest QSO", "session lookup failed")
		return
	}

	// Real-time dupe check: callsign+band already worked in this session?
	existingRow, dupeCheckErr := queries.CheckDupe(r.Context(), db.CheckDupeParams{
		SessionUuid: sessionUUID,
		Callsign:    req.Callsign,
		Band:        req.Band,
	})
	isDupe := dupeCheckErr == nil // found a matching non-dupe QSO = this is a dupe

	// Auto-fill RST if not provided.
	rstSent := req.RstSent
	rstRcvd := req.RstRcvd
	if rstSent == nil || rstRcvd == nil {
		defaultRST := defaultRSTForMode(req.Mode)
		if rstSent == nil {
			rstSent = &defaultRST
		}
		if rstRcvd == nil {
			rstRcvd = &defaultRST
		}
	}

	// Determine QSO timestamp.
	datetimeOn := time.Now().UTC()
	if req.DatetimeOn != nil {
		datetimeOn = req.DatetimeOn.UTC()
	}

	// Atomically increment serial counter (even for dupes — serial is assigned per attempt).
	var sentSerial *int32
	if session.ExchangeTemplate == "serial" {
		serial, err := queries.IncrementSerialCounter(r.Context(), sessionUUID)
		if err != nil {
			writeFailure(w, http.StatusInternalServerError, "failed to log contest QSO", "serial increment failed")
			return
		}
		sentSerial = &serial
	}

	// Build sent exchange string.
	sentExchange := buildSentExchange(session, sentSerial, req)

	// Parse received serial (for serial-based contests).
	recvSerial := parseRecvSerial(req.ExchangeRcvd, session.ExchangeTemplate)

	// Insert the QSO.
	qsoRow, err := queries.InsertContestQSO(r.Context(), db.InsertContestQSOParams{
		SessionUuid:     sessionUUID,
		CreatedByUserID: userID,
		Callsign:        req.Callsign,
		Band:            req.Band,
		Mode:            req.Mode,
		DatetimeOn:      pgtype.Timestamptz{Time: datetimeOn, Valid: true},
		RstSent:         rstSent,
		RstRcvd:         rstRcvd,
		FrequencyHz:     req.FrequencyHz,
	})
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to log contest QSO", "qso insert failed")
		return
	}

	// Insert the exchange record.
	exchRow, err := queries.InsertContestQSOExchange(r.Context(), db.InsertContestQSOExchangeParams{
		SessionUuid:  sessionUUID,
		QsoID:        qsoRow.ID,
		SentSerial:   sentSerial,
		RecvSerial:   recvSerial,
		SentExchange: normalizeOptional(&sentExchange),
		RecvExchange: normalizeOptional(&req.ExchangeRcvd),
		IsDupe:       isDupe,
	})
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to log contest QSO", "exchange insert failed")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to log contest QSO", "transaction failed")
		return
	}

	resp := contestQSOResponse{
		UUID:         qsoRow.Uuid.String(),
		Callsign:     qsoRow.Callsign,
		Band:         qsoRow.Band,
		Mode:         qsoRow.Mode,
		DatetimeOn:   qsoRow.DatetimeOn.Time.UTC(),
		RstSent:      qsoRow.RstSent,
		RstRcvd:      qsoRow.RstRcvd,
		FrequencyHz:  qsoRow.FrequencyHz,
		SentSerial:   exchRow.SentSerial,
		SentExchange: exchRow.SentExchange,
		RecvExchange: exchRow.RecvExchange,
		IsDupe:       exchRow.IsDupe,
	}

	_ = existingRow // referenced via isDupe
	status := http.StatusCreated
	msg := "contest QSO logged"
	if isDupe {
		status = http.StatusOK
		msg = "contest QSO logged (duplicate)"
	}
	writeSuccess(w, status, msg, resp)
}

// CheckDupe handles GET /v1/contests/{uuid}/check-dupe.
// Query params: callsign, band.
// Must return within 10ms — relies on idx_qsos_contest_dupe.
func (h *ContestHandler) CheckDupe(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	sessionUUID, err := parseContestUUID(r)
	if err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", err.Error())
		return
	}

	callsign := strings.TrimSpace(r.URL.Query().Get("callsign"))
	band := strings.TrimSpace(r.URL.Query().Get("band"))
	if callsign == "" || band == "" {
		writeFailure(w, http.StatusBadRequest, "invalid request", "callsign and band are required")
		return
	}

	tx, err := beginTenantTx(r.Context(), h.pool, userID)
	if err != nil {
		writeFailure(w, http.StatusServiceUnavailable, "database unavailable", "failed to prepare tenant context")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	queries := db.New(tx)
	row, err := queries.CheckDupe(r.Context(), db.CheckDupeParams{
		SessionUuid: sessionUUID,
		Callsign:    callsign,
		Band:        band,
	})

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "dupe check failed", "transaction failed")
		return
	}

	if errors.Is(err, pgx.ErrNoRows) {
		// No prior QSO — not a dupe.
		writeSuccess(w, http.StatusOK, "dupe check complete", dupeCheckResponse{Dupe: false})
		return
	}
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "dupe check failed", "query failed")
		return
	}

	// Found a prior non-dupe QSO — this callsign+band is already worked.
	prev := &dupeCheckPrevious{
		UUID:         row.QsoUuid.String(),
		Callsign:     row.Callsign,
		Band:         row.Band,
		Mode:         row.Mode,
		DatetimeOn:   row.DatetimeOn.Time.UTC(),
		SentSerial:   row.SentSerial,
		RecvExchange: row.RecvExchange,
	}
	writeSuccess(w, http.StatusOK, "dupe check complete", dupeCheckResponse{
		Dupe:        true,
		PreviousQSO: prev,
	})
}

// Stats handles GET /v1/contests/{uuid}/stats.
func (h *ContestHandler) Stats(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	sessionUUID, err := parseContestUUID(r)
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
	row, err := queries.GetContestStats(r.Context(), sessionUUID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeFailure(w, http.StatusOK, "contest session not found", "not found")
		return
	}
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to get contest stats", "query failed")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to get contest stats", "transaction failed")
		return
	}

	// Calculate rates: QSOs/hour from the windowed counts.
	var ratePerHour, ratePer10Min, ratePerMin float64
	if row.TotalQsos > 0 {
		ratePerHour = float64(row.RateLast60min)
		ratePer10Min = float64(row.RateLast10min) * 6.0
		ratePerMin = float64(row.RateLast1min) * 60.0
	}

	var firstQSOAt, lastQSOAt *time.Time
	if ts, ok := row.FirstQsoAt.(time.Time); ok && !ts.IsZero() {
		t := ts.UTC()
		firstQSOAt = &t
	}
	if ts, ok := row.LastQsoAt.(time.Time); ok && !ts.IsZero() {
		t := ts.UTC()
		lastQSOAt = &t
	}

	writeSuccess(w, http.StatusOK, "contest stats retrieved", contestStatsResponse{
		TotalQSOs:       row.TotalQsos,
		DupeQSOs:        row.DupeQsos,
		UniqueCallsigns: row.UniqueCallsigns,
		SerialCounter:   row.SerialCounter,
		RatePerHour:     ratePerHour,
		RatePer10Min:    ratePer10Min,
		RatePerMin:      ratePerMin,
		FirstQSOAt:      firstQSOAt,
		LastQSOAt:       lastQSOAt,
	})
}

// ─── Helpers ───────────────────────────────────────────────────────────────

func parseContestUUID(r *http.Request) (uuid.UUID, error) {
	raw := chi.URLParam(r, "contestUUID")
	parsed, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, fmt.Errorf("invalid contest session UUID")
	}
	return parsed, nil
}

func validateContestCreate(req contestCreateRequest) error {
	if strings.TrimSpace(req.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if strings.TrimSpace(req.ContestCode) == "" {
		return fmt.Errorf("contest_id is required")
	}
	return nil
}

func validateContestQSORequest(req contestQSORequest) error {
	if strings.TrimSpace(req.Callsign) == "" {
		return fmt.Errorf("callsign is required")
	}
	if strings.TrimSpace(req.Band) == "" {
		return fmt.Errorf("band is required")
	}
	if strings.TrimSpace(req.Mode) == "" {
		return fmt.Errorf("mode is required")
	}
	return nil
}

// defaultRSTForMode returns "599" for CW/RTTY/digital modes, "59" for voice modes.
func defaultRSTForMode(mode string) string {
	switch strings.ToUpper(strings.TrimSpace(mode)) {
	case "CW", "RTTY", "FT8", "FT4", "PSK31", "PSK63", "DIGI", "DATA", "OLIVIA", "MFSK", "JT65":
		return "599"
	default:
		return "59"
	}
}

// buildSentExchange assembles the full sent exchange string for a QSO.
func buildSentExchange(session db.GetContestSessionByUUIDRow, sentSerial *int32, req contestQSORequest) string {
	switch session.ExchangeTemplate {
	case "serial":
		if sentSerial != nil {
			prefix := ""
			if session.ExchangeSent != nil {
				prefix = strings.TrimSpace(*session.ExchangeSent) + " "
			}
			return fmt.Sprintf("%s%03d", prefix, *sentSerial)
		}
		return ""
	case "grid":
		if session.ExchangeSent != nil {
			return strings.TrimSpace(*session.ExchangeSent)
		}
		return ""
	default:
		if session.ExchangeSent != nil {
			return strings.TrimSpace(*session.ExchangeSent)
		}
		return ""
	}
}

// parseRecvSerial extracts the leading integer from an exchange string for serial-based contests.
func parseRecvSerial(exchange, template string) *int32 {
	if template != "serial" {
		return nil
	}
	trimmed := strings.TrimSpace(exchange)
	if trimmed == "" {
		return nil
	}
	var n int32
	if _, err := fmt.Sscanf(trimmed, "%d", &n); err != nil || n <= 0 {
		return nil
	}
	return &n
}

func toOptTimestamptz(t *time.Time) pgtype.Timestamptz {
	if t == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: t.UTC(), Valid: true}
}

func coalesceString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

// ─── Response mappers ──────────────────────────────────────────────────────

func contestSessionResponseFromCreate(row db.CreateContestSessionRow, logbookUUID uuid.UUID, contestCode, contestName string) contestSessionResponse {
	resp := contestSessionResponse{
		UUID:                row.Uuid.String(),
		LogbookUUID:         logbookUUID.String(),
		ContestCode:         contestCode,
		ContestName:         contestName,
		Name:                row.Name,
		ExchangeTemplate:    row.ExchangeTemplate,
		ExchangeSent:        row.ExchangeSent,
		Status:              row.Status,
		SerialCounter:       row.SerialCounter,
		CategoryOperator:    row.CategoryOperator,
		CategoryAssisted:    row.CategoryAssisted,
		CategoryBand:        row.CategoryBand,
		CategoryMode:        row.CategoryMode,
		CategoryPower:       row.CategoryPower,
		CategoryStation:     row.CategoryStation,
		CategoryTime:        row.CategoryTime,
		CategoryTransmitter: row.CategoryTransmitter,
		CategoryOverlay:     row.CategoryOverlay,
		OperatorsLine:       row.OperatorsLine,
		ClubName:            row.ClubName,
		Location:            row.Location,
		Soapbox:             row.Soapbox,
		CreatedAt:           row.CreatedAt.Time.UTC(),
		UpdatedAt:           row.UpdatedAt.Time.UTC(),
	}
	if row.StartsAt.Valid {
		t := row.StartsAt.Time.UTC()
		resp.StartsAt = &t
	}
	if row.EndsAt.Valid {
		t := row.EndsAt.Time.UTC()
		resp.EndsAt = &t
	}
	return resp
}

func contestSessionResponseFromGet(row db.GetContestSessionByUUIDRow) contestSessionResponse {
	resp := contestSessionResponse{
		UUID:                row.Uuid.String(),
		LogbookUUID:         row.LogbookUuid.String(),
		ContestCode:         row.ContestCode,
		ContestName:         row.ContestName,
		Name:                row.Name,
		MyCallsign:          row.MyCallsign,
		ExchangeTemplate:    row.ExchangeTemplate,
		ExchangeSent:        row.ExchangeSent,
		Status:              row.Status,
		SerialCounter:       row.SerialCounter,
		CategoryOperator:    row.CategoryOperator,
		CategoryAssisted:    row.CategoryAssisted,
		CategoryBand:        row.CategoryBand,
		CategoryMode:        row.CategoryMode,
		CategoryPower:       row.CategoryPower,
		CategoryStation:     row.CategoryStation,
		CategoryTime:        row.CategoryTime,
		CategoryTransmitter: row.CategoryTransmitter,
		CategoryOverlay:     row.CategoryOverlay,
		OperatorsLine:       row.OperatorsLine,
		ClubName:            row.ClubName,
		Location:            row.Location,
		Soapbox:             row.Soapbox,
		CreatedAt:           row.CreatedAt.Time.UTC(),
		UpdatedAt:           row.UpdatedAt.Time.UTC(),
	}
	if row.StartsAt.Valid {
		t := row.StartsAt.Time.UTC()
		resp.StartsAt = &t
	}
	if row.EndsAt.Valid {
		t := row.EndsAt.Time.UTC()
		resp.EndsAt = &t
	}
	return resp
}

func contestSessionResponseFromList(row db.ListContestSessionsRow) contestSessionResponse {
	resp := contestSessionResponse{
		UUID:             row.Uuid.String(),
		LogbookUUID:      row.LogbookUuid.String(),
		ContestCode:      row.ContestCode,
		ContestName:      row.ContestName,
		Name:             row.Name,
		MyCallsign:       row.MyCallsign,
		ExchangeTemplate: row.ExchangeTemplate,
		Status:           row.Status,
		SerialCounter:    row.SerialCounter,
		CategoryOperator: row.CategoryOperator,
		CategoryBand:     row.CategoryBand,
		CategoryMode:     row.CategoryMode,
		CategoryPower:    row.CategoryPower,
		CreatedAt:        row.CreatedAt.Time.UTC(),
	}
	if row.StartsAt.Valid {
		t := row.StartsAt.Time.UTC()
		resp.StartsAt = &t
	}
	if row.EndsAt.Valid {
		t := row.EndsAt.Time.UTC()
		resp.EndsAt = &t
	}
	return resp
}

func contestSessionResponseFromUpdate(row db.UpdateContestSessionRow) contestSessionResponse {
	resp := contestSessionResponse{
		UUID:                row.Uuid.String(),
		LogbookUUID:         fmt.Sprintf("%d", row.LogbookID),
		ExchangeTemplate:    row.ExchangeTemplate,
		ExchangeSent:        row.ExchangeSent,
		Status:              row.Status,
		SerialCounter:       row.SerialCounter,
		CategoryOperator:    row.CategoryOperator,
		CategoryAssisted:    row.CategoryAssisted,
		CategoryBand:        row.CategoryBand,
		CategoryMode:        row.CategoryMode,
		CategoryPower:       row.CategoryPower,
		CategoryStation:     row.CategoryStation,
		CategoryTime:        row.CategoryTime,
		CategoryTransmitter: row.CategoryTransmitter,
		CategoryOverlay:     row.CategoryOverlay,
		OperatorsLine:       row.OperatorsLine,
		ClubName:            row.ClubName,
		Location:            row.Location,
		Soapbox:             row.Soapbox,
		Name:                row.Name,
		CreatedAt:           row.CreatedAt.Time.UTC(),
		UpdatedAt:           row.UpdatedAt.Time.UTC(),
	}
	if row.StartsAt.Valid {
		t := row.StartsAt.Time.UTC()
		resp.StartsAt = &t
	}
	if row.EndsAt.Valid {
		t := row.EndsAt.Time.UTC()
		resp.EndsAt = &t
	}
	return resp
}
