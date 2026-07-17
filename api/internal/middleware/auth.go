package middleware

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/FtlC-ian/radioledger/api/internal/auth"
	"github.com/FtlC-ian/radioledger/api/internal/crypto"
	"github.com/FtlC-ian/radioledger/api/internal/database/sqlc"
	"github.com/FtlC-ian/radioledger/api/internal/metrics"
)

// errIPNotAllowed is returned by authenticateAPIKey when the client's IP address is
// not present in the key's allowed_ips list. The Auth middleware maps this to HTTP
// 403 Forbidden (not 401 Unauthorized) so callers can distinguish auth failure from
// network policy rejection.
var errIPNotAllowed = errors.New("api key auth: client IP not in allowed_ips list")

// Auth returns an HTTP middleware that validates bearer tokens.
//
// Supports two authentication paths:
//  1. JWT tokens (Zitadel OIDC or dev mode) — routed through the Authenticator.
//  2. API keys ("Bearer rl_...") — hashed and looked up in the api_keys table.
//
// On success, the authenticated UserInfo is stored in the request context and the
// next handler is called. On failure, the middleware responds with 401 Unauthorized.
//
// Architecture reference: ARCHITECTURE.md § "Authentication: Zitadel"
// Security reference: FINAL_SECURITY_AUDIT.md § "API key authentication path"
func Auth(authenticator auth.Authenticator, pool *pgxpool.Pool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var (
				userInfo auth.UserInfo
				authErr  error
			)

			token, ok := extractBearerToken(r)
			if ok {
				if isAPIKeyToken(token) {
					// API key path: hash the token and look it up in the database.
					userInfo, authErr = authenticateAPIKey(r.Context(), r, pool, token)
				} else {
					// JWT path: delegate to the configured authenticator (Zitadel or dev).
					userInfo, authErr = authenticator.ValidateToken(r.Context(), token)
				}
			} else {
				// EventSource cannot set Authorization headers in browsers.
				// For /stream endpoints, allow a short-lived one-time stream token in query params.
				streamToken := strings.TrimSpace(r.URL.Query().Get("stream_token"))
				if streamToken == "" {
					// Backward-compatible alias while clients migrate.
					streamToken = strings.TrimSpace(r.URL.Query().Get("stream_ticket"))
				}
				if streamToken != "" && strings.HasSuffix(r.URL.Path, "/stream") {
					userID, tokenErr := ConsumeStreamToken(streamToken, r.URL.Path, time.Now())
					if tokenErr == nil {
						userInfo = auth.UserInfo{UserID: userID}
					} else {
						authErr = tokenErr
					}
				} else {
					authErr = errors.New("missing Authorization header")
				}
			}

			if authErr != nil {
				slog.WarnContext(r.Context(), "auth: token validation failed",
					slog.String("error", authErr.Error()),
					slog.String("path", sanitizedPathForLogs(r)),
				)
				w.Header().Set("Content-Type", "application/json")
				// IP restriction failures are policy violations, not credential failures.
				// Return 403 Forbidden so the caller can distinguish the two cases.
				if errors.Is(authErr, errIPNotAllowed) {
					w.WriteHeader(http.StatusForbidden)
					_, _ = w.Write([]byte(`{"success":false,"message":"forbidden","error":"client IP not in allowed list"}`))
					return
				}
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"success":false,"message":"unauthorized","error":"invalid or expired token"}`))
				return
			}

			ctx := auth.ContextWithUserInfo(r.Context(), userInfo)
			SetUserID(ctx, userInfo.UserID)
			metrics.MarkUserActive(userInfo.UserID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// isAPIKeyToken returns true if the bearer token looks like a RadioLedger API key.
// All API keys begin with the "rl_" prefix.
func isAPIKeyToken(token string) bool {
	return strings.HasPrefix(token, "rl_")
}

// authenticateAPIKey validates a "rl_..." bearer token against the api_keys table.
//
// Process:
//  1. Hash the plaintext token with SHA-256.
//  2. Query api_keys WHERE key_hash = hash AND revoked_at IS NULL AND not expired.
//  3. Fetch the corresponding user row to populate UserInfo.
//  4. Asynchronously update last_used_at (best-effort, non-blocking).
//
// The plaintext token is NEVER logged.
func authenticateAPIKey(ctx context.Context, r *http.Request, pool *pgxpool.Pool, token string) (auth.UserInfo, error) {
	if pool == nil {
		return auth.UserInfo{}, fmt.Errorf("api key auth: database pool not available")
	}

	keyHash := crypto.HashAPIKey(token)

	// Query without RLS context — this is the authentication step itself.
	// RLS context (app.current_user_id) is set later by the tenant middleware.
	q := sqlc.New(pool)
	apiKey, err := q.GetAPIKeyByHash(ctx, keyHash)
	if err != nil {
		// Do not distinguish "not found" from "database error" — prevents oracle attacks.
		return auth.UserInfo{}, fmt.Errorf("api key auth: authentication failed")
	}

	// Enforce IP allowlist. If the key has a non-empty allowed_ips list, the client
	// IP must appear in it. Reject with errIPNotAllowed so the Auth middleware can
	// return 403 Forbidden instead of 401 Unauthorized.
	if len(apiKey.AllowedIps) > 0 {
		clientIPStr := ClientIP(r)
		clientAddr, parseErr := netip.ParseAddr(clientIPStr)
		if parseErr != nil {
			return auth.UserInfo{}, errIPNotAllowed
		}
		allowed := false
		for _, allowedAddr := range apiKey.AllowedIps {
			if clientAddr == allowedAddr {
				allowed = true
				break
			}
		}
		if !allowed {
			return auth.UserInfo{}, errIPNotAllowed
		}
	}

	// Fetch the user record to populate UserInfo (need uuid + email for context).
	var userUUID [16]byte
	var userEmail string
	err = pool.QueryRow(ctx,
		`SELECT uuid, email FROM users WHERE id = $1 AND deleted_at IS NULL`,
		apiKey.UserID,
	).Scan(&userUUID, &userEmail)
	if err != nil {
		return auth.UserInfo{}, fmt.Errorf("api key auth: user lookup failed")
	}

	// Best-effort: update last_used_at asynchronously.
	// Don't block the request on this audit update.
	clientIP := ClientIP(r)
	keyID := apiKey.ID
	go func() {
		bgCtx := context.Background()
		var ipArg interface{}
		if clientIP != "" {
			ipArg = clientIP
		}
		_, _ = pool.Exec(bgCtx,
			`UPDATE api_keys SET last_used_at = NOW(), last_used_ip = $2::inet WHERE id = $1`,
			keyID, ipArg,
		)
	}()

	// Capture the key's scopes to enforce least-privilege access downstream.
	// The RequireAPIKeyScope middleware uses this slice to gate restricted endpoints.
	scopes := make([]string, len(apiKey.Scopes))
	copy(scopes, apiKey.Scopes)

	return auth.UserInfo{
		UserID:       apiKey.UserID,
		UserUUID:     userUUID,
		Email:        userEmail,
		ExternalID:   "", // API keys don't have an external OIDC subject
		APIKeyScopes: &scopes,
	}, nil
}

// extractBearerToken parses the Authorization header and returns the bearer token.
// Returns ("", false) if the header is missing or not in "Bearer <token>" format.
func extractBearerToken(r *http.Request) (string, bool) {
	authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
	if authHeader == "" {
		return "", false
	}

	const prefix = "Bearer "
	if !strings.HasPrefix(authHeader, prefix) {
		return "", false
	}

	token := strings.TrimSpace(authHeader[len(prefix):])
	if token == "" {
		return "", false
	}
	return token, true
}
