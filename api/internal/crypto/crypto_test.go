package crypto_test

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"testing"

	"github.com/FtlC-ian/radioledger/api/internal/crypto"
)

// randomKey generates a fresh random 32-byte key, base64-encoded.
func randomKeyB64(t *testing.T) string {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("generate random key: %v", err)
	}
	return base64.StdEncoding.EncodeToString(key)
}

func randomKey(t *testing.T) []byte {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("generate random key: %v", err)
	}
	return key
}

// TestEncryptDecryptRoundTrip verifies that encrypting a plaintext and then decrypting
// the result produces the original plaintext.
func TestEncryptDecryptRoundTrip(t *testing.T) {
	kr, err := crypto.NewKeyringFromBase64(randomKeyB64(t))
	if err != nil {
		t.Fatalf("NewKeyringFromBase64: %v", err)
	}

	userID := int64(42)
	plaintext := []byte("K1ABC:supersecretapikey12345")

	ciphertext, version, err := kr.Encrypt(userID, plaintext)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	if version != 1 {
		t.Fatalf("expected key version 1, got %d", version)
	}

	if len(ciphertext) < 12+16 {
		t.Fatalf("ciphertext too short: %d bytes", len(ciphertext))
	}

	// Ciphertext must not contain plaintext.
	if bytes.Contains(ciphertext, plaintext) {
		t.Fatal("ciphertext contains plaintext — encryption not working")
	}

	recovered, err := kr.Decrypt(userID, version, ciphertext)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}

	if !bytes.Equal(recovered, plaintext) {
		t.Fatalf("decrypted text mismatch: got %q, want %q", recovered, plaintext)
	}
}

// TestEncryptProducesUniqueCiphertexts verifies that encrypting the same plaintext twice
// produces different ciphertexts (due to random nonce). This is a fundamental GCM property.
func TestEncryptProducesUniqueCiphertexts(t *testing.T) {
	kr, err := crypto.NewKeyringFromBase64(randomKeyB64(t))
	if err != nil {
		t.Fatalf("NewKeyringFromBase64: %v", err)
	}

	plaintext := []byte("same-plaintext-every-time")

	ct1, _, err := kr.Encrypt(1, plaintext)
	if err != nil {
		t.Fatalf("Encrypt 1: %v", err)
	}
	ct2, _, err := kr.Encrypt(1, plaintext)
	if err != nil {
		t.Fatalf("Encrypt 2: %v", err)
	}

	if bytes.Equal(ct1, ct2) {
		t.Fatal("two encryptions of the same plaintext produced identical ciphertext — nonce reuse!")
	}
}

// TestDifferentUsersProduceDifferentCiphertexts verifies per-user key derivation:
// encrypting the same plaintext for two different users produces different ciphertexts.
func TestDifferentUsersProduceDifferentCiphertexts(t *testing.T) {
	kr, err := crypto.NewKeyringFromBase64(randomKeyB64(t))
	if err != nil {
		t.Fatalf("NewKeyringFromBase64: %v", err)
	}

	plaintext := []byte("same-api-key-different-users")

	ct1, v1, err := kr.Encrypt(1001, plaintext)
	if err != nil {
		t.Fatalf("Encrypt user 1001: %v", err)
	}
	ct2, v2, err := kr.Encrypt(1002, plaintext)
	if err != nil {
		t.Fatalf("Encrypt user 1002: %v", err)
	}

	if bytes.Equal(ct1, ct2) {
		t.Fatal("different users produced identical ciphertext — per-user key derivation not working")
	}

	// Cross-decrypt must fail: user 1001's ciphertext must not decrypt as user 1002.
	_, err = kr.Decrypt(1002, v1, ct1)
	if err == nil {
		t.Fatal("user 1002 should not be able to decrypt user 1001's ciphertext")
	}

	// Each user can decrypt their own ciphertext.
	if _, err := kr.Decrypt(1001, v1, ct1); err != nil {
		t.Fatalf("user 1001 Decrypt: %v", err)
	}
	if _, err := kr.Decrypt(1002, v2, ct2); err != nil {
		t.Fatalf("user 1002 Decrypt: %v", err)
	}
}

