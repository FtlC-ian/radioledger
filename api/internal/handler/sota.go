package handler

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/FtlC-ian/radioledger/api/internal/auth"
	db "github.com/FtlC-ian/radioledger/api/internal/database/sqlc"
)

// SOTAHandler serves SOTA activation endpoints.
type SOTAHandler struct {
	pool *pgxpool.Pool
}

// NewSOTAHandler creates a SOTA handler.
func NewSOTAHandler(pool *pgxpool.Pool) *SOTAHandler {
	return &SOTAHandler{pool: pool}
}

// Create handles POST /v1/activations/sota.
func (h *SOTAHandler) Create(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	var req activationCreateRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", "invalid JSON body")
		return
	}

	reference := normalizeActivationReference(req.Reference)
	if reference == "" {
		writeFailure(w, http.StatusBadRequest, "invalid request", "reference is required")
		return
	}
	if err := validateActivationReference(activationProgramSOTA, reference); err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", err.Error())
		return
	}

	activationDate, err := parseActivationDate(req.ActivationDate)
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
	logbookID, _, err := resolveActivationLogbook(r.Context(), queries, userID, req.LogbookUUID, auth.PermissionQSOCreate)
	if err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", err.Error())
		return
	}

	stationLocationID, err := resolveStationLocationID(r.Context(), queries, req.StationLocationUUID, nil)
	if err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", err.Error())
		return
	}

	created, err := queries.CreateActivation(r.Context(), db.CreateActivationParams{
		UserID:            userID,
		LogbookID:         logbookID,
		Program:           activationProgramSOTA,
		Reference:         reference,
		ActivationDate:    activationDate,
		StationLocationID: stationLocationID,
		Notes:             normalizeOptional(req.Notes),
		Status:            "in_progress",
	})
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to create activation", "insert failed")
		return
	}

	row, err := queries.GetActivationByUUIDAndProgram(r.Context(), db.GetActivationByUUIDAndProgramParams{
		ActivationUuid: created.Uuid,
		Program:        activationProgramSOTA,
	})
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to create activation", "lookup failed")
		return
	}

	statusRow, err := queries.GetActivationStatusByUUIDAndProgram(r.Context(), db.GetActivationStatusByUUIDAndProgramParams{
		ActivationUuid: created.Uuid,
		Program:        activationProgramSOTA,
	})
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to create activation", "status query failed")
		return
	}
	validation := computeActivationValidation(activationProgramSOTA, statusRow)

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to create activation", "transaction failed")
		return
	}

	writeSuccess(w, http.StatusCreated, "sota activation created", activationResponseFromLookupRow(row, validation))
}

// List handles GET /v1/activations/sota.
func (h *SOTAHandler) List(w http.ResponseWriter, r *http.Request) {
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
	rows, err := queries.ListActivationsByProgram(r.Context(), db.ListActivationsByProgramParams{
		UserID:  userID,
		Program: activationProgramSOTA,
	})
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to list activations", "query failed")
		return
	}

	items := make([]activationResponse, 0, len(rows))
	for _, row := range rows {
		statusRow, err := queries.GetActivationStatusByUUIDAndProgram(r.Context(), db.GetActivationStatusByUUIDAndProgramParams{
			ActivationUuid: row.Uuid,
			Program:        activationProgramSOTA,
		})
		if err != nil {
			writeFailure(w, http.StatusInternalServerError, "failed to list activations", "status query failed")
			return
		}
		validation := computeActivationValidation(activationProgramSOTA, statusRow)
		items = append(items, activationResponseFromListRow(row, validation))
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to list activations", "transaction failed")
		return
	}

	writeSuccess(w, http.StatusOK, "sota activations listed", map[string]any{"items": items})
}

// Get handles GET /v1/activations/sota/{activationUUID}.
func (h *SOTAHandler) Get(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	activationUUID, err := uuid.Parse(chi.URLParam(r, "activationUUID"))
	if err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", "invalid activation UUID")
		return
	}

	tx, err := beginTenantTx(r.Context(), h.pool, userID)
	if err != nil {
		writeFailure(w, http.StatusServiceUnavailable, "database unavailable", "failed to prepare tenant context")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	queries := db.New(tx)
	row, err := queries.GetActivationByUUIDAndProgram(r.Context(), db.GetActivationByUUIDAndProgramParams{
		ActivationUuid: activationUUID,
		Program:        activationProgramSOTA,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeFailure(w, http.StatusOK, "activation not found", "activation not found")
		return
	}
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to fetch activation", "query failed")
		return
	}
	if _, err := ensureLogbookPermission(r.Context(), queries, userID, row.LogbookUuid, auth.PermissionQSORead); err != nil {
		if errors.Is(err, errForbiddenRBAC) {
			writeFailure(w, http.StatusForbidden, "forbidden", "insufficient permissions")
			return
		}
		writeFailure(w, http.StatusInternalServerError, "authorization failed", "could not resolve logbook role")
		return
	}

	statusRow, err := queries.GetActivationStatusByUUIDAndProgram(r.Context(), db.GetActivationStatusByUUIDAndProgramParams{
		ActivationUuid: activationUUID,
		Program:        activationProgramSOTA,
	})
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to fetch activation", "status query failed")
		return
	}
	validation := computeActivationValidation(activationProgramSOTA, statusRow)

	qsoRows, err := queries.ListActivationQSOsByUUIDAndProgram(r.Context(), db.ListActivationQSOsByUUIDAndProgramParams{
		ActivationUuid: activationUUID,
		Program:        activationProgramSOTA,
	})
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to fetch activation", "qso query failed")
		return
	}

	qsos := make([]activationQSOResponse, 0, len(qsoRows))
	for _, qsoRow := range qsoRows {
		qsos = append(qsos, activationQSOFromRow(qsoRow))
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to fetch activation", "transaction failed")
		return
	}

	writeSuccess(w, http.StatusOK, "sota activation retrieved", map[string]any{
		"activation": activationResponseFromLookupRow(row, validation),
		"qsos":       qsos,
	})
}

