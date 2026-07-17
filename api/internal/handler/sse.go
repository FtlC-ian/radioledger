package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	db "github.com/FtlC-ian/radioledger/api/internal/database/sqlc"
	"github.com/FtlC-ian/radioledger/api/internal/middleware"
)

const (
	importSSEPollInterval = 500 * time.Millisecond
	importSSEMaxDuration  = 30 * time.Minute
	streamTokenTTL        = 30 * time.Second
)

// SSEHandler provides Server-Sent Event endpoints.
type SSEHandler struct {
	pool *pgxpool.Pool
}

// NewSSEHandler creates an SSEHandler.
func NewSSEHandler(pool *pgxpool.Pool) *SSEHandler {
	return &SSEHandler{pool: pool}
}

type importProgressEvent struct {
	Processed  int32   `json:"processed"`
	Total      int32   `json:"total"`
	Imported   int32   `json:"imported"`
	Duplicates int32   `json:"duplicates"`
	Errors     int32   `json:"errors"`
	Percent    float64 `json:"percent"`
}

type importCompleteEvent struct {
	Status     string `json:"status"`
	Imported   int32  `json:"imported"`
	Duplicates int32  `json:"duplicates"`
	Errors     int32  `json:"errors"`
}

type importErrorEvent struct {
	Status     string `json:"status"`
	Imported   int32  `json:"imported"`
	Duplicates int32  `json:"duplicates"`
	Errors     int32  `json:"errors"`
	Error      string `json:"error"`
}

type streamTokenResponse struct {
	Token     string `json:"token"`
	ExpiresIn int    `json:"expires_in"`
}

type streamTokenRequest struct {
	Path       string `json:"path"`
	StreamPath string `json:"stream_path"`
}

// CreateStreamToken handles POST /v1/stream-token.
// It exchanges the caller's bearer token for a short-lived, one-time stream token
// so the browser can open EventSource without placing the bearer credential in the URL.
func (h *SSEHandler) CreateStreamToken(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	streamPath, err := streamPathFromTokenRequest(r)
	if err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", err.Error())
		return
	}

	importUUID, err := importUUIDFromStreamPath(streamPath)
	if err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", err.Error())
		return
	}

	if _, _, err := h.loadImportJobForUser(r.Context(), userID, importUUID); errors.Is(err, pgx.ErrNoRows) {
		writeFailure(w, http.StatusNotFound, "not found", "import job not found")
		return
	} else if err != nil {
		writeFailure(w, http.StatusInternalServerError, "query failed", "could not fetch import job")
		return
	}

	token, err := middleware.IssueStreamToken(userID, streamPath, streamTokenTTL)
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to issue stream token", "internal error")
		return
	}

	writeSuccess(w, http.StatusOK, "stream token issued", streamTokenResponse{
		Token:     token,
		ExpiresIn: int(streamTokenTTL / time.Second),
	})
}

