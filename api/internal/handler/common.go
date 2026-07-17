package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/FtlC-ian/radioledger/api/internal/auth"
)

const (
	maxPageSize     = 100
	defaultPageSize = 25
)

type apiResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
	Data    any    `json:"data,omitempty"`
	Error   string `json:"error,omitempty"`
}

func writeSuccess(w http.ResponseWriter, status int, message string, data any) {
	writeJSON(w, status, apiResponse{
		Success: true,
		Message: message,
		Data:    data,
	})
}

func writeFailure(w http.ResponseWriter, status int, message, errMsg string) {
	writeJSON(w, status, apiResponse{
		Success: false,
		Message: message,
		Error:   errMsg,
	})
}

// requestUserID extracts the authenticated user's internal database ID from the
// request context. The Auth middleware must have run before this is called.
//
// Returns (0, error) if no authenticated user is present in the context.
// Handlers should return 401 in this case (though in practice the Auth middleware
// already guards protected routes before the handler is reached).
//
// SECURITY: This function ONLY reads from the authenticated context set by the
// Auth middleware. It does NOT accept user identity from HTTP headers (X-User-ID
// or similar). Any such fallback would be a critical authentication bypass.
func requestUserID(r *http.Request) (int64, error) {
	if userID, ok := auth.UserIDFromContext(r.Context()); ok && userID > 0 {
		return userID, nil
	}

	return 0, fmt.Errorf("unauthenticated request")
}

// beginTenantTx begins a transaction with RLS context set for the given user.
// Called by handlers that manage their own transactions directly (as opposed to
// using the transaction placed in context by the Tenant middleware).
//
// Callers must defer tx.Rollback immediately after calling this function.
// A committed transaction treats Rollback as a no-op, so this is always safe.
func beginTenantTx(ctx context.Context, pool *pgxpool.Pool, userID int64) (pgx.Tx, error) {
	if pool == nil {
		return nil, fmt.Errorf("database pool not initialized")
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}

	if _, err := tx.Exec(ctx, "SET LOCAL ROLE radioledger_api"); err != nil {
		_ = tx.Rollback(ctx)
		return nil, fmt.Errorf("set role: %w", err)
	}

	if _, err := tx.Exec(ctx, "SELECT set_config('app.current_user_id', $1, true)", strconv.FormatInt(userID, 10)); err != nil {
		_ = tx.Rollback(ctx)
		return nil, fmt.Errorf("set tenant context: %w", err)
	}

	return tx, nil
}

type cursor struct {
	DatetimeOn time.Time
	ID         int64
}

func encodeCursor(c cursor) string {
	raw := fmt.Sprintf("%s|%d", c.DatetimeOn.UTC().Format(time.RFC3339Nano), c.ID)
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeCursor(value string) (*cursor, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}

	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return nil, fmt.Errorf("invalid cursor")
	}

	parts := strings.SplitN(string(raw), "|", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid cursor")
	}

	t, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return nil, fmt.Errorf("invalid cursor timestamp")
	}

	id, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid cursor id")
	}

	return &cursor{DatetimeOn: t.UTC(), ID: id}, nil
}

func parsePageSize(raw string) int32 {
	if strings.TrimSpace(raw) == "" {
		return defaultPageSize
	}

	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return defaultPageSize
	}
	if n > maxPageSize {
		n = maxPageSize
	}
	return int32(n)
}

// maxJSONBodySize is the maximum allowed size for JSON request bodies (1 MB).
// This prevents memory exhaustion from oversized payloads. The ADIF import
// endpoint has its own, larger limit via http.MaxBytesReader.
const maxJSONBodySize = 1 * 1024 * 1024

func decodeJSONBody(r *http.Request, dst any) error {
	// Wrap the body in a size-limited reader to prevent memory exhaustion.
	r.Body = http.MaxBytesReader(nil, r.Body, maxJSONBodySize)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	return nil
}
