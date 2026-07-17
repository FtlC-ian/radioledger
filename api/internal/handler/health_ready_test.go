package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type fakeReadyDB struct {
	pingErr           error
	versionRows       []fakeGooseVersionRow
	legacyPresent     *bool
	versionQueryErr   error
	legacyPresenceErr error
}

type fakeGooseVersionRow struct {
	version int64
	applied bool
}

func (f fakeReadyDB) Ping(context.Context) error { return f.pingErr }

func (f fakeReadyDB) Query(_ context.Context, query string, _ ...any) (pgx.Rows, error) {
	if strings.Contains(query, "to_regclass") {
		if f.legacyPresenceErr != nil {
			return nil, f.legacyPresenceErr
		}
		present := false
		if f.legacyPresent != nil {
			present = *f.legacyPresent
		}
		return &fakeReadyRows{legacyRows: []bool{present}}, nil
	}
	if f.versionQueryErr != nil {
		return nil, f.versionQueryErr
	}
	return &fakeReadyRows{versionRows: f.versionRows}, nil
}

type fakeReadyRows struct {
	versionRows []fakeGooseVersionRow
	legacyRows  []bool
	idx         int
	closed      bool
}

func (r *fakeReadyRows) Close()                                       { r.closed = true }
func (r *fakeReadyRows) Err() error                                   { return nil }
func (r *fakeReadyRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (r *fakeReadyRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r *fakeReadyRows) Next() bool {
	if r.idx >= len(r.versionRows)+len(r.legacyRows) {
		r.closed = true
		return false
	}
	r.idx++
	return true
}
func (r *fakeReadyRows) Scan(dest ...any) error {
	if r.idx == 0 || r.idx > len(r.versionRows)+len(r.legacyRows) {
		return errors.New("scan called without current row")
	}
	if len(r.legacyRows) > 0 {
		if len(dest) != 1 {
			return errors.New("expected one destination")
		}
		present, ok := dest[0].(*bool)
		if !ok {
			return errors.New("expected *bool present destination")
		}
		*present = r.legacyRows[r.idx-1]
		return nil
	}
	if len(dest) != 2 {
		return errors.New("expected two destinations")
	}
	version, ok := dest[0].(*int64)
	if !ok {
		return errors.New("expected *int64 version destination")
	}
	applied, ok := dest[1].(*bool)
	if !ok {
		return errors.New("expected *bool applied destination")
	}
	row := r.versionRows[r.idx-1]
	*version = row.version
	*applied = row.applied
	return nil
}
func (r *fakeReadyRows) Values() ([]any, error) { return nil, errors.New("not implemented") }
func (r *fakeReadyRows) RawValues() [][]byte    { return nil }
func (r *fakeReadyRows) Conn() *pgx.Conn        { return nil }

func TestReady_ReturnsOKWhenDatabaseReachableAndMigrationsCurrent(t *testing.T) {
	h := newHealthHandler(fakeReadyDB{versionRows: []fakeGooseVersionRow{{version: 3, applied: true}}}, 3)

	status, body := callReady(t, h)

	if status != http.StatusOK {
		t.Fatalf("expected status 200, got %d", status)
	}
	if body["status"] != "ready" {
		t.Fatalf("expected ready status, got %#v", body)
	}
	if _, ok := body["reason"]; ok {
		t.Fatalf("ready response should not include reason, got %#v", body)
	}
}

func TestReady_Returns503WhenDatabaseUnreachable(t *testing.T) {
	h := newHealthHandler(fakeReadyDB{pingErr: errors.New("connection refused")}, 1)

	status, body := callReady(t, h)

	if status != http.StatusServiceUnavailable {
		t.Fatalf("expected status 503, got %d", status)
	}
	if body["status"] != "unavailable" || body["reason"] != "database unreachable" {
		t.Fatalf("unexpected body: %#v", body)
	}
}

func TestReady_Returns503WhenMigrationsAreStale(t *testing.T) {
	h := newHealthHandler(fakeReadyDB{versionRows: []fakeGooseVersionRow{{version: 2, applied: true}}}, 3)

	status, body := callReady(t, h)

	if status != http.StatusServiceUnavailable {
		t.Fatalf("expected status 503, got %d", status)
	}
	if body["status"] != "unavailable" || body["reason"] != "migrations_pending" {
		t.Fatalf("unexpected body: %#v", body)
	}
}

func TestReady_AcceptsLegacyConsolidatedSchemaWithoutGooseHistory(t *testing.T) {
	present := true
	h := newHealthHandler(fakeReadyDB{versionQueryErr: errors.New("relation does not exist"), legacyPresent: &present}, 1)

	status, body := callReady(t, h)

	if status != http.StatusOK {
		t.Fatalf("expected status 200, got %d", status)
	}
	if body["status"] != "ready" {
		t.Fatalf("expected ready status, got %#v", body)
	}
}

func TestCurrentGooseVersionIgnoresRolledBackVersions(t *testing.T) {
	version, err := currentGooseVersion(context.Background(), fakeReadyDB{versionRows: []fakeGooseVersionRow{
		{version: 3, applied: false},
		{version: 3, applied: true},
		{version: 2, applied: true},
	}})
	if err != nil {
		t.Fatalf("currentGooseVersion returned error: %v", err)
	}
	if version != 2 {
		t.Fatalf("expected current version 2 after version 3 rollback, got %d", version)
	}
}

func TestReady_Returns503WhenMigrationVersionUnavailable(t *testing.T) {
	h := newHealthHandler(fakeReadyDB{versionQueryErr: errors.New("relation does not exist")}, 1)

	status, body := callReady(t, h)

	if status != http.StatusServiceUnavailable {
		t.Fatalf("expected status 503, got %d", status)
	}
	if body["status"] != "unavailable" || body["reason"] != "migration version unavailable" {
		t.Fatalf("unexpected body: %#v", body)
	}
}

func callReady(t *testing.T, h *HealthHandler) (int, map[string]string) {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	rec := httptest.NewRecorder()
	h.Ready(rec, req)

	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
	return rec.Code, body
}
