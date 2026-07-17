package handler

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/FtlC-ian/radioledger/api/internal/auth"
	db "github.com/FtlC-ian/radioledger/api/internal/database/sqlc"
)

var errForbiddenRBAC = errors.New("insufficient permissions")

func resolveLogbookRole(ctx context.Context, queries *db.Queries, userID int64, logbookUUID uuid.UUID) (auth.Role, error) {
	roleValue, err := queries.GetUserRoleForLogbook(ctx, db.GetUserRoleForLogbookParams{
		LogbookUuid: logbookUUID,
		UserID:      userID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", errForbiddenRBAC
	}
	if err != nil {
		return "", err
	}

	role, ok := auth.ParseRole(roleValue)
	if !ok {
		return "", fmt.Errorf("unknown role %q", roleValue)
	}
	return role, nil
}

func ensureLogbookPermission(ctx context.Context, queries *db.Queries, userID int64, logbookUUID uuid.UUID, permission auth.Permission) (auth.Role, error) {
	role, err := resolveLogbookRole(ctx, queries, userID, logbookUUID)
	if err != nil {
		return "", err
	}
	if !role.HasPermission(permission) {
		return "", errForbiddenRBAC
	}
	return role, nil
}

// ensureLogbookPermissionPool checks logbook permission by opening a short-lived
// tenant-scoped transaction against the pool. Use this when no tenant transaction
// is already in progress (e.g. before the main work transaction is opened).
//
// The user_roles table has RLS policies that require radioledger_api role and
// app.current_user_id to be set; calling db.New(pool) directly bypasses those
// policies and always returns zero rows. This function sets the required RLS
// context inside a transaction before querying.
func ensureLogbookPermissionPool(ctx context.Context, pool *pgxpool.Pool, userID int64, logbookUUID uuid.UUID, permission auth.Permission) (auth.Role, error) {
	tx, err := beginTenantTx(ctx, pool, userID)
	if err != nil {
		return "", fmt.Errorf("begin tenant tx for RBAC check: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	queries := db.New(tx)
	return ensureLogbookPermission(ctx, queries, userID, logbookUUID, permission)
}
