// Package plan defines the access-policy abstraction for RadioLedger.
//
// The core ships with a DefaultProvider that returns unlimited access for
// everything — self-hosted behavior. Other deployments may swap this out for a
// provider that checks their own account system and tier limits.
//
// Self-hosted operators are never restricted; the tier column in the users
// table exists but only affects auto-sync frequency (not resource limits) when
// using the DefaultProvider.
package plan

import (
	"context"
	"time"
)

// Plan represents a user's current access state.
type Plan struct {
	// Tier is a deployment-defined access tier name.
	Tier string `json:"tier"`

	// ExpiresAt is when a deployment-defined access period expires. Nil means no expiry.
	ExpiresAt *time.Time `json:"expires_at"`

	// Limits defines the resource caps for this plan tier.
	Limits Limits `json:"limits"`

	// UpgradeURL is an optional deployment account-management URL. It is empty by default.
	UpgradeURL string `json:"upgrade_url,omitempty"`
}

// Limits defines the resource caps for a plan tier.
// Use -1 to indicate unlimited.
type Limits struct {
	MaxQSOs         int64 `json:"max_qsos"`
	MaxLogbooks     int   `json:"max_logbooks"`
	MaxSyncServices int   `json:"max_sync_services"`

	// Feature flags
	APIAccess      bool `json:"api_access"`
	MobileAccess   bool `json:"mobile_access"`
	DesktopCapture bool `json:"desktop_capture"`
	PrioritySync   bool `json:"priority_sync"`
	ContestLogging bool `json:"contest_logging"`
	AdvancedStats  bool `json:"advanced_stats"`
	AwardsTracking bool `json:"awards_tracking"`
}

// Provider resolves the current plan for a user.
//
// The core ships a DefaultProvider that reads from the users table and returns
// unlimited access for self-hosted installations. A deployment that needs
// different access controls may supply its own provider.
//
// All implementations must be safe for concurrent use from multiple goroutines.
type Provider interface {
	// GetPlan returns the active plan and limits for a user.
	// Used by the plan status endpoint to show the user their current tier and limits.
	GetPlan(ctx context.Context, userID int64) (*Plan, error)

	// CheckLimit returns nil if the action is allowed, or a PlanLimitError
	// describing which limit was hit.
	//
	// DefaultProvider always returns nil — self-hosted users are never restricted.
	// A custom provider may enforce deployment-specific limits.
	CheckLimit(ctx context.Context, userID int64, resource Resource) error
}

// Resource identifies a countable or gate-able action.
type Resource int

const (
	// ResourceQSOCreate gates QSO creation and bulk import.
	ResourceQSOCreate Resource = iota

	// ResourceLogbookCreate gates logbook creation.
	ResourceLogbookCreate

	// ResourceSyncService gates adding a new sync service credential.
	ResourceSyncService

	// ResourceAPIAccess gates API key creation (boolean feature flag).
	ResourceAPIAccess

	// ResourceMobileAccess gates mobile app access (boolean feature flag).
	ResourceMobileAccess

	// ResourceDesktopCapture gates desktop capture features (boolean feature flag).
	ResourceDesktopCapture

	// ResourceContestLog gates contest logging features (boolean feature flag).
	ResourceContestLog

	// ResourceAdvancedStats gates advanced analytics endpoints (boolean feature flag).
	ResourceAdvancedStats

	// ResourceAwardsTracking gates awards tracking features (boolean feature flag).
	ResourceAwardsTracking
)
