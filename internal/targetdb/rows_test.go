package targetdb

import (
	"context"
	"testing"
	"time"
)

func TestRowsPaginatesByPrimaryKey(t *testing.T) {
	conn, schema := fixture(t)
	ctx := context.Background()
	first, err := conn.Rows(ctx, schema, "customers", 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Rows) != 50 || !first.HasMore || len(first.Columns) != 5 || first.Columns[0] != "id" {
		t.Fatalf("unexpected first page: %d rows, has_more=%v, columns=%v", len(first.Rows), first.HasMore, first.Columns)
	}
	if first.Rows[0][0] != int64(1) || first.Rows[49][0] != int64(50) || first.Rows[0][1] != "Nama 1" || first.Rows[0][4] != nil {
		t.Fatalf("unexpected values: %v", first.Rows[0])
	}
	last, err := conn.Rows(ctx, schema, "customers", 50, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(last.Rows) != 20 || last.HasMore || last.Rows[0][0] != int64(101) {
		t.Fatalf("unexpected last page: %d rows, has_more=%v", len(last.Rows), last.HasMore)
	}
	empty, err := conn.Rows(ctx, schema, "orders", 50, 0)
	if err != nil || len(empty.Rows) != 0 || empty.HasMore {
		t.Fatalf("empty table should return no rows: %+v %v", empty, err)
	}
}

func TestRowsConvertsValuesForJSON(t *testing.T) {
	conn, schema := fixture(t)
	_, err := conn.DB.Exec("CREATE TABLE " + schema + ".events(id bigint PRIMARY KEY, happened_at timestamptz NOT NULL);" +
		"INSERT INTO " + schema + ".events VALUES (9007199254740993, '2026-10-24T10:00:00Z')")
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
	if err != nil || !parsed.Equal(time.Date(2026, 10, 24, 10, 0, 0, 0, time.UTC)) {
		t.Fatalf("unexpected timestamp %q: %v", text, err)
	}
}

func TestRowsRejectsInvalidInput(t *testing.T) {
	conn, schema := fixture(t)
	ctx := context.Background()
	for _, tc := range []struct{ limit, offset int }{{0, 0}, {201, 0}, {50, -1}, {50, 1000001}} {
		if _, err := conn.Rows(ctx, schema, "customers", tc.limit, tc.offset); err == nil {
			t.Errorf("accepted limit=%d offset=%d", tc.limit, tc.offset)
		}
	}
	for _, name := range []string{"missing", `customers"; DROP TABLE orders; --`, "schema_migrations"} {
		if _, err := conn.Rows(ctx, schema, name, 50, 0); err == nil {
			t.Errorf("accepted table name %q", name)
		}
	}
	if _, err := conn.Rows(ctx, schema, "orders", 50, 0); err != nil {
		t.Fatalf("orders table must survive the injection attempt: %v", err)
	}
}
