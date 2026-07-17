package handler

import (
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/FtlC-ian/radioledger/api/internal/crypto"
	syncsvc "github.com/FtlC-ian/radioledger/api/internal/services/sync"
)

// SyncHandler handles sync status and trigger endpoints.
type SyncHandler struct {
	pool            *pgxpool.Pool
	riverClient     *river.Client[pgx.Tx]
	credentialStore syncsvc.CredentialStore
}

// NewSyncHandler creates a SyncHandler.
func NewSyncHandler(pool *pgxpool.Pool, riverClient *river.Client[pgx.Tx], keyring *crypto.Keyring) *SyncHandler {
	var store syncsvc.CredentialStore
	if keyring != nil {
		store = syncsvc.NewPostgresStore(pool, keyring)
	}

	return &SyncHandler{
		pool:            pool,
		riverClient:     riverClient,
		credentialStore: store,
	}
}

type qsoServiceStatusResponse struct {
	ID           int64      `json:"id"`
	Service      string     `json:"service"`
	Status       string     `json:"status"`
	LastSyncedAt *time.Time `json:"last_synced_at,omitempty"`
	RemoteID     *string    `json:"remote_id,omitempty"`
	ErrorMessage *string    `json:"error_message,omitempty"`
	ErrorCode    *string    `json:"error_code,omitempty"`
	RetryCount   int16      `json:"retry_count"`
	NextRetryAt  *time.Time `json:"next_retry_at,omitempty"`
}

type qsoStatusItemResponse struct {
	QSOUUID         string                     `json:"qso_uuid"`
	Callsign        string                     `json:"callsign"`
	Band            string                     `json:"band"`
	Mode            string                     `json:"mode"`
	DatetimeOn      time.Time                  `json:"datetime_on"`
	HasConflict     bool                       `json:"has_conflict"`
	ConflictID      *int64                     `json:"conflict_id,omitempty"`
	ServiceStatuses []qsoServiceStatusResponse `json:"service_statuses"`
}

type paginationResponse struct {
	Page       int   `json:"page"`
	PageSize   int   `json:"page_size"`
	Total      int64 `json:"total"`
	TotalPages int   `json:"total_pages"`
}

type serviceProgressResponse struct {
	PendingCount      int64      `json:"pending_count"`
	UploadedCount     int64      `json:"uploaded_count"`
	FailedCount       int64      `json:"failed_count"`
	TotalCount        int64      `json:"total_count"`
	LastActivityAt    *time.Time `json:"last_activity_at,omitempty"`
	LastError         *string    `json:"last_error,omitempty"`
	ErrorMessage      *string    `json:"error_message,omitempty"`
	HasPermanentError bool       `json:"has_permanent_error"`
	IsRunning         bool       `json:"is_running"`
	IsStalled         bool       `json:"is_stalled"`
}

