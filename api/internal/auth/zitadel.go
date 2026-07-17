package auth

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	db "github.com/FtlC-ian/radioledger/api/internal/database/sqlc"
)

// ErrInviteRequired is returned when Zitadel authentication is valid but local
// account provisioning is gated behind an invite.
var ErrInviteRequired = errors.New("invite required")

// ZitadelAuth validates RS256 JWTs issued by a Zitadel instance.
//
// On first call, it fetches the JWKS (JSON Web Key Set) from Zitadel's
// /oauth/v2/keys endpoint. Keys are cached in memory and refreshed every
// KeyRefreshInterval (default: 1 hour) or when a requested key ID is not found.
//
// On successful validation, ZitadelAuth looks up (or provisions) the user in the
// local database using the Zitadel subject claim ("sub"). The users table must
// have a zitadel_id column.
//
// Configuration via environment variables (see config.Config):
//
//	ZITADEL_URL        — base URL of the Zitadel instance (required for zitadel mode)
//	ZITADEL_CLIENT_ID  — OAuth2 client ID for audience validation (required)
//	AUTH_MODE=zitadel  — enables this implementation
type ZitadelAuth struct {
	// ZitadelURL is the base URL of the Zitadel instance.
	// Example: "https://your-org.zitadel.cloud" or "http://zitadel:8080" for Docker.
	ZitadelURL string

	// ClientID is the OIDC client ID (audience) expected in validated tokens.
	// Tokens whose "aud" claim does not contain this value will be rejected.
	// Must match ZITADEL_CLIENT_ID.
	ClientID string

	// Pool is used to look up or provision the local user record on first login.
	Pool *pgxpool.Pool

	// RequireInviteKey blocks automatic first-time provisioning until an invite is consumed.
	RequireInviteKey bool

	// KeyRefreshInterval controls JWKS key cache TTL. Defaults to 1 hour.
	KeyRefreshInterval time.Duration

	mu          sync.RWMutex
	keys        map[string]*rsa.PublicKey // kid → RSA public key
	lastFetched time.Time
}

// OIDCIdentity is the normalized Zitadel identity extracted from a validated access token.
type OIDCIdentity struct {
	Subject string
	Email   string
}

// zitadelClaims holds the JWT claims extracted from a Zitadel access token.
type zitadelClaims struct {
	jwt.RegisteredClaims
	Email string `json:"email"`
	Name  string `json:"name"`
}

// jwkSet is the JWKS response from Zitadel's /oauth/v2/keys endpoint.
type jwkSet struct {
	Keys []jwk `json:"keys"`
}

// jwk represents a single JSON Web Key (RSA only; EC keys are skipped).
type jwk struct {
	Kid string `json:"kid"`
	Kty string `json:"kty"`
	Alg string `json:"alg"`
	N   string `json:"n"` // RSA modulus (base64url)
	E   string `json:"e"` // RSA public exponent (base64url)
}

// ValidateToken implements Authenticator for production Zitadel mode.
func (z *ZitadelAuth) ValidateToken(ctx context.Context, token string) (UserInfo, error) {
	identity, err := z.ResolveIdentityFromToken(ctx, token)
	if err != nil {
		return UserInfo{}, fmt.Errorf("zitadel auth: %w", err)
	}

	userInfo, found, err := z.LookupOrLinkUser(ctx, identity)
	if err != nil {
		return UserInfo{}, fmt.Errorf("zitadel auth: user lookup failed: %w", err)
	}
	if found {
		userInfo.ExternalID = identity.Subject
		return userInfo, nil
	}

	if z.RequireInviteKey {
		return UserInfo{}, ErrInviteRequired
	}

	userInfo, err = z.ProvisionUser(ctx, identity)
	if err != nil {
		return UserInfo{}, fmt.Errorf("zitadel auth: user provision failed: %w", err)
	}
	userInfo.ExternalID = identity.Subject
	return userInfo, nil
}

// ResolveIdentityFromToken validates the Zitadel access token and returns the normalized subject/email pair.
func (z *ZitadelAuth) ResolveIdentityFromToken(ctx context.Context, token string) (OIDCIdentity, error) {
	claims, err := z.parseClaims(ctx, token)
	if err != nil {
		return OIDCIdentity{}, err
	}

	email, err := z.resolveEmail(ctx, token, claims.Email)
	if err != nil {
		return OIDCIdentity{}, err
	}

	return OIDCIdentity{
		Subject: claims.Subject,
		Email:   email,
	}, nil
}

