# Database migrations

RadioLedger uses Goose migrations for every deployed database. Migration 001 is
the consolidated base schema; do not rewrite it after a deployment has recorded
version 1. Put subsequent schema or reference-data changes in the next numbered
migration and make the upgrade safe for existing tenant data.

## Legacy baseline

Some pre-Goose staging databases contain the consolidated schema without a
`goose_db_version` row. Run `../bootstrap_legacy_goose.sql` before `goose up`.
The bootstrap records version 1 only for an exact reviewed schema fingerprint,
does nothing on an empty database, and rejects partial or unknown variants.

Migration 002 reconciles the reviewed staging variants with the canonical
schema and reference catalogs. It fails closed if a retired `vault_cert`
credential is still present.

Migration 003 grants the API role read-only access to Goose's migration history
so `/ready` can verify migration currency on both fresh and legacy-baselined
databases.

## Commands

```bash
psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -f database/bootstrap_legacy_goose.sql
goose -dir database/migrations postgres "$DATABASE_URL" up
goose -dir database/migrations postgres "$DATABASE_URL" status
```

CI must run Goose itself, assert the expected version, and exercise both a fresh
database and the supported legacy upgrade path. Do not emulate migration
behavior by concatenating `Up` sections except when deliberately constructing a
pre-Goose fixture before the baseline test.
