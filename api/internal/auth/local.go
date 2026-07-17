package auth

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	db "github.com/FtlC-ian/radioledger/api/internal/database/sqlc"
)

const (
	localTokenTTL    = 30 * 24 * time.Hour // 30 days
	localTokenIssuer = "radioledger-local"
)

// LocalAuth is an Authenticator for local/self-hosted deployments that accepts
// HS256-signed JWTs issued by this server.
//
// It is enabled when AUTH_MODE=local (or the backward-compatible alias AUTH_MODE=dev)
// and MUST NEVER be used in production (APP_ENV=production).
type LocalAuth struct {
	// Secret is the HMAC-SHA256 signing key for JWT tokens.
	// Derive from MasterKey using Config.LocalJWTSecret() in production.
	// A fixed development default is used when MasterKey is not configured.
	Secret []byte

	// Pool is used to look up user records when validating tokens.
	Pool *pgxpool.Pool
}

// localClaims holds the JWT payload for a local-mode token.
type localClaims struct {
	jwt.RegisteredClaims
	// UserID is the internal DB primary key embedded in the token for fast lookup.
	UserID int64 `json:"uid"`
	// Email is duplicated in the token for convenience (avoids a DB hit on most paths).
	Email string `json:"email"`
}

// ValidateToken implements Authenticator for local mode.
//
// Accepts HS256 JWTs issued by IssueToken.
func (l *LocalAuth) ValidateToken(ctx context.Context, token string) (UserInfo, error) {
	// Parse and validate the HS256 JWT.
	var claims localClaims
	parsed, err := jwt.ParseWithClaims(token, &claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("local auth: unexpected signing method %v", t.Header["alg"])
		}
		return l.Secret, nil
	},
		jwt.WithIssuer(localTokenIssuer),
		jwt.WithExpirationRequired(),
	)
	if err != nil || !parsed.Valid {
		return UserInfo{}, fmt.Errorf("local auth: invalid token: %w", err)
	}

	if claims.UserID <= 0 {
		return UserInfo{}, fmt.Errorf("local auth: token missing uid claim")
	}

	// Verify the user still exists (handles deleted accounts).
	// Acquire a connection and switch to worker role to bypass RLS —
	// at this point no app.user_id is set, so RLS would filter the row out.
	conn, connErr := l.Pool.Acquire(ctx)
	if connErr != nil {
		return UserInfo{}, fmt.Errorf("local auth: acquire conn: %w", connErr)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "SET LOCAL ROLE radioledger_worker"); err != nil {
		return UserInfo{}, fmt.Errorf("local auth: set role: %w", err)
	}

	queries := db.New(conn)
	row, lookupErr := queries.GetUserByID(ctx, claims.UserID)
	if lookupErr != nil {
		return UserInfo{}, fmt.Errorf("local auth: user %d not found: %w", claims.UserID, lookupErr)
	}

	return UserInfo{
		UserID:   row.ID,
		UserUUID: row.Uuid,
		Email:    row.Email,
	}, nil
}

// IssueToken creates a signed HS256 JWT for the given user.
// The token is valid for localTokenTTL (30 days).
func (l *LocalAuth) IssueToken(userID int64, email string) (string, error) {
	now := time.Now()
	claims := localClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    localTokenIssuer,
			Subject:   strconv.FormatInt(userID, 10),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(localTokenTTL)),
		},
		UserID: userID,
		Email:  email,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(l.Secret)
}