// LookupOrLinkUser resolves an existing local account for the validated OIDC identity.
// It first looks up by stored zitadel_id, then links a pre-existing local account by email.
func (z *ZitadelAuth) LookupOrLinkUser(ctx context.Context, identity OIDCIdentity) (UserInfo, bool, error) {
	if z.Pool == nil {
		return UserInfo{}, false, fmt.Errorf("database pool not configured")
	}

	tx, err := beginWorkerTx(ctx, z.Pool)
	if err != nil {
		return UserInfo{}, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := db.New(tx)
	zitadelID := identity.Subject

	row, err := q.GetUserByZitadelID(ctx, &zitadelID)
	if err == nil {
		if err := tx.Commit(ctx); err != nil {
			return UserInfo{}, false, fmt.Errorf("commit tx: %w", err)
		}
		return userInfoFromLookupRow(row.ID, row.Uuid, row.Email), true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return UserInfo{}, false, fmt.Errorf("lookup by zitadel_id: %w", err)
	}

	if identity.Email == "" {
		return UserInfo{}, false, nil
	}

	linked, err := q.LinkUserZitadelIDByEmail(ctx, db.LinkUserZitadelIDByEmailParams{
		ZitadelID: &zitadelID,
		Email:     identity.Email,
	})
	if err == nil {
		if err := tx.Commit(ctx); err != nil {
			return UserInfo{}, false, fmt.Errorf("commit tx: %w", err)
		}
		return userInfoFromLookupRow(linked.ID, linked.Uuid, linked.Email), true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return UserInfo{}, false, fmt.Errorf("link zitadel_id to existing user: %w", err)
	}

	return UserInfo{}, false, nil
}

// ProvisionUser creates a new local account for a validated Zitadel identity.
func (z *ZitadelAuth) ProvisionUser(ctx context.Context, identity OIDCIdentity) (UserInfo, error) {
	if z.Pool == nil {
		return UserInfo{}, fmt.Errorf("database pool not configured")
	}
	if identity.Email == "" {
		return UserInfo{}, fmt.Errorf("cannot provision user: token missing email claim")
	}

	tx, err := beginWorkerTx(ctx, z.Pool)
	if err != nil {
		return UserInfo{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := db.New(tx)
	zitadelID := identity.Subject
	row, err := q.CreateZitadelUser(ctx, db.CreateZitadelUserParams{
		Email:     identity.Email,
		ZitadelID: &zitadelID,
		Timezone:  nil,
	})
	if err != nil {
		return UserInfo{}, fmt.Errorf("create user: %w", err)
	}

	if err := CreateDefaultLogbookForUser(ctx, tx, row.ID); err != nil {
		return UserInfo{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return UserInfo{}, fmt.Errorf("commit tx: %w", err)
	}

	return userInfoFromLookupRow(row.ID, row.Uuid, row.Email), nil
}

func beginWorkerTx(ctx context.Context, pool *pgxpool.Pool) (pgx.Tx, error) {
	if pool == nil {
		return nil, fmt.Errorf("database pool not configured")
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	if _, err := tx.Exec(ctx, "SET LOCAL ROLE radioledger_worker"); err != nil {
		_ = tx.Rollback(ctx)
		return nil, fmt.Errorf("set role: %w", err)
	}
	return tx, nil
}

func userInfoFromLookupRow(id int64, userUUID uuid.UUID, email string) UserInfo {
	return UserInfo{
		UserID:   id,
		UserUUID: userUUID,
		Email:    email,
	}
}

func (z *ZitadelAuth) parseClaims(ctx context.Context, token string) (*zitadelClaims, error) {
	var claims zitadelClaims

	parseOpts := []jwt.ParserOption{
		jwt.WithIssuer(z.ZitadelURL),
		jwt.WithExpirationRequired(),
	}
	// Enforce audience claim when a client ID is configured.
	// This prevents tokens issued for other Zitadel applications (same instance)
	// from being accepted by RadioLedger. ZITADEL_CLIENT_ID must be set in production.
	if z.ClientID != "" {
		parseOpts = append(parseOpts, jwt.WithAudience(z.ClientID))
	}

	parsed, err := jwt.ParseWithClaims(token, &claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}

		kid, _ := t.Header["kid"].(string)
		key, keyErr := z.getKey(ctx, kid)
		if keyErr != nil {
			return nil, fmt.Errorf("get signing key: %w", keyErr)
		}
		return key, nil
	}, parseOpts...)
	if err != nil || !parsed.Valid {
		return nil, fmt.Errorf("invalid token: %w", err)
	}
	if claims.Subject == "" {
		return nil, fmt.Errorf("token missing sub claim")
	}
	return &claims, nil
}

// getKey returns the RSA public key for the given key ID.
// Fetches from Zitadel if the cache is empty or stale, or if the kid is not found.
func (z *ZitadelAuth) getKey(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	z.mu.RLock()
	key, found := z.keys[kid]
	stale := time.Since(z.lastFetched) > z.refreshInterval()
	z.mu.RUnlock()

	if found && !stale {
		return key, nil
	}

	// Refresh keys from Zitadel.
	if err := z.fetchKeys(ctx); err != nil {
		if found {
			// Return the stale key rather than failing completely during a Zitadel outage.
			return key, nil
		}
		return nil, fmt.Errorf("fetch JWKS: %w", err)
	}

	z.mu.RLock()
	key, found = z.keys[kid]
	z.mu.RUnlock()

	if !found {
		return nil, fmt.Errorf("key ID %q not found in JWKS", kid)
	}
	return key, nil
}

// userInfoResponse holds the response from the OIDC /userinfo endpoint.
type userInfoResponse struct {
	Sub   string `json:"sub"`
	Email string `json:"email"`
	Name  string `json:"name"`
}

func (z *ZitadelAuth) resolveEmail(ctx context.Context, accessToken, jwtEmail string) (string, error) {
	email := normalizeEmail(jwtEmail)
	if email != "" {
		return email, nil
	}

	ui, err := z.fetchUserInfo(ctx, accessToken)
	if err != nil {
		return "", fmt.Errorf("token missing email claim and userinfo fetch failed: %w", err)
	}

	email = normalizeEmail(ui.Email)
	if email == "" {
		return "", fmt.Errorf("token missing email claim and userinfo response missing email")
	}

	return email, nil
}

// fetchUserInfo calls the Zitadel /oidc/v1/userinfo endpoint using the access token
// to retrieve user claims (email, name) that may not be present in the JWT.
//
// A dedicated HTTP client with a 5-second timeout is used to prevent goroutine
// leaks if the Zitadel instance is slow or unresponsive.
func (z *ZitadelAuth) fetchUserInfo(ctx context.Context, accessToken string) (*userInfoResponse, error) {
	url := strings.TrimRight(z.ZitadelURL, "/") + "/oidc/v1/userinfo"

	timeoutCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(timeoutCtx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	httpClient := &http.Client{Timeout: 5 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("userinfo returned %d", resp.StatusCode)
	}

	var ui userInfoResponse
	if err := json.Unmarshal(body, &ui); err != nil {
		return nil, fmt.Errorf("parse userinfo: %w", err)
	}
	return &ui, nil
}

// fetchKeys retrieves the JWKS from Zitadel and updates the in-memory key cache.
//
// A dedicated HTTP client with a 10-second timeout is used to prevent goroutine
// leaks if the Zitadel instance is slow or unresponsive.
func (z *ZitadelAuth) fetchKeys(ctx context.Context) error {
	jwksURL := strings.TrimRight(z.ZitadelURL, "/") + "/oauth/v2/keys"

	timeoutCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(timeoutCtx, http.MethodGet, jwksURL, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}

	httpClient := &http.Client{Timeout: 10 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("GET %s: %w", jwksURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("JWKS endpoint returned %d: %s", resp.StatusCode, body)
	}

	var set jwkSet
	if err := json.Unmarshal(body, &set); err != nil {
		return fmt.Errorf("parse JWKS: %w", err)
	}

	newKeys := make(map[string]*rsa.PublicKey, len(set.Keys))
	for _, k := range set.Keys {
		if k.Kty != "RSA" {
			continue // skip non-RSA keys (EC keys are not used by Zitadel by default)
		}
		pub, parseErr := parseRSAKey(k)
		if parseErr != nil {
			continue // skip unparseable keys; don't fail the whole refresh
		}
		newKeys[k.Kid] = pub
	}

	z.mu.Lock()
	z.keys = newKeys
	z.lastFetched = time.Now()
	z.mu.Unlock()

	return nil
}

// parseRSAKey decodes a JWK RSA public key into a Go *rsa.PublicKey.
// The modulus (N) and exponent (E) are base64url-encoded big integers.
func parseRSAKey(k jwk) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(k.N)
	if err != nil {
		return nil, fmt.Errorf("decode N: %w", err)
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(k.E)
	if err != nil {
		return nil, fmt.Errorf("decode E: %w", err)
	}

	n := new(big.Int).SetBytes(nBytes)
	var e int
	for _, b := range eBytes {
		e = e<<8 | int(b)
	}

	return &rsa.PublicKey{N: n, E: e}, nil
}

func (z *ZitadelAuth) refreshInterval() time.Duration {
	if z.KeyRefreshInterval <= 0 {
		return time.Hour
	}
	return z.KeyRefreshInterval
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
