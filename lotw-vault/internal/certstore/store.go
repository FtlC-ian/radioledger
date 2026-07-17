// Package certstore defines the storage interface for LoTW certificates and
// provides a filesystem-backed and SQLite-backed implementation. The backend
// is intentionally abstracted so that implementations can be substituted
// without touching calling code.
package certstore

import (
	"time"
)

// CertMeta holds public metadata about a callsign certificate.
// Private key material is never stored here — only in the encrypted blob.
type CertMeta struct {
	UserID     string    `json:"user_id"`
	Callsign   string    `json:"callsign"`
	DXCC       string    `json:"dxcc"`
	Gridsquare string    `json:"gridsquare"`
	CQZ        string    `json:"cqz"`
	ITUZ       string    `json:"ituz"`
	QSOStart   string    `json:"qso_start"` // ARRL OID 1.3.6.1.4.1.12348.1.2
	QSOEnd     string    `json:"qso_end"`   // ARRL OID 1.3.6.1.4.1.12348.1.3
	NotBefore  time.Time `json:"not_before"`
	NotAfter   time.Time `json:"not_after"`
	// CertDER is the raw DER-encoded X.509 certificate (NOT the private key).
	CertDER []byte `json:"cert_der"`
	// CAChainDER is the raw DER-encoded CA certificate chain (optional).
	CAChainDER []byte `json:"ca_chain_der,omitempty"`
}

// EncryptedEntry is what actually lives in the store.
type EncryptedEntry struct {
	Meta          CertMeta `json:"meta"`
	EncryptedKey  []byte   `json:"encrypted_key"` // AES-256-GCM(private_key_DER)
	Argon2Salt    []byte   `json:"argon2_salt"`
	Argon2Time    uint32   `json:"argon2_time"`
	Argon2Memory  uint32   `json:"argon2_memory"`
	Argon2Threads uint8    `json:"argon2_threads"`
}

// Store is the storage interface for LoTW certificate entries.
// Implementations must be safe for concurrent use.
type Store interface {
	// Save persists an encrypted certificate entry.
	Save(entry *EncryptedEntry) error

	// Load retrieves a certificate entry by user ID.
	Load(userID string) (*EncryptedEntry, error)

	// Delete removes a certificate entry by user ID.
	Delete(userID string) error

	// Exists reports whether an entry exists for the given user ID.
	Exists(userID string) bool
}
