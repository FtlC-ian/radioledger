package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/FtlC-ian/radioledger/api/internal/auth"
	"github.com/FtlC-ian/radioledger/api/internal/middleware"
	"github.com/FtlC-ian/radioledger/pkg/plan"
)

// ── Helpers ───────────────────────────────────────────────────────────────────

// okHandler is a trivial handler that records whether it was reached.
type okHandler struct{ called bool }

func (h *okHandler) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	h.called = true
	w.WriteHeader(http.StatusCreated)
}

// mockProvider implements plan.Provider for tests.
type mockProvider struct {
	err error
}

func (m *mockProvider) CheckLimit(_ context.Context, _ int64, _ plan.Resource) error {
	return m.err
}

func (m *mockProvider) GetPlan(_ context.Context, _ int64) (*plan.Plan, error) {
	return &plan.Plan{Tier: "free", Limits: plan.Unlimited}, nil
}

// requestWithUser builds a request with an authenticated user in context.
func requestWithUser(userID int64) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("{}"))
	info := auth.UserInfo{UserID: userID}
	r = r.WithContext(auth.ContextWithUserInfo(r.Context(), info))
	return r
}

// ── Tests ─────────────────────────────────────────────────────────────────────

// TestEnforcePlan_AllowsWhenProviderReturnsNil verifies that the middleware
// passes the request through when CheckLimit returns nil.
func TestEnforcePlan_AllowsWhenProviderReturnsNil(t *testing.T) {
	provider := &mockProvider{err: nil}
	inner := &okHandler{}
	handler := middleware.EnforcePlan(provider, plan.ResourceQSOCreate)(inner)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, requestWithUser(1))

	if !inner.called {
		t.Error("expected inner handler to be called, but it was not")
	}
	if rec.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d", rec.Code)
	}
}

// TestEnforcePlan_BlocksWhenLimitHit verifies that the middleware responds 403
// when CheckLimit returns a PlanLimitError.
func TestEnforcePlan_BlocksWhenLimitHit(t *testing.T) {
	limitErr := &plan.PlanLimitError{
		Resource: plan.ResourceQSOCreate,
		Tier:     "free",
		Limit:    500,
		Current:  500,
		Message:  "free tier limit of 500 QSOs reached",
	}
	provider := &mockProvider{err: limitErr}
	inner := &okHandler{}
	handler := middleware.EnforcePlan(provider, plan.ResourceQSOCreate)(inner)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, requestWithUser(1))

	if inner.called {
		t.Error("inner handler should NOT have been called when limit is hit")
	}
	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "plan limit reached") {
		t.Errorf("expected 'plan limit reached' in body, got: %s", body)
	}
}

// TestEnforcePlan_FailsOpenOnUnexpectedError verifies that unexpected (non-plan)
// errors from the provider are logged and the request is allowed through.
func TestEnforcePlan_FailsOpenOnUnexpectedError(t *testing.T) {
	provider := &mockProvider{err: context.DeadlineExceeded}
	inner := &okHandler{}
	handler := middleware.EnforcePlan(provider, plan.ResourceQSOCreate)(inner)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, requestWithUser(1))

	// Should fail open — inner handler should be reached.
	if !inner.called {
		t.Error("expected inner handler to be called on unexpected error (fail open), but it was not")
	}
}

// TestEnforcePlan_BlocksUnauthenticated verifies that requests without an auth
// context are rejected with 401 (defence in depth — auth middleware should have
// blocked these first).
func TestEnforcePlan_BlocksUnauthenticated(t *testing.T) {
	provider := &mockProvider{err: nil}
	inner := &okHandler{}
	handler := middleware.EnforcePlan(provider, plan.ResourceQSOCreate)(inner)

	req := httptest.NewRequest(http.MethodPost, "/", nil) // no auth in context
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if inner.called {
		t.Error("inner handler should NOT have been called for unauthenticated request")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

// TestEnforcePlan_DefaultProviderAlwaysAllows verifies that the DefaultProvider
// (nil pool, self-hosted mode) never blocks any request.
func TestEnforcePlan_DefaultProviderAlwaysAllows(t *testing.T) {
	provider := plan.NewDefaultProvider(nil) // nil pool: CheckLimit doesn't use it
	inner := &okHandler{}

	resources := []plan.Resource{
		plan.ResourceQSOCreate,
		plan.ResourceLogbookCreate,
		plan.ResourceSyncService,
	}

	for _, resource := range resources {
		inner.called = false
		h := middleware.EnforcePlan(provider, resource)(inner)

		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, requestWithUser(99))

		if !inner.called {
			t.Errorf("DefaultProvider: inner handler not called for resource %d", resource)
		}
		if rec.Code != http.StatusCreated {
			t.Errorf("DefaultProvider resource %d: expected 201, got %d", resource, rec.Code)
		}
	}
}
