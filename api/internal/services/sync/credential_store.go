// Package sync — credential_store.go defines the pluggable CredentialStore interface
// for encrypted credential management.
//
// # Design
//
// CredentialStore is the single abstraction layer between HTTP handlers and
// the underlying secret storage backend. The default implementation (PostgresStore)
// uses the existing AES-256-GCM + HKDF per-user key derivation implemented in
// internal/crypto. An optional VaultStore (not yet implemented) could replace it
// for enterprise deployments without changing any handler code.
//
// # Verification
//
// Save attempts immediate verification for supported services (QRZ, eQSL, Club Log).
// If verification fails, credentials are still persisted but marked unverified
// (`last_verified_at = NULL`). The Verify method re-tests already-stored
// credentials on demand.
//
// # Key Rotation
//
// RotateKey re-encrypts all credential rows that were encrypted with an older key
// version. It operates in small batches to avoid long-running transactions and
// supports concurrent execution (last-write-wins on the row level).
package sync

import (
	"context"
	"time"
)

// CredentialStore is the pluggable interface for encrypted external-service credential management.
//
// Implementors must:
//   - Never log or return plaintext credentials.
//   - Enforce user isolation (RLS or equivalent).
//   - Treat Save as an upsert (one active credential per user+service).
type CredentialStore interface {
	// Save encrypts, persists, and (for supported services) verifies credentials.
	// Verification failure does NOT block saving; instead the row is persisted as
	// unverified (last_verified_at = NULL) and the returned StoredCredential carries
	// Verified=false plus VerificationError.
	// If SkipVerify is true, the external-service check is skipped.
	Save(ctx context.Context, p StoreParams) (*StoredCredential, error)

	// Get decrypts and returns the plaintext credential bytes for (userID, service).
	// The caller must ensure plaintext is not logged or stored.
	Get(ctx context.Context, userID int64, service string) ([]byte, error)

	// Delete removes credentials for (userID, service).
	// Returns (true, nil) if the row was found and deleted, (false, nil) if not found.
	Delete(ctx context.Context, userID int64, service string) (bool, error)

	// List returns credential metadata for all of a user's configured services.
	// Plaintext credentials are never included in the result.
	List(ctx context.Context, userID int64) ([]CredentialSummary, error)

	// Verify re-tests stored credentials against the external service and, on success,
	// updates last_verified_at. Returns an error if verification fails.
	Verify(ctx context.Context, userID int64, service string) error

	// RotateKey re-encrypts all credential rows that use an older key version
	// (i.e., key_version < keyring.CurrentVersion()) in batches of batchSize.
	// Returns the total number of rows rotated.
	// Safe to call concurrently — uses row-level locking (SELECT FOR UPDATE SKIP LOCKED).
	RotateKey(ctx context.Context, batchSize int) (int, error)
}

// StoreParams contains all inputs required to store a credential.
type StoreParams struct {
	UserID         int64
	Service        string
	CredentialType string
	// Plaintext is the raw credential bytes. It must not be logged.
	// The Save implementation encrypts this before writing to the database.
	Plaintext []byte
	// SkipVerify bypasses the external-service verification step.
	// Used internally during key rotation (credentials were already verified when first saved).
	SkipVerify bool
	// ExpiresAt is an optional credential expiry time.
	ExpiresAt *time.Time
}

// StoredCredential is returned after a successful Save call.
// It contains only metadata — plaintext is never returned.
type StoredCredential struct {
	Service           string
	CredentialType    string
	KeyVersion        int32
	IsActive          bool
	LastVerifiedAt    *time.Time
	Verified          bool
	VerificationError string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// CredentialSummary is a metadata-only view of a stored credential.
// Never contains plaintext or ciphertext.
type CredentialSummary struct {
	Service        string
	CredentialType string
	KeyVersion     int32
	IsActive       bool
	LastVerifiedAt *time.Time
	LastUsedAt     *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}
