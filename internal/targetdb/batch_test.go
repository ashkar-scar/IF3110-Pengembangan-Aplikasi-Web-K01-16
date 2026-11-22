package targetdb

import "testing"

func TestParseBatch(t *testing.T) {
	up := `UPDATE customers SET tier = 'GOLD' WHERE id IN (SELECT id FROM customers WHERE id > $1 ORDER BY id LIMIT $2)`
	down := `UPDATE customers SET tier = 'SILVER' WHERE id IN (SELECT id FROM customers WHERE id > $1 AND id <= $2 ORDER BY id LIMIT $3)`
	if _, err := ParseBatch(up, down, "id", false); err != nil {
		t.Fatal(err)
	}
	for name, query := range map[string]string{
		"cursor mutation": `UPDATE customers SET id = 1 WHERE id IN (SELECT id FROM customers WHERE id > $1 ORDER BY id LIMIT $2)`,
		"different table": `UPDATE customers SET tier = 'GOLD' WHERE id IN (SELECT id FROM other WHERE id > $1 ORDER BY id LIMIT $2)`,
		"side effect":     `UPDATE customers SET tier = evil() WHERE id IN (SELECT id FROM customers WHERE id > $1 ORDER BY id LIMIT $2)`,
		"unbounded":       `UPDATE customers SET tier = 'GOLD'`,
		"internal":        `UPDATE ruangdb_internal.jobs SET tier = 'GOLD' WHERE id IN (SELECT id FROM ruangdb_internal.jobs WHERE id > $1 ORDER BY id LIMIT $2)`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseBatch(query, down, "id", false); err == nil {
				t.Fatal("unsafe batch accepted")
			}
		})
	}
	if _, err := ParseBatch(up, up, "id", false); err == nil {
		t.Fatal("unbounded down accepted")
	}
}
