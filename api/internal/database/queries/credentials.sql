-- Queries for user_service_credentials (encrypted external service credentials).
-- Plaintext is NEVER stored or logged here. All encryption/decryption happens in
-- the Go crypto layer before calling these queries.
--
-- Schema reference: docs/SCHEMA.md § user_service_credentials
-- Encryption spec: docs/ARCHITECTURE.md § "Credential Encryption: AES-256-GCM"

-- name: UpsertCredential :one
-- Insert or update an encrypted credential for a (user_id, service) pair.
-- ON CONFLICT updates the ciphertext, key_version, and metadata.
-- The UNIQUE(user_id, service) constraint ensures one active credential per service per user.
INSERT INTO user_service_credentials (
    user_id,
    service,
    credential_type,
    credentials,
    key_version,
    expires_at,
    is_active,
    updated_at
)
VALUES (
    $1,  -- user_id
    $2,  -- service
    $3,  -- credential_type
    $4,  -- credentials (AES-256-GCM ciphertext: nonce || ciphertext || tag)
    $5,  -- key_version
    $6,  -- expires_at (NULL = no expiry)
    TRUE,
    NOW()
)
ON CONFLICT (user_id, service) DO UPDATE SET
    credential_type = EXCLUDED.credential_type,
    credentials     = EXCLUDED.credentials,
    key_version     = EXCLUDED.key_version,
    expires_at      = EXCLUDED.expires_at,
    is_active       = TRUE,
    updated_at      = NOW()
RETURNING id, user_id, service, credential_type, key_version, expires_at, last_used_at, last_verified_at, is_active, created_at, updated_at;

-- name: GetCredential :one
-- Retrieve the encrypted credential blob for a specific (user, service).
-- RLS enforces that only the owning user can retrieve their own credentials.
-- Callers must decrypt the credentials blob before using it.
SELECT id, user_id, service, credential_type, credentials, key_version, expires_at, last_used_at, last_verified_at, is_active, created_at, updated_at
FROM user_service_credentials
WHERE user_id = $1
  AND service = $2
  AND is_active = TRUE;

-- name: ListCredentials :many
-- List credentials for a user — service names and metadata ONLY.
-- The encrypted credentials blob is intentionally excluded from this query.
-- Callers see which services are configured, but cannot retrieve the secrets.
SELECT id, user_id, service, credential_type, key_version, expires_at, last_used_at, last_verified_at, is_active, created_at, updated_at
FROM user_service_credentials
WHERE user_id = $1
  AND is_active = TRUE
ORDER BY service ASC;

-- name: DeleteCredential :execrows
-- Hard-delete the credential row.
-- Encrypted ciphertext is worthless without the master key, so there is no
-- audit value in retaining it after a user removes a service integration.
DELETE FROM user_service_credentials
WHERE user_id = $1
  AND service = $2;

-- name: UpdateCredentialLastUsed :exec
-- Record when a credential was last used for a sync operation.
-- Called by sync workers when they successfully use a stored credential.
UPDATE user_service_credentials
SET last_used_at = NOW()
WHERE user_id = $1
  AND service = $2;
