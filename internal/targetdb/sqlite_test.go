package targetdb

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

const sqliteName = "0123456789abcdef0123456789abcdef.db"

func openTestSQLite(t *testing.T, dir string) *Connection {
	t.Helper()
	conn, err := OpenSQLite(context.Background(), dir, sqliteName)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func execute(conn *Connection, id, sql string, base int64) (int64, error) {
	return conn.Execute(context.Background(), Execution{ID: id, SQL: sql, ActorID: 1, ActorName: "editor", BaseVersion: base})
}

const shopSchema = `CREATE TABLE customers (id INTEGER PRIMARY KEY, name TEXT NOT NULL, email TEXT UNIQUE, credit REAL DEFAULT 0 CHECK (credit >= 0));
CREATE TABLE orders (id INTEGER PRIMARY KEY, customer_id INTEGER NOT NULL REFERENCES customers(id) ON DELETE CASCADE, note TEXT);
CREATE INDEX orders_customer_idx ON orders(customer_id);
INSERT INTO customers(id, name, email) VALUES (1, 'Rani', 'rani@example.test'), (2, 'Bayu', NULL);
INSERT INTO orders(customer_id, note) VALUES (1, 'a'), (1, 'b'), (2, 'c')`

func TestSQLiteRejectsFilesOutsideDataDirectory(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"../escape.db", "notes.db", filepath.Join(dir, sqliteName), sqliteName + "?mode=memory", "file:" + sqliteName} {
		if _, err := OpenSQLite(context.Background(), dir, name); err == nil {
			t.Errorf("accepted file name %q", name)
		}
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatal("invalid name created a file")
	}
}

func TestSQLiteMigrationExplorerAndRollback(t *testing.T) {
	conn := openTestSQLite(t, t.TempDir())
	ctx := context.Background()
	version, err := execute(conn, "attempt-1", shopSchema, 0)
	if err != nil || version != 1 {
		t.Fatalf("version %d, error %v", version, err)
	}
	snapshot, err := conn.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Tables) != 2 || snapshot.Schemas()[0] != "main" {
		t.Fatalf("unexpected tables: %+v", snapshot.Tables)
	}
	orders, _ := snapshot.Find("main", "orders")
	kinds := map[string]string{}
	for _, constraint := range orders.Constraints {
		kinds[constraint.Kind] = constraint.Definition
	}
	if kinds["p"] != `PRIMARY KEY ("id")` || kinds["f"] != `FOREIGN KEY ("customer_id") REFERENCES "customers"("id") ON DELETE CASCADE` {
		t.Fatalf("unexpected constraints: %v", kinds)
	}
	if len(orders.Indexes) != 1 || orders.Indexes[0].Definition != "CREATE INDEX orders_customer_idx ON orders(customer_id)" {
		t.Fatalf("unexpected indexes: %+v", orders.Indexes)
	}
	customers, _ := snapshot.Find("main", "customers")
	if !strings.Contains(customers.Definition, "CHECK (credit >= 0)") || customers.Columns[3].Default == nil || *customers.Columns[3].Default != "0" {
		t.Fatalf("raw definition or default missing: %+v", customers)
	}
	page, err := conn.Rows(ctx, "main", "orders", 2, 0)
	if err != nil || len(page.Rows) != 2 || !page.HasMore || page.Columns[2] != "note" {
		t.Fatalf("pagination failed: %+v %v", page, err)
	}

	if _, err = execute(conn, "attempt-2", "ALTER TABLE customers ADD COLUMN phone TEXT; INSERT INTO missing VALUES (1)", 1); err == nil || !strings.Contains(err.Error(), "statement 2") {
		t.Fatalf("expected statement 2 failure, got %v", err)
	}
	snapshot, _ = conn.Snapshot(ctx)
	customers, _ = snapshot.Find("main", "customers")
	if len(customers.Columns) != 4 {
		t.Fatal("rolled back DDL left a column")
	}
	if _, err = execute(conn, "attempt-3", "INSERT INTO orders(customer_id) VALUES (99)", 1); err == nil {
		t.Fatal("foreign key was not enforced")
	}
	if version, _ = conn.Version(ctx); version != 1 {
		t.Fatalf("failed attempts changed version to %d", version)
	}
	if again, err := execute(conn, "attempt-1", shopSchema, 0); err != nil || again != 1 {
		t.Fatal("repeated attempt was executed twice")
	}
	if found, ok, err := conn.Lookup(ctx, "attempt-1"); err != nil || !ok || found != 1 {
		t.Fatal("ledger lookup failed")
	}
	if _, ok, _ := conn.Lookup(ctx, "attempt-2"); ok {
		t.Fatal("failed attempt found in ledger")
	}
	if _, err = execute(conn, "attempt-4", "INSERT INTO orders(customer_id) VALUES (2)", 0); !errors.Is(err, ErrVersion) {
		t.Fatalf("stale base version accepted: %v", err)
	}
}

