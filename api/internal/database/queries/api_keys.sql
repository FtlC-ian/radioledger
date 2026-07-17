-- Queries for api_keys table.
-- API keys use a show-once pattern: the plaintext key is returned once at creation,
-- only the SHA-256 hash is ever stored. Authentication hashes the incoming bearer
-- token and compares against key_hash.
--
-- Schema reference: docs/SCHEMA.md § api_keys
-- Security spec: docs/ARCHITECTURE.md § "Token architecture" (API Keys row)

-- name: CreateAPIKey :one
-- Insert a new API key record. The plaintext key has already been generated and
-- hashed by the application layer before this query is called.
-- key_hash = hex(SHA-256(full_plaintext_key))
-- key_prefix = first 8 chars of full_plaintext_key (for display identification)
INSERT INTO api_keys (
    user_id,
    name,
    key_hash,
    key_prefix,
    scopes,
    expires_at
)
VALUES (
    $1,  -- user_id
    $2,  -- name (user-visible label)
    $3,  -- key_hash (SHA-256 hex of full key)
    $4,  -- key_prefix (first 8 chars of plaintext key, for display)
    $5,  -- scopes (TEXT[])
    $6   -- expires_at (NULL = no expiry)
)
RETURNING id, uuid, user_id, name, key_hash, key_prefix, scopes, allowed_ips, expires_at, last_used_at, last_used_ip, revoked_at, created_at;

-- name: ListAPIKeys :many
-- List API keys for a user — never returns key_hash (the secret hash).
-- Returns prefix, name, scopes, timestamps for display.
-- Revoked keys are excluded by default (revoked_at IS NULL).
SELECT uuid, user_id, name, key_prefix, scopes, expires_at, last_used_at, revoked_at, created_at
FROM api_keys
WHERE user_id = $1
  AND revoked_at IS NULL
ORDER BY created_at DESC;

-- name: GetAPIKeyByHash :one
-- Look up an API key by its SHA-256 hash for authentication.
-- Called by the auth middleware when it receives a "Bearer rl_..." token.
-- Returns the full row including user_id (needed to set RLS context).
-- Filters out revoked and expired keys.
SELECT id, uuid, user_id, name, key_hash, key_prefix, scopes, allowed_ips, expires_at, last_used_at, last_used_ip, revoked_at, created_at
FROM api_keys
WHERE key_hash = $1
  AND revoked_at IS NULL
  AND (expires_at IS NULL OR expires_at > NOW());

-- name: RevokeAPIKey :execrows
-- Revoke an API key by UUID. Sets revoked_at to the current time.
-- RLS ensures only the owning user can revoke their own keys.
UPDATE api_keys
SET revoked_at = NOW()
WHERE uuid = $1
  AND user_id = $2
  AND revoked_at IS NULL;

-- name: UpdateAPIKeyLastUsed :exec
-- Record the last-used timestamp and source IP for an API key.
-- Called after every successful API key authentication.
-- Uses a direct id (internal PK) for efficiency — id is already known from GetAPIKeyByHash.
UPDATE api_keys
SET last_used_at = NOW(),
    last_used_ip = $2
WHERE id = $1;
