package handler_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestAuth_UnauthenticatedRequestsReturn401 verifies that all /v1/* protected
// routes reject requests without a bearer token with HTTP 401.
func TestAuth_UnauthenticatedRequestsReturn401(t *testing.T) {
	_, h := setupIntegration(t)

	protectedRoutes := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/v1/auth/me"},
		{http.MethodPatch, "/v1/auth/profile"},
		{http.MethodGet, "/v1/logbooks"},
		{http.MethodPost, "/v1/logbooks"},
		{http.MethodGet, "/v1/logbooks/default"},
		{http.MethodGet, "/v1/logbooks/00000000-0000-0000-0000-000000000001"},
		{http.MethodGet, "/v1/logbooks/00000000-0000-0000-0000-000000000001/qsos"},
		{http.MethodPost, "/v1/logbooks/00000000-0000-0000-0000-000000000001/qsos"},
		{http.MethodGet, "/v1/import/00000000-0000-0000-0000-000000000001"},
	}

	for _, tc := range protectedRoutes {
		tc := tc // capture range variable
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			// userID=0 → doJSON sends no Authorization header
			status, env := doJSON(t, h, tc.method, tc.path, 0, nil)
			if status != http.StatusUnauthorized {
				t.Errorf("expected 401 for unauthenticated %s %s, got %d (success=%v error=%q)",
					tc.method, tc.path, status, env.Success, env.Error)
			}
		})
	}
}

// TestAuth_HealthEndpointsArePublic ensures /health and /ready never require auth.
func TestAuth_HealthEndpointsArePublic(t *testing.T) {
	_, h := setupIntegration(t)

	for _, path := range []string{"/health", "/ready"} {
		path := path // capture range variable
		t.Run(path, func(t *testing.T) {
			status, _ := doJSON(t, h, http.MethodGet, path, 0, nil)
			if status == http.StatusUnauthorized {
				t.Errorf("%s returned 401 — health endpoints must be public", path)
			}
		})
	}
}

// TestAuth_InvalidTokenReturns401 verifies that malformed or unknown tokens are rejected.
func TestAuth_InvalidTokenReturns401(t *testing.T) {
	_, h := setupIntegration(t)

	badTokens := []struct {
		name  string
		token string
	}{
		{"garbage", "garbage"},
		{"empty-id", "dev-user-"},
		{"non-numeric-id", "dev-user-abc"},
		{"zero-id", "dev-user-0"},
		{"nonexistent-user", "dev-user-999999999999"},
		{"no-prefix", "12345"},
	}

	for _, tc := range badTokens {
		tc := tc // capture range variable
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/v1/logbooks", nil)
			req.Header.Set("Authorization", "Bearer "+tc.token)

			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Errorf("expected 401 for bad token %q, got %d", tc.token, rec.Code)
			}
		})
	}
}

// TestAuth_NoAuthHeaderReturns401 verifies that requests with no Authorization header get 401.
func TestAuth_NoAuthHeaderReturns401(t *testing.T) {
	_, h := setupIntegration(t)

	req := httptest.NewRequest(http.MethodGet, "/v1/logbooks", nil)
	// No Authorization header
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for missing Authorization header, got %d", rec.Code)
	}
}

// TestAuth_LocalRegistrationAndLogin tests the local-mode register and login endpoints.
func TestAuth_LocalRegistrationAndLogin(t *testing.T) {
	pool, h := setupIntegration(t)

	email := fmt.Sprintf("auth_test_%d@example.test", time.Now().UnixNano())
	password := "hunter2secure!"

	// Register a new user.
	status, env := doJSON(t, h, http.MethodPost, "/v1/auth/register", 0, map[string]any{
		"email":        email,
		"password":     password,
		"display_name": "Auth Test User",
	})
	if status != http.StatusCreated || !env.Success {
		t.Fatalf("register failed: status=%d success=%v error=%q", status, env.Success, env.Error)
	}

	var reg struct {
		Token string `json:"token"`
		User  struct {
			UUID  string `json:"uuid"`
			Email string `json:"email"`
		} `json:"user"`
	}
	decodeData(t, env.Data, &reg)

	if reg.Token == "" {
		t.Fatal("expected non-empty token from register")
	}
	if reg.User.Email != email {
		t.Fatalf("expected email %s, got %s", email, reg.User.Email)
	}

	// The returned token is a JWT — use it directly in an Authorization header.
	// Verify it works for /v1/auth/me.
	{
		req := httptest.NewRequest(http.MethodGet, "/v1/auth/me", nil)
		req.Header.Set("Authorization", "Bearer "+reg.Token)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("me with JWT token failed: status=%d body=%s", rec.Code, rec.Body.String())
		}
	}

	// Retrieve user ID via /v1/auth/me so we can use doJSON for subsequent calls.
	var userID int64
	{
		var meProfile struct {
			UUID string `json:"uuid"`
		}
		req := httptest.NewRequest(http.MethodGet, "/v1/auth/me", nil)
		req.Header.Set("Authorization", "Bearer "+reg.Token)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		var meEnv apiEnvelope
		if err := json.Unmarshal(rec.Body.Bytes(), &meEnv); err != nil {
			t.Fatalf("decode me response: %v", err)
		}
		decodeData(t, meEnv.Data, &meProfile)

		// Look up the internal user ID from the database using the UUID.
		err := pool.QueryRow(context.Background(),
			`SELECT id FROM users WHERE uuid = $1`, meProfile.UUID,
		).Scan(&userID)
		if err != nil {
			t.Fatalf("lookup user ID by UUID: %v", err)
		}
	}

	// Verify user has a default logbook created automatically on registration.
	status, env = doJSON(t, h, http.MethodGet, "/v1/logbooks/default", userID, nil)
	if status != http.StatusOK || !env.Success {
		t.Fatalf("default logbook should exist after registration: status=%d success=%v error=%q",
			status, env.Success, env.Error)
	}

	// Login with correct credentials should succeed and return a JWT token.
	status, env = doJSON(t, h, http.MethodPost, "/v1/auth/login", 0, map[string]any{
		"email":    email,
		"password": password,
	})
	if status != http.StatusOK || !env.Success {
		t.Fatalf("login failed: status=%d success=%v error=%q", status, env.Success, env.Error)
	}

	var login struct {
		Token string `json:"token"`
	}
	decodeData(t, env.Data, &login)

	if login.Token == "" {
		t.Error("expected non-empty token from login")
	}

	// Login with wrong password should fail with 401.
	status, _ = doJSON(t, h, http.MethodPost, "/v1/auth/login", 0, map[string]any{
		"email":    email,
		"password": "wrongpassword!",
	})
	if status != http.StatusUnauthorized {
		t.Errorf("expected 401 for wrong password, got %d", status)
	}

	// Register with a short password should fail with 400.
	status, _ = doJSON(t, h, http.MethodPost, "/v1/auth/register", 0, map[string]any{
		"email":    fmt.Sprintf("short_%d@example.test", time.Now().UnixNano()),
		"password": "short",
	})
	if status != http.StatusBadRequest {
		t.Errorf("expected 400 for short password, got %d", status)
	}

	// Duplicate registration should fail with 409 Conflict.
	status, _ = doJSON(t, h, http.MethodPost, "/v1/auth/register", 0, map[string]any{
		"email":    email,
		"password": "anotherpassword!",
	})
	if status != http.StatusConflict {
		t.Errorf("expected 409 Conflict for duplicate registration, got %d", status)
	}

	// Cleanup the registered user.
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = pool.Exec(ctx, `DELETE FROM qsos WHERE logbook_id IN (SELECT id FROM logbooks WHERE user_id = $1)`, userID)
		_, _ = pool.Exec(ctx, `DELETE FROM logbooks WHERE user_id = $1`, userID)
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID)
	})
}

// TestAuth_ChangePassword tests the POST /v1/auth/change-password endpoint.
func TestAuth_ChangePassword(t *testing.T) {
	pool, h := setupIntegration(t)

	email := fmt.Sprintf("changepw_%d@example.test", time.Now().UnixNano())
	password := "initial-password-99"
	newPassword := "updated-password-99"

	// Register to get a user.
	status, env := doJSON(t, h, http.MethodPost, "/v1/auth/register", 0, map[string]any{
		"email":    email,
		"password": password,
	})
	if status != http.StatusCreated || !env.Success {
		t.Fatalf("register failed: status=%d success=%v error=%q", status, env.Success, env.Error)
	}

	var reg struct {
		Token string `json:"token"`
		User  struct {
			UUID string `json:"uuid"`
		} `json:"user"`
	}
	decodeData(t, env.Data, &reg)

	// Look up the internal user ID.
	var userID int64
	if err := pool.QueryRow(context.Background(),
		`SELECT id FROM users WHERE uuid = $1`, reg.User.UUID,
	).Scan(&userID); err != nil {
		t.Fatalf("lookup user ID: %v", err)
	}

	// change-password with correct old password.
	status, env = doJSON(t, h, http.MethodPost, "/v1/auth/change-password", userID, map[string]any{
		"old_password": password,
		"new_password": newPassword,
	})
	if status != http.StatusOK || !env.Success {
		t.Fatalf("change-password failed: status=%d success=%v error=%q", status, env.Success, env.Error)
	}

	// Old password should no longer work.
	status, _ = doJSON(t, h, http.MethodPost, "/v1/auth/login", 0, map[string]any{
		"email":    email,
		"password": password,
	})
	if status != http.StatusUnauthorized {
		t.Errorf("expected 401 after password change with old password, got %d", status)
	}

	// New password should work.
	status, env = doJSON(t, h, http.MethodPost, "/v1/auth/login", 0, map[string]any{
		"email":    email,
		"password": newPassword,
	})
	if status != http.StatusOK || !env.Success {
		t.Fatalf("login with new password failed: status=%d success=%v error=%q", status, env.Success, env.Error)
	}

	// change-password with wrong old password should return 401.
	status, _ = doJSON(t, h, http.MethodPost, "/v1/auth/change-password", userID, map[string]any{
		"old_password": "wrongoldpassword!",
		"new_password": "someotherpassword!",
	})
	if status != http.StatusUnauthorized {
		t.Errorf("expected 401 for wrong old password, got %d", status)
	}

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = pool.Exec(ctx, `DELETE FROM logbooks WHERE user_id = $1`, userID)
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID)
	})
}

// TestAuth_UserCanOnlySeeOwnData verifies RLS isolation between two authenticated users.
func TestAuth_UserCanOnlySeeOwnData(t *testing.T) {
	pool, h := setupIntegration(t)
	_ = pool

	userA := createTestUser(t, pool, "rls-userA")
	userB := createTestUser(t, pool, "rls-userB")

	// User A creates a logbook.
	logbookA := createLogbookViaAPI(t, h, userA.ID, "User A Log", true)

	// User A creates a QSO in their logbook.
	createQSOViaAPI(t, h, userA.ID, logbookA, map[string]any{
		"callsign":    "W1XYZ",
		"band":        "20m",
		"mode":        "CW",
		"datetime_on": time.Now().UTC().Format(time.RFC3339),
	})

	// User B should not see user A's logbook (RLS returns not-found, not forbidden).
	status, env := doJSON(t, h, http.MethodGet, "/v1/logbooks/"+logbookA, userB.ID, nil)
	if status == http.StatusOK && env.Success {
		t.Errorf("user B should not be able to fetch user A's logbook %s", logbookA)
	}

	// User B's logbook list should be empty (no logbooks of their own).
	status, env = doJSON(t, h, http.MethodGet, "/v1/logbooks", userB.ID, nil)
	if status != http.StatusOK || !env.Success {
		t.Fatalf("list logbooks for user B failed: status=%d success=%v", status, env.Success)
	}
	var listed listLogbooksPayload
	decodeData(t, env.Data, &listed)
	if len(listed.Items) != 0 {
		t.Errorf("expected user B to have 0 logbooks, got %d", len(listed.Items))
	}

	// User A's logbook list should show exactly their 1 logbook.
	status, env = doJSON(t, h, http.MethodGet, "/v1/logbooks", userA.ID, nil)
	if status != http.StatusOK || !env.Success {
		t.Fatalf("list logbooks for user A failed: status=%d success=%v", status, env.Success)
	}
	decodeData(t, env.Data, &listed)
	if len(listed.Items) != 1 {
		t.Errorf("expected user A to have 1 logbook, got %d", len(listed.Items))
	}

	// User B should see 0 QSOs in user A's logbook (RLS enforces this).
	status, env = doJSON(t, h, http.MethodGet, "/v1/logbooks/"+logbookA+"/qsos", userB.ID, nil)
	if status == http.StatusOK && env.Success {
		var qsos listQSOsPayload
		decodeData(t, env.Data, &qsos)
		if len(qsos.Items) != 0 {
			t.Errorf("user B should see 0 QSOs in user A's logbook, got %d", len(qsos.Items))
		}
	}
}

// TestAuth_RegistrationCreatesDefaultLogbook ensures new users get a default logbook on registration.
func TestAuth_RegistrationCreatesDefaultLogbook(t *testing.T) {
	pool, h := setupIntegration(t)

	email := fmt.Sprintf("logbook_auto_%d@example.test", time.Now().UnixNano())

	status, env := doJSON(t, h, http.MethodPost, "/v1/auth/register", 0, map[string]any{
		"email":    email,
		"password": "securepw123",
	})
	if status != http.StatusCreated || !env.Success {
		t.Fatalf("register failed: status=%d success=%v error=%q", status, env.Success, env.Error)
	}

	var reg struct {
		User struct {
			UUID string `json:"uuid"`
		} `json:"user"`
	}
	decodeData(t, env.Data, &reg)

	// Look up the internal user ID from the database using the UUID.
	var userID int64
	if err := pool.QueryRow(context.Background(),
		`SELECT id FROM users WHERE uuid = $1`, reg.User.UUID,
	).Scan(&userID); err != nil {
		t.Fatalf("lookup user ID by UUID: %v", err)
	}

	// Default logbook must exist immediately after registration.
	status, env = doJSON(t, h, http.MethodGet, "/v1/logbooks/default", userID, nil)
	if status != http.StatusOK || !env.Success {
		t.Fatalf("default logbook missing after registration: status=%d success=%v error=%q",
			status, env.Success, env.Error)
	}

	var lb logbookPayload
	decodeData(t, env.Data, &lb)
	if !lb.IsDefault {
		t.Error("expected is_default=true on auto-created logbook")
	}

	// Cleanup the registered user.
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = pool.Exec(ctx, `DELETE FROM qsos WHERE logbook_id IN (SELECT id FROM logbooks WHERE user_id = $1)`, userID)
		_, _ = pool.Exec(ctx, `DELETE FROM logbooks WHERE user_id = $1`, userID)
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID)
	})
}

// TestAuth_MeReturnsUserProfile verifies GET /v1/auth/me returns correct user data.
func TestAuth_MeReturnsUserProfile(t *testing.T) {
	pool, h := setupIntegration(t)

	user := createTestUser(t, pool, "me-test")
	if _, err := pool.Exec(context.Background(), `
		UPDATE users
		SET callsign = 'N0ME', grid_square = 'EM35FX'
		WHERE id = $1
	`, user.ID); err != nil {
		t.Fatalf("seed user profile: %v", err)
	}

	status, env := doJSON(t, h, http.MethodGet, "/v1/auth/me", user.ID, nil)
	if status != http.StatusOK || !env.Success {
		t.Fatalf("GET /v1/auth/me failed: status=%d success=%v error=%q", status, env.Success, env.Error)
	}

	var profile struct {
		UUID       string  `json:"uuid"`
		Email      string  `json:"email"`
		Callsign   *string `json:"callsign"`
		GridSquare *string `json:"grid_square"`
	}
	decodeData(t, env.Data, &profile)

	if profile.UUID != user.UUID.String() {
		t.Errorf("expected UUID %s, got %s", user.UUID.String(), profile.UUID)
	}
	if profile.Callsign == nil || *profile.Callsign != "N0ME" {
		t.Fatalf("expected callsign N0ME, got %#v", profile.Callsign)
	}
	if profile.GridSquare == nil || *profile.GridSquare != "EM35FX" {
		t.Fatalf("expected grid_square EM35FX, got %#v", profile.GridSquare)
	}
}

func TestAuth_UpdateProfileSuccess(t *testing.T) {
	pool, h := setupIntegration(t)
	user := createTestUser(t, pool, "profile-success")

	status, env := doJSON(t, h, http.MethodPatch, "/v1/auth/profile", user.ID, map[string]any{
		"callsign":    "n0new",
		"grid_square": "em35fx",
	})
	if status != http.StatusOK || !env.Success {
		t.Fatalf("PATCH /v1/auth/profile failed: status=%d success=%v error=%q", status, env.Success, env.Error)
	}

	var profile struct {
		Callsign   *string `json:"callsign"`
		GridSquare *string `json:"grid_square"`
	}
	decodeData(t, env.Data, &profile)

	if profile.Callsign == nil || *profile.Callsign != "N0NEW" {
		t.Fatalf("expected callsign N0NEW, got %#v", profile.Callsign)
	}
	if profile.GridSquare == nil || *profile.GridSquare != "EM35FX" {
		t.Fatalf("expected grid_square EM35FX, got %#v", profile.GridSquare)
	}

	var storedCallsign, storedGrid *string
	if err := pool.QueryRow(context.Background(), `
		SELECT callsign, grid_square FROM users WHERE id = $1
	`, user.ID).Scan(&storedCallsign, &storedGrid); err != nil {
		t.Fatalf("load updated user: %v", err)
	}
	if storedCallsign == nil || *storedCallsign != "N0NEW" {
		t.Fatalf("expected stored callsign N0NEW, got %#v", storedCallsign)
	}
	if storedGrid == nil || *storedGrid != "EM35FX" {
		t.Fatalf("expected stored grid_square EM35FX, got %#v", storedGrid)
	}
}

func TestAuth_UpdateProfileRejectsDuplicateCallsign(t *testing.T) {
	pool, h := setupIntegration(t)
	owner := createTestUser(t, pool, "profile-owner")
	other := createTestUser(t, pool, "profile-other")

	status, env := doJSON(t, h, http.MethodPatch, "/v1/auth/profile", owner.ID, map[string]any{
		"callsign": "K1ABC",
	})
	if status != http.StatusOK || !env.Success {
		t.Fatalf("seed owner callsign failed: status=%d success=%v error=%q", status, env.Success, env.Error)
	}

	status, env = doJSON(t, h, http.MethodPatch, "/v1/auth/profile", other.ID, map[string]any{
		"callsign": "K1ABC",
	})
	if status != http.StatusConflict {
		t.Fatalf("expected 409 for duplicate callsign, got %d (success=%v error=%q)", status, env.Success, env.Error)
	}
}

func TestAuth_UpdateProfileRejectsInvalidCallsignFormat(t *testing.T) {
	pool, h := setupIntegration(t)
	user := createTestUser(t, pool, "profile-bad-call")

	status, env := doJSON(t, h, http.MethodPatch, "/v1/auth/profile", user.ID, map[string]any{
		"callsign": "12",
	})
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid callsign, got %d (success=%v error=%q)", status, env.Success, env.Error)
	}
}

func TestAuth_UpdateProfileRejectsInvalidGridSquareFormat(t *testing.T) {
	pool, h := setupIntegration(t)
	user := createTestUser(t, pool, "profile-bad-grid")

	status, env := doJSON(t, h, http.MethodPatch, "/v1/auth/profile", user.ID, map[string]any{
		"callsign":    "K1GRID",
		"grid_square": "ZZ99",
	})
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid grid square, got %d (success=%v error=%q)", status, env.Success, env.Error)
	}
}