// Status returns paginated QSO sync status rows.
//
// GET /v1/sync/status
func (h *SyncHandler) Status(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	q := r.URL.Query()
	page := parsePositiveInt(q.Get("page"), 1)
	pageSize := parsePositiveInt(q.Get("page_size"), 25)
	if pageSize > 200 {
		pageSize = 200
	}

	dateFrom := parseDateQuery(q.Get("date_from"))
	dateTo := parseDateQuery(q.Get("date_to"))

	rows, total, err := syncsvc.GetQSOSyncStatus(r.Context(), h.pool, userID, syncsvc.SyncStatusFilter{
		Service:  strings.TrimSpace(q.Get("service")),
		Status:   strings.TrimSpace(q.Get("status")),
		Callsign: strings.TrimSpace(q.Get("callsign")),
		DateFrom: dateFrom,
		DateTo:   dateTo,
		Page:     page,
		PageSize: pageSize,
	})
	if err != nil {
		slog.ErrorContext(r.Context(), "sync status query failed", slog.String("error", err.Error()))
		writeFailure(w, http.StatusInternalServerError, "sync status unavailable", "query failed")
		return
	}

	servicesSet := map[string]struct{}{}
	items := make([]qsoStatusItemResponse, 0, len(rows))
	for _, row := range rows {
		svc := make([]qsoServiceStatusResponse, 0, len(row.ServiceStatuses))
		for _, status := range row.ServiceStatuses {
			servicesSet[status.Service] = struct{}{}
			svc = append(svc, qsoServiceStatusResponse{
				ID:           status.SyncID,
				Service:      status.Service,
				Status:       status.Status,
				LastSyncedAt: status.LastSyncedAt,
				RemoteID:     status.RemoteID,
				ErrorMessage: status.ErrorMessage,
				ErrorCode:    status.ErrorCode,
				RetryCount:   status.RetryCount,
				NextRetryAt:  status.NextRetryAt,
			})
		}
		items = append(items, qsoStatusItemResponse{
			QSOUUID:         row.QSOUUID,
			Callsign:        row.Callsign,
			Band:            row.Band,
			Mode:            row.Mode,
			DatetimeOn:      row.DatetimeOn,
			HasConflict:     row.HasConflict,
			ConflictID:      row.ConflictID,
			ServiceStatuses: svc,
		})
	}

	services := make([]string, 0, len(servicesSet))
	for svc := range servicesSet {
		services = append(services, svc)
	}
	sort.Strings(services)

	totalPages := 0
	if total > 0 {
		totalPages = int((total + int64(pageSize) - 1) / int64(pageSize))
	}

	progressRows, err := syncsvc.GetSyncProgressByService(r.Context(), h.pool, userID)
	if err != nil {
		slog.ErrorContext(r.Context(), "sync progress query failed", slog.String("error", err.Error()))
		writeFailure(w, http.StatusInternalServerError, "sync status unavailable", "query failed")
		return
	}

	progress := make(map[string]serviceProgressResponse, len(progressRows))
	for _, row := range progressRows {
		progress[row.Service] = serviceProgressResponse{
			PendingCount:      row.PendingCount,
			UploadedCount:     row.UploadedCount,
			FailedCount:       row.FailedCount,
			TotalCount:        row.TotalCount,
			LastActivityAt:    row.LastActivityAt,
			LastError:         row.LastError,
			ErrorMessage:      row.ErrorMessage,
			HasPermanentError: row.HasPermanentError,
			IsRunning:         row.IsRunning,
			IsStalled:         row.IsStalled,
		}
	}

	writeSuccess(w, http.StatusOK, "sync status", map[string]any{
		"items":           items,
		"services":        progress,
		"service_filters": services,
		"pagination": paginationResponse{
			Page:       page,
			PageSize:   pageSize,
			Total:      total,
			TotalPages: totalPages,
		},
	})
}

// servicesHealthResponse is the top-level response for GET /v1/sync/services.
type servicesHealthResponse struct {
	Services map[string]string `json:"services"`
}

// Services returns runtime health for each sync service.
//
// GET /v1/sync/services
func (h *SyncHandler) Services(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	health, err := syncsvc.GetServiceHealthForUser(r.Context(), h.pool, userID)
	if err != nil {
		slog.ErrorContext(r.Context(), "sync services query failed", slog.String("error", err.Error()))
		writeFailure(w, http.StatusInternalServerError, "sync services unavailable", "query failed")
		return
	}

	services := make(map[string]string, len(health))
	for _, h := range health {
		services[h.Service] = h.Status
	}

	writeSuccess(w, http.StatusOK, "sync services", servicesHealthResponse{Services: services})
}

// TriggerSync manually triggers a sync for the specified service.
//
// POST /v1/sync/trigger/{service}
func (h *SyncHandler) TriggerSync(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	service := chi.URLParam(r, "service")
	if err := h.enqueueServiceJobs(r, userID, service); err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "unknown service") {
			status = http.StatusBadRequest
		}
		if strings.Contains(err.Error(), "not yet implemented") {
			status = http.StatusNotImplemented
		}
		if strings.Contains(err.Error(), "not configured") {
			status = http.StatusServiceUnavailable
		}
		writeFailure(w, status, "trigger failed", err.Error())
		return
	}

	writeSuccess(w, http.StatusAccepted, fmt.Sprintf("%s sync triggered", service), nil)
}

