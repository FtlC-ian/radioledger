-- +goose Up
-- The API readiness endpoint reads Goose's migration history using the
-- least-privileged application role. Legacy-baselined databases create this
-- table after migration 001's broad grants have already run, so grant the
-- required read access explicitly. Migration 001's broad fresh-schema grant
-- also included write privileges on this Goose-owned table; remove those so
-- both database histories expose the same SELECT-only surface.
REVOKE INSERT, UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER
    ON TABLE goose_db_version FROM radioledger_api;
GRANT SELECT ON TABLE goose_db_version TO radioledger_api;

-- +goose Down
-- Deliberately retain this least-privilege grant during an application
-- rollback. Migration 001 already grants it on fresh databases, and revoking
-- it would make `/ready` fail after rolling back a legacy-baselined database.
SELECT 1;
