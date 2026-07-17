// Package sync — postgres_store.go implements CredentialStore backed by PostgreSQL.
//
// PostgresStore uses AES-256-GCM encryption (via internal/crypto.Keyring) and
// stores ciphertext in the user_service_credentials table. It enforces RLS by
// always setting the tenant context before any query.
package sync

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/FtlC-ian/radioledger/api/internal/crypto"
	"github.com/FtlC-ian/radioledger/api/internal/database/sqlc"
)

// PostgresStore is the production CredentialStore implementation.
// It uses the existing user_service_credentials table and AES-256-GCM keyring.
//
// Construct via NewPostgresStore; the zero value is not usable.
type PostgresStore struct {
	pool    *pgxpool.Pool
	keyring *crypto.Keyring
}

// NewPostgresStore creates a PostgresStore.
// pool and keyring must not be nil.
func NewPostgresStore(pool *pgxpool.Pool, keyring *crypto.Keyring) *PostgresStore {
	return &PostgresStore{pool: pool, keyring: keyring}
}

// Verify that PostgresStore implements CredentialStore at compile time.
var _ CredentialStore = (*PostgresStore)(nil)

// ──────────────────────────────────────────────────────────────────────────────
// CredentialStore implementation
// ──────────────────────────────────────────────────────────────────────────────

