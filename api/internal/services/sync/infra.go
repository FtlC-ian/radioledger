package sync

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/FtlC-ian/radioledger/api/internal/config"
)

type ServicePolicy struct {
	RateLimitRPS            int
	MaxRetries              int
	CircuitFailureThreshold int
	CircuitRecoveryTimeout  time.Duration
}

type InfraConfig struct {
	Services       map[string]ServicePolicy
	RetryBaseDelay time.Duration
	RetryMaxDelay  time.Duration
	RetryJitter    float64
	QueueWarnDepth int
	QueueCritDepth int
}

func defaultInfraConfig() InfraConfig {
	defaultPolicy := ServicePolicy{
		RateLimitRPS:            1,
		MaxRetries:              8,
		CircuitFailureThreshold: 5,
		CircuitRecoveryTimeout:  60 * time.Second,
	}
	return InfraConfig{
		Services: map[string]ServicePolicy{
			"eqsl":    defaultPolicy,
			"clublog": {RateLimitRPS: 5, MaxRetries: 8, CircuitFailureThreshold: 5, CircuitRecoveryTimeout: 60 * time.Second},
			"lotw":    defaultPolicy,
			"qrz":     {RateLimitRPS: 2, MaxRetries: 8, CircuitFailureThreshold: 5, CircuitRecoveryTimeout: 60 * time.Second},
			"sota":    defaultPolicy,
			"pota":    defaultPolicy,
		},
		RetryBaseDelay: 1 * time.Second,
		RetryMaxDelay:  5 * time.Minute,
		RetryJitter:    0.2,
		QueueWarnDepth: 1000,
		QueueCritDepth: 10000,
	}
}

func loadInfraConfigFromConfig(cfg *config.Config) InfraConfig {
	infraCfg := defaultInfraConfig()
	if cfg == nil {
		return infraCfg
	}

	rateLimits := map[string]int{
		"eqsl":    cfg.SyncRateLimitEQSLRPS,
		"clublog": cfg.SyncRateLimitClublogRPS,
		"lotw":    cfg.SyncRateLimitLotwRPS,
		"qrz":     cfg.SyncRateLimitQrzRPS,
		"sota":    cfg.SyncRateLimitSotaRPS,
		"pota":    cfg.SyncRateLimitPotaRPS,
	}
	for service, limit := range rateLimits {
		if limit > 0 {
			policy := infraCfg.Services[service]
			policy.RateLimitRPS = limit
			infraCfg.Services[service] = policy
		}
	}

	maxRetries := map[string]int{
		"eqsl":    cfg.SyncMaxRetriesEQSL,
		"clublog": cfg.SyncMaxRetriesClublog,
		"lotw":    cfg.SyncMaxRetriesLotw,
		"qrz":     cfg.SyncMaxRetriesQrz,
		"sota":    cfg.SyncMaxRetriesSota,
		"pota":    cfg.SyncMaxRetriesPota,
	}
	for service, retries := range maxRetries {
		if retries > 0 {
			policy := infraCfg.Services[service]
			policy.MaxRetries = retries
			infraCfg.Services[service] = policy
		}
	}

	if cfg.SyncRetryBaseDelay > 0 {
		infraCfg.RetryBaseDelay = cfg.SyncRetryBaseDelay
	}
	if cfg.SyncRetryMaxDelay > 0 {
		infraCfg.RetryMaxDelay = cfg.SyncRetryMaxDelay
	}
	if cfg.SyncRetryJitter > 0 && cfg.SyncRetryJitter <= 1 {
		infraCfg.RetryJitter = cfg.SyncRetryJitter
	}
	if cfg.SyncCircuitFailureThreshold > 0 {
		for service, policy := range infraCfg.Services {
			policy.CircuitFailureThreshold = cfg.SyncCircuitFailureThreshold
			infraCfg.Services[service] = policy
		}
	}
	if cfg.SyncCircuitRecoveryTimeout > 0 {
		for service, policy := range infraCfg.Services {
			policy.CircuitRecoveryTimeout = cfg.SyncCircuitRecoveryTimeout
			infraCfg.Services[service] = policy
		}
	}
	if cfg.SyncQueueWarnDepth > 0 {
		infraCfg.QueueWarnDepth = cfg.SyncQueueWarnDepth
	}
	if cfg.SyncQueueCriticalDepth > 0 {
		infraCfg.QueueCritDepth = cfg.SyncQueueCriticalDepth
	}

	return infraCfg
}

type CircuitState string

const (
	CircuitClosed   CircuitState = "closed"
	CircuitOpen     CircuitState = "open"
	CircuitHalfOpen CircuitState = "half_open"
)

type Infra struct {
	pool *pgxpool.Pool
	cfg  InfraConfig

	randMu sync.Mutex
	rand   *rand.Rand
}

func NewInfra(pool *pgxpool.Pool, cfg InfraConfig) *Infra {
	return &Infra{pool: pool, cfg: cfg, rand: rand.New(rand.NewSource(time.Now().UnixNano()))}
}

