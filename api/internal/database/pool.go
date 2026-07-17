// Package database provides PostgreSQL connection pool setup for the RadioLedger API.
// All database access goes through the pool; direct connections are not used.
// The pool is configured for the connection limits and health-check policies defined in Config.
package database

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/FtlC-ian/radioledger/api/internal/config"
	"github.com/FtlC-ian/radioledger/api/internal/tracing"
)

// NewPool creates and returns a configured pgxpool.Pool.
// It parses the DATABASE_URL from config, applies pool sizing and health-check
// parameters, and validates connectivity before returning.
//
// The caller is responsible for calling pool.Close() when the server shuts down.
func NewPool(ctx context.Context, cfg *config.Config) (*pgxpool.Pool, error) {
	poolCfg, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("parsing DATABASE_URL: %w", err)
	}

	poolCfg.MaxConns = cfg.DBMaxConns
	poolCfg.MinConns = cfg.DBMinConns
	poolCfg.MaxConnLifetime = cfg.DBMaxConnLife
	poolCfg.MaxConnIdleTime = cfg.DBMaxConnIdle
	poolCfg.HealthCheckPeriod = cfg.DBHealthPeriod

	poolCfg.ConnConfig.Tracer = tracing.NewPGXTracer()

	// AfterConnect hook — good place to set session-level defaults if needed.
	// The tenant middleware uses SET LOCAL (transaction-scoped) for RLS variables,
	// so nothing session-level is required here.

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("creating pgxpool: %w", err)
	}

	// Validate connectivity immediately. Fail fast rather than surfacing errors later.
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("pinging database: %w", err)
	}

	slog.InfoContext(ctx, "database pool established",
		slog.Int("max_conns", int(cfg.DBMaxConns)),
		slog.Int("min_conns", int(cfg.DBMinConns)),
	)

	return pool, nil
}
