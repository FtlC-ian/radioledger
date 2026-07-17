# Database Guide

> Schema conventions, migrations, and sqlc usage for RadioLedger.

## Read First

Before touching the database, read [SCHEMA.md](../SCHEMA.md) in full. The schema is the source of truth.

## Key Principles

- **COMMENT ON everything**: Every table, column, type, and enum gets a COMMENT explaining purpose, valid values, ADIF mapping, and edge cases
- **Never use GORM**: sqlc + pgx/v5 is the pattern
- **RLS on every tenant-scoped table**: No exceptions
- **ADIF fields get typed columns**: Common ADIF fields get real columns. Obscure/unknown fields go to `extra` JSONB

## Schema Conventions

### Naming

- Tables: `snake_case`, plural noun (`qsos`, `logbooks`, `station_callsigns`)
- Columns: `snake_case` (`datetime_on`, `rst_sent`)
- Types/enums: `snake_case` (`qsl_status`, `sync_service`)
- Indexes: `idx_{table}_{column(s)}`
- FK constraints: `fk_{table}_{referenced_table}`

### Standard Columns

All tenant-scoped tables include:

```sql
uuid          UUID DEFAULT gen_random_uuid() NOT NULL,
created_at    TIMESTAMPTZ DEFAULT NOW() NOT NULL,
updated_at    TIMESTAMPTZ DEFAULT NOW() NOT NULL,
deleted_at    TIMESTAMPTZ  -- soft delete; NULL = active
```

### Foreign Keys

Always specify `ON DELETE` behavior. Never leave it implicit:

```sql
logbook_id BIGINT NOT NULL REFERENCES logbooks(id) ON DELETE CASCADE,
```

### COMMENT ON

Every new column must have a comment:

```sql
COMMENT ON COLUMN qsos.gridsquare IS
  'Maidenhead locator of worked station. ADIF: GRIDSQUARE. '
  'Stored as provided by operator (4 or 6 chars). '
  'Normalized to uppercase. Example: FN42aa → FN42AA.';
```

## Migrations

Migrations live in `database/migrations/` using goose SQL format.

### Creating a Migration

```bash
goose -dir database/migrations create add_qso_power_column sql
```

This creates `database/migrations/YYYYMMDDHHMMSS_add_qso_power_column.sql`.

### Migration Format

```sql
-- +goose Up
ALTER TABLE qsos ADD COLUMN tx_pwr NUMERIC(7,2);
COMMENT ON COLUMN qsos.tx_pwr IS 'Transmit power in watts. ADIF: TX_PWR. NULL if not recorded.';

-- +goose Down
ALTER TABLE qsos DROP COLUMN tx_pwr;
```

### Rules

- Each migration is a separate file
- Forward (`Up`) and rollback (`Down`) both required
- Never modify existing migration files — create a new one
- Migration files get their own git commit
- After schema change: update `docs/SCHEMA.md`

### Running Migrations

```bash
make migrate            # run pending migrations
goose status            # check migration state
goose up-by-one         # apply one migration
goose down              # rollback one migration
```

## sqlc

sqlc generates type-safe Go code from SQL queries. **Never write raw SQL in Go.**

### Query Files

SQL queries live in `database/queries/{resource}.sql`:

```sql
-- name: GetQSO :one
SELECT * FROM qsos
WHERE uuid = $1
  AND deleted_at IS NULL;

-- name: ListQSOsByLogbook :many
SELECT * FROM qsos
WHERE logbook_id = $1
  AND deleted_at IS NULL
ORDER BY datetime_on DESC
LIMIT $2;
```

### Regenerating

```bash
sqlc generate
# or: make sqlc
```

### Using Generated Code

```go
q := repository.New(db)
qso, err := q.GetQSO(ctx, qsoUUID)
```

## RLS Policy Pattern

Every tenant table needs RLS:

```sql
ALTER TABLE qsos ENABLE ROW LEVEL SECURITY;

CREATE POLICY qsos_user_isolation ON qsos
  USING (
    logbook_id IN (
      SELECT id FROM logbooks WHERE user_id = current_setting('app.current_user_id')::BIGINT
    )
  );
```

And a test to verify isolation (see [Testing Guide](testing-guide.md)).

## Related

- [SCHEMA.md](../SCHEMA.md) — full schema
- [Architecture Overview](architecture-overview.md)
- [Testing Guide](testing-guide.md)
- [Development Setup](development-setup.md)