func (i *Infra) EnsureTables(ctx context.Context) error {
	if i.pool == nil {
		return nil
	}
	_, err := i.pool.Exec(ctx, `
CREATE TABLE IF NOT EXISTS sync_rate_limit_window (
  service      TEXT NOT NULL,
  bucket_start TIMESTAMPTZ NOT NULL,
  count        INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (service, bucket_start)
);
CREATE TABLE IF NOT EXISTS sync_circuit_state (
  service               TEXT PRIMARY KEY,
  state                 TEXT NOT NULL DEFAULT 'closed',
  consecutive_failures  INTEGER NOT NULL DEFAULT 0,
  opened_at             TIMESTAMPTZ,
  half_open_in_flight   BOOLEAN NOT NULL DEFAULT FALSE,
  updated_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  last_error            TEXT
);
`)
	return err
}

func (i *Infra) policy(service string) ServicePolicy {
	if p, ok := i.cfg.Services[service]; ok {
		return p
	}
	return ServicePolicy{RateLimitRPS: 1, MaxRetries: 8, CircuitFailureThreshold: 5, CircuitRecoveryTimeout: 60 * time.Second}
}

func (i *Infra) ConsumeRateLimit(ctx context.Context, service string) (bool, error) {
	p := i.policy(service)
	tx, err := i.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, "sync_rl_"+service); err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM sync_rate_limit_window WHERE service=$1 AND bucket_start < date_trunc('second', now()) - interval '1 minute'`, service); err != nil {
		return false, err
	}
	var count int
	if err := tx.QueryRow(ctx, `
INSERT INTO sync_rate_limit_window (service, bucket_start, count)
VALUES ($1, date_trunc('second', now()), 1)
ON CONFLICT (service, bucket_start)
DO UPDATE SET count = sync_rate_limit_window.count + 1
RETURNING count
`, service).Scan(&count); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return count <= p.RateLimitRPS, nil
}

func (i *Infra) IsRateLimitedNow(ctx context.Context, service string) (bool, error) {
	p := i.policy(service)
	var count int
	err := i.pool.QueryRow(ctx, `
SELECT COALESCE(count,0)
FROM sync_rate_limit_window
WHERE service=$1 AND bucket_start = date_trunc('second', now())
`, service).Scan(&count)
	if err != nil {
		return false, err
	}
	return count >= p.RateLimitRPS, nil
}

func (i *Infra) AllowCircuit(ctx context.Context, service string) (bool, time.Duration, error) {
	p := i.policy(service)
	tx, err := i.pool.Begin(ctx)
	if err != nil {
		return false, 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, "sync_cb_"+service); err != nil {
		return false, 0, err
	}
	_, _ = tx.Exec(ctx, `INSERT INTO sync_circuit_state (service) VALUES ($1) ON CONFLICT (service) DO NOTHING`, service)

	var state string
	var consecutive int
	var openedAt *time.Time
	var halfOpenInFlight bool
	var updatedAt time.Time
	if err := tx.QueryRow(ctx, `
SELECT state, consecutive_failures, opened_at, half_open_in_flight, updated_at
FROM sync_circuit_state
WHERE service = $1
FOR UPDATE
`, service).Scan(&state, &consecutive, &openedAt, &halfOpenInFlight, &updatedAt); err != nil {
		return false, 0, err
	}
	_ = consecutive

	now := time.Now()
	switch CircuitState(state) {
	case CircuitClosed:
		if err := tx.Commit(ctx); err != nil {
			return false, 0, err
		}
		return true, 0, nil
	case CircuitOpen:
		if openedAt == nil {
			t := now
			openedAt = &t
		}
		if now.Before(openedAt.Add(p.CircuitRecoveryTimeout)) {
			if err := tx.Commit(ctx); err != nil {
				return false, 0, err
			}
			return false, openedAt.Add(p.CircuitRecoveryTimeout).Sub(now), nil
		}
		_, err := tx.Exec(ctx, `
UPDATE sync_circuit_state
SET state='half_open', half_open_in_flight=TRUE, updated_at=NOW()
WHERE service=$1
`, service)
		if err != nil {
			return false, 0, err
		}
		if err := tx.Commit(ctx); err != nil {
			return false, 0, err
		}
		return true, 0, nil
	case CircuitHalfOpen:
		if halfOpenInFlight {
			// If the probe has been in-flight longer than 2× the recovery timeout,
			// assume the probe worker crashed (panic/SIGKILL) before it could call
			// RecordSuccess or RecordFailure. Reset the flag so a new probe is
			// admitted rather than leaving the circuit permanently stuck in half_open.
			staleCutoff := now.Add(-2 * p.CircuitRecoveryTimeout)
			if updatedAt.Before(staleCutoff) {
				halfOpenInFlight = false
			}
		}
		if halfOpenInFlight {
			if err := tx.Commit(ctx); err != nil {
				return false, 0, err
			}
			return false, p.CircuitRecoveryTimeout, nil
		}
		_, err := tx.Exec(ctx, `
