package handler

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	"github.com/FtlC-ian/radioledger/api/internal/auth"
	"github.com/FtlC-ian/radioledger/api/internal/crypto"
	db "github.com/FtlC-ian/radioledger/api/internal/database/sqlc"
	"github.com/FtlC-ian/radioledger/api/internal/jobs"
	"github.com/FtlC-ian/radioledger/api/internal/services/qrz"
	"github.com/FtlC-ian/radioledger/api/internal/services/qsoenrich"
)

const (
	// maxImportFileSize is the maximum allowed ADIF upload size (500 MB).
	maxImportFileSize = 500 * 1024 * 1024

	// maxMultipartMemory is the in-memory buffer for multipart parsing.
	// Files larger than this are buffered to disk by net/http.
	maxMultipartMemory = 32 * 1024 * 1024 // 32 MB
)

// ImportHandler handles ADIF import endpoints.
type ImportHandler struct {
	pool        *pgxpool.Pool
	riverClient *river.Client[pgx.Tx]
	keyring     *crypto.Keyring
}

// NewImportHandler creates an ImportHandler wired to the given pool and River client.
// The River client is used to enqueue ADIFImportArgs jobs after file upload.
func NewImportHandler(pool *pgxpool.Pool, riverClient *river.Client[pgx.Tx], keyring *crypto.Keyring) *ImportHandler {
	return &ImportHandler{
		pool:        pool,
		riverClient: riverClient,
		keyring:     keyring,
	}
}

// importJobResponse is the JSON body returned for POST /v1/import/adif (202 Accepted).
type importJobResponse struct {
	JobUUID   string `json:"job_uuid"`
	Status    string `json:"status"`
	StatusURL string `json:"status_url"`
}

// importStatusResponse is the JSON body returned for GET /v1/import/{uuid}.
type importStatusResponse struct {
	UUID         string  `json:"uuid"`
	LogbookUUID  string  `json:"logbook_uuid"`
	Status       string  `json:"status"`
	TotalRecords *int32  `json:"total_records,omitempty"`
	Imported     int32   `json:"imported"`
	Duplicate    int32   `json:"duplicate"`
	Skipped      int32   `json:"skipped"`
	Errors       int32   `json:"errors"`
	Warnings     int32   `json:"warnings"`
	PctComplete  float64 `json:"pct_complete"`
	Filename     *string `json:"filename,omitempty"`
	CreatedAt    string  `json:"created_at"`
	StartedAt    *string `json:"started_at,omitempty"`
	CompletedAt  *string `json:"completed_at,omitempty"`
}

type enrichQSOsRequest struct {
	LogbookUUID *string `json:"logbook_uuid,omitempty"`
}

type qrzImportRequest struct {
	LogbookUUID *string `json:"logbook_uuid,omitempty"`
	APIKey      *string `json:"api_key,omitempty"`
}

