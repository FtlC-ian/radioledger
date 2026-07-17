-- name: GetUserByID :one
SELECT
    id,
    uuid,
    email,
    callsign,
    onboarding_complete,
    display_name,
    timezone,
    created_at,
    updated_at
FROM users
WHERE id = sqlc.arg(id)
  AND deleted_at IS NULL;

-- name: GetUserByEmail :one
SELECT
    id,
    uuid,
    email,
    callsign,
    onboarding_complete,
    display_name,
    timezone,
    created_at,
    updated_at
FROM users
WHERE LOWER(email) = LOWER(sqlc.arg(email))
  AND deleted_at IS NULL;

-- name: GetUserByZitadelID :one
SELECT
    id,
    uuid,
    email,
    callsign,
    onboarding_complete,
    display_name,
    timezone,
    created_at,
    updated_at
FROM users
WHERE zitadel_id = sqlc.arg(zitadel_id)
  AND deleted_at IS NULL;

-- name: LinkUserZitadelIDByEmail :one
UPDATE users
SET zitadel_id = sqlc.arg(zitadel_id),
    updated_at = NOW()
WHERE LOWER(email) = LOWER(sqlc.arg(email))
  AND deleted_at IS NULL
  AND zitadel_id IS NULL
RETURNING
    id,
    uuid,
    email,
    callsign,
    onboarding_complete,
    display_name,
    timezone,
    created_at,
    updated_at;

-- name: CreateUser :one
INSERT INTO users (
    email,
    password_hash,
    callsign,
    display_name,
    timezone,
    onboarding_complete
)
VALUES (
    sqlc.arg(email),
    sqlc.narg(password_hash),
    CASE
        WHEN sqlc.narg(callsign)::text IS NULL THEN NULL
        ELSE UPPER(sqlc.narg(callsign)::text)
    END,
    sqlc.narg(display_name),
    COALESCE(sqlc.narg(timezone)::text, 'UTC'),
    CASE
        WHEN sqlc.narg(callsign)::text IS NULL THEN FALSE
        ELSE TRUE
    END
)
RETURNING
    id,
    uuid,
    email,
    callsign,
    onboarding_complete,
    display_name,
    timezone,
    created_at,
    updated_at;

-- name: GetUserByCallsign :one
SELECT
    id,
    uuid,
    email,
    callsign,
    onboarding_complete,
    display_name,
    timezone,
    created_at,
    updated_at
FROM users
WHERE upper(callsign) = upper(sqlc.arg(callsign))
  AND deleted_at IS NULL;

-- name: GetUserByEmailWithPassword :one
SELECT
    id,
    uuid,
    email,
    password_hash,
    callsign,
    onboarding_complete,
    display_name,
    timezone,
    created_at,
    updated_at
FROM users
WHERE LOWER(email) = LOWER(sqlc.arg(email))
  AND deleted_at IS NULL;

-- name: CreateZitadelUser :one
INSERT INTO users (
    email,
    zitadel_id,
    timezone
)
VALUES (
    LOWER(BTRIM(sqlc.arg(email)::text)),
    sqlc.arg(zitadel_id),
    COALESCE(sqlc.narg(timezone)::text, 'UTC')
)
RETURNING
    id,
    uuid,
    email,
    callsign,
    onboarding_complete,
    display_name,
    timezone,
    created_at,
    updated_at;

-- name: UpdatePasswordHash :exec
UPDATE users
SET password_hash = sqlc.arg(password_hash),
    updated_at    = NOW()
WHERE id = sqlc.arg(id)
  AND deleted_at IS NULL;
