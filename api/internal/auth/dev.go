package auth

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	db "github.com/FtlC-ian/radioledger/api/internal/database/sqlc"
)

// DevAuth is an Authenticator for local development that accepts simple bearer
// tokens in the format "dev-user-{id}" where {id} is the internal database user ID.
//
// This implementation requires NO external services. It is enabled when AUTH_MODE=dev
// and MUST NEVER be used in production — the server enforces this by refusing to
// start with AUTH_MODE=dev when APP_ENV=production.
//
// Token format:  "dev-user-12345"
// HTTP header:   Authorization: Bearer dev-user-12345
//
// The user must already exist in the database. The token encodes no credentials;
// it simply maps an integer ID to a database user record. This is intentionally
// insecure — the whole point is to eliminate auth friction during local development.
type DevAuth struct {
	Pool *pgxpool.Pool
}

// ValidateToken implements Authenticator for dev mode.
// Accepts tokens in the format "dev-user-{id}". Looks up the user in the database
// to populate UserInfo fields. Returns an error if the format is invalid or if the
// user does not exist in the database.
func (d *DevAuth) ValidateToken(ctx context.Context, token string) (UserInfo, error) {
	const prefix = "dev-user-"
	if !strings.HasPrefix(token, prefix) {
		return UserInfo{}, fmt.Errorf("dev auth: invalid token format — expected dev-user-{id}, got %q", token)
	}

	idStr := token[len(prefix):]
	userID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || userID <= 0 {
		return UserInfo{}, fmt.Errorf("dev auth: invalid user ID in token: %q", idStr)
	}

	// Look up the user to populate UserInfo (UUID, email).
	// This ensures the token references a real, non-deleted user account.
	queries := db.New(d.Pool)
	row, err := queries.GetUserByID(ctx, userID)
	if err != nil {
		return UserInfo{}, fmt.Errorf("dev auth: user ID %d not found: %w", userID, err)
	}

	return UserInfo{
		UserID:   row.ID,
		UserUUID: row.Uuid,
		Email:    row.Email,
	}, nil
}
