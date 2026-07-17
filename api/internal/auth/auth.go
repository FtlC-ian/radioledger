// Package auth provides pluggable authentication for the RadioLedger API.
//
// The design follows ARCHITECTURE.md § "Authentication: Zitadel": in production,
// bearer tokens are RS256 JWTs issued by Zitadel and validated against its JWKS
// endpoint. In development, local auth still uses signed JWTs (HS256) without
// external dependencies.
//
// Usage in middleware:
//
//	authenticator := auth.NewFromConfig(cfg, pool)
//	r.Use(middleware.Auth(authenticator))
//
// Retrieve the authenticated identity inside a handler:
//
//	info, ok := auth.UserInfoFromContext(r.Context())
package auth

import (
	"context"

	"github.com/google/uuid"
)

// UserInfo contains the authenticated user's identity extracted from a validated token.
// The auth middleware populates this and stores it in the request context.
// All downstream handlers receive it via UserInfoFromContext.
type UserInfo struct {
	// UserID is the internal database primary key (BIGSERIAL).
	// Used for RLS enforcement via SET LOCAL app.current_user_id.
	// NEVER expose this in API responses — use UserUUID instead.
	UserID int64

	// UserUUID is the external-facing stable identifier (UUID v4).
	// Always use this in API responses and URLs.
	UserUUID uuid.UUID

	// Email is the user's email address.
	Email string

	// ExternalID is the Zitadel user subject claim ("sub").
	// Empty string in dev mode. Set in production Zitadel mode.
	ExternalID string

	// APIKeyScopes holds the scopes granted to an API key. Nil means no scope
	// restriction — the request was authenticated via JWT (not an API key).
	// A non-nil slice (even empty) means the request was authenticated with an
	// API key; only the listed scopes are permitted on restricted endpoints.
	APIKeyScopes *[]string
}

// Authenticator validates bearer tokens and returns authenticated user information.
// Implementations must be safe for concurrent use from multiple goroutines.
type Authenticator interface {
	// ValidateToken parses and validates the given bearer token string.
	// Returns UserInfo on success, or an error if the token is invalid or expired.
	// Implementations should return descriptive errors for server-side logging;
	// the auth middleware returns only a generic 401 to the HTTP client.
	ValidateToken(ctx context.Context, token string) (UserInfo, error)
}

// contextKey is a private type for storing auth values in context.
// Using a named type prevents collisions with keys from other packages.
type contextKey int

const userInfoKey contextKey = iota

// ContextWithUserInfo returns a new context containing the given UserInfo.
// Called by the auth middleware after successful token validation.
func ContextWithUserInfo(ctx context.Context, info UserInfo) context.Context {
	return context.WithValue(ctx, userInfoKey, info)
}

// UserInfoFromContext retrieves the UserInfo placed by the auth middleware.
// Returns (zero UserInfo, false) if the context has no authenticated user
// (e.g., health endpoints that bypass auth).
func UserInfoFromContext(ctx context.Context) (UserInfo, bool) {
	info, ok := ctx.Value(userInfoKey).(UserInfo)
	return info, ok
}

// UserIDFromContext is a convenience helper returning the authenticated user's
// internal DB ID. Returns (0, false) if the context is unauthenticated.
func UserIDFromContext(ctx context.Context) (int64, bool) {
	info, ok := UserInfoFromContext(ctx)
	if !ok || info.UserID == 0 {
		return 0, false
	}
	return info.UserID, true
}
