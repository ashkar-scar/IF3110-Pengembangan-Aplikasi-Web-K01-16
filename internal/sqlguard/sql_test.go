package sqlguard

import "testing"

func TestSQLBoundaries(t *testing.T) {
	for _, tc := range []struct {
		sql     string
		count   int
		invalid bool
	}{
		{"CREATE TABLE demo(id int); INSERT INTO demo VALUES(1)", 2, false},
		{"INSERT INTO demo VALUES('a;COMMIT; ''b'); -- semicolon ;\nSELECT 1", 2, false},
		{"SELECT $$a;b$$; SELECT $tag$c;d$tag$", 2, false},
		{"/* initial */ SELECT 1;", 1, false},
		{"SELECT 'unterminated", 0, true}, {"/* unterminated", 0, true}, {"/* outer /* nested */ */ SELECT 1", 0, true},
		{"SELECT 'a\\'; COMMIT; --'", 0, true},
	} {
		t.Run(tc.sql, func(t *testing.T) {
			s, err := Split(tc.sql)
			if (err != nil) != tc.invalid || !tc.invalid && len(s) != tc.count {
				t.Fatalf("got %d statements, error %v", len(s), err)
			}
		})
	}
}

func TestTransactionAndLedgerProtection(t *testing.T) {
	for _, sql := range []string{"COMMIT", "SELECT 1; COMMIT; SELECT 2", `DELETE FROM public."schema_migrations"`, "SELECT set_config('role','admin',true)", "ATTACH DATABASE 'other.db' AS other", "SELECT load_extension('unsafe')", "CREATE INDEX CONCURRENTLY x ON y(z)", "CREATE FUNCTION unsafe() RETURNS void AS 'x' LANGUAGE sql", "DO $$ BEGIN COMMIT; END $$"} {
		if _, err := Migration(sql); err == nil {
			t.Errorf("accepted unsafe SQL: %s", sql)
		}
	}
	if _, err := Migration("INSERT INTO demo VALUES('COMMIT schema_migrations')"); err != nil {
		t.Fatal("literal data was treated as SQL")
	}
	if _, err := Plan("SELECT 1;DELETE FROM demo"); err == nil {
		t.Fatal("multi-statement plan accepted")
	}
	if _, err := Plan("ALTER TABLE demo ADD COLUMN name text"); err == nil {
		t.Fatal("unsupported DDL plan accepted")
	}
}

func TestPlanAcceptsSingleQuery(t *testing.T) {
	for _, sql := range []string{"SELECT * FROM demo WHERE id=1;", "UPDATE demo SET name='a;b' WHERE id=1", "DELETE FROM demo", "INSERT INTO demo VALUES(1)", "WITH x AS (SELECT 1) SELECT * FROM x", "-- komentar\nSELECT 1"} {
		statement, err := Plan(sql)
		if err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
		if statement.SQL == "" || statement.SQL[len(statement.SQL)-1] == ';' {
			t.Fatalf("%s: statement not trimmed: %q", sql, statement.SQL)
		}
	}
	for _, sql := range []string{"", "   ", "SELECT 1; SELECT 2", "DROP TABLE demo", "EXPLAIN ANALYZE DELETE FROM demo"} {
		if _, err := Plan(sql); err == nil {
			t.Errorf("accepted %q", sql)
		}
	}
}
