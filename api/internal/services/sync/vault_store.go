package sync

import (
	"context"
	"errors"
)

// ErrVaultStoreUnimplemented indicates that Vault-backed credential storage is
// not yet available in this build.
var ErrVaultStoreUnimplemented = errors.New("vault credential store is not implemented")

// VaultStore is a future enterprise CredentialStore implementation.
//
// It is intentionally a stub for now, so deployments can compile against a
// pluggable store contract while defaulting to PostgresStore.
type VaultStore struct{}

// NewVaultStore returns a stub VaultStore.
func NewVaultStore() *VaultStore { return &VaultStore{} }

var _ CredentialStore = (*VaultStore)(nil)

func (s *VaultStore) Save(ctx context.Context, p StoreParams) (*StoredCredential, error) {
	return nil, ErrVaultStoreUnimplemented
}

func (s *VaultStore) Get(ctx context.Context, userID int64, service string) ([]byte, error) {
	return nil, ErrVaultStoreUnimplemented
}

func (s *VaultStore) Delete(ctx context.Context, userID int64, service string) (bool, error) {
	return false, ErrVaultStoreUnimplemented
}

func (s *VaultStore) List(ctx context.Context, userID int64) ([]CredentialSummary, error) {
	return nil, ErrVaultStoreUnimplemented
}

func (s *VaultStore) Verify(ctx context.Context, userID int64, service string) error {
	return ErrVaultStoreUnimplemented
}

func (s *VaultStore) RotateKey(ctx context.Context, batchSize int) (int, error) {
	return 0, ErrVaultStoreUnimplemented
}
