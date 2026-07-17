// Package handler — lotw.go: HTTP handlers for LoTW (Logbook of the World) integration.
//
// LoTW uses a two-step flow: sign QSOs via the lotw-vault microservice, then upload
// the resulting .tq8 blob to ARRL. This handler manages the REST surface while the
// actual signing and uploading happens asynchronously in LoTWSyncWorker.
//
// All endpoints require authentication. The vault user ID is derived from the
// authenticated identity:
//   - Local auth:   "local:{user_id}"
//   - Zitadel auth: "zitadel:{subject}"
//
// Endpoints:
//
//	POST   /v1/lotw/cert                  — upload .p12 certificate to vault
//	GET    /v1/lotw/cert                  — get certificate info (no password)
//	POST   /v1/lotw/cert/delete           — delete certificate
//	POST   /v1/lotw/cert/rotate-password  — rotate vault password
//	POST   /v1/lotw/sync                  — trigger sync job
//	GET    /v1/lotw/sync/status           — get job status by ID
//	GET    /v1/lotw/sync/pending          — count pending (unsent) QSOs
//	GET    /v1/lotw/sync/history          — paginated job history
//	GET    /v1/lotw/settings              — get LoTW preferences
//	PUT    /v1/lotw/settings              — update preferences
package handler

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/FtlC-ian/radioledger/api/internal/auth"
	"github.com/FtlC-ian/radioledger/api/internal/crypto"
	lotwsvc "github.com/FtlC-ian/radioledger/api/internal/services/lotw"
	syncsvc "github.com/FtlC-ian/radioledger/api/internal/services/sync"
)

// LoTWHandler handles all LoTW REST endpoints.
type LoTWHandler struct {
	pool        *pgxpool.Pool
	riverClient *river.Client[pgx.Tx]
	vaultClient *lotwsvc.VaultClient
	keyring     *crypto.Keyring
}

// NewLoTWHandler creates a LoTWHandler.
// riverClient may be nil in environments where background jobs are disabled.
// keyring is used to encrypt the auto-generated vault password in user_service_credentials.
func NewLoTWHandler(pool *pgxpool.Pool, riverClient *river.Client[pgx.Tx], vaultClient *lotwsvc.VaultClient, keyring *crypto.Keyring) *LoTWHandler {
	return &LoTWHandler{
		pool:        pool,
		riverClient: riverClient,
		vaultClient: vaultClient,
		keyring:     keyring,
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Certificate management
// ──────────────────────────────────────────────────────────────────────────────

// ImportCert handles POST /v1/lotw/cert.
// Accepts a multipart form with fields: cert (file), cert_password.
// A vault password is auto-generated and stored encrypted — users never see it.
// Forwards the .p12 to the vault and returns the parsed certificate metadata.
func (h *LoTWHandler) ImportCert(w http.ResponseWriter, r *http.Request) {
	info, ok := auth.UserInfoFromContext(r.Context())
	if !ok {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", "no authenticated user")
		return
	}
	vaultUserID := vaultUserIDFromInfo(info)

	const maxUploadSize = 2 * 1024 * 1024 // 2 MB — .p12 files are tiny
	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid multipart form", err.Error())
		return
	}

	certFile, _, err := r.FormFile("cert")
	if err != nil {
		writeFailure(w, http.StatusBadRequest, "missing cert file", "field 'cert' is required")
		return
	}
	defer func() { _ = certFile.Close() }()

	p12Data, err := io.ReadAll(io.LimitReader(certFile, maxUploadSize))
	if err != nil {
		writeFailure(w, http.StatusBadRequest, "failed to read cert file", err.Error())
		return
	}
	if len(p12Data) == 0 {
		writeFailure(w, http.StatusBadRequest, "empty cert file", "uploaded file is empty")
		return
	}

	certPassword := r.FormValue("cert_password")

	// Auto-generate a cryptographically random vault password.
	// The user never sees or enters this — it's stored encrypted in user_service_credentials
	// and loaded by the sync worker automatically.
	rawKey := make([]byte, 32)
	if _, err := rand.Read(rawKey); err != nil {
		writeFailure(w, http.StatusInternalServerError, "cert import failed", "could not generate vault password")
		return
	}
	vaultPassword := base64.URLEncoding.EncodeToString(rawKey)

	certInfo, err := h.vaultClient.ImportCert(r.Context(), vaultUserID, p12Data, certPassword, vaultPassword)
	if err != nil {
		status, msg := vaultErrToHTTP(err)
		writeFailure(w, status, "cert import failed", msg)
		return
	}

	// Store the auto-generated vault password encrypted in user_service_credentials.
	if storeErr := h.storeVaultPassword(r, info.UserID, vaultPassword); storeErr != nil {
		// This is a serious problem — cert imported but password not stored means sync will fail.
		// Log at error level and return a 500 so the user retries.
		slog.ErrorContext(r.Context(), "failed to store vault password after cert import — cert state may be inconsistent",
			slog.Int64("user_id", info.UserID), slog.String("error", storeErr.Error()))
		writeFailure(w, http.StatusInternalServerError, "cert import failed",
			"certificate was imported but the signing key could not be saved. Please try again.")
		return
	}

	// Mark has_cert = true in lotw_sync_status.
	if upsertErr := h.upsertHasCert(r, info.UserID, true); upsertErr != nil {
		slog.WarnContext(r.Context(), "failed to upsert lotw_sync_status after cert import", slog.String("error", upsertErr.Error()))
	}

	// Mark LoTW as configured in user_service_credentials so the sync dashboard shows it.
	if upsertErr := h.upsertLotwCredential(r, info.UserID, true); upsertErr != nil {
		slog.WarnContext(r.Context(), "failed to upsert user_service_credentials for lotw", slog.String("error", upsertErr.Error()))
	}

	// Backfill sync_status rows for LoTW so the dashboard shows accurate counts.
	// Run in background to avoid blocking the HTTP response.
	// Use context.WithoutCancel so the goroutine outlives the request while preserving
	// tracing/logging values; add a timeout so it doesn't run indefinitely.
	if h.pool != nil {
		go func(uid int64) {
			ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Minute)
			defer cancel()
			log := slog.With(slog.Int64("user_id", uid), slog.String("service", "lotw"))
			counts, err := syncsvc.BackfillSyncStatusForService(ctx, h.pool, uid, "lotw")
			if err != nil {
				log.ErrorContext(ctx, "lotw cert import: sync_status backfill failed", slog.String("error", err.Error()))
				return
			}
			log.InfoContext(ctx, "lotw cert import: sync_status backfill complete",
				slog.Int64("confirmed", counts.Confirmed),
				slog.Int64("pending", counts.Pending),
			)
		}(info.UserID)
	}

	writeSuccess(w, http.StatusOK, "certificate imported", certInfo)
}

