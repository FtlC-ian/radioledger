package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/FtlC-ian/radioledger/api/internal/middleware"
)

// TestRequestID_GeneratesIDWhenAbsent verifies a request ID is generated when the
// client does not supply one.
func TestRequestID_GeneratesIDWhenAbsent(t *testing.T) {
	handler := middleware.RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := middleware.RequestIDFromContext(r.Context())
		if id == "" {
			t.Error("expected request ID in context, got empty string")
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Header().Get("X-Request-ID") == "" {
		t.Error("expected X-Request-ID header in response, got empty string")
	}
}

// TestRequestID_PropagatesClientID verifies the middleware reuses an existing X-Request-ID.
func TestRequestID_PropagatesClientID(t *testing.T) {
	const clientID = "client-supplied-id-abc123"

	handler := middleware.RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := middleware.RequestIDFromContext(r.Context())
		if id != clientID {
			t.Errorf("expected context ID %q, got %q", clientID, id)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-ID", clientID)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("X-Request-ID"); got != clientID {
		t.Errorf("expected response X-Request-ID %q, got %q", clientID, got)
	}
}

// TestRequestID_Uniqueness verifies that two requests without supplied IDs get different IDs.
func TestRequestID_Uniqueness(t *testing.T) {
	var ids [2]string
	i := 0

	handler := middleware.RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ids[i] = middleware.RequestIDFromContext(r.Context())
		i++
		w.WriteHeader(http.StatusOK)
	}))

	for range 2 {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}

	if ids[0] == "" || ids[1] == "" {
		t.Error("expected non-empty request IDs")
	}
	if ids[0] == ids[1] {
		t.Errorf("expected unique request IDs, both were %q", ids[0])
	}
}
