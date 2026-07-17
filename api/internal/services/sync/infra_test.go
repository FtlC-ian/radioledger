package sync

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/FtlC-ian/radioledger/api/internal/config"
)

func setupInfraTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()

	var (
		ctr *postgres.PostgresContainer
		err error
	)
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Skipf("docker/testcontainers unavailable: %v", r)
			}
		}()
		ctr, err = postgres.Run(ctx,
			"postgres:16-alpine",
			postgres.WithDatabase("radioledger"),
			postgres.WithUsername("postgres"),
			postgres.WithPassword("postgres"),
			postgres.BasicWaitStrategies(),
		)
	}()
	if err != nil {
		t.Skipf("start postgres container: %v", err)
	}
	t.Cleanup(func() { _ = ctr.Terminate(ctx) })

	dsn, err := ctr.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("postgres dsn: %v", err)
	}

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pgxpool: %v", err)
	}
	t.Cleanup(pool.Close)

	return pool
}

func TestLoadInfraConfigFromConfigUsesStartupSnapshot(t *testing.T) {
	cfg := &config.Config{
		SyncRateLimitEQSLRPS:        7,
		SyncMaxRetriesEQSL:          11,
		SyncRetryBaseDelay:          3 * time.Second,
		SyncRetryMaxDelay:           9 * time.Minute,
		SyncRetryJitter:             0.4,
		SyncCircuitFailureThreshold: 9,
		SyncCircuitRecoveryTimeout:  2 * time.Minute,
		SyncQueueWarnDepth:          123,
		SyncQueueCriticalDepth:      456,
	}

	infraCfg := loadInfraConfigFromConfig(cfg)
	policy := infraCfg.Services["eqsl"]
	if policy.RateLimitRPS != 7 {
		t.Fatalf("eqsl rate limit = %d, want 7", policy.RateLimitRPS)
	}
	if policy.MaxRetries != 11 {
		t.Fatalf("eqsl max retries = %d, want 11", policy.MaxRetries)
	}
	if policy.CircuitFailureThreshold != 9 {
		t.Fatalf("circuit failure threshold = %d, want 9", policy.CircuitFailureThreshold)
	}
	if policy.CircuitRecoveryTimeout != 2*time.Minute {
		t.Fatalf("circuit recovery timeout = %v, want 2m", policy.CircuitRecoveryTimeout)
	}
	if infraCfg.RetryBaseDelay != 3*time.Second {
		t.Fatalf("retry base delay = %v, want 3s", infraCfg.RetryBaseDelay)
	}
	if infraCfg.RetryMaxDelay != 9*time.Minute {
		t.Fatalf("retry max delay = %v, want 9m", infraCfg.RetryMaxDelay)
	}
	if infraCfg.RetryJitter != 0.4 {
		t.Fatalf("retry jitter = %v, want 0.4", infraCfg.RetryJitter)
	}
	if infraCfg.QueueWarnDepth != 123 {
		t.Fatalf("queue warn depth = %d, want 123", infraCfg.QueueWarnDepth)
	}
	if infraCfg.QueueCritDepth != 456 {
		t.Fatalf("queue crit depth = %d, want 456", infraCfg.QueueCritDepth)
	}
}

func TestInitWorkerInfraUsesInjectedConfig(t *testing.T) {
	prev := getInfra()
	t.Cleanup(func() {
		SetWorkerInfraForTests(prev)
	})

	cfg := &config.Config{
		SyncRateLimitEQSLRPS:        6,
		SyncMaxRetriesEQSL:          10,
		SyncCircuitFailureThreshold: 8,
		SyncCircuitRecoveryTimeout:  90 * time.Second,
		SyncRetryBaseDelay:          2 * time.Second,
		SyncRetryMaxDelay:           7 * time.Minute,
		SyncRetryJitter:             0.3,
	}
	if err := InitWorkerInfra(context.Background(), nil, cfg); err != nil {
		t.Fatalf("InitWorkerInfra: %v", err)
	}

	infra := getInfra()
	if infra == nil {
		t.Fatal("expected global infra to be initialized")
	}

	policy := infra.policy("eqsl")
	if policy.RateLimitRPS != 6 {
		t.Fatalf("global eqsl rate limit = %d, want 6", policy.RateLimitRPS)
	}
	if policy.MaxRetries != 10 {
		t.Fatalf("global eqsl max retries = %d, want 10", policy.MaxRetries)
	}
	if policy.CircuitFailureThreshold != 8 {
		t.Fatalf("global circuit threshold = %d, want 8", policy.CircuitFailureThreshold)
	}
	if policy.CircuitRecoveryTimeout != 90*time.Second {
		t.Fatalf("global circuit recovery timeout = %v, want 90s", policy.CircuitRecoveryTimeout)
	}
	if infra.cfg.RetryBaseDelay != 2*time.Second {
		t.Fatalf("global retry base delay = %v, want 2s", infra.cfg.RetryBaseDelay)
	}
	if infra.cfg.RetryMaxDelay != 7*time.Minute {
		t.Fatalf("global retry max delay = %v, want 7m", infra.cfg.RetryMaxDelay)
	}
	if infra.cfg.RetryJitter != 0.3 {
		t.Fatalf("global retry jitter = %v, want 0.3", infra.cfg.RetryJitter)
	}
}