// GetCertInfo handles GET /v1/lotw/cert.
// Returns public certificate metadata from the vault. No password required.
func (h *LoTWHandler) GetCertInfo(w http.ResponseWriter, r *http.Request) {
	info, ok := auth.UserInfoFromContext(r.Context())
	if !ok {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", "no authenticated user")
		return
	}
	vaultUserID := vaultUserIDFromInfo(info)

	certInfo, err := h.vaultClient.GetCertInfo(r.Context(), vaultUserID)
	if err != nil {
		status, msg := vaultErrToHTTP(err)
		writeFailure(w, status, "cert info unavailable", msg)
		return
	}

	writeSuccess(w, http.StatusOK, "cert info", certInfo)
}

// DeleteCert handles POST /v1/lotw/cert/delete.
// The vault password is loaded automatically from user_service_credentials.
func (h *LoTWHandler) DeleteCert(w http.ResponseWriter, r *http.Request) {
	info, ok := auth.UserInfoFromContext(r.Context())
	if !ok {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", "no authenticated user")
		return
	}
	vaultUserID := vaultUserIDFromInfo(info)

	// Load the stored vault password.
	vaultPassword, err := h.loadVaultPassword(r, info.UserID)
	if err != nil {
		slog.ErrorContext(r.Context(), "failed to load vault password for cert delete", slog.String("error", err.Error()))
		writeFailure(w, http.StatusInternalServerError, "cert deletion failed", "could not load signing key")
		return
	}
	if vaultPassword == "" {
		writeFailure(w, http.StatusBadRequest, "no certificate", "no certificate or signing key found")
		return
	}

	if err := h.vaultClient.DeleteCert(r.Context(), vaultUserID, vaultPassword); err != nil {
		status, msg := vaultErrToHTTP(err)
		writeFailure(w, status, "cert deletion failed", msg)
		return
	}

	// Update has_cert to false.
	if upsertErr := h.upsertHasCert(r, info.UserID, false); upsertErr != nil {
		slog.WarnContext(r.Context(), "failed to update lotw_sync_status after cert delete", slog.String("error", upsertErr.Error()))
	}

	// Deactivate LoTW in user_service_credentials.
	if upsertErr := h.upsertLotwCredential(r, info.UserID, false); upsertErr != nil {
		slog.WarnContext(r.Context(), "failed to deactivate user_service_credentials for lotw", slog.String("error", upsertErr.Error()))
	}

	writeSuccess(w, http.StatusOK, "certificate deleted", nil)
}

