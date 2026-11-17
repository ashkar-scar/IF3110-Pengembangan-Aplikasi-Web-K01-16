package sqlguard

import "testing"

// Uji pemecahan statement SQL dan penolakan kutip atau komentar tak valid.
func TestSQLBoundaries(t *testing.T) {
	cases := []struct {
		sql     string
		count   int
		invalid bool
	}{
		{"CREATE TABLE demo(id int); INSERT INTO demo VALUES(1)", 2, false},
		{"INSERT INTO demo VALUES('a;COMMIT; ''b'); -- semicolon ;\nSELECT 1", 2, false},
		{"SELECT $$a;b$$; SELECT $tag$c;d$tag$", 2, false},
		{"/* initial */ SELECT 1;", 1, false},
		{"SELECT 'unterminated", 0, true},
		{"/* unterminated", 0, true},
		{"/* outer /* nested */ */ SELECT 1", 0, true},
		{"SELECT 'a\\'; COMMIT; --'", 0, true},
	}

	for _, tc := range cases {
		t.Run(tc.sql, func(t *testing.T) {
			statements, err := Split(tc.sql)
			if tc.invalid {
				if err == nil {
					t.Fatalf("expected error for invalid SQL %q, got %d statements", tc.sql, len(statements))
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tc.sql, err)
			}
			if len(statements) != tc.count {
				t.Fatalf("got %d statements, want %d", len(statements), tc.count)
			}
		})
	}
}

// Pastikan kontrol transaksi manual dan akses ledger ditolak.
func TestTransactionAndLedgerProtection(t *testing.T) {
	unsafeQueries := []string{
		"COMMIT",
		"SELECT 1; COMMIT; SELECT 2",
		`DELETE FROM public."schema_migrations"`,
		"SELECT set_config('role','admin',true)",
		"ATTACH DATABASE 'other.db' AS other",
		"SELECT load_extension('unsafe')",
		"CREATE INDEX CONCURRENTLY x ON y(z)",
		"CREATE FUNCTION unsafe() RETURNS void AS 'x' LANGUAGE sql",
		"DO $$ BEGIN COMMIT; END $$",
	}
	for _, sql := range unsafeQueries {
		_, err := Migration(sql)
		if err == nil {
			t.Errorf("accepted unsafe SQL: %s", sql)
		}
	}

	_, err := Migration("INSERT INTO demo VALUES('COMMIT schema_migrations')")
	if err != nil {
		t.Fatalf("literal data was treated as SQL: %v", err)
	}
	_, err = Plan("SELECT 1;DELETE FROM demo")
	if err == nil {
		t.Fatal("multi-statement plan accepted")
	}
	_, err = Plan("ALTER TABLE demo ADD COLUMN name text")
	if err == nil {
		t.Fatal("unsupported DDL plan accepted")
	}
}

// Pastikan Plan menerima satu query DML/SELECT valid dan merapikan ';'.
func TestPlanAcceptsSingleQuery(t *testing.T) {
	validQueries := []string{
		"SELECT * FROM demo WHERE id=1;",
		"UPDATE demo SET name='a;b' WHERE id=1",
		"DELETE FROM demo",
		"INSERT INTO demo VALUES(1)",
		"WITH x AS (SELECT 1) SELECT * FROM x",
		"-- komentar\nSELECT 1",
	}
	for _, sql := range validQueries {
		statement, err := Plan(sql)
		if err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
		if statement.SQL == "" {
			t.Fatalf("%s: statement SQL is empty", sql)
		}
		if statement.SQL[len(statement.SQL)-1] == ';' {
			t.Fatalf("%s: trailing semicolon was not trimmed: %q", sql, statement.SQL)
		}
	}

	rejectedQueries := []string{
		"",
		"   ",
		"SELECT 1; SELECT 2",
		"DROP TABLE demo",
		"EXPLAIN ANALYZE DELETE FROM demo",
	}
	for _, sql := range rejectedQueries {
		_, err := Plan(sql)
		if err == nil {
			t.Errorf("accepted %q", sql)
		}
	}
}
