// Package handler — eqsl.go: HTTP handlers for eQSL.cc confirmation pulling.
//
// These endpoints let users manually trigger an eQSL inbox pull and check the
// status of their last pull. The actual download and matching happens
// asynchronously in EQSLConfirmationPullWorker.
//
// Endpoints:
//
//	POST /v1/eqsl/sync/pull    — trigger manual inbox pull for authenticated user
//	GET  /v1/eqsl/sync/status  — get last pull status / results
package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/FtlC-ian/radioledger/api/internal/auth"
	"github.com/FtlC-ian/radioledger/api/internal/crypto"
	"github.com/FtlC-ian/radioledger/api/internal/services/eqsl"
	syncsvc "github.com/FtlC-ian/radioledger/api/internal/services/sync"
)

// EQSLHandler handles eQSL confirmation pull REST endpoints.
type EQSLHandler struct {
	pool        *pgxpool.Pool
	riverClient *river.Client[pgx.Tx]
	keyring     *crypto.Keyring
}

// NewEQSLHandler creates an EQSLHandler.
func NewEQSLHandler(pool *pgxpool.Pool, riverClient *river.Client[pgx.Tx], keyring *crypto.Keyring) *EQSLHandler {
	return &EQSLHandler{
		pool:        pool,
		riverClient: riverClient,
		keyring:     keyring,
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// POST /v1/eqsl/sync/pull
// ──────────────────────────────────────────────────────────────────────────────

type eqslPullResponse struct {
	JobID int64 `json:"job_id"`
}

// PullConfirmations handles POST /v1/eqsl/sync/pull.
// Enqueues an EQSLConfirmationPullWorker job for the authenticated user.
// The pull is incremental: it uses the last stored pull date as RcvdSince.
func (h *EQSLHandler) PullConfirmations(w http.ResponseWriter, r *http.Request) {
	info, ok := auth.UserInfoFromContext(r.Context())
	if !ok {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", "no authenticated user")
		return
	}

	if h.riverClient == nil {
		writeFailure(w, http.StatusServiceUnavailable, "background jobs not available", "river client is not configured")
		return
	}

	// Verify eQSL credentials exist before enqueuing.
	if h.keyring != nil {
		ciphertext, keyVersion, credErr := h.loadRawCredential(r, info.UserID)
		if credErr != nil {
			writeFailure(w, http.StatusInternalServerError, "pull failed", credErr.Error())
			return
		}
		if len(ciphertext) == 0 {
			writeFailure(w, http.StatusBadRequest, "pull failed", "no eQSL credentials configured — add them in Settings first")
			return
		}
		// Attempt to decrypt and validate credentials.
		plaintext, decryptErr := h.keyring.Decrypt(info.UserID, keyVersion, ciphertext)
		if decryptErr != nil {
			writeFailure(w, http.StatusInternalServerError, "pull failed", "could not decrypt eQSL credentials")
			return
		}
		if _, credsErr := eqsl.DecodeCredentials(plaintext); credsErr != nil {
			writeFailure(w, http.StatusBadRequest, "pull failed", "invalid eQSL credentials — re-save in Settings")
			return
		}
	}

	// Load last pull timestamp for incremental pull.
	lastPullAt, err := h.fetchLastPullAt(r, info.UserID)
	if err != nil {
		slog.ErrorContext(r.Context(), "failed to load eqsl last pull date", slog.String("error", err.Error()))
		// Non-fatal: proceed without since-date (full pull).
		lastPullAt = nil
	}

	jobID, err := syncsvc.EnqueueEQSLConfirmationPull(r.Context(), h.riverClient, info.UserID, lastPullAt, 0)
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "pull failed", "could not enqueue job")
		return
	}

	writeSuccess(w, http.StatusAccepted, "eQSL confirmation pull queued", eqslPullResponse{JobID: jobID})
}

// ──────────────────────────────────────────────────────────────────────────────
// GET /v1/eqsl/sync/status
// ──────────────────────────────────────────────────────────────────────────────

type eqslSyncStatusResponse struct {
	LastPullAt         *time.Time `json:"last_pull_at,omitempty"`
	HasCredentials     bool       `json:"has_credentials"`
	TotalConfirmed     int64      `json:"total_confirmed"`
	TotalAGConfirmed   int64      `json:"total_ag_confirmed"`
}

// SyncStatus handles GET /v1/eqsl/sync/status.
// Returns the user's eQSL pull state including last pull time and confirmation counts.
func (h *EQSLHandler) SyncStatus(w http.ResponseWriter, r *http.Request) {
	info, ok := auth.UserInfoFromContext(r.Context())
	if !ok {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", "no authenticated user")
		return
	}

	var resp eqslSyncStatusResponse

	// Last pull timestamp from eqsl_sync_status.
	lastPullAt, err := h.fetchLastPullAt(r, info.UserID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		writeFailure(w, http.StatusInternalServerError, "status unavailable", err.Error())
		return
	}
	resp.LastPullAt = lastPullAt

	// Check whether eQSL credentials are configured.
	if h.keyring != nil {
		ciphertext, _, credErr := h.loadRawCredential(r, info.UserID)
		resp.HasCredentials = credErr == nil && len(ciphertext) > 0
	}

	// Count confirmed and AG-confirmed eQSLs for this user.
	err = h.pool.QueryRow(r.Context(), `
		SELECT
			COUNT(*) FILTER (WHERE qc.eqsl_confirmed = TRUE),
			COUNT(*) FILTER (WHERE qc.eqsl_confirmed = TRUE AND qc.eqsl_ag = TRUE)
		FROM qso_confirmations qc
		JOIN qsos q ON q.id = qc.qso_id
		JOIN logbooks lb ON lb.id = q.logbook_id
		WHERE lb.user_id = $1
		  AND q.deleted_at IS NULL
	`, info.UserID).Scan(&resp.TotalConfirmed, &resp.TotalAGConfirmed)
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "status unavailable", err.Error())
		return
	}

	writeSuccess(w, http.StatusOK, "eqsl sync status", resp)
}

// ──────────────────────────────────────────────────────────────────────────────
// Internal helpers
// ──────────────────────────────────────────────────────────────────────────────

// fetchLastPullAt loads the last_pull_at timestamp from eqsl_sync_status.
// Returns (nil, nil) when no row exists.
func (h *EQSLHandler) fetchLastPullAt(r *http.Request, userID int64) (*time.Time, error) {
	var ts pgtype.Timestamptz
	err := h.pool.QueryRow(r.Context(), `SELECT last_pull_at FROM eqsl_sync_status WHERE user_id = $1`, userID).Scan(&ts)
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

// loadRawCredential fetches the encrypted credential bytes and key version for service='eqsl'.
// Returns (nil, 0, nil) when no credential row exists.
func (h *EQSLHandler) loadRawCredential(r *http.Request, userID int64) ([]byte, int32, error) {
	var ciphertext []byte
	var keyVersion int32
	err := h.pool.QueryRow(r.Context(), `
		SELECT credentials, key_version
		FROM user_service_credentials
		WHERE user_id = $1 AND service = 'eqsl' AND is_active = TRUE
	`, userID).Scan(&ciphertext, &keyVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	return ciphertext, keyVersion, nil
}
