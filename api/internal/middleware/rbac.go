package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/FtlC-ian/radioledger/api/internal/auth"
	db "github.com/FtlC-ian/radioledger/api/internal/database/sqlc"
)

type rbacContextKey int

const logbookRoleKey rbacContextKey = iota

// RequireLogbookPermission enforces RBAC permissions for logbook-scoped routes.
// It resolves the caller's role for {logbookUUID}, checks whether the role grants
// the required permission, and returns 403 when access is denied.
func RequireLogbookPermission(pool *pgxpool.Pool, permission auth.Permission) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if pool == nil {
				writeRBACFailure(w, http.StatusServiceUnavailable, "database unavailable", "authorization is unavailable")
				return
			}

			userID, ok := auth.UserIDFromContext(r.Context())
			if !ok || userID <= 0 {
				writeRBACFailure(w, http.StatusUnauthorized, "unauthorized", "missing authenticated user")
				return
			}

			logbookUUID, err := uuid.Parse(chi.URLParam(r, "logbookUUID"))
			if err != nil {
				writeRBACFailure(w, http.StatusBadRequest, "invalid request", "invalid logbook UUID")
				return
			}

			// Must use a tenant-scoped transaction because user_roles has RLS
			// policies that require radioledger_api role and app.current_user_id.
			tx, err := pool.Begin(r.Context())
			if err != nil {
				writeRBACFailure(w, http.StatusServiceUnavailable, "database unavailable", "could not begin transaction")
				return
			}
			defer func() { _ = tx.Rollback(r.Context()) }()

			if _, err := tx.Exec(r.Context(), "SET LOCAL ROLE radioledger_api"); err != nil {
				writeRBACFailure(w, http.StatusInternalServerError, "authorization failed", "could not set role")
				return
			}
			if _, err := tx.Exec(r.Context(), "SELECT set_config('app.current_user_id', $1, true)", strconv.FormatInt(userID, 10)); err != nil {
				writeRBACFailure(w, http.StatusInternalServerError, "authorization failed", "could not set tenant context")
				return
			}

			queries := db.New(tx)
			roleValue, err := queries.GetUserRoleForLogbook(r.Context(), db.GetUserRoleForLogbookParams{
				LogbookUuid: logbookUUID,
				UserID:      userID,
			})
			if errors.Is(err, pgx.ErrNoRows) {
				writeRBACFailure(w, http.StatusForbidden, "forbidden", "insufficient permissions")
				return
			}
			if err != nil {
				writeRBACFailure(w, http.StatusInternalServerError, "authorization failed", "could not resolve role")
				return
			}

			role, valid := auth.ParseRole(roleValue)
			if !valid || !role.HasPermission(permission) {
				writeRBACFailure(w, http.StatusForbidden, "forbidden", "insufficient permissions")
				return
			}

			ctx := context.WithValue(r.Context(), logbookRoleKey, role)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// LogbookRoleFromContext returns the role resolved by RequireLogbookPermission.
func LogbookRoleFromContext(ctx context.Context) (auth.Role, bool) {
	role, ok := ctx.Value(logbookRoleKey).(auth.Role)
	return role, ok
}

type rbacEnvelope struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
}

func writeRBACFailure(w http.ResponseWriter, status int, message, errMsg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(rbacEnvelope{
		Success: false,
		Message: message,
		Error:   errMsg,
	})
}
