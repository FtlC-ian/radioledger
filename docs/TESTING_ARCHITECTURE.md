# Testing Architecture

## Philosophy

Every feature ships with tests. No exceptions. The test suite is the safety net that lets us move fast without breaking things. If it's not tested, it's not done.

Target: 90%+ code coverage on the API, 100% coverage on the ADIF parser, E2E tests for every user-facing workflow.

## Test Layers

### 1. Unit Tests (Go)

Fast, isolated, no external dependencies.

**What they cover:**
- ADIF parser (every field type, every edge case, malformed input)
- Maidenhead grid conversion and validation
- Callsign parsing and normalization (prefix extraction, suffix handling, DXCC resolution)
- Signal report validation (RST for phone/CW, dB for digital)
- QSO deduplication logic
- Award calculation logic
- Credential encryption/decryption
- Input validation and sanitization

**Conventions:**
- Table-driven tests for all parsing logic
- `testdata/` directory for ADIF fixtures
- Golden file tests for ADIF export canonicalization (import → export → compare normalized semantic output)
- Fuzz testing on ADIF parser (Go's native fuzzing)

```go
// Example: table-driven ADIF field test
func TestParseCallsign(t *testing.T) {
    tests := []struct {
        name     string
        input    string
        wantCall string
        wantDXCC int
        wantErr  bool
    }{
        {"simple US", "W5XXX", "W5XXX", 291, false},
        {"with portable", "W5XXX/P", "W5XXX", 291, false},
        {"DX prefix override", "VK9/W5XXX", "VK9/W5XXX", 35, false},
        {"maritime mobile", "W5XXX/MM", "W5XXX", 0, false},
        {"empty", "", "", 0, true},
    }
    // ...
}
```

### 2. Integration Tests (Go + PostgreSQL)

Test the API handlers against a real PostgreSQL instance with PostGIS.

**Infrastructure:**
- Testcontainers-go spins up PostgreSQL + PostGIS in Docker per test suite
- Each test gets a fresh schema (run migrations, seed reference data)
- Tests run in parallel with isolated transactions (each test wraps in a transaction and rolls back)
- RLS policies verified: explicitly test that user A cannot access user B's data

**What they cover:**
- Every API endpoint (CRUD for QSOs, logbooks, users, etc.)
- ADIF import pipeline (upload → parse → deduplicate → store)
- ADIF export (filter → format → verify semantic round-trip fidelity, not byte identity)
- Sync service adapters (mocked external services)
- Authentication and authorization (RBAC role checks)
- Rate limiting behavior
- Search and filtering queries
- Award progress calculation
- Contest workflow tables (contest_sessions, contest_qso_exchange, multipliers, Cabrillo generation)
- Operator vs station-callsign attribution integrity
- Paper QSL workflow tables (routes, batches, batch items)
- Bulk operations performance (10k, 50k, 100k QSO imports)

**Test Database Setup:**
```go
func setupTestDB(t *testing.T) *pgxpool.Pool {
    ctx := context.Background()
    pg, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
        ContainerRequest: testcontainers.ContainerRequest{
            Image:        "postgis/postgis:17-3.5",
            ExposedPorts: []string{"5432/tcp"},
            Env: map[string]string{
                "POSTGRES_DB":       "radioledger_test",
                "POSTGRES_USER":     "test",
                "POSTGRES_PASSWORD": "test",
            },
            WaitingFor: wait.ForListeningPort("5432/tcp"),
        },
        Started: true,
    })
    // Run migrations...
    // Seed reference data (bands, modes, DXCC entities)...
    return pool
}
```

**RLS Isolation Test Pattern:**
```go
func TestRLSIsolation(t *testing.T) {
    // Create QSO as user A
    qso := createTestQSO(t, userA, "W1AW", "20m", "SSB")
    
    // Attempt to read as user B — must return empty
    conn := getConnAsUser(t, userB)
    rows, err := conn.Query(ctx, "SELECT * FROM qsos WHERE id = $1", qso.ID)
    assert.NoError(t, err)
    assert.False(t, rows.Next(), "user B should not see user A's QSO")
    
    // Attempt to update as user B — must affect 0 rows
    tag, err := conn.Exec(ctx, "UPDATE qsos SET comment = 'hacked' WHERE id = $1", qso.ID)
    assert.NoError(t, err)
    assert.Equal(t, int64(0), tag.RowsAffected())
}
```

### 3. API Tests (HTTP-level)

Full HTTP request/response testing against a running API server.

**Framework:** Go's httptest + custom test helpers (similar to PubNerd's LT-core-api pattern)