type rotatePasswordRequest struct {
	OldPassword string `json:"old_password"`
	NewPassword string `json:"new_password"`
}

// RotatePassword handles POST /v1/lotw/cert/rotate-password.
func (h *LoTWHandler) RotatePassword(w http.ResponseWriter, r *http.Request) {
	info, ok := auth.UserInfoFromContext(r.Context())
	if !ok {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", "no authenticated user")
		return
	}
	vaultUserID := vaultUserIDFromInfo(info)

	var req rotatePasswordRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid body", err.Error())
		return
	}
	if req.OldPassword == "" || req.NewPassword == "" {
		writeFailure(w, http.StatusBadRequest, "missing passwords", "old_password and new_password are required")
		return
	}

	if err := h.vaultClient.RotatePassword(r.Context(), vaultUserID, req.OldPassword, req.NewPassword); err != nil {
		status, msg := vaultErrToHTTP(err)
		writeFailure(w, status, "password rotation failed", msg)
		return
	}

	writeSuccess(w, http.StatusOK, "password rotated", nil)
}

// ──────────────────────────────────────────────────────────────────────────────
// Sync
// ──────────────────────────────────────────────────────────────────────────────

type lotwTriggerSyncRequest struct {
	QSOIDs []int64 `json:"qso_ids,omitempty"`
}

type triggerSyncResponse struct {
	JobID    int64 `json:"job_id"`
	QSOCount int   `json:"qso_count"`
}

// TriggerSync handles POST /v1/lotw/sync.
// Creates a lotw_sync_jobs row and enqueues a River LoTWSyncArgs job.
func (h *LoTWHandler) TriggerSync(w http.ResponseWriter, r *http.Request) {
	info, ok := auth.UserInfoFromContext(r.Context())
	if !ok {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", "no authenticated user")
		return
	}
	vaultUserID := vaultUserIDFromInfo(info)

	var req lotwTriggerSyncRequest
	// Body is optional — omitting it syncs all pending QSOs.
	_ = decodeJSONBody(r, &req)

	// Resolve QSO IDs: if none specified, fetch all pending (lotw_sent_at IS NULL).
	qsoIDs := req.QSOIDs
	if len(qsoIDs) == 0 {
		var err error
		qsoIDs, err = h.fetchPendingLoTWQSOIDs(r, info.UserID)
		if err != nil {
			slog.ErrorContext(r.Context(), "failed to fetch pending lotw qso ids", slog.String("error", err.Error()))
			writeFailure(w, http.StatusInternalServerError, "sync trigger failed", "could not fetch pending QSOs")
			return
		}
	}
	if len(qsoIDs) == 0 {
		writeSuccess(w, http.StatusOK, "no pending QSOs to sync", triggerSyncResponse{QSOCount: 0})
		return
	}

	if h.riverClient == nil {
		writeFailure(w, http.StatusServiceUnavailable, "background jobs not available", "river client is not configured")
		return
	}

	// Create the lotw_sync_jobs tracking row.
	qsoIDsJSON, err := json.Marshal(qsoIDs)
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "sync trigger failed", "could not marshal qso ids")
		return
	}
	var jobID int64
	err = h.pool.QueryRow(r.Context(), `
		INSERT INTO lotw_sync_jobs (user_id, status, qso_count, qso_ids)
		VALUES ($1, 'pending', $2, $3::jsonb)
		RETURNING id
	`, info.UserID, len(qsoIDs), qsoIDsJSON).Scan(&jobID)
	if err != nil {
		slog.ErrorContext(r.Context(), "failed to create lotw_sync_jobs row", slog.String("error", err.Error()))
		writeFailure(w, http.StatusInternalServerError, "sync trigger failed", "could not create job row")
		return
	}

	// Enqueue the River job. The vault password is loaded from the credential store by the worker.
	if err := syncsvc.EnqueueLoTWSync(r.Context(), h.riverClient, jobID, info.UserID, vaultUserID); err != nil {
		slog.ErrorContext(r.Context(), "failed to enqueue lotw sync job", slog.String("error", err.Error()))
		// Mark the job as failed so it doesn't linger as 'pending'.
		_, _ = h.pool.Exec(r.Context(), `
			UPDATE lotw_sync_jobs SET status = 'failed', error = 'failed to enqueue' WHERE id = $1
		`, jobID)
		writeFailure(w, http.StatusInternalServerError, "sync trigger failed", "could not enqueue job")
		return
	}

	if payload, err := h.loadStoredPasswords(r, info.UserID); err == nil && strings.TrimSpace(payload.WebPassword) != "" {
		if callsign, callErr := h.resolveLoTWLoginCallsign(r, info); callErr == nil && callsign != "" {
			lastPullAt, _ := h.fetchLastPullAt(r, info.UserID)
			if _, pullErr := syncsvc.EnqueueLoTWConfirmationPull(r.Context(), h.riverClient, info.UserID, callsign, lastPullAt, 30*time.Second); pullErr != nil {
				slog.WarnContext(r.Context(), "failed to enqueue lotw confirmation pull after sync",
					slog.String("error", pullErr.Error()))
			}
		}
	}

	writeSuccess(w, http.StatusAccepted, "LoTW sync queued", triggerSyncResponse{
		JobID:    jobID,
		QSOCount: len(qsoIDs),
	})
}

