package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"log/slog"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/FtlC-ian/radioledger/api/internal/auth"
	db "github.com/FtlC-ian/radioledger/api/internal/database/sqlc"
	"github.com/FtlC-ian/radioledger/api/internal/metrics"
	confirmsvc "github.com/FtlC-ian/radioledger/api/internal/services/confirmation"
	"github.com/FtlC-ian/radioledger/api/internal/services/qsoenrich"
	syncsvc "github.com/FtlC-ian/radioledger/api/internal/services/sync"
)

// QSOHandler handles HTTP requests for QSO resources.
type QSOHandler struct {
	pool        *pgxpool.Pool
	riverClient *river.Client[pgx.Tx]
}

// NewQSOHandler creates a QSOHandler with its dependencies.
// riverClient is optional; if nil, auto-sync is disabled.
func NewQSOHandler(pool *pgxpool.Pool) *QSOHandler {
	return &QSOHandler{pool: pool}
}

// NewQSOHandlerWithSync creates a QSOHandler with River client for auto-sync.
func NewQSOHandlerWithSync(pool *pgxpool.Pool, riverClient *river.Client[pgx.Tx]) *QSOHandler {
	return &QSOHandler{pool: pool, riverClient: riverClient}
}

type qsoUpsertRequest struct {
	Callsign     string    `json:"callsign"`
	Name         *string   `json:"name,omitempty"`
	Qth          *string   `json:"qth,omitempty"`
	Band         string    `json:"band"`
	Mode         string    `json:"mode"`
	Submode      *string   `json:"submode,omitempty"`
	FrequencyHz  *int64    `json:"frequency_hz,omitempty"`
	DatetimeOn   time.Time `json:"datetime_on"`
	RstSent      *string   `json:"rst_sent"`
	RstRcvd      *string   `json:"rst_rcvd"`
	TxPower      *float64  `json:"tx_power,omitempty"`
	Gridsquare   *string   `json:"gridsquare"`
	MyGridsquare *string   `json:"my_gridsquare,omitempty"`
	Dxcc         *int32    `json:"dxcc"`
	Comment      *string   `json:"comment"`
	Notes        *string   `json:"notes"`
}

type qsoPatchRequest struct {
	Callsign     *string    `json:"callsign"`
	Name         *string    `json:"name,omitempty"`
	Qth          *string    `json:"qth,omitempty"`
	Band         *string    `json:"band"`
	Mode         *string    `json:"mode"`
	Submode      *string    `json:"submode,omitempty"`
	FrequencyHz  *int64     `json:"frequency_hz,omitempty"`
	DatetimeOn   *time.Time `json:"datetime_on"`
	RstSent      *string    `json:"rst_sent"`
	RstRcvd      *string    `json:"rst_rcvd"`
	TxPower      *float64   `json:"tx_power,omitempty"`
	Gridsquare   *string    `json:"gridsquare"`
	MyGridsquare *string    `json:"my_gridsquare,omitempty"`
	Dxcc         *int32     `json:"dxcc"`
	Comment      *string    `json:"comment"`
	Notes        *string    `json:"notes"`
}

