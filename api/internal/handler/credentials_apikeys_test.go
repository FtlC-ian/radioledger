package handler_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/FtlC-ian/radioledger/api/internal/config"
	"github.com/FtlC-ian/radioledger/api/internal/crypto"
	"github.com/FtlC-ian/radioledger/api/internal/router"
)

func randomMasterKeyB64(t *testing.T) string {
	t.Helper()
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("generate master key: %v", err)
	}
	return base64.StdEncoding.EncodeToString(b)
}

func setupWithMasterKey(t *testing.T, masterKeyB64 string) (*pgxpool.Pool, http.Handler) {
	t.Helper()
	pool, _ := setupIntegration(t)
	kr, err := crypto.NewKeyringFromBase64(masterKeyB64)
	if err != nil {
		t.Fatalf("NewKeyringFromBase64: %v", err)
	}
	cfg := &config.Config{
		CORSAllowedOrigins: "https://integration.test",
		RateLimitIPRPS:     1000,
		RateLimitIPBurst:   2000,
		AuthMode:           "dev",
		Env:                "development",
	}
	return pool, router.NewWithKeyring(cfg, pool, nil, kr)
}

func doRaw(t *testing.T, h http.Handler, method, path, authToken string, body any) (int, apiEnvelope) {
	t.Helper()
	var reqBody *bytes.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reqBody = bytes.NewReader(raw)
	} else {
		reqBody = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reqBody)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if authToken != "" {
		req.Header.Set("Authorization", "Bearer "+authToken)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var env apiEnvelope
	if strings.Contains(rec.Header().Get("Content-Type"), "application/json") && rec.Body.Len() > 0 {
		_ = json.Unmarshal(rec.Body.Bytes(), &env)
	}
	return rec.Code, env
}

func cleanupCredentials(t *testing.T, pool *pgxpool.Pool, userID int64) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = pool.Exec(ctx, `DELETE FROM user_service_credentials WHERE user_id = $1`, userID)
		_, _ = pool.Exec(ctx, `DELETE FROM api_keys WHERE user_id = $1`, userID)
	})
}

func TestIntegration_CredentialCRUD(t *testing.T) {
	pool, h := setupWithMasterKey(t, randomMasterKeyB64(t))
	user := createTestUser(t, pool, "cred-crud")
	cleanupCredentials(t, pool, user.ID)

	// POST — store QRZ credential
	status, env := doJSON(t, h, http.MethodPost, "/v1/credentials", user.ID, map[string]any{
		"service": "qrz", "credential_type": "api_key", "value": "SUPERSECRET_QRZ",
	})
	if status != http.StatusOK || !env.Success {
		t.Fatalf("POST cred: status=%d success=%v error=%q", status, env.Success, env.Error)
	}
	if strings.Contains(string(env.Data), "SUPERSECRET_QRZ") {
		t.Fatal("response contains plaintext — security violation!")
	}

	type credR struct {
		Service    string `json:"service"`
		KeyVersion int32  `json:"key_version"`
		IsActive   bool   `json:"is_active"`
	}
	var cr credR
	decodeData(t, env.Data, &cr)
	if cr.Service != "qrz" {
		t.Fatalf("expected qrz, got %q", cr.Service)
	}
	if cr.KeyVersion < 1 {
		t.Fatalf("expected key_version >= 1")
	}
	if !cr.IsActive {
		t.Fatal("expected is_active=true")
	}

	// Store second (eqsl)
	status, env = doJSON(t, h, http.MethodPost, "/v1/credentials", user.ID, map[string]any{
		"service": "eqsl", "credential_type": "username_password", "value": "p4ssw0rd",
	})
	if status != http.StatusOK || !env.Success {
		t.Fatalf("POST eqsl: status=%d error=%q", status, env.Error)
	}

	// GET list — no secrets
	status, env = doJSON(t, h, http.MethodGet, "/v1/credentials", user.ID, nil)
	if status != http.StatusOK || !env.Success {
		t.Fatalf("GET creds: status=%d error=%q", status, env.Error)
	}
	type listR struct {
		Items []credR `json:"items"`
		Count int     `json:"count"`
	}
	var lr listR
	decodeData(t, env.Data, &lr)
	if lr.Count != 2 {
		t.Fatalf("expected 2, got %d", lr.Count)
	}
	if strings.Contains(string(env.Data), "SUPERSECRET") || strings.Contains(string(env.Data), "p4ssw0rd") {
		t.Fatal("list contains secrets!")
	}

	// DELETE qrz
	status, env = doJSON(t, h, http.MethodDelete, "/v1/credentials/qrz", user.ID, nil)
	if status != http.StatusOK || !env.Success {
		t.Fatalf("DELETE: status=%d error=%q", status, env.Error)
	}
	_, listEnv := doJSON(t, h, http.MethodGet, "/v1/credentials", user.ID, nil)
	decodeData(t, listEnv.Data, &lr)
	if lr.Count != 1 {
		t.Fatalf("expected 1 after delete, got %d", lr.Count)
	}
	if lr.Items[0].Service != "eqsl" {
		t.Fatalf("expected eqsl, got %q", lr.Items[0].Service)
	}
}

