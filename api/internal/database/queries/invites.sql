-- Queries for invite_keys table.

-- name: CountActiveInvitesByCreator :one
SELECT COUNT(*)
FROM invite_keys
WHERE created_by = sqlc.arg(created_by)
  AND revoked_at IS NULL
  AND uses_count < max_uses
  AND (expires_at IS NULL OR expires_at > NOW());

-- name: CreateInviteKey :one
INSERT INTO invite_keys (
    code,
    created_by,
    max_uses,
    expires_at
)
VALUES (
    UPPER(BTRIM(sqlc.arg(code)::text)),
    sqlc.arg(created_by),
    COALESCE(sqlc.narg(max_uses)::integer, 1),
    sqlc.narg(expires_at)
)
RETURNING id, code, created_by, used_by, max_uses, uses_count, expires_at, created_at, revoked_at;

-- name: ListInviteKeysByCreator :many
SELECT id, code, created_by, used_by, max_uses, uses_count, expires_at, created_at, revoked_at
FROM invite_keys
WHERE created_by = sqlc.arg(created_by)
ORDER BY created_at DESC, id DESC;

-- name: RevokeInviteKey :execrows
UPDATE invite_keys
SET revoked_at = NOW()
WHERE id = sqlc.arg(id)
  AND created_by = sqlc.arg(created_by)
  AND revoked_at IS NULL;

-- name: GetActiveInviteByCode :one
SELECT id, code, created_by, used_by, max_uses, uses_count, expires_at, created_at, revoked_at
FROM invite_keys
WHERE code = UPPER(BTRIM(sqlc.arg(code)::text))
  AND revoked_at IS NULL
  AND (expires_at IS NULL OR expires_at > NOW())
  AND uses_count < max_uses;

-- name: ConsumeInviteByCode :one
UPDATE invite_keys
SET uses_count = uses_count + 1,
    used_by = sqlc.arg(used_by)
WHERE code = UPPER(BTRIM(sqlc.arg(code)::text))
  AND revoked_at IS NULL
  AND (expires_at IS NULL OR expires_at > NOW())
  AND uses_count < max_uses
RETURNING id, code, created_by, used_by, max_uses, uses_count, expires_at, created_at, revoked_at;
