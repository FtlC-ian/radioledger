# Code Style

> Go, TypeScript, and SQL conventions for RadioLedger.

## Go

### General Rules

- **gofmt**: Run before every commit. CI enforces this.
- **go vet**: Fix all warnings. CI enforces this.
- **golangci-lint**: Fix all issues. CI enforces this.

### Error Handling

Always wrap errors with context:

```go
// GOOD
if err != nil {
    return fmt.Errorf("creating QSO for logbook %s: %w", logbookUUID, err)
}

// BAD
if err != nil {
    return err
}
```

Use `errors.Is` and `errors.As` for error type checking — never string comparison.

### No Global State

Use dependency injection via constructor functions:

```go
// GOOD
type QSOService struct {
    db  *pgxpool.Pool
    log *slog.Logger
}

func NewQSOService(db *pgxpool.Pool, log *slog.Logger) *QSOService {
    return &QSOService{db: db, log: log}
}

// BAD: global db var
var db *pgxpool.Pool
```

### Logging

Use `slog` (stdlib, not third-party):

```go
slog.InfoContext(ctx, "QSO created",
    slog.String("uuid", qso.UUID.String()),
    slog.String("callsign", qso.Callsign),
    slog.Duration("duration", time.Since(start)),
)
```

**Never log sensitive data**: passwords, API keys, tokens, callsign-linked PII.

### sqlc Patterns

**Never build SQL strings.** Use sqlc-generated functions:

```go
// GOOD
qso, err := q.GetQSO(ctx, qsoUUID)

// BAD
row := db.QueryRow(ctx, "SELECT * FROM qsos WHERE uuid = '"+uuid+"'")
```

### Struct Tags

```go
type CreateQSORequest struct {
    Callsign   string     `json:"callsign" validate:"required,min=3,max=20"`
    Band       string     `json:"band"     validate:"required"`
    Mode       string     `json:"mode"     validate:"required"`
    DatetimeOn time.Time  `json:"datetime_on" validate:"required"`
    Comment    *string    `json:"comment"`  // pointer = optional field
}
```

## SQL

### Keywords

Uppercase SQL keywords:

```sql
-- GOOD
SELECT id, callsign, band FROM qsos WHERE logbook_id = $1;

-- BAD
select id, callsign, band from qsos where logbook_id = $1;
```

### Explicit Column Lists

```sql
-- GOOD
INSERT INTO qsos (callsign, band, mode, datetime_on)
VALUES ($1, $2, $3, $4);

-- BAD
INSERT INTO qsos VALUES ($1, $2, $3, $4);
```

### COMMENT ON

Every table, column, and type gets a comment:

```sql
COMMENT ON COLUMN qsos.band IS
  'Amateur band. ADIF: BAND. Derived from freq when possible. '
  'Values: 160m, 80m, 60m, 40m, 30m, 20m, 17m, 15m, 12m, 10m, '
  '6m, 2m, 70cm, ...';
```

## TypeScript (Web/Desktop/Mobile)

### Strict Mode

TypeScript strict mode is enabled. No `any` types without a comment explaining why.

### No `any`

```typescript
// GOOD
interface QSO {
  uuid: string;
  callsign: string;
  band: string;
}

// BAD
const qso: any = { ... };
```

### Component Tests

Test files live alongside component files:

```
src/
  components/
    QSOForm.vue
    QSOForm.test.ts   ← alongside the component
```

## File Organization

- One resource per handler file: `handlers/qsos.go`, `handlers/logbooks.go`
- Middleware in `middleware/`
- Business logic in `service/` (no HTTP concerns)
- Data access in `repository/` (sqlc-generated)

## Commit Messages

Use [Conventional Commits](https://www.conventionalcommits.org/):

```
feat: add POTA park reference to QSO form
fix: correct DXCC resolution for VK9 prefix override
docs: add API pagination guide
test: add RLS isolation test for logbooks table
refactor: extract callsign normalization to pkg/callsign
```

## Related

- [Pull Requests](pull-requests.md)
- [Testing Guide](testing-guide.md)
- [Development Setup](development-setup.md)