func TestIntegration_CredentialRLSIsolation(t *testing.T) {
	pool, h := setupWithMasterKey(t, randomMasterKeyB64(t))
	userA := createTestUser(t, pool, "cred-rls-a")
	userB := createTestUser(t, pool, "cred-rls-b")
	cleanupCredentials(t, pool, userA.ID)
	cleanupCredentials(t, pool, userB.ID)

	status, env := doJSON(t, h, http.MethodPost, "/v1/credentials", userA.ID, map[string]any{
		"service": "qrz", "credential_type": "api_key", "value": "secretA",
	})
	if status != http.StatusOK || !env.Success {
		t.Fatalf("userA store: status=%d error=%q", status, env.Error)
	}

	// User B lists — must see 0
	_, listEnv := doJSON(t, h, http.MethodGet, "/v1/credentials", userB.ID, nil)
	type lr struct {
		Count int `json:"count"`
	}
	var listed lr
	decodeData(t, listEnv.Data, &listed)
	if listed.Count != 0 {
		t.Fatalf("RLS failure: user B sees %d credentials", listed.Count)
	}

	// User B delete — must fail
	_, delEnv := doJSON(t, h, http.MethodDelete, "/v1/credentials/qrz", userB.ID, nil)
	if delEnv.Success {
		t.Fatal("RLS failure: user B deleted user A's credential")
	}

	// User A's cred still there
	_, listA := doJSON(t, h, http.MethodGet, "/v1/credentials", userA.ID, nil)
	decodeData(t, listA.Data, &listed)
	if listed.Count != 1 {
		t.Fatalf("user A's cred gone after B's delete attempt (count=%d)", listed.Count)
	}
}

func TestIntegration_CredentialUpsert(t *testing.T) {
	pool, h := setupWithMasterKey(t, randomMasterKeyB64(t))
	user := createTestUser(t, pool, "cred-upsert")
	cleanupCredentials(t, pool, user.ID)

	doJSON(t, h, http.MethodPost, "/v1/credentials", user.ID, map[string]any{
		"service": "qrz", "credential_type": "api_key", "value": "old",
	})
	doJSON(t, h, http.MethodPost, "/v1/credentials", user.ID, map[string]any{
		"service": "qrz", "credential_type": "api_key", "value": "new",
	})

	_, listEnv := doJSON(t, h, http.MethodGet, "/v1/credentials", user.ID, nil)
	type lr struct {
		Count int `json:"count"`
	}
	var listed lr
	decodeData(t, listEnv.Data, &listed)
	if listed.Count != 1 {
		t.Fatalf("expected 1 after upsert, got %d", listed.Count)
	}
}

func TestIntegration_CredentialInvalidService(t *testing.T) {
	pool, h := setupWithMasterKey(t, randomMasterKeyB64(t))
	user := createTestUser(t, pool, "cred-bad-svc")
	cleanupCredentials(t, pool, user.ID)

	status, env := doJSON(t, h, http.MethodPost, "/v1/credentials", user.ID, map[string]any{
		"service": "twitter", "credential_type": "api_key", "value": "x",
	})
	if status != http.StatusBadRequest || env.Success {
		t.Fatalf("expected 400 for invalid service, got %d success=%v", status, env.Success)
	}
}