type pullConfirmationsResponse struct {
	JobID int64 `json:"job_id"`
}

// PullConfirmations handles POST /v1/lotw/pull-confirmations.
// It enqueues a River job that downloads newly confirmed LoTW QSOs and matches
// them back to local records.
func (h *LoTWHandler) PullConfirmations(w http.ResponseWriter, r *http.Request) {
	info, ok := auth.UserInfoFromContext(r.Context())
	if !ok {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", "no authenticated user")
		return
	}
	if h.riverClient == nil {
		writeFailure(w, http.StatusServiceUnavailable, "background jobs not available", "river client is not configured")
		return
	}

	payload, err := h.loadStoredPasswords(r, info.UserID)
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "confirmation pull failed", err.Error())
		return
	}
	if strings.TrimSpace(payload.WebPassword) == "" {
		writeFailure(w, http.StatusBadRequest, "confirmation pull failed", "save your LoTW web password in Settings before pulling confirmations")
		return
	}

	callsign, err := h.resolveLoTWLoginCallsign(r, info)
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "confirmation pull failed", err.Error())
		return
	}
	if callsign == "" {
		writeFailure(w, http.StatusBadRequest, "confirmation pull failed", "no callsign is configured for this account")
		return
	}

	lastPullAt, err := h.fetchLastPullAt(r, info.UserID)
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "confirmation pull failed", err.Error())
		return
	}

	jobID, err := syncsvc.EnqueueLoTWConfirmationPull(r.Context(), h.riverClient, info.UserID, callsign, lastPullAt, 0)
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "confirmation pull failed", "could not enqueue job")
		return
	}

	writeSuccess(w, http.StatusAccepted, "LoTW confirmation pull queued", pullConfirmationsResponse{JobID: jobID})
}

type syncJobStatusResponse struct {
	ID           int64      `json:"id"`
	Status       string     `json:"status"`
	QSOCount     int        `json:"qso_count"`
	TQ8Size      *int       `json:"tq8_size,omitempty"`
	ARRLResponse *string    `json:"arrl_response,omitempty"`
	Error        *string    `json:"error,omitempty"`
	RetryCount   int        `json:"retry_count"`
	MaxRetries   int        `json:"max_retries"`
	CreatedAt    time.Time  `json:"created_at"`
	StartedAt    *time.Time `json:"started_at,omitempty"`
	CompletedAt  *time.Time `json:"completed_at,omitempty"`
}

// SyncStatus handles GET /v1/lotw/sync/status?job_id=N.
func (h *LoTWHandler) SyncStatus(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	jobIDStr := r.URL.Query().Get("job_id")
	jobID, err := strconv.ParseInt(jobIDStr, 10, 64)
	if err != nil || jobID <= 0 {
		writeFailure(w, http.StatusBadRequest, "invalid job_id", "job_id must be a positive integer")
		return
	}

	var resp syncJobStatusResponse
	var createdAt, startedAt, completedAt pgtype.Timestamptz
	err = h.pool.QueryRow(r.Context(), `
		SELECT id, status, qso_count, tq8_size, arrl_response, error,
		       retry_count, max_retries, created_at, started_at, completed_at
		FROM lotw_sync_jobs
		WHERE id = $1 AND user_id = $2
	`, jobID, userID).Scan(
		&resp.ID, &resp.Status, &resp.QSOCount, &resp.TQ8Size,
		&resp.ARRLResponse, &resp.Error,
		&resp.RetryCount, &resp.MaxRetries,
		&createdAt, &startedAt, &completedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		writeFailure(w, http.StatusNotFound, "job not found", fmt.Sprintf("no job with id %d", jobID))
		return
	}
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "job status unavailable", err.Error())
		return
	}
	if createdAt.Valid {
		resp.CreatedAt = createdAt.Time
	}
	if startedAt.Valid {
		t := startedAt.Time
		resp.StartedAt = &t
	}
	if completedAt.Valid {
		t := completedAt.Time
		resp.CompletedAt = &t
	}

	writeSuccess(w, http.StatusOK, "job status", resp)
}

