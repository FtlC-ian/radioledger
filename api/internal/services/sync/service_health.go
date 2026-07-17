package sync

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ServiceHealth represents runtime health for a sync service.
type ServiceHealth struct {
	Service string
	Status  string // ok|rate_limited|circuit_open|not_configured
}

// GetGlobalServiceHealth returns the circuit breaker / rate-limit state for every
// known sync service without requiring a user context. It is safe to expose on a
// public, unauthenticated endpoint so the frontend can surface system-wide status
// messages (e.g. "LoTW is under high load") without leaking per-user credential info.
//
// Possible status values:
//
//	"ok"           — service is reachable and not rate-limited
//	"circuit_open" — circuit breaker has tripped after consecutive failures
//	"rate_limited" — current second's rate budget is exhausted
func GetGlobalServiceHealth(ctx context.Context, pool *pgxpool.Pool) ([]ServiceHealth, error) {
	services := []string{"eqsl", "clublog", "lotw", "qrz"}
	infra := infraOrFallback(pool)

	result := make([]ServiceHealth, 0, len(services))
	for _, svc := range services {
		status := "ok"
		if state, err := infra.CircuitState(ctx, svc); err == nil && state == CircuitOpen {
			status = "circuit_open"
		} else if limited, err := infra.IsRateLimitedNow(ctx, svc); err == nil && limited {
			status = "rate_limited"
		}
		result = append(result, ServiceHealth{Service: svc, Status: status})
	}
	return result, nil
}

func GetServiceHealthForUser(ctx context.Context, pool *pgxpool.Pool, userID int64) ([]ServiceHealth, error) {
	services := []string{"eqsl", "clublog", "lotw", "qrz", "pota"}
	enabled := map[string]bool{}

	rows, err := pool.Query(ctx, `
		SELECT service
		FROM user_service_credentials
		WHERE user_id = $1 AND is_active = TRUE
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var svc string
		if err := rows.Scan(&svc); err != nil {
			return nil, err
		}
		enabled[svc] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	infra := infraOrFallback(pool)
	result := make([]ServiceHealth, 0, len(services))
	for _, svc := range services {
		status := "ok"
		if !enabled[svc] {
			status = "not_configured"
		} else {
			if state, err := infra.CircuitState(ctx, svc); err == nil && state == CircuitOpen {
				status = "circuit_open"
			} else if limited, err := infra.IsRateLimitedNow(ctx, svc); err == nil && limited {
				status = "rate_limited"
			}
		}
		result = append(result, ServiceHealth{Service: svc, Status: status})
	}
	return result, nil
}
