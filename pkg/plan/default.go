package plan

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DefaultProvider implements Provider for self-hosted RadioLedger installations.
//
// Behavior:
//   - GetPlan reads the subscription_tier from the users table and returns the
//     corresponding display limits from TierLimits. The tier column is respected
//     for display purposes (so the plan status endpoint shows meaningful data),
//     but CheckLimit always returns nil — self-hosted users are never restricted.
//   - CheckLimit always returns nil. No resource enforcement occurs.
//
// This is the default behavior shipped in the open-source core. A deployment
// may replace it with its own entitlement provider when needed.
type DefaultProvider struct {
	Pool *pgxpool.Pool
}

// NewDefaultProvider creates a DefaultProvider backed by the given pool.
func NewDefaultProvider(pool *pgxpool.Pool) *DefaultProvider {
	return &DefaultProvider{Pool: pool}
}

// GetPlan reads the user's subscription_tier and returns the static tier limits
// from TierLimits. The limits are for display only — CheckLimit never enforces
// them on self-hosted installations.
func (d *DefaultProvider) GetPlan(ctx context.Context, userID int64) (*Plan, error) {
	if d.Pool == nil {
		return &Plan{Tier: "free", Limits: Unlimited}, nil
	}

	conn, err := d.Pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("plan: acquire conn: %w", err)
	}
	defer conn.Release()

	var tier string
	err = conn.QueryRow(ctx,
		`SELECT subscription_tier FROM users WHERE id = $1 AND deleted_at IS NULL`,
		userID,
	).Scan(&tier)
	if err != nil {
		return nil, fmt.Errorf("plan: query user tier: %w", err)
	}

	return &Plan{
		Tier:   tier,
		Limits: Unlimited, // self-hosted: always unlimited regardless of tier
	}, nil
}

// CheckLimit always returns nil for self-hosted installations.
// No resource caps are enforced by the DefaultProvider.
func (d *DefaultProvider) CheckLimit(_ context.Context, _ int64, _ Resource) error {
	return nil
}