type pendingCountResponse struct {
	PendingCount int64 `json:"pending_count"`
}

// SyncPending handles GET /v1/lotw/sync/pending.
// Returns the count of QSOs not yet submitted to LoTW.
func (h *LoTWHandler) SyncPending(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	var count int64
	err = h.pool.QueryRow(r.Context(), `
		SELECT COUNT(*)
		FROM qsos q
		JOIN logbooks lb ON lb.id = q.logbook_id
		WHERE lb.user_id = $1
		  AND q.lotw_sent_at IS NULL
		  AND q.deleted_at IS NULL
	`, userID).Scan(&count)
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "pending count unavailable", err.Error())
		return
	}

	writeSuccess(w, http.StatusOK, "pending count", pendingCountResponse{PendingCount: count})
}

type syncHistoryResponse struct {
	Items      []syncJobStatusResponse `json:"items"`
	Pagination paginationResponse      `json:"pagination"`
}

// SyncHistory handles GET /v1/lotw/sync/history.
// Returns paginated lotw_sync_jobs for the authenticated user.
func (h *LoTWHandler) SyncHistory(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	q := r.URL.Query()
	page := parsePositiveInt(q.Get("page"), 1)
	pageSize := parsePositiveInt(q.Get("page_size"), 20)
	if pageSize > 100 {
		pageSize = 100
	}
	offset := (page - 1) * pageSize

	var total int64
	if err := h.pool.QueryRow(r.Context(),
		`SELECT COUNT(*) FROM lotw_sync_jobs WHERE user_id = $1`, userID,
	).Scan(&total); err != nil {
		writeFailure(w, http.StatusInternalServerError, "history unavailable", err.Error())
		return
	}

	rows, err := h.pool.Query(r.Context(), `
		SELECT id, status, qso_count, tq8_size, arrl_response, error,
		       retry_count, max_retries, created_at, started_at, completed_at
		FROM lotw_sync_jobs
		WHERE user_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`, userID, pageSize, offset)
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "history unavailable", err.Error())
		return
	}
	defer rows.Close()

	items := make([]syncJobStatusResponse, 0)
	for rows.Next() {
		var item syncJobStatusResponse
		var createdAt, startedAt, completedAt pgtype.Timestamptz
		if err := rows.Scan(
			&item.ID, &item.Status, &item.QSOCount, &item.TQ8Size,
			&item.ARRLResponse, &item.Error,
			&item.RetryCount, &item.MaxRetries,
			&createdAt, &startedAt, &completedAt,
		); err != nil {
			writeFailure(w, http.StatusInternalServerError, "history unavailable", err.Error())
			return
		}
		if createdAt.Valid {
			item.CreatedAt = createdAt.Time
		}
		if startedAt.Valid {
			t := startedAt.Time
			item.StartedAt = &t
		}
		if completedAt.Valid {
			t := completedAt.Time
			item.CompletedAt = &t
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		writeFailure(w, http.StatusInternalServerError, "history unavailable", err.Error())
		return
	}

	totalPages := 0
	if total > 0 {
		totalPages = int((total + int64(pageSize) - 1) / int64(pageSize))
	}

	writeSuccess(w, http.StatusOK, "sync history", syncHistoryResponse{
		Items: items,
		Pagination: paginationResponse{
			Page:       page,
			PageSize:   pageSize,
			Total:      total,
			TotalPages: totalPages,
		},
	})
}

// ──────────────────────────────────────────────────────────────────────────────
// Settings
// ──────────────────────────────────────────────────────────────────────────────