func TestIntegration_APIKeyCRUD(t *testing.T) {
	pool, h := setupIntegration(t)
	user := createTestUser(t, pool, "apikey-crud")
	cleanupCredentials(t, pool, user.ID)

	// POST — generate key
	status, env := doJSON(t, h, http.MethodPost, "/v1/api-keys", user.ID, map[string]any{
		"name": "WSJT-X Station", "scopes": []string{"read", "write"},
	})
	if status != http.StatusCreated || !env.Success {
		t.Fatalf("gen key: status=%d success=%v error=%q", status, env.Success, env.Error)
	}
	assertNoIDKey(t, env.Data)

	type keyR struct {
		Key       string   `json:"key"`
		UUID      string   `json:"uuid"`
		KeyPrefix string   `json:"key_prefix"`
		Name      string   `json:"name"`
		Scopes    []string `json:"scopes"`
	}
	var cr keyR
	decodeData(t, env.Data, &cr)
	if cr.Key == "" {
		t.Fatal("missing key")
	}
	if !strings.HasPrefix(cr.Key, "rl_") {
		t.Fatalf("bad prefix: %q", cr.Key[:5])
	}
	if cr.UUID == "" {
		t.Fatal("missing uuid")
	}
	if len(cr.Key) >= 12 && cr.KeyPrefix != cr.Key[:12] {
		t.Fatalf("key_prefix=%q key[:12]=%q", cr.KeyPrefix, cr.Key[:12])
	}

	// GET list
	status, env = doJSON(t, h, http.MethodGet, "/v1/api-keys", user.ID, nil)
	if status != http.StatusOK || !env.Success {
		t.Fatalf("list keys: status=%d error=%q", status, env.Error)
	}
	if strings.Contains(string(env.Data), "key_hash") {
		t.Fatal("list exposes key_hash!")
	}
	type listR struct {
		Items []struct {
			UUID string `json:"uuid"`
		} `json:"items"`
		Count int `json:"count"`
	}
	var lr listR
	decodeData(t, env.Data, &lr)
	if lr.Count != 1 {
		t.Fatalf("expected 1, got %d", lr.Count)
	}
	if lr.Items[0].UUID != cr.UUID {
		t.Fatalf("uuid mismatch")
	}

	// DELETE
	status, env = doJSON(t, h, http.MethodDelete, "/v1/api-keys/"+cr.UUID, user.ID, nil)
	if status != http.StatusOK || !env.Success {
		t.Fatalf("revoke: status=%d error=%q", status, env.Error)
	}
	_, listEnv := doJSON(t, h, http.MethodGet, "/v1/api-keys", user.ID, nil)
	decodeData(t, listEnv.Data, &lr)
	if lr.Count != 0 {
		t.Fatalf("expected 0 after revoke, got %d", lr.Count)
	}
}

func TestIntegration_APIKeyAuthentication(t *testing.T) {
	pool, h := setupIntegration(t)
	user := createTestUser(t, pool, "apikey-auth")
	cleanupCredentials(t, pool, user.ID)

	status, env := doJSON(t, h, http.MethodPost, "/v1/api-keys", user.ID, map[string]any{
		"name": "Auth Test", "scopes": []string{"read"},
	})
	if status != http.StatusCreated || !env.Success {
		t.Fatalf("gen key: status=%d error=%q", status, env.Error)
	}
	type kr struct {
		Key string `json:"key"`
	}
	var kd kr
	decodeData(t, env.Data, &kd)

	// Use API key to access protected endpoint
	status2, env2 := doRaw(t, h, http.MethodGet, "/v1/logbooks", kd.Key, nil)
	if status2 != http.StatusOK || !env2.Success {
		t.Fatalf("API key auth: status=%d success=%v error=%q", status2, env2.Success, env2.Error)
	}
}

func TestIntegration_RevokedAPIKeyReturns401(t *testing.T) {
	pool, h := setupIntegration(t)
	user := createTestUser(t, pool, "apikey-rev401")
	cleanupCredentials(t, pool, user.ID)

	status, env := doJSON(t, h, http.MethodPost, "/v1/api-keys", user.ID, map[string]any{
		"name": "Revoke Test",
	})
	if status != http.StatusCreated || !env.Success {
		t.Fatalf("gen key: status=%d error=%q", status, env.Error)
	}
	type kr struct{ Key, UUID string }
	var kd kr
	decodeData(t, env.Data, &kd)
	if kd.Key == "" || kd.UUID == "" {
		t.Fatal("missing key/uuid")
	}

	// Pre-revocation: key works
	if s, _ := doRaw(t, h, http.MethodGet, "/v1/logbooks", kd.Key, nil); s != http.StatusOK {
		t.Fatalf("pre-revoke: expected 200, got %d", s)
	}

	// Revoke
	status, env = doJSON(t, h, http.MethodDelete, "/v1/api-keys/"+kd.UUID, user.ID, nil)
	if status != http.StatusOK || !env.Success {
		t.Fatalf("revoke: status=%d error=%q", status, env.Error)
	}

	// Post-revocation: key returns 401
	if s, _ := doRaw(t, h, http.MethodGet, "/v1/logbooks", kd.Key, nil); s != http.StatusUnauthorized {
		t.Fatalf("post-revoke: expected 401, got %d", s)
	}
}

