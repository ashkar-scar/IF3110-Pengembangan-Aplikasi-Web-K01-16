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
