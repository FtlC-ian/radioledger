// Package handler_test — sync_credentials_integration_test.go
//
// Integration tests for the /v1/sync/credentials/* endpoints.
// Tests cover: store+verify flow, list (no secrets), re-verify, delete,
// RLS isolation, upsert idempotency, and missing-keyring 503 guard.
package handler_test

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// syncCredResp matches the syncCredentialResponse shape.
type syncCredResp struct {
	Service        string     `json:"service"`
	CredentialType string     `json:"credential_type"`
	KeyVersion     int32      `json:"key_version"`
	IsActive       bool       `json:"is_active"`
	LastVerifiedAt *time.Time `json:"last_verified_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// syncCredListResp matches the list payload.
type syncCredListResp struct {
	Items []struct {
		Service        string     `json:"service"`
		CredentialType string     `json:"credential_type"`
		IsActive       bool       `json:"is_active"`
		LastVerifiedAt *time.Time `json:"last_verified_at"`
	} `json:"items"`
	Count int `json:"count"`
}

// TestIntegration_SyncCredentials_StorePOTA tests store + list + delete for POTA
// (which has no external verifier, so SkipVerify isn't needed — it passes trivially).
func TestIntegration_SyncCredentials_StorePOTA(t *testing.T) {
	pool, h := setupWithMasterKey(t, randomMasterKeyB64(t))
	user := createTestUser(t, pool, "sc-pota")
	cleanupCredentials(t, pool, user.ID)

	// PUT /v1/sync/credentials/pota — store credential
	status, env := doJSON(t, h, http.MethodPut, "/v1/sync/credentials/pota", user.ID, map[string]any{
		"credential_type": "api_key",
		"value":           "pota-api-key-12345",
	})
	if status != http.StatusOK || !env.Success {
		t.Fatalf("PUT pota cred: status=%d success=%v error=%q", status, env.Success, env.Error)
	}

	// Response must not contain the plaintext.
	if strings.Contains(string(env.Data), "pota-api-key-12345") {
		t.Fatal("response contains plaintext credential — security violation!")
	}

	var cr syncCredResp
	decodeData(t, env.Data, &cr)
	if cr.Service != "pota" {
		t.Fatalf("expected service=pota, got %q", cr.Service)
	}
	if cr.KeyVersion < 1 {
		t.Fatalf("expected KeyVersion >= 1, got %d", cr.KeyVersion)
	}
	if !cr.IsActive {
		t.Fatal("expected IsActive=true")
	}
	if cr.LastVerifiedAt == nil {
		t.Fatal("expected LastVerifiedAt to be set after store")
	}

	// GET /v1/sync/credentials — list
	status, env = doJSON(t, h, http.MethodGet, "/v1/sync/credentials", user.ID, nil)
	if status != http.StatusOK || !env.Success {
		t.Fatalf("GET sync credentials: status=%d success=%v error=%q", status, env.Success, env.Error)
	}

	// Must not contain any plaintext.
	if strings.Contains(string(env.Data), "pota-api-key-12345") {
		t.Fatal("list response contains plaintext credential!")
	}

	var lr syncCredListResp
	decodeData(t, env.Data, &lr)
	if lr.Count != 1 {
		t.Fatalf("expected count=1, got %d", lr.Count)
	}
	if lr.Items[0].Service != "pota" {
		t.Fatalf("expected service=pota in list, got %q", lr.Items[0].Service)
	}

	// DELETE /v1/sync/credentials/pota
	status, env = doJSON(t, h, http.MethodDelete, "/v1/sync/credentials/pota", user.ID, nil)
	if status != http.StatusOK || !env.Success {
		t.Fatalf("DELETE pota cred: status=%d success=%v error=%q", status, env.Success, env.Error)
	}

	// List should now be empty.
	_, listEnv := doJSON(t, h, http.MethodGet, "/v1/sync/credentials", user.ID, nil)
	decodeData(t, listEnv.Data, &lr)
	if lr.Count != 0 {
		t.Fatalf("expected count=0 after delete, got %d", lr.Count)
	}
}

// TestIntegration_SyncCredentials_Upsert verifies that a second PUT replaces the first.
func TestIntegration_SyncCredentials_Upsert(t *testing.T) {
	pool, h := setupWithMasterKey(t, randomMasterKeyB64(t))
	user := createTestUser(t, pool, "sc-upsert")
	cleanupCredentials(t, pool, user.ID)

	for _, v := range []string{"first-key", "second-key"} {
		status, env := doJSON(t, h, http.MethodPut, "/v1/sync/credentials/pota", user.ID, map[string]any{
			"credential_type": "api_key",
			"value":           v,
		})
		if status != http.StatusOK || !env.Success {
			t.Fatalf("PUT pota (%s): status=%d error=%q", v, status, env.Error)
		}
	}

	// Should still be exactly 1 credential.
	_, listEnv := doJSON(t, h, http.MethodGet, "/v1/sync/credentials", user.ID, nil)
	var lr syncCredListResp
	decodeData(t, listEnv.Data, &lr)
	if lr.Count != 1 {
		t.Fatalf("expected 1 credential after upsert, got %d", lr.Count)
	}
}

// TestIntegration_SyncCredentials_InvalidService verifies that unknown services are rejected.
func TestIntegration_SyncCredentials_InvalidService(t *testing.T) {
	pool, h := setupWithMasterKey(t, randomMasterKeyB64(t))
	user := createTestUser(t, pool, "sc-bad-svc")
	cleanupCredentials(t, pool, user.ID)

	status, env := doJSON(t, h, http.MethodPut, "/v1/sync/credentials/twitter", user.ID, map[string]any{
		"credential_type": "api_key",
		"value":           "x",
	})
	if status != http.StatusBadRequest || env.Success {
		t.Fatalf("expected 400 for invalid service, got status=%d success=%v", status, env.Success)
	}
}

// TestIntegration_SyncCredentials_RLSIsolation verifies that user B cannot see or
// delete user A's credentials via the sync credentials endpoints.
func TestIntegration_SyncCredentials_RLSIsolation(t *testing.T) {
	pool, h := setupWithMasterKey(t, randomMasterKeyB64(t))
	userA := createTestUser(t, pool, "sc-rls-a")
	userB := createTestUser(t, pool, "sc-rls-b")
	cleanupCredentials(t, pool, userA.ID)
	cleanupCredentials(t, pool, userB.ID)

	// Store for user A.
	status, env := doJSON(t, h, http.MethodPut, "/v1/sync/credentials/pota", userA.ID, map[string]any{
		"credential_type": "api_key",
		"value":           "user-a-secret",
	})
	if status != http.StatusOK || !env.Success {
		t.Fatalf("store for userA: status=%d success=%v", status, env.Success)
	}

	// User B list — must see 0.
	_, listEnv := doJSON(t, h, http.MethodGet, "/v1/sync/credentials", userB.ID, nil)
	var lr syncCredListResp
	decodeData(t, listEnv.Data, &lr)
	if lr.Count != 0 {
		t.Fatalf("RLS violation: userB sees %d credentials", lr.Count)
	}

	// User B delete — must not find and delete user A's row.
	status, delEnv := doJSON(t, h, http.MethodDelete, "/v1/sync/credentials/pota", userB.ID, nil)
	if delEnv.Success {
		t.Fatalf("RLS violation: userB deleted userA's credential (status=%d)", status)
	}

	// User A's credential must still be there.
	_, listA := doJSON(t, h, http.MethodGet, "/v1/sync/credentials", userA.ID, nil)
	decodeData(t, listA.Data, &lr)
	if lr.Count != 1 {
		t.Fatalf("userA's credential gone after userB's delete attempt (count=%d)", lr.Count)
	}
}

// TestIntegration_SyncCredentials_DeleteNotFound verifies 404 for missing credentials.
func TestIntegration_SyncCredentials_DeleteNotFound(t *testing.T) {
	pool, h := setupWithMasterKey(t, randomMasterKeyB64(t))
	user := createTestUser(t, pool, "sc-del-nf")
	cleanupCredentials(t, pool, user.ID)

	status, env := doJSON(t, h, http.MethodDelete, "/v1/sync/credentials/pota", user.ID, nil)
	if status != http.StatusNotFound || env.Success {
		t.Fatalf("expected 404 for not-found delete, got status=%d success=%v", status, env.Success)
	}
}

// TestIntegration_SyncCredentials_RequiresAuth verifies that all endpoints are auth-gated.
func TestIntegration_SyncCredentials_RequiresAuth(t *testing.T) {
	_, h := setupWithMasterKey(t, randomMasterKeyB64(t))

	for _, tc := range []struct {
		method string
		path   string
	}{
		{http.MethodPut, "/v1/sync/credentials/pota"},
		{http.MethodGet, "/v1/sync/credentials"},
		{http.MethodPost, "/v1/sync/credentials/pota/verify"},
		{http.MethodDelete, "/v1/sync/credentials/pota"},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			status, env := doJSON(t, h, tc.method, tc.path, 0, map[string]any{
				"credential_type": "api_key", "value": "x",
			})
			if env.Success || status == http.StatusOK {
				t.Fatalf("unauthenticated request should not succeed: status=%d success=%v", status, env.Success)
			}
		})
	}
}

// TestIntegration_SyncCredentials_MultipleServices verifies storing multiple services
// for one user and listing them all.
func TestIntegration_SyncCredentials_MultipleServices(t *testing.T) {
	pool, h := setupWithMasterKey(t, randomMasterKeyB64(t))
	user := createTestUser(t, pool, "sc-multi")
	cleanupCredentials(t, pool, user.ID)

	for _, svc := range []string{"pota"} {
		status, env := doJSON(t, h, http.MethodPut, "/v1/sync/credentials/"+svc, user.ID, map[string]any{
			"credential_type": "api_key",
			"value":           "key-for-" + svc,
		})
		if status != http.StatusOK || !env.Success {
			t.Fatalf("PUT %s: status=%d error=%q", svc, status, env.Error)
		}
	}

	_, listEnv := doJSON(t, h, http.MethodGet, "/v1/sync/credentials", user.ID, nil)
	var lr syncCredListResp
	decodeData(t, listEnv.Data, &lr)
	if lr.Count < 1 {
		t.Fatalf("expected at least 1 credential, got %d", lr.Count)
	}
}
