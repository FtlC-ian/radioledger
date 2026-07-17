// Package router wires together the chi router, middleware stack, and all HTTP handlers
// for the RadioLedger API server.
package router

import (
	"compress/gzip"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riandyrn/otelchi"
	"github.com/riverqueue/river"

	"github.com/FtlC-ian/radioledger/api/internal/auth"
	"github.com/FtlC-ian/radioledger/api/internal/config"
	"github.com/FtlC-ian/radioledger/api/internal/crypto"
	"github.com/FtlC-ian/radioledger/api/internal/handler"
	"github.com/FtlC-ian/radioledger/api/internal/metrics"
	"github.com/FtlC-ian/radioledger/api/internal/middleware"
	lotwsvc "github.com/FtlC-ian/radioledger/api/internal/services/lotw"
	"github.com/FtlC-ian/radioledger/pkg/plan"
)

// Option is a functional option for configuring the router.
type Option func(*routerConfig)

// routerConfig holds optional router configuration set via functional options.
type routerConfig struct {
	planProvider plan.Provider
}

// WithPlanProvider sets a custom plan.Provider on the router.
// The provider is used by the plan status endpoint and plan enforcement middleware.
//
// If not specified, the router uses plan.DefaultProvider (self-hosted: unlimited).
func WithPlanProvider(p plan.Provider) Option {
	return func(rc *routerConfig) {
		rc.planProvider = p
	}
}

// New creates and returns a fully configured chi router.
//
// Middleware stack (in order, per ARCHITECTURE.md):
//
//	RequestID → OTel → Logger → Recoverer → CORS → Metrics → RateLimitIP → Compress
//	(then per-route group) → Auth → Tenant(RLS) → [handler]
//
// Health endpoints (/health, /ready) are exempt from auth and rate limiting.
// Auth endpoints (/v1/auth/register, /v1/auth/login) are exempt from the Auth middleware.
// All other /v1/* routes require a valid bearer token (JWT or API key).
func New(cfg *config.Config, pool *pgxpool.Pool, riverClient *river.Client[pgx.Tx], opts ...Option) http.Handler {
	return NewWithKeyring(cfg, pool, riverClient, buildKeyring(cfg), opts...)
}

