// Package sync — postgres_store_test.go: integration tests for PostgresStore.
//
// These tests require a running PostgreSQL instance with the RadioLedger schema applied.
// They require RADIOLEDGER_TEST_DATABASE_URL; without it they skip rather than
// guessing a developer-specific local database URL.
//
// Tests cover:
//   - Store (upsert, key version recorded)
//   - Retrieve (decrypt round-trip)
//   - Delete (found / not-found)
//   - ListServices (metadata only, no ciphertext)
//   - RotateKey (re-encrypt to new key version)
//   - RLS isolation (user A cannot see user B's credentials)
package sync

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/FtlC-ian/radioledger/api/internal/crypto"
)

// ──────────────────────────────────────────────────────────────────────────────
// Test infrastructure
// ──────────────────────────────────────────────────────────────────────────────

func setupStoreTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	dbURL := os.Getenv("RADIOLEDGER_TEST_DATABASE_URL")
	if strings.TrimSpace(dbURL) == "" {
		t.Skip("RADIOLEDGER_TEST_DATABASE_URL is required for PostgresStore integration tests")
	}

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Skipf("skipping postgres_store integration tests: cannot create pool: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("skipping postgres_store integration tests: cannot connect to db: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func randomTestKey(t *testing.T) []byte {
	t.Helper()
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return b
}

func randomTestKeyB64(t *testing.T) string {
	t.Helper()
	return base64.StdEncoding.EncodeToString(randomTestKey(t))
}

func newTestKeyring(t *testing.T) *crypto.Keyring {
	t.Helper()
	kr, err := crypto.NewKeyringFromBase64(randomTestKeyB64(t))
	if err != nil {
		t.Fatalf("NewKeyringFromBase64: %v", err)
	}
	return kr
}

// createStoreTestUser inserts a minimal user row and returns its ID.
// Cleans up the user (and cascade-deletes credentials) on test completion.
func createStoreTestUser(t *testing.T, pool *pgxpool.Pool, label string) int64 {
	t.Helper()
	email := fmt.Sprintf("store_test_%s_%d@example.test", label, time.Now().UnixNano())
	var id int64
	err := pool.QueryRow(context.Background(), `
		INSERT INTO users (email, timezone, display_name)
		VALUES ($1, 'UTC', $2)
		RETURNING id
	`, email, "StoreTest "+label).Scan(&id)
	if err != nil {
		t.Fatalf("create test user: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = pool.Exec(ctx, `DELETE FROM user_service_credentials WHERE user_id = $1`, id)
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, id)
	})
	return id
}

// ──────────────────────────────────────────────────────────────────────────────
// Tests
// ──────────────────────────────────────────────────────────────────────────────

func TestPostgresStore_StoreAndRetrieve(t *testing.T) {
	pool := setupStoreTestDB(t)
	kr := newTestKeyring(t)
	store := NewPostgresStore(pool, kr)
	userID := createStoreTestUser(t, pool, "store-retrieve")

	plaintext := []byte("mySecretAPIKey123")

	sc, err := store.Store(context.Background(), StoreParams{
		UserID:         userID,
		Service:        "pota", // pota has no external verifier
		CredentialType: "api_key",
		Plaintext:      plaintext,
		SkipVerify:     true,
	})
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	if sc.Service != "pota" {
		t.Fatalf("expected service=pota, got %q", sc.Service)
	}
	if sc.KeyVersion < 1 {
		t.Fatalf("expected KeyVersion >= 1, got %d", sc.KeyVersion)
	}
	if !sc.IsActive {
		t.Fatal("expected IsActive=true")
	}

	// Retrieve must decrypt back to the original plaintext.
	got, err := store.Retrieve(context.Background(), userID, "pota")
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if string(got) != string(plaintext) {
		t.Fatalf("plaintext mismatch: got %q, want %q", got, plaintext)
	}
}

