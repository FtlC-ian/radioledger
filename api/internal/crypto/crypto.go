// Package crypto provides AES-256-GCM encryption for RadioLedger credential storage.
//
// # Design
//
// RadioLedger stores external service credentials (QRZ API keys, eQSL passwords,
// ClubLog API keys) encrypted in the database. Plaintext NEVER enters the database.
//
// The encryption scheme uses per-user derived keys to limit blast radius:
// compromising one user's encrypted data does not expose other users.
//
//	Master Key (MK) — from RADIOLEDGER_MASTER_KEY env var or KMS; never in DB
//	    │
//	    └── HKDF-SHA256(MK, info="user:{user_id}:v{key_version}")
//	            → User Encryption Key (UEK, 32 bytes)
//	                │
//	                └── AES-256-GCM(UEK, random_nonce, plaintext)
//	                        → BYTEA: nonce(12) || ciphertext+tag(16)
//
// # Key Rotation
//
// The key_version column in user_service_credentials tracks which derivation version
// was used for each row. On rotation:
//  1. Bump the current key version in the Keyring.
//  2. A background job re-encrypts all rows user-by-user, writing the new key_version.
//  3. Decrypt always reads the stored key_version and derives the matching key.
//  4. Old versions remain decryptable until re-encrypted, then can be retired.
//
// # Storage Format
//
// Ciphertext is stored as raw BYTEA in PostgreSQL:
//
//	[12 bytes nonce][ciphertext][16 bytes GCM tag]
//
// The nonce is random and unique per encrypted value. AES-256-GCM's GCM tag is
// appended by Go's cipher.AEAD.Seal call (included in the returned ciphertext slice).
//
// References:
//   - ARCHITECTURE.md § "Credential Encryption: AES-256-GCM"
//   - SCHEMA.md § user_service_credentials
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"

	"golang.org/x/crypto/hkdf"
)

const (
	// keySize is the AES-256 key length in bytes.
	keySize = 32

	// nonceSize is the GCM standard nonce length in bytes.
	nonceSize = 12
)

// Keyring holds one or more versioned master keys and the current (latest) version number.
// Multiple versions allow gradual key rotation without immediately re-encrypting all rows.
//
// The zero value is not valid; use NewKeyring.
type Keyring struct {
	// keys maps key_version → 32-byte master key.
	// Version numbers start at 1.
	keys map[int32][]byte

	// currentVersion is the version used for new encryptions.
	currentVersion int32
}

// NewKeyring constructs a Keyring with a single key at version 1.
// masterKey must be exactly 32 bytes.
//
// To add additional key versions for rotation use AddKey.
func NewKeyring(masterKey []byte) (*Keyring, error) {
	if len(masterKey) != keySize {
		return nil, fmt.Errorf("crypto: master key must be %d bytes, got %d", keySize, len(masterKey))
	}

	k := make([]byte, keySize)
	copy(k, masterKey)

	return &Keyring{
		keys:           map[int32][]byte{1: k},
		currentVersion: 1,
	}, nil
}

// NewKeyringFromBase64 constructs a Keyring from a base64-encoded 32-byte master key.
// This is the primary constructor for production use, reading from RADIOLEDGER_MASTER_KEY.
//
// The base64 string must decode to exactly 32 bytes.
func NewKeyringFromBase64(b64 string) (*Keyring, error) {
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		// Also try URL-safe encoding.
		raw, err = base64.RawURLEncoding.DecodeString(b64)
		if err != nil {
			return nil, fmt.Errorf("crypto: master key is not valid base64: %w", err)
		}
	}
	return NewKeyring(raw)
}

// AddKey registers an additional key version. This is used during key rotation to
// allow decryption of rows encrypted with a previous master key while new rows are
// encrypted with the new key.
//
// After calling AddKey(newKey), currentVersion becomes newVersion.
// Decryption of rows with any previously registered version continues to work.
//
// newVersion must be greater than the current version.
// newKey must be exactly 32 bytes.
func (kr *Keyring) AddKey(newVersion int32, newKey []byte) error {
	if newVersion <= kr.currentVersion {
		return fmt.Errorf("crypto: new key version %d must be greater than current version %d",
			newVersion, kr.currentVersion)
	}
	if len(newKey) != keySize {
		return fmt.Errorf("crypto: master key must be %d bytes, got %d", keySize, len(newKey))
	}

	k := make([]byte, keySize)
	copy(k, newKey)
	kr.keys[newVersion] = k
	kr.currentVersion = newVersion
	return nil
}

// CurrentVersion returns the key version used for new encryptions.
func (kr *Keyring) CurrentVersion() int32 {
	return kr.currentVersion
}