func TestRateLimiterSharedAcrossInfraInstances(t *testing.T) {
	pool := setupInfraTestDB(t)
	cfg := defaultInfraConfig()
	cfg.Services["eqsl"] = ServicePolicy{RateLimitRPS: 1, MaxRetries: 8, CircuitFailureThreshold: 5, CircuitRecoveryTimeout: time.Minute}

	infraA := NewInfra(pool, cfg)
	infraB := NewInfra(pool, cfg)
	if err := infraA.EnsureTables(context.Background()); err != nil {
		t.Fatalf("ensure tables: %v", err)
	}

	ok, err := infraA.ConsumeRateLimit(context.Background(), "eqsl")
	if err != nil || !ok {
		t.Fatalf("first consume: ok=%v err=%v", ok, err)
	}

	ok, err = infraB.ConsumeRateLimit(context.Background(), "eqsl")
	if err != nil {
		t.Fatalf("second consume err: %v", err)
	}
	if ok {
		t.Fatalf("expected second consume to be rate limited")
	}
}

func TestCircuitBreakerHalfOpenFlow(t *testing.T) {
	pool := setupInfraTestDB(t)
	cfg := defaultInfraConfig()
	cfg.Services["eqsl"] = ServicePolicy{RateLimitRPS: 1, MaxRetries: 8, CircuitFailureThreshold: 2, CircuitRecoveryTimeout: 1 * time.Second}
	infra := NewInfra(pool, cfg)
	if err := infra.EnsureTables(context.Background()); err != nil {
		t.Fatalf("ensure tables: %v", err)
	}

	if ok, _, err := infra.AllowCircuit(context.Background(), "eqsl"); err != nil || !ok {
		t.Fatalf("initial allow: ok=%v err=%v", ok, err)
	}
	if tripped, err := infra.RecordFailure(context.Background(), "eqsl", "fail1"); err != nil || tripped {
		t.Fatalf("first failure: tripped=%v err=%v", tripped, err)
	}
	if tripped, err := infra.RecordFailure(context.Background(), "eqsl", "fail2"); err != nil || !tripped {
		t.Fatalf("second failure: tripped=%v err=%v", tripped, err)
	}

	if ok, _, err := infra.AllowCircuit(context.Background(), "eqsl"); err != nil {
		t.Fatalf("allow while open err: %v", err)
	} else if ok {
		t.Fatalf("expected open circuit to block")
	}

	time.Sleep(1200 * time.Millisecond)
	if ok, _, err := infra.AllowCircuit(context.Background(), "eqsl"); err != nil || !ok {
		t.Fatalf("half-open probe allow: ok=%v err=%v", ok, err)
	}
	if ok, _, err := infra.AllowCircuit(context.Background(), "eqsl"); err != nil {
		t.Fatalf("second half-open allow err: %v", err)
	} else if ok {
		t.Fatalf("expected half-open to allow only one in-flight probe")
	}

	if err := infra.RecordSuccess(context.Background(), "eqsl"); err != nil {
		t.Fatalf("record success: %v", err)
	}
	if ok, _, err := infra.AllowCircuit(context.Background(), "eqsl"); err != nil || !ok {
		t.Fatalf("allow after close: ok=%v err=%v", ok, err)
	}
}

func TestRetryBackoffWithJitterAndMaxRetries(t *testing.T) {
	cfg := defaultInfraConfig()
	cfg.RetryBaseDelay = 1 * time.Second
	cfg.RetryMaxDelay = 5 * time.Minute
	cfg.RetryJitter = 0.2
	cfg.Services["eqsl"] = ServicePolicy{RateLimitRPS: 1, MaxRetries: 2, CircuitFailureThreshold: 5, CircuitRecoveryTimeout: time.Minute}

	infra := NewInfra(nil, cfg)

	d1, ok := infra.RetryDelay("eqsl", 0)
	if !ok || d1 < 800*time.Millisecond || d1 > 1200*time.Millisecond {
		t.Fatalf("retry1 out of jitter range: %v ok=%v", d1, ok)
	}
	d2, ok := infra.RetryDelay("eqsl", 1)
	if !ok || d2 < 1600*time.Millisecond || d2 > 2400*time.Millisecond {
		t.Fatalf("retry2 out of jitter range: %v ok=%v", d2, ok)
	}
	if _, ok := infra.RetryDelay("eqsl", 2); ok {
		t.Fatalf("expected retries exceeded at retryCount=2 when max retries is 2")
	}
}

