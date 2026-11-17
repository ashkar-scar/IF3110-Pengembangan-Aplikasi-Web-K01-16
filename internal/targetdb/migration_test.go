package targetdb

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

// Ambil versi DB saat ini.
func currentVersion(t *testing.T, conn *Connection) int64 {
	t.Helper()
	version, err := conn.Version(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return version
}

// Uji eksekusi migration, ledger, idempotency retry, dan lookup attempt.
func TestExecuteAppliesMigrationAndWritesLedger(t *testing.T) {
	conn, schema := fixture(t)
	ctx := context.Background()
	base := currentVersion(t, conn)

	execution := Execution{
		ID: schema + "-create",
		SQL: `CREATE TABLE ` + schema + `.products(id integer PRIMARY KEY, name text NOT NULL);
INSERT INTO ` + schema + `.products VALUES (1, 'Buku; tulis')`,
		ActorID:     7,
		ActorName:   "editor",
		BaseVersion: base,
	}

	version, err := conn.Execute(ctx, execution)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if version != base+1 {
		t.Fatalf("got version %d, want %d", version, base+1)
	}
	if got := currentVersion(t, conn); got != base+1 {
		t.Fatalf("got stored version %d, want %d", got, base+1)
	}

	page, err := conn.Rows(ctx, schema, "products", 10, 0)
	if err != nil {
		t.Fatalf("Rows failed: %v", err)
	}
	if len(page.Rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(page.Rows))
	}
	if page.Rows[0][1] != "Buku; tulis" {
		t.Fatalf("got product name %v, want %q", page.Rows[0][1], "Buku; tulis")
	}

	found, ok, err := conn.Lookup(ctx, execution.ID)
	if err != nil {
		t.Fatalf("Lookup failed: %v", err)
	}
	if !ok || found != version {
		t.Fatalf("lookup returned found=%v version=%d, want true and %d", ok, found, version)
	}

	// Execute ulang attempt ID yang sama mengembalikan versi lama tanpa eksekusi ulang.
	again, err := conn.Execute(ctx, execution)
	if err != nil {
		t.Fatalf("idempotent retry failed: %v", err)
	}
	if again != version {
		t.Fatalf("retry returned version %d, want %d", again, version)
	}
	if got := currentVersion(t, conn); got != version {
		t.Fatalf("retry changed stored version to %d, want %d", got, version)
	}

	_, ok, err = conn.Lookup(ctx, schema+"-missing")
	if err != nil {
		t.Fatalf("Lookup for missing attempt failed: %v", err)
	}
	if ok {
		t.Fatal("missing attempt was unexpectedly found in ledger")
	}
}

// Pastikan gagal di satu statement me-rollback seluruh transaksi.
func TestExecuteRollsBackEveryStatementOnFailure(t *testing.T) {
	conn, schema := fixture(t)
	ctx := context.Background()
	base := currentVersion(t, conn)

	sql := `INSERT INTO ` + schema + `.customers(id, name) VALUES (500, 'Baru');
UPDATE ` + schema + `.customers SET credit = -1 WHERE id = 1`
	_, err := conn.Execute(ctx, Execution{
		ID:          schema + "-fail",
		SQL:         sql,
		BaseVersion: base,
	})
	if err == nil {
		t.Fatal("expected statement 2 to fail, got nil error")
	}
	if !strings.Contains(err.Error(), "statement 2") {
		t.Fatalf("expected statement 2 reference in error, got %v", err)
	}
	if !strings.Contains(err.Error(), "23514") {
		t.Fatalf("expected SQLSTATE 23514 (check violation), got %v", err)
	}

	var count int
	err = conn.DB.QueryRow("SELECT count(*) FROM " + schema + ".customers").Scan(&count)
	if err != nil {
		t.Fatalf("count query failed: %v", err)
	}
	if count != 120 {
		t.Fatalf("first statement was not rolled back: got %d customers, want 120", count)
	}
	if got := currentVersion(t, conn); got != base {
		t.Fatalf("failed migration changed version to %d, want %d", got, base)
	}
	_, ok, err := conn.Lookup(ctx, schema+"-fail")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("failed migration must not be written to the ledger")
	}
}

// Pastikan Execute menolak versi lama, schema drift, dan SQL berbahaya.
func TestExecuteRejectsStaleVersionDriftAndUnsafeSQL(t *testing.T) {
	conn, schema := fixture(t)
	ctx := context.Background()
	base := currentVersion(t, conn)
	addColumn := "ALTER TABLE " + schema + ".customers ADD COLUMN phone text"

	_, err := conn.Execute(ctx, Execution{
		ID:          schema + "-stale",
		SQL:         addColumn,
		BaseVersion: base + 5,
	})
	if !errors.Is(err, ErrVersion) {
		t.Fatalf("expected ErrVersion, got %v", err)
	}

	fingerprint := snapshotOf(t, conn).Fingerprint()
	_, err = conn.DB.Exec("ALTER TABLE " + schema + ".customers ADD COLUMN outside text")
	if err != nil {
		t.Fatal(err)
	}

	_, err = conn.Execute(ctx, Execution{
		ID:          schema + "-drift",
		SQL:         addColumn,
		BaseVersion: base,
		Fingerprint: fingerprint,
	})
	if !errors.Is(err, ErrDrift) {
		t.Fatalf("expected ErrDrift, got %v", err)
	}

	unsafeQueries := []string{
		addColumn + "; COMMIT",
		"DELETE FROM public.schema_migrations",
		"SELECT pg_sleep(1)",
	}
	for _, unsafe := range unsafeQueries {
		_, err := conn.Execute(ctx, Execution{
			ID:          schema + "-unsafe",
			SQL:         unsafe,
			BaseVersion: base,
		})
		if err == nil {
			t.Errorf("accepted unsafe SQL %q", unsafe)
		}
	}

	if got := currentVersion(t, conn); got != base {
		t.Fatalf("rejected migrations changed version to %d, want %d", got, base)
	}

	freshFingerprint := snapshotOf(t, conn).Fingerprint()
	_, err = conn.Execute(ctx, Execution{
		ID:          schema + "-ok",
		SQL:         addColumn,
		BaseVersion: base,
		Fingerprint: freshFingerprint,
	})
	if err != nil {
		t.Fatalf("fresh fingerprint must be accepted: %v", err)
	}
}

// Pastikan migration paralel tetap berurutan dan hanya satu yang menang.
func TestConcurrentMigrationsStayLinear(t *testing.T) {
	conn, schema := fixture(t)
	base := currentVersion(t, conn)

	var wg sync.WaitGroup
	results := make([]error, 5)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, results[i] = conn.Execute(context.Background(), Execution{
				ID:          schema + "-race-" + string(rune('a'+i)),
				SQL:         "UPDATE " + schema + ".customers SET note = 'race' WHERE id = 1",
				BaseVersion: base,
			})
		}()
	}
	wg.Wait()

	success := 0
	for _, err := range results {
		if err == nil {
			success++
		} else if !errors.Is(err, ErrVersion) {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	if success != 1 {
		t.Fatalf("expected 1 winning migration, got %d", success)
	}
	if got := currentVersion(t, conn); got != base+1 {
		t.Fatalf("expected version %d, got %d", base+1, got)
	}
}
