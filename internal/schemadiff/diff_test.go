package schemadiff

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/targetdb"
)

func TestDiffChangesAndRiskSelection(t *testing.T) {
	if got := rewriteSchema("CHECK (name <> 'source.demo') REFERENCES source.items(id)", "source", "target"); got != "CHECK (name <> 'source.demo') REFERENCES \"target\".items(id)" {
		t.Fatal(got)
	}
	old := targetdb.Snapshot{Driver: "postgres", Tables: []targetdb.Table{{Schema: "public", Name: "obsolete", Columns: []targetdb.Column{{Name: "id", Type: "integer"}}}}}
	desired := targetdb.Snapshot{Driver: "postgres", Tables: []targetdb.Table{{Schema: "public", Name: "new_table", Columns: []targetdb.Column{{Name: "id", Type: "integer", Nullable: false}}}}}
	r, err := Compare(old, desired, "public", "public", false)
	if err != nil || len(r.Changes) != 2 || strings.Contains(r.SQL, "DROP") || !strings.Contains(r.SQL, "CREATE TABLE") {
		t.Fatal(r, err)
	}
	r, err = Compare(old, desired, "public", "public", true)
	if err != nil || !strings.Contains(r.SQL, "DROP TABLE") {
		t.Fatal(r, err)
	}
	r, err = Compare(desired, desired, "public", "public", true)
	if err != nil || len(r.Changes) != 0 || r.SQL != "" {
		t.Fatal(r, err)
	}
	desired.Driver = "sqlite"
	if _, err := Compare(old, desired, "public", "main", true); err == nil {
		t.Fatal("cross dialect accepted")
	}
}

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
	if _, err = desired.DB.Exec("CREATE TABLE items(id INTEGER PRIMARY KEY, name TEXT NOT NULL); CREATE INDEX items_name ON items(name)"); err != nil {
		t.Fatal(err)
	}
	a, _ := current.Snapshot(ctx)
	b, _ := desired.Snapshot(ctx)
	r, err := Compare(a, b, "main", "main", false)
	if err != nil || r.Manual {
		t.Fatal(r, err)
	}
	if _, err = current.Execute(ctx, targetdb.Execution{ID: "diff", SQL: r.SQL, Fingerprint: a.Fingerprint()}); err != nil {
		t.Fatal(err, r.SQL)
	}
	a, _ = current.Snapshot(ctx)
	r, err = Compare(a, b, "main", "main", false)
	if err != nil || len(r.Changes) != 0 {
		t.Fatal(r, err)
	}
	b.Tables[0].Columns[1].Type = "INTEGER"
	r, _ = Compare(a, b, "main", "main", false)
	if !r.Manual {
		t.Fatal("SQLite rebuild should be manual")
	}
}

func TestPostgresGeneratedMigrationMatchesDesired(t *testing.T) {
	dsn := os.Getenv("TEST_TARGET_DSN")
	if dsn == "" {
		t.Skip("set TEST_TARGET_DSN")
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(u.Port())
	password, _ := u.User.Password()
	ctx := context.Background()
	conn, err := targetdb.OpenPostgres(ctx, targetdb.Config{Host: u.Hostname(), Port: port, Database: strings.TrimPrefix(u.Path, "/"), Username: u.User.Username(), SSLMode: "disable"}, password)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	name := fmt.Sprintf("diff_%d", time.Now().UnixNano())
	aSchema, bSchema := name+"_a", name+"_b"
	_, err = conn.DB.Exec("CREATE SCHEMA " + aSchema + ";CREATE SCHEMA " + bSchema + ";CREATE TABLE " + aSchema + ".items(id INTEGER PRIMARY KEY);CREATE TABLE " + bSchema + ".items(id INTEGER PRIMARY KEY,name TEXT NOT NULL DEFAULT 'demo');CREATE INDEX items_name ON " + bSchema + ".items(name);CREATE TABLE " + bSchema + ".orders(id INTEGER PRIMARY KEY,item_id INTEGER REFERENCES " + bSchema + ".items(id))")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.DB.Exec("DROP SCHEMA " + aSchema + " CASCADE;DROP SCHEMA " + bSchema + " CASCADE")
	before, err := conn.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	r, err := Compare(before, before, aSchema, bSchema, false)
	if err != nil || r.Manual {
		t.Fatal(r, err)
	}
	version, err := conn.Version(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = conn.Execute(ctx, targetdb.Execution{ID: name, SQL: r.SQL, BaseVersion: version, Fingerprint: before.Fingerprint()}); err != nil {
		t.Fatal(err, r.SQL)
	}
	after, err := conn.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	r, err = Compare(after, after, aSchema, bSchema, true)
	if err != nil || len(r.Changes) != 0 {
		t.Fatal("generated SQL did not reach desired schema", r, err)
	}
}