UPDATE sync_circuit_state
SET half_open_in_flight=TRUE, updated_at=NOW()
WHERE service=$1
`, service)
		if err != nil {
			return false, 0, err
		}
		if err := tx.Commit(ctx); err != nil {
			return false, 0, err
		}
		return true, 0, nil
	default:
		if err := tx.Commit(ctx); err != nil {
			return false, 0, err
		}
		return true, 0, nil
	}
}

func (i *Infra) RecordSuccess(ctx context.Context, service string) error {
	// Acquire the same advisory lock used by AllowCircuit and RecordFailure so
	// that a concurrent RecordFailure cannot race a RecordSuccess and inadvertently
	// close a circuit that was just tripped.
	tx, err := i.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, "sync_cb_"+service); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
INSERT INTO sync_circuit_state (service, state, consecutive_failures, opened_at, half_open_in_flight, updated_at, last_error)
VALUES ($1, 'closed', 0, NULL, FALSE, NOW(), NULL)
ON CONFLICT (service)
DO UPDATE SET state='closed', consecutive_failures=0, opened_at=NULL, half_open_in_flight=FALSE, updated_at=NOW(), last_error=NULL
`, service)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (i *Infra) RecordFailure(ctx context.Context, service, errMsg string) (bool, error) {
	p := i.policy(service)
	tx, err := i.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, "sync_cb_"+service); err != nil {
		return false, err
	}
	_, _ = tx.Exec(ctx, `INSERT INTO sync_circuit_state (service) VALUES ($1) ON CONFLICT (service) DO NOTHING`, service)

	var state string
	var consecutive int
	if err := tx.QueryRow(ctx, `SELECT state, consecutive_failures FROM sync_circuit_state WHERE service=$1 FOR UPDATE`, service).Scan(&state, &consecutive); err != nil {
		return false, err
	}

	shouldOpen := false
	nextFailures := consecutive + 1
	if CircuitState(state) == CircuitHalfOpen {
		shouldOpen = true
	} else if nextFailures >= p.CircuitFailureThreshold {
		shouldOpen = true
	}

	if shouldOpen {
		_, err = tx.Exec(ctx, `
UPDATE sync_circuit_state
SET state='open', consecutive_failures=$2, opened_at=NOW(), half_open_in_flight=FALSE, updated_at=NOW(), last_error=$3
WHERE service=$1
`, service, nextFailures, errMsg)
	} else {
		_, err = tx.Exec(ctx, `
UPDATE sync_circuit_state
SET state='closed', consecutive_failures=$2, half_open_in_flight=FALSE, updated_at=NOW(), last_error=$3
WHERE service=$1
`, service, nextFailures, errMsg)
	}
	if err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return shouldOpen, nil
}

func (i *Infra) CircuitState(ctx context.Context, service string) (CircuitState, error) {
	var state string
	err := i.pool.QueryRow(ctx, `SELECT COALESCE((SELECT state FROM sync_circuit_state WHERE service=$1), 'closed')`, service).Scan(&state)
	if err != nil {
		return CircuitClosed, err
	}
	return CircuitState(state), nil
}

func (i *Infra) RetryDelay(service string, retryCount int16) (time.Duration, bool) {
	p := i.policy(service)
	attemptNumber := int(retryCount) + 1
	if attemptNumber > p.MaxRetries {
		return 0, false
	}
	base := float64(i.cfg.RetryBaseDelay) * math.Pow(2, float64(retryCount))
	if base > float64(i.cfg.RetryMaxDelay) {
		base = float64(i.cfg.RetryMaxDelay)
	}
	jitterRange := i.cfg.RetryJitter
	factor := 1.0
	if jitterRange > 0 {
		i.randMu.Lock()
		factor = 1.0 + ((i.rand.Float64()*2 - 1) * jitterRange)
		i.randMu.Unlock()
	}
	d := time.Duration(base * factor)
	if d < time.Second {
		d = time.Second
	}
	return d, true
}

func (i *Infra) NextRetryAt(service string, retryCount int16) (time.Time, bool) {
	d, ok := i.RetryDelay(service, retryCount)
	if !ok {
		return time.Time{}, false
	}
	return time.Now().Add(d), true
}

var (
	globalInfraMu sync.RWMutex
	globalInfra   *Infra
)

func InitWorkerInfra(ctx context.Context, pool *pgxpool.Pool, cfg *config.Config) error {
	infra := NewInfra(pool, loadInfraConfigFromConfig(cfg))
	if err := infra.EnsureTables(ctx); err != nil {
		return fmt.Errorf("ensure sync infra tables: %w", err)
	}
	globalInfraMu.Lock()
	globalInfra = infra
	globalInfraMu.Unlock()
	return nil
}

func SetWorkerInfraForTests(infra *Infra) {
	globalInfraMu.Lock()
	defer globalInfraMu.Unlock()
	globalInfra = infra
}

func getInfra() *Infra {
	globalInfraMu.RLock()
	defer globalInfraMu.RUnlock()
	return globalInfra
}