type bulkUploadRequest struct {
	Service string `json:"service"`
}

// BulkUpload triggers upload of all unsynced QSOs for a service.
//
// POST /v1/sync/bulk-upload
func (h *SyncHandler) BulkUpload(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	var req bulkUploadRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid body", err.Error())
		return
	}
	service := strings.TrimSpace(req.Service)
	if service == "" {
		writeFailure(w, http.StatusBadRequest, "invalid service", "service is required")
		return
	}

	if !isUploadService(service) {
		writeFailure(w, http.StatusBadRequest, "invalid service", "supported: eqsl, clublog, qrz, pota")
		return
	}

	updated, err := syncsvc.BulkUploadUnsynced(r.Context(), h.pool, userID, service)
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "bulk upload failed", err.Error())
		return
	}

	if err := h.enqueueServiceJobs(r, userID, service); err != nil {
		writeFailure(w, http.StatusServiceUnavailable, "bulk upload failed", err.Error())
		return
	}

	writeSuccess(w, http.StatusAccepted, "bulk upload enqueued", map[string]any{
		"service":        service,
		"queued_qsos":    updated,
		"triggered_jobs": true,
	})
}

type verifyCredentialsRequest struct {
	Service string `json:"service"`
}

type verifyCredentialsResult struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// VerifyCredentials validates already-configured credentials for a sync service.
//
// POST /v1/sync/verify-credentials
func (h *SyncHandler) VerifyCredentials(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}
	if h.credentialStore == nil {
		writeFailure(w, http.StatusServiceUnavailable, "encryption not configured", "RADIOLEDGER_MASTER_KEY is not set")
		return
	}

	var req verifyCredentialsRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid body", err.Error())
		return
	}

	service := strings.ToLower(strings.TrimSpace(req.Service))
	if service != "qrz" && service != "eqsl" && service != "clublog" && service != "pota" {
		writeFailure(w, http.StatusBadRequest, "invalid service", "service must be one of: qrz, eqsl, clublog, pota")
		return
	}

	if err := h.credentialStore.Verify(r.Context(), userID, service); err != nil {
		msg := err.Error()
		switch {
		case strings.Contains(msg, "no credentials"):
			writeSuccess(w, http.StatusOK, "credential verification failed", verifyCredentialsResult{
				Success: false,
				Error:   fmt.Sprintf("No %s credentials configured", strings.ToUpper(service)),
			})
		case strings.Contains(msg, "credential verification failed"), strings.Contains(msg, "authentication failed"):
			writeSuccess(w, http.StatusOK, "credential verification failed", verifyCredentialsResult{
				Success: false,
				Error:   msg,
			})
		default:
			slog.ErrorContext(r.Context(), "sync credential verification failed", slog.String("service", service), slog.String("error", msg))
			writeSuccess(w, http.StatusOK, "credential verification failed", verifyCredentialsResult{
				Success: false,
				Error:   "Could not verify credentials right now",
			})
		}
		return
	}

	writeSuccess(w, http.StatusOK, "credential verified", verifyCredentialsResult{Success: true})
}

type conflictItemResponse struct {
	ID             int64                     `json:"id"`
	QSOUUID        string                    `json:"qso_uuid"`
	Callsign       string                    `json:"callsign"`
	Band           string                    `json:"band"`
	Mode           string                    `json:"mode"`
	DatetimeOn     time.Time                 `json:"datetime_on"`
	ServiceA       string                    `json:"service_a"`
	ServiceB       string                    `json:"service_b"`
	FieldConflicts map[string]map[string]any `json:"field_conflicts"`
	Status         string                    `json:"status"`
	CreatedAt      time.Time                 `json:"created_at"`
	UpdatedAt      time.Time                 `json:"updated_at"`
}