// TestKeyRotation verifies the key rotation scenario:
//  1. Encrypt data with key version A (master key A).
//  2. Add a new master key B at version 2.
//  3. Old data (version 1) still decrypts using the stored key_version.
//  4. New encryptions use version 2 and also decrypt correctly.
func TestKeyRotation(t *testing.T) {
	masterKeyA := randomKey(t)
	kr, err := crypto.NewKeyring(masterKeyA)
	if err != nil {
		t.Fatalf("NewKeyring: %v", err)
	}

	userID := int64(99)
	plaintext := []byte("secret-value-encrypted-with-key-A")

	// Encrypt with key A (version 1).
	ctA, versionA, err := kr.Encrypt(userID, plaintext)
	if err != nil {
		t.Fatalf("Encrypt with A: %v", err)
	}
	if versionA != 1 {
		t.Fatalf("expected version 1 for key A, got %d", versionA)
	}

	// Add a new master key B at version 2.
	masterKeyB := randomKey(t)
	if err := kr.AddKey(2, masterKeyB); err != nil {
		t.Fatalf("AddKey: %v", err)
	}

	if kr.CurrentVersion() != 2 {
		t.Fatalf("expected current version 2, got %d", kr.CurrentVersion())
	}

	// Old data encrypted with version 1 must still decrypt.
	recoveredA, err := kr.Decrypt(userID, versionA, ctA)
	if err != nil {
		t.Fatalf("Decrypt with version A after rotation: %v", err)
	}
	if !bytes.Equal(recoveredA, plaintext) {
		t.Fatalf("decrypted A mismatch after rotation")
	}

	// New encryptions use version 2.
	newPlaintext := []byte("new-secret-encrypted-with-key-B")
	ctB, versionB, err := kr.Encrypt(userID, newPlaintext)
	if err != nil {
		t.Fatalf("Encrypt with B: %v", err)
	}
	if versionB != 2 {
		t.Fatalf("expected version 2 for new encryption, got %d", versionB)
	}

	recoveredB, err := kr.Decrypt(userID, versionB, ctB)
	if err != nil {
		t.Fatalf("Decrypt with version B: %v", err)
	}
	if !bytes.Equal(recoveredB, newPlaintext) {
		t.Fatalf("decrypted B mismatch")
	}
}

// TestDecryptTamperedCiphertextFails verifies that GCM authentication fails
// when the ciphertext is modified (integrity protection).
func TestDecryptTamperedCiphertextFails(t *testing.T) {
	kr, err := crypto.NewKeyringFromBase64(randomKeyB64(t))
	if err != nil {
		t.Fatalf("NewKeyringFromBase64: %v", err)
	}

	userID := int64(1)
	plaintext := []byte("integrity-protected-value")

	ct, version, err := kr.Encrypt(userID, plaintext)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	// Flip a byte in the middle of the ciphertext.
	tampered := make([]byte, len(ct))
	copy(tampered, ct)
	tampered[len(tampered)/2] ^= 0xFF

	_, err = kr.Decrypt(userID, version, tampered)
	if err == nil {
		t.Fatal("expected decryption of tampered ciphertext to fail, but it succeeded")
	}
}

// TestHashAPIKey verifies that the same input produces the same hash,
// and different inputs produce different hashes.
func TestHashAPIKey(t *testing.T) {
	key := "rl_TestKeyForHashing1234567890"

	hash1 := crypto.HashAPIKey(key)
	hash2 := crypto.HashAPIKey(key)

	if hash1 != hash2 {
		t.Fatalf("same input produced different hashes: %q vs %q", hash1, hash2)
	}
	if len(hash1) != 64 {
		t.Fatalf("expected 64-char hex SHA-256 hash, got len=%d: %q", len(hash1), hash1)
	}

	otherHash := crypto.HashAPIKey("rl_DifferentKey1234567890abcde")
	if hash1 == otherHash {
		t.Fatal("different inputs produced the same hash — collision detected")
	}
}

