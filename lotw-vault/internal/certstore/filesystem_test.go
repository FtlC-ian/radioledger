package certstore_test

import (
	"os"
	"testing"
	"time"

	"github.com/FtlC-ian/radioledger/lotw-vault/internal/certstore"
)

func TestFilesystemStore_SaveLoadDelete(t *testing.T) {
	dir := t.TempDir()
	store, err := certstore.NewFilesystemStore(dir)
	if err != nil {
		t.Fatalf("NewFilesystemStore: %v", err)
	}

	entry := &certstore.EncryptedEntry{
		Meta: certstore.CertMeta{
			UserID:    "user123",
			Callsign:  "W1TEST",
			DXCC:      "291",
			NotBefore: time.Now().Add(-time.Hour),
			NotAfter:  time.Now().Add(24 * time.Hour),
		},
		EncryptedKey:  []byte("fake-encrypted-key"),
		Argon2Salt:    []byte("fake-salt"),
		Argon2Time:    2,
		Argon2Memory:  65536,
		Argon2Threads: 4,
	}

	// Save
	if err := store.Save(entry); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if !store.Exists("user123") {
		t.Fatal("Exists should return true after Save")
	}

	// Load
	loaded, err := store.Load("user123")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Meta.Callsign != "W1TEST" {
		t.Errorf("Callsign = %q, want %q", loaded.Meta.Callsign, "W1TEST")
	}
	if string(loaded.EncryptedKey) != "fake-encrypted-key" {
		t.Errorf("EncryptedKey = %q", loaded.EncryptedKey)
	}

	// Delete
	if err := store.Delete("user123"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if store.Exists("user123") {
		t.Fatal("Exists should return false after Delete")
	}
}

func TestFilesystemStore_LoadNotFound(t *testing.T) {
	dir := t.TempDir()
	store, _ := certstore.NewFilesystemStore(dir)

	_, err := store.Load("nonexistent")
	if err == nil {
		t.Fatal("expected error loading nonexistent user")
	}
}

func TestFilesystemStore_InvalidUserID(t *testing.T) {
	dir := t.TempDir()
	store, _ := certstore.NewFilesystemStore(dir)

	_, err := store.Load("../../../etc/passwd")
	if err == nil {
		t.Fatal("expected error for path-traversal user_id")
	}
}

func TestFilesystemStore_PermissionsRestrictive(t *testing.T) {
	dir := t.TempDir()
	store, _ := certstore.NewFilesystemStore(dir)

	entry := &certstore.EncryptedEntry{
		Meta:          certstore.CertMeta{UserID: "permtest"},
		EncryptedKey:  []byte("key"),
		Argon2Salt:    []byte("salt"),
		Argon2Time:    1,
		Argon2Memory:  1024,
		Argon2Threads: 1,
	}
	_ = store.Save(entry)

	// Check that the file is owner-only readable (0600)
	info, err := os.Stat(dir + "/permtest.json")
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("file permissions = %o, want 0600", perm)
	}
}