// UploadADIF handles POST /v1/import/adif.
//
// Accepts a multipart/form-data request with:
//   - "file": the ADIF file (.adi or .adif extension)
//   - "logbook_uuid": UUID of the target logbook
//
// Validates file size and extension, saves the file to a temp location,
// creates an import_jobs row (status=pending), enqueues a River job, and
// returns 202 Accepted with the job UUID for polling.
func (h *ImportHandler) UploadADIF(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	// Reject requests that exceed the size limit before parsing multipart.
	r.Body = http.MaxBytesReader(w, r.Body, maxImportFileSize+1024) // extra for form fields

	if err := r.ParseMultipartForm(maxMultipartMemory); err != nil {
		if strings.Contains(err.Error(), "request body too large") {
			writeFailure(w, http.StatusRequestEntityTooLarge, "file too large",
				fmt.Sprintf("file must be under %d MB", maxImportFileSize/(1024*1024)))
			return
		}
		writeFailure(w, http.StatusBadRequest, "invalid request", "failed to parse multipart form")
		return
	}

	// Extract logbook UUID from form field.
	logbookUUIDStr := strings.TrimSpace(r.FormValue("logbook_uuid"))
	if logbookUUIDStr == "" {
		writeFailure(w, http.StatusBadRequest, "invalid request", "logbook_uuid form field is required")
		return
	}
	logbookUUID, err := uuid.Parse(logbookUUIDStr)
	if err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", "invalid logbook_uuid")
		return
	}

	if _, err := ensureLogbookPermissionPool(r.Context(), h.pool, userID, logbookUUID, auth.PermissionImportADIF); err != nil {
		if errors.Is(err, errForbiddenRBAC) {
			writeFailure(w, http.StatusForbidden, "forbidden", "insufficient permissions")
			return
		}
		writeFailure(w, http.StatusInternalServerError, "authorization failed", "could not resolve logbook role")
		return
	}

	// Extract the uploaded file.
	file, header, err := r.FormFile("file")
	if err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", "missing or invalid file field")
		return
	}
	defer func() { _ = file.Close() }()

	// Validate file extension.
	ext := strings.ToLower(filepath.Ext(header.Filename))
	if ext != ".adi" && ext != ".adif" {
		writeFailure(w, http.StatusBadRequest, "invalid request",
			"file must have .adi or .adif extension")
		return
	}

	// Validate file size from Content-Length (if available) or by reading.
	fileSize := header.Size
	if fileSize > maxImportFileSize {
		writeFailure(w, http.StatusRequestEntityTooLarge, "file too large",
			fmt.Sprintf("file size %d bytes exceeds maximum %d bytes", fileSize, maxImportFileSize))
		return
	}

	// Save uploads under an explicit shared directory when containers configure
	// RADIOLEDGER_IMPORT_TMPDIR; otherwise respect TMPDIR through os.TempDir.
	// This keeps production mounts deliberate and avoids hard-coding a host path.
	importTmpDir := os.Getenv("RADIOLEDGER_IMPORT_TMPDIR")
	if importTmpDir == "" {
		importTmpDir = filepath.Join(os.TempDir(), "radioledger")
	}
	if err := os.MkdirAll(importTmpDir, 0o700); err != nil {
		slog.ErrorContext(r.Context(), "failed to create import temp dir", slog.String("error", err.Error()))
		writeFailure(w, http.StatusInternalServerError, "upload failed", "could not prepare temp directory")
		return
	}
	tmpFile, err := os.CreateTemp(importTmpDir, "radioledger-import-*.adif")
	if err != nil {
		slog.ErrorContext(r.Context(), "failed to create temp file for import", slog.String("error", err.Error()))
		writeFailure(w, http.StatusInternalServerError, "upload failed", "could not save uploaded file")
		return
	}
	defer func() { _ = tmpFile.Close() }()

	written, err := io.Copy(tmpFile, file)
	if err != nil {
		_ = os.Remove(tmpFile.Name())
		writeFailure(w, http.StatusInternalServerError, "upload failed", "could not write uploaded file")
		return
	}
	// Sync to avoid data loss before the worker opens the file.
	if err := tmpFile.Sync(); err != nil {
		_ = os.Remove(tmpFile.Name())
		writeFailure(w, http.StatusInternalServerError, "upload failed", "could not flush uploaded file")
		return
	}

	// Use tenant tx to create the import job (RLS enforces logbook ownership).
	tx, err := beginTenantTx(r.Context(), h.pool, userID)
	if err != nil {
		_ = os.Remove(tmpFile.Name())
		writeFailure(w, http.StatusServiceUnavailable, "database unavailable", "failed to prepare tenant context")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	queries := db.New(tx)

	filename := header.Filename
	fileSizeBytes := written
	importJob, err := queries.CreateImportJob(r.Context(), db.CreateImportJobParams{
		LogbookUuid:   logbookUUID,
		Filename:      &filename,
		FileSizeBytes: &fileSizeBytes,
		Source:        "web",
	})
	if errors.Is(err, pgx.ErrNoRows) {
		_ = os.Remove(tmpFile.Name())
		writeFailure(w, http.StatusOK, "logbook not found", "logbook not found or access denied")
		return
	}
	if err != nil {
		_ = os.Remove(tmpFile.Name())
		slog.ErrorContext(r.Context(), "failed to create import job", slog.String("error", err.Error()))
		writeFailure(w, http.StatusInternalServerError, "import failed", "could not create import job")
		return
	}

	// Enqueue the River job within the same transaction.
	// If the transaction rolls back, the job is also rolled back (transactional enqueueing).
	if h.riverClient != nil {
		_, err = h.riverClient.InsertTx(r.Context(), tx, jobs.ADIFImportArgs{
			ImportJobID: importJob.ID,
			FilePath:    tmpFile.Name(),
			LogbookID:   importJob.LogbookID,
			UserID:      importJob.UserID,
		}, nil)
		if err != nil {
			_ = os.Remove(tmpFile.Name())
			slog.ErrorContext(r.Context(), "failed to enqueue river job", slog.String("error", err.Error()))
			writeFailure(w, http.StatusInternalServerError, "import failed", "could not enqueue import job")
			return
		}
	}

	if err := tx.Commit(r.Context()); err != nil {
		_ = os.Remove(tmpFile.Name())
		writeFailure(w, http.StatusInternalServerError, "import failed", "transaction failed")
		return
	}

	jobUUID := importJob.Uuid.String()
	writeJSON(w, http.StatusAccepted, apiResponse{
		Success: true,
		Message: "import job accepted",
		Data: importJobResponse{
			JobUUID:   jobUUID,
			Status:    "pending",
			StatusURL: "/v1/import/" + jobUUID,
		},
	})
}

