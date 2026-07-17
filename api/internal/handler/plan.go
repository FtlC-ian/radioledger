package handler

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/FtlC-ian/radioledger/pkg/plan"
)

// PlanHandler handles the plan status endpoint.
type PlanHandler struct {
	pool     *pgxpool.Pool
	provider plan.Provider
}

// NewPlanHandler creates a PlanHandler with the given pool and provider.
func NewPlanHandler(pool *pgxpool.Pool, provider plan.Provider) *PlanHandler {
	return &PlanHandler{pool: pool, provider: provider}
}

// planUsage holds the current resource usage counts for a user.
type planUsage struct {
	QSOs         int64 `json:"qsos"`
	Logbooks     int   `json:"logbooks"`
	SyncServices int   `json:"sync_services"`
}

// planStatusResponse is the JSON body for GET /api/v1/account/plan.
type planStatusResponse struct {
	Tier       string     `json:"tier"`
	ExpiresAt  *time.Time `json:"expires_at"`
	Usage      planUsage  `json:"usage"`
	Limits     planLimitsResponse `json:"limits"`
	UpgradeURL *string    `json:"upgrade_url"`
}

// planLimitsResponse is the limits portion of the plan status response.
// Uses pointers so that -1 (unlimited) is rendered as null in JSON.
type planLimitsResponse struct {
	MaxQSOs         *int64 `json:"max_qsos"`
	MaxLogbooks     *int   `json:"max_logbooks"`
	MaxSyncServices *int   `json:"max_sync_services"`
}

// Status handles GET /v1/account/plan.
//
// Returns the current user's plan tier, limits, and live usage counts.
// Self-hosted installations return unlimited limits; deployments with a custom
// provider return the limits supplied by that provider.
func (h *PlanHandler) Status(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	// Fetch the user's plan (tier + limits) from the provider.
	p, err := h.provider.GetPlan(r.Context(), userID)
	if err != nil {
		slog.ErrorContext(r.Context(), "plan: failed to get plan",
			slog.Int64("user_id", userID),
			slog.String("error", err.Error()),
		)
		writeFailure(w, http.StatusInternalServerError, "failed to get plan", "plan lookup failed")
		return
	}

	// Fetch live usage counts using the tenant context (RLS enforces per-user scoping).
	usage, err := h.fetchUsage(r, userID)
	if err != nil {
		slog.ErrorContext(r.Context(), "plan: failed to fetch usage counts",
			slog.Int64("user_id", userID),
			slog.String("error", err.Error()),
		)
		writeFailure(w, http.StatusInternalServerError, "failed to get usage", "usage query failed")
		return
	}

	resp := planStatusResponse{
		Tier:      p.Tier,
		ExpiresAt: p.ExpiresAt,
		Usage:     *usage,
		Limits:    limitsToResponse(p.Limits),
	}

	if p.UpgradeURL != "" {
		url := p.UpgradeURL
		resp.UpgradeURL = &url
	}

	writeSuccess(w, http.StatusOK, "plan status retrieved", resp)
}

// fetchUsage queries the current resource usage counts for the given user.
// Runs inside a tenant transaction so RLS filters to the authenticated user.
func (h *PlanHandler) fetchUsage(r *http.Request, userID int64) (*planUsage, error) {
	tx, err := beginTenantTx(r.Context(), h.pool, userID)
	if err != nil {
		return nil, fmt.Errorf("begin tenant tx: %w", err)
	}
	defer func() { _ = tx.Rollback(r.Context()) }()

	var usage planUsage

	// Count QSOs (RLS filters to current user).
	if err := tx.QueryRow(r.Context(),
		`SELECT COUNT(*) FROM qsos WHERE deleted_at IS NULL`,
	).Scan(&usage.QSOs); err != nil {
		return nil, fmt.Errorf("count qsos: %w", err)
	}

	// Count logbooks (RLS filters to current user).
	var logbookCount int64
	if err := tx.QueryRow(r.Context(),
		`SELECT COUNT(*) FROM logbooks WHERE deleted_at IS NULL`,
	).Scan(&logbookCount); err != nil {
		return nil, fmt.Errorf("count logbooks: %w", err)
	}
	usage.Logbooks = int(logbookCount)

	// Count active sync service credentials (RLS filters to current user).
	var syncCount int64
	if err := tx.QueryRow(r.Context(),
		`SELECT COUNT(*) FROM user_service_credentials WHERE is_active = TRUE`,
	).Scan(&syncCount); err != nil {
		return nil, fmt.Errorf("count sync credentials: %w", err)
	}
	usage.SyncServices = int(syncCount)

	if err := tx.Commit(r.Context()); err != nil {
		return nil, fmt.Errorf("commit usage tx: %w", err)
	}

	return &usage, nil
}

// limitsToResponse converts plan.Limits to the HTTP response shape.
// A limit of -1 (unlimited) is rendered as null in JSON.
func limitsToResponse(l plan.Limits) planLimitsResponse {
	resp := planLimitsResponse{}

	if l.MaxQSOs >= 0 {
		v := l.MaxQSOs
		resp.MaxQSOs = &v
	}
	if l.MaxLogbooks >= 0 {
		v := l.MaxLogbooks
		resp.MaxLogbooks = &v
	}
	if l.MaxSyncServices >= 0 {
		v := l.MaxSyncServices
		resp.MaxSyncServices = &v
	}

	return resp
}

