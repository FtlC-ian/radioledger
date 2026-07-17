package handler

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/FtlC-ian/radioledger/api/internal/auth"
	db "github.com/FtlC-ian/radioledger/api/internal/database/sqlc"
	adifpkg "github.com/FtlC-ian/radioledger/pkg/adif"
)

// POTAHandler serves POTA activation endpoints.
type POTAHandler struct {
	pool *pgxpool.Pool
}

// NewPOTAHandler creates a POTA handler.
func NewPOTAHandler(pool *pgxpool.Pool) *POTAHandler {
	return &POTAHandler{pool: pool}
}

// Create handles POST /v1/activations/pota.
func (h *POTAHandler) Create(w http.ResponseWriter, r *http.Request) {
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
	if err := validateActivationReference(activationProgramPOTA, reference); err != nil {
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
		Program:           activationProgramPOTA,
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
		Program:        activationProgramPOTA,
	})
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to create activation", "lookup failed")
		return
	}

	statusRow, err := queries.GetActivationStatusByUUIDAndProgram(r.Context(), db.GetActivationStatusByUUIDAndProgramParams{
		ActivationUuid: created.Uuid,
		Program:        activationProgramPOTA,
	})
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to create activation", "status query failed")
		return
	}
	validation := computeActivationValidation(activationProgramPOTA, statusRow)

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to create activation", "transaction failed")
		return
	}

	writeSuccess(w, http.StatusCreated, "pota activation created", activationResponseFromLookupRow(row, validation))
}

// List handles GET /v1/activations/pota.
func (h *POTAHandler) List(w http.ResponseWriter, r *http.Request) {
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
		Program: activationProgramPOTA,
	})
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to list activations", "query failed")
		return
	}

	items := make([]activationResponse, 0, len(rows))
	for _, row := range rows {
		statusRow, err := queries.GetActivationStatusByUUIDAndProgram(r.Context(), db.GetActivationStatusByUUIDAndProgramParams{
			ActivationUuid: row.Uuid,
			Program:        activationProgramPOTA,
		})
		if err != nil {
			writeFailure(w, http.StatusInternalServerError, "failed to list activations", "status query failed")
			return
		}
		validation := computeActivationValidation(activationProgramPOTA, statusRow)
		items = append(items, activationResponseFromListRow(row, validation))
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to list activations", "transaction failed")
		return
	}

	writeSuccess(w, http.StatusOK, "pota activations listed", map[string]any{"items": items})
}

// Get handles GET /v1/activations/pota/{activationUUID}.
func (h *POTAHandler) Get(w http.ResponseWriter, r *http.Request) {
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
		Program:        activationProgramPOTA,
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
		Program:        activationProgramPOTA,
	})
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to fetch activation", "status query failed")
		return
	}
	validation := computeActivationValidation(activationProgramPOTA, statusRow)

	qsoRows, err := queries.ListActivationQSOsByUUIDAndProgram(r.Context(), db.ListActivationQSOsByUUIDAndProgramParams{
		ActivationUuid: activationUUID,
		Program:        activationProgramPOTA,
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

	writeSuccess(w, http.StatusOK, "pota activation retrieved", map[string]any{
		"activation": activationResponseFromLookupRow(row, validation),
		"qsos":       qsos,
	})
}

// Update handles PUT /v1/activations/pota/{activationUUID}.
func (h *POTAHandler) Update(w http.ResponseWriter, r *http.Request) {
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
		Program:        activationProgramPOTA,
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
	if err := validateActivationReference(activationProgramPOTA, reference); err != nil {
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
		Program:           activationProgramPOTA,
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
		Program:        activationProgramPOTA,
	})
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to update activation", "lookup failed")
		return
	}

	statusRow, err := queries.GetActivationStatusByUUIDAndProgram(r.Context(), db.GetActivationStatusByUUIDAndProgramParams{
		ActivationUuid: updated.Uuid,
		Program:        activationProgramPOTA,
	})
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to update activation", "status query failed")
		return
	}
	validation := computeActivationValidation(activationProgramPOTA, statusRow)

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to update activation", "transaction failed")
		return
	}

	writeSuccess(w, http.StatusOK, "pota activation updated", activationResponseFromLookupRow(row, validation))
}

// Status handles GET /v1/activations/pota/{activationUUID}/status.
func (h *POTAHandler) Status(w http.ResponseWriter, r *http.Request) {
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
		Program:        activationProgramPOTA,
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
		Program:        activationProgramPOTA,
	})
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to fetch activation status", "status query failed")
		return
	}
	validation := computeActivationValidation(activationProgramPOTA, statusRow)

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to fetch activation status", "transaction failed")
		return
	}

	writeSuccess(w, http.StatusOK, "pota activation status retrieved", validation)
}