// UploadQRZ handles POST /v1/import/qrz.
//
// Accepts JSON body:
//
//	{"logbook_uuid":"..."}
//
// or (to save/update QRZ credentials before enqueueing):
//
//	{"logbook_uuid":"...","api_key":"..."}
func (h *ImportHandler) UploadQRZ(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	var req qrzImportRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", "invalid JSON body")
		return
	}

	apiKey := ""
	if req.APIKey != nil {
		apiKey = strings.TrimSpace(*req.APIKey)
	}

	var logbookUUID uuid.UUID
	if req.LogbookUUID != nil && strings.TrimSpace(*req.LogbookUUID) != "" {
		parsed, parseErr := uuid.Parse(strings.TrimSpace(*req.LogbookUUID))
		if parseErr != nil {
			writeFailure(w, http.StatusBadRequest, "invalid request", "invalid logbook_uuid")
			return
		}
		logbookUUID = parsed
	} else {
		tx, txErr := beginTenantTx(r.Context(), h.pool, userID)
		if txErr != nil {
			writeFailure(w, http.StatusServiceUnavailable, "database unavailable", "failed to prepare tenant context")
			return
		}
		defer func() { _ = tx.Rollback(r.Context()) }()

		q := db.New(tx)
		def, defErr := q.GetDefaultLogbook(r.Context(), userID)
		if errors.Is(defErr, pgx.ErrNoRows) {
			writeFailure(w, http.StatusBadRequest, "invalid request", "no default logbook found")
			return
		}
		if defErr != nil {
			writeFailure(w, http.StatusInternalServerError, "query failed", "could not resolve default logbook")
			return
		}
		if commitErr := tx.Commit(r.Context()); commitErr != nil {
			writeFailure(w, http.StatusInternalServerError, "query failed", "transaction failed")
			return
		}
		logbookUUID = def.Uuid
	}

	if _, err := ensureLogbookPermissionPool(r.Context(), h.pool, userID, logbookUUID, auth.PermissionImportADIF); err != nil {
		if errors.Is(err, errForbiddenRBAC) {
			writeFailure(w, http.StatusForbidden, "forbidden", "insufficient permissions")
			return
		}
		writeFailure(w, http.StatusInternalServerError, "authorization failed", "could not resolve logbook role")
		return
	}

	tx, err := beginTenantTx(r.Context(), h.pool, userID)
	if err != nil {
		writeFailure(w, http.StatusServiceUnavailable, "database unavailable", "failed to prepare tenant context")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	queries := db.New(tx)

	if apiKey != "" {
		if h.keyring == nil {
			writeFailure(w, http.StatusServiceUnavailable, "encryption not configured", "RADIOLEDGER_MASTER_KEY is not set")
			return
		}

		plaintext, encErr := qrz.EncodeLogbookCredentials(apiKey)
		if encErr != nil {
			writeFailure(w, http.StatusBadRequest, "invalid request", "api_key must not be empty")
			return
		}

		ciphertext, keyVersion, encErr := h.keyring.Encrypt(userID, plaintext)
		if encErr != nil {
			slog.ErrorContext(r.Context(), "failed to encrypt qrz credentials", slog.String("error", encErr.Error()))
			writeFailure(w, http.StatusInternalServerError, "import failed", "could not store qrz credentials")
			return
		}

		if _, upsertErr := queries.UpsertCredential(r.Context(), db.UpsertCredentialParams{
			UserID:         userID,
			Service:        "qrz",
			CredentialType: "api_key",
			Credentials:    ciphertext,
			KeyVersion:     keyVersion,
			ExpiresAt:      pgtype.Timestamptz{},
		}); upsertErr != nil {
			slog.ErrorContext(r.Context(), "failed to persist qrz credentials", slog.String("error", upsertErr.Error()))
			writeFailure(w, http.StatusInternalServerError, "import failed", "could not store qrz credentials")
			return
		}
	}

	if _, err := queries.GetCredential(r.Context(), db.GetCredentialParams{UserID: userID, Service: "qrz"}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeFailure(w, http.StatusBadRequest, "missing credentials", "no saved QRZ API key found")
			return
		}
		writeFailure(w, http.StatusInternalServerError, "query failed", "could not verify saved QRZ credentials")
		return
	}

	filename := "qrz-logbook.adi"
	importJob, err := queries.CreateImportJob(r.Context(), db.CreateImportJobParams{
		LogbookUuid: logbookUUID,
		Filename:    &filename,
		Source:      "qrz",
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeFailure(w, http.StatusBadRequest, "logbook not found", "logbook not found or access denied")
		return
	}
	if err != nil {
		slog.ErrorContext(r.Context(), "failed to create qrz import job", slog.String("error", err.Error()))
		writeFailure(w, http.StatusInternalServerError, "import failed", "could not create import job")
		return
	}

	if h.riverClient != nil {
		_, err = h.riverClient.InsertTx(r.Context(), tx, jobs.QRZImportArgs{
			ImportJobID: importJob.ID,
			LogbookID:   importJob.LogbookID,
			UserID:      importJob.UserID,
		}, nil)
		if err != nil {
			slog.ErrorContext(r.Context(), "failed to enqueue qrz import worker", slog.String("error", err.Error()))
			writeFailure(w, http.StatusInternalServerError, "import failed", "could not enqueue import job")
			return
		}
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "import failed", "transaction failed")
		return
	}

	jobUUID := importJob.Uuid.String()
	writeJSON(w, http.StatusAccepted, apiResponse{
		Success: true,
		Message: "import job accepted",
		Data: importJobResponse{
			JobUUID:   jobUUID,
			Status:    "pending",
			StatusURL: "/v1/import/" + jobUUID,
		},
	})
}