// deriveUserKey derives a per-user AES-256 key from the master key for a given
// user ID and key version using HKDF-SHA256.
//
// The HKDF info string is: "user:{userID}:v{keyVersion}"
// This binds the derived key to the specific user and rotation version.
//
// Security property: even if an attacker learns one user's derived key (e.g., from
// an in-memory exploit), they cannot derive other users' keys without the master key.
func (kr *Keyring) deriveUserKey(userID int64, keyVersion int32) ([]byte, error) {
	masterKey, ok := kr.keys[keyVersion]
	if !ok {
		return nil, fmt.Errorf("crypto: unknown key version %d", keyVersion)
	}

	info := fmt.Sprintf("user:%d:v%d", userID, keyVersion)

	// HKDF: extract + expand using SHA-256.
	// salt=nil uses the HKDF default (hash-length zero bytes), which is secure.
	reader := hkdf.New(sha256.New, masterKey, nil, []byte(info))

	derived := make([]byte, keySize)
	if _, err := io.ReadFull(reader, derived); err != nil {
		return nil, fmt.Errorf("crypto: HKDF key derivation failed: %w", err)
	}

	return derived, nil
}

// Encrypt encrypts plaintext for a specific user using the current key version.
//
// Returns the ciphertext as a []byte in the storage format:
//
//	[12-byte random nonce][GCM ciphertext + 16-byte tag]
//
// Also returns the key_version used, which must be stored alongside the ciphertext
// (in the key_version column) so Decrypt can derive the same key.
//
// The plaintext MUST NOT be logged. The caller is responsible for ensuring this.
func (kr *Keyring) Encrypt(userID int64, plaintext []byte) (ciphertext []byte, keyVersion int32, err error) {
	keyVersion = kr.currentVersion

	userKey, err := kr.deriveUserKey(userID, keyVersion)
	if err != nil {
		return nil, 0, err
	}
	defer wipeBytes(userKey)

	block, err := aes.NewCipher(userKey)
	if err != nil {
		return nil, 0, fmt.Errorf("crypto: create AES cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, 0, fmt.Errorf("crypto: create GCM: %w", err)
	}

	// Generate a random nonce. Each plaintext gets a unique nonce — this is
	// critical for GCM security. Never reuse a nonce with the same key.
	nonce := make([]byte, nonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, 0, fmt.Errorf("crypto: generate nonce: %w", err)
	}

	// GCM.Seal appends ciphertext+tag to dst (the nonce here).
	// Result: nonce || ciphertext || tag
	result := gcm.Seal(nonce, nonce, plaintext, nil)

	return result, keyVersion, nil
}

// Decrypt decrypts ciphertext that was produced by Encrypt.
//
// keyVersion must match the version stored alongside the ciphertext in the database.
// The user-specific key is re-derived from the master key for that version.
//
// Returns the plaintext on success. The caller must ensure plaintext is not logged.
func (kr *Keyring) Decrypt(userID int64, keyVersion int32, ciphertext []byte) ([]byte, error) {
	if len(ciphertext) < nonceSize+16 { // nonce + min GCM tag
		return nil, fmt.Errorf("crypto: ciphertext too short")
	}

	userKey, err := kr.deriveUserKey(userID, keyVersion)
	if err != nil {
		return nil, err
	}
	defer wipeBytes(userKey)

	block, err := aes.NewCipher(userKey)
	if err != nil {
		return nil, fmt.Errorf("crypto: create AES cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("crypto: create GCM: %w", err)
	}

	nonce := ciphertext[:nonceSize]
	sealed := ciphertext[nonceSize:]

	plaintext, err := gcm.Open(nil, nonce, sealed, nil)
	if err != nil {
		return nil, fmt.Errorf("crypto: decryption failed (wrong key or corrupted data): %w", err)
	}

	return plaintext, nil
}

// HashAPIKey computes the SHA-256 hex digest of a plaintext API key.
// This is used both when storing a new key (store the hash, not the plaintext)
// and when authenticating (hash the incoming token, look up by hash).
//
// The input token MUST NOT be logged. Only the returned hash is safe to log or store.
func HashAPIKey(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// wipeBytes zeroes a byte slice to reduce the window in which a secret key
// lives in memory. This is a best-effort measure — Go's garbage collector may
// have already created copies of the slice.
func wipeBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

// GenerateMasterKey generates a cryptographically random 32-byte master key
// encoded as base64 (standard encoding). This is used for self-hosted first-run
// auto-generation when RADIOLEDGER_MASTER_KEY is not set.
//
// The returned string can be passed directly to NewKeyringFromBase64.
// NEVER log the returned value — it is the root secret for all credential encryption.
func GenerateMasterKey() (string, error) {
	raw := make([]byte, keySize)
	if _, err := io.ReadFull(rand.Reader, raw); err != nil {
		return "", fmt.Errorf("crypto: generate master key: %w", err)
	}
	return base64.StdEncoding.EncodeToString(raw), nil
}
