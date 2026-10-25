package targetdb

import (
	"context"
	"testing"
)

func TestSQLiteRowsWithoutPrimaryKeyHaveStablePages(t *testing.T) {
	for _, tc := range []struct {
		name    string
		columns string
	}{
		{"ordinary", "value INTEGER NOT NULL"},
		{"rowid_column", "value INTEGER NOT NULL, ROWID TEXT"},
		{"two_rowid_columns", "value INTEGER NOT NULL, ROWID TEXT, _ROWID_ TEXT"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conn := openTestSQLite(t, t.TempDir())
			conn.DB.SetMaxOpenConns(1)
			if _, err := conn.DB.Exec("CREATE TABLE unkeyed_events (" + tc.columns + ")"); err != nil {
				t.Fatal(err)
			}
			for value := 1; value <= 75; value++ {
				if _, err := conn.DB.Exec("INSERT INTO unkeyed_events(value) VALUES ($1)", value); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := conn.DB.Exec("PRAGMA reverse_unordered_selects=ON"); err != nil {
				t.Fatal(err)
			}
			checkUnkeyedPages(t, conn, "main")
		})
	}
}

func TestPostgresRowsWithoutPrimaryKeyHaveStablePages(t *testing.T) {
	conn, schema := fixture(t)
	table := Quote(schema) + ".unkeyed_events"
	if _, err := conn.DB.Exec("CREATE TABLE " + table + " (value INTEGER NOT NULL);" +
		"INSERT INTO " + table + " SELECT n FROM generate_series(1,75) n"); err != nil {
		t.Fatal(err)
	}
	checkUnkeyedPages(t, conn, schema)
}

func checkUnkeyedPages(t *testing.T, conn *Connection, schema string) {
	t.Helper()
	for offset := 0; offset < 75; offset += 20 {
		page, err := conn.Rows(context.Background(), schema, "unkeyed_events", 20, offset)
		if err != nil {
			t.Fatal(err)
		}
		wantRows := min(20, 75-offset)
		if len(page.Rows) != wantRows || page.HasMore != (offset+wantRows < 75) {
			t.Fatalf("offset %d: got %d rows, has_more=%v", offset, len(page.Rows), page.HasMore)
		}
		for index, row := range page.Rows {
			if want := int64(offset + index + 1); row[0] != want {
				t.Fatalf("offset %d row %d: got %v, want %d", offset, index, row[0], want)
			}
		}
	}
}