**What they cover:**
- Request validation (bad input returns proper error responses)
- Authentication (expired tokens, invalid tokens, missing tokens)
- Authorization (role-based access per endpoint)
- Content negotiation (JSON responses, ADIF file downloads)
- Pagination, filtering, sorting
- Error response format consistency
- CORS headers
- Rate limit headers

**Test Organization:**
```
api/
├── internal/
│   ├── handler/
│   │   ├── qso_handler.go
│   │   ├── qso_handler_test.go      # Unit tests
│   │   └── qso_handler_api_test.go  # API integration tests
│   ├── service/
│   │   ├── qso_service.go
│   │   └── qso_service_test.go
│   └── ...
└── tests/
    ├── api/                          # Full API test suites
    │   ├── qso_test.go
    │   ├── logbook_test.go
    │   ├── import_test.go
    │   ├── sync_test.go
    │   └── auth_test.go
    └── fixtures/
        ├── adif/                     # Test ADIF files
        └── golden/                   # Golden file outputs
```

### 4. UDP Integration Tests

Test the desktop client's UDP listener against real protocol implementations.

**What they cover:**
- WSJT-X binary protocol parsing (all message types)
- JS8Call protocol parsing
- N1MM+ XML parsing
- QSO creation from UDP → API server round trip
- Malformed packet handling (no crashes, proper error logging)
- Rate limiting on incoming packets
- Offline queue behavior (server unreachable → queue → reconnect → drain)

**Approach:**
- Go test package that sends UDP packets matching real WSJT-X/N1MM+ format
- Captured packet recordings from actual software for replay testing
- Fuzz testing on UDP parser (random bytes should never crash)

```go
func TestWSJTXQSOLogged(t *testing.T) {
    // Start UDP listener on random port
    listener := udp.NewListener("127.0.0.1:0", mockAPIClient)
    defer listener.Close()
    
    // Send a real WSJT-X "QSO Logged" packet (type 5)
    packet := buildWSJTXPacket(wsjtx.MsgQSOLogged, wsjtx.QSOLoggedPayload{
        DateTimeOff: time.Now(),
        DXCall:      "W1AW",
        DXGrid:      "FN31",
        TxFreq:      14074000,
        Mode:        "FT8",
        RSTSent:     "-10",
        RSTRecv:     "+05",
    })
    
    conn, _ := net.Dial("udp", listener.Addr().String())
    conn.Write(packet)
    
    // Verify QSO was created via mock API client
    assert.Eventually(t, func() bool {
        return mockAPIClient.LastQSO != nil
    }, 2*time.Second, 100*time.Millisecond)
    
    assert.Equal(t, "W1AW", mockAPIClient.LastQSO.Callsign)
    assert.Equal(t, "FT8", mockAPIClient.LastQSO.Mode)
}
```

### 5. E2E Tests (Playwright)

Full browser automation testing the web UI end-to-end.

**Framework:** Playwright (TypeScript)

**What they cover:**
- User registration and login flow
- ADIF file upload and import
- QSO manual entry form
- Log browsing, searching, filtering
- Award progress dashboard
- Sync service configuration
- Settings and profile management
- Logbook management (create, switch, configure)
- Responsive design (desktop, tablet, mobile viewport)

**Test Organization:**
```
web/
├── e2e/
│   ├── playwright.config.ts
│   ├── fixtures/
│   │   ├── auth.ts           # Login/register helpers
│   │   ├── seed.ts           # API calls to seed test data
│   │   └── adif-files/       # Test ADIF files for upload
│   ├── tests/
│   │   ├── auth.spec.ts
│   │   ├── import.spec.ts
│   │   ├── logging.spec.ts
│   │   ├── search.spec.ts
│   │   ├── awards.spec.ts
│   │   ├── sync.spec.ts
│   │   └── settings.spec.ts
│   └── pages/                # Page object models
│       ├── login.page.ts
│       ├── dashboard.page.ts
│       ├── logbook.page.ts
│       └── import.page.ts
```

