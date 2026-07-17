// Package config provides environment-based configuration for the RadioLedger API server.
// Configuration is loaded via go-envconfig, which reads from environment variables.
// All required variables must be set; optional variables have sensible defaults.
package config

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sethvargo/go-envconfig"
	"golang.org/x/crypto/hkdf"

	"github.com/FtlC-ian/radioledger/api/internal/crypto"
)

// Config holds all runtime configuration for the API server.
// Values are populated from environment variables at startup.
type Config struct {
	// Server configuration
	Port int `env:"PORT,default=9091"`
	// MetricsPort is the port used to serve the minimal /metrics + /health
	// endpoint when running in --mode=worker. Has no effect in server/all mode
	// (metrics are served on Port with the full API).
	MetricsPort     int           `env:"METRICS_PORT,default=2112"`
	ShutdownTimeout time.Duration `env:"SHUTDOWN_TIMEOUT,default=30s"`
	ReadTimeout     time.Duration `env:"READ_TIMEOUT,default=10s"`
	WriteTimeout    time.Duration `env:"WRITE_TIMEOUT,default=30s"`
	IdleTimeout     time.Duration `env:"IDLE_TIMEOUT,default=120s"`

	// Database configuration.
	// DATABASE_URL is the full PostgreSQL connection string.
	// Example: postgres://radioledger:secret@localhost:5432/radioledger?sslmode=require
	DatabaseURL    string        `env:"DATABASE_URL,required"`
	DBMaxConns     int32         `env:"DB_MAX_CONNS,default=20"`
	DBMinConns     int32         `env:"DB_MIN_CONNS,default=2"`
	DBMaxConnLife  time.Duration `env:"DB_MAX_CONN_LIFETIME,default=1h"`
	DBMaxConnIdle  time.Duration `env:"DB_MAX_CONN_IDLE_TIME,default=30m"`
	DBHealthPeriod time.Duration `env:"DB_HEALTH_CHECK_PERIOD,default=1m"`

	// Auth configuration.
	// AUTH_MODE selects the authentication implementation.
	//   "local"   — local password-based auth with bcrypt + HS256 JWTs; no external
	//               dependency. Allowed only when APP_ENV != "production".
	//               "dev" is a backward-compatible alias for "local".
	//   "zitadel" — validates RS256 JWTs against Zitadel's JWKS endpoint.
	//               Requires ZITADEL_URL to be set.
	AuthMode string `env:"AUTH_MODE,default=zitadel"`

	// Zitadel OIDC / auth configuration.
	// Required when AUTH_MODE=zitadel.
	ZitadelURL      string `env:"ZITADEL_URL"`       // Base URL of your Zitadel instance
	ZitadelClientID string `env:"ZITADEL_CLIENT_ID"` // OAuth2 client ID for this API
	ZitadelKey      string `env:"ZITADEL_KEY"`       // Service account key for introspection

	// CORS configuration.
	// CORSAllowedOrigins is a comma-separated list of allowed origins.
	// Example: https://radioledger.app,https://staging.radioledger.app
	// An empty value means no cross-origin requests are allowed.
	CORSAllowedOrigins string `env:"CORS_ALLOWED_ORIGINS"`

	// Rate limiting
	RateLimitIPRPS   float64 `env:"RATE_LIMIT_IP_RPS,default=20"`   // requests/sec per IP
	RateLimitIPBurst int     `env:"RATE_LIMIT_IP_BURST,default=50"` // burst allowance per IP

	// Registration controls.
	RequireInviteKey        bool          `env:"REQUIRE_INVITE_KEY,default=false"`
	MaxInvitesPerUser       int           `env:"MAX_INVITES_PER_USER,default=5"`
	InviteReplenishEnabled  bool          `env:"INVITE_REPLENISH_ENABLED,default=false"`
	InviteReplenishInterval time.Duration `env:"INVITE_REPLENISH_INTERVAL,default=168h"`

	// TrustedProxies is a comma-separated list of CIDRs for reverse proxies
	// whose X-Forwarded-For / X-Real-IP headers should be trusted.
	// Empty means headers are ignored and only RemoteAddr is used.
	TrustedProxies string `env:"TRUSTED_PROXIES"`

	// MasterKey is used for AES-256-GCM credential encryption.
	// If empty, Load() will try RADIOLEDGER_MASTER_KEY_FILE and finally auto-generate.
	// NEVER log this value.
	MasterKey string `env:"RADIOLEDGER_MASTER_KEY"`

	// DataDir is used for self-hosted runtime data (including auto-generated master key file).
	DataDir string `env:"RADIOLEDGER_DATA_DIR,default=./data"`
	// MasterKeyFile is the filename or absolute path for persisted first-run master key.
	MasterKeyFile string `env:"RADIOLEDGER_MASTER_KEY_FILE,default=.master-key"`

	// Logging
	LogLevel  string `env:"LOG_LEVEL,default=info"`
	LogFormat string `env:"LOG_FORMAT"` // "json" or "text"; empty = auto by env

	// OpenTelemetry tracing configuration.
	OTELExporter    string `env:"OTEL_EXPORTER"`
	OTLPEndpoint    string `env:"OTEL_EXPORTER_OTLP_ENDPOINT"`
	OTELServiceName string `env:"OTEL_SERVICE_NAME,default=radioledger-api"`

	// Env is the application environment name.
	// Valid values: "development", "staging", "production"
	Env string `env:"APP_ENV,default=production"`

	// LoTW integration
	// LoTWVaultURL is the base URL of the lotw-vault microservice.
	// The vault must be on the vault-internal Docker network; the main API
	// is the only service that can reach it.
	LoTWVaultURL string `env:"LOTW_VAULT_URL,default=http://lotw-vault:8081"`

	// ClubLog integration.
	// ClubLogAPIKey is the server-level Club Log application API key used to
	// authenticate upload/delete/poll requests.
	ClubLogAPIKey string `env:"CLUBLOG_API_KEY"`

	// POTA integration.
	// POTAAPIBaseURL overrides the default POTA API base URL (https://api.pota.app).
	POTAAPIBaseURL string `env:"POTA_API_BASE_URL,default=https://api.pota.app"`
	// POTAAuthURL is the endpoint used to exchange username/password for a JWT.
	// Only required when users authenticate to POTA with username/password.
	POTAAuthURL string `env:"POTA_AUTH_URL"`

	// Admin access.
	// AdminEmails is a comma-separated list of email addresses that are granted
	// server-level admin access. Supports two env var names for backward
	// compatibility: RADIOLEDGER_ADMIN_EMAILS (preferred) and ADMIN_EMAILS (fallback).
	//
	// Note: go-envconfig does not support fallback env vars natively; the fallback
	// is handled in Load() after envconfig.Process.
	AdminEmails string `env:"RADIOLEDGER_ADMIN_EMAILS"`

	// Sync infrastructure tunables.
	// These override the defaults in the sync.InfraConfig for each service.
	// Per-service rate limits (requests per second). Zero means use default.
	SyncRateLimitEQSLRPS    int `env:"SYNC_RATE_LIMIT_EQSL_RPS"`
	SyncRateLimitClublogRPS int `env:"SYNC_RATE_LIMIT_CLUBLOG_RPS"`
	SyncRateLimitLotwRPS    int `env:"SYNC_RATE_LIMIT_LOTW_RPS"`
	SyncRateLimitQrzRPS     int `env:"SYNC_RATE_LIMIT_QRZ_RPS"`
	SyncRateLimitSotaRPS    int `env:"SYNC_RATE_LIMIT_SOTA_RPS"`
	SyncRateLimitPotaRPS    int `env:"SYNC_RATE_LIMIT_POTA_RPS"`
	// Per-service max retry counts. Zero means use default.
	SyncMaxRetriesEQSL    int `env:"SYNC_MAX_RETRIES_EQSL"`
	SyncMaxRetriesClublog int `env:"SYNC_MAX_RETRIES_CLUBLOG"`
	SyncMaxRetriesLotw    int `env:"SYNC_MAX_RETRIES_LOTW"`
	SyncMaxRetriesQrz     int `env:"SYNC_MAX_RETRIES_QRZ"`
	SyncMaxRetriesSota    int `env:"SYNC_MAX_RETRIES_SOTA"`
	SyncMaxRetriesPota    int `env:"SYNC_MAX_RETRIES_POTA"`
	// Shared retry and circuit-breaker tunables. Zero/empty means use defaults.
	SyncRetryBaseDelay          time.Duration `env:"SYNC_RETRY_BASE_DELAY"`
	SyncRetryMaxDelay           time.Duration `env:"SYNC_RETRY_MAX_DELAY"`
	SyncRetryJitter             float64       `env:"SYNC_RETRY_JITTER"`
	SyncCircuitFailureThreshold int           `env:"SYNC_CIRCUIT_FAILURE_THRESHOLD"`
	SyncCircuitRecoveryTimeout  time.Duration `env:"SYNC_CIRCUIT_RECOVERY_TIMEOUT"`
	SyncQueueWarnDepth          int           `env:"SYNC_QUEUE_WARN_DEPTH"`
	SyncQueueCriticalDepth      int           `env:"SYNC_QUEUE_CRITICAL_DEPTH"`

	trustedProxyCIDRs []netip.Prefix
}

