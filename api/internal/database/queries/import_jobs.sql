-- name: CreateImportJob :one
-- Creates a new import job record and returns the created row including its UUID.
INSERT INTO import_jobs (
    user_id,
    logbook_id,
    filename,
    file_size_bytes,
    status,
    source,
    dedup_strategy,
    timestamp_strategy
)
SELECT
    app_current_user_id(),
    lb.id,
    sqlc.narg(filename)::text,
    sqlc.narg(file_size_bytes)::bigint,
    'pending',
    sqlc.arg(source)::text,
    'skip',
    'trust_utc'
FROM logbooks lb
WHERE lb.uuid = sqlc.arg(logbook_uuid)::uuid
  AND lb.deleted_at IS NULL
  AND app_has_logbook_min_role(lb.id, app_current_user_id(), 'operator')
RETURNING id, uuid, user_id, logbook_id, filename, file_size_bytes, status,
          total_records, imported, skipped, duplicate, errors, warnings,
          source, dedup_strategy, timestamp_strategy, source_timezone, adif_version,
          created_at, started_at, completed_at;

-- name: GetImportJobByUUID :one
-- Returns an import job by its UUID, scoped to the requesting user via RLS.
SELECT
    ij.id,
    ij.uuid,
    ij.user_id,
    ij.logbook_id,
    lb.uuid AS logbook_uuid,
    ij.filename,
    ij.file_size_bytes,
    ij.status,
    ij.total_records,
    ij.imported,
    ij.skipped,
    ij.duplicate,
    ij.errors,
    ij.warnings,
    ij.source,
    ij.dedup_strategy,
    ij.timestamp_strategy,
    ij.source_timezone,
    ij.adif_version,
    ij.created_at,
    ij.started_at,
    ij.completed_at
FROM import_jobs ij
JOIN logbooks lb ON lb.id = ij.logbook_id
WHERE ij.uuid = sqlc.arg(import_job_uuid)::uuid;

-- name: GetImportJobByID :one
-- Returns an import job by its internal ID (used by the River worker).
SELECT
    id, uuid, user_id, logbook_id,
    filename, file_size_bytes, status,
    total_records, imported, skipped, duplicate, errors, warnings,
    source, dedup_strategy, timestamp_strategy, source_timezone, adif_version,
    created_at, started_at, completed_at
FROM import_jobs
WHERE id = sqlc.arg(id)::bigint;

-- name: StartImportJob :one
-- Transitions a job to processing status and sets started_at.
UPDATE import_jobs
SET
    status = 'processing',
    started_at = NOW(),
    total_records = sqlc.narg(total_records)::integer,
    adif_version = sqlc.narg(adif_version)::text
WHERE id = sqlc.arg(id)::bigint
  AND status = 'pending'
RETURNING id, uuid, status, started_at, total_records;

-- name: UpdateImportJobProgress :exec
-- Updates rolling counters for a running import job. Called periodically by the worker.
UPDATE import_jobs
SET
    imported = sqlc.arg(imported)::integer,
    skipped = sqlc.arg(skipped)::integer,
    duplicate = sqlc.arg(duplicate)::integer,
    errors = sqlc.arg(errors)::integer,
    warnings = sqlc.arg(warnings)::integer
WHERE id = sqlc.arg(id)::bigint;

-- name: CompleteImportJob :exec
-- Marks an import job as complete or errored with final counters.
UPDATE import_jobs
SET
    status = sqlc.arg(status)::text,
    completed_at = NOW(),
    total_records = sqlc.narg(total_records)::integer,
    imported = sqlc.arg(imported)::integer,
    skipped = sqlc.arg(skipped)::integer,
    duplicate = sqlc.arg(duplicate)::integer,
    errors = sqlc.arg(errors)::integer,
    warnings = sqlc.arg(warnings)::integer
WHERE id = sqlc.arg(id)::bigint;

-- name: CreateImportJobError :exec
-- Records a single per-record parse or validation error for an import job.
INSERT INTO import_job_errors (
    import_job_id,
    severity,
    record_number,
    line_number,
    adif_field,
    reason_code,
    reason_detail,
    raw_fragment
)
VALUES (
    sqlc.arg(import_job_id)::bigint,
    sqlc.arg(severity)::text,
    sqlc.narg(record_number)::integer,
    sqlc.narg(line_number)::integer,
    sqlc.narg(adif_field)::text,
    sqlc.narg(reason_code)::text,
    sqlc.arg(reason_detail)::text,
    sqlc.narg(raw_fragment)::text
);

-- name: GetImportJobErrors :many
-- Returns errors for an import job, used for the error listing endpoint.
SELECT
    id, severity, record_number, line_number,
    adif_field, reason_code, reason_detail, raw_fragment, created_at
FROM import_job_errors
WHERE import_job_id = (
    SELECT id FROM import_jobs WHERE uuid = sqlc.arg(import_job_uuid)::uuid
)
ORDER BY id
LIMIT sqlc.arg(page_size)::integer;

-- name: GetLatestImportJobErrorByUUID :one
-- Returns the most recent error reason for a given import job UUID.
SELECT reason_detail
FROM import_job_errors
WHERE import_job_id = (
    SELECT id FROM import_jobs WHERE uuid = sqlc.arg(import_job_uuid)::uuid
)
ORDER BY id DESC
LIMIT 1;
