// Command server is the entry point for the RadioLedger API server.
package appserver

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"

	"github.com/FtlC-ian/radioledger/api/internal/auth"
	"github.com/FtlC-ian/radioledger/api/internal/config"
	"github.com/FtlC-ian/radioledger/api/internal/crypto"
	"github.com/FtlC-ian/radioledger/api/internal/database"
	"github.com/FtlC-ian/radioledger/api/internal/handler"
	"github.com/FtlC-ian/radioledger/api/internal/jobs"
	"github.com/FtlC-ian/radioledger/api/internal/logging"
	"github.com/FtlC-ian/radioledger/api/internal/metrics"
	"github.com/FtlC-ian/radioledger/api/internal/middleware"
	"github.com/FtlC-ian/radioledger/api/internal/router"
	confirmsvc "github.com/FtlC-ian/radioledger/api/internal/services/confirmation"
	lotwsvc "github.com/FtlC-ian/radioledger/api/internal/services/lotw"
	syncsvc "github.com/FtlC-ian/radioledger/api/internal/services/sync"
	"github.com/FtlC-ian/radioledger/api/internal/tracing"
	"github.com/FtlC-ian/radioledger/pkg/plan"
)

// Hooks allows callers (like corp builds) to inject plan/billing behavior
// without duplicating the core server wiring.
type Hooks struct {
	// PlanProvider overrides the default self-hosted plan provider.
	PlanProvider plan.Provider

	// PlanProviderFactory can construct a provider after DB initialization.
	// If provided, it takes precedence over PlanProvider.
	PlanProviderFactory func(ctx context.Context, pool *pgxpool.Pool) (plan.Provider, error)

	// PreServe runs after core DB/river migrations and before server/router startup.
	PreServe func(ctx context.Context, pool *pgxpool.Pool) error

	// ComposeHandler wraps/replaces the core router handler.
	ComposeHandler func(core http.Handler, deps HandlerDeps) (http.Handler, error)
}

// HandlerDeps exposes auth/database helpers for composed handlers.
type HandlerDeps struct {
	Pool            *pgxpool.Pool
	RequireAuth     func(next http.Handler) http.Handler
	UserFromRequest func(r *http.Request) (int64, string, error)
}