// Load reads configuration from the environment and returns a populated Config.
// Returns an error if any required variable is missing or a value fails to parse.
func Load(ctx context.Context) (*Config, error) {
	var cfg Config
	if err := envconfig.Process(ctx, &cfg); err != nil {
		return nil, fmt.Errorf("processing environment config: %w", err)
	}
	if err := ensureMasterKey(&cfg); err != nil {
		return nil, err
	}

	trustedCIDRs, err := parseTrustedProxyCIDRs(cfg.TrustedProxies)
	if err != nil {
		return nil, err
	}
	cfg.trustedProxyCIDRs = trustedCIDRs

	// ADMIN_EMAILS is a backward-compatible fallback for RADIOLEDGER_ADMIN_EMAILS.
	// go-envconfig does not support multi-key fallback natively, so we handle it here.
	if cfg.AdminEmails == "" {
		cfg.AdminEmails = strings.TrimSpace(os.Getenv("ADMIN_EMAILS"))
	}

	// Trim the POTA base URL so callers don't have to.
	cfg.POTAAPIBaseURL = strings.TrimRight(strings.TrimSpace(cfg.POTAAPIBaseURL), "/")
	if cfg.POTAAPIBaseURL == "" {
		cfg.POTAAPIBaseURL = "https://api.pota.app"
	}

	return &cfg, nil
}