// Export handles POST /v1/activations/pota/{activationUUID}/export.
func (h *POTAHandler) Export(w http.ResponseWriter, r *http.Request) {
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
		Program:        activationProgramPOTA,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeFailure(w, http.StatusOK, "activation not found", "activation not found")
		return
	}
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to export activation", "query failed")
		return
	}
	if _, err := ensureLogbookPermission(r.Context(), queries, userID, activationRow.LogbookUuid, auth.PermissionExportADIF); err != nil {
		if errors.Is(err, errForbiddenRBAC) {
			writeFailure(w, http.StatusForbidden, "forbidden", "insufficient permissions")
			return
		}
		writeFailure(w, http.StatusInternalServerError, "authorization failed", "could not resolve logbook role")
		return
	}

	statusRow, err := queries.GetActivationStatusByUUIDAndProgram(r.Context(), db.GetActivationStatusByUUIDAndProgramParams{
		ActivationUuid: activationUUID,
		Program:        activationProgramPOTA,
	})
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to export activation", "status query failed")
		return
	}
	validation := computeActivationValidation(activationProgramPOTA, statusRow)

	qsoRows, err := queries.ListActivationQSOsByUUIDAndProgram(r.Context(), db.ListActivationQSOsByUUIDAndProgramParams{
		ActivationUuid: activationUUID,
		Program:        activationProgramPOTA,
	})
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to export activation", "qso query failed")
		return
	}
	if len(qsoRows) == 0 {
		writeFailure(w, http.StatusBadRequest, "failed to export activation", "activation has no QSOs to export")
		return
	}

	var buf bytes.Buffer
	writer := adifpkg.NewWriterWithOptions(&buf, adifpkg.WriterOptions{
		ProgramVersion: "0.1.0",
		IncludeHeader:  true,
	})
	if err := writer.WriteHeader(); err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to export activation", "failed to write ADIF header")
		return
	}

	for _, row := range qsoRows {
		rec := &adifpkg.Record{}
		setField(rec, "CALL", row.Callsign)
		setField(rec, "BAND", row.Band)
		setField(rec, "MODE", row.Mode)
		setFieldPtr(rec, "SUBMODE", row.Submode)
		adifpkg.CanonicalizeRecordMode(rec)

		on := row.DatetimeOn.Time.UTC()
		setField(rec, "QSO_DATE", on.Format("20060102"))
		setField(rec, "TIME_ON", on.Format("150405"))

		setFieldPtr(rec, "RST_SENT", row.RstSent)
		setFieldPtr(rec, "RST_RCVD", row.RstRcvd)
		setFreqField(rec, "FREQ", row.FrequencyHz)
		setField(rec, "STATION_CALLSIGN", row.StationCallsignExport)
		setField(rec, "MY_GRIDSQUARE", row.MyGridsquareExport)
		setField(rec, "MY_SIG", activationProgramPOTA)
		setField(rec, "MY_SIG_INFO", activationRow.Reference)
		setFieldPtr(rec, "SIG", row.Sig)
		setFieldPtr(rec, "SIG_INFO", row.SigInfo)
		setFieldPtr(rec, "COMMENT", row.Comment)
		setFieldPtr(rec, "NOTES", row.Notes)

		if err := writer.WriteRecord(rec); err != nil {
			writeFailure(w, http.StatusInternalServerError, "failed to export activation", "failed to write ADIF record")
			return
		}
	}

	warnings := append([]string{}, validation.Warnings...)
	if len(validation.MissingRequiredFields) > 0 {
		warnings = append(warnings, "Some QSOs are missing POTA-required fields")
	}
	if validation.UniqueCallsigns < validation.MinimumContacts {
		warnings = append(warnings, fmt.Sprintf("Activation currently has %d/%d required unique callsigns", validation.UniqueCallsigns, validation.MinimumContacts))
	}

	filename := fmt.Sprintf("radioledger-pota-%s-%s.adi", strings.ToLower(strings.ReplaceAll(activationRow.Reference, "/", "-")), formatDate(activationRow.ActivationDate))

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to export activation", "transaction failed")
		return
	}

	writeSuccess(w, http.StatusOK, "pota activation exported", map[string]any{
		"filename":            filename,
		"adif":                buf.String(),
		"qso_count":           len(qsoRows),
		"unique_callsigns":    validation.UniqueCallsigns,
		"ready_to_submit":     validation.ReadyToSubmit,
		"missing_fields":      validation.MissingRequiredFields,
		"validation_warnings": warnings,
	})
}
