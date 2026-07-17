// Package crypto provides key derivation and symmetric encryption for the LoTW Vault.
// Private keys are encrypted at rest with AES-256-GCM using an Argon2id-derived key.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters — tuned for interactive use (balance security vs. latency).
const (
	argon2Time    = 2
	argon2Memory  = 64 * 1024 // 64 MiB
	argon2Threads = 4
	argon2KeyLen  = 32 // AES-256
	saltLen       = 32
)

// DeriveKey derives a 32-byte AES key from the user password and salt using Argon2id
// with the built-in default parameters. Use for encrypting new keys.
func DeriveKey(password string, salt []byte) []byte {
	return argon2.IDKey([]byte(password), salt, argon2Time, argon2Memory, argon2Threads, argon2KeyLen)
}

// DeriveKeyWithParams derives a 32-byte AES key using the supplied Argon2id parameters.
// Use for decrypting existing keys whose parameters were stored alongside the ciphertext.
func DeriveKeyWithParams(password string, salt []byte, t, m uint32, threads uint8) []byte {
	return argon2.IDKey([]byte(password), salt, t, m, threads, argon2KeyLen)
}

// GenerateSalt generates a cryptographically random salt for Argon2.
func GenerateSalt() ([]byte, error) {
	salt := make([]byte, saltLen)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, fmt.Errorf("generate salt: %w", err)
	}
	return salt, nil
}

// Encrypt encrypts plaintext with AES-256-GCM using the provided key.
// Returns the combined nonce+ciphertext blob.
func Encrypt(key, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("new gcm: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}
	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)
	return ciphertext, nil
}

// Decrypt decrypts a nonce+ciphertext blob produced by Encrypt.
func Decrypt(key, blob []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("new gcm: %w", err)
	}
	nonceSize := gcm.NonceSize()
	if len(blob) < nonceSize {
		return nil, errors.New("ciphertext too short")
	}
	nonce, ciphertext := blob[:nonceSize], blob[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}
	return plaintext, nil
}

// ZeroBytes zeroes a byte slice in memory to clear sensitive data.
func ZeroBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
