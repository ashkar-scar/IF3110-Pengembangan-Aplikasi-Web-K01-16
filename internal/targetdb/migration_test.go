package targetdb

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

func currentVersion(t *testing.T, conn *Connection) int64 {
	t.Helper()
	version, err := conn.Version(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return version
}

func TestExecuteAppliesMigrationAndWritesLedger(t *testing.T) {
	conn, schema := fixture(t)
	ctx := context.Background()
	base := currentVersion(t, conn)
	execution := Execution{
		ID:          schema + "-create",
		SQL:         "CREATE TABLE " + schema + ".products(id integer PRIMARY KEY, name text NOT NULL); INSERT INTO " + schema + ".products VALUES (1, 'Buku; tulis')",
		ActorID:     7,
		ActorName:   "editor",
		BaseVersion: base,
	}
	version, err := conn.Execute(ctx, execution)
	if err != nil || version != base+1 || currentVersion(t, conn) != base+1 {
		t.Fatalf("version %d (base %d), error %v", version, base, err)
	}
	page, err := conn.Rows(ctx, schema, "products", 10, 0)
	if err != nil || len(page.Rows) != 1 || page.Rows[0][1] != "Buku; tulis" {
		t.Fatalf("migration result not visible: %+v %v", page, err)
	}
	found, ok, err := conn.Lookup(ctx, execution.ID)
	if err != nil || !ok || found != version {
		t.Fatalf("lookup returned %d %v %v", found, ok, err)
	}
	again, err := conn.Execute(ctx, execution)
	if err != nil || again != version || currentVersion(t, conn) != version {
		t.Fatalf("retrying the same attempt must not run twice: %d %v", again, err)
	}
	if _, ok, err = conn.Lookup(ctx, schema+"-missing"); err != nil || ok {
		t.Fatalf("unknown attempt found: %v %v", ok, err)
	}
}

func TestExecuteRollsBackEveryStatementOnFailure(t *testing.T) {
	conn, schema := fixture(t)
	ctx := context.Background()
	base := currentVersion(t, conn)
	_, err := conn.Execute(ctx, Execution{
		ID:          schema + "-fail",
		SQL:         "INSERT INTO " + schema + ".customers(id, name) VALUES (500, 'Baru'); UPDATE " + schema + ".customers SET credit = -1 WHERE id = 1",
		BaseVersion: base,
	})
	if err == nil || !strings.Contains(err.Error(), "statement 2") || !strings.Contains(err.Error(), "23514") {
		t.Fatalf("expected statement 2 to fail with a check violation, got %v", err)
	}
	var count int
	if err = conn.DB.QueryRow("SELECT count(*) FROM " + schema + ".customers").Scan(&count); err != nil || count != 120 {
		t.Fatalf("first statement was not rolled back: %d %v", count, err)
	}
	if currentVersion(t, conn) != base {
		t.Fatal("failed migration must not change the version")
	}
	if _, ok, _ := conn.Lookup(ctx, schema+"-fail"); ok {
		t.Fatal("failed migration must not be written to the ledger")
	}
}

func TestExecuteRejectsStaleVersionDriftAndUnsafeSQL(t *testing.T) {
	conn, schema := fixture(t)
	ctx := context.Background()
	base := currentVersion(t, conn)
	add := "ALTER TABLE " + schema + ".customers ADD COLUMN phone text"
	if _, err := conn.Execute(ctx, Execution{ID: schema + "-stale", SQL: add, BaseVersion: base + 5}); !errors.Is(err, ErrVersion) {
		t.Fatalf("expected ErrVersion, got %v", err)
	}
	fingerprint := snapshotOf(t, conn).Fingerprint()
	if _, err := conn.DB.Exec("ALTER TABLE " + schema + ".customers ADD COLUMN outside text"); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Execute(ctx, Execution{ID: schema + "-drift", SQL: add, BaseVersion: base, Fingerprint: fingerprint}); !errors.Is(err, ErrDrift) {
		t.Fatalf("expected ErrDrift, got %v", err)
	}
	for _, unsafe := range []string{
		add + "; COMMIT",
		"DELETE FROM public.schema_migrations",
		"SELECT pg_sleep(1)",
	} {
		if _, err := conn.Execute(ctx, Execution{ID: schema + "-unsafe", SQL: unsafe, BaseVersion: base}); err == nil {
			t.Errorf("accepted unsafe SQL %q", unsafe)
		}
	}
	if currentVersion(t, conn) != base {
		t.Fatal("rejected migrations must not change the version")
	}
	fingerprint = snapshotOf(t, conn).Fingerprint()
	if _, err := conn.Execute(ctx, Execution{ID: schema + "-ok", SQL: add, BaseVersion: base, Fingerprint: fingerprint}); err != nil {
		t.Fatalf("fresh fingerprint must be accepted: %v", err)
	}
}

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
	if success != 1 || currentVersion(t, conn) != base+1 {
		t.Fatalf("expected exactly one winner, got %d (version %d, base %d)", success, currentVersion(t, conn), base)
	}
}