type lotwSettingsResponse struct {
	HasCert          bool       `json:"has_cert"`
	HasWebPassword   bool       `json:"has_web_password"`
	AutoSyncPrompt   bool       `json:"auto_sync_prompt"`
	LastSyncAt       *time.Time `json:"last_sync_at,omitempty"`
	LastPullAt       *time.Time `json:"last_pull_at,omitempty"`
	LastSyncQSOCount *int       `json:"last_sync_qso_count,omitempty"`
	LastSyncResult   *string    `json:"last_sync_result,omitempty"`
	LastSyncError    *string    `json:"last_sync_error,omitempty"`
	TotalQSOsSynced  int        `json:"total_qsos_synced"`
}

// GetSettings handles GET /v1/lotw/settings.
// Returns LoTW preferences and aggregate sync state. Creates the row if missing.
func (h *LoTWHandler) GetSettings(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	// Upsert to ensure a row always exists.
	var resp lotwSettingsResponse
	var lastSyncAt, lastPullAt pgtype.Timestamptz
	err = h.pool.QueryRow(r.Context(), `
		INSERT INTO lotw_sync_status (user_id)
		VALUES ($1)
		ON CONFLICT (user_id) DO UPDATE SET updated_at = lotw_sync_status.updated_at
		RETURNING has_cert, auto_sync_prompt, last_sync_at, last_pull_at, last_sync_qso_count,
		          last_sync_result, last_sync_error, total_qsos_synced
	`, userID).Scan(
		&resp.HasCert, &resp.AutoSyncPrompt,
		&lastSyncAt, &lastPullAt, &resp.LastSyncQSOCount,
		&resp.LastSyncResult, &resp.LastSyncError,
		&resp.TotalQSOsSynced,
	)
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "settings unavailable", err.Error())
		return
	}
	if lastSyncAt.Valid {
		t := lastSyncAt.Time
		resp.LastSyncAt = &t
	}
	if lastPullAt.Valid {
		t := lastPullAt.Time
		resp.LastPullAt = &t
	}
	if resp.HasWebPassword, err = h.hasWebPassword(r, userID); err != nil {
		writeFailure(w, http.StatusInternalServerError, "settings unavailable", err.Error())
		return
	}

	writeSuccess(w, http.StatusOK, "lotw settings", resp)
}

type updateSettingsRequest struct {
	AutoSyncPrompt *bool   `json:"auto_sync_prompt"`
	LoTWPassword   *string `json:"lotw_password,omitempty"`
}

// UpdateSettings handles PUT /v1/lotw/settings.
func (h *LoTWHandler) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	var req updateSettingsRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid body", err.Error())
		return
	}
	if req.AutoSyncPrompt == nil && req.LoTWPassword == nil {
		writeFailure(w, http.StatusBadRequest, "nothing to update", "provide auto_sync_prompt and/or lotw_password")
		return
	}

	if req.AutoSyncPrompt != nil {
		_, err = h.pool.Exec(r.Context(), `
			INSERT INTO lotw_sync_status (user_id, auto_sync_prompt)
			VALUES ($1, $2)
			ON CONFLICT (user_id) DO UPDATE SET
			    auto_sync_prompt = EXCLUDED.auto_sync_prompt,
			    updated_at = NOW()
		`, userID, *req.AutoSyncPrompt)
		if err != nil {
			writeFailure(w, http.StatusInternalServerError, "settings update failed", err.Error())
			return
		}
	}

	if req.LoTWPassword != nil {
		if err := h.storeWebPassword(r, userID, strings.TrimSpace(*req.LoTWPassword)); err != nil {
			writeFailure(w, http.StatusBadRequest, "settings update failed", err.Error())
			return
		}
	}

	hasWebPassword, err := h.hasWebPassword(r, userID)
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "settings update failed", err.Error())
		return
	}

	resp := map[string]any{"has_web_password": hasWebPassword}
	if req.AutoSyncPrompt != nil {
		resp["auto_sync_prompt"] = *req.AutoSyncPrompt
	}
	writeSuccess(w, http.StatusOK, "settings updated", resp)
}

// ──────────────────────────────────────────────────────────────────────────────
// Internal helpers
// ──────────────────────────────────────────────────────────────────────────────

// vaultUserIDFromInfo builds the vault user ID string from the authenticated user.
// Format: "local:{user_id}" for local auth, "zitadel:{subject}" for Zitadel.
func vaultUserIDFromInfo(info auth.UserInfo) string {
	if info.ExternalID != "" {
		return "zitadel:" + info.ExternalID
	}
	return fmt.Sprintf("local:%d", info.UserID)
}

