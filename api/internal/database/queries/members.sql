-- name: GetUserRoleForLogbook :one
SELECT ur.role
FROM user_roles ur
JOIN logbooks lb ON lb.id = ur.logbook_id
WHERE lb.uuid = sqlc.arg(logbook_uuid)
  AND ur.user_id = sqlc.arg(user_id)
  AND lb.deleted_at IS NULL;

-- name: ListLogbookMembers :many
SELECT
    u.id AS user_id,
    u.uuid AS user_uuid,
    u.email,
    u.callsign,
    u.display_name,
    ur.role,
    ur.created_at,
    ur.updated_at
FROM user_roles ur
JOIN logbooks lb ON lb.id = ur.logbook_id
JOIN users u ON u.id = ur.user_id
WHERE lb.uuid = sqlc.arg(logbook_uuid)
  AND lb.deleted_at IS NULL
  AND u.deleted_at IS NULL
ORDER BY app_role_rank(ur.role) DESC, lower(u.email) ASC;

-- name: GetLogbookMemberByUserUUID :one
SELECT
    ur.logbook_id,
    u.id AS user_id,
    u.uuid AS user_uuid,
    u.email,
    u.callsign,
    u.display_name,
    ur.role,
    ur.created_at,
    ur.updated_at
FROM user_roles ur
JOIN logbooks lb ON lb.id = ur.logbook_id
JOIN users u ON u.id = ur.user_id
WHERE lb.uuid = sqlc.arg(logbook_uuid)
  AND u.uuid = sqlc.arg(user_uuid)
  AND lb.deleted_at IS NULL
  AND u.deleted_at IS NULL;

-- name: UpsertLogbookMemberRole :one
INSERT INTO user_roles (
    logbook_id,
    user_id,
    role,
    invited_by
)
SELECT
    lb.id,
    sqlc.arg(user_id),
    sqlc.arg(role),
    sqlc.narg(invited_by)
FROM logbooks lb
WHERE lb.uuid = sqlc.arg(logbook_uuid)
  AND lb.deleted_at IS NULL
ON CONFLICT (logbook_id, user_id)
DO UPDATE SET
    role = EXCLUDED.role,
    invited_by = EXCLUDED.invited_by,
    updated_at = NOW()
RETURNING
    id,
    uuid,
    logbook_id,
    user_id,
    role,
    invited_by,
    created_at,
    updated_at;

-- name: UpdateLogbookMemberRoleByUserUUID :one
UPDATE user_roles ur
SET
    role = sqlc.arg(role),
    invited_by = sqlc.narg(invited_by),
    updated_at = NOW()
FROM logbooks lb, users u
WHERE ur.logbook_id = lb.id
  AND ur.user_id = u.id
  AND lb.uuid = sqlc.arg(logbook_uuid)
  AND u.uuid = sqlc.arg(user_uuid)
  AND lb.deleted_at IS NULL
  AND u.deleted_at IS NULL
RETURNING
    ur.id,
    ur.uuid,
    ur.logbook_id,
    ur.user_id,
    ur.role,
    ur.invited_by,
    ur.created_at,
    ur.updated_at;

-- name: DeleteLogbookMemberByUserUUID :execrows
DELETE FROM user_roles ur
USING logbooks lb, users u
WHERE ur.logbook_id = lb.id
  AND ur.user_id = u.id
  AND lb.uuid = sqlc.arg(logbook_uuid)
  AND u.uuid = sqlc.arg(user_uuid)
  AND lb.deleted_at IS NULL
  AND u.deleted_at IS NULL;

-- name: GetLogbookOwner :one
SELECT
    u.id AS user_id,
    u.uuid AS user_uuid,
    ur.role
FROM user_roles ur
JOIN logbooks lb ON lb.id = ur.logbook_id
JOIN users u ON u.id = ur.user_id
WHERE lb.uuid = sqlc.arg(logbook_uuid)
  AND ur.role = 'owner'
  AND lb.deleted_at IS NULL
LIMIT 1;

-- name: SetLogbookOwnerUser :execrows
UPDATE logbooks
SET
    user_id = sqlc.arg(user_id),
    is_default = FALSE,
    updated_at = NOW()
WHERE uuid = sqlc.arg(logbook_uuid)
  AND deleted_at IS NULL;

-- name: SetLogbookMemberRoleByUserID :execrows
UPDATE user_roles
SET
    role = sqlc.arg(role),
    invited_by = sqlc.narg(invited_by),
    updated_at = NOW()
WHERE logbook_id = sqlc.arg(logbook_id)
  AND user_id = sqlc.arg(user_id);