// StreamImportProgress handles GET /v1/import/{importUUID}/stream.
//
// It emits:
//   - event: progress every 500ms while the import is pending/processing
//   - event: complete when the import reaches complete status
//   - event: error when the import fails or is cancelled
//
// The stream terminates when the client disconnects or 30 minutes elapse.
func (h *SSEHandler) StreamImportProgress(w http.ResponseWriter, r *http.Request) {
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

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeFailure(w, http.StatusInternalServerError, "stream unsupported", "response writer does not support streaming")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("X-Content-Type-Options", "nosniff")

	ctx, cancel := context.WithTimeout(r.Context(), importSSEMaxDuration)
	defer cancel()

	ticker := time.NewTicker(importSSEPollInterval)
	defer ticker.Stop()

	for {
		row, importErrReason, err := h.loadImportJobForUser(ctx, userID, importUUID)
		if errors.Is(err, pgx.ErrNoRows) {
			_ = writeSSEEvent(w, "error", importErrorEvent{
				Status: "failed",
				Error:  "import job not found",
			})
			flusher.Flush()
			return
		}
		if err != nil {
			_ = writeSSEEvent(w, "error", importErrorEvent{
				Status: "failed",
				Error:  "failed to fetch import status",
			})
			flusher.Flush()
			return
		}

		switch row.Status {
		case "pending", "processing":
			progress := buildImportProgressEvent(row)
			if err := writeSSEEvent(w, "progress", progress); err != nil {
				return
			}
			flusher.Flush()
		case "complete":
			if err := writeSSEEvent(w, "complete", importCompleteEvent{
				Status:     "completed",
				Imported:   row.Imported,
				Duplicates: row.Duplicate,
				Errors:     row.Errors,
			}); err != nil {
				return
			}
			flusher.Flush()
			return
		case "error", "cancelled":
			reason := "import failed"
			if importErrReason != nil && *importErrReason != "" {
				reason = *importErrReason
			}
			if err := writeSSEEvent(w, "error", importErrorEvent{
				Status:     "failed",
				Imported:   row.Imported,
				Duplicates: row.Duplicate,
				Errors:     row.Errors,
				Error:      reason,
			}); err != nil {
				return
			}
			flusher.Flush()
			return
		default:
			if err := writeSSEEvent(w, "error", importErrorEvent{
				Status: "failed",
				Error:  fmt.Sprintf("unexpected import status: %s", row.Status),
			}); err != nil {
				return
			}
			flusher.Flush()
			return
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func streamPathFromTokenRequest(r *http.Request) (string, error) {
	var req streamTokenRequest
	if r.Body != nil {
		defer func() { _ = r.Body.Close() }()
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			return "", errors.New("request body must be valid JSON")
		}
	}

	streamPath := strings.TrimSpace(req.Path)
	if streamPath == "" {
		streamPath = strings.TrimSpace(req.StreamPath)
	}
	if streamPath == "" {
		return "", errors.New("path is required")
	}
	if !strings.HasSuffix(streamPath, "/stream") {
		return "", errors.New("path must target a /stream endpoint")
	}
	return streamPath, nil
}

func importUUIDFromStreamPath(path string) (uuid.UUID, error) {
	path = strings.TrimSpace(path)
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 4 || parts[0] != "v1" || parts[1] != "import" || parts[3] != "stream" {
		return uuid.Nil, errors.New("unsupported stream path")
	}
	importUUID, err := uuid.Parse(parts[2])
	if err != nil {
		return uuid.Nil, errors.New("invalid import job UUID in stream path")
	}
	return importUUID, nil
}

func (h *SSEHandler) loadImportJobForUser(
	ctx context.Context,
	userID int64,
	importUUID uuid.UUID,
) (db.GetImportJobByUUIDRow, *string, error) {
	tx, err := beginTenantTx(ctx, h.pool, userID)
	if err != nil {
		return db.GetImportJobByUUIDRow{}, nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	row, err := queries.GetImportJobByUUID(ctx, importUUID)
	if err != nil {
		return db.GetImportJobByUUIDRow{}, nil, err
	}

	var latestErr *string
	if row.Status == "error" || row.Status == "cancelled" {
		reason, reasonErr := queries.GetLatestImportJobErrorByUUID(ctx, importUUID)
		if reasonErr == nil {
			latestErr = &reason
		} else if !errors.Is(reasonErr, pgx.ErrNoRows) {
			return db.GetImportJobByUUIDRow{}, nil, reasonErr
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return db.GetImportJobByUUIDRow{}, nil, err
	}

	return row, latestErr, nil
}

func buildImportProgressEvent(row db.GetImportJobByUUIDRow) importProgressEvent {
	processed := row.Imported + row.Duplicate + row.Errors + row.Skipped
	total := processed
	if row.TotalRecords != nil && *row.TotalRecords > 0 {
		total = *row.TotalRecords
	}

	percent := 0.0
	if total > 0 {
		percent = float64(processed) / float64(total) * 100
	}
	if percent > 100 {
		percent = 100
	}

	return importProgressEvent{
		Processed:  processed,
		Total:      total,
		Imported:   row.Imported,
		Duplicates: row.Duplicate,
		Errors:     row.Errors,
		Percent:    percent,
	}
}

func writeSSEEvent(w http.ResponseWriter, event string, payload any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "event: %s\n", event); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", encoded); err != nil {
		return err
	}
	return nil
}