// TestHalfOpenStaleProbeSelfHeals verifies that a half_open probe stuck with
// half_open_in_flight=TRUE (e.g. from a crashed worker) is automatically reset
// by AllowCircuit once the stale timeout (2× recovery) elapses.
func TestHalfOpenStaleProbeSelfHeals(t *testing.T) {
	pool := setupInfraTestDB(t)
	cfg := defaultInfraConfig()
	// Use a very short recovery timeout so we can fake staleness with a direct UPDATE.
	cfg.Services["eqsl"] = ServicePolicy{
		RateLimitRPS:            1,
		MaxRetries:              8,
		CircuitFailureThreshold: 2,
		CircuitRecoveryTimeout:  1 * time.Second,
	}
	infra := NewInfra(pool, cfg)
	if err := infra.EnsureTables(context.Background()); err != nil {
		t.Fatalf("ensure tables: %v", err)
	}

	// Trip the circuit.
	for i := 0; i < 2; i++ {
		if _, err := infra.RecordFailure(context.Background(), "eqsl", "fail"); err != nil {
			t.Fatalf("record failure %d: %v", i, err)
		}
	}

	// Wait for the recovery window to elapse and let the first AllowCircuit
	// transition it to half_open with half_open_in_flight=TRUE.
	time.Sleep(1200 * time.Millisecond)
	if ok, _, err := infra.AllowCircuit(context.Background(), "eqsl"); err != nil || !ok {
		t.Fatalf("expected probe allowed into half_open: ok=%v err=%v", ok, err)
	}

	// Simulate a crashed probe: back-date updated_at beyond the stale threshold
	// (2 × 1s = 2s → use 3s ago to be safe).
	_, err := pool.Exec(context.Background(), `
		UPDATE sync_circuit_state
		SET updated_at = NOW() - INTERVAL '3 seconds'
		WHERE service = 'eqsl'
	`)
	if err != nil {
		t.Fatalf("back-date updated_at: %v", err)
	}

	// AllowCircuit should detect the stale flag and admit a new probe.
	ok, _, err := infra.AllowCircuit(context.Background(), "eqsl")
	if err != nil {
		t.Fatalf("stale probe check err: %v", err)
	}
	if !ok {
		t.Fatal("expected AllowCircuit to reset stale half_open_in_flight and admit new probe")
	}
}

// TestRateLimiterServiceIsolation ensures that consuming the rate limit for one
// service does not affect a different service's budget.
func TestRateLimiterServiceIsolation(t *testing.T) {
	pool := setupInfraTestDB(t)
	cfg := defaultInfraConfig()
	cfg.Services["eqsl"] = ServicePolicy{RateLimitRPS: 1, MaxRetries: 8, CircuitFailureThreshold: 5, CircuitRecoveryTimeout: time.Minute}
	cfg.Services["clublog"] = ServicePolicy{RateLimitRPS: 5, MaxRetries: 8, CircuitFailureThreshold: 5, CircuitRecoveryTimeout: time.Minute}

	infra := NewInfra(pool, cfg)
	if err := infra.EnsureTables(context.Background()); err != nil {
		t.Fatalf("ensure tables: %v", err)
	}

	// Exhaust eqsl's 1 RPS budget.
	ok, err := infra.ConsumeRateLimit(context.Background(), "eqsl")
	if err != nil || !ok {
		t.Fatalf("first eqsl consume: ok=%v err=%v", ok, err)
	}
	ok, err = infra.ConsumeRateLimit(context.Background(), "eqsl")
	if err != nil {
		t.Fatalf("second eqsl consume err: %v", err)
	}
	if ok {
		t.Fatal("eqsl should be rate limited after 1 request/s")
	}

	// clublog should still have budget remaining.
	ok, err = infra.ConsumeRateLimit(context.Background(), "clublog")
	if err != nil {
		t.Fatalf("clublog consume err: %v", err)
	}
	if !ok {
		t.Fatal("clublog should NOT be rate limited (independent budget)")
	}
}

// TestRecordSuccessAfterFailureDoesNotReopenCircuit verifies that a RecordSuccess
// following a RecordFailure (which tripped the circuit) does not reopen a circuit
// that a concurrent failure trip had legitimately closed to 'open'.
func TestRecordSuccessResetsCircuitToClosedFromOpen(t *testing.T) {
	pool := setupInfraTestDB(t)
	cfg := defaultInfraConfig()
	cfg.Services["eqsl"] = ServicePolicy{RateLimitRPS: 1, MaxRetries: 8, CircuitFailureThreshold: 2, CircuitRecoveryTimeout: time.Minute}
	infra := NewInfra(pool, cfg)
	if err := infra.EnsureTables(context.Background()); err != nil {
		t.Fatalf("ensure tables: %v", err)
	}

	// Trip the circuit.
	for i := 0; i < 2; i++ {
		if _, err := infra.RecordFailure(context.Background(), "eqsl", "fail"); err != nil {
			t.Fatalf("trip failure %d: %v", i, err)
		}
	}
	state, err := infra.CircuitState(context.Background(), "eqsl")
	if err != nil || state != CircuitOpen {
		t.Fatalf("expected open circuit, got %q err=%v", state, err)
	}

	// RecordSuccess should close the circuit regardless (probe succeeded).
	if err := infra.RecordSuccess(context.Background(), "eqsl"); err != nil {
		t.Fatalf("record success: %v", err)
	}
	state, err = infra.CircuitState(context.Background(), "eqsl")
	if err != nil || state != CircuitClosed {
		t.Fatalf("expected closed circuit after success, got %q err=%v", state, err)
	}
}
