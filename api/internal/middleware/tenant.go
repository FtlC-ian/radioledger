package middleware

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/FtlC-ian/radioledger/api/internal/auth"
)

// tenantContextKey is used to store the database transaction in the request context.
// Only the tenant middleware and the database helpers should access this key directly.
type tenantContextKey int

const dbTxKey tenantContextKey = iota

// Tenant returns an HTTP middleware that sets up the PostgreSQL Row-Level Security
// context for every authenticated request.
//
// It begins a transaction, sets SET LOCAL app.current_user_id to the authenticated
// user's internal ID, and stores the transaction in the request context. Handlers
// retrieve the transaction via TxFromContext.
//
// IMPORTANT: This middleware must run AFTER the Auth middleware, which places the
// authenticated UserInfo in the request context. Requests without a user ID in
// context (unauthenticated) will receive a 503 response.
//
// CRITICAL: Uses SET LOCAL (transaction-scoped), NOT bare SET (session-scoped).
// Session-scoped settings bleed into the next request on a pooled connection and
// would allow cross-tenant data access. See ARCHITECTURE.md § "Multi-Tenant Isolation".
//
// Architecture reference: ARCHITECTURE.md § "Multi-Tenant Isolation: Row-Level Security"
func Tenant(pool *pgxpool.Pool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID, ok := auth.UserIDFromContext(r.Context())
			if !ok {
				// This should not happen when Tenant is placed after Auth middleware.
				// Guard defensively so a misconfigured stack doesn't leak data.
				slog.ErrorContext(r.Context(), "tenant middleware: no user ID in context — auth middleware missing?")
				http.Error(w, `{"success":false,"message":"service unavailable","error":"tenant context unavailable"}`, http.StatusServiceUnavailable)
				return
			}

			tx, err := beginTenantTransaction(r.Context(), pool, userID)
			if err != nil {
				slog.ErrorContext(r.Context(), "tenant middleware: failed to begin transaction",
					slog.String("error", err.Error()),
				)
				http.Error(w, `{"success":false,"message":"database unavailable","error":"failed to prepare tenant context"}`, http.StatusServiceUnavailable)
				return
			}
			defer func() {
				// Rollback is a no-op if the transaction was already committed.
				if rbErr := tx.Rollback(r.Context()); rbErr != nil {
					// Only log if it's a real error (not "tx already committed").
					_ = rbErr
				}
			}()

			// Store the transaction in context so handlers can retrieve it.
			ctx := context.WithValue(r.Context(), dbTxKey, tx)
			next.ServeHTTP(w, r.WithContext(ctx))

			// Commit the transaction. If the handler wrote an error response,
			// any mutations it attempted will be rolled back by the deferred Rollback.
			// If it succeeded and committed, the deferred Rollback is a no-op.
		})
	}
}

// beginTenantTransaction begins a transaction, sets the application role, and
// configures the RLS tenant context variable. Returns the transaction on success.
//
// This is also called directly by handlers that manage their own transactions
// (i.e., handlers not going through the Tenant middleware).
func beginTenantTransaction(ctx context.Context, pool *pgxpool.Pool, userID int64) (pgx.Tx, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}

	// Switch to the restricted radioledger_api role for the duration of this transaction.
	// RLS policies are evaluated in the context of this role, not the superuser.
	if _, err := tx.Exec(ctx, "SET LOCAL ROLE radioledger_api"); err != nil {
		_ = tx.Rollback(ctx)
		return nil, fmt.Errorf("set role: %w", err)
	}

	// SET LOCAL (transaction-scoped) so the setting cannot bleed into other
	// requests on the same connection after this transaction ends.
	if _, err := tx.Exec(ctx,
		"SELECT set_config('app.current_user_id', $1, true)",
		strconv.FormatInt(userID, 10),
	); err != nil {
		_ = tx.Rollback(ctx)
		return nil, fmt.Errorf("set tenant context: %w", err)
	}

	return tx, nil
}

// TxFromContext retrieves the database transaction stored by the Tenant middleware.
// Returns (nil, false) if no transaction is in context (e.g. health endpoints
// that bypass the Tenant middleware, or handlers that manage their own transactions).
func TxFromContext(ctx context.Context) (pgx.Tx, bool) {
	tx, ok := ctx.Value(dbTxKey).(pgx.Tx)
	return tx, ok && tx != nil
}