// Save encrypts the plaintext credential, optionally verifies it against the
// external service, then upserts the row. Verification failures do not block save.
func (s *PostgresStore) Save(ctx context.Context, p StoreParams) (*StoredCredential, error) {
	verified := p.SkipVerify || !serviceRequiresVerification(p.Service)
	verificationErr := ""

	if !p.SkipVerify && serviceRequiresVerification(p.Service) {
		if err := verifyCredential(ctx, p.Service, p.Plaintext); err != nil {
			verified = false
			verificationErr = err.Error()
		} else {
			verified = true
		}
	}

	ciphertext, keyVersion, err := s.keyring.Encrypt(p.UserID, p.Plaintext)
	if err != nil {
		return nil, fmt.Errorf("postgres_store: encrypt: %w", err)
	}

	tx, err := s.beginTenantTx(ctx, p.UserID)
	if err != nil {
		return nil, fmt.Errorf("postgres_store: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := sqlc.New(tx)
	var expiresAt pgtype.Timestamptz
	if p.ExpiresAt != nil {
		expiresAt = pgtype.Timestamptz{Time: *p.ExpiresAt, Valid: true}
	}

	row, err := q.UpsertCredential(ctx, sqlc.UpsertCredentialParams{
		UserID:         p.UserID,
		Service:        p.Service,
		CredentialType: p.CredentialType,
		Credentials:    ciphertext,
		KeyVersion:     keyVersion,
		ExpiresAt:      expiresAt,
	})
	if err != nil {
		return nil, fmt.Errorf("postgres_store: upsert: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("postgres_store: commit: %w", err)
	}

	var lastVerifiedAt *time.Time
	if verified {
		now := time.Now().UTC()
		if err := s.updateVerifiedAt(ctx, p.UserID, p.Service); err != nil {
			slog.WarnContext(ctx, "postgres_store: update last_verified_at failed after save",
				slog.String("service", p.Service), slog.String("error", err.Error()))
		} else {
			lastVerifiedAt = &now
		}
	} else if err := s.clearVerifiedAt(ctx, p.UserID, p.Service); err != nil {
		slog.WarnContext(ctx, "postgres_store: clear last_verified_at failed after save",
			slog.String("service", p.Service), slog.String("error", err.Error()))
	}

	if lastVerifiedAt == nil && row.LastVerifiedAt.Valid {
		t := row.LastVerifiedAt.Time
		lastVerifiedAt = &t
	}
	if !verified {
		lastVerifiedAt = nil
	}

	sc := &StoredCredential{
		Service:           row.Service,
		CredentialType:    row.CredentialType,
		KeyVersion:        row.KeyVersion,
		IsActive:          row.IsActive,
		LastVerifiedAt:    lastVerifiedAt,
		Verified:          verified,
		VerificationError: verificationErr,
	}
	if row.CreatedAt.Valid {
		sc.CreatedAt = row.CreatedAt.Time
	}
	if row.UpdatedAt.Valid {
		sc.UpdatedAt = row.UpdatedAt.Time
	}

	return sc, nil
}

// Store is a backward-compatible alias for Save.
func (s *PostgresStore) Store(ctx context.Context, p StoreParams) (*StoredCredential, error) {
	return s.Save(ctx, p)
}

// Get decrypts and returns the plaintext credential for (userID, service).
func (s *PostgresStore) Get(ctx context.Context, userID int64, service string) ([]byte, error) {
	tx, err := s.beginTenantTx(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("postgres_store: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := sqlc.New(tx)
	row, err := q.GetCredential(ctx, sqlc.GetCredentialParams{UserID: userID, Service: service})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("postgres_store: no credentials for service %s", service)
		}
		return nil, fmt.Errorf("postgres_store: get credential: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("postgres_store: commit: %w", err)
	}

	plaintext, err := s.keyring.Decrypt(userID, row.KeyVersion, row.Credentials)
	if err != nil {
		return nil, fmt.Errorf("postgres_store: decrypt: %w", err)
	}

	return plaintext, nil
}

// Retrieve is a backward-compatible alias for Get.
func (s *PostgresStore) Retrieve(ctx context.Context, userID int64, service string) ([]byte, error) {
	return s.Get(ctx, userID, service)
}

// Delete removes the credential row for (userID, service).
// Returns (true, nil) if found+deleted, (false, nil) if not found.
func (s *PostgresStore) Delete(ctx context.Context, userID int64, service string) (bool, error) {
	tx, err := s.beginTenantTx(ctx, userID)
	if err != nil {
		return false, fmt.Errorf("postgres_store: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := sqlc.New(tx)
	affected, err := q.DeleteCredential(ctx, sqlc.DeleteCredentialParams{UserID: userID, Service: service})
	if err != nil {
		return false, fmt.Errorf("postgres_store: delete: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("postgres_store: commit: %w", err)
	}

	return affected > 0, nil
}

// Verify re-tests stored credentials against the external service.
// On success, updates last_verified_at.
func (s *PostgresStore) Verify(ctx context.Context, userID int64, service string) error {
	plaintext, err := s.Get(ctx, userID, service)
	if err != nil {
		return fmt.Errorf("postgres_store: retrieve for verify: %w", err)
	}

	if err := verifyCredential(ctx, service, plaintext); err != nil {
		return fmt.Errorf("credential verification failed for %s: %w", service, err)
	}

	// Update last_verified_at.
	if err := s.updateVerifiedAt(ctx, userID, service); err != nil {
		// Log but don't fail — the credential did verify successfully.
		slog.WarnContext(ctx, "postgres_store: failed to update last_verified_at",
			slog.String("service", service), slog.String("error", err.Error()))
	}

	return nil
}

// RotateKey re-encrypts all rows whose key_version is less than the current keyring version.
// Processes in batches of batchSize using SELECT FOR UPDATE SKIP LOCKED to support
// concurrent rotation (e.g. multiple worker processes).
// Returns the total number of rows rotated.
func (s *PostgresStore) RotateKey(ctx context.Context, batchSize int) (int, error) {
	if batchSize <= 0 {
		batchSize = 50
	}

	currentVersion := s.keyring.CurrentVersion()
	total := 0

	for {
		n, err := s.rotateBatch(ctx, currentVersion, batchSize)
		if err != nil {
			return total, fmt.Errorf("postgres_store: rotate batch: %w", err)
		}
		total += n
		if n < batchSize {
			// Fewer rows than batch size means we've processed all outdated rows.
			break
		}
	}

	return total, nil
}

// rotateBatch re-encrypts one batch of outdated credential rows.
// Returns the count of rows processed in this batch.
func (s *PostgresStore) rotateBatch(ctx context.Context, currentVersion int32, batchSize int) (int, error) {
	// We need a superuser or admin tx here (no RLS tenant context — we're rotating
	// credentials across all users). Use a plain connection without SET ROLE.
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Lock a batch of rows that need rotation.
	const selectSQL = `
SELECT id, user_id, credentials, key_version
FROM user_service_credentials
WHERE key_version < $1
LIMIT $2
FOR UPDATE SKIP LOCKED`

	rows, err := tx.Query(ctx, selectSQL, currentVersion, batchSize)
	if err != nil {
		return 0, fmt.Errorf("select for update: %w", err)
	}

	type credRow struct {
		id         int64
		userID     int64
		ciphertext []byte
		keyVersion int32
	}
	var batch []credRow
	for rows.Next() {
		var r credRow
		if err := rows.Scan(&r.id, &r.userID, &r.ciphertext, &r.keyVersion); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scan: %w", err)
		}
		batch = append(batch, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("rows: %w", err)
	}

	if len(batch) == 0 {
		return 0, nil
	}

	const updateSQL = `
UPDATE user_service_credentials
SET credentials = $1, key_version = $2, updated_at = NOW()
WHERE id = $3`

	rotated := 0
	for _, r := range batch {
		// Decrypt with the old key version.
		plaintext, err := s.keyring.Decrypt(r.userID, r.keyVersion, r.ciphertext)
		if err != nil {
			slog.Error("postgres_store: rotate: decrypt failed — skipping row",
				slog.Int64("id", r.id), slog.Int64("user_id", r.userID),
				slog.Int("key_version", int(r.keyVersion)), slog.String("error", err.Error()))
			continue
		}

		// Re-encrypt with the current key version.
		newCiphertext, newVersion, err := s.keyring.Encrypt(r.userID, plaintext)
		if err != nil {
			slog.Error("postgres_store: rotate: encrypt failed — skipping row",
				slog.Int64("id", r.id), slog.String("error", err.Error()))
			continue
		}

		if _, err := tx.Exec(ctx, updateSQL, newCiphertext, newVersion, r.id); err != nil {
			slog.Error("postgres_store: rotate: update failed — skipping row",
				slog.Int64("id", r.id), slog.String("error", err.Error()))
			continue
		}

		rotated++
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}

	return rotated, nil
}

// List returns credential metadata for all of a user's active credentials.
func (s *PostgresStore) List(ctx context.Context, userID int64) ([]CredentialSummary, error) {
	tx, err := s.beginTenantTx(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("postgres_store: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := sqlc.New(tx)
	rows, err := q.ListCredentials(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("postgres_store: list: %w", err)
	}

	summaries := make([]CredentialSummary, 0, len(rows))
	for _, row := range rows {
		cs := CredentialSummary{
			Service:        row.Service,
			CredentialType: row.CredentialType,
			KeyVersion:     row.KeyVersion,
			IsActive:       row.IsActive,
		}
		if row.LastVerifiedAt.Valid {
			t := row.LastVerifiedAt.Time
			cs.LastVerifiedAt = &t
		}
		if row.LastUsedAt.Valid {
			t := row.LastUsedAt.Time
			cs.LastUsedAt = &t
		}
		if row.CreatedAt.Valid {
			cs.CreatedAt = row.CreatedAt.Time
		}
		if row.UpdatedAt.Valid {
			cs.UpdatedAt = row.UpdatedAt.Time
		}
		summaries = append(summaries, cs)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("postgres_store: commit: %w", err)
	}

	return summaries, nil
}

// ListServices is a backward-compatible alias for List.
func (s *PostgresStore) ListServices(ctx context.Context, userID int64) ([]CredentialSummary, error) {
	return s.List(ctx, userID)
}

// ──────────────────────────────────────────────────────────────────────────────
// Helpers
// ──────────────────────────────────────────────────────────────────────────────

// updateVerifiedAt sets last_verified_at = NOW() for (userID, service).
// Uses a plain connection (no tenant tx needed — just updating one column).
func (s *PostgresStore) updateVerifiedAt(ctx context.Context, userID int64, service string) error {
	// Use tenant tx to respect RLS.
	tx, err := s.beginTenantTx(ctx, userID)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	const updateSQL = `
UPDATE user_service_credentials
SET last_verified_at = NOW(), updated_at = NOW()
WHERE user_id = $1 AND service = $2`

	if _, err := tx.Exec(ctx, updateSQL, userID, service); err != nil {
		return fmt.Errorf("update last_verified_at: %w", err)
	}

	return tx.Commit(ctx)
}

func (s *PostgresStore) clearVerifiedAt(ctx context.Context, userID int64, service string) error {
	tx, err := s.beginTenantTx(ctx, userID)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	const clearSQL = `
UPDATE user_service_credentials
SET last_verified_at = NULL, updated_at = NOW()
WHERE user_id = $1 AND service = $2`

	if _, err := tx.Exec(ctx, clearSQL, userID, service); err != nil {
		return fmt.Errorf("clear last_verified_at: %w", err)
	}

	return tx.Commit(ctx)
}

func serviceRequiresVerification(service string) bool {
	switch service {
	case "qrz", "eqsl", "clublog", "sota":
		return true
	default:
		return false
	}
}

// beginTenantTx begins a transaction with the RLS tenant context for userID.
func (s *PostgresStore) beginTenantTx(ctx context.Context, userID int64) (pgx.Tx, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}

	if _, err := tx.Exec(ctx, "SET LOCAL ROLE radioledger_api"); err != nil {
		_ = tx.Rollback(ctx)
		return nil, fmt.Errorf("set role: %w", err)
	}

	if _, err := tx.Exec(ctx, "SELECT set_config('app.current_user_id', $1, true)",
		strconv.FormatInt(userID, 10)); err != nil {
		_ = tx.Rollback(ctx)
		return nil, fmt.Errorf("set tenant context: %w", err)
	}

	return tx, nil
}

// RecordKeyGeneratedAt stores the master key generation timestamp in system_settings.
// Called once on first run when the key is auto-generated.
func RecordKeyGeneratedAt(ctx context.Context, pool *pgxpool.Pool, t time.Time) error {
	const upsertSQL = `
INSERT INTO system_settings (key, value, updated_at)
VALUES ('crypto.master_key_generated_at', $1, NOW())
ON CONFLICT (key) DO NOTHING`

	_, err := pool.Exec(ctx, upsertSQL, t.UTC().Format(time.RFC3339))
	return err
}
