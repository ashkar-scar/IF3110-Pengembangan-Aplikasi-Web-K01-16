package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/auth"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/core"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/targetdb"
)

const shopFile = "0123456789abcdef0123456789abcdef.db"

type fakeTargets struct {
	targets map[int64]Target
	grants  map[int64]map[int64]core.Permission
}

func (f fakeTargets) Targets(ctx context.Context, userID int64) ([]Target, error) {
	result := []Target{}
	for id, permission := range f.grants[userID] {
		target := f.targets[id]
		target.Permission = permission
		result = append(result, target)
	}
	return result, nil
}

func (f fakeTargets) Target(ctx context.Context, userID, targetID int64) (Target, error) {
	target, ok := f.targets[targetID]
	if !ok {
		return Target{}, ErrTargetNotFound
	}
	target.Permission = f.grants[userID][targetID]
	return target, nil
}

func testAPI(t *testing.T) *API {
	t.Helper()
	dir := t.TempDir()
	conn, err := targetdb.OpenSQLite(context.Background(), dir, shopFile)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_, err = conn.Execute(context.Background(), targetdb.Execution{ID: "seed", BaseVersion: 0, SQL: `CREATE TABLE customers (id INTEGER PRIMARY KEY, name TEXT NOT NULL);
CREATE TABLE orders (id INTEGER PRIMARY KEY, customer_id INTEGER NOT NULL REFERENCES customers(id), note TEXT);
INSERT INTO customers VALUES (1, 'Rani'), (2, 'Bayu');
INSERT INTO orders(customer_id, note) VALUES (1, 'a'), (1, 'b'), (2, 'c')`})
	if err != nil {
		t.Fatal(err)
	}
	a := &API{dataDir: dir}
	a.targets = fakeTargets{
		targets: map[int64]Target{
			1: {ID: 1, Name: "shop", Driver: "sqlite", Config: targetdb.Config{Database: shopFile}},
			2: {ID: 2, Name: "broken", Driver: "postgres", Config: targetdb.Config{Host: "127.0.0.1", Port: 1, Database: "x", Username: "u", SSLMode: "disable"}, Password: "super-secret"},
		},
		grants: map[int64]map[int64]core.Permission{
			10: {1: core.PermissionView, 2: core.PermissionView},
			20: {1: core.PermissionNone},
		},
	}
	return a
}

func session(userID int64, admin bool) auth.AuthenticationResult {
	return auth.AuthenticationResult{
		User:    core.User{ID: userID, Username: "user", Active: true, PlatformAdmin: admin},
		Session: core.Session{CSRFToken: "csrf-token"},
	}
}

func call(handler func(http.ResponseWriter, *http.Request, auth.AuthenticationResult), s auth.AuthenticationResult, method, target, id string, header ...string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, nil)
	r.SetPathValue("id", id)
	if len(header) == 2 {
		r.Header.Set(header[0], header[1])
	}
	w := httptest.NewRecorder()
	handler(w, r, s)
	return w
}

func decode(t *testing.T, w *httptest.ResponseRecorder, status int, into any) {
	t.Helper()
	if w.Code != status {
		t.Fatalf("status %d, want %d: %s", w.Code, status, w.Body.String())
	}
	if into != nil {
		if err := json.Unmarshal(w.Body.Bytes(), into); err != nil {
			t.Fatal(err)
		}
	}
}

func TestExplorerRequiresSession(t *testing.T) {
	a := testAPI(t)
	mux := http.NewServeMux()
	a.explorerRoutes(mux)
	for _, path := range []string{"/api/databases", "/api/databases/1/schema", "/api/databases/1/rows?table=orders"} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s returned %d without a session", path, w.Code)
		}
	}
	empty := &API{}
	mux = http.NewServeMux()
	empty.explorerRoutes(mux)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/databases", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("explorer routes must stay off until a target source is configured, got %d", w.Code)
	}
}

func TestDatabasesHidesConnectionSecrets(t *testing.T) {
	a := testAPI(t)
	w := call(a.databases, session(10, false), "GET", "/api/databases", "")
	var targets []map[string]any
	decode(t, w, http.StatusOK, &targets)
	if len(targets) != 2 || strings.Contains(w.Body.String(), "super-secret") || strings.Contains(w.Body.String(), "127.0.0.1") {
		t.Fatalf("unexpected database list: %s", w.Body.String())
	}
	w = call(a.databases, session(99, false), "GET", "/api/databases", "")
	if w.Code != http.StatusOK || strings.TrimSpace(w.Body.String()) != "[]" {
		t.Fatalf("user without grants must get an empty list: %s", w.Body.String())
	}
}