**Key E2E Scenarios:**
```typescript
test('ADIF import shows instant statistics', async ({ page, apiContext }) => {
    // Upload a 500-QSO ADIF file
    await page.goto('/import');
    await page.setInputFiles('[data-testid="adif-upload"]', 'fixtures/adif-files/500qsos.adi');
    
    // Should show progress
    await expect(page.getByTestId('import-progress')).toBeVisible();
    
    // Should show results summary
    await expect(page.getByTestId('import-count')).toHaveText('500 QSOs imported');
    await expect(page.getByTestId('dxcc-count')).toBeVisible();
    await expect(page.getByTestId('band-breakdown')).toBeVisible();
});

test('RLS prevents cross-user data access via URL manipulation', async ({ browser }) => {
    // User A creates a QSO
    const userAContext = await browser.newContext();
    const userAPage = await userAContext.newPage();
    await loginAs(userAPage, 'userA');
    const qsoId = await createQSO(userAPage, { callsign: 'W1AW', band: '20m' });
    
    // User B tries to access it directly
    const userBContext = await browser.newContext();
    const userBPage = await userBContext.newPage();
    await loginAs(userBPage, 'userB');
    await userBPage.goto(`/qso/${qsoId}`);
    
    // Should get 404 or forbidden, not the QSO
    await expect(userBPage.getByTestId('not-found')).toBeVisible();
});
```

### 6. ADIF Corpus Testing

Dedicated test suite for ADIF compatibility with real-world files.

**Strategy:**
- Collect ADIF exports from every major logging program
- Store in `testdata/adif/` with source program noted
- Round-trip test each file: import → export → semantic compare (field-equivalent, deterministic canonical output)
- Track compatibility percentage per program

**Target Programs:**
- Ham Radio Deluxe
- Log4OM
- WSJT-X
- N1MM+
- DXKeeper / DXLab Suite
- ACLog (N3FJP)
- MacLoggerDX
- RUMlogNG
- CloudLog / Wavelog
- CQRLOG
- HAMRS
- LoTW export files

```go
func TestADIFCorpus(t *testing.T) {
    files, _ := filepath.Glob("testdata/adif/*.adi")
    for _, f := range files {
        t.Run(filepath.Base(f), func(t *testing.T) {
            // Parse
            records, err := adif.ParseFile(f)
            assert.NoError(t, err, "parse should not fail")
            assert.NotEmpty(t, records)
            
            // Round-trip
            var buf bytes.Buffer
            err = adif.Write(&buf, records)
            assert.NoError(t, err)
            
            reparsed, err := adif.Parse(bytes.NewReader(buf.Bytes()))
            assert.NoError(t, err)
            assert.Equal(t, len(records), len(reparsed))
            
            // Field-by-field comparison
            for i, orig := range records {
                assertQSOEqual(t, orig, reparsed[i])
            }
        })
    }
}
```

### 7. Performance / Load Tests

Not in CI (too slow), but run before releases and after major changes.

**Tools:** k6 or vegeta for HTTP load testing, custom Go benchmarks for hot paths

**Scenarios:**
- ADIF import: 100k QSOs in under 30 seconds
- Search: sub-100ms response with 1M QSOs in database
- Concurrent users: 100 simultaneous loggers (contest weekend simulation)
- Sync worker throughput: process 10k pending sync items in 5 minutes
- PostGIS queries: spatial search over 1M QSOs under 200ms

## CI/CD Pipeline

### On Every PR
1. `go vet` + `golangci-lint`
2. Unit tests (fast, no Docker)
3. Integration tests (testcontainers PostgreSQL)
4. API tests
5. Playwright E2E tests
6. ADIF corpus tests
7. Coverage report (fail if below threshold)

### On Merge to Main
1. All of the above
2. Build Docker images
3. Deploy to staging
4. Run smoke tests against staging
5. Tag release if version bumped

### Nightly
1. Full test suite
2. Performance benchmarks (track regressions)
3. ADIF fuzz testing (extended runs)
4. Dependency vulnerability scan

## Test Data Management

### Fixtures
- `testdata/adif/` — Real-world ADIF files from various programs
- `testdata/udp/` — Captured UDP packets from WSJT-X, JS8Call, N1MM+
- `testdata/golden/` — Canonical expected outputs for semantic golden tests
- `database/seeds/` — Reference data (bands, modes, DXCC entities)

### Test Users
Standard test users seeded in test database:
- `admin@test.com` — Admin role
- `operator@test.com` — Operator role
- `viewer@test.com` — Viewer role
- `sync@test.com` — External-service sync test user
- `admin@test.com` — Administrative-permission test user

## Monitoring in Production

Testing doesn't stop at CI:
- Synthetic monitoring: periodic ADIF import/export round-trip
- Sync health checks: verify each service adapter is functional
- Error rate alerting: spike in 5xx responses
- Performance monitoring: p95 response time per endpoint
- Database query performance: pg_stat_statements analysis

## Ground Rules

1. No PR merges without passing tests
2. New features require tests in the same PR
3. Bug fixes require a regression test proving the fix
4. ADIF parser changes require full corpus re-test
5. Security-sensitive changes require Playwright E2E verification
6. Performance-sensitive changes require benchmark comparison