// Conflicts lists open sync conflicts for the user.
//
// GET /v1/sync/conflicts
func (h *SyncHandler) Conflicts(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	page := parsePositiveInt(r.URL.Query().Get("page"), 1)
	pageSize := parsePositiveInt(r.URL.Query().Get("page_size"), 25)

	rows, total, err := syncsvc.GetSyncConflicts(r.Context(), h.pool, userID, page, pageSize)
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "sync conflicts unavailable", err.Error())
		return
	}

	items := make([]conflictItemResponse, 0, len(rows))
	for _, row := range rows {
		items = append(items, conflictItemResponse{
			ID:             row.ID,
			QSOUUID:        row.QSOUUID,
			Callsign:       row.Callsign,
			Band:           row.Band,
			Mode:           row.Mode,
			DatetimeOn:     row.DatetimeOn,
			ServiceA:       row.ServiceA,
			ServiceB:       row.ServiceB,
			FieldConflicts: row.FieldConflicts,
			Status:         row.Status,
			CreatedAt:      row.CreatedAt,
			UpdatedAt:      row.UpdatedAt,
		})
	}

	totalPages := 0
	if total > 0 {
		totalPages = int((total + int64(pageSize) - 1) / int64(pageSize))
	}

	writeSuccess(w, http.StatusOK, "sync conflicts", map[string]any{
		"items": items,
		"pagination": paginationResponse{
			Page:       page,
			PageSize:   pageSize,
			Total:      total,
			TotalPages: totalPages,
		},
	})
}

type resolveConflictRequest struct {
	Fields map[string]string `json:"fields"`
}

// ResolveConflict accepts field-level conflict resolution picks.
//
// POST /v1/sync/conflicts/{id}/resolve
func (h *SyncHandler) ResolveConflict(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	conflictID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || conflictID <= 0 {
		writeFailure(w, http.StatusBadRequest, "invalid conflict id", "id must be a positive integer")
		return
	}

	var req resolveConflictRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid body", err.Error())
		return
	}
	if len(req.Fields) == 0 {
		writeFailure(w, http.StatusBadRequest, "invalid body", "fields map is required")
		return
	}

	ok, err := syncsvc.ResolveSyncConflict(r.Context(), h.pool, userID, conflictID, req.Fields)
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "resolve failed", err.Error())
		return
	}
	if !ok {
		writeFailure(w, http.StatusNotFound, "not found", "open conflict not found")
		return
	}

	writeSuccess(w, http.StatusOK, "conflict resolved", map[string]any{
		"id":       conflictID,
		"resolved": true,
	})
}

type retryRequest struct {
	QSOUUID string `json:"qso_uuid"`
	Service string `json:"service"`
}

type cancelSyncRequest struct {
	Service string `json:"service"`
}

// Retry retries failed sync rows for a single QSO, a service, or both.
//
// POST /v1/sync/retry
func (h *SyncHandler) Retry(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	var req retryRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid body", err.Error())
		return
	}

	affected, services, err := syncsvc.RetryFailedSyncs(r.Context(), h.pool, userID, req.QSOUUID, req.Service)
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "retry failed", err.Error())
		return
	}

	for _, svc := range services {
		if isUploadService(svc) {
			if err := h.enqueueServiceJobs(r, userID, svc); err != nil {
				slog.WarnContext(r.Context(), "retry enqueue failed", slog.String("service", svc), slog.String("error", err.Error()))
			}
		}
	}

	writeSuccess(w, http.StatusAccepted, "retry enqueued", map[string]any{
		"retried":  affected,
		"services": services,
	})
}

// Cancel marks all pending sync rows for a service as cancelled for the current user.
//
// POST /v1/sync/cancel
func (h *SyncHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	var req cancelSyncRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid body", err.Error())
		return
	}

	service := strings.ToLower(strings.TrimSpace(req.Service))
	if !isUploadService(service) {
		writeFailure(w, http.StatusBadRequest, "invalid service", "service must be one of: eqsl, clublog, qrz, pota")
		return
	}

	cancelled, err := syncsvc.CancelPendingSync(r.Context(), h.pool, userID, service)
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "cancel failed", err.Error())
		return
	}

	writeSuccess(w, http.StatusOK, "sync cancelled", map[string]any{
		"service":   service,
		"cancelled": cancelled,
	})
}

