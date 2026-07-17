-- name: CreateLogbook :one
INSERT INTO logbooks (
    user_id,
    name,
    callsign,
    description,
    is_default
)
VALUES (
    sqlc.arg(user_id),
    sqlc.arg(name),
    CASE
        WHEN sqlc.narg(callsign)::text IS NULL THEN NULL
        ELSE UPPER(sqlc.narg(callsign)::text)
    END,
    sqlc.narg(description),
    sqlc.arg(is_default)
)
RETURNING
    id,
    uuid,
    user_id,
    name,
    callsign,
    description,
    is_default,
    created_at,
    updated_at;

-- name: GetLogbookByUUID :one
SELECT
    id,
    uuid,
    user_id,
    name,
    callsign,
    description,
    is_default,
    created_at,
    updated_at
FROM logbooks
WHERE uuid = sqlc.arg(logbook_uuid)
  AND deleted_at IS NULL;

-- name: ListLogbooksByUser :many
SELECT
    l.id,
    l.uuid,
    l.user_id,
    l.name,
    l.callsign,
    l.description,
    l.is_default,
    l.created_at,
    l.updated_at
FROM logbooks l
JOIN user_roles ur ON ur.logbook_id = l.id
WHERE ur.user_id = sqlc.arg(user_id)
  AND l.deleted_at IS NULL
ORDER BY l.created_at DESC, l.id DESC;

-- name: UpdateLogbook :one
UPDATE logbooks
SET
    name = sqlc.arg(name),
    callsign = CASE
        WHEN sqlc.narg(callsign)::text IS NULL THEN NULL
        ELSE UPPER(sqlc.narg(callsign)::text)
    END,
    description = sqlc.narg(description),
    is_default = sqlc.arg(is_default),
    updated_at = NOW()
WHERE uuid = sqlc.arg(logbook_uuid)
  AND deleted_at IS NULL
RETURNING
    id,
    uuid,
    user_id,
    name,
    callsign,
    description,
    is_default,
    created_at,
    updated_at;

-- name: DeleteLogbook :one
UPDATE logbooks
SET
    deleted_at = NOW(),
    is_default = FALSE,
    updated_at = NOW()
WHERE uuid = sqlc.arg(logbook_uuid)
  AND deleted_at IS NULL
RETURNING uuid;

-- name: GetDefaultLogbook :one
SELECT
    id,
    uuid,
    user_id,
    name,
    callsign,
    description,
    is_default,
    created_at,
    updated_at
FROM logbooks
WHERE user_id = sqlc.arg(user_id)
  AND is_default = TRUE
  AND deleted_at IS NULL
LIMIT 1;
