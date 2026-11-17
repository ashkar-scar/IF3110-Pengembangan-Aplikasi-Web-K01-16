package schemadiff

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/targetdb"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/testdb"
)

// Uji rewrite schema, filter destruktif, dan tolak diff lintas DBMS.
func TestDiffChangesAndRiskSelection(t *testing.T) {
	got := rewriteSchema("CHECK (name <> 'source.demo') REFERENCES source.items(id)", "source", "target")
	want := `CHECK (name <> 'source.demo') REFERENCES "target".items(id)`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}

	old := targetdb.Snapshot{
		Driver: "postgres",
		Tables: []targetdb.Table{{
			Schema:  "public",
			Name:    "obsolete",
			Columns: []targetdb.Column{{Name: "id", Type: "integer"}},
		}},
	}
	desired := targetdb.Snapshot{
		Driver: "postgres",
		Tables: []targetdb.Table{{
			Schema:  "public",
			Name:    "new_table",
			Columns: []targetdb.Column{{Name: "id", Type: "integer", Nullable: false}},
		}},
	}

	safeResult, err := Compare(old, desired, "public", "public", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(safeResult.Changes) != 2 {
		t.Fatalf("got %d changes, want 2", len(safeResult.Changes))
	}
	if strings.Contains(safeResult.SQL, "DROP") || !strings.Contains(safeResult.SQL, "CREATE TABLE") {
		t.Fatalf("safe SQL must include CREATE TABLE without DROP, got: %s", safeResult.SQL)
	}

	destructiveResult, err := Compare(old, desired, "public", "public", true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(destructiveResult.SQL, "DROP TABLE") {
		t.Fatalf("destructive SQL must include DROP TABLE, got: %s", destructiveResult.SQL)
	}

	identicalResult, err := Compare(desired, desired, "public", "public", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(identicalResult.Changes) != 0 || identicalResult.SQL != "" {
		t.Fatalf("identical schemas should produce no diff, got %+v", identicalResult)
	}

	desired.Driver = "sqlite"
	_, err = Compare(old, desired, "public", "main", true)
	if err == nil {
		t.Fatal("cross dialect accepted")
	}
}

// Uji DDL SQLite otomatis untuk tabel baru dan flag manual untuk ubah kolom.
func TestSQLiteGeneratorAndManualChanges(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	current, err := targetdb.OpenSQLite(ctx, dir, "00000000000000000000000000000001.db")
	if err != nil {
		t.Fatal(err)
	}
	defer current.Close()

	desired, err := targetdb.OpenSQLite(ctx, dir, "00000000000000000000000000000002.db")
	if err != nil {
		t.Fatal(err)
	}
	defer desired.Close()

	desiredSQL := "CREATE TABLE items(id INTEGER PRIMARY KEY, name TEXT NOT NULL); " +
		"CREATE INDEX items_name ON items(name)"
	_, err = desired.DB.Exec(desiredSQL)
	if err != nil {
		t.Fatal(err)
	}

	currentSnap, err := current.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	desiredSnap, err := desired.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}

	diff, err := Compare(currentSnap, desiredSnap, "main", "main", false)
	if err != nil {
		t.Fatal(err)
	}
	if diff.Manual {
		t.Fatalf("expected automated SQLite diff, got manual flag: %+v", diff)
	}

	_, err = current.Execute(ctx, targetdb.Execution{
		ID:          "diff",
		SQL:         diff.SQL,
		Fingerprint: currentSnap.Fingerprint(),
	})
	if err != nil {
		t.Fatalf("generated SQL failed (%v): %s", err, diff.SQL)
	}

	afterSnap, err := current.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	afterDiff, err := Compare(afterSnap, desiredSnap, "main", "main", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(afterDiff.Changes) != 0 {
		t.Fatalf("expected empty diff after applying SQL, got %+v", afterDiff.Changes)
	}

	desiredSnap.Tables[0].Columns[1].Type = "INTEGER"
	rebuildDiff, err := Compare(afterSnap, desiredSnap, "main", "main", false)
	if err != nil {
		t.Fatal(err)
	}
	if !rebuildDiff.Manual {
		t.Fatal("SQLite column type change should require manual rebuild")
	}
}

// Pastikan SQL diff PostgreSQL menyamakan kedua schema saat dijalankan.
func TestPostgresGeneratedMigrationMatchesDesired(t *testing.T) {
	dsn := testdb.Target(t)
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(parsed.Port())
	password, _ := parsed.User.Password()

	ctx := context.Background()
	conn, err := targetdb.OpenPostgres(ctx, targetdb.Config{
		Host:     parsed.Hostname(),
		Port:     port,
		Database: strings.TrimPrefix(parsed.Path, "/"),
		Username: parsed.User.Username(),
		SSLMode:  "disable",
	}, password)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	name := fmt.Sprintf("diff_%d", time.Now().UnixNano())
	aSchema, bSchema := name+"_a", name+"_b"
	setupSQL := `
		CREATE SCHEMA ` + aSchema + `;
		CREATE SCHEMA ` + bSchema + `;
		CREATE TABLE ` + aSchema + `.items(id INTEGER PRIMARY KEY);
		CREATE TABLE ` + bSchema + `.items(id INTEGER PRIMARY KEY, name TEXT NOT NULL DEFAULT 'demo');
		CREATE INDEX items_name ON ` + bSchema + `.items(name);
		CREATE TABLE ` + bSchema + `.orders(id INTEGER PRIMARY KEY, item_id INTEGER REFERENCES ` + bSchema + `.items(id))
	`
	_, err = conn.DB.Exec(setupSQL)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.DB.Exec("DROP SCHEMA " + aSchema + " CASCADE; DROP SCHEMA " + bSchema + " CASCADE")

	before, err := conn.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	diff, err := Compare(before, before, aSchema, bSchema, false)
	if err != nil {
		t.Fatal(err)
	}
	if diff.Manual {
		t.Fatalf("expected automated PostgreSQL diff, got manual: %+v", diff)
	}

	version, err := conn.Version(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = conn.Execute(ctx, targetdb.Execution{
		ID:          name,
		SQL:         diff.SQL,
		BaseVersion: version,
		Fingerprint: before.Fingerprint(),
	})
	if err != nil {
		t.Fatalf("generated SQL failed (%v): %s", err, diff.SQL)
	}

	after, err := conn.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	afterDiff, err := Compare(after, after, aSchema, bSchema, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(afterDiff.Changes) != 0 {
		t.Fatalf("generated SQL did not reach desired schema: %+v", afterDiff.Changes)
	}
}

// Pastikan Compare membedakan schema hilang dari schema valid yang kosong.
func TestDiffDistinguishesMissingAndEmptySchemas(t *testing.T) {
	current := targetdb.Snapshot{
		Driver:      "postgres",
		SchemaNames: []string{"public"},
		Tables:      []targetdb.Table{{Schema: "public", Name: "employees"}},
	}
	empty := targetdb.Snapshot{
		Driver:      "postgres",
		SchemaNames: []string{"empty"},
	}

	for _, destructive := range []bool{false, true} {
		for _, names := range [][2]string{{"public", "typo"}, {"typo", "empty"}} {
			result, err := Compare(current, empty, names[0], names[1], destructive)
			if err == nil {
				t.Fatalf("missing schema %v accepted", names)
			}
			if result.SQL != "" || len(result.Changes) != 0 {
				t.Fatalf("missing schema generated SQL: %+v", result)
			}
		}

		dropResult, err := Compare(current, empty, "public", "empty", destructive)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(dropResult.SQL, "DROP TABLE") != destructive {
			t.Fatalf("existing empty schema destructive=%v: %+v", destructive, dropResult)
		}

		createResult, err := Compare(empty, current, "empty", "public", destructive)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(createResult.SQL, "CREATE TABLE") {
			t.Fatalf("empty current schema missing CREATE TABLE: %+v", createResult)
		}
	}
}