// IsDevelopment returns true when running in the development environment.
// Use only for developer-convenience features; never to bypass security.
func (c *Config) IsDevelopment() bool {
	return c.Env == "development"
}

// IsLocalAuth returns true when local authentication mode is active.
// Local auth handles registration and login directly with bcrypt password hashing
// and issues HS256 JWT tokens. "dev" is a backward-compatible alias.
// MUST NOT be used in production — the server enforces this at startup.
func (c *Config) IsLocalAuth() bool {
	return c.AuthMode == "local" || c.AuthMode == "dev"
}

// IsDevAuth returns true when the dev/local authentication mode is active.
// Deprecated: use IsLocalAuth instead. Kept for backward compatibility.
func (c *Config) IsDevAuth() bool {
	return c.IsLocalAuth()
}

func (c *Config) EffectiveMaxInvitesPerUser() int {
	if c == nil || c.MaxInvitesPerUser <= 0 {
		return 5
	}
	return c.MaxInvitesPerUser
}

func (c *Config) EffectiveInviteReplenishInterval() time.Duration {
	if c == nil || c.InviteReplenishInterval <= 0 {
		return 168 * time.Hour
	}
	return c.InviteReplenishInterval
}

// LocalJWTSecret returns the HMAC secret used to sign local-mode JWT tokens.
// If MasterKey is set, derives a dedicated JWT secret from it using HKDF-SHA256.
// Falls back to a fixed development secret when MasterKey is empty (tests / first run).
// The returned secret is always 32 bytes long.
func (c *Config) LocalJWTSecret() []byte {
	const devSecret = "radioledger-local-dev-jwt-secret!"
	if c.MasterKey == "" {
		return []byte(devSecret)
	}

	masterKeyBytes, err := base64.StdEncoding.DecodeString(strings.TrimSpace(c.MasterKey))
	if err != nil || len(masterKeyBytes) == 0 {
		masterKeyBytes = []byte(c.MasterKey)
	}

	reader := hkdf.New(sha256.New, masterKeyBytes, nil, []byte("radioledger-jwt-signing-v1"))
	secret := make([]byte, 32)
	if _, err := io.ReadFull(reader, secret); err != nil {
		return []byte(devSecret)
	}
	return secret
}

// TrustedProxyCIDRs returns the parsed trusted proxy CIDRs.
// Callers receive a copy and may modify it safely.
func (c *Config) TrustedProxyCIDRs() []netip.Prefix {
	if len(c.trustedProxyCIDRs) == 0 {
		return nil
	}
	out := make([]netip.Prefix, len(c.trustedProxyCIDRs))
	copy(out, c.trustedProxyCIDRs)
	return out
}

func parseTrustedProxyCIDRs(raw string) ([]netip.Prefix, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}

	parts := strings.Split(raw, ",")
	prefixes := make([]netip.Prefix, 0, len(parts))
	for _, part := range parts {
		cidr := strings.TrimSpace(part)
		if cidr == "" {
			continue
		}
		prefix, err := netip.ParsePrefix(cidr)
		if err != nil {
			return nil, fmt.Errorf("invalid TRUSTED_PROXIES CIDR %q: %w", cidr, err)
		}
		prefixes = append(prefixes, prefix)
	}
	return prefixes, nil
}

func ensureMasterKey(cfg *Config) error {
	if strings.TrimSpace(cfg.MasterKey) != "" {
		return nil
	}

	path := strings.TrimSpace(cfg.MasterKeyFile)
	if path == "" {
		path = ".master-key"
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(strings.TrimSpace(cfg.DataDir), path)
	}

	if b, err := os.ReadFile(path); err == nil {
		if k := strings.TrimSpace(string(b)); k != "" {
			cfg.MasterKey = k
			return nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("reading master key file: %w", err)
	}

	generated, err := crypto.GenerateMasterKey()
	if err != nil {
		return fmt.Errorf("generating RADIOLEDGER_MASTER_KEY: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("creating master key directory: %w", err)
	}
	if err := os.WriteFile(path, []byte(generated+"\n"), 0o600); err != nil {
		return fmt.Errorf("writing master key file: %w", err)
	}
	cfg.MasterKey = generated

	fmt.Fprintf(os.Stderr, "\n[SECURITY][RADIOLEDGER] RADIOLEDGER_MASTER_KEY was not set.\n")
	fmt.Fprintf(os.Stderr, "[SECURITY][RADIOLEDGER] Generated and persisted a new master key at: %s\n", path)
	fmt.Fprintln(os.Stderr, "[SECURITY][RADIOLEDGER] BACK THIS UP NOW. Losing this key means encrypted credentials are unrecoverable.")
	fmt.Fprintln(os.Stderr)

	return nil
}