// syncHistoryItemResponse is a single history item in the response.
type syncHistoryItemResponse struct {
	ID           int64      `json:"id"`
	Service      string     `json:"service"`
	Status       string     `json:"status"`
	LastSyncedAt *time.Time `json:"last_synced_at,omitempty"`
	Error        *string    `json:"error,omitempty"`
	ErrorCode    *string    `json:"error_code,omitempty"`
	RetryCount   int16      `json:"retry_count"`
	QSOUuid      string     `json:"qso_uuid"`
	Callsign     string     `json:"callsign"`
	Band         string     `json:"band"`
	Mode         string     `json:"mode"`
	DatetimeOn   time.Time  `json:"datetime_on"`
}

// History returns recent sync activity for the authenticated user.
//
// GET /v1/sync/history?limit=50
func (h *SyncHandler) History(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	limit := int(parsePageSize(r.URL.Query().Get("limit")))
	if limit > 200 {
		limit = 200
	}

	rows, err := syncsvc.GetSyncHistory(r.Context(), h.pool, userID, limit)
	if err != nil {
		slog.ErrorContext(r.Context(), "sync history query failed", slog.String("error", err.Error()))
		writeFailure(w, http.StatusInternalServerError, "sync history unavailable", "query failed")
		return
	}

	items := make([]syncHistoryItemResponse, 0, len(rows))
	for _, row := range rows {
		items = append(items, syncHistoryItemResponse{
			ID:           row.ID,
			Service:      row.Service,
			Status:       row.Status,
			LastSyncedAt: row.LastSyncedAt,
			Error:        row.ErrorMessage,
			ErrorCode:    row.ErrorCode,
			RetryCount:   row.RetryCount,
			QSOUuid:      row.QSOUuid,
			Callsign:     row.Callsign,
			Band:         row.Band,
			Mode:         row.Mode,
			DatetimeOn:   row.DatetimeOn,
		})
	}

	writeSuccess(w, http.StatusOK, "sync history", map[string]any{"items": items})
}

func (h *SyncHandler) enqueueServiceJobs(r *http.Request, userID int64, service string) error {
	service = strings.TrimSpace(strings.ToLower(service))
	switch service {
	case "eqsl", "clublog", "qrz", "pota":
		// supported
	case "lotw":
		return fmt.Errorf("LoTW sync is not yet implemented")
	default:
		return fmt.Errorf("unknown service %q; supported: eqsl, clublog, qrz, pota", service)
	}

	if h.riverClient == nil {
		return fmt.Errorf("background job queue is not configured")
	}

	switch service {
	case "eqsl":
		if err := syncsvc.EnqueueEQSLUpload(r.Context(), h.riverClient, userID); err != nil {
			return fmt.Errorf("could not enqueue eQSL upload: %w", err)
		}
		_ = syncsvc.EnqueueEQSLDownload(r.Context(), h.riverClient, userID)
	case "clublog":
		if err := syncsvc.EnqueueClubLogUpload(r.Context(), h.riverClient, userID); err != nil {
			return fmt.Errorf("could not enqueue Club Log upload: %w", err)
		}
		_ = syncsvc.EnqueueClubLogPoll(r.Context(), h.riverClient, userID)
	case "qrz":
		if err := syncsvc.EnqueueQRZUpload(r.Context(), h.riverClient, userID); err != nil {
			return fmt.Errorf("could not enqueue QRZ upload: %w", err)
		}
		_ = syncsvc.EnqueueQRZPoll(r.Context(), h.riverClient, userID)
	case "pota":
		if err := syncsvc.EnqueuePOTAUpload(r.Context(), h.riverClient, userID); err != nil {
			return fmt.Errorf("could not enqueue POTA upload: %w", err)
		}
	}

	return nil
}

func parsePositiveInt(raw string, fallback int) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}

func parseDateQuery(raw string) *time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		t = t.UTC()
		return &t
	}
	if t, err := time.Parse("2006-01-02", raw); err == nil {
		t = t.UTC()
		return &t
	}
	return nil
}

func isUploadService(service string) bool {
	switch strings.TrimSpace(strings.ToLower(service)) {
	case "eqsl", "clublog", "qrz", "pota":
		return true
	default:
		return false
	}
}