// NewWithKeyring is the constructor used by both production and integration tests.
// Tests pass a pre-built keyring with a known key to test credential encryption.
func NewWithKeyring(cfg *config.Config, pool *pgxpool.Pool, riverClient *river.Client[pgx.Tx], keyring *crypto.Keyring, opts ...Option) http.Handler {
	// Apply functional options.
	rc := &routerConfig{}
	for _, opt := range opts {
		opt(rc)
	}
	// Default to DefaultProvider (self-hosted: unlimited access).
	if rc.planProvider == nil {
		rc.planProvider = plan.NewDefaultProvider(pool)
	}
	r := chi.NewRouter()

	var allowedOrigins []string
	if cfg.CORSAllowedOrigins != "" {
		for _, o := range strings.Split(cfg.CORSAllowedOrigins, ",") {
			if trimmed := strings.TrimSpace(o); trimmed != "" {
				allowedOrigins = append(allowedOrigins, trimmed)
			}
		}
	}

	middleware.SetTrustedProxyCIDRs(cfg.TrustedProxyCIDRs())

	ipLimiter := middleware.NewIPRateLimiter(cfg.RateLimitIPRPS, cfg.RateLimitIPBurst)

	r.Use(middleware.RequestID)
	r.Use(otelchi.Middleware(cfg.OTELServiceName))
	r.Use(middleware.Logger)
	r.Use(chimiddleware.Recoverer)
	r.Use(middleware.SecurityHeaders)
	r.Use(middleware.CORS(allowedOrigins))
	r.Use(metrics.Middleware)
	r.Use(middleware.RateLimitIP(ipLimiter))
	r.Use(chimiddleware.Compress(gzip.DefaultCompression))

	authenticator, localAuth := newAuthenticator(cfg, pool)
	zitadelAuth, _ := authenticator.(*auth.ZitadelAuth)

	healthHandler := handler.NewHealthHandler(pool)
	serviceStatusHandler := handler.NewServiceStatusHandler(pool)
	authHandler := handler.NewAuthHandler(pool, cfg, localAuth, zitadelAuth)
	inviteHandler := handler.NewInviteHandler(pool, cfg)
	logbookHandler := handler.NewLogbookHandler(pool)
	memberHandler := handler.NewMemberHandler(pool)
	qsoHandler := handler.NewQSOHandlerWithSync(pool, riverClient)
	importHandler := handler.NewImportHandler(pool, riverClient, keyring)
	exportHandler := handler.NewExportHandler(pool)
	statsHandler := handler.NewStatsHandler(pool)
	awardsHandler := handler.NewAwardsHandlerWithSync(pool, riverClient)
	potaHandler := handler.NewPOTAHandler(pool)
	sotaHandler := handler.NewSOTAHandler(pool)
	credentialHandler := handler.NewCredentialHandler(pool, keyring)
	apiKeyHandler := handler.NewAPIKeyHandler(pool)
	locationHandler := handler.NewLocationHandler(pool)
	callsignHandler := handler.NewCallsignHandler(pool)
	callsignLookupHandler := handler.NewCallsignLookupHandler(pool, keyring)
	sseHandler := handler.NewSSEHandler(pool)
	contestHandler := handler.NewContestHandler(pool)
	cabrilloHandler := handler.NewCabrilloHandler(pool)
	notificationHandler := handler.NewNotificationHandler(pool)
	preferencesHandler := handler.NewPreferencesHandler(pool)
	bandModeVisibilityHandler := handler.NewBandModeVisibilityHandler(pool)
	spotsHandler := handler.NewSpotsHandler(pool)
	// PSK Reporter disabled — will revisit as desktop-driven or full mirror.
	// pskReporterHandler := handler.NewPSKReporterHandler(pool)
	syncHandler := handler.NewSyncHandler(pool, riverClient, keyring)
	syncCredentialHandler := handler.NewSyncCredentialHandler(pool, keyring)
	desktopHandler := handler.NewDesktopHandler(pool)
	confirmationHandler := handler.NewConfirmationHandler(pool)
	callsignDBHandler := handler.NewCallsignDBHandler(pool)
	adminHandler := handler.NewAdminHandler(pool, riverClient)
	vaultClient := lotwsvc.NewVaultClient(cfg.LoTWVaultURL)
	lotwHandler := handler.NewLoTWHandler(pool, riverClient, vaultClient, keyring)
	eqslHandler := handler.NewEQSLHandler(pool, riverClient, keyring)
	planHandler := handler.NewPlanHandler(pool, rc.planProvider)

	// Public endpoints — no auth required.
	r.Get("/health", healthHandler.Health)
	r.Get("/ready", healthHandler.Ready)
	r.Handle("/metrics", metrics.Handler())

	// Public service status — no auth required; safe for frontend polling.
	// Returns global circuit-breaker state so the UI can surface health banners.
	r.Get("/v1/status/services", serviceStatusHandler.ServiceStatus)

	// Public callsign database endpoints — SEO-critical, no auth required.
	// These are outside the authenticated /v1 group intentionally.
	r.Route("/v1/callsign", func(r chi.Router) {
		r.Get("/search", callsignDBHandler.DBSearch)
		r.Get("/{call}/grid", callsignDBHandler.GridLookup)
		r.Get("/{call}/profile", callsignDBHandler.GetCallsignProfile)
		r.Get("/{call}", callsignDBHandler.DBLookup)

		// Authenticated profile update — requires valid JWT/API key.
		r.With(middleware.Auth(authenticator, pool)).Put("/{call}/profile", callsignDBHandler.UpdateCallsignProfile)
	})

	r.Route("/v1", func(r chi.Router) {
		r.Post("/auth/register", authHandler.Register)
		r.Post("/auth/login", authHandler.Login)
		r.Post("/auth/validate-invite", authHandler.ValidateInvite)
		r.Post("/auth/consume-invite", authHandler.ConsumeInvite)

		r.Group(func(r chi.Router) {
			// Auth middleware supports both JWT tokens and "Bearer rl_..." API keys.
			r.Use(middleware.Auth(authenticator, pool))

			r.Get("/auth/me", authHandler.Me)
			r.Get("/auth/callsign-availability", authHandler.CallsignAvailability)
			r.Patch("/auth/profile", authHandler.UpdateProfile)
			r.Delete("/auth/me", authHandler.DeleteMe)
			r.Post("/auth/change-password", authHandler.ChangePassword)

			r.Route("/invites", func(r chi.Router) {
				r.Post("/", inviteHandler.Create)
				r.Get("/", inviteHandler.List)
				r.Delete("/{id}", inviteHandler.Revoke)
			})

			// Plan status — returns current tier, limits, and live usage counts.
			r.Get("/account/plan", planHandler.Status)

			r.Route("/logbooks", func(r chi.Router) {
				r.With(middleware.RequireAPIKeyScope(middleware.ScopeLogbooksRead)).Get("/", logbookHandler.List)
				r.With(middleware.RequireAPIKeyScope(middleware.ScopeLogbooksWrite), middleware.EnforcePlan(rc.planProvider, plan.ResourceLogbookCreate)).Post("/", logbookHandler.Create)
				r.With(middleware.RequireAPIKeyScope(middleware.ScopeLogbooksRead)).Get("/default", logbookHandler.Default)
				r.With(middleware.RequireAPIKeyScope(middleware.ScopeLogbooksRead), middleware.RequireLogbookPermission(pool, auth.PermissionLogbookRead)).Get("/{logbookUUID}", logbookHandler.Get)
				r.With(middleware.RequireAPIKeyScope(middleware.ScopeLogbooksWrite), middleware.RequireLogbookPermission(pool, auth.PermissionLogbookUpdate)).Put("/{logbookUUID}", logbookHandler.Update)
				r.With(middleware.RequireAPIKeyScope(middleware.ScopeLogbooksWrite), middleware.RequireLogbookPermission(pool, auth.PermissionLogbookDelete)).Delete("/{logbookUUID}", logbookHandler.Delete)

				r.With(middleware.RequireAPIKeyScope(middleware.ScopeLogbooksRead), middleware.RequireLogbookPermission(pool, auth.PermissionLogbookRead)).Get("/{logbookUUID}/members", memberHandler.List)
				r.With(middleware.RequireAPIKeyScope(middleware.ScopeLogbooksWrite), middleware.RequireLogbookPermission(pool, auth.PermissionManageMembers)).Post("/{logbookUUID}/members", memberHandler.Invite)
				r.With(middleware.RequireAPIKeyScope(middleware.ScopeLogbooksWrite), middleware.RequireLogbookPermission(pool, auth.PermissionManageMembers)).Put("/{logbookUUID}/members/{userUUID}", memberHandler.UpdateRole)
				r.With(middleware.RequireAPIKeyScope(middleware.ScopeLogbooksWrite), middleware.RequireLogbookPermission(pool, auth.PermissionManageMembers)).Delete("/{logbookUUID}/members/{userUUID}", memberHandler.Remove)
				r.With(middleware.RequireAPIKeyScope(middleware.ScopeLogbooksWrite), middleware.RequireLogbookPermission(pool, auth.PermissionTransferOwnership)).Post("/{logbookUUID}/transfer-ownership", memberHandler.TransferOwnership)
			})

			r.Route("/logbooks/{logbookUUID}/qsos", func(r chi.Router) {
				r.With(middleware.RequireAPIKeyScope(middleware.ScopeQSOsRead), middleware.RequireLogbookPermission(pool, auth.PermissionQSORead)).Get("/", qsoHandler.List)
				r.With(middleware.RequireAPIKeyScope(middleware.ScopeQSOsWrite), middleware.RequireLogbookPermission(pool, auth.PermissionQSOCreate), middleware.EnforcePlan(rc.planProvider, plan.ResourceQSOCreate)).Post("/", qsoHandler.Create)
				r.With(middleware.RequireAPIKeyScope(middleware.ScopeQSOsRead), middleware.RequireLogbookPermission(pool, auth.PermissionQSORead)).Get("/{qsoUUID}", qsoHandler.Get)
				r.With(middleware.RequireAPIKeyScope(middleware.ScopeQSOsWrite), middleware.RequireLogbookPermission(pool, auth.PermissionQSOUpdate)).Put("/{qsoUUID}", qsoHandler.Update)
				r.With(middleware.RequireAPIKeyScope(middleware.ScopeQSOsWrite), middleware.RequireLogbookPermission(pool, auth.PermissionQSOUpdate)).Patch("/{qsoUUID}", qsoHandler.Patch)
				r.With(middleware.RequireAPIKeyScope(middleware.ScopeQSOsDelete), middleware.RequireLogbookPermission(pool, auth.PermissionQSODelete)).Delete("/{qsoUUID}", qsoHandler.Delete)
			})

			r.With(middleware.RequireAPIKeyScope(middleware.ScopeADIFImport)).Post("/stream-token", sseHandler.CreateStreamToken)

			r.Route("/import", func(r chi.Router) {
				r.With(middleware.RequireAPIKeyScope(middleware.ScopeADIFImport), middleware.EnforcePlan(rc.planProvider, plan.ResourceQSOCreate)).Post("/adif", importHandler.UploadADIF)
				r.With(middleware.RequireAPIKeyScope(middleware.ScopeADIFImport), middleware.EnforcePlan(rc.planProvider, plan.ResourceQSOCreate)).Post("/qrz", importHandler.UploadQRZ)
				r.With(middleware.RequireAPIKeyScope(middleware.ScopeADIFImport)).Post("/enrich", importHandler.EnrichQSOs)
				r.With(middleware.RequireAPIKeyScope(middleware.ScopeADIFImport)).Get("/{importUUID}", importHandler.GetImportStatus)
				r.With(middleware.RequireAPIKeyScope(middleware.ScopeADIFImport)).Get("/{importUUID}/stream", sseHandler.StreamImportProgress)
			})

			r.Route("/export", func(r chi.Router) {
				r.With(middleware.RequireAPIKeyScope(middleware.ScopeADIFExport)).Get("/adif", exportHandler.ADIF)
			})

			r.Route("/stats", func(r chi.Router) {
				// Basic stats are available to all tiers. Advanced-only stats
				// can be gated individually when we differentiate them.
				r.Get("/", statsHandler.Get)
				r.Get("/overview", statsHandler.Overview)
				r.Get("/by-band", statsHandler.ByBand)
				r.Get("/by-mode", statsHandler.ByMode)
				r.Get("/by-period", statsHandler.ByPeriod)
				r.Get("/countries-over-time", statsHandler.CountriesOverTime)
				r.Get("/top-callsigns", statsHandler.TopCallsigns)
				r.Get("/top-countries", statsHandler.TopCountries)
				r.Get("/operating-patterns", statsHandler.OperatingPatterns)
				r.Get("/activity-heatmap", statsHandler.ActivityHeatmap)
			})

			r.Route("/awards", func(r chi.Router) {
				// Unified award progress endpoints (issue #50).
				r.Get("/", awardsHandler.List)
				r.Post("/refresh", awardsHandler.Refresh)

				// Legacy per-type endpoints (retained for backwards compatibility).
				r.Get("/dxcc", awardsHandler.DXCC)
				r.Get("/was", awardsHandler.WAS)
				r.Get("/grids", awardsHandler.Grids)
				r.Get("/pota", awardsHandler.POTA)
				r.Get("/sota", awardsHandler.SOTA)

				// Parameterised type routes: /awards/{type} and /awards/{type}/needs.
				// chi evaluates static routes before wildcard, so /dxcc /was /grids /pota
				// above still match when called by name.
				r.Get("/{type}", awardsHandler.ByType)
				r.Get("/{type}/needs", awardsHandler.Needs)
			})

			r.Route("/activations", func(r chi.Router) {
				r.Route("/pota", func(r chi.Router) {
					r.Post("/", potaHandler.Create)
					r.Get("/", potaHandler.List)
					r.Get("/{activationUUID}", potaHandler.Get)
					r.Put("/{activationUUID}", potaHandler.Update)
					r.Get("/{activationUUID}/status", potaHandler.Status)
					r.Post("/{activationUUID}/export", potaHandler.Export)
				})

				r.Route("/sota", func(r chi.Router) {
					r.Post("/", sotaHandler.Create)
					r.Get("/", sotaHandler.List)
					r.Get("/{activationUUID}", sotaHandler.Get)
					r.Put("/{activationUUID}", sotaHandler.Update)
					r.Get("/{activationUUID}/status", sotaHandler.Status)
				})
			})

			r.Get("/preferences", preferencesHandler.Get)
			r.Put("/preferences", preferencesHandler.Put)
			r.Get("/preferences/", preferencesHandler.Get)
			r.Put("/preferences/", preferencesHandler.Put)
			r.Get("/preferences/band-mode-visibility", bandModeVisibilityHandler.Get)

			r.Route("/notifications", func(r chi.Router) {
				r.Post("/", notificationHandler.Create)
				r.Get("/", notificationHandler.List)
				r.Get("/unread-count", notificationHandler.UnreadCount)
				r.Put("/read-all", notificationHandler.MarkAllRead)
				r.Put("/{notificationUUID}/read", notificationHandler.MarkRead)
				r.Delete("/{notificationUUID}", notificationHandler.Delete)
			})

			// PSK Reporter reception reports — disabled, will revisit as desktop-driven or full mirror.
			// r.Route("/pskreporter", func(r chi.Router) {
			// 	r.Get("/reports", pskReporterHandler.ListReports)
			// 	r.Get("/reports/match/{qso_id}", pskReporterHandler.MatchQSO)
			// })

			r.Route("/spots", func(r chi.Router) {
				r.Get("/", spotsHandler.List)
				r.Get("/needed", spotsHandler.Needed)
				r.Get("/watch-rules", spotsHandler.ListWatchRules)
				r.Post("/watch-rules", spotsHandler.CreateWatchRule)
				r.Put("/watch-rules/{watchRuleUUID}", spotsHandler.UpdateWatchRule)
				r.Delete("/watch-rules/{watchRuleUUID}", spotsHandler.DeleteWatchRule)
				r.Get("/preferences", spotsHandler.GetPreferences)
				r.Put("/preferences", spotsHandler.PutPreferences)
			})

			// Service credential management (AES-256-GCM encrypted at rest).
			// POST stores/updates, GET lists service names only, DELETE removes.
			r.Route("/credentials", func(r chi.Router) {
				r.Post("/", credentialHandler.Store)
				r.Get("/", credentialHandler.List)
				r.Delete("/{service}", credentialHandler.Delete)
			})

			// API key management (show-once key generation pattern).
			// POST creates, GET lists prefix+metadata, DELETE revokes.
			r.Route("/api-keys", func(r chi.Router) {
				r.With(middleware.EnforcePlan(rc.planProvider, plan.ResourceAPIAccess)).Post("/", apiKeyHandler.Generate)
				r.Get("/", apiKeyHandler.List)
				r.Delete("/{uuid}", apiKeyHandler.Revoke)
			})

			// Station location CRUD (required for LoTW tQSL integration).
			r.Route("/locations", func(r chi.Router) {
				r.Get("/", locationHandler.List)
				r.Post("/", locationHandler.Create)
				r.Get("/{locationUUID}", locationHandler.Get)
				r.Put("/{locationUUID}", locationHandler.Update)
				r.Delete("/{locationUUID}", locationHandler.Delete)
			})

			// User callsign management (personal callsign history).
			r.Route("/callsigns", func(r chi.Router) {
				r.Get("/", callsignHandler.ListCallsigns)
				r.Post("/", callsignHandler.CreateCallsign)
				r.Put("/{callsignUUID}", callsignHandler.UpdateCallsign)
				r.Delete("/{callsignUUID}", callsignHandler.DeleteCallsign)
			})

			// Station callsign management (M:N operator/callsign model for club/contest use).
			r.Route("/station-callsigns", func(r chi.Router) {
				r.Get("/", callsignHandler.ListStationCallsigns)
				r.Post("/", callsignHandler.CreateStationCallsign)
				r.Put("/{stationCallsignUUID}", callsignHandler.UpdateStationCallsign)
				r.Delete("/{stationCallsignUUID}", callsignHandler.DeleteStationCallsign)
			})

			// Callsign lookup (cache-first, falls back to QRZ XML API when credentials configured).
			// GET /v1/lookup/{callsign}        — full callsign info
			// GET /v1/callsigns/autocomplete   — prefix search for QSO form type-ahead
			r.Get("/lookup/{callsign}", callsignLookupHandler.Lookup)
			r.Get("/callsigns/autocomplete", callsignLookupHandler.Autocomplete)

			// Sync status and control endpoints.
			r.Route("/sync", func(r chi.Router) {
				r.With(middleware.RequireAPIKeyScope(middleware.ScopeSyncStatus)).Get("/status", syncHandler.Status)
				r.With(middleware.RequireAPIKeyScope(middleware.ScopeSyncStatus)).Get("/services", syncHandler.Services)
				r.With(middleware.RequireAPIKeyScope(middleware.ScopeSyncTrigger)).Post("/trigger/{service}", syncHandler.TriggerSync)
				r.With(middleware.RequireAPIKeyScope(middleware.ScopeSyncTrigger)).Post("/bulk-upload", syncHandler.BulkUpload)
				r.With(middleware.RequireAPIKeyScope(middleware.ScopeSyncTrigger)).Post("/verify-credentials", syncHandler.VerifyCredentials)
				r.With(middleware.RequireAPIKeyScope(middleware.ScopeSyncStatus)).Get("/conflicts", syncHandler.Conflicts)
				r.With(middleware.RequireAPIKeyScope(middleware.ScopeSyncTrigger)).Post("/conflicts/{id}/resolve", syncHandler.ResolveConflict)
				r.With(middleware.RequireAPIKeyScope(middleware.ScopeSyncTrigger)).Post("/retry", syncHandler.Retry)
				r.With(middleware.RequireAPIKeyScope(middleware.ScopeSyncTrigger)).Post("/cancel", syncHandler.Cancel)
				r.With(middleware.RequireAPIKeyScope(middleware.ScopeSyncStatus)).Get("/history", syncHandler.History)

				// Credential management with verification-on-save.
				// PUT stores+verifies, GET lists metadata only (no secrets),
				// POST /verify re-tests existing creds, DELETE removes.
				r.Route("/credentials", func(r chi.Router) {
					r.With(middleware.EnforcePlan(rc.planProvider, plan.ResourceSyncService)).Put("/{service}", syncCredentialHandler.StoreAndVerify)
					r.Get("/", syncCredentialHandler.List)
					r.Post("/{service}/verify", syncCredentialHandler.ReVerify)
					r.Delete("/{service}", syncCredentialHandler.Delete)
				})
			})

			// Server admin endpoints.
			r.Route("/admin", func(r chi.Router) {
				r.Use(middleware.RequireAdmin(cfg.AdminEmails))
				r.Get("/jobs", adminHandler.ListJobs)
				r.Post("/jobs/{id}/retry", adminHandler.RetryJob)
				r.Post("/sync/trigger", adminHandler.TriggerSync)
				r.Get("/sync/overview", adminHandler.SyncOverview)
			})

			// Contest session management and logging.
			r.Route("/contests", func(r chi.Router) {
				r.Post("/", contestHandler.Create)
				r.Get("/", contestHandler.List)
				r.Get("/{contestUUID}", contestHandler.Get)
				r.Put("/{contestUUID}", contestHandler.Update)
				r.Post("/{contestUUID}/qso", contestHandler.LogQSO)
				r.Get("/{contestUUID}/check-dupe", contestHandler.CheckDupe)
				r.Get("/{contestUUID}/stats", contestHandler.Stats)
				r.Get("/{contestUUID}/export/cabrillo", cabrilloHandler.ExportCabrillo)
				r.Get("/{contestUUID}/export/adif", cabrilloHandler.ExportADIF)
			})

			// QSO confirmation endpoints.
			r.Route("/confirmations", func(r chi.Router) {
				r.Get("/", confirmationHandler.List)
				r.Get("/pending", confirmationHandler.Pending)
				r.Get("/stats", confirmationHandler.Stats)
				r.Post("/{id}/confirm", confirmationHandler.Confirm)
				r.Post("/{id}/reject", confirmationHandler.Reject)
			})

			// Desktop client push endpoints.
			// The desktop sends only metadata here; private keys never leave the operator's machine.
			r.Route("/desktop", func(r chi.Router) {
				r.Post("/cert-expiry", desktopHandler.CertExpiry)
			})

			// eQSL.cc confirmation pull endpoints.
			// Trigger manual inbox pull and check pull status/results.
			r.Route("/eqsl", func(r chi.Router) {
				r.Post("/sync/pull", eqslHandler.PullConfirmations)
				r.Get("/sync/status", eqslHandler.SyncStatus)
			})

			// LoTW (Logbook of the World) integration.
			// Certificate management and async sync via the lotw-vault microservice.
			r.Route("/lotw", func(r chi.Router) {
				// Certificate management.
				r.Post("/cert", lotwHandler.ImportCert)
				r.Get("/cert", lotwHandler.GetCertInfo)
				r.Post("/cert/delete", lotwHandler.DeleteCert)
				r.Post("/cert/rotate-password", lotwHandler.RotatePassword)

				// Sync operations.
				r.Post("/sync", lotwHandler.TriggerSync)
				r.Post("/pull-confirmations", lotwHandler.PullConfirmations)
				r.Get("/sync/status", lotwHandler.SyncStatus)
				r.Get("/sync/pending", lotwHandler.SyncPending)
				r.Get("/sync/history", lotwHandler.SyncHistory)

				// User preferences and aggregate state.
				r.Get("/settings", lotwHandler.GetSettings)
				r.Put("/settings", lotwHandler.UpdateSettings)
			})
		})
	})

	return r
}

// newAuthenticator constructs the appropriate Authenticator based on config.
// Returns both the Authenticator (used by middleware) and, for local mode,
// the *auth.LocalAuth instance (used by the auth handler to issue tokens).
func newAuthenticator(cfg *config.Config, pool *pgxpool.Pool) (auth.Authenticator, *auth.LocalAuth) {
	if cfg.IsLocalAuth() {
		la := &auth.LocalAuth{
			Secret: cfg.LocalJWTSecret(),
			Pool:   pool,
		}
		return la, la
	}

	return &auth.ZitadelAuth{
		ZitadelURL:       cfg.ZitadelURL,
		ClientID:         cfg.ZitadelClientID,
		Pool:             pool,
		RequireInviteKey: cfg.RequireInviteKey,
	}, nil
}

// buildKeyring constructs a Keyring from the configured master key.
// Returns nil if RADIOLEDGER_MASTER_KEY is not set; credential endpoints
// will return 503 in that case (expected only in misconfigured deployments).
func buildKeyring(cfg *config.Config) *crypto.Keyring {
	if cfg.MasterKey == "" {
		return nil
	}
	kr, err := crypto.NewKeyringFromBase64(cfg.MasterKey)
	if err != nil {
		return nil
	}
	return kr
}