// Main is the default entrypoint for the open-source core binary.
func Main() {
	if err := Run(Hooks{}); err != nil {
		slog.Error("server exited with error", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

// Run starts the core RadioLedger server with optional hook injection.
// This function reads like a table of contents — each step is delegated to
// a focused helper so the flow is visible at a glance.
func Run(hooks Hooks) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	mode := flag.String("mode", "server", "run mode: server|worker|all")
	flag.Parse()

	// ── Config & Observability ──────────────────────────────────────────────
	cfg, err := config.Load(ctx)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	logging.Setup(logging.Config{
		Env:    cfg.Env,
		Level:  cfg.LogLevel,
		Format: cfg.LogFormat,
		Out:    os.Stdout,
	})
	metrics.Init()

	tp, err := tracing.Init(ctx, tracing.Config{
		ServiceName:  cfg.OTELServiceName,
		Environment:  cfg.Env,
		Exporter:     cfg.OTELExporter,
		OTLPEndpoint: cfg.OTLPEndpoint,
	})
	if err != nil {
		return fmt.Errorf("initializing tracing: %w", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := tp.Shutdown(shutdownCtx); err != nil {
			slog.Error("failed to shut down tracer provider", slog.String("error", err.Error()))
		}
	}()

	// ── Security Guards ─────────────────────────────────────────────────────
	// Refuse to start with local auth in production. This is the single enforcement
	// point; comments elsewhere in the codebase reference this check.
	if cfg.IsLocalAuth() && cfg.Env == "production" {
		return fmt.Errorf("FATAL: AUTH_MODE=local (or dev) is not allowed when APP_ENV=production. " +
			"Set AUTH_MODE=zitadel and configure ZITADEL_URL for production deployments")
	}

	slog.Info("RadioLedger API server starting",
		slog.String("env", cfg.Env),
		slog.Int("port", cfg.Port),
	)

	// ── Database ────────────────────────────────────────────────────────────
	pool, err := database.NewPool(ctx, cfg)
	if err != nil {
		return fmt.Errorf("connecting to database: %w", err)
	}
	defer func() {
		slog.Info("closing database pool")
		pool.Close()
	}()

	// Auto-run River migrations on every startup (idempotent — safe to call always).
	if err := runRiverMigrations(ctx, pool); err != nil {
		return fmt.Errorf("running river migrations: %w", err)
	}

	// ── Hooks ───────────────────────────────────────────────────────────────
	resolvedPlanProvider := hooks.PlanProvider
	if hooks.PreServe != nil {
		if err := hooks.PreServe(ctx, pool); err != nil {
			return fmt.Errorf("pre-serve hook: %w", err)
		}
	}
	if hooks.PlanProviderFactory != nil {
		provider, err := hooks.PlanProviderFactory(ctx, pool)
		if err != nil {
			return fmt.Errorf("plan provider factory: %w", err)
		}
		resolvedPlanProvider = provider
	}

	// ── Infrastructure ──────────────────────────────────────────────────────
	metrics.StartRiverQueueDepthCollector(ctx, pool, 15*time.Second)
	metrics.StartDBPoolMetricsCollector(ctx, pool, 15*time.Second)
	if err := syncsvc.InitWorkerInfra(ctx, pool, cfg); err != nil {
		return fmt.Errorf("initializing sync worker infra: %w", err)
	}

	// ── Keyring ─────────────────────────────────────────────────────────────
	var keyring *crypto.Keyring
	if cfg.MasterKey != "" {
		kr, krErr := crypto.NewKeyringFromBase64(cfg.MasterKey)
		if krErr != nil {
			return fmt.Errorf("FATAL: RADIOLEDGER_MASTER_KEY is invalid: %w", krErr)
		}
		keyring = kr
	}

	// ── Workers & Periodic Jobs ─────────────────────────────────────────────
	vaultClient := lotwsvc.NewVaultClient(cfg.LoTWVaultURL)
	workers := registerWorkers(cfg, pool, keyring, vaultClient)

	periodicJobs, err := buildPeriodicJobs(cfg, *mode)
	if err != nil {
		return fmt.Errorf("building periodic jobs: %w", err)
	}

	riverClient, err := handler.NewRiverClientForPool(pool, workers, periodicJobs...)
	if err != nil {
		return fmt.Errorf("creating river client: %w", err)
	}

	// cascadeWorker and autoSyncWorker are registered after the client is created
	// so that RiverClient is passed as a constructor argument and the structs are
	// fully initialised before river.AddWorker is called.
	river.AddWorker(workers, &confirmsvc.CascadeConfirmationWorker{Pool: pool, RiverClient: riverClient})
	river.AddWorker(workers, &jobs.AutoSyncSchedulerWorker{Pool: pool, RiverClient: riverClient})

	if err := riverClient.Start(ctx); err != nil {
		return fmt.Errorf("starting river client: %w", err)
	}

	// ── Bootstrap Syncs ─────────────────────────────────────────────────────
	go scheduleBootstrapSyncs(ctx, pool, riverClient)

	defer func() {
		slog.Info("stopping river job queue")
		stopCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		if err := riverClient.Stop(stopCtx); err != nil {
			slog.Error("river stop error", slog.String("error", err.Error()))
		}
	}()

	// ── Worker-only mode ────────────────────────────────────────────────────
	if *mode == "worker" {
		return runWorkerMode(ctx, cfg, pool)
	}

	// ── HTTP Server ─────────────────────────────────────────────────────────
	h, err := buildHandler(cfg, pool, riverClient, resolvedPlanProvider, hooks)
	if err != nil {
		return fmt.Errorf("compose handler hook: %w", err)
	}
	srv, serverErr := startHTTPServer(cfg, h)

	if *mode == "all" || *mode == "server" {
		select {
		case <-ctx.Done():
			slog.Info("shutdown signal received, draining connections",
				slog.Duration("timeout", cfg.ShutdownTimeout),
			)
		case err := <-serverErr:
			return err
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}

	slog.Info("server shut down cleanly")
	return nil
}

// ── Private helpers ────────────────────────────────────────────────────────

func runWorkerMode(ctx context.Context, cfg *config.Config, pool *pgxpool.Pool) error {
	slog.Info("running in worker mode", slog.Int("metrics_port", cfg.MetricsPort))

	// Worker-only mode: expose a minimal HTTP server for Prometheus /metrics
	// and liveness probes. The full API is NOT served here.
	healthHandler := handler.NewHealthHandler(pool)
	metricsMux := http.NewServeMux()
	metricsMux.Handle("/metrics", metrics.Handler())
	metricsMux.HandleFunc("/health", healthHandler.Health)
	metricsMux.HandleFunc("/ready", healthHandler.Ready)
	metricsSrv := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.MetricsPort),
		Handler:      metricsMux,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
		IdleTimeout:  cfg.IdleTimeout,
	}
	go func() {
		slog.Info("worker metrics server listening", slog.String("addr", metricsSrv.Addr))
		if err := metricsSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("worker metrics server error", slog.String("error", err.Error()))
		}
	}()

	<-ctx.Done()
	slog.Info("worker shutdown signal received, draining", slog.Duration("timeout", cfg.ShutdownTimeout))
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := metricsSrv.Shutdown(shutdownCtx); err != nil {
		slog.Error("worker metrics server graceful shutdown error", slog.String("error", err.Error()))
	}
	return nil
}

func buildHandler(
	cfg *config.Config,
	pool *pgxpool.Pool,
	riverClient *river.Client[pgx.Tx],
	planProvider plan.Provider,
	hooks Hooks,
) (http.Handler, error) {
	routerOpts := make([]router.Option, 0, 1)
	if planProvider != nil {
		routerOpts = append(routerOpts, router.WithPlanProvider(planProvider))
	}
	h := router.New(cfg, pool, riverClient, routerOpts...)

	if hooks.ComposeHandler == nil {
		return h, nil
	}

	authenticator, _ := newAuthenticator(cfg, pool)
	deps := HandlerDeps{
		Pool: pool,
		RequireAuth: func(next http.Handler) http.Handler {
			return middleware.Auth(authenticator, pool)(next)
		},
		UserFromRequest: func(r *http.Request) (int64, string, error) {
			info, ok := auth.UserInfoFromContext(r.Context())
			if !ok || info.UserID <= 0 {
				return 0, "", fmt.Errorf("authentication required")
			}
			return info.UserID, info.Email, nil
		},
	}
	composed, composeErr := hooks.ComposeHandler(h, deps)
	if composeErr != nil {
		return nil, composeErr
	}
	return composed, nil
}

// newAuthenticator mirrors the router auth strategy so composed handlers can
// share the same auth middleware and request context user info.
func newAuthenticator(cfg *config.Config, pool *pgxpool.Pool) (auth.Authenticator, *auth.LocalAuth) {
	if cfg.IsLocalAuth() {
		la := &auth.LocalAuth{
			Secret: cfg.LocalJWTSecret(),
			Pool:   pool,
		}
		return la, la
	}

	return &auth.ZitadelAuth{
		ZitadelURL: cfg.ZitadelURL,
		ClientID:   cfg.ZitadelClientID,
		Pool:       pool,
	}, nil
}

// runRiverMigrations applies any pending River queue schema migrations and
// grants the necessary privileges on River tables/sequences to the application
// roles. This mirrors the logic in cmd/migrate-river/main.go and is safe to
// call on every startup because River migrations are idempotent.
func runRiverMigrations(ctx context.Context, pool *pgxpool.Pool) error {
	migrator, err := rivermigrate.New(riverpgxv5.New(pool), nil)
	if err != nil {
		return fmt.Errorf("creating river migrator: %w", err)
	}

	res, err := migrator.Migrate(ctx, rivermigrate.DirectionUp, nil)
	if err != nil {
		return fmt.Errorf("applying river migrations: %w", err)
	}

	for _, v := range res.Versions {
		slog.Info("applied River migration", slog.Int("version", v.Version))
	}
	if len(res.Versions) == 0 {
		slog.Debug("River migrations already up to date")
	}

	return applyRiverRoleGrants(ctx, pool)
}

// applyRiverRoleGrants grants SELECT/INSERT/UPDATE/DELETE on River tables and
// USAGE/SELECT on River sequences to the radioledger_api role, and SELECT to
// radioledger_worker. This allows enqueueing jobs from within SET LOCAL ROLE
// radioledger_api transactions. The grants are idempotent.
func applyRiverRoleGrants(ctx context.Context, pool *pgxpool.Pool) error {
	const grantSQL = `
DO $$
DECLARE
	has_api_role BOOLEAN := EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'radioledger_api');
	has_worker_role BOOLEAN := EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'radioledger_worker');
	t RECORD;
	s RECORD;
BEGIN
	FOR t IN
		SELECT schemaname, tablename
		FROM pg_tables
		WHERE schemaname = 'public' AND tablename LIKE 'river\_%' ESCAPE '\'
	LOOP
		IF has_api_role THEN
			EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE %I.%I TO radioledger_api', t.schemaname, t.tablename);
		END IF;
		IF has_worker_role THEN
			EXECUTE format('GRANT SELECT ON TABLE %I.%I TO radioledger_worker', t.schemaname, t.tablename);
		END IF;
	END LOOP;

	FOR s IN
		SELECT sequence_schema, sequence_name
		FROM information_schema.sequences
		WHERE sequence_schema = 'public' AND sequence_name LIKE 'river\_%' ESCAPE '\'
	LOOP
		IF has_api_role THEN
			EXECUTE format('GRANT USAGE, SELECT ON SEQUENCE %I.%I TO radioledger_api', s.sequence_schema, s.sequence_name);
		END IF;
		IF has_worker_role THEN
			EXECUTE format('GRANT USAGE, SELECT ON SEQUENCE %I.%I TO radioledger_worker', s.sequence_schema, s.sequence_name);
		END IF;
	END LOOP;
END
$$;
`

	if _, err := pool.Exec(ctx, grantSQL); err != nil {
		return fmt.Errorf("apply river grants: %w", err)
	}

	slog.Debug("applied River grants for radioledger_api/radioledger_worker roles")
	return nil
}

// Ensure river.Client[pgx.Tx] type is recognized by the compiler.
var _ *river.Client[pgx.Tx]
