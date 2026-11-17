package targetdb

import (
	"context"
	"testing"
	"time"
)

// Uji pagination baris berdasar primary key (halaman penuh, akhir, dan kosong).
func TestRowsPaginatesByPrimaryKey(t *testing.T) {
	conn, schema := fixture(t)
	ctx := context.Background()

	first, err := conn.Rows(ctx, schema, "customers", 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Rows) != 50 {
		t.Fatalf("got %d rows on first page, want 50", len(first.Rows))
	}
	if !first.HasMore {
		t.Fatal("expected HasMore=true on first page")
	}
	if len(first.Columns) != 5 || first.Columns[0] != "id" {
		t.Fatalf("unexpected columns: %v", first.Columns)
	}
	if first.Rows[0][0] != int64(1) || first.Rows[49][0] != int64(50) {
		t.Fatalf("unexpected first/last row IDs: %v .. %v", first.Rows[0][0], first.Rows[49][0])
	}
	if first.Rows[0][1] != "Nama 1" || first.Rows[0][4] != nil {
		t.Fatalf("unexpected row values: %v", first.Rows[0])
	}

	last, err := conn.Rows(ctx, schema, "customers", 50, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(last.Rows) != 20 {
		t.Fatalf("got %d rows on last page, want 20", len(last.Rows))
	}
	if last.HasMore {
		t.Fatal("expected HasMore=false on last page")
	}
	if last.Rows[0][0] != int64(101) {
		t.Fatalf("got first row ID %v on last page, want 101", last.Rows[0][0])
	}

	empty, err := conn.Rows(ctx, schema, "orders", 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(empty.Rows) != 0 || empty.HasMore {
		t.Fatalf("empty table should return 0 rows and HasMore=false, got %d rows and HasMore=%v", len(empty.Rows), empty.HasMore)
	}
}

// Uji konversi bigint besar dan timestamp agar aman untuk JSON.
func TestRowsConvertsValuesForJSON(t *testing.T) {
	conn, schema := fixture(t)
	setupSQL := `
		CREATE TABLE ` + schema + `.events(id bigint PRIMARY KEY, happened_at timestamptz NOT NULL);
		INSERT INTO ` + schema + `.events VALUES (9007199254740993, '2026-10-24T10:00:00Z')
	`
	_, err := conn.DB.Exec(setupSQL)
	if err != nil {
		t.Fatal(err)
	}

	page, err := conn.Rows(context.Background(), schema, "events", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if page.Rows[0][0] != "9007199254740993" {
		t.Fatalf("large bigint must become a string, got %#v", page.Rows[0][0])
	}

	text, ok := page.Rows[0][1].(string)
	if !ok {
		t.Fatalf("timestamp must become a string, got %#v", page.Rows[0][1])
	}
	parsed, err := time.Parse(time.RFC3339Nano, text)
	if err != nil {
		t.Fatalf("failed to parse timestamp %q: %v", text, err)
	}
	wantTime := time.Date(2026, 10, 24, 10, 0, 0, 0, time.UTC)
	if !parsed.Equal(wantTime) {
		t.Fatalf("got timestamp %v, want %v", parsed, wantTime)
	}
}

// Pastikan Rows menolak pagination ilegal dan injeksi nama tabel.
func TestRowsRejectsInvalidInput(t *testing.T) {
	conn, schema := fixture(t)
	ctx := context.Background()

	invalidPagination := []struct{ limit, offset int }{
		{0, 0},
		{201, 0},
		{50, -1},
		{50, 1000001},
	}
	for _, tc := range invalidPagination {
		_, err := conn.Rows(ctx, schema, "customers", tc.limit, tc.offset)
		if err == nil {
			t.Errorf("accepted limit=%d offset=%d", tc.limit, tc.offset)
		}
	}

	invalidTables := []string{
		"missing",
		`customers"; DROP TABLE orders; --`,
		"schema_migrations",
	}
	for _, name := range invalidTables {
		_, err := conn.Rows(ctx, schema, name, 50, 0)
		if err == nil {
			t.Errorf("accepted table name %q", name)
		}
	}

	_, err := conn.Rows(ctx, schema, "orders", 50, 0)
	if err != nil {
		t.Fatalf("orders table must survive the injection attempt: %v", err)
	}
}
