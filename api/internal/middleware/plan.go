package middleware

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/FtlC-ian/radioledger/api/internal/auth"
	"github.com/FtlC-ian/radioledger/pkg/plan"
)

// EnforcePlan returns a middleware that checks the given resource limit before
// allowing the request to proceed.
//
// If the plan.Provider.CheckLimit returns nil, the request passes through.
// If it returns a *plan.PlanLimitError, the middleware responds with HTTP 403
// and a descriptive error message.
// Any other error is treated as a non-fatal billing failure and the request is
// allowed through (fail open) to avoid blocking users due to billing infra issues.
//
// The DefaultProvider always returns nil, so this middleware is a no-op for
// self-hosted installations.
func EnforcePlan(provider plan.Provider, resource plan.Resource) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID, ok := auth.UserIDFromContext(r.Context())
			if !ok || userID == 0 {
				// Auth middleware should have already rejected unauthenticated requests.
				// Fail safe: block the request.
				writePlanJSON(w, http.StatusUnauthorized, "unauthorized", "unauthenticated request")
				return
			}

			if err := provider.CheckLimit(r.Context(), userID, resource); err != nil {
				var limitErr *plan.PlanLimitError
				if errors.As(err, &limitErr) {
					slog.InfoContext(r.Context(), "plan limit reached",
						slog.Int64("user_id", userID),
						slog.String("tier", limitErr.Tier),
						slog.Int64("limit", limitErr.Limit),
						slog.Int64("current", limitErr.Current),
					)
					writePlanJSON(w, http.StatusForbidden, "plan limit reached", limitErr.Error())
					return
				}

				// Unexpected error from the plan provider — log it and fail open
				// (let the request through) to avoid blocking users due to billing
				// infrastructure failures. The operation itself may still fail for
				// other reasons, but we don't want billing downtime to block logging.
				slog.ErrorContext(r.Context(), "plan: CheckLimit error (failing open)",
					slog.Int64("user_id", userID),
					slog.String("error", err.Error()),
				)
			}

			next.ServeHTTP(w, r)
		})
	}
}

// writePlanJSON writes a JSend-compatible JSON error response.
func writePlanJSON(w http.ResponseWriter, status int, message, errMsg string) {
	body, _ := json.Marshal(struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
		Error   string `json:"error"`
	}{
		Success: false,
		Message: message,
		Error:   errMsg,
	})
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}