// GetImportStatus handles GET /v1/import/{uuid}.
// Returns the current status and progress counters for an import job.
// RLS ensures users can only see their own import jobs.
// EnrichQSOs handles POST /v1/import/enrich.
//
// It performs a one-shot metadata backfill for existing QSOs with missing
// country/DXCC/CQ zone/ITU zone/continent fields.
//
// Optional JSON body:
//
//	{"logbook_uuid":"..."}
//
// If logbook_uuid is omitted, all logbooks the user can operate are processed.
func (h *ImportHandler) EnrichQSOs(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	var req enrichQSOsRequest
	if r.ContentLength > 0 {
		if err := decodeJSONBody(r, &req); err != nil {
			writeFailure(w, http.StatusBadRequest, "invalid request", "invalid JSON body")
			return
		}
	}

	var logbookUUID *uuid.UUID
	if req.LogbookUUID != nil && strings.TrimSpace(*req.LogbookUUID) != "" {
		parsed, parseErr := uuid.Parse(strings.TrimSpace(*req.LogbookUUID))
		if parseErr != nil {
			writeFailure(w, http.StatusBadRequest, "invalid request", "invalid logbook_uuid")
			return
		}
		logbookUUID = &parsed

		if _, permErr := ensureLogbookPermissionPool(r.Context(), h.pool, userID, parsed, auth.PermissionImportADIF); permErr != nil {
			if errors.Is(permErr, errForbiddenRBAC) {
				writeFailure(w, http.StatusForbidden, "forbidden", "insufficient permissions")
				return
			}
			writeFailure(w, http.StatusInternalServerError, "authorization failed", "could not resolve logbook role")
			return
		}
	}

	tx, err := beginTenantTx(r.Context(), h.pool, userID)
	if err != nil {
		writeFailure(w, http.StatusServiceUnavailable, "database unavailable", "failed to prepare tenant context")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()

	if h.riverClient != nil {
		_, err = h.riverClient.InsertTx(r.Context(), tx, jobs.QSOEnrichmentBackfillArgs{
			UserID:      userID,
			LogbookUUID: logbookUUID,
		}, nil)
		if err != nil {
			writeFailure(w, http.StatusInternalServerError, "enrichment failed", "could not enqueue enrichment job")
			return
		}

		if err := tx.Commit(r.Context()); err != nil {
			writeFailure(w, http.StatusInternalServerError, "enrichment failed", "transaction failed")
			return
		}

		writeJSON(w, http.StatusAccepted, apiResponse{
			Success: true,
			Message: "qso enrichment job accepted",
			Data: map[string]any{
				"queued": true,
			},
		})
		return
	}

	updated, err := qsoenrich.EnrichAccessible(r.Context(), tx, logbookUUID)
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "enrichment failed", "could not backfill qso metadata")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "enrichment failed", "transaction failed")
		return
	}

	writeSuccess(w, http.StatusOK, "qso enrichment complete", map[string]any{
		"updated": updated,
		"queued":  false,
	})
}

