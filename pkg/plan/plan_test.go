package plan_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/FtlC-ian/radioledger/pkg/plan"
)

// ── DefaultProvider ──────────────────────────────────────────────────────────

// TestDefaultProvider_CheckLimit_AlwaysNil verifies that DefaultProvider never
// blocks any resource operation, regardless of the resource type.
func TestDefaultProvider_CheckLimit_AlwaysNil(t *testing.T) {
	p := plan.NewDefaultProvider(nil) // nil pool: CheckLimit doesn't use it

	resources := []plan.Resource{
		plan.ResourceQSOCreate,
		plan.ResourceLogbookCreate,
		plan.ResourceSyncService,
		plan.ResourceAPIAccess,
		plan.ResourceMobileAccess,
		plan.ResourceDesktopCapture,
		plan.ResourceContestLog,
		plan.ResourceAdvancedStats,
		plan.ResourceAwardsTracking,
	}

	for _, r := range resources {
		if err := p.CheckLimit(context.Background(), 42, r); err != nil {
			t.Errorf("DefaultProvider.CheckLimit resource %d: expected nil, got %v", r, err)
		}
	}
}

// TestDefaultProvider_GetPlan_NilPool verifies that GetPlan returns a safe
// default plan when no pool is configured (useful in tests / minimal setups).
func TestDefaultProvider_GetPlan_NilPool(t *testing.T) {
	p := plan.NewDefaultProvider(nil)

	pl, err := p.GetPlan(context.Background(), 42)
	if err != nil {
		t.Fatalf("GetPlan with nil pool: unexpected error: %v", err)
	}
	if pl == nil {
		t.Fatal("GetPlan with nil pool: expected non-nil Plan")
	}
	if pl.Tier != "free" {
		t.Errorf("GetPlan with nil pool: expected tier 'free', got %q", pl.Tier)
	}
	// Self-hosted should return unlimited limits.
	if pl.Limits.MaxQSOs != -1 {
		t.Errorf("GetPlan with nil pool: expected MaxQSOs=-1 (unlimited), got %d", pl.Limits.MaxQSOs)
	}
	if pl.Limits.MaxLogbooks != -1 {
		t.Errorf("GetPlan with nil pool: expected MaxLogbooks=-1 (unlimited), got %d", pl.Limits.MaxLogbooks)
	}
	if pl.Limits.MaxSyncServices != -1 {
		t.Errorf("GetPlan with nil pool: expected MaxSyncServices=-1 (unlimited), got %d", pl.Limits.MaxSyncServices)
	}
}

// ── TierLimits ───────────────────────────────────────────────────────────────

// TestTierLimits_FreeTierHasLimits verifies the free tier enforces caps.
func TestTierLimits_FreeTierHasLimits(t *testing.T) {
	l := plan.LimitsForTier("free")

	if l.MaxQSOs <= 0 && l.MaxQSOs != -1 {
		t.Errorf("free tier MaxQSOs should be positive or -1, got %d", l.MaxQSOs)
	}
	if l.MaxLogbooks <= 0 && l.MaxLogbooks != -1 {
		t.Errorf("free tier MaxLogbooks should be positive or -1, got %d", l.MaxLogbooks)
	}
	if l.MaxSyncServices <= 0 && l.MaxSyncServices != -1 {
		t.Errorf("free tier MaxSyncServices should be positive or -1, got %d", l.MaxSyncServices)
	}
}

// TestTierLimits_ClubTierUnlimited verifies the club tier has unlimited resources.
func TestTierLimits_ClubTierUnlimited(t *testing.T) {
	l := plan.LimitsForTier("club")

	if l.MaxQSOs != -1 {
		t.Errorf("club tier MaxQSOs: expected -1 (unlimited), got %d", l.MaxQSOs)
	}
	if l.MaxLogbooks != -1 {
		t.Errorf("club tier MaxLogbooks: expected -1 (unlimited), got %d", l.MaxLogbooks)
	}
	if l.MaxSyncServices != -1 {
		t.Errorf("club tier MaxSyncServices: expected -1 (unlimited), got %d", l.MaxSyncServices)
	}
	if !l.AdvancedStats {
		t.Error("club tier: expected AdvancedStats=true")
	}
	if !l.AwardsTracking {
		t.Error("club tier: expected AwardsTracking=true")
	}
}

// TestTierLimits_PremiumExceedsStandard verifies tier limits are monotonically
// increasing (premium allows at least as much as standard).
func TestTierLimits_PremiumExceedsStandard(t *testing.T) {
	standard := plan.LimitsForTier("standard")
	premium := plan.LimitsForTier("premium")

	// MaxQSOs: premium should be greater OR unlimited (-1).
	if premium.MaxQSOs != -1 && premium.MaxQSOs < standard.MaxQSOs {
		t.Errorf("premium MaxQSOs (%d) < standard MaxQSOs (%d)", premium.MaxQSOs, standard.MaxQSOs)
	}
	// MaxLogbooks: same.
	if premium.MaxLogbooks != -1 && premium.MaxLogbooks < standard.MaxLogbooks {
		t.Errorf("premium MaxLogbooks (%d) < standard MaxLogbooks (%d)", premium.MaxLogbooks, standard.MaxLogbooks)
	}
}