func TestSQLiteRejectsUnsafeStatements(t *testing.T) {
	dir := t.TempDir()
	conn := openTestSQLite(t, dir)
	for i, sql := range []string{"ATTACH DATABASE 'other.db' AS other", "PRAGMA foreign_keys=OFF", "CREATE TABLE a(id INTEGER); DELETE FROM schema_migrations", "VACUUM INTO 'copy.db'", "CREATE TEMP TABLE scratch(id INTEGER)", "SELECT load_extension('evil')"} {
		if _, err := execute(conn, "unsafe-"+string(rune('a'+i)), sql, 0); err == nil {
			t.Errorf("accepted %q", sql)
		}
	}
	entries, _ := os.ReadDir(dir)
	for _, entry := range entries {
		if entry.Name() != sqliteName {
			t.Fatalf("unexpected file %s", entry.Name())
		}
	}
}

func TestSQLiteConcurrentMigrationsStayLinear(t *testing.T) {
	dir := t.TempDir()
	first, second := openTestSQLite(t, dir), openTestSQLite(t, dir)
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for i, conn := range []*Connection{first, second} {
		wg.Add(1)
		go func(i int, conn *Connection) {
			defer wg.Done()
			_, err := execute(conn, "parallel-"+string(rune('a'+i)), "CREATE TABLE t"+string(rune('a'+i))+"(id INTEGER)", 0)
			results <- err
		}(i, conn)
	}
	wg.Wait()
	close(results)
	success, conflict := 0, 0
	for err := range results {
		switch {
		case err == nil:
			success++
		case errors.Is(err, ErrVersion):
			conflict++
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("success=%d conflict=%d", success, conflict)
	}
}

func TestSQLiteDriftCheckRunsInsideMigrationTransaction(t *testing.T) {
	conn := openTestSQLite(t, t.TempDir())
	ctx := context.Background()
	if _, err := execute(conn, "seed", "CREATE TABLE notes (id INTEGER PRIMARY KEY)", 0); err != nil {
		t.Fatal(err)
	}
	snapshot, _ := conn.Snapshot(ctx)
	fingerprint := snapshot.Fingerprint()
	version, err := conn.Execute(ctx, Execution{ID: "fresh", SQL: "ALTER TABLE notes ADD COLUMN title TEXT", ActorID: 1, ActorName: "u", BaseVersion: 1, Fingerprint: fingerprint})
	if err != nil || version != 2 {
		t.Fatalf("matching fingerprint rejected: %v", err)
	}
	_, err = conn.Execute(ctx, Execution{ID: "stale", SQL: "ALTER TABLE notes ADD COLUMN body TEXT", ActorID: 1, ActorName: "u", BaseVersion: 2, Fingerprint: fingerprint})
	if !errors.Is(err, ErrDrift) {
		t.Fatalf("stale fingerprint accepted: %v", err)
	}
}

func TestSQLiteDataDirectoryWithSpaces(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "SEMESTER 5", "WEB APPLICATION%20DEVELOPMENT")
	conn := openTestSQLite(t, dir)
	if _, err := execute(conn, "spaces", "CREATE TABLE notes (id INTEGER)", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, sqliteName)); err != nil {
		t.Fatalf("database file not created in the configured directory: %v", err)
	}
}

func TestSQLiteURIForWindowsPath(t *testing.T) {
	uri := sqliteURI(`C:/Users/user/Documents/SEMESTER 5/data/`+sqliteName, nil)
	if uri != "file:///C:/Users/user/Documents/SEMESTER%205/data/"+sqliteName {
		t.Fatalf("unexpected URI %s", uri)
	}
}