func (h *ImportHandler) GetImportStatus(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	importUUID, err := uuid.Parse(chi.URLParam(r, "importUUID"))
	if err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", "invalid import job UUID")
		return
	}

	tx, err := beginTenantTx(r.Context(), h.pool, userID)
	if err != nil {
		writeFailure(w, http.StatusServiceUnavailable, "database unavailable", "failed to prepare tenant context")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	queries := db.New(tx)
	row, err := queries.GetImportJobByUUID(r.Context(), importUUID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeFailure(w, http.StatusOK, "import job not found", "import job not found")
		return
	}
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "query failed", "could not fetch import job")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "query failed", "transaction failed")
		return
	}

	writeSuccess(w, http.StatusOK, "import job retrieved", importStatusFromRow(row))
}

// importStatusFromRow converts a GetImportJobByUUIDRow to the API response shape.
func importStatusFromRow(row db.GetImportJobByUUIDRow) importStatusResponse {
	resp := importStatusResponse{
		UUID:         row.Uuid.String(),
		LogbookUUID:  row.LogbookUuid.String(),
		Status:       row.Status,
		TotalRecords: row.TotalRecords,
		Imported:     row.Imported,
		Duplicate:    row.Duplicate,
		Skipped:      row.Skipped,
		Errors:       row.Errors,
		Warnings:     row.Warnings,
		Filename:     row.Filename,
		CreatedAt:    row.CreatedAt.Time.UTC().Format(time.RFC3339),
	}

	if row.TotalRecords != nil && *row.TotalRecords > 0 {
		resp.PctComplete = float64(row.Imported) / float64(*row.TotalRecords) * 100
		if resp.PctComplete > 100 {
			resp.PctComplete = 100
		}
	}
	if row.Status == "complete" || row.Status == "error" {
		resp.PctComplete = 100
	}

	if row.StartedAt.Valid {
		s := row.StartedAt.Time.UTC().Format(time.RFC3339)
		resp.StartedAt = &s
	}
	if row.CompletedAt.Valid {
		s := row.CompletedAt.Time.UTC().Format(time.RFC3339)
		resp.CompletedAt = &s
	}

	return resp
}

// NewRiverClientForPool creates a River client wired to the given pgxpool.Pool.
// The River client manages the worker goroutines; start/stop it alongside the server.
// Workers must be registered with river.AddWorker before calling Start.
func NewRiverClientForPool(pool *pgxpool.Pool, workers *river.Workers, periodicJobs ...*river.PeriodicJob) (*river.Client[pgx.Tx], error) {
	riverClient, err := river.NewClient(riverpgxv5.New(pool), &river.Config{
		Queues: map[string]river.QueueConfig{
			river.QueueDefault: {MaxWorkers: 4},
		},
		Workers:      workers,
		PeriodicJobs: periodicJobs,
	})
	if err != nil {
		return nil, fmt.Errorf("create river client: %w", err)
	}
	return riverClient, nil
}