func TestIntegration_APIKeyRLSIsolation(t *testing.T) {
	pool, h := setupIntegration(t)
	userA := createTestUser(t, pool, "ak-rls-a")
	userB := createTestUser(t, pool, "ak-rls-b")
	cleanupCredentials(t, pool, userA.ID)
	cleanupCredentials(t, pool, userB.ID)

	status, env := doJSON(t, h, http.MethodPost, "/v1/api-keys", userA.ID, map[string]any{
		"name": "User A Key",
	})
	if status != http.StatusCreated || !env.Success {
		t.Fatalf("userA create: status=%d error=%q", status, env.Error)
	}
	type kr struct {
		UUID string `json:"uuid"`
	}
	var ak kr
	decodeData(t, env.Data, &ak)

	// User B list — 0
	type lr struct {
		Count int `json:"count"`
	}
	_, lb := doJSON(t, h, http.MethodGet, "/v1/api-keys", userB.ID, nil)
	var listed lr
	decodeData(t, lb.Data, &listed)
	if listed.Count != 0 {
		t.Fatalf("RLS: B sees %d of A's keys", listed.Count)
	}

	// User B revoke A's key — 404
	s, _ := doJSON(t, h, http.MethodDelete, "/v1/api-keys/"+ak.UUID, userB.ID, nil)
	if s != http.StatusNotFound {
		t.Fatalf("RLS: B revoke got %d, want 404", s)
	}

	// A's key still there
	_, la := doJSON(t, h, http.MethodGet, "/v1/api-keys", userA.ID, nil)
	decodeData(t, la.Data, &listed)
	if listed.Count != 1 {
		t.Fatalf("A's key deleted by B (count=%d)", listed.Count)
	}
}

func TestIntegration_APIKeyScopesValidation(t *testing.T) {
	pool, h := setupIntegration(t)
	user := createTestUser(t, pool, "ak-scopes")
	cleanupCredentials(t, pool, user.ID)

	s, env := doJSON(t, h, http.MethodPost, "/v1/api-keys", user.ID, map[string]any{
		"name": "Bad", "scopes": []string{"delete_everything"},
	})
	if s != http.StatusBadRequest || env.Success {
		t.Fatalf("expected 400 invalid scope, got %d success=%v", s, env.Success)
	}

	s, env = doJSON(t, h, http.MethodPost, "/v1/api-keys", user.ID, map[string]any{
		"name": "Good", "scopes": []string{"read", "write", "import", "export"},
	})
	if s != http.StatusCreated || !env.Success {
		t.Fatalf("expected 201 valid scopes, got %d error=%q", s, env.Error)
	}
}

func TestIntegration_APIKeyNameRequired(t *testing.T) {
	pool, h := setupIntegration(t)
	user := createTestUser(t, pool, "ak-noname")
	cleanupCredentials(t, pool, user.ID)

	s, env := doJSON(t, h, http.MethodPost, "/v1/api-keys", user.ID, map[string]any{"name": ""})
	if s != http.StatusBadRequest || env.Success {
		t.Fatalf("expected 400 empty name, got %d success=%v", s, env.Success)
	}
}

func TestIntegration_APIKeyUniqueGeneration(t *testing.T) {
	pool, h := setupIntegration(t)
	user := createTestUser(t, pool, "ak-unique")
	cleanupCredentials(t, pool, user.ID)

	gen := func(name string) string {
		t.Helper()
		s, env := doJSON(t, h, http.MethodPost, "/v1/api-keys", user.ID, map[string]any{"name": name})
		if s != http.StatusCreated || !env.Success {
			t.Fatalf("gen %s: %d %q", name, s, env.Error)
		}
		type kr struct {
			Key string `json:"key"`
		}
		var k kr
		decodeData(t, env.Data, &k)
		return k.Key
	}

	k1, k2 := gen("K1"), gen("K2")
	if k1 == k2 {
		t.Fatal("duplicate keys — RNG failure!")
	}
	t.Logf("unique: %s != %s", k1[:12], k2[:12])
}

func TestIntegration_APIKeyDeleteNonExistent(t *testing.T) {
	pool, h := setupIntegration(t)
	user := createTestUser(t, pool, "ak-nokey")
	cleanupCredentials(t, pool, user.ID)

	s, env := doJSON(t, h, http.MethodDelete, "/v1/api-keys/00000000-0000-0000-0000-000000000000", user.ID, nil)
	if s != http.StatusNotFound || env.Success {
		t.Fatalf("expected 404, got %d success=%v", s, env.Success)
	}
}
