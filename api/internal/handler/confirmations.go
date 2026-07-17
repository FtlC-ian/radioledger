package handler

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ConfirmationHandler handles HTTP requests for the QSO confirmation system.
type ConfirmationHandler struct {
	pool *pgxpool.Pool
}

// NewConfirmationHandler creates a ConfirmationHandler.
func NewConfirmationHandler(pool *pgxpool.Pool) *ConfirmationHandler {
	return &ConfirmationHandler{pool: pool}
}

// ─────────────────────────────────────────────────────────────────────────────
// Response types
// ─────────────────────────────────────────────────────────────────────────────

type confirmationResponse struct {
	ID                int64      `json:"id"`
	QSOID             int64      `json:"-"` // internal; not exposed
	MatchedQSOID      *int64     `json:"matched_qso_id,omitempty"`
	OurCallsign       string     `json:"our_callsign"`
	TheirCallsign     string     `json:"their_callsign"`
	Band              string     `json:"band"`
	Mode              string     `json:"mode"`
	QSODate           string     `json:"qso_date"`
	QSOTime           string     `json:"qso_time"`
	Status            string     `json:"status"`
	OurVerification   string     `json:"our_verification"`
	TheirVerification string     `json:"their_verification"`
	LoTWConfirmed     bool       `json:"lotw_confirmed"`
	LoTWConfirmedAt   *time.Time `json:"lotw_confirmed_at,omitempty"`
	EQSLConfirmed     bool       `json:"eqsl_confirmed"`
	EQSLConfirmedAt   *time.Time `json:"eqsl_confirmed_at,omitempty"`
	QRZConfirmed      bool       `json:"qrz_confirmed"`
	QRZConfirmedAt    *time.Time `json:"qrz_confirmed_at,omitempty"`
	RLConfirmed       bool       `json:"rl_confirmed"`
	RLConfirmedAt     *time.Time `json:"rl_confirmed_at,omitempty"`
	ConfirmedAt       *time.Time `json:"confirmed_at,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

type confirmationStatsResponse struct {
	Total        int64                `json:"total"`
	Confirmed    int64                `json:"confirmed"`
	Matched      int64                `json:"matched"`
	Pending      int64                `json:"pending"`
	Unconfirmed  int64                `json:"unconfirmed"`
	Rejected     int64                `json:"rejected"`
	Rate         float64              `json:"confirmation_rate"` // confirmed / total * 100
	BySources    confirmationSources  `json:"by_source"`
}

type confirmationSources struct {
	LoTW       int64 `json:"lotw"`
	EQSL       int64 `json:"eqsl"`
	QRZ        int64 `json:"qrz"`
	RLNative   int64 `json:"rl_native"`
}

// ─────────────────────────────────────────────────────────────────────────────
// GET /v1/confirmations
// List the authenticated user's confirmation records.
// Query params: status, band, date_from (RFC3339), date_to (RFC3339), limit, after (cursor id)
// ─────────────────────────────────────────────────────────────────────────────

func (h *ConfirmationHandler) List(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	q := r.URL.Query()
	statusFilter := strings.TrimSpace(q.Get("status"))
	bandFilter   := strings.TrimSpace(q.Get("band"))
	dateFrom     := strings.TrimSpace(q.Get("date_from"))
	dateTo       := strings.TrimSpace(q.Get("date_to"))
	limitStr     := q.Get("limit")
	afterStr     := q.Get("after") // cursor: last seen id

	limit := int64(parsePageSize(limitStr))
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	var afterID int64
	if afterStr != "" {
		afterID, _ = strconv.ParseInt(afterStr, 10, 64)
	}

	// Build the query dynamically based on filters.
	args := []any{userID, limit + 1}
	whereExtra := ""

	if statusFilter != "" {
		whereExtra += " AND qc.status = $" + strconv.Itoa(len(args)+1)
		args = append(args, statusFilter)
	}
	if bandFilter != "" {
		whereExtra += " AND qc.band = $" + strconv.Itoa(len(args)+1)
		args = append(args, bandFilter)
	}
	if dateFrom != "" {
		t, err := time.Parse(time.RFC3339, dateFrom)
		if err == nil {
			whereExtra += " AND qc.qso_date >= $" + strconv.Itoa(len(args)+1)
			args = append(args, t.UTC().Format("2006-01-02"))
		}
	}
	if dateTo != "" {
		t, err := time.Parse(time.RFC3339, dateTo)
		if err == nil {
			whereExtra += " AND qc.qso_date <= $" + strconv.Itoa(len(args)+1)
			args = append(args, t.UTC().Format("2006-01-02"))
		}
	}
	if afterID > 0 {
		whereExtra += " AND qc.id < $" + strconv.Itoa(len(args)+1)
		args = append(args, afterID)
	}

	sql := `
		SELECT
			qc.id, qc.qso_id, qc.matched_qso_id,
			qc.our_callsign, qc.their_callsign,
			qc.band, qc.mode,
			qc.qso_date::text, qc.qso_time::text,
			qc.status,
			qc.our_verification, qc.their_verification,
			qc.lotw_confirmed, qc.lotw_confirmed_at,
			qc.eqsl_confirmed, qc.eqsl_confirmed_at,
			qc.qrz_confirmed,  qc.qrz_confirmed_at,
			qc.rl_confirmed,   qc.rl_confirmed_at,
			qc.confirmed_at,
			qc.created_at, qc.updated_at
		FROM qso_confirmations qc
		JOIN qsos q ON q.id = qc.qso_id
		JOIN logbooks lb ON lb.id = q.logbook_id
		WHERE lb.user_id = $1` + whereExtra + `
		ORDER BY qc.id DESC
		LIMIT $2
	`

	rows, err := h.pool.Query(r.Context(), sql, args...)
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to list confirmations", "query failed")
		return
	}
	defer rows.Close()

	items := make([]confirmationResponse, 0)
	for rows.Next() {
		c, scanErr := scanConfirmation(rows)
		if scanErr != nil {
			writeFailure(w, http.StatusInternalServerError, "failed to list confirmations", "scan failed")
			return
		}
		items = append(items, c)
	}
	if err := rows.Err(); err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to list confirmations", "rows error")
		return
	}

	resp := map[string]any{"items": items}
	if int64(len(items)) > limit {
		items = items[:limit]
		resp["items"] = items
		resp["next_cursor"] = strconv.FormatInt(items[len(items)-1].ID, 10)
	}

	writeSuccess(w, http.StatusOK, "confirmations listed", resp)
}

// ─────────────────────────────────────────────────────────────────────────────
// GET /v1/confirmations/pending
// QSOs that have a match waiting but haven't been explicitly confirmed by us.
// ─────────────────────────────────────────────────────────────────────────────

func (h *ConfirmationHandler) Pending(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	limitStr := r.URL.Query().Get("limit")
	limit := int64(parsePageSize(limitStr))
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	rows, err := h.pool.Query(r.Context(), `
		SELECT
			qc.id, qc.qso_id, qc.matched_qso_id,
			qc.our_callsign, qc.their_callsign,
			qc.band, qc.mode,
			qc.qso_date::text, qc.qso_time::text,
			qc.status,
			qc.our_verification, qc.their_verification,
			qc.lotw_confirmed, qc.lotw_confirmed_at,
			qc.eqsl_confirmed, qc.eqsl_confirmed_at,
			qc.qrz_confirmed,  qc.qrz_confirmed_at,
			qc.rl_confirmed,   qc.rl_confirmed_at,
			qc.confirmed_at,
			qc.created_at, qc.updated_at
		FROM qso_confirmations qc
		JOIN qsos q ON q.id = qc.qso_id
		JOIN logbooks lb ON lb.id = q.logbook_id
		WHERE lb.user_id = $1
		  AND qc.status = 'matched'
		  AND qc.matched_qso_id IS NOT NULL
		ORDER BY qc.created_at DESC
		LIMIT $2
	`, userID, limit)
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to list pending confirmations", "query failed")
		return
	}
	defer rows.Close()

	items := make([]confirmationResponse, 0)
	for rows.Next() {
		c, scanErr := scanConfirmation(rows)
		if scanErr != nil {
			writeFailure(w, http.StatusInternalServerError, "failed to list pending confirmations", "scan failed")
			return
		}
		items = append(items, c)
	}
	if err := rows.Err(); err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to list pending confirmations", "rows error")
		return
	}

	writeSuccess(w, http.StatusOK, "pending confirmations listed", map[string]any{"items": items})
}

// ─────────────────────────────────────────────────────────────────────────────
// GET /v1/confirmations/stats
// ─────────────────────────────────────────────────────────────────────────────

func (h *ConfirmationHandler) Stats(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	row := h.pool.QueryRow(r.Context(), `
		SELECT
			COUNT(*)                                       AS total,
			COUNT(*) FILTER (WHERE qc.status = 'confirmed')   AS confirmed,
			COUNT(*) FILTER (WHERE qc.status = 'matched')     AS matched,
			COUNT(*) FILTER (WHERE qc.status = 'pending')     AS pending,
			COUNT(*) FILTER (WHERE qc.status = 'unconfirmed') AS unconfirmed,
			COUNT(*) FILTER (WHERE qc.status = 'rejected')    AS rejected,
			COUNT(*) FILTER (WHERE qc.lotw_confirmed = TRUE)  AS lotw,
			COUNT(*) FILTER (WHERE qc.eqsl_confirmed = TRUE)  AS eqsl,
			COUNT(*) FILTER (WHERE qc.qrz_confirmed  = TRUE)  AS qrz,
			COUNT(*) FILTER (WHERE qc.rl_confirmed   = TRUE)  AS rl_native
		FROM qso_confirmations qc
		JOIN qsos q ON q.id = qc.qso_id
		JOIN logbooks lb ON lb.id = q.logbook_id
		WHERE lb.user_id = $1
	`, userID)

	var stats confirmationStatsResponse
	if err := row.Scan(
		&stats.Total,
		&stats.Confirmed,
		&stats.Matched,
		&stats.Pending,
		&stats.Unconfirmed,
		&stats.Rejected,
		&stats.BySources.LoTW,
		&stats.BySources.EQSL,
		&stats.BySources.QRZ,
		&stats.BySources.RLNative,
	); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		writeFailure(w, http.StatusInternalServerError, "failed to get confirmation stats", "query failed")
		return
	}

	if stats.Total > 0 {
		stats.Rate = float64(stats.Confirmed) / float64(stats.Total) * 100.0
	}

	writeSuccess(w, http.StatusOK, "confirmation stats retrieved", stats)
}

// ─────────────────────────────────────────────────────────────────────────────
// POST /v1/confirmations/{id}/confirm
// Manually confirm a matched QSO.
// ─────────────────────────────────────────────────────────────────────────────

func (h *ConfirmationHandler) Confirm(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	confID, err := parseConfirmationID(r)
	if err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", err.Error())
		return
	}

	tag, err := h.pool.Exec(r.Context(), `
		UPDATE qso_confirmations qc
		SET
			status        = 'confirmed',
			rl_confirmed  = TRUE,
			rl_confirmed_at = now(),
			confirmed_at  = COALESCE(qc.confirmed_at, now()),
			updated_at    = now()
		FROM qsos q
		JOIN logbooks lb ON lb.id = q.logbook_id
		WHERE qc.id = $1
		  AND qc.qso_id = q.id
		  AND lb.user_id = $2
		  AND qc.status IN ('matched', 'pending')
	`, confID, userID)
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to confirm qso", "update failed")
		return
	}
	if tag.RowsAffected() == 0 {
		writeFailure(w, http.StatusOK, "confirmation not found or not confirmable",
			"confirmation not found, already confirmed/rejected, or does not belong to you")
		return
	}

	writeSuccess(w, http.StatusOK, "qso confirmed", map[string]any{"id": confID})
}

// ─────────────────────────────────────────────────────────────────────────────
// POST /v1/confirmations/{id}/reject
// Dispute a matched QSO.
// ─────────────────────────────────────────────────────────────────────────────

func (h *ConfirmationHandler) Reject(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	confID, err := parseConfirmationID(r)
	if err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", err.Error())
		return
	}

	tag, err := h.pool.Exec(r.Context(), `
		UPDATE qso_confirmations qc
		SET
			status     = 'rejected',
			updated_at = now()
		FROM qsos q
		JOIN logbooks lb ON lb.id = q.logbook_id
		WHERE qc.id = $1
		  AND qc.qso_id = q.id
		  AND lb.user_id = $2
		  AND qc.status NOT IN ('rejected', 'confirmed')
	`, confID, userID)
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to reject qso", "update failed")
		return
	}
	if tag.RowsAffected() == 0 {
		writeFailure(w, http.StatusOK, "confirmation not found or not rejectable",
			"confirmation not found, already confirmed/rejected, or does not belong to you")
		return
	}

	writeSuccess(w, http.StatusOK, "qso rejected", map[string]any{"id": confID})
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

func parseConfirmationID(r *http.Request) (int64, error) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		return 0, errors.New("invalid confirmation id")
	}
	return id, nil
}

// pgxScanner is a minimal interface for both pgx.Rows and pgx.Row.
type pgxScanner interface {
	Scan(dest ...any) error
}

func scanConfirmation(rows pgxScanner) (confirmationResponse, error) {
	var c confirmationResponse
	var (
		lotwAt, eqslAt, qrzAt, rlAt, confAt *time.Time
		createdAt, updatedAt                 time.Time
	)
	err := rows.Scan(
		&c.ID, &c.QSOID, &c.MatchedQSOID,
		&c.OurCallsign, &c.TheirCallsign,
		&c.Band, &c.Mode,
		&c.QSODate, &c.QSOTime,
		&c.Status,
		&c.OurVerification, &c.TheirVerification,
		&c.LoTWConfirmed, &lotwAt,
		&c.EQSLConfirmed, &eqslAt,
		&c.QRZConfirmed, &qrzAt,
		&c.RLConfirmed, &rlAt,
		&confAt,
		&createdAt, &updatedAt,
	)
	if err != nil {
		return confirmationResponse{}, err
	}
	c.LoTWConfirmedAt = lotwAt
	c.EQSLConfirmedAt = eqslAt
	c.QRZConfirmedAt  = qrzAt
	c.RLConfirmedAt   = rlAt
	c.ConfirmedAt     = confAt
	c.CreatedAt       = createdAt
	c.UpdatedAt       = updatedAt
	return c, nil
}