func TestPostgresStore_StoreUpsert(t *testing.T) {
	pool := setupStoreTestDB(t)
	kr := newTestKeyring(t)
	store := NewPostgresStore(pool, kr)
	userID := createStoreTestUser(t, pool, "upsert")

	for i, v := range []string{"first", "second"} {
		_, err := store.Store(context.Background(), StoreParams{
			UserID:         userID,
			Service:        "pota",
			CredentialType: "api_key",
			Plaintext:      []byte(v),
			SkipVerify:     true,
		})
		if err != nil {
			t.Fatalf("Store #%d: %v", i, err)
		}
	}

	// Only one row should exist.
	summaries, err := store.ListServices(context.Background(), userID)
	if err != nil {
		t.Fatalf("ListServices: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected 1 credential after upsert, got %d", len(summaries))
	}

	// The stored value must be the last one written.
	got, err := store.Retrieve(context.Background(), userID, "pota")
	if err != nil {
		t.Fatalf("Retrieve after upsert: %v", err)
	}
	if string(got) != "second" {
		t.Fatalf("expected 'second', got %q", got)
	}
}

func TestPostgresStore_Delete(t *testing.T) {
	pool := setupStoreTestDB(t)
	kr := newTestKeyring(t)
	store := NewPostgresStore(pool, kr)
	userID := createStoreTestUser(t, pool, "delete")

	_, err := store.Store(context.Background(), StoreParams{
		UserID:         userID,
		Service:        "pota",
		CredentialType: "api_key",
		Plaintext:      []byte("to-be-deleted"),
		SkipVerify:     true,
	})
	if err != nil {
		t.Fatalf("Store: %v", err)
	}

	// Delete — found.
	found, err := store.Delete(context.Background(), userID, "pota")
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if !found {
		t.Fatal("expected found=true on first delete")
	}

	// Delete again — not found.
	found, err = store.Delete(context.Background(), userID, "pota")
	if err != nil {
		t.Fatalf("Delete (2nd): %v", err)
	}
	if found {
		t.Fatal("expected found=false on second delete")
	}
}

func TestPostgresStore_ListServicesMetadataOnly(t *testing.T) {
	pool := setupStoreTestDB(t)
	kr := newTestKeyring(t)
	store := NewPostgresStore(pool, kr)
	userID := createStoreTestUser(t, pool, "list-meta")

	for _, svc := range []string{"pota", "eqsl"} {
		_, err := store.Store(context.Background(), StoreParams{
			UserID:         userID,
			Service:        svc,
			CredentialType: "api_key",
			Plaintext:      []byte("secret-" + svc),
			SkipVerify:     true,
		})
		if err != nil {
			t.Fatalf("Store %s: %v", svc, err)
		}
	}

	summaries, err := store.ListServices(context.Background(), userID)
	if err != nil {
		t.Fatalf("ListServices: %v", err)
	}
	if len(summaries) != 2 {
		t.Fatalf("expected 2 summaries, got %d", len(summaries))
	}

	// Verify no plaintext in the summary struct (it's metadata only).
	for _, s := range summaries {
		if s.Service == "" {
			t.Error("summary has empty service name")
		}
		if s.KeyVersion < 1 {
			t.Errorf("summary %s has KeyVersion < 1: %d", s.Service, s.KeyVersion)
		}
	}
}

