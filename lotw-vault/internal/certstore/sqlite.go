package certstore

import (
	"crypto/x509"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite driver
)

const schema = `
CREATE TABLE IF NOT EXISTS certs (
	user_id         TEXT PRIMARY KEY,
	callsign        TEXT NOT NULL,
	encrypted_key   BLOB NOT NULL,
	argon2_salt     BLOB NOT NULL,
	argon2_time     INTEGER NOT NULL DEFAULT 2,
	argon2_memory   INTEGER NOT NULL DEFAULT 65536,
	argon2_threads  INTEGER NOT NULL DEFAULT 4,
	kdf_version     INTEGER NOT NULL DEFAULT 1,
	cert_der        BLOB NOT NULL,
	ca_chain_der    BLOB,
	dxcc            TEXT,
	gridsquare      TEXT,
	cqz             TEXT,
	ituz            TEXT,
	qso_not_before  TEXT,
	qso_not_after   TEXT,
	cert_not_before TEXT,
	cert_not_after  TEXT,
	created_at      TEXT NOT NULL DEFAULT (datetime('now')),
	updated_at      TEXT NOT NULL DEFAULT (datetime('now'))
);
`

// migrations contains SQL statements that are applied once at startup.
// Each statement is run and its error is ignored if it contains "duplicate column name"
// (which SQLite returns when the column already exists — harmless for ADD COLUMN migrations).
var migrations = []string{
	`ALTER TABLE certs ADD COLUMN argon2_time    INTEGER NOT NULL DEFAULT 2`,
	`ALTER TABLE certs ADD COLUMN argon2_memory  INTEGER NOT NULL DEFAULT 65536`,
	`ALTER TABLE certs ADD COLUMN argon2_threads INTEGER NOT NULL DEFAULT 4`,
	`ALTER TABLE certs ADD COLUMN kdf_version    INTEGER NOT NULL DEFAULT 1`,
}

// SQLiteStore stores encrypted certificate entries in a SQLite database.
// It is the recommended backend for production deployments.
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLiteStore opens (or creates) a SQLite database at the given path and
// initialises the schema. The returned store is safe for concurrent use.
func NewSQLiteStore(path string) (*SQLiteStore, error) {
	// Enable WAL mode for better concurrent read performance, and set a
	// reasonable busy timeout so concurrent writers don't instantly error.
	dsn := path + "?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=on"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %s: %w", path, err)
	}

	// One writer at a time keeps things simple and safe.
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("init schema: %w", err)
	}

	// Run column-add migrations for existing databases.
	// Errors from "duplicate column name" are intentionally ignored.
	for _, mig := range migrations {
		if _, err := db.Exec(mig); err != nil {
			if !strings.Contains(err.Error(), "duplicate column name") {
				_ = db.Close()
				return nil, fmt.Errorf("run migration %q: %w", mig, err)
			}
		}
	}

	return &SQLiteStore{db: db}, nil
}

// Close releases the underlying database connection.
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

// Save upserts an encrypted certificate entry into the database.
func (s *SQLiteStore) Save(entry *EncryptedEntry) error {
	if entry.Meta.UserID == "" {
		return errors.New("user_id must not be empty")
	}

	notBefore := entry.Meta.NotBefore.UTC().Format(time.RFC3339)
	notAfter := entry.Meta.NotAfter.UTC().Format(time.RFC3339)

	const q = `
INSERT INTO certs
	(user_id, callsign, encrypted_key, argon2_salt,
	 argon2_time, argon2_memory, argon2_threads, kdf_version,
	 cert_der, ca_chain_der,
	 dxcc, gridsquare, cqz, ituz,
	 qso_not_before, qso_not_after, cert_not_before, cert_not_after,
	 updated_at)
VALUES
	(?, ?, ?, ?,
	 ?, ?, ?, ?,
	 ?, ?,
	 ?, ?, ?, ?,
	 ?, ?, ?, ?,
	 datetime('now'))
ON CONFLICT(user_id) DO UPDATE SET
	callsign        = excluded.callsign,
	encrypted_key   = excluded.encrypted_key,
	argon2_salt     = excluded.argon2_salt,
	argon2_time     = excluded.argon2_time,
	argon2_memory   = excluded.argon2_memory,
	argon2_threads  = excluded.argon2_threads,
	kdf_version     = excluded.kdf_version,
	cert_der        = excluded.cert_der,
	ca_chain_der    = excluded.ca_chain_der,
	dxcc            = excluded.dxcc,
	gridsquare      = excluded.gridsquare,
	cqz             = excluded.cqz,
	ituz            = excluded.ituz,
	qso_not_before  = excluded.qso_not_before,
	qso_not_after   = excluded.qso_not_after,
	cert_not_before = excluded.cert_not_before,
	cert_not_after  = excluded.cert_not_after,
	updated_at      = datetime('now')
`
	_, err := s.db.Exec(q,
		entry.Meta.UserID,
		entry.Meta.Callsign,
		entry.EncryptedKey,
		entry.Argon2Salt,
		entry.Argon2Time,
		entry.Argon2Memory,
		entry.Argon2Threads,
		1, // kdf_version — increment if the KDF algorithm ever changes
		entry.Meta.CertDER,
		nullableBlob(entry.Meta.CAChainDER),
		nullableStr(entry.Meta.DXCC),
		nullableStr(entry.Meta.Gridsquare),
		nullableStr(entry.Meta.CQZ),
		nullableStr(entry.Meta.ITUZ),
		nullableStr(entry.Meta.QSOStart),
		nullableStr(entry.Meta.QSOEnd),
		notBefore,
		notAfter,
	)
	if err != nil {
		return fmt.Errorf("upsert cert: %w", err)
	}
	return nil
}