func (h *LoTWHandler) resolveLoTWLoginCallsign(r *http.Request, info auth.UserInfo) (string, error) {
	var callsign *string
	if err := h.pool.QueryRow(r.Context(), `SELECT callsign FROM users WHERE id = $1`, info.UserID).Scan(&callsign); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	if callsign != nil && strings.TrimSpace(*callsign) != "" {
		return strings.ToUpper(strings.TrimSpace(*callsign)), nil
	}
	if h.vaultClient == nil {
		return "", nil
	}
	certInfo, err := h.vaultClient.GetCertInfo(r.Context(), vaultUserIDFromInfo(info))
	if err != nil {
		return "", nil
	}
	return strings.ToUpper(strings.TrimSpace(certInfo.Callsign)), nil
}

func (h *LoTWHandler) fetchLastPullAt(r *http.Request, userID int64) (*time.Time, error) {
	var ts pgtype.Timestamptz
	err := h.pool.QueryRow(r.Context(), `SELECT last_pull_at FROM lotw_sync_status WHERE user_id = $1`, userID).Scan(&ts)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !ts.Valid {
		return nil, nil
	}
	t := ts.Time.UTC()
	return &t, nil
}

// vaultErrToHTTP maps vault sentinel errors to appropriate HTTP status codes and messages.
func vaultErrToHTTP(err error) (int, string) {
	switch {
	case errors.Is(err, lotwsvc.ErrCertAlreadyExists):
		return http.StatusConflict, "A certificate is already stored. Delete it before uploading a new one."
	case errors.Is(err, lotwsvc.ErrInvalidCert):
		return http.StatusBadRequest, "The certificate is invalid or the .p12 password is wrong."
	case errors.Is(err, lotwsvc.ErrWrongPassword):
		return http.StatusUnauthorized, "Incorrect vault password."
	case errors.Is(err, lotwsvc.ErrNoCert):
		return http.StatusNotFound, "No certificate found. Upload your .p12 certificate first."
	case errors.Is(err, lotwsvc.ErrVaultInternal):
		return http.StatusServiceUnavailable, "The LoTW vault is temporarily unavailable."
	default:
		return http.StatusServiceUnavailable, fmt.Sprintf("Vault error: %s", err)
	}
}

// upsertHasCert updates the has_cert flag in lotw_sync_status for a user.
func (h *LoTWHandler) upsertHasCert(r *http.Request, userID int64, hasCert bool) error {
	_, err := h.pool.Exec(r.Context(), `
		INSERT INTO lotw_sync_status (user_id, has_cert)
		VALUES ($1, $2)
		ON CONFLICT (user_id) DO UPDATE SET
		    has_cert   = EXCLUDED.has_cert,
		    updated_at = NOW()
	`, userID, hasCert)
	return err
}

// loadStoredPasswords retrieves and decrypts the stored LoTW credential payload.
// Returns an empty payload when no credential is found.
func (h *LoTWHandler) loadStoredPasswords(r *http.Request, userID int64) (lotwsvc.StoredPasswords, error) {
	if h.keyring == nil {
		return lotwsvc.StoredPasswords{}, fmt.Errorf("keyring not configured")
	}
	row := h.pool.QueryRow(r.Context(), `
		SELECT credentials, key_version
		FROM user_service_credentials
		WHERE user_id = $1 AND service = 'lotw'
	`, userID)

	var ciphertext []byte
	var keyVersion int32
	if err := row.Scan(&ciphertext, &keyVersion); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return lotwsvc.StoredPasswords{}, nil
		}
		return lotwsvc.StoredPasswords{}, fmt.Errorf("fetch lotw credentials: %w", err)
	}
	plaintext, err := h.keyring.Decrypt(userID, keyVersion, ciphertext)
	if err != nil {
		return lotwsvc.StoredPasswords{}, fmt.Errorf("decrypt lotw credentials: %w", err)
	}
	payload, err := lotwsvc.DecodeStoredPasswords(plaintext)
	if err != nil {
		return lotwsvc.StoredPasswords{}, err
	}
	return payload, nil
}

// loadVaultPassword retrieves and decrypts the stored vault password for LoTW.
// Returns ("", nil) if no credential is found.
func (h *LoTWHandler) loadVaultPassword(r *http.Request, userID int64) (string, error) {
	payload, err := h.loadStoredPasswords(r, userID)
	if err != nil {
		return "", err
	}
	return payload.VaultPassword, nil
}

