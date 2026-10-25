package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/core"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/targetdb"
)

func explainRequest(a *API, user int64, sql string) *httptest.ResponseRecorder {
	body, _ := json.Marshal(map[string]string{"sql": sql})
	r := httptest.NewRequest("POST", "/api/databases/1/explain", bytes.NewReader(body))
	r.SetPathValue("id", "1")
	r.Header.Set("X-CSRF-Token", "csrf-token")
	w := httptest.NewRecorder()
	a.explain(w, r, session(user, false))
	return w
}

func TestExplainShowsPlanWithoutExecuting(t *testing.T) {
	a := testAPI(t)
	a.targets.(fakeTargets).grants[40] = map[int64]core.Permission{1: core.PermissionEdit}
	var plan struct {
		Driver string              `json:"driver"`
		Raw    []targetdb.PlanStep `json:"raw"`
	}
	decode(t, explainRequest(a, 40, "DELETE FROM orders WHERE customer_id = 1"), http.StatusOK, &plan)
	if plan.Driver != "sqlite" || len(plan.Raw) == 0 {
		t.Fatalf("unexpected plan: %+v", plan)
	}
	var page targetdb.RowPage
	decode(t, call(a.explore, session(10, false), "GET", "/api/databases/1/rows?table=orders", "1"), http.StatusOK, &page)
	if len(page.Rows) != 3 {
		t.Fatal("EXPLAIN must not delete rows")
	}
	decode(t, explainRequest(a, 40, "DROP TABLE orders"), http.StatusUnprocessableEntity, nil)
	decode(t, explainRequest(a, 40, "SELECT 1; SELECT 2"), http.StatusUnprocessableEntity, nil)
	decode(t, explainRequest(a, 40, "SELECT * FROM missing"), http.StatusUnprocessableEntity, nil)
	decode(t, explainRequest(a, 10, "SELECT * FROM orders"), http.StatusForbidden, nil)
}