// TestTierLimits_UnknownTierFallsBackToUnlimited verifies that unrecognised tier
// names (e.g., from a custom self-hosted deployment) return unlimited.
func TestTierLimits_UnknownTierFallsBackToUnlimited(t *testing.T) {
	l := plan.LimitsForTier("enterprise-custom-tier")

	if l.MaxQSOs != -1 {
		t.Errorf("unknown tier MaxQSOs: expected -1 (unlimited), got %d", l.MaxQSOs)
	}
}

// TestTierLimits_AllKnownTiersPresent verifies that all expected tier names
// are defined in TierLimits.
func TestTierLimits_AllKnownTiersPresent(t *testing.T) {
	tiers := []string{"free", "standard", "premium", "club"}
	for _, tier := range tiers {
		if _, ok := plan.TierLimits[tier]; !ok {
			t.Errorf("expected tier %q in TierLimits map", tier)
		}
	}
}

// ── PlanLimitError ───────────────────────────────────────────────────────────

// TestPlanLimitError_Error formats correctly.
func TestPlanLimitError_Error(t *testing.T) {
	err := &plan.PlanLimitError{
		Resource: plan.ResourceQSOCreate,
		Tier:     "free",
		Limit:    500,
		Current:  500,
	}

	msg := err.Error()
	if msg == "" {
		t.Error("PlanLimitError.Error() returned empty string")
	}
}

// TestPlanLimitError_CustomMessage uses the Message field.
func TestPlanLimitError_CustomMessage(t *testing.T) {
	want := "you've hit the QSO limit for your plan"
	err := &plan.PlanLimitError{
		Resource: plan.ResourceQSOCreate,
		Tier:     "free",
		Limit:    500,
		Current:  500,
		Message:  want,
	}

	if err.Error() != want {
		t.Errorf("PlanLimitError.Error(): expected %q, got %q", want, err.Error())
	}
}

// TestIsPlanLimitError identifies plan limit errors correctly.
func TestIsPlanLimitError(t *testing.T) {
	limitErr := &plan.PlanLimitError{Tier: "free", Limit: 500, Current: 500}

	if !plan.IsPlanLimitError(limitErr) {
		t.Error("IsPlanLimitError(*PlanLimitError): expected true")
	}
	if !plan.IsPlanLimitError(fmt.Errorf("wrapped: %w", limitErr)) {
		t.Error("IsPlanLimitError(wrapped PlanLimitError): expected true")
	}
	if plan.IsPlanLimitError(nil) {
		t.Error("IsPlanLimitError(nil): expected false")
	}
	if plan.IsPlanLimitError(context.DeadlineExceeded) {
		t.Error("IsPlanLimitError(DeadlineExceeded): expected false")
	}
}

// ── MockProvider ─────────────────────────────────────────────────────────────

// mockProvider is a test double for plan.Provider.
type mockProvider struct {
	checkLimitFn func(ctx context.Context, userID int64, resource plan.Resource) error
	getPlanFn    func(ctx context.Context, userID int64) (*plan.Plan, error)
}

func (m *mockProvider) CheckLimit(ctx context.Context, userID int64, resource plan.Resource) error {
	if m.checkLimitFn != nil {
		return m.checkLimitFn(ctx, userID, resource)
	}
	return nil
}

func (m *mockProvider) GetPlan(ctx context.Context, userID int64) (*plan.Plan, error) {
	if m.getPlanFn != nil {
		return m.getPlanFn(ctx, userID)
	}
	return &plan.Plan{Tier: "free", Limits: plan.Unlimited}, nil
}

// TestMockProvider_BlocksWhenLimitHit verifies that a mock provider correctly
// returns a PlanLimitError (pattern for testing enforcement middleware).
func TestMockProvider_BlocksWhenLimitHit(t *testing.T) {
	p := &mockProvider{
		checkLimitFn: func(_ context.Context, _ int64, resource plan.Resource) error {
			if resource == plan.ResourceQSOCreate {
				return &plan.PlanLimitError{
					Resource: resource,
					Tier:     "free",
					Limit:    500,
					Current:  500,
					Message:  "free tier limit of 500 QSOs reached",
				}
			}
			return nil
		},
	}

	err := p.CheckLimit(context.Background(), 1, plan.ResourceQSOCreate)
	if err == nil {
		t.Fatal("expected PlanLimitError, got nil")
	}
	if !plan.IsPlanLimitError(err) {
		t.Errorf("expected PlanLimitError, got %T: %v", err, err)
	}

	// Other resources should still pass.
	if err := p.CheckLimit(context.Background(), 1, plan.ResourceLogbookCreate); err != nil {
		t.Errorf("ResourceLogbookCreate: expected nil, got %v", err)
	}
}
