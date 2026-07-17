package crypto_test

import (
	"bytes"
	"testing"

	vaultcrypto "github.com/FtlC-ian/radioledger/lotw-vault/internal/crypto"
)

func TestEncryptDecryptRoundtrip(t *testing.T) {
	salt, err := vaultcrypto.GenerateSalt()
	if err != nil {
		t.Fatalf("GenerateSalt: %v", err)
	}

	key := vaultcrypto.DeriveKey("test-password", salt)
	plaintext := []byte("this is a secret RSA private key DER blob")

	ciphertext, err := vaultcrypto.Encrypt(key, plaintext)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	if bytes.Equal(ciphertext, plaintext) {
		t.Fatal("ciphertext should not equal plaintext")
	}

	decrypted, err := vaultcrypto.Decrypt(key, ciphertext)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}

	if !bytes.Equal(decrypted, plaintext) {
		t.Errorf("decrypted != plaintext: got %q, want %q", decrypted, plaintext)
	}
}

func TestDecryptWrongKey(t *testing.T) {
	salt, _ := vaultcrypto.GenerateSalt()
	key := vaultcrypto.DeriveKey("correct-password", salt)
	plaintext := []byte("secret data")

	ciphertext, err := vaultcrypto.Encrypt(key, plaintext)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	wrongKey := vaultcrypto.DeriveKey("wrong-password", salt)
	_, err = vaultcrypto.Decrypt(wrongKey, ciphertext)
	if err == nil {
		t.Fatal("expected error decrypting with wrong key, got nil")
	}
}

func TestDeriveKeyDeterministic(t *testing.T) {
	salt := []byte("fixed-salt-for-test-do-not-use-in-prod")
	key1 := vaultcrypto.DeriveKey("password123", salt)
	key2 := vaultcrypto.DeriveKey("password123", salt)
	if !bytes.Equal(key1, key2) {
		t.Error("DeriveKey should be deterministic for same password+salt")
	}
}

func TestDeriveKeyDifferentPasswords(t *testing.T) {
	salt := []byte("fixed-salt-for-test")
	k1 := vaultcrypto.DeriveKey("password1", salt)
	k2 := vaultcrypto.DeriveKey("password2", salt)
	if bytes.Equal(k1, k2) {
		t.Error("different passwords should produce different keys")
	}
}

func TestGenerateSaltUnique(t *testing.T) {
	s1, _ := vaultcrypto.GenerateSalt()
	s2, _ := vaultcrypto.GenerateSalt()
	if bytes.Equal(s1, s2) {
		t.Error("GenerateSalt should produce unique salts")
	}
}