func TestPostgresStore_RLSIsolation(t *testing.T) {
	pool := setupStoreTestDB(t)
	kr := newTestKeyring(t)
	store := NewPostgresStore(pool, kr)
	userA := createStoreTestUser(t, pool, "rls-a")
	userB := createStoreTestUser(t, pool, "rls-b")

	_, err := store.Store(context.Background(), StoreParams{
		UserID:         userA,
		Service:        "pota",
		CredentialType: "api_key",
		Plaintext:      []byte("user-a-secret"),
		SkipVerify:     true,
	})
	if err != nil {
		t.Fatalf("Store for userA: %v", err)
	}

	// UserB should see 0 credentials.
	summaries, err := store.ListServices(context.Background(), userB)
	if err != nil {
		t.Fatalf("ListServices for userB: %v", err)
	}
	if len(summaries) != 0 {
		t.Fatalf("RLS violation: userB sees %d credentials belonging to userA", len(summaries))
	}

	// UserB delete should not affect userA's data.
	found, err := store.Delete(context.Background(), userB, "pota")
	if err != nil {
		t.Fatalf("Delete by userB: %v", err)
	}
	if found {
		t.Fatal("RLS violation: userB deleted userA's credential")
	}

	// UserA's credential must still be there.
	summaries, err = store.ListServices(context.Background(), userA)
	if err != nil {
		t.Fatalf("ListServices for userA after userB delete: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("userA's credential gone after userB's delete attempt (count=%d)", len(summaries))
	}
}

func TestPostgresStore_RotateKey(t *testing.T) {
	pool := setupStoreTestDB(t)

	// Start with key version 1.
	masterKeyA := randomTestKey(t)
	kr, err := crypto.NewKeyring(masterKeyA)
	if err != nil {
		t.Fatalf("NewKeyring: %v", err)
	}
	store := NewPostgresStore(pool, kr)
	userID := createStoreTestUser(t, pool, "rotate")

	plaintext := []byte("rotation-test-secret")

	sc, err := store.Store(context.Background(), StoreParams{
		UserID:         userID,
		Service:        "pota",
		CredentialType: "api_key",
		Plaintext:      plaintext,
		SkipVerify:     true,
	})
	if err != nil {
		t.Fatalf("Store with v1: %v", err)
	}
	if sc.KeyVersion != 1 {
		t.Fatalf("expected key_version=1, got %d", sc.KeyVersion)
	}

	// Add key version 2.
	masterKeyB := randomTestKey(t)
	if err := kr.AddKey(2, masterKeyB); err != nil {
		t.Fatalf("AddKey v2: %v", err)
	}
	if kr.CurrentVersion() != 2 {
		t.Fatalf("expected current version 2, got %d", kr.CurrentVersion())
	}

	// Rotate — should process the one v1 row.
	rotated, err := store.RotateKey(context.Background(), 10)
	if err != nil {
		t.Fatalf("RotateKey: %v", err)
	}
	if rotated < 1 {
		t.Fatalf("expected at least 1 row rotated, got %d", rotated)
	}

	// After rotation, Retrieve must still return the original plaintext.
	got, err := store.Retrieve(context.Background(), userID, "pota")
	if err != nil {
		t.Fatalf("Retrieve after rotation: %v", err)
	}
	if string(got) != string(plaintext) {
		t.Fatalf("plaintext mismatch after rotation: got %q, want %q", got, plaintext)
	}

	// Verify the stored key_version was bumped to 2.
	var keyVersion int32
	err = pool.QueryRow(context.Background(),
		`SELECT key_version FROM user_service_credentials WHERE user_id = $1 AND service = 'pota'`,
		userID).Scan(&keyVersion)
	if err != nil {
		t.Fatalf("query key_version: %v", err)
	}
	if keyVersion != 2 {
		t.Fatalf("expected key_version=2 after rotation, got %d", keyVersion)
	}

	// Second RotateKey should process 0 rows (nothing left at v1).
	rotated2, err := store.RotateKey(context.Background(), 10)
	if err != nil {
		t.Fatalf("RotateKey #2: %v", err)
	}
	if rotated2 != 0 {
		t.Fatalf("expected 0 rows on second rotation, got %d", rotated2)
	}
}

func TestPostgresStore_RetrieveNotFound(t *testing.T) {
	pool := setupStoreTestDB(t)
	kr := newTestKeyring(t)
	store := NewPostgresStore(pool, kr)
	userID := createStoreTestUser(t, pool, "not-found")

	_, err := store.Retrieve(context.Background(), userID, "pota")
	if err == nil {
		t.Fatal("expected error for missing credential, got nil")
	}
}
