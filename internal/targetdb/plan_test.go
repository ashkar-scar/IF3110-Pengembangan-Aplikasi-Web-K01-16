package targetdb

import (
	"context"
	"strings"
	"testing"
)

func TestSQLiteExplainDoesNotExecute(t *testing.T) {
	conn := openTestSQLite(t, t.TempDir())
	if _, err := execute(conn, "seed", shopSchema, 0); err != nil {
		t.Fatal(err)
	}
	plan, err := conn.Explain(context.Background(), "DELETE FROM orders WHERE customer_id = 1")
	if err != nil {
		t.Fatal(err)
	}
	steps := plan.Raw.([]PlanStep)
	if plan.Driver != "sqlite" || len(steps) == 0 || !strings.Contains(steps[0].Detail, "orders") {
		t.Fatalf("unexpected plan: %+v", plan)
	}
	page, _ := conn.Rows(context.Background(), "main", "orders", 10, 0)
	if len(page.Rows) != 3 {
		t.Fatal("EXPLAIN deleted rows")
	}
}

func TestPostgresExplainDoesNotExecute(t *testing.T) {
	conn, schema := fixture(t)
	plan, err := conn.Explain(context.Background(), "DELETE FROM "+schema+".customers WHERE id > 10")
	if err != nil {
		t.Fatal(err)
	}
	roots, ok := plan.Raw.([]any)
	if plan.Driver != "postgres" || !ok || len(roots) != 1 {
		t.Fatalf("unexpected plan: %+v", plan)
	}
	node := roots[0].(map[string]any)["Plan"].(map[string]any)
	if node["Node Type"] != "ModifyTable" {
		t.Fatalf("unexpected root node: %v", node["Node Type"])
	}
	var count int
	if err = conn.DB.QueryRow("SELECT count(*) FROM " + schema + ".customers").Scan(&count); err != nil || count != 120 {
		t.Fatalf("EXPLAIN deleted rows: %d %v", count, err)
	}
	for _, query := range []string{"SELECT 1; SELECT 2", "CREATE TABLE x (id int)", "EXPLAIN ANALYZE DELETE FROM " + schema + ".customers"} {
		if _, err = conn.Explain(context.Background(), query); err == nil {
			t.Errorf("accepted %q", query)
		}
	}
}
