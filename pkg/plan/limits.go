package plan

// Unlimited is a Limits value where every cap is -1 (no restrictions) and
// all feature flags are enabled. Used by DefaultProvider for self-hosted installs.
var Unlimited = Limits{
	MaxQSOs:         -1,
	MaxLogbooks:     -1,
	MaxSyncServices: -1,
	APIAccess:       true,
	MobileAccess:    true,
	DesktopCapture:  true,
	PrioritySync:    true,
	ContestLogging:  true,
	AdvancedStats:   true,
	AwardsTracking:  true,
}

// TierLimits maps subscription tier names to their resource caps.
//
// These are optional reference values for deployment tiers. The DefaultProvider
// uses Unlimited for all tiers because self-hosted installations are unrestricted.
//
// Operators running self-hosted multi-tenant deployments can override this
// by implementing their own Provider — see pkg/plan.Provider.
var TierLimits = map[string]Limits{
	"free": {
		MaxQSOs:         500,
		MaxLogbooks:     1,
		MaxSyncServices: 1,
		APIAccess:       false,
		MobileAccess:    true,
		DesktopCapture:  false,
		PrioritySync:    false,
		ContestLogging:  false,
		AdvancedStats:   false,
		AwardsTracking:  true,
	},
	"standard": {
		MaxQSOs:         5_000,
		MaxLogbooks:     3,
		MaxSyncServices: 3,
		APIAccess:       true,
		MobileAccess:    true,
		DesktopCapture:  true,
		PrioritySync:    false,
		ContestLogging:  true,
		AdvancedStats:   true,
		AwardsTracking:  true,
	},
	"premium": {
		MaxQSOs:         -1,
		MaxLogbooks:     10,
		MaxSyncServices: 5,
		APIAccess:       true,
		MobileAccess:    true,
		DesktopCapture:  true,
		PrioritySync:    true,
		ContestLogging:  true,
		AdvancedStats:   true,
		AwardsTracking:  true,
	},
	"club": {
		MaxQSOs:         -1,
		MaxLogbooks:     -1,
		MaxSyncServices: -1,
		APIAccess:       true,
		MobileAccess:    true,
		DesktopCapture:  true,
		PrioritySync:    true,
		ContestLogging:  true,
		AdvancedStats:   true,
		AwardsTracking:  true,
	},
}

// LimitsForTier returns the Limits for the given subscription tier.
// Falls back to Unlimited if the tier is not recognised (defensive; self-hosted
// operators may use custom tier names via direct SQL).
func LimitsForTier(tier string) Limits {
	if l, ok := TierLimits[tier]; ok {
		return l
	}
	return Unlimited
}
