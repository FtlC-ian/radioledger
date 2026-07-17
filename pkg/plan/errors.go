package plan

import (
	"errors"
	"fmt"
)

// PlanLimitError is returned by Provider.CheckLimit when a resource cap has
// been reached. Callers should translate this into an HTTP 403 response with a
// clear message pointing the user toward an upgrade.
type PlanLimitError struct {
	// Resource is the operation that was blocked.
	Resource Resource

	// Tier is the user's current subscription tier.
	Tier string

	// Limit is the cap that was reached. -1 means unlimited (should never appear here).
	Limit int64

	// Current is the user's current usage count.
	Current int64

	// Message is a human-readable description of the limit.
	Message string
}

func (e *PlanLimitError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("plan limit reached: %s tier allows %d, currently at %d",
		e.Tier, e.Limit, e.Current)
}

// IsPlanLimitError returns true if err is (or wraps) a *PlanLimitError.
func IsPlanLimitError(err error) bool {
	if err == nil {
		return false
	}
	var target *PlanLimitError
	return errors.As(err, &target)
}
