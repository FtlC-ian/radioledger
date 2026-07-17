package certstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sync"
)

var safeIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_\-]+$`)

// FilesystemStore stores encrypted certificate entries as JSON files on disk.
// Each user gets a single file: <dataDir>/<userID>.json
//
// The files contain only the encrypted private key blob (never plaintext), along
// with the Argon2 salt and public certificate metadata.
type FilesystemStore struct {
	dataDir string
	mu      sync.RWMutex
}

// NewFilesystemStore creates (or opens) a filesystem store at the given directory.
func NewFilesystemStore(dataDir string) (*FilesystemStore, error) {
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return nil, fmt.Errorf("create data dir %s: %w", dataDir, err)
	}
	return &FilesystemStore{dataDir: dataDir}, nil
}

func (s *FilesystemStore) path(userID string) (string, error) {
	if !safeIDPattern.MatchString(userID) {
		return "", fmt.Errorf("invalid user_id %q: must match [a-zA-Z0-9_-]+", userID)
	}
	return filepath.Join(s.dataDir, userID+".json"), nil
}

// Save writes an entry to disk atomically (write-to-temp, then rename).
func (s *FilesystemStore) Save(entry *EncryptedEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	p, err := s.path(entry.Meta.UserID)
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal entry: %w", err)
	}

	// Atomic write: write to temp file then rename.
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := os.Rename(tmp, p); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

// Load reads and deserializes an entry from disk.
func (s *FilesystemStore) Load(userID string) (*EncryptedEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	p, err := s.path(userID)
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("no certificate found for user %q", userID)
		}
		return nil, fmt.Errorf("read entry: %w", err)
	}

	var entry EncryptedEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, fmt.Errorf("unmarshal entry: %w", err)
	}
	return &entry, nil
}

// Delete removes a user's certificate entry from disk.
func (s *FilesystemStore) Delete(userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	p, err := s.path(userID)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("no certificate found for user %q", userID)
		}
		return fmt.Errorf("delete entry: %w", err)
	}
	return nil
}

// Exists reports whether a certificate entry file exists for the given user.
func (s *FilesystemStore) Exists(userID string) bool {
	p, err := s.path(userID)
	if err != nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, err = os.Stat(p)
	return err == nil
}
