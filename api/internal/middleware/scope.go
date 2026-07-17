package middleware

import (
	"encoding/json"
	"net/http"

	"github.com/FtlC-ian/radioledger/api/internal/auth"
)

// API key scope constants. These match the values stored in the api_keys.scopes
// column (TEXT[]). See docs/SCHEMA.md § api_keys and ARCHITECTURE.md § "Token
// architecture" for the full scope vocabulary.
//
// A JWT-authenticated request is never scope-restricted. Scopes only apply to
// requests authenticated with a "Bearer rl_..." API key.
const (
	ScopeQSOsRead      = "qsos:read"
	ScopeQSOsWrite     = "qsos:write"
	ScopeQSOsDelete    = "qsos:delete"
	ScopeADIFImport    = "adif:import"
	ScopeADIFExport    = "adif:export"
	ScopeLogbooksRead  = "logbooks:read"
	ScopeLogbooksWrite = "logbooks:write"
	ScopeSyncTrigger   = "sync:trigger"
	ScopeSyncStatus    = "sync:status"
)

// RequireAPIKeyScope returns middleware that enforces API key scope restrictions.
//
// Behaviour:
//   - JWT-authenticated requests (UserInfo.APIKeyScopes == nil) pass through
//     unconditionally — JWT users are governed solely by RBAC.
//   - API key-authenticated requests (UserInfo.APIKeyScopes != nil) must have
//     the requested scope present in the slice. Missing scope → 403 Forbidden.
//
// This middleware must be placed after the Auth middleware in the chain so that
// UserInfo is already in the request context.
//
// Usage in the router (mirrors RequireLogbookPermission pattern):
//
//	r.With(middleware.RequireAPIKeyScope(middleware.ScopeQSOsRead)).Get("/", qsoHandler.List)
func RequireAPIKeyScope(scope string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			info, ok := auth.UserInfoFromContext(r.Context())
			if !ok {
				writeScopeFailure(w, http.StatusUnauthorized, "unauthorized", "missing authenticated user")
				return
			}

			// Nil APIKeyScopes means JWT authentication — no scope restriction.
			if info.APIKeyScopes == nil {
				next.ServeHTTP(w, r)
				return
			}

			// API key: verify the required scope is present.
			for _, s := range *info.APIKeyScopes {
				if s == scope {
					next.ServeHTTP(w, r)
					return
				}
			}

			writeScopeFailure(w, http.StatusForbidden, "forbidden",
				"api key is missing required scope: "+scope)
		})
	}
}

type scopeEnvelope struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
}

func writeScopeFailure(w http.ResponseWriter, status int, message, errMsg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(scopeEnvelope{
		Success: false,
		Message: message,
		Error:   errMsg,
	})
}