type qsoResponse struct {
	UUID         string    `json:"uuid"`
	LogbookUUID  string    `json:"logbook_uuid"`
	Callsign     string    `json:"callsign"`
	Name         *string   `json:"name,omitempty"`
	QTH          *string   `json:"qth,omitempty"`
	Band         string    `json:"band"`
	Mode         string    `json:"mode"`
	Submode      *string   `json:"submode,omitempty"`
	FrequencyHz  *int64    `json:"frequency_hz,omitempty"`
	DatetimeOn   time.Time `json:"datetime_on"`
	RstSent      *string   `json:"rst_sent,omitempty"`
	RstRcvd      *string   `json:"rst_rcvd,omitempty"`
	TxPower      *float64  `json:"tx_power,omitempty"`
	Gridsquare   *string   `json:"gridsquare,omitempty"`
	MyGridsquare *string   `json:"my_gridsquare,omitempty"`
	Dxcc         *int32    `json:"dxcc,omitempty"`
	Country      *string   `json:"country,omitempty"`
	CQZone       *int16    `json:"cq_zone,omitempty"`
	ITUZone      *int16    `json:"itu_zone,omitempty"`
	Continent    *string   `json:"continent,omitempty"`
	Comment      *string   `json:"comment,omitempty"`
	Notes        *string   `json:"notes,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (h *QSOHandler) List(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	logbookUUID, err := uuid.Parse(chi.URLParam(r, "logbookUUID"))
	if err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", "invalid logbook UUID")
		return
	}

	c, err := decodeCursor(r.URL.Query().Get("after"))
	if err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", err.Error())
		return
	}

	limit := parsePageSize(r.URL.Query().Get("limit"))

	tx, err := beginTenantTx(r.Context(), h.pool, userID)
	if err != nil {
		writeFailure(w, http.StatusServiceUnavailable, "database unavailable", "failed to prepare tenant context")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	queries := db.New(tx)

	var rows []db.SearchQSOsRow
	if hasSearchFilters(r) {
		params, searchErr := buildSearchParams(r, logbookUUID, c, limit)
		if searchErr != nil {
			writeFailure(w, http.StatusBadRequest, "invalid request", searchErr.Error())
			return
		}

		rows, err = queries.SearchQSOs(r.Context(), params)
	} else {
		params := db.ListQSOsByLogbookParams{
			LogbookUuid: logbookUUID,
			PageSize:    limit,
		}
		if c != nil {
			params.CursorDatetime = pgtype.Timestamptz{Time: c.DatetimeOn.UTC(), Valid: true}
			params.CursorID = &c.ID
		}

		listRows, listErr := queries.ListQSOsByLogbook(r.Context(), params)
		if listErr != nil {
			err = listErr
		} else {
			rows = make([]db.SearchQSOsRow, 0, len(listRows))
			for _, row := range listRows {
				rows = append(rows, db.SearchQSOsRow(row))
			}
		}
	}
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to list qsos", "query failed")
		return
	}

	items := make([]qsoResponse, 0, len(rows))
	for _, row := range rows {
		items = append(items, qsoFromSearchRow(row))
	}

	response := map[string]any{"items": items}
	if len(rows) == int(limit) {
		last := rows[len(rows)-1]
		response["next_cursor"] = encodeCursor(cursor{
			DatetimeOn: last.DatetimeOn.Time.UTC(),
			ID:         last.ID,
		})
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to list qsos", "transaction failed")
		return
	}

	writeSuccess(w, http.StatusOK, "qsos listed", response)
}

func (h *QSOHandler) Get(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	logbookUUID, qsoUUID, err := parseQSOPathParams(r)
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
	row, err := queries.GetQSOByUUID(r.Context(), db.GetQSOByUUIDParams{
		QsoUuid:     qsoUUID,
		LogbookUuid: logbookUUID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeFailure(w, http.StatusOK, "qso not found", "qso not found")
		return
	}
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to fetch qso", "query failed")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to fetch qso", "transaction failed")
		return
	}

	writeSuccess(w, http.StatusOK, "qso retrieved", qsoFromGetRow(row))
}

func (h *QSOHandler) Create(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	logbookUUID, err := uuid.Parse(chi.URLParam(r, "logbookUUID"))
	if err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", "invalid logbook UUID")
		return
	}

	var req qsoUpsertRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", "invalid JSON body")
		return
	}
	if err := validateQSOUpsert(req); err != nil {
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
	row, err := queries.CreateQSO(r.Context(), db.CreateQSOParams{
		CreatedByUserID: &userID,
		Callsign:        strings.ToUpper(strings.TrimSpace(req.Callsign)),
		Name:            normalizeOptional(req.Name),
		Qth:             normalizeOptional(req.Qth),
		Band:            strings.TrimSpace(req.Band),
		Mode:            strings.TrimSpace(req.Mode),
		Submode:         normalizeOptional(req.Submode),
		FrequencyHz:     req.FrequencyHz,
		DatetimeOn:      pgtype.Timestamptz{Time: req.DatetimeOn.UTC(), Valid: true},
		RstSent:         normalizeOptional(req.RstSent),
		RstRcvd:         normalizeOptional(req.RstRcvd),
		TxPower:         float64ToNumeric(req.TxPower),
		Gridsquare:      normalizeOptional(req.Gridsquare),
		MyGridsquare:    normalizeOptional(req.MyGridsquare),
		Dxcc:            req.Dxcc,
		Comment:         normalizeOptional(req.Comment),
		Notes:           normalizeOptional(req.Notes),
		LogbookUuid:     logbookUUID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeFailure(w, http.StatusOK, "logbook not found", "logbook not found")
		return
	}
	if err != nil {
		slog.ErrorContext(r.Context(), "qso create insert failed",
			slog.String("error", err.Error()),
			slog.String("logbook_uuid", logbookUUID.String()),
			slog.Int64("user_id", userID),
			slog.String("callsign", strings.ToUpper(strings.TrimSpace(req.Callsign))),
			slog.String("band", strings.TrimSpace(req.Band)),
			slog.String("mode", strings.TrimSpace(req.Mode)),
		)
		writeFailure(w, http.StatusInternalServerError, "failed to create qso", "insert failed")
		return
	}

	if _, err := qsoenrich.EnrichOneFromCallsignRecords(r.Context(), tx, row.ID); err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to create qso", "enrichment failed")
		return
	}

	refreshed, err := queries.GetQSOByUUID(r.Context(), db.GetQSOByUUIDParams{
		QsoUuid:     row.Uuid,
		LogbookUuid: logbookUUID,
	})
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to create qso", "query failed")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to create qso", "transaction failed")
		return
	}

	metrics.IncQSOsLogged(1)

	// Auto-sync: queue sync jobs for all enabled services.
	// This runs after commit so the QSO is visible to sync workers.
	h.triggerAutoSync(r.Context(), userID, row.ID, false)

	// Confirmation matching: enqueue async job to find matching QSO from the other side.
	h.triggerQSOMatch(r.Context(), userID, row.ID)

	writeSuccess(w, http.StatusCreated, "qso created", qsoFromGetRow(refreshed))
}

func (h *QSOHandler) Update(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	logbookUUID, qsoUUID, err := parseQSOPathParams(r)
	if err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", err.Error())
		return
	}

	var req qsoUpsertRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", "invalid JSON body")
		return
	}
	if err := validateQSOUpsert(req); err != nil {
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
	if err := ensureContributorCanMutateQSO(r.Context(), queries, userID, logbookUUID, qsoUUID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeFailure(w, http.StatusOK, "qso not found", "qso not found")
			return
		}
		if errors.Is(err, errContributorCannotMutateOtherQSO) {
			writeFailure(w, http.StatusForbidden, "forbidden", err.Error())
			return
		}
		writeFailure(w, http.StatusInternalServerError, "failed to update qso", "authorization check failed")
		return
	}

	row, err := queries.UpdateQSO(r.Context(), db.UpdateQSOParams{
		Callsign:     strings.ToUpper(strings.TrimSpace(req.Callsign)),
		Name:         normalizeOptional(req.Name),
		Qth:          normalizeOptional(req.Qth),
		Band:         strings.TrimSpace(req.Band),
		Mode:         strings.TrimSpace(req.Mode),
		Submode:      normalizeOptional(req.Submode),
		FrequencyHz:  req.FrequencyHz,
		DatetimeOn:   pgtype.Timestamptz{Time: req.DatetimeOn.UTC(), Valid: true},
		RstSent:      normalizeOptional(req.RstSent),
		RstRcvd:      normalizeOptional(req.RstRcvd),
		TxPower:      float64ToNumeric(req.TxPower),
		Gridsquare:   normalizeOptional(req.Gridsquare),
		MyGridsquare: normalizeOptional(req.MyGridsquare),
		Dxcc:         req.Dxcc,
		Comment:      normalizeOptional(req.Comment),
		Notes:        normalizeOptional(req.Notes),
		QsoUuid:      qsoUUID,
		LogbookUuid:  logbookUUID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeFailure(w, http.StatusOK, "qso not found", "qso not found")
		return
	}
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to update qso", "update failed")
		return
	}

	if _, err := qsoenrich.EnrichOneFromCallsignRecords(r.Context(), tx, row.ID); err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to update qso", "enrichment failed")
		return
	}

	servicesToResync, err := resetQSOSyncStatus(r.Context(), tx, row.ID)
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to update qso", "sync status reset failed")
		return
	}

	if err := queries.MarkUserAwardsDirty(r.Context(), userID); err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to update qso", "award refresh scheduling failed")
		return
	}

	refreshed, err := queries.GetQSOByUUID(r.Context(), db.GetQSOByUUIDParams{
		QsoUuid:     row.Uuid,
		LogbookUuid: logbookUUID,
	})
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to update qso", "query failed")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to update qso", "transaction failed")
		return
	}

	h.enqueueSyncJobs(r.Context(), userID, servicesToResync, "qso update")

	writeSuccess(w, http.StatusOK, "qso updated", qsoFromGetRow(refreshed))
}

func (h *QSOHandler) Patch(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	logbookUUID, qsoUUID, err := parseQSOPathParams(r)
	if err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", err.Error())
		return
	}

	var req qsoPatchRequest
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
	queries := db.New(tx)
	if err := ensureContributorCanMutateQSO(r.Context(), queries, userID, logbookUUID, qsoUUID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeFailure(w, http.StatusOK, "qso not found", "qso not found")
			return
		}
		if errors.Is(err, errContributorCannotMutateOtherQSO) {
			writeFailure(w, http.StatusForbidden, "forbidden", err.Error())
			return
		}
		writeFailure(w, http.StatusInternalServerError, "failed to update qso", "authorization check failed")
		return
	}

	existing, err := queries.GetQSOByUUID(r.Context(), db.GetQSOByUUIDParams{
		QsoUuid:     qsoUUID,
		LogbookUuid: logbookUUID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeFailure(w, http.StatusOK, "qso not found", "qso not found")
		return
	}
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to update qso", "query failed")
		return
	}

	upsert := qsoUpsertRequest{
		Callsign:     existing.Callsign,
		Name:         existing.Name,
		Qth:          existing.Qth,
		Band:         existing.Band,
		Mode:         existing.Mode,
		Submode:      existing.Submode,
		FrequencyHz:  existing.FrequencyHz,
		DatetimeOn:   existing.DatetimeOn.Time,
		RstSent:      existing.RstSent,
		RstRcvd:      existing.RstRcvd,
		TxPower:      numericToFloat64(existing.TxPower),
		Gridsquare:   existing.Gridsquare,
		MyGridsquare: existing.MyGridsquare,
		Dxcc:         existing.Dxcc,
		Comment:      existing.Comment,
		Notes:        existing.Notes,
	}

	if req.Callsign != nil {
		upsert.Callsign = *req.Callsign
	}
	if req.Name != nil {
		upsert.Name = req.Name
	}
	if req.Qth != nil {
		upsert.Qth = req.Qth
	}
	if req.Band != nil {
		upsert.Band = *req.Band
	}
	if req.Mode != nil {
		upsert.Mode = *req.Mode
	}
	if req.Submode != nil {
		upsert.Submode = req.Submode
	}
	if req.FrequencyHz != nil {
		upsert.FrequencyHz = req.FrequencyHz
	}
	if req.DatetimeOn != nil {
		upsert.DatetimeOn = req.DatetimeOn.UTC()
	}
	if req.RstSent != nil {
		upsert.RstSent = req.RstSent
	}
	if req.RstRcvd != nil {
		upsert.RstRcvd = req.RstRcvd
	}
	if req.TxPower != nil {
		upsert.TxPower = req.TxPower
	}
	if req.Gridsquare != nil {
		upsert.Gridsquare = req.Gridsquare
	}
	if req.MyGridsquare != nil {
		upsert.MyGridsquare = req.MyGridsquare
	}
	if req.Dxcc != nil {
		upsert.Dxcc = req.Dxcc
	}
	if req.Comment != nil {
		upsert.Comment = req.Comment
	}
	if req.Notes != nil {
		upsert.Notes = req.Notes
	}

	if err := validateQSOUpsert(upsert); err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", err.Error())
		return
	}

	row, err := queries.UpdateQSO(r.Context(), db.UpdateQSOParams{
		Callsign:     strings.ToUpper(strings.TrimSpace(upsert.Callsign)),
		Name:         normalizeOptional(upsert.Name),
		Qth:          normalizeOptional(upsert.Qth),
		Band:         strings.TrimSpace(upsert.Band),
		Mode:         strings.TrimSpace(upsert.Mode),
		Submode:      normalizeOptional(upsert.Submode),
		FrequencyHz:  upsert.FrequencyHz,
		DatetimeOn:   pgtype.Timestamptz{Time: upsert.DatetimeOn.UTC(), Valid: true},
		RstSent:      normalizeOptional(upsert.RstSent),
		RstRcvd:      normalizeOptional(upsert.RstRcvd),
		TxPower:      float64ToNumeric(upsert.TxPower),
		Gridsquare:   normalizeOptional(upsert.Gridsquare),
		MyGridsquare: normalizeOptional(upsert.MyGridsquare),
		Dxcc:         upsert.Dxcc,
		Comment:      normalizeOptional(upsert.Comment),
		Notes:        normalizeOptional(upsert.Notes),
		QsoUuid:      qsoUUID,
		LogbookUuid:  logbookUUID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeFailure(w, http.StatusOK, "qso not found", "qso not found")
		return
	}
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to update qso", "update failed")
		return
	}

	if _, err := qsoenrich.EnrichOneFromCallsignRecords(r.Context(), tx, row.ID); err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to update qso", "enrichment failed")
		return
	}

	servicesToResync, err := resetQSOSyncStatus(r.Context(), tx, row.ID)
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to update qso", "sync status reset failed")
		return
	}

	if err := queries.MarkUserAwardsDirty(r.Context(), userID); err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to update qso", "award refresh scheduling failed")
		return
	}

	refreshed, err := queries.GetQSOByUUID(r.Context(), db.GetQSOByUUIDParams{
		QsoUuid:     row.Uuid,
		LogbookUuid: logbookUUID,
	})
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to update qso", "query failed")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to update qso", "transaction failed")
		return
	}

	h.enqueueSyncJobs(r.Context(), userID, servicesToResync, "qso patch")

	writeSuccess(w, http.StatusOK, "qso updated", qsoFromGetRow(refreshed))
}

func (h *QSOHandler) Delete(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	logbookUUID, qsoUUID, err := parseQSOPathParams(r)
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
	if err := ensureContributorCanMutateQSO(r.Context(), queries, userID, logbookUUID, qsoUUID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeFailure(w, http.StatusOK, "qso not found", "qso not found")
			return
		}
		if errors.Is(err, errContributorCannotMutateOtherQSO) {
			writeFailure(w, http.StatusForbidden, "forbidden", err.Error())
			return
		}
		writeFailure(w, http.StatusInternalServerError, "failed to delete qso", "authorization check failed")
		return
	}

	deletedQSO, err := queries.GetQSOByUUID(r.Context(), db.GetQSOByUUIDParams{
		QsoUuid:     qsoUUID,
		LogbookUuid: logbookUUID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeFailure(w, http.StatusOK, "qso not found", "qso not found")
		return
	}
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to delete qso", "query failed")
		return
	}

	if err := queries.DeleteQSO(r.Context(), db.DeleteQSOParams{
		QsoUuid:     qsoUUID,
		LogbookUuid: logbookUUID,
	}); err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to delete qso", "delete failed")
		return
	}

	if _, err := tx.Exec(r.Context(), `DELETE FROM sync_status WHERE qso_id = $1`, deletedQSO.ID); err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to delete qso", "sync cleanup failed")
		return
	}

	if err := queries.MarkUserAwardsDirty(r.Context(), userID); err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to delete qso", "award refresh scheduling failed")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to delete qso", "transaction failed")
		return
	}

	// Auto-sync: queue Club Log delete job (eQSL has no delete API).
	// Use context.WithoutCancel so the goroutine outlives the HTTP request while
	// preserving any tracing/logging values from the original context.
	if h.riverClient != nil && deletedQSO.DatetimeOn.Valid {
		bgCtx := context.WithoutCancel(r.Context())
		go func() {
			_ = syncsvc.EnqueueClubLogDelete(bgCtx, h.riverClient, userID,
				deletedQSO.Callsign, deletedQSO.Band, deletedQSO.Mode,
				deletedQSO.DatetimeOn.Time.UTC().Format(time.RFC3339))
		}()
	}

	writeSuccess(w, http.StatusOK, "qso deleted", map[string]string{"uuid": qsoUUID.String()})
}

var errContributorCannotMutateOtherQSO = errors.New("contributors can only edit or delete their own QSOs")

func ensureContributorCanMutateQSO(ctx context.Context, queries *db.Queries, userID int64, logbookUUID, qsoUUID uuid.UUID) error {
	roleValue, err := queries.GetUserRoleForLogbook(ctx, db.GetUserRoleForLogbookParams{
		LogbookUuid: logbookUUID,
		UserID:      userID,
	})
	if err != nil {
		return err
	}

	role, ok := auth.ParseRole(roleValue)
	if !ok {
		return fmt.Errorf("unknown role %q", roleValue)
	}
	if role != auth.RoleContributor {
		return nil
	}

	createdBy, err := queries.GetQSOCreatorByUUID(ctx, db.GetQSOCreatorByUUIDParams{
		QsoUuid:     qsoUUID,
		LogbookUuid: logbookUUID,
	})
	if err != nil {
		return err
	}
	if createdBy == nil || *createdBy != userID {
		return errContributorCannotMutateOtherQSO
	}

	return nil
}

func parseQSOPathParams(r *http.Request) (uuid.UUID, uuid.UUID, error) {
	logbookUUID, err := uuid.Parse(chi.URLParam(r, "logbookUUID"))
	if err != nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("invalid logbook UUID")
	}
	qsoUUID, err := uuid.Parse(chi.URLParam(r, "qsoUUID"))
	if err != nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("invalid qso UUID")
	}
	return logbookUUID, qsoUUID, nil
}

func validateQSOUpsert(req qsoUpsertRequest) error {
	if strings.TrimSpace(req.Callsign) == "" {
		return fmt.Errorf("callsign is required")
	}
	if strings.TrimSpace(req.Band) == "" {
		return fmt.Errorf("band is required")
	}
	if strings.TrimSpace(req.Mode) == "" {
		return fmt.Errorf("mode is required")
	}
	if req.DatetimeOn.IsZero() {
		return fmt.Errorf("datetime_on is required")
	}
	return nil
}

func hasSearchFilters(r *http.Request) bool {
	q := r.URL.Query()
	for _, key := range []string{"callsign", "band", "mode", "date_from", "date_to", "dxcc", "gridsquare"} {
		if strings.TrimSpace(q.Get(key)) != "" {
			return true
		}
	}
	return false
}

func buildSearchParams(r *http.Request, logbookUUID uuid.UUID, c *cursor, limit int32) (db.SearchQSOsParams, error) {
	q := r.URL.Query()
	params := db.SearchQSOsParams{
		LogbookUuid: logbookUUID,
		PageSize:    limit,
	}

	if c != nil {
		params.CursorDatetime = pgtype.Timestamptz{Time: c.DatetimeOn.UTC(), Valid: true}
		params.CursorID = &c.ID
	}

	if v := strings.TrimSpace(q.Get("callsign")); v != "" {
		params.CallsignFilter = &v
	}
	if v := strings.TrimSpace(q.Get("band")); v != "" {
		params.BandFilter = &v
	}
	if v := strings.TrimSpace(q.Get("mode")); v != "" {
		params.ModeFilter = &v
	}
	if v := strings.TrimSpace(q.Get("gridsquare")); v != "" {
		params.GridsquarePrefix = &v
	}
	if v := strings.TrimSpace(q.Get("dxcc")); v != "" {
		dxcc, err := strconv.Atoi(v)
		if err != nil {
			return db.SearchQSOsParams{}, fmt.Errorf("invalid dxcc")
		}
		dxcc32 := int32(dxcc)
		params.DxccFilter = &dxcc32
	}
	if v := strings.TrimSpace(q.Get("date_from")); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return db.SearchQSOsParams{}, fmt.Errorf("invalid date_from")
		}
		params.DateFrom = pgtype.Timestamptz{Time: t.UTC(), Valid: true}
	}
	if v := strings.TrimSpace(q.Get("date_to")); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return db.SearchQSOsParams{}, fmt.Errorf("invalid date_to")
		}
		params.DateTo = pgtype.Timestamptz{Time: t.UTC(), Valid: true}
	}

	return params, nil
}

func normalizeOptional(v *string) *string {
	if v == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*v)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func qsoFromGetRow(row db.GetQSOByUUIDRow) qsoResponse {
	return qsoResponse{
		UUID:         row.Uuid.String(),
		LogbookUUID:  row.LogbookUuid.String(),
		Callsign:     row.Callsign,
		Name:         row.Name,
		QTH:          row.Qth,
		Band:         row.Band,
		Mode:         row.Mode,
		Submode:      row.Submode,
		FrequencyHz:  row.FrequencyHz,
		DatetimeOn:   row.DatetimeOn.Time.UTC(),
		RstSent:      row.RstSent,
		RstRcvd:      row.RstRcvd,
		TxPower:      numericToFloat64(row.TxPower),
		Gridsquare:   row.Gridsquare,
		MyGridsquare: row.MyGridsquare,
		Dxcc:         row.Dxcc,
		Country:      row.Country,
		CQZone:       row.CqZone,
		ITUZone:      row.ItuZone,
		Continent:    row.Continent,
		Comment:      row.Comment,
		Notes:        row.Notes,
		CreatedAt:    row.CreatedAt.Time.UTC(),
		UpdatedAt:    row.UpdatedAt.Time.UTC(),
	}
}

func qsoFromSearchRow(row db.SearchQSOsRow) qsoResponse {
	return qsoResponse{
		UUID:        row.Uuid.String(),
		LogbookUUID: row.LogbookUuid.String(),
		Callsign:    row.Callsign,
		Band:        row.Band,
		Mode:        row.Mode,
		DatetimeOn:  row.DatetimeOn.Time.UTC(),
		RstSent:     row.RstSent,
		RstRcvd:     row.RstRcvd,
		Name:        row.Name,
		QTH:         row.Qth,
		Gridsquare:  row.Gridsquare,
		Dxcc:        row.Dxcc,
		Country:     row.Country,
		CQZone:      row.CqZone,
		ITUZone:     row.ItuZone,
		Continent:   row.Continent,
		Comment:     row.Comment,
		Notes:       row.Notes,
		CreatedAt:   row.CreatedAt.Time.UTC(),
		UpdatedAt:   row.UpdatedAt.Time.UTC(),
	}
}

// resetQSOSyncStatus resets all existing sync_status rows for a QSO back to
// pending and returns the affected services for job enqueueing.
func resetQSOSyncStatus(ctx context.Context, tx pgx.Tx, qsoID int64) ([]string, error) {
	rows, err := tx.Query(ctx, `
		UPDATE sync_status
		SET status = 'pending',
			last_synced_at = NULL,
			remote_id = NULL,
			error_message = NULL,
			last_error_code = NULL,
			retry_count = 0,
			next_retry_at = NULL,
			updated_at = NOW()
		WHERE qso_id = $1
		RETURNING service
	`, qsoID)
	if err != nil {
		return nil, fmt.Errorf("reset sync_status rows: %w", err)
	}
	defer rows.Close()

	serviceSet := map[string]struct{}{}
	for rows.Next() {
		var service string
		if scanErr := rows.Scan(&service); scanErr != nil {
			return nil, fmt.Errorf("scan reset sync service: %w", scanErr)
		}
		serviceSet[service] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate reset sync services: %w", err)
	}

	services := make([]string, 0, len(serviceSet))
	for svc := range serviceSet {
		services = append(services, svc)
	}
	sort.Strings(services)

	return services, nil
}

func (h *QSOHandler) enqueueSyncJobs(ctx context.Context, userID int64, services []string, reason string) {
	if h.riverClient == nil || len(services) == 0 {
		return
	}

	for _, svc := range services {
		switch svc {
		case "eqsl":
			if err := syncsvc.EnqueueEQSLUpload(ctx, h.riverClient, userID); err != nil {
				slog.WarnContext(ctx, "failed to enqueue eqsl upload",
					slog.String("reason", reason),
					slog.String("error", err.Error()))
			}
		case "clublog":
			if err := syncsvc.EnqueueClubLogUpload(ctx, h.riverClient, userID); err != nil {
				slog.WarnContext(ctx, "failed to enqueue clublog upload",
					slog.String("reason", reason),
					slog.String("error", err.Error()))
			}
		case "qrz":
			if err := syncsvc.EnqueueQRZUpload(ctx, h.riverClient, userID); err != nil {
				slog.WarnContext(ctx, "failed to enqueue qrz upload",
					slog.String("reason", reason),
					slog.String("error", err.Error()))
			}
		case "sota":
			if err := syncsvc.EnqueueSOTAUpload(ctx, h.riverClient, userID); err != nil {
				slog.WarnContext(ctx, "failed to enqueue sota upload",
					slog.String("reason", reason),
					slog.String("error", err.Error()))
			}
		case "pota":
			if err := syncsvc.EnqueuePOTAUpload(ctx, h.riverClient, userID); err != nil {
				slog.WarnContext(ctx, "failed to enqueue pota upload",
					slog.String("reason", reason),
					slog.String("error", err.Error()))
			}
		}
	}
}

// triggerAutoSync inserts pending sync_status rows and enqueues River jobs for
// all enabled services. Called after QSO create.
// skipInsert=true means sync_status rows already exist (future use).
//
// This is best-effort: errors are logged but do not affect the HTTP response.
// The QSO is already committed at this point, so sync will eventually catch up
// even if job enqueueing fails here.
func (h *QSOHandler) triggerAutoSync(ctx context.Context, userID, qsoID int64, skipInsert bool) {
	if h.riverClient == nil {
		return
	}

	services, err := syncsvc.EnabledServicesForUser(ctx, h.pool, userID)
	if err != nil || len(services) == 0 {
		return
	}

	// Insert pending sync rows.
	if !skipInsert {
		conn, err := h.pool.Acquire(ctx)
		if err == nil {
			defer conn.Release()
			tx, err := conn.Begin(ctx)
			if err == nil {
				if insertErr := syncsvc.InsertPendingSyncForQSO(ctx, tx, qsoID, services); insertErr != nil {
					_ = tx.Rollback(ctx)
				} else {
					_ = tx.Commit(ctx)
				}
			}
		}
	}

	h.enqueueSyncJobs(ctx, userID, services, "qso create")
}

// triggerQSOMatch enqueues a QSOMatchWorker job after a QSO is created.
// This runs asynchronously so it never delays the HTTP response.
// If River is not configured (e.g. in tests), the call is a no-op.
func (h *QSOHandler) triggerQSOMatch(ctx context.Context, userID, qsoID int64) {
	if h.riverClient == nil {
		return
	}
	// Use context.WithoutCancel so the goroutine outlives the HTTP request while
	// preserving any tracing/logging values from the original context.
	bgCtx := context.WithoutCancel(ctx)
	go func() {
		if err := confirmsvc.EnqueueQSOMatch(bgCtx, h.riverClient, qsoID, userID); err != nil {
			slog.WarnContext(bgCtx, "failed to enqueue qso match job",
				slog.Int64("qso_id", qsoID),
				slog.String("error", err.Error()),
			)
		}
	}()
}
