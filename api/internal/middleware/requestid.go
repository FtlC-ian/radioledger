// Package middleware provides HTTP middleware for the RadioLedger API server.
package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
)

// contextKey is an unexported type for context keys used in this package.
// Prevents key collisions with other packages using context.
type contextKey int

const (
	requestIDKey contextKey = iota
	loggerKey
	requestMetaKey
)

type requestMeta struct {
	requestID  string
	userID     int64
	durationMS int64
}

// RequestID is an HTTP middleware that ensures every request has an X-Request-ID header.
// If the incoming request already carries an X-Request-ID it is reused; otherwise a new
// random 16-byte hex ID is generated.
//
// The request ID is:
//   - Written to the response header (X-Request-ID) so callers can correlate client/server logs.
//   - Stored in the request context for downstream middleware and handlers.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			id = generateRequestID()
		}

		// Echo back to the client so they can correlate their logs.
		w.Header().Set("X-Request-ID", id)

		meta := &requestMeta{requestID: id}
		ctx := context.WithValue(r.Context(), requestIDKey, id)
		ctx = context.WithValue(ctx, requestMetaKey, meta)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequestIDFromContext retrieves the request ID stored by RequestID middleware.
// Returns an empty string if no ID is in the context (e.g. in tests that bypass middleware).
func RequestIDFromContext(ctx context.Context) string {
	if meta, ok := ctx.Value(requestMetaKey).(*requestMeta); ok && meta != nil {
		return meta.requestID
	}
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}

// SetUserID stores the authenticated user ID in request context metadata.
// It is safe to call multiple times during request processing.
func SetUserID(ctx context.Context, userID int64) {
	if meta, ok := ctx.Value(requestMetaKey).(*requestMeta); ok && meta != nil {
		meta.userID = userID
	}
}

// UserIDFromContext returns the user ID set in request metadata.
func UserIDFromContext(ctx context.Context) (int64, bool) {
	if meta, ok := ctx.Value(requestMetaKey).(*requestMeta); ok && meta != nil && meta.userID > 0 {
		return meta.userID, true
	}
	return 0, false
}

// SetDurationMS stores request duration (in milliseconds) in context metadata.
func SetDurationMS(ctx context.Context, durationMS int64) {
	if meta, ok := ctx.Value(requestMetaKey).(*requestMeta); ok && meta != nil {
		meta.durationMS = durationMS
	}
}

// DurationMSFromContext returns request duration metadata when present.
func DurationMSFromContext(ctx context.Context) (int64, bool) {
	if meta, ok := ctx.Value(requestMetaKey).(*requestMeta); ok && meta != nil {
		return meta.durationMS, true
	}
	return 0, false
}

// generateRequestID returns a cryptographically random 16-byte hex string.
func generateRequestID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// Should never happen; rand.Read only fails if the OS entropy pool is broken.
		panic("requestid: failed to generate random ID: " + err.Error())
	}
	return hex.EncodeToString(b)
}
