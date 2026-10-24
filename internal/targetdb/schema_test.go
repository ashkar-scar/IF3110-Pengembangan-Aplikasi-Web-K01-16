package targetdb

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

func fixture(t *testing.T) (*Connection, string) {
	t.Helper()
	config, password := postgresConfig(t)
	conn, err := OpenPostgres(context.Background(), config, password)
	if err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("fixture_%d", time.Now().UnixNano())
	_, err = conn.DB.Exec("CREATE SCHEMA " + schema + ";" +
		"CREATE TABLE " + schema + ".customers(id integer PRIMARY KEY, name text NOT NULL, email text UNIQUE, credit numeric(10,2) CHECK (credit >= 0), note text);" +
		"CREATE TABLE " + schema + ".orders(id integer PRIMARY KEY, customer_id integer REFERENCES " + schema + ".customers(id));" +
		"CREATE INDEX customer_name_idx ON " + schema + ".customers(name);" +
		"CREATE TABLE " + schema + ".schema_migrations(version bigint);" +
		"INSERT INTO " + schema + ".customers SELECT n, 'Nama ' || n, 'user' || n || '@example.test', n * 10.5, NULL FROM generate_series(1,120) n")
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

func snapshotOf(t *testing.T, conn *Connection) Snapshot {
	t.Helper()
	snapshot, err := conn.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func TestSnapshotReadsPostgresCatalog(t *testing.T) {
	conn, schema := fixture(t)
	snapshot := snapshotOf(t, conn)
	if snapshot.Driver != "postgres" || !slices.Contains(snapshot.Schemas(), schema) {
		t.Fatalf("fixture schema missing from snapshot: %v", snapshot.Schemas())
	}
	customers, err := snapshot.Find(schema, "customers")
	if err != nil {
		t.Fatal(err)
	}
	columns := map[string]Column{}
	for _, column := range customers.Columns {
		columns[column.Name] = column
	}
	if len(customers.Columns) != 5 || columns["id"].Nullable || !columns["note"].Nullable || columns["credit"].Type != "numeric(10,2)" {
		t.Fatalf("unexpected columns: %+v", customers.Columns)
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
	if len(customers.Indexes) != 3 || named.Constraint || named.Unique || fmt.Sprint(named.Columns) != "[name]" || !indexes["customers_pkey"].Constraint {
		t.Fatalf("unexpected indexes: %+v", customers.Indexes)
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
	if _, err = snapshot.Find(schema, "schema_migrations"); err == nil {
		t.Fatal("ledger table must be hidden from the explorer")
	}
	if _, err = snapshot.Find(schema, "missing"); err == nil {
		t.Fatal("missing table was found")
	}
}

func TestFingerprintTracksSchemaChanges(t *testing.T) {
	conn, schema := fixture(t)
	first := snapshotOf(t, conn).Fingerprint()
	if len(first) != 64 || snapshotOf(t, conn).Fingerprint() != first {
		t.Fatal("fingerprint must be stable while the schema is unchanged")
	}
	if _, err := conn.DB.Exec("ALTER TABLE " + schema + ".customers ADD COLUMN phone text"); err != nil {
		t.Fatal(err)
	}
	if snapshotOf(t, conn).Fingerprint() == first {
		t.Fatal("fingerprint did not change after ALTER TABLE")
	}
}