// Update handles PUT /v1/activations/sota/{activationUUID}.
func (h *SOTAHandler) Update(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	activationUUID, err := uuid.Parse(chi.URLParam(r, "activationUUID"))
	if err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", "invalid activation UUID")
		return
	}

	var req activationUpdateRequest
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
	existing, err := queries.GetActivationByUUIDAndProgram(r.Context(), db.GetActivationByUUIDAndProgramParams{
		ActivationUuid: activationUUID,
		Program:        activationProgramSOTA,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeFailure(w, http.StatusOK, "activation not found", "activation not found")
		return
	}
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to update activation", "query failed")
		return
	}

	if _, err := ensureLogbookPermission(r.Context(), queries, userID, existing.LogbookUuid, auth.PermissionQSOUpdate); err != nil {
		if errors.Is(err, errForbiddenRBAC) {
			writeFailure(w, http.StatusForbidden, "forbidden", "insufficient permissions")
			return
		}
		writeFailure(w, http.StatusInternalServerError, "authorization failed", "could not resolve logbook role")
		return
	}

	reference := existing.Reference
	if req.Reference != nil {
		reference = normalizeActivationReference(*req.Reference)
	}
	if reference == "" {
		writeFailure(w, http.StatusBadRequest, "invalid request", "reference is required")
		return
	}
	if err := validateActivationReference(activationProgramSOTA, reference); err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", err.Error())
		return
	}

	activationDate := existing.ActivationDate
	if req.ActivationDate != nil {
		parsedDate, err := parseActivationDate(*req.ActivationDate)
		if err != nil {
			writeFailure(w, http.StatusBadRequest, "invalid request", err.Error())
			return
		}
		activationDate = parsedDate
	}

	stationLocationID, err := resolveStationLocationID(r.Context(), queries, req.StationLocationUUID, existing.StationLocationID)
	if err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", err.Error())
		return
	}

	notes := existing.Notes
	if req.Notes != nil {
		notes = normalizeOptional(req.Notes)
	}

	status, err := parseActivationStatus("", existing.Status)
	if err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", err.Error())
		return
	}
	if req.Status != nil {
		status, err = parseActivationStatus(*req.Status, existing.Status)
		if err != nil {
			writeFailure(w, http.StatusBadRequest, "invalid request", err.Error())
			return
		}
	}

	updated, err := queries.UpdateActivationByUUIDAndProgram(r.Context(), db.UpdateActivationByUUIDAndProgramParams{
		Reference:         reference,
		ActivationDate:    activationDate,
		StationLocationID: stationLocationID,
		Notes:             notes,
		Status:            status,
		ActivationUuid:    activationUUID,
		Program:           activationProgramSOTA,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeFailure(w, http.StatusOK, "activation not found", "activation not found")
		return
	}
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to update activation", "update failed")
		return
	}

	row, err := queries.GetActivationByUUIDAndProgram(r.Context(), db.GetActivationByUUIDAndProgramParams{
		ActivationUuid: updated.Uuid,
		Program:        activationProgramSOTA,
	})
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to update activation", "lookup failed")
		return
	}

	statusRow, err := queries.GetActivationStatusByUUIDAndProgram(r.Context(), db.GetActivationStatusByUUIDAndProgramParams{
		ActivationUuid: updated.Uuid,
		Program:        activationProgramSOTA,
	})
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to update activation", "status query failed")
		return
	}
	validation := computeActivationValidation(activationProgramSOTA, statusRow)

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to update activation", "transaction failed")
		return
	}

	writeSuccess(w, http.StatusOK, "sota activation updated", activationResponseFromLookupRow(row, validation))
}

// Status handles GET /v1/activations/sota/{activationUUID}/status.
func (h *SOTAHandler) Status(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	activationUUID, err := uuid.Parse(chi.URLParam(r, "activationUUID"))
	if err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", "invalid activation UUID")
		return
	}

	tx, err := beginTenantTx(r.Context(), h.pool, userID)
	if err != nil {
		writeFailure(w, http.StatusServiceUnavailable, "database unavailable", "failed to prepare tenant context")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	queries := db.New(tx)
	activationRow, err := queries.GetActivationByUUIDAndProgram(r.Context(), db.GetActivationByUUIDAndProgramParams{
		ActivationUuid: activationUUID,
		Program:        activationProgramSOTA,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeFailure(w, http.StatusOK, "activation not found", "activation not found")
		return
	}
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to fetch activation status", "query failed")
		return
	}
	if _, err := ensureLogbookPermission(r.Context(), queries, userID, activationRow.LogbookUuid, auth.PermissionQSORead); err != nil {
		if errors.Is(err, errForbiddenRBAC) {
			writeFailure(w, http.StatusForbidden, "forbidden", "insufficient permissions")
			return
		}
		writeFailure(w, http.StatusInternalServerError, "authorization failed", "could not resolve logbook role")
		return
	}

	statusRow, err := queries.GetActivationStatusByUUIDAndProgram(r.Context(), db.GetActivationStatusByUUIDAndProgramParams{
		ActivationUuid: activationUUID,
		Program:        activationProgramSOTA,
	})
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to fetch activation status", "status query failed")
		return
	}
	validation := computeActivationValidation(activationProgramSOTA, statusRow)

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to fetch activation status", "transaction failed")
		return
	}

	writeSuccess(w, http.StatusOK, "sota activation status retrieved", validation)
}
