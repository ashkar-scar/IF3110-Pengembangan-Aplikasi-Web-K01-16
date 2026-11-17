package targetdb

import (
	"context"
	"strings"
	"testing"
)

// Pastikan EXPLAIN QUERY PLAN SQLite membaca rencana tanpa mengubah data.
func TestSQLiteExplainDoesNotExecute(t *testing.T) {
	conn := openTestSQLite(t, t.TempDir())
	_, err := execute(conn, "seed", shopSchema, 0)
	if err != nil {
		t.Fatal(err)
	}

	plan, err := conn.Explain(context.Background(), "DELETE FROM orders WHERE customer_id = 1")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Driver != "sqlite" {
		t.Fatalf("got driver %q, want sqlite", plan.Driver)
	}

	steps := plan.Raw.([]PlanStep)
	if len(steps) == 0 || !strings.Contains(steps[0].Detail, "orders") {
		t.Fatalf("unexpected SQLite plan steps: %+v", steps)
	}

	page, err := conn.Rows(context.Background(), "main", "orders", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Rows) != 3 {
		t.Fatalf("EXPLAIN deleted rows: got %d, want 3", len(page.Rows))
	}
}

// Pastikan EXPLAIN PostgreSQL mengembalikan pohon JSON dan menolak DDL.
func TestPostgresExplainDoesNotExecute(t *testing.T) {
	conn, schema := fixture(t)

	plan, err := conn.Explain(context.Background(), "DELETE FROM "+schema+".customers WHERE id > 10")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Driver != "postgres" {
		t.Fatalf("got driver %q, want postgres", plan.Driver)
	}

	roots, ok := plan.Raw.([]any)
	if !ok || len(roots) != 1 {
		t.Fatalf("unexpected PostgreSQL plan roots: %+v", plan.Raw)
	}
	node := roots[0].(map[string]any)["Plan"].(map[string]any)
	if node["Node Type"] != "ModifyTable" {
		t.Fatalf("unexpected root node type %v, want ModifyTable", node["Node Type"])
	}

	var count int
	err = conn.DB.QueryRow("SELECT count(*) FROM " + schema + ".customers").Scan(&count)
	if err != nil {
		t.Fatal(err)
	}
	if count != 120 {
		t.Fatalf("EXPLAIN deleted rows: got %d, want 120", count)
	}

	rejected := []string{
		"SELECT 1; SELECT 2",
		"CREATE TABLE x (id int)",
		"EXPLAIN ANALYZE DELETE FROM " + schema + ".customers",
	}
	for _, query := range rejected {
		_, err = conn.Explain(context.Background(), query)
		if err == nil {
			t.Errorf("accepted %q", query)
		}
	}
}