func TestExploreReadsSchemaTablesAndRows(t *testing.T) {
	a := testAPI(t)
	viewer := session(10, false)
	var overview struct {
		Snapshot    targetdb.Snapshot `json:"snapshot"`
		Fingerprint string            `json:"fingerprint"`
		Version     int64             `json:"version"`
	}
	decode(t, call(a.explore, viewer, "GET", "/api/databases/1/schema", "1"), http.StatusOK, &overview)
	if overview.Version != 1 || len(overview.Fingerprint) != 64 || len(overview.Snapshot.Tables) != 2 {
		t.Fatalf("unexpected overview: %+v", overview)
	}
	var schemas []string
	decode(t, call(a.explore, viewer, "GET", "/api/databases/1/schemas", "1"), http.StatusOK, &schemas)
	if len(schemas) != 1 || schemas[0] != "main" {
		t.Fatalf("unexpected schemas: %v", schemas)
	}
	var tables []targetdb.Table
	decode(t, call(a.explore, viewer, "GET", "/api/databases/1/tables", "1"), http.StatusOK, &tables)
	if len(tables) != 2 || tables[0].Name != "customers" {
		t.Fatalf("unexpected tables: %+v", tables)
	}
	var table targetdb.Table
	decode(t, call(a.explore, viewer, "GET", "/api/databases/1/table?table=orders", "1"), http.StatusOK, &table)
	if len(table.Columns) != 3 || len(table.Constraints) != 2 {
		t.Fatalf("unexpected table detail: %+v", table)
	}
	var page targetdb.RowPage
	decode(t, call(a.explore, viewer, "GET", "/api/databases/1/rows?table=orders&limit=2", "1"), http.StatusOK, &page)
	if len(page.Rows) != 2 || !page.HasMore || page.Columns[2] != "note" {
		t.Fatalf("unexpected first page: %+v", page)
	}
	decode(t, call(a.explore, viewer, "GET", "/api/databases/1/rows?table=orders&limit=2&offset=2", "1"), http.StatusOK, &page)
	if len(page.Rows) != 1 || page.HasMore {
		t.Fatalf("unexpected last page: %+v", page)
	}
}

func TestExploreRejectsInvalidRequests(t *testing.T) {
	a := testAPI(t)
	cases := []struct {
		name   string
		user   int64
		target string
		id     string
		status int
	}{
		{"no permission", 20, "/api/databases/1/schema", "1", http.StatusForbidden},
		{"unknown database", 10, "/api/databases/9/schema", "9", http.StatusNotFound},
		{"invalid id", 10, "/api/databases/abc/schema", "abc", http.StatusNotFound},
		{"missing table", 10, "/api/databases/1/table?table=missing", "1", http.StatusNotFound},
		{"missing table rows", 10, "/api/databases/1/rows?table=missing", "1", http.StatusNotFound},
		{"ledger hidden", 10, "/api/databases/1/rows?table=schema_migrations", "1", http.StatusNotFound},
		{"limit not a number", 10, "/api/databases/1/rows?table=orders&limit=x", "1", http.StatusUnprocessableEntity},
		{"limit too large", 10, "/api/databases/1/rows?table=orders&limit=500", "1", http.StatusUnprocessableEntity},
		{"negative offset", 10, "/api/databases/1/rows?table=orders&offset=-1", "1", http.StatusUnprocessableEntity},
		{"unreachable target", 10, "/api/databases/2/schema", "2", http.StatusServiceUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := call(a.explore, session(tc.user, false), "GET", tc.target, tc.id)
			decode(t, w, tc.status, nil)
			if strings.Contains(w.Body.String(), "super-secret") || strings.Contains(w.Body.String(), "127.0.0.1") {
				t.Fatalf("error response leaked connection details: %s", w.Body.String())
			}
		})
	}
}

func TestTestTargetRequiresAdminAndCSRF(t *testing.T) {
	a := testAPI(t)
	decode(t, call(a.testTarget, session(10, false), "POST", "/api/admin/databases/1/test", "1", "X-CSRF-Token", "csrf-token"), http.StatusForbidden, nil)
	decode(t, call(a.testTarget, session(30, true), "POST", "/api/admin/databases/1/test", "1"), http.StatusForbidden, nil)
	var result map[string]bool
	decode(t, call(a.testTarget, session(30, true), "POST", "/api/admin/databases/1/test", "1", "X-CSRF-Token", "csrf-token"), http.StatusOK, &result)
	if !result["connected"] {
		t.Fatalf("unexpected result: %v", result)
	}
	decode(t, call(a.testTarget, session(30, true), "POST", "/api/admin/databases/2/test", "2", "X-CSRF-Token", "csrf-token"), http.StatusServiceUnavailable, nil)
}

func TestRouterRegistersExplorerRoutes(t *testing.T) {
	router := testAPI(t).Router()
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", "/api/databases", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("explorer route must be registered behind login, got %d", w.Code)
	}
}
