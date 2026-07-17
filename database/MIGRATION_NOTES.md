# Migration Notes: Initial Schema, Seeds, and Verification

## What was done

- Extracted SQL from `docs/SCHEMA.md` into a goose migration:
  - `database/migrations/001_initial_schema.sql`
  - Includes full `-- +goose Up` and `-- +goose Down` sections.
- Removed the old placeholder migration:
  - `database/migrations/001_initial.sql`

## Fixes required to make schema executable

1. **Excluded non-migration example snippets**
   - Omitted SQL example blocks that are not schema DDL:
     - `SET LOCAL app.current_user_id = '42';`
     - Deduplication sample `SELECT ...` query.

2. **Fixed markdown-escaped SQL quoting**
   - `SCHEMA.md` contained many `\'` sequences inside SQL blocks.
   - Converted these into valid PostgreSQL SQL quoting in the migration file.

3. **Made role creation idempotent**
   - Replaced plain `CREATE ROLE ...` with `DO $$ ... IF NOT EXISTS ... $$` for:
     - `radioledger_api`
     - `radioledger_worker`

4. **Added explicit role grants**
   - `radioledger_api`: schema usage + CRUD on all tables + sequence usage/select.
   - `radioledger_worker`: schema usage + table read + sequence usage/select + write access needed for sync/audit tables.

5. **Down migration hardening**
   - Added robust reverse-order drops.
   - Dropped extensions before roles.
   - Added `DROP OWNED BY` guards so role drops do not fail due to lingering grants.

## Database verification performed

Using `/opt/homebrew/Cellar/postgresql@17/17.8/bin/psql` against local DB `radioledger`.

### Migration apply
- Ran the `Up` section successfully with `ON_ERROR_STOP=1`.
- All tables, indexes, policies, functions, and triggers created successfully.

### Roles
Verified roles exist and are non-bypass RLS:
- `radioledger_api` (`rolcanlogin=true`, `rolbypassrls=false`)
- `radioledger_worker` (`rolcanlogin=true`, `rolbypassrls=false`)

### RLS validation
Inserted test data as superuser for two users (Alice/Bob), each with their own logbook.

As `SET ROLE radioledger_api`:
- With `app.current_user_id = 1` (Alice): only Alice rows visible.
- With `app.current_user_id = 2` (Bob): only Bob rows visible.
- Direct checks for cross-tenant rows returned `0` rows visible.

### Seed data created and loaded
Created:
- `database/seeds/001_bands.sql` (HF through VHF/UHF/SHF/microwave, including 2190m and 630m)
- `database/seeds/002_modes.sql` (common modes incl. SSB/CW/FT8/FT4/JS8/RTTY/PSK31/AM/FM/etc.)
- `database/seeds/003_dxcc_entities.sql` (full current DXCC set, 340 entities; exceeds top-50 requirement)

Loaded successfully. Counts:
- `bands`: 25
- `modes`: 22
- `dxcc_entities`: 340

### PostGIS trigger test
Inserted a QSO with `my_gridsquare='EM12'` and `gridsquare='FN20'`.

Verified trigger behavior:
- `my_location` computed (`POINT(-97 32.5)`)
- `their_location` computed (`POINT(-75 40.5)`)
- `distance_km` computed (`2150.52` km)

## Pre-Goose upgrade bridge

`bootstrap_legacy_goose.sql` recognizes only explicitly reviewed complete
fingerprints. It records migration 1 for an exact match and rejects unknown or
partial schemas. `002_reconcile_legacy_schema.sql` then brings approved legacy
installations forward without dropping application tables or tenant data:

- canonicalizes the ADIF band, mode, and 2190m region-allocation catalogs;
- retires the unused `vault_cert` credential type only when no row uses it;
- normalizes application-role table and sequence privileges while leaving River
  objects alone; and
- restores the award/eQSL worker policies required by current background jobs.

The migration is idempotent. Its down section is intentionally a no-op because
recreating an ambiguous historical schema would be unsafe.
