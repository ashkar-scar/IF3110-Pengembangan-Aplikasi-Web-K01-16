package targetdb

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

// Buat schema uji sementara PostgreSQL berisi tabel dan 120 baris contoh.
func fixture(t *testing.T) (*Connection, string) {
	t.Helper()
	config, password := postgresConfig(t)
	conn, err := OpenPostgres(context.Background(), config, password)
	if err != nil {
		t.Fatal(err)
	}

	schema := fmt.Sprintf("fixture_%d", time.Now().UnixNano())
	setupSQL := `
		CREATE SCHEMA ` + schema + `;
		CREATE TABLE ` + schema + `.customers(
			id integer PRIMARY KEY,
			name text NOT NULL,
			email text UNIQUE,
			credit numeric(10,2) CHECK (credit >= 0),
			note text
		);
		CREATE TABLE ` + schema + `.orders(
			id integer PRIMARY KEY,
			customer_id integer REFERENCES ` + schema + `.customers(id)
		);
		CREATE INDEX customer_name_idx ON ` + schema + `.customers(name);
		CREATE TABLE ` + schema + `.schema_migrations(version bigint);
		INSERT INTO ` + schema + `.customers
		SELECT n, 'Nama ' || n, 'user' || n || '@example.test', n * 10.5, NULL
		FROM generate_series(1, 120) AS n
	`
	_, err = conn.DB.Exec(setupSQL)
	if err != nil {
		conn.Close()
		t.Fatal(err)
	}

	t.Cleanup(func() {
		conn.DB.Exec("DROP SCHEMA " + schema + " CASCADE")
		conn.Close()
	})
	return conn, schema
}

// Ambil snapshot DB untuk test.
func snapshotOf(t *testing.T, conn *Connection) Snapshot {
	t.Helper()
	snapshot, err := conn.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

// Pastikan Snapshot membaca kolom, constraint, index, dan menyembunyikan ledger.
func TestSnapshotReadsPostgresCatalog(t *testing.T) {
	conn, schema := fixture(t)
	snapshot := snapshotOf(t, conn)

	if snapshot.Driver != "postgres" {
		t.Fatalf("got driver %q, want postgres", snapshot.Driver)
	}
	if !slices.Contains(snapshot.Schemas(), schema) {
		t.Fatalf("fixture schema %q missing from snapshot: %v", schema, snapshot.Schemas())
	}

	customers, err := snapshot.Find(schema, "customers")
	if err != nil {
		t.Fatal(err)
	}

	columns := map[string]Column{}
	for _, column := range customers.Columns {
		columns[column.Name] = column
	}
	if len(customers.Columns) != 5 {
		t.Fatalf("got %d columns, want 5", len(customers.Columns))
	}
	if columns["id"].Nullable {
		t.Fatal("id column must not be nullable")
	}
	if !columns["note"].Nullable {
		t.Fatal("note column must be nullable")
	}
	if columns["credit"].Type != "numeric(10,2)" {
		t.Fatalf("got credit type %q, want numeric(10,2)", columns["credit"].Type)
	}

	kinds := map[string]string{}
	for _, constraint := range customers.Constraints {
		kinds[constraint.Kind] = constraint.Name
	}
	if kinds["p"] != "customers_pkey" || kinds["u"] != "customers_email_key" || kinds["c"] != "customers_credit_check" {
		t.Fatalf("unexpected constraints: %+v", customers.Constraints)
	}

	indexes := map[string]Index{}
	for _, index := range customers.Indexes {
		indexes[index.Name] = index
	}
	named := indexes["customer_name_idx"]
	if len(customers.Indexes) != 3 {
		t.Fatalf("got %d indexes, want 3", len(customers.Indexes))
	}
	if named.Constraint || named.Unique || fmt.Sprint(named.Columns) != "[name]" {
		t.Fatalf("unexpected non-unique index metadata: %+v", named)
	}
	if !indexes["customers_pkey"].Constraint {
		t.Fatal("customers_pkey must be marked as backing a constraint")
	}

	orders, err := snapshot.Find(schema, "orders")
	if err != nil {
		t.Fatal(err)
	}
	foreign := ""
	for _, constraint := range orders.Constraints {
		if constraint.Kind == "f" {
			foreign = constraint.Definition
		}
	}
	if !strings.Contains(foreign, "REFERENCES "+schema+".customers(id)") {
		t.Fatalf("foreign key not found: %+v", orders.Constraints)
	}

	_, err = snapshot.Find(schema, "schema_migrations")
	if err == nil {
		t.Fatal("ledger table must be hidden from the explorer")
	}
	_, err = snapshot.Find(schema, "missing")
	if err == nil {
		t.Fatal("missing table was found")
	}
}

// Pastikan fingerprint stabil saat skema tetap dan berubah setelah DDL.
func TestFingerprintTracksSchemaChanges(t *testing.T) {
	conn, schema := fixture(t)
	first := snapshotOf(t, conn).Fingerprint()
	if len(first) != 64 {
		t.Fatalf("got fingerprint length %d, want 64", len(first))
	}
	if second := snapshotOf(t, conn).Fingerprint(); second != first {
		t.Fatal("fingerprint must be stable while the schema is unchanged")
	}

	_, err := conn.DB.Exec("ALTER TABLE " + schema + ".customers ADD COLUMN phone text")
	if err != nil {
		t.Fatal(err)
	}
	if snapshotOf(t, conn).Fingerprint() == first {
		t.Fatal("fingerprint did not change after ALTER TABLE")
	}
}

// Pastikan schema PostgreSQL yang masih kosong tetap masuk daftar Schemas.
func TestSnapshotIncludesEmptyPostgresSchema(t *testing.T) {
	conn, schema := fixture(t)
	emptySchema := schema + "_empty"
	_, err := conn.DB.Exec("CREATE SCHEMA " + Quote(emptySchema))
	if err != nil {
		t.Fatal(err)
	}

	snapshot := snapshotOf(t, conn)
	if !slices.Contains(snapshot.Schemas(), emptySchema) {
		t.Fatalf("empty schema %q missing: %v", emptySchema, snapshot.Schemas())
	}
	for _, table := range snapshot.Tables {
		if table.Schema == emptySchema {
			t.Fatal("empty schema unexpectedly contains a table")
		}
	}
}
