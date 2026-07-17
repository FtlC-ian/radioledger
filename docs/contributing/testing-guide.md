# Testing Guide

> How to write and run tests in RadioLedger.

## Testing Philosophy

Every feature, every bug fix ships with tests. No exceptions. See [TESTING_ARCHITECTURE.md](../TESTING_ARCHITECTURE.md) for the full testing strategy.

## Test Layers

| Layer | Tool | What it tests |
|-------|------|---------------|
| Unit | Go `testing` | Parsing, validation, business logic |
| Integration | testcontainers-go | API endpoints + real PostgreSQL |
| Database | testcontainers-go | RLS isolation, schema constraints |
| E2E | Playwright | Full user workflows |
| ADIF corpus | Custom | Round-trip fidelity with real ADIF files |

## Running Tests

```bash
# All tests
make test

# Fast unit tests only (no Docker)
cd api && go test ./... -short

# Integration tests (needs Docker)
cd api && go test ./... -run Integration

# With coverage
cd api && go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out

# E2E tests
make e2e
```

## Writing Tests

### Unit Test Pattern

```go
func TestCallsignNormalization(t *testing.T) {
    tests := []struct {
        name     string
        input    string
        expected string
        wantErr  bool
    }{
        {"standard callsign", "w1aw", "W1AW", false},
        {"portable suffix", "W1AW/P", "W1AW/P", false},
        {"prefix override", "vk9/w1aw", "VK9/W1AW", false},
        {"invalid callsign", "!!!!", "", true},
        {"empty", "", "", true},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got, err := callsign.Normalize(tt.input)
            if (err != nil) != tt.wantErr {
                t.Errorf("wantErr %v, got error %v", tt.wantErr, err)
            }
            if got != tt.expected {
                t.Errorf("expected %q, got %q", tt.expected, got)
            }
        })
    }
}
```

Use **table-driven tests** for all parsing and validation logic.

### Integration Test Pattern

Integration tests use testcontainers-go to spin up a real PostgreSQL:

```go
func TestQSOCreate_Integration(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping integration test in short mode")
    }
    
    db := testutil.MustStartDB(t)
    defer db.Close()
    
    client := testutil.NewAPIClient(t, db)
    
    // Create logbook
    lb := client.MustCreateLogbook(t, "Test Log")
    
    // Create QSO
    resp := client.POST("/v1/logbooks/"+lb.UUID+"/qsos", map[string]any{
        "callsign":    "W1AW",
        "band":        "20m",
        "mode":        "SSB",
        "datetime_on": "2026-02-28T14:32:00Z",
    })
    require.Equal(t, true, resp.Success)
    require.Equal(t, "W1AW", resp.Data["callsign"])
}
```

### RLS Isolation Test Pattern

Every new tenant-scoped table needs an RLS isolation test:

```go
func TestQSOsRLSIsolation(t *testing.T) {
    // Create two users
    userA := testutil.MustCreateUser(t, db, "User A")
    userB := testutil.MustCreateUser(t, db, "User B")
    
    // User A creates a QSO
    qsoA := testutil.MustCreateQSO(t, db, userA, "W1AW")
    
    // User B should NOT be able to see User A's QSO
    clientB := testutil.NewAPIClientForUser(t, db, userB)
    qso := clientB.GetQSO(qsoA.UUID)
    require.False(t, qso.Success, "User B must not see User A's QSO")
}
```

### Test Naming

Name tests by scenario, not implementation:

```go
// GOOD:
func TestImportRejectsInvalidCallsign(t *testing.T)
func TestDuplicateQSOSkippedOnImport(t *testing.T)

// BAD:
func TestValidation(t *testing.T)
func TestImport(t *testing.T)
```

## ADIF Corpus Tests

The `testdata/adif/` directory contains real-world ADIF files from major loggers. The ADIF parser must handle all of them without errors:

```bash
cd api && go test ./pkg/adifparser/... -run TestCorpus
```

If you change the ADIF parser, run the full corpus test before committing.

## Playwright E2E Tests

E2E tests live in `web/tests/`:

```bash
cd web && pnpm exec playwright test
```

Write E2E tests for new UI workflows. At minimum:
- Happy path through the workflow
- Error state handling

## Related

- [TESTING_ARCHITECTURE.md](../TESTING_ARCHITECTURE.md)
- [Development Setup](development-setup.md)
- [Database Guide](database-guide.md)
