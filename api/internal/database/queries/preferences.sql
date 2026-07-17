-- name: GetUserPreferences :one
SELECT
    id,
    uuid,
    display_name,
    timezone,
    grid_square,
    default_power_watts,
    preferences,
    updated_at
FROM users
WHERE id = $1
  AND deleted_at IS NULL;

-- name: UpdateUserPreferences :one
UPDATE users
SET
    display_name = sqlc.narg(display_name),
    timezone = COALESCE(sqlc.narg(timezone)::text, timezone),
    grid_square = CASE
        WHEN sqlc.narg(grid_square)::text IS NULL THEN grid_square
        ELSE UPPER(sqlc.narg(grid_square)::text)
    END,
    default_power_watts = COALESCE(sqlc.narg(default_power_watts)::numeric, default_power_watts),
    preferences = COALESCE(sqlc.narg(preferences)::jsonb, preferences),
    updated_at = NOW()
WHERE id = sqlc.arg(id)
  AND deleted_at IS NULL
RETURNING
    id,
    uuid,
    display_name,
    timezone,
    grid_square,
    default_power_watts,
    preferences,
    updated_at;

-- name: SoftDeleteUserByID :execrows
UPDATE users
SET deleted_at = NOW(),
    updated_at = NOW()
WHERE id = $1
  AND deleted_at IS NULL;
