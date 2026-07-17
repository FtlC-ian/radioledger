package handler

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	db "github.com/FtlC-ian/radioledger/api/internal/database/sqlc"
)

const (
	defaultPSKReportLimit = 50
	maxPSKReportLimit     = 200

	// qsoMatchWindowMinutes is the time window around a QSO datetime used to
	// find matching PSK reception reports (±15 minutes).
	qsoMatchWindowMinutes = 15
)

// PSKReporterHandler handles requests to the /v1/pskreporter/ endpoints.
type PSKReporterHandler struct {
	pool *pgxpool.Pool
}

// NewPSKReporterHandler creates a new PSKReporterHandler.
func NewPSKReporterHandler(pool *pgxpool.Pool) *PSKReporterHandler {
	return &PSKReporterHandler{pool: pool}
}

// ─── Response types ───────────────────────────────────────────────────────────

type pskReportResponse struct {
	ID               int64    `json:"id"`
	SenderCallsign   string   `json:"sender_callsign"`
	ReceiverCallsign string   `json:"receiver_callsign"`
	FrequencyKHz     *string  `json:"frequency_khz,omitempty"`
	Mode             *string  `json:"mode,omitempty"`
	SNR              *int16   `json:"snr,omitempty"`
	Grid             *string  `json:"grid,omitempty"`
	SpottedAt        string   `json:"spotted_at"`
}

// ─── psk cursor ───────────────────────────────────────────────────────────────

type pskCursor struct {
	SpottedAt time.Time
	ID        int64
}

func encodePSKCursor(c pskCursor) string {
	raw := fmt.Sprintf("%s|%d", c.SpottedAt.UTC().Format(time.RFC3339Nano), c.ID)
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodePSKCursor(value string) (*pskCursor, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return nil, fmt.Errorf("invalid cursor")
	}
	parts := strings.SplitN(string(raw), "|", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid cursor format")
	}
	t, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return nil, fmt.Errorf("invalid cursor timestamp")
	}
	id, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid cursor id")
	}
	return &pskCursor{SpottedAt: t.UTC(), ID: id}, nil
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func numericToString(n pgtype.Numeric) *string {
	if !n.Valid {
		return nil
	}
	f, err := n.Float64Value()
	if err != nil || !f.Valid {
		return nil
	}
	s := strconv.FormatFloat(f.Float64, 'f', 3, 64)
	return &s
}

func pskReportRow(r db.PskReceptionReport) pskReportResponse {
	resp := pskReportResponse{
		ID:               r.ID,
		SenderCallsign:   r.SenderCallsign,
		ReceiverCallsign: r.ReceiverCallsign,
		FrequencyKHz:     numericToString(r.FrequencyKhz),
		Mode:             r.Mode,
		SNR:              r.Snr,
		Grid:             r.Grid,
		SpottedAt:        r.SpottedAt.Time.UTC().Format(time.RFC3339),
	}
	return resp
}

// ─── Handlers ────────────────────────────────────────────────────────────────

// ListReports handles GET /v1/pskreporter/reports
// Returns a paginated list of PSK reception reports for the authenticated user.
func (h *PSKReporterHandler) ListReports(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	limit := parsePSKLimit(r.URL.Query().Get("limit"))

	c, err := decodePSKCursor(r.URL.Query().Get("after"))
	if err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid cursor", err.Error())
		return
	}

	tx, err := beginTenantTx(r.Context(), h.pool, userID)
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "database error", err.Error())
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()

	queries := db.New(tx)

	params := db.ListPSKReceptionReportsParams{
		UserID:     userID,
		LimitCount: int32(limit),
	}
	if c != nil {
		params.CursorSpottedAt = pgtype.Timestamptz{Time: c.SpottedAt, Valid: true}
		params.CursorID = &c.ID
	}

	rows, err := queries.ListPSKReceptionReports(r.Context(), params)
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to list PSK reports", err.Error())
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "database error", err.Error())
		return
	}

	reports := make([]pskReportResponse, 0, len(rows))
	for _, row := range rows {
		reports = append(reports, pskReportRow(row))
	}

	response := map[string]any{"reports": reports}
	if len(rows) == limit {
		last := rows[len(rows)-1]
		response["next_cursor"] = encodePSKCursor(pskCursor{
			SpottedAt: last.SpottedAt.Time.UTC(),
			ID:        last.ID,
		})
	}

	writeSuccess(w, http.StatusOK, "", response)
}

// MatchQSO handles GET /v1/pskreporter/reports/match/:qso_id
// Finds PSK reception reports that match a specific QSO by callsign and time window.
func (h *PSKReporterHandler) MatchQSO(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	qsoUUIDStr := chi.URLParam(r, "qso_id")
	qsoUUID, err := uuid.Parse(qsoUUIDStr)
	if err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid qso_id", "must be a valid UUID")
		return
	}

	tx, err := beginTenantTx(r.Context(), h.pool, userID)
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "database error", err.Error())
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()

	queries := db.New(tx)

	// Look up the QSO to get callsign and datetime.
	qso, err := queries.GetQSOForPSKMatch(r.Context(), qsoUUID)
	if err != nil {
		writeFailure(w, http.StatusNotFound, "QSO not found", "no QSO with that ID exists or you do not have access")
		return
	}

	qsoTime := qso.DatetimeOn.Time.UTC()
	windowStart := qsoTime.Add(-qsoMatchWindowMinutes * time.Minute)
	windowEnd := qsoTime.Add(qsoMatchWindowMinutes * time.Minute)

	matchRows, err := queries.ListPSKReportsByCallsignAndWindow(r.Context(), db.ListPSKReportsByCallsignAndWindowParams{
		UserID:         userID,
		SenderCallsign: qso.Callsign,
		WindowStart:    pgtype.Timestamptz{Time: windowStart, Valid: true},
		WindowEnd:      pgtype.Timestamptz{Time: windowEnd, Valid: true},
		LimitCount:     maxPSKReportLimit,
	})
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to find matching PSK reports", err.Error())
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "database error", err.Error())
		return
	}

	reports := make([]pskReportResponse, 0, len(matchRows))
	for _, row := range matchRows {
		reports = append(reports, pskReportRow(row))
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"data": map[string]any{
			"qso_uuid":     qso.Uuid.String(),
			"callsign":     qso.Callsign,
			"band":         qso.Band,
			"mode":         qso.Mode,
			"datetime_on":  qsoTime.Format(time.RFC3339),
			"window_start": windowStart.Format(time.RFC3339),
			"window_end":   windowEnd.Format(time.RFC3339),
			"reports":      reports,
		},
	})
}

// parsePSKLimit parses and clamps the limit query parameter.
func parsePSKLimit(raw string) int {
	if raw == "" {
		return defaultPSKReportLimit
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return defaultPSKReportLimit
	}
	if n > maxPSKReportLimit {
		return maxPSKReportLimit
	}
	return n
}


