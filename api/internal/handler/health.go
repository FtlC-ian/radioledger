// Package handler contains HTTP request handlers for the RadioLedger API.
// Handlers are intentionally thin: they parse input, call a service/repository,
// and format output. Business logic belongs in the service layer.
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/FtlC-ian/radioledger/api/internal/database/migrations"
	syncsvc "github.com/FtlC-ian/radioledger/api/internal/services/sync"
)

// readinessDB is the database surface needed by /ready.
type readinessDB interface {
	Ping(context.Context) error
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

// HealthHandler holds dependencies for the health and readiness endpoints.
type HealthHandler struct {
	db                       readinessDB
	expectedMigrationVersion int64
}

// NewHealthHandler creates a HealthHandler with the given database pool.
// pool may be nil during early startup; the handler will report unready in that case.
func NewHealthHandler(pool *pgxpool.Pool) *HealthHandler {
	if pool == nil {
		return newHealthHandler(nil, migrations.ExpectedGooseVersion)
	}
	return newHealthHandler(pool, migrations.ExpectedGooseVersion)
}

func newHealthHandler(db readinessDB, expectedMigrationVersion int64) *HealthHandler {
	return &HealthHandler{db: db, expectedMigrationVersion: expectedMigrationVersion}
}

// Health handles GET /health (liveness probe).
// Returns 200 {"status":"ok"} if the process is alive. No database check is performed.
// This endpoint is suitable for load-balancer health checks and Kubernetes liveness probes.
// It is exempt from authentication and rate limiting (configured in the router).
func (h *HealthHandler) Health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Ready handles GET /ready (readiness probe).
// Returns 200 {"status":"ready"} if the database is reachable and current.
// Returns 503 {"status":"unavailable","reason":"..."} if the database is down,
// not yet initialized, or behind the migration version expected by this build.
//
// This endpoint is suitable for Kubernetes readiness probes and deployment health gates.
// Traffic should not be routed to the pod until /ready returns 200.
// It is exempt from authentication and rate limiting (configured in the router).
func (h *HealthHandler) Ready(w http.ResponseWriter, r *http.Request) {
	if h.db == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"status": "unavailable",
			"reason": "database pool not initialized",
		})
		return
	}

	if err := h.db.Ping(r.Context()); err != nil {
		slog.WarnContext(r.Context(), "readiness check: database ping failed",
			slog.String("error", err.Error()),
		)
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"status": "unavailable",
			"reason": "database unreachable",
		})
		return
	}

	currentMigrationVersion, err := currentGooseVersion(r.Context(), h.db)
	if err != nil {
		if h.expectedMigrationVersion == migrations.ConsolidatedLegacyGooseVersion && legacyConsolidatedSchemaPresent(r.Context(), h.db) {
			slog.WarnContext(r.Context(), "readiness check: accepting legacy consolidated schema without goose history",
				slog.String("error", err.Error()),
			)
			currentMigrationVersion = migrations.ConsolidatedLegacyGooseVersion
		} else {
			slog.WarnContext(r.Context(), "readiness check: migration version query failed",
				slog.String("error", err.Error()),
			)
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{
				"status": "unavailable",
				"reason": "migration version unavailable",
			})
			return
		}
	}
	if currentMigrationVersion < h.expectedMigrationVersion {
		slog.WarnContext(r.Context(), "readiness check: migrations pending",
			slog.Int64("current_version", currentMigrationVersion),
			slog.Int64("expected_version", h.expectedMigrationVersion),
		)
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"status": "unavailable",
			"reason": "migrations_pending",
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func legacyConsolidatedSchemaPresent(ctx context.Context, db readinessDB) bool {
	rows, err := db.Query(ctx, `
SELECT to_regclass('public.users') IS NOT NULL
   AND to_regclass('public.qsos') IS NOT NULL
   AND to_regclass('public.logbooks') IS NOT NULL
`)
	if err != nil {
		return false
	}
	defer rows.Close()
	if !rows.Next() {
		return false
	}
	var present bool
	if err := rows.Scan(&present); err != nil {
		return false
	}
	return present && rows.Err() == nil
}

func currentGooseVersion(ctx context.Context, db readinessDB) (int64, error) {
	rows, err := db.Query(ctx, `
SELECT version_id, is_applied
FROM goose_db_version
ORDER BY id DESC
`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	rolledBack := make(map[int64]struct{})
	for rows.Next() {
		var version int64
		var applied bool
		if err := rows.Scan(&version, &applied); err != nil {
			return 0, err
		}
		if _, skip := rolledBack[version]; skip {
			continue
		}
		if applied {
			return version, nil
		}
		rolledBack[version] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, nil
		}
		return 0, err
	}
	return 0, nil
}

// ServiceStatusHandler handles the public service status endpoint.
// It surfaces circuit-breaker and rate-limit state for each sync service so
// the frontend can display banners like "LoTW is under high load" without
// requiring user authentication.
type ServiceStatusHandler struct {
	pool *pgxpool.Pool
}

// NewServiceStatusHandler creates a ServiceStatusHandler backed by pool.
func NewServiceStatusHandler(pool *pgxpool.Pool) *ServiceStatusHandler {
	return &ServiceStatusHandler{pool: pool}
}

// serviceStatusItem is a single service entry in the status response.
type serviceStatusItem struct {
	Service string `json:"service"`
	Status  string `json:"status"`
}

// ServiceStatus handles GET /v1/status/services.
//
// Returns the global circuit-breaker / rate-limit state for every known sync
// service. This endpoint is intentionally unauthenticated so that frontend code
// can poll it without a user session to show system-wide health indicators.
//
// Response shape:
//
//	{
//	  "services": [
//	    {"service":"eqsl",    "status":"ok"},
//	    {"service":"clublog", "status":"ok"},
//	    {"service":"lotw",    "status":"circuit_open"},
//	    {"service":"qrz",    "status":"ok"}
//	  ]
//	}
//
// Possible status values:
//
//	"ok"           — service is reachable and not rate-limited
//	"circuit_open" — circuit breaker has tripped after consecutive failures
//	"rate_limited" — current second's rate budget is exhausted
func (h *ServiceStatusHandler) ServiceStatus(w http.ResponseWriter, r *http.Request) {
	health, err := syncsvc.GetGlobalServiceHealth(r.Context(), h.pool)
	if err != nil {
		slog.WarnContext(r.Context(), "service status query failed, returning empty list",
			slog.String("error", err.Error()),
		)
		writeJSON(w, http.StatusOK, map[string]any{"services": []serviceStatusItem{}})
		return
	}

	items := make([]serviceStatusItem, 0, len(health))
	for _, s := range health {
		items = append(items, serviceStatusItem{Service: s.Service, Status: s.Status})
	}

	writeJSON(w, http.StatusOK, map[string]any{"services": items})
}

// writeJSON marshals v to JSON and writes it to w with the given status code.
// Sets Content-Type: application/json.
// Logs a warning if marshalling fails (should be unreachable with known types).
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("writeJSON: failed to encode response", slog.String("error", err.Error()))
	}
}
