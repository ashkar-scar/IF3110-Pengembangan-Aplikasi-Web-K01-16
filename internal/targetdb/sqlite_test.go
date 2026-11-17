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

// Buka database SQLite sementara untuk satu unit test.
func openTestSQLite(t *testing.T, dir string) *Connection {
	t.Helper()
	conn, err := OpenSQLite(context.Background(), dir, sqliteName)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

// Jalankan satu attempt migration dengan aktor default test.
func execute(conn *Connection, id, sql string, base int64) (int64, error) {
	return conn.Execute(context.Background(), Execution{
		ID:          id,
		SQL:         sql,
		ActorID:     1,
		ActorName:   "editor",
		BaseVersion: base,
	})
}

const shopSchema = `
	CREATE TABLE customers (
		id INTEGER PRIMARY KEY,
		name TEXT NOT NULL,
		email TEXT UNIQUE,
		credit REAL DEFAULT 0 CHECK (credit >= 0)
	);
	CREATE TABLE orders (
		id INTEGER PRIMARY KEY,
		customer_id INTEGER NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
		note TEXT
	);
	CREATE INDEX orders_customer_idx ON orders(customer_id);
	INSERT INTO customers(id, name, email) VALUES (1, 'Rani', 'rani@example.test'), (2, 'Bayu', NULL);
	INSERT INTO orders(customer_id, note) VALUES (1, 'a'), (1, 'b'), (2, 'c')
`

// Pastikan OpenSQLite menolak nama file non-32-hex atau path traversal.
func TestSQLiteRejectsFilesOutsideDataDirectory(t *testing.T) {
	dir := t.TempDir()
	invalidNames := []string{
		"../escape.db",
		"notes.db",
		filepath.Join(dir, sqliteName),
		sqliteName + "?mode=memory",
		"file:" + sqliteName,
	}
	for _, name := range invalidNames {
		_, err := OpenSQLite(context.Background(), dir, name)
		if err == nil {
			t.Errorf("accepted file name %q", name)
		}
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("invalid file name created %d entries", len(entries))
	}
}

// Uji alur migration SQLite: snapshot, explorer, rollback DDL, idempoten, dan cek versi.
func TestSQLiteMigrationExplorerAndRollback(t *testing.T) {
	conn := openTestSQLite(t, t.TempDir())
	ctx := context.Background()

	version, err := execute(conn, "attempt-1", shopSchema, 0)
	if err != nil {
		t.Fatalf("seed migration failed: %v", err)
	}
	if version != 1 {
		t.Fatalf("got version %d, want 1", version)
	}

	t.Run("snapshot dan pagination explorer", func(t *testing.T) {
		snapshot, err := conn.Snapshot(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(snapshot.Tables) != 2 {
			t.Fatalf("got %d tables, want 2", len(snapshot.Tables))
		}
		if snapshot.Schemas()[0] != "main" {
			t.Fatalf("got schema %q, want main", snapshot.Schemas()[0])
		}

		orders, err := snapshot.Find("main", "orders")
		if err != nil {
			t.Fatal(err)
		}
		kinds := map[string]string{}
		for _, constraint := range orders.Constraints {
			kinds[constraint.Kind] = constraint.Definition
		}
		if kinds["p"] != `PRIMARY KEY ("id")` {
			t.Fatalf("unexpected primary key constraint: %q", kinds["p"])
		}
		if kinds["f"] != `FOREIGN KEY ("customer_id") REFERENCES "customers"("id") ON DELETE CASCADE` {
			t.Fatalf("unexpected foreign key constraint: %q", kinds["f"])
		}
		if len(orders.Indexes) != 1 {
			t.Fatalf("got %d indexes, want 1", len(orders.Indexes))
		}
		if orders.Indexes[0].Definition != "CREATE INDEX orders_customer_idx ON orders(customer_id)" {
			t.Fatalf("unexpected index definition: %+v", orders.Indexes[0])
		}

		customers, err := snapshot.Find("main", "customers")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(customers.Definition, "CHECK (credit >= 0)") {
			t.Fatalf("raw CHECK definition missing from %q", customers.Definition)
		}
		if customers.Columns[3].Default == nil {
			t.Fatalf("expected non-nil default on credit column: %+v", customers.Columns[3])
		}
		if *customers.Columns[3].Default != "0" {
			t.Fatalf("unexpected default on credit column: %q", *customers.Columns[3].Default)
		}

		page, err := conn.Rows(ctx, "main", "orders", 2, 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Rows) != 2 {
			t.Fatalf("got %d rows, want 2", len(page.Rows))
		}
		if !page.HasMore {
			t.Fatal("expected HasMore=true")
		}
		if page.Columns[2] != "note" {
			t.Fatalf("got third column %q, want note", page.Columns[2])
		}
	})

	t.Run("rollback DDL dan enforcement foreign key", func(t *testing.T) {
		_, err := execute(conn, "attempt-2", "ALTER TABLE customers ADD COLUMN phone TEXT; INSERT INTO missing VALUES (1)", 1)
		if err == nil {
			t.Fatal("expected statement 2 failure, got nil error")
		}
		if !strings.Contains(err.Error(), "statement 2") {
			t.Fatalf("expected statement 2 failure, got %v", err)
		}

		snapshot, err := conn.Snapshot(ctx)
		if err != nil {
			t.Fatal(err)
		}
		customers, err := snapshot.Find("main", "customers")
		if err != nil {
			t.Fatal(err)
		}
		if len(customers.Columns) != 4 {
			t.Fatalf("rolled back DDL left %d columns, want 4", len(customers.Columns))
		}

		_, err = execute(conn, "attempt-3", "INSERT INTO orders(customer_id) VALUES (99)", 1)
		if err == nil {
			t.Fatal("foreign key was not enforced")
		}

		version, err := conn.Version(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if version != 1 {
			t.Fatalf("got version=%d, want 1", version)
		}
	})

	t.Run("eksekusi ulang idempoten dan tolak versi lama", func(t *testing.T) {
		again, err := execute(conn, "attempt-1", shopSchema, 0)
		if err != nil {
			t.Fatal(err)
		}
		if again != 1 {
			t.Fatalf("repeated attempt returned version=%d, want 1", again)
		}

		found, ok, err := conn.Lookup(ctx, "attempt-1")
		if err != nil {
			t.Fatal(err)
		}
		if !ok || found != 1 {
			t.Fatalf("ledger lookup returned found=%d ok=%v", found, ok)
		}

		_, ok, err = conn.Lookup(ctx, "attempt-2")
		if err != nil {
			t.Fatal(err)
		}
		if ok {
			t.Fatal("failed attempt found in ledger")
		}

		_, err = execute(conn, "attempt-4", "INSERT INTO orders(customer_id) VALUES (2)", 0)
		if !errors.Is(err, ErrVersion) {
			t.Fatalf("stale base version accepted: %v", err)
		}
	})
}

// Pastikan SQLite menolak statement berbahaya dan tidak membuat file lain.
func TestSQLiteRejectsUnsafeStatements(t *testing.T) {
	dir := t.TempDir()
	conn := openTestSQLite(t, dir)

	unsafeQueries := []string{
		"ATTACH DATABASE 'other.db' AS other",
		"PRAGMA foreign_keys=OFF",
		"CREATE TABLE a(id INTEGER); DELETE FROM schema_migrations",
		"VACUUM INTO 'copy.db'",
		"CREATE TEMP TABLE scratch(id INTEGER)",
		"SELECT load_extension('evil')",
	}
	for i, sql := range unsafeQueries {
		_, err := execute(conn, "unsafe-"+string(rune('a'+i)), sql, 0)
		if err == nil {
			t.Errorf("accepted %q", sql)
		}
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() != sqliteName {
			t.Fatalf("unexpected file created: %s", entry.Name())
		}
	}
}

// Pastikan dua migration bersamaan berjalan berurutan (satu menang, satu konflik versi).
func TestSQLiteConcurrentMigrationsStayLinear(t *testing.T) {
	dir := t.TempDir()
	first := openTestSQLite(t, dir)
	second := openTestSQLite(t, dir)

	results := make(chan error, 2)
	var wg sync.WaitGroup
	for i, conn := range []*Connection{first, second} {
		wg.Add(1)
		go func(i int, conn *Connection) {
			defer wg.Done()
			suffix := string(rune('a' + i))
			_, err := execute(conn, "parallel-"+suffix, "CREATE TABLE t"+suffix+"(id INTEGER)", 0)
			results <- err
		}(i, conn)
	}
	wg.Wait()
	close(results)

	success := 0
	conflict := 0
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
		t.Fatalf("got success=%d conflict=%d, want 1 and 1", success, conflict)
	}
}

// Pastikan cek schema drift berjalan di dalam transaksi SQLite.
func TestSQLiteDriftCheckRunsInsideMigrationTransaction(t *testing.T) {
	conn := openTestSQLite(t, t.TempDir())
	ctx := context.Background()

	_, err := execute(conn, "seed", "CREATE TABLE notes (id INTEGER PRIMARY KEY)", 0)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := conn.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	fingerprint := snapshot.Fingerprint()

	version, err := conn.Execute(ctx, Execution{
		ID:          "fresh",
		SQL:         "ALTER TABLE notes ADD COLUMN title TEXT",
		ActorID:     1,
		ActorName:   "u",
		BaseVersion: 1,
		Fingerprint: fingerprint,
	})
	if err != nil {
		t.Fatal(err)
	}
	if version != 2 {
		t.Fatalf("got version=%d, want 2", version)
	}

	_, err = conn.Execute(ctx, Execution{
		ID:          "stale",
		SQL:         "ALTER TABLE notes ADD COLUMN body TEXT",
		ActorID:     1,
		ActorName:   "u",
		BaseVersion: 2,
		Fingerprint: fingerprint,
	})
	if !errors.Is(err, ErrDrift) {
		t.Fatalf("stale fingerprint accepted: %v", err)
	}
}

// Pastikan direktori data dengan spasi dan '%' tetap terbuka.
func TestSQLiteDataDirectoryWithSpaces(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "SEMESTER 5", "WEB APPLICATION%20DEVELOPMENT")
	conn := openTestSQLite(t, dir)
	_, err := execute(conn, "spaces", "CREATE TABLE notes (id INTEGER)", 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = os.Stat(filepath.Join(dir, sqliteName))
	if err != nil {
		t.Fatalf("database file not created in the configured directory: %v", err)
	}
}

// Pastikan path Windows dengan spasi di-encode jadi file URI yang valid.
func TestSQLiteURIForWindowsPath(t *testing.T) {
	uri := sqliteURI(`C:/Users/user/Documents/SEMESTER 5/data/`+sqliteName, nil)
	want := "file:///C:/Users/user/Documents/SEMESTER%205/data/" + sqliteName
	if uri != want {
		t.Fatalf("got URI %s, want %s", uri, want)
	}
}
