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
	"github.com/jackc/pgx/v5/pgxpool"

	db "github.com/FtlC-ian/radioledger/api/internal/database/sqlc"
)

// LogbookHandler handles logbook CRUD endpoints.
type LogbookHandler struct {
	pool *pgxpool.Pool
}

// NewLogbookHandler creates a LogbookHandler with dependencies.
func NewLogbookHandler(pool *pgxpool.Pool) *LogbookHandler {
	return &LogbookHandler{pool: pool}
}

type logbookUpsertRequest struct {
	Name        string  `json:"name"`
	Callsign    *string `json:"callsign"`
	Description *string `json:"description"`
	IsDefault   bool    `json:"is_default"`
}

type logbookResponse struct {
	UUID        string    `json:"uuid"`
	Name        string    `json:"name"`
	Callsign    *string   `json:"callsign,omitempty"`
	Description *string   `json:"description,omitempty"`
	IsDefault   bool      `json:"is_default"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// List handles GET /v1/logbooks.
func (h *LogbookHandler) List(w http.ResponseWriter, r *http.Request) {
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
	rows, err := queries.ListLogbooksByUser(r.Context(), userID)
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to list logbooks", "query failed")
		return
	}

	items := make([]logbookResponse, 0, len(rows))
	for _, row := range rows {
		items = append(items, logbookFromListRow(row))
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to list logbooks", "transaction failed")
		return
	}

	writeSuccess(w, http.StatusOK, "logbooks listed", map[string]any{"items": items})
}

// Get handles GET /v1/logbooks/{logbookUUID}.
func (h *LogbookHandler) Get(w http.ResponseWriter, r *http.Request) {
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

	tx, err := beginTenantTx(r.Context(), h.pool, userID)
	if err != nil {
		writeFailure(w, http.StatusServiceUnavailable, "database unavailable", "failed to prepare tenant context")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	queries := db.New(tx)
	row, err := queries.GetLogbookByUUID(r.Context(), logbookUUID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeFailure(w, http.StatusOK, "logbook not found", "logbook not found")
		return
	}
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to fetch logbook", "query failed")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to fetch logbook", "transaction failed")
		return
	}

	writeSuccess(w, http.StatusOK, "logbook retrieved", logbookFromGetRow(row))
}

// Create handles POST /v1/logbooks.
func (h *LogbookHandler) Create(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	var req logbookUpsertRequest
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
	if req.IsDefault {
		if _, err := tx.Exec(r.Context(), `
			UPDATE logbooks
			SET is_default = FALSE, updated_at = NOW()
			WHERE user_id = $1 AND deleted_at IS NULL
		`, userID); err != nil {
			writeFailure(w, http.StatusInternalServerError, "failed to create logbook", "failed to update default logbook")
			return
		}
	}

	queries := db.New(tx)
	row, err := queries.CreateLogbook(r.Context(), db.CreateLogbookParams{
		UserID:      userID,
		Name:        strings.TrimSpace(req.Name),
		Callsign:    normalizeOptional(req.Callsign),
		Description: normalizeOptional(req.Description),
		IsDefault:   req.IsDefault,
	})
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to create logbook", "insert failed")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to create logbook", "transaction failed")
		return
	}

	writeSuccess(w, http.StatusCreated, "logbook created", logbookFromCreateRow(row))
}

// Update handles PUT /v1/logbooks/{logbookUUID}.
func (h *LogbookHandler) Update(w http.ResponseWriter, r *http.Request) {
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

	var req logbookUpsertRequest
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
	if req.IsDefault {
		if _, err := tx.Exec(r.Context(), `
			UPDATE logbooks
			SET is_default = FALSE, updated_at = NOW()
			WHERE user_id = (
				SELECT user_id FROM logbooks WHERE uuid = $1 AND deleted_at IS NULL
			)
			AND uuid <> $1
			AND deleted_at IS NULL
		`, logbookUUID); err != nil {
			writeFailure(w, http.StatusInternalServerError, "failed to update logbook", "failed to update default logbook")
			return
		}
	}

	queries := db.New(tx)
	row, err := queries.UpdateLogbook(r.Context(), db.UpdateLogbookParams{
		Name:        strings.TrimSpace(req.Name),
		Callsign:    normalizeOptional(req.Callsign),
		Description: normalizeOptional(req.Description),
		IsDefault:   req.IsDefault,
		LogbookUuid: logbookUUID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeFailure(w, http.StatusOK, "logbook not found", "logbook not found")
		return
	}
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to update logbook", "update failed")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to update logbook", "transaction failed")
		return
	}

	writeSuccess(w, http.StatusOK, "logbook updated", logbookFromUpdateRow(row))
}

// Delete handles DELETE /v1/logbooks/{logbookUUID}.
func (h *LogbookHandler) Delete(w http.ResponseWriter, r *http.Request) {
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

	tx, err := beginTenantTx(r.Context(), h.pool, userID)
	if err != nil {
		writeFailure(w, http.StatusServiceUnavailable, "database unavailable", "failed to prepare tenant context")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	queries := db.New(tx)
	deleted, err := queries.DeleteLogbook(r.Context(), logbookUUID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeFailure(w, http.StatusOK, "logbook not found", "logbook not found")
		return
	}
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to delete logbook", "delete failed")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to delete logbook", "transaction failed")
		return
	}

	writeSuccess(w, http.StatusOK, "logbook deleted", map[string]string{"uuid": deleted.String()})
}

// Default handles GET /v1/logbooks/default.
func (h *LogbookHandler) Default(w http.ResponseWriter, r *http.Request) {
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
	row, err := queries.GetDefaultLogbook(r.Context(), userID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeFailure(w, http.StatusOK, "default logbook not found", "default logbook not found")
		return
	}
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to fetch default logbook", "query failed")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to fetch default logbook", "transaction failed")
		return
	}

	writeSuccess(w, http.StatusOK, "default logbook retrieved", logbookFromDefaultRow(row))
}

func logbookFromCreateRow(row db.CreateLogbookRow) logbookResponse {
	return logbookResponse{
		UUID:        row.Uuid.String(),
		Name:        row.Name,
		Callsign:    row.Callsign,
		Description: row.Description,
		IsDefault:   row.IsDefault,
		CreatedAt:   row.CreatedAt.Time.UTC(),
		UpdatedAt:   row.UpdatedAt.Time.UTC(),
	}
}

func logbookFromGetRow(row db.GetLogbookByUUIDRow) logbookResponse {
	return logbookResponse{
		UUID:        row.Uuid.String(),
		Name:        row.Name,
		Callsign:    row.Callsign,
		Description: row.Description,
		IsDefault:   row.IsDefault,
		CreatedAt:   row.CreatedAt.Time.UTC(),
		UpdatedAt:   row.UpdatedAt.Time.UTC(),
	}
}

func logbookFromListRow(row db.ListLogbooksByUserRow) logbookResponse {
	return logbookResponse{
		UUID:        row.Uuid.String(),
		Name:        row.Name,
		Callsign:    row.Callsign,
		Description: row.Description,
		IsDefault:   row.IsDefault,
		CreatedAt:   row.CreatedAt.Time.UTC(),
		UpdatedAt:   row.UpdatedAt.Time.UTC(),
	}
}

func logbookFromUpdateRow(row db.UpdateLogbookRow) logbookResponse {
	return logbookResponse{
		UUID:        row.Uuid.String(),
		Name:        row.Name,
		Callsign:    row.Callsign,
		Description: row.Description,
		IsDefault:   row.IsDefault,
		CreatedAt:   row.CreatedAt.Time.UTC(),
		UpdatedAt:   row.UpdatedAt.Time.UTC(),
	}
}

func logbookFromDefaultRow(row db.GetDefaultLogbookRow) logbookResponse {
	return logbookResponse{
		UUID:        row.Uuid.String(),
		Name:        row.Name,
		Callsign:    row.Callsign,
		Description: row.Description,
		IsDefault:   row.IsDefault,
		CreatedAt:   row.CreatedAt.Time.UTC(),
		UpdatedAt:   row.UpdatedAt.Time.UTC(),
	}
}

func logbookPathID(r *http.Request) (uuid.UUID, error) {
	logbookUUID, err := uuid.Parse(chi.URLParam(r, "logbookUUID"))
	if err != nil {
		return uuid.Nil, fmt.Errorf("invalid logbook UUID")
	}
	return logbookUUID, nil
}