func (h *LoTWHandler) hasWebPassword(r *http.Request, userID int64) (bool, error) {
	if h.keyring == nil {
		// Keyring not configured (RADIOLEDGER_MASTER_KEY not set).
		// Credentials cannot be read, but this is not an error for a read-only
		// settings fetch — just report that no web password is stored.
		return false, nil
	}
	payload, err := h.loadStoredPasswords(r, userID)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(payload.WebPassword) != "", nil
}

func (h *LoTWHandler) storeLoTWPasswords(r *http.Request, userID int64, payload lotwsvc.StoredPasswords, active bool) error {
	if h.keyring == nil {
		return fmt.Errorf("keyring not configured (RADIOLEDGER_MASTER_KEY not set)")
	}
	encoded, err := lotwsvc.EncodeStoredPasswords(payload)
	if err != nil {
		return err
	}
	ciphertext, keyVersion, err := h.keyring.Encrypt(userID, encoded)
	if err != nil {
		return fmt.Errorf("encrypt lotw credentials: %w", err)
	}
	_, err = h.pool.Exec(r.Context(), `
		INSERT INTO user_service_credentials (user_id, service, credential_type, credentials, key_version, is_active)
		VALUES ($1, 'lotw', 'username_password', $2, $3, $4)
		ON CONFLICT (user_id, service) DO UPDATE SET
		    credential_type = 'username_password',
		    credentials     = EXCLUDED.credentials,
		    key_version     = EXCLUDED.key_version,
		    is_active       = EXCLUDED.is_active,
		    updated_at      = NOW()
	`, userID, ciphertext, keyVersion, active)
	return err
}

// storeVaultPassword encrypts and upserts the auto-generated vault password into
// user_service_credentials for service="lotw". This is called immediately after a
// successful cert import so the sync worker can retrieve it without user input.
func (h *LoTWHandler) storeVaultPassword(r *http.Request, userID int64, plaintext string) error {
	payload, err := h.loadStoredPasswords(r, userID)
	if err != nil {
		return err
	}
	payload.VaultPassword = strings.TrimSpace(plaintext)
	return h.storeLoTWPasswords(r, userID, payload, true)
}

func (h *LoTWHandler) storeWebPassword(r *http.Request, userID int64, plaintext string) error {
	var hasCert bool
	if err := h.pool.QueryRow(r.Context(), `SELECT COALESCE(has_cert, FALSE) FROM lotw_sync_status WHERE user_id = $1`, userID).Scan(&hasCert); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if !hasCert {
		return fmt.Errorf("import your LoTW certificate before saving a LoTW web password")
	}
	payload, err := h.loadStoredPasswords(r, userID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(payload.VaultPassword) == "" {
		return fmt.Errorf("import your LoTW certificate before saving a LoTW web password")
	}
	payload.WebPassword = strings.TrimSpace(plaintext)
	return h.storeLoTWPasswords(r, userID, payload, true)
}

// upsertLotwCredential marks LoTW as configured (or not) in user_service_credentials
// so the sync dashboard service health check picks it up.
// Note: on deactivation (cert delete) we mark is_active=FALSE but do NOT wipe
// the stored password, so a re-import overwrites it cleanly via storeVaultPassword.
func (h *LoTWHandler) upsertLotwCredential(r *http.Request, userID int64, active bool) error {
	if active {
		// Activating: only set is_active — the real creds are written by storeVaultPassword.
		_, err := h.pool.Exec(r.Context(), `
			UPDATE user_service_credentials
			SET is_active = TRUE, updated_at = NOW()
			WHERE user_id = $1 AND service = 'lotw'
		`, userID)
		return err
	}
	_, err := h.pool.Exec(r.Context(), `
		UPDATE user_service_credentials
		SET is_active = FALSE, updated_at = NOW()
		WHERE user_id = $1 AND service = 'lotw'
	`, userID)
	return err
}

// fetchPendingLoTWQSOIDs returns IDs of QSOs not yet submitted to LoTW for a user.
func (h *LoTWHandler) fetchPendingLoTWQSOIDs(r *http.Request, userID int64) ([]int64, error) {
	const maxPending = 5000
	rows, err := h.pool.Query(r.Context(), `
		SELECT q.id
		FROM qsos q
		JOIN logbooks lb ON lb.id = q.logbook_id
		WHERE lb.user_id = $1
		  AND q.lotw_sent_at IS NULL
		  AND q.deleted_at IS NULL
		ORDER BY q.datetime_on ASC
		LIMIT $2
	`, userID, maxPending)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