// TestNewKeyringRequires32Bytes verifies that invalid key lengths are rejected.
func TestNewKeyringRequires32Bytes(t *testing.T) {
	_, err := crypto.NewKeyring(make([]byte, 16)) // too short
	if err == nil {
		t.Fatal("expected error for 16-byte key, got nil")
	}

	_, err = crypto.NewKeyring(make([]byte, 64)) // too long
	if err == nil {
		t.Fatal("expected error for 64-byte key, got nil")
	}

	_, err = crypto.NewKeyring(make([]byte, 32)) // exactly right
	if err != nil {
		t.Fatalf("expected no error for 32-byte key, got: %v", err)
	}
}

// TestNewKeyringFromBase64InvalidInput verifies that non-base64 input is rejected.
func TestNewKeyringFromBase64InvalidInput(t *testing.T) {
	_, err := crypto.NewKeyringFromBase64("not-valid-base64!!!")
	if err == nil {
		t.Fatal("expected error for invalid base64, got nil")
	}

	// Valid base64 but wrong length.
	shortKey := base64.StdEncoding.EncodeToString(make([]byte, 16))
	_, err = crypto.NewKeyringFromBase64(shortKey)
	if err == nil {
		t.Fatal("expected error for base64-encoded 16-byte key, got nil")
	}
}

// TestEmptyPlaintext verifies that empty plaintext can be encrypted/decrypted.
func TestEmptyPlaintext(t *testing.T) {
	kr, err := crypto.NewKeyringFromBase64(randomKeyB64(t))
	if err != nil {
		t.Fatalf("NewKeyringFromBase64: %v", err)
	}

	ct, version, err := kr.Encrypt(1, []byte{})
	if err != nil {
		t.Fatalf("Encrypt empty: %v", err)
	}

	recovered, err := kr.Decrypt(1, version, ct)
	if err != nil {
		t.Fatalf("Decrypt empty: %v", err)
	}

	if len(recovered) != 0 {
		t.Fatalf("expected empty plaintext, got %q", recovered)
	}
}

// TestAddKeyVersionMustIncrease verifies that AddKey rejects lower or equal versions.
func TestAddKeyVersionMustIncrease(t *testing.T) {
	kr, err := crypto.NewKeyring(randomKey(t))
	if err != nil {
		t.Fatalf("NewKeyring: %v", err)
	}

	// Same version (1) should fail.
	if err := kr.AddKey(1, randomKey(t)); err == nil {
		t.Fatal("expected error when adding same version, got nil")
	}

	// Lower version should fail.
	if err := kr.AddKey(0, randomKey(t)); err == nil {
		t.Fatal("expected error when adding lower version, got nil")
	}

	// Higher version should succeed.
	if err := kr.AddKey(2, randomKey(t)); err != nil {
		t.Fatalf("expected AddKey(2) to succeed: %v", err)
	}
}

// TestGenerateMasterKey verifies that auto-generated master keys are valid,
// unique, and usable for encryption/decryption.
func TestGenerateMasterKey(t *testing.T) {
	k1, err := crypto.GenerateMasterKey()
	if err != nil {
		t.Fatalf("GenerateMasterKey: %v", err)
	}
	if k1 == "" {
		t.Fatal("GenerateMasterKey returned empty string")
	}

	k2, err := crypto.GenerateMasterKey()
	if err != nil {
		t.Fatalf("GenerateMasterKey #2: %v", err)
	}
	if k1 == k2 {
		t.Fatal("two GenerateMasterKey calls returned identical keys — RNG failure!")
	}

	// Generated key must be usable as a keyring.
	kr, err := crypto.NewKeyringFromBase64(k1)
	if err != nil {
		t.Fatalf("NewKeyringFromBase64 with generated key: %v", err)
	}
	ct, ver, err := kr.Encrypt(1, []byte("test-payload"))
	if err != nil {
		t.Fatalf("Encrypt with generated key: %v", err)
	}
	pt, err := kr.Decrypt(1, ver, ct)
	if err != nil {
		t.Fatalf("Decrypt with generated key: %v", err)
	}
	if string(pt) != "test-payload" {
		t.Fatalf("roundtrip mismatch: got %q", pt)
	}
}