// Load retrieves a certificate entry by user ID.
func (s *SQLiteStore) Load(userID string) (*EncryptedEntry, error) {
	const q = `
SELECT callsign, encrypted_key, argon2_salt,
       argon2_time, argon2_memory, argon2_threads,
       cert_der, ca_chain_der,
       dxcc, gridsquare, cqz, ituz,
       qso_not_before, qso_not_after, cert_not_before, cert_not_after
FROM certs WHERE user_id = ?
`
	row := s.db.QueryRow(q, userID)

	var (
		callsign                     string
		encryptedKey, argon2Salt     []byte
		argon2Time, argon2Memory     int64
		argon2Threads                int64
		certDER, caChainDER          []byte
		dxcc, gridsquare             sql.NullString
		cqz, ituz                    sql.NullString
		qsoNotBefore, qsoNotAfter    sql.NullString
		certNotBeforeStr             sql.NullString
		certNotAfterStr              sql.NullString
	)

	err := row.Scan(
		&callsign, &encryptedKey, &argon2Salt,
		&argon2Time, &argon2Memory, &argon2Threads,
		&certDER, &caChainDER,
		&dxcc, &gridsquare, &cqz, &ituz,
		&qsoNotBefore, &qsoNotAfter, &certNotBeforeStr, &certNotAfterStr,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("no certificate found for user")
	}
	if err != nil {
		return nil, fmt.Errorf("query cert: %w", err)
	}

	// Parse cert timestamps — fallback to parsing DER if columns somehow missing.
	certNotBefore, certNotAfter, err := parseCertTimes(certNotBeforeStr, certNotAfterStr, certDER)
	if err != nil {
		return nil, fmt.Errorf("parse cert times: %w", err)
	}

	return &EncryptedEntry{
		Meta: CertMeta{
			UserID:     userID,
			Callsign:   callsign,
			DXCC:       dxcc.String,
			Gridsquare: gridsquare.String,
			CQZ:        cqz.String,
			ITUZ:       ituz.String,
			QSOStart:   qsoNotBefore.String,
			QSOEnd:     qsoNotAfter.String,
			NotBefore:  certNotBefore,
			NotAfter:   certNotAfter,
			CertDER:    certDER,
			CAChainDER: caChainDER,
		},
		EncryptedKey:  encryptedKey,
		Argon2Salt:    argon2Salt,
		// Use the stored KDF parameters — NOT hardcoded constants.
		Argon2Time:    uint32(argon2Time),
		Argon2Memory:  uint32(argon2Memory),
		Argon2Threads: uint8(argon2Threads),
	}, nil
}

// Delete removes a user's certificate entry from the database.
func (s *SQLiteStore) Delete(userID string) error {
	res, err := s.db.Exec(`DELETE FROM certs WHERE user_id = ?`, userID)
	if err != nil {
		return fmt.Errorf("delete cert: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("no certificate found for user")
	}
	return nil
}

// Exists reports whether a certificate entry exists for the given user ID.
func (s *SQLiteStore) Exists(userID string) bool {
	var n int
	_ = s.db.QueryRow(`SELECT COUNT(1) FROM certs WHERE user_id = ?`, userID).Scan(&n)
	return n > 0
}

// ── helpers ──────────────────────────────────────────────────────────────────

func nullableStr(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}

func nullableBlob(b []byte) interface{} {
	if len(b) == 0 {
		return nil
	}
	return b
}

// parseCertTimes returns NotBefore/NotAfter, preferring the stored RFC3339 strings
// and falling back to parsing the DER certificate when columns are empty.
func parseCertTimes(notBeforeStr, notAfterStr sql.NullString, certDER []byte) (time.Time, time.Time, error) {
	if notBeforeStr.Valid && notAfterStr.Valid && notBeforeStr.String != "" && notAfterStr.String != "" {
		nb, err := time.Parse(time.RFC3339, notBeforeStr.String)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("parse cert_not_before: %w", err)
		}
		na, err := time.Parse(time.RFC3339, notAfterStr.String)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("parse cert_not_after: %w", err)
		}
		return nb, na, nil
	}

	// Fallback: parse from DER.
	if len(certDER) == 0 {
		return time.Time{}, time.Time{}, errors.New("no cert DER available to derive timestamps")
	}
	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("parse cert DER: %w", err)
	}
	return cert.NotBefore, cert.NotAfter, nil
}
