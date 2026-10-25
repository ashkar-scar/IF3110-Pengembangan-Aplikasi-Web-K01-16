package api

import (
	"context"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/core"
)

func TestExplorerDistinguishesEmptyAndMissingSchema(t *testing.T) {
	a := testAPI(t)
	conn, err := a.open(context.Background(), a.targets.(fakeTargets).targets[1])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.DB.Exec("DROP TABLE orders; DROP TABLE customers"); err != nil {
		conn.Close()
		t.Fatal(err)
	}
	conn.Close()
	w := call(a.explorerPage, session(10, false), "GET", "/databases/1?schema=main", "1")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Belum ada tabel") || !strings.Contains(w.Body.String(), ">main</a>") {
		t.Fatalf("empty schema: %d %s", w.Code, w.Body.String())
	}
	w = call(a.explorerPage, session(10, false), "GET", "/databases/1?schema=missing", "1")
	if w.Code != 404 || !strings.Contains(w.Body.String(), "Schema tidak ditemukan") {
		t.Fatalf("missing schema: %d %s", w.Code, w.Body.String())
	}
}

func TestDiffRejectsMissingSchemaBeforeGeneratingDraft(t *testing.T) {
	a := testAPI(t)
	a.targets.(fakeTargets).grants[10][1] = core.PermissionEdit
	for _, destructive := range []string{"", "yes"} {
		for _, action := range []string{"compare", "draft"} {
			form := url.Values{"source_id": {"1"}, "current_schema": {"main"}, "desired_schema": {"typo"}, "destructive": {destructive}, "action": {action}}
			r := httptest.NewRequest("POST", "/databases/1/diff", strings.NewReader(form.Encode()))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			r.SetPathValue("id", "1")
			w := httptest.NewRecorder()
			a.schemaDiffPage(w, r, session(10, false))
			if w.Code != 422 || !strings.Contains(w.Body.String(), "tidak ditemukan") || strings.Contains(w.Body.String(), "DROP TABLE") {
				t.Fatalf("%d: %s", w.Code, w.Body.String())
			}
		}
	}
}
