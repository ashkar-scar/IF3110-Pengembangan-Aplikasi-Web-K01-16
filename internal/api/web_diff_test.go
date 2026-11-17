package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/core"
)

// Pastikan explorer membedakan schema kosong dari schema tidak ada.
func TestExplorerDistinguishesEmptyAndMissingSchema(t *testing.T) {
	a := testAPI(t)
	conn, err := a.open(context.Background(), a.targets.(fakeTargets).targets[1])
	if err != nil {
		t.Fatal(err)
	}
	_, err = conn.DB.Exec("DROP TABLE orders; DROP TABLE customers")
	if err != nil {
		conn.Close()
		t.Fatal(err)
	}
	conn.Close()

	emptyResp := call(a.explorerPage, session(10, false), "GET", "/databases/1?schema=main", "1")
	if emptyResp.Code != http.StatusOK {
		t.Fatalf("empty schema status %d, want 200", emptyResp.Code)
	}
	if !strings.Contains(emptyResp.Body.String(), "Belum ada tabel") || !strings.Contains(emptyResp.Body.String(), ">main</a>") {
		t.Fatalf("unexpected empty schema body: %s", emptyResp.Body.String())
	}

	missingResp := call(a.explorerPage, session(10, false), "GET", "/databases/1?schema=missing", "1")
	if missingResp.Code != http.StatusNotFound {
		t.Fatalf("missing schema status %d, want 404", missingResp.Code)
	}
	if !strings.Contains(missingResp.Body.String(), "Schema tidak ditemukan") {
		t.Fatalf("unexpected missing schema body: %s", missingResp.Body.String())
	}
}

// Pastikan schema diff menolak schema salah sebelum membentuk draft SQL.
func TestDiffRejectsMissingSchemaBeforeGeneratingDraft(t *testing.T) {
	a := testAPI(t)
	a.targets.(fakeTargets).grants[10][1] = core.PermissionEdit

	for _, destructive := range []string{"", "yes"} {
		for _, action := range []string{"compare", "draft"} {
			form := url.Values{
				"source_id":      {"1"},
				"current_schema": {"main"},
				"desired_schema": {"typo"},
				"destructive":    {destructive},
				"action":         {action},
			}
			r := httptest.NewRequest("POST", "/databases/1/diff", strings.NewReader(form.Encode()))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			r.SetPathValue("id", "1")
			w := httptest.NewRecorder()
			a.schemaDiffPage(w, r, session(10, false))

			if w.Code != http.StatusUnprocessableEntity {
				t.Fatalf("destructive=%q action=%q status=%d, want 422", destructive, action, w.Code)
			}
			if !strings.Contains(w.Body.String(), "tidak ditemukan") {
				t.Fatalf("expected missing schema message, got: %s", w.Body.String())
			}
			if strings.Contains(w.Body.String(), "DROP TABLE") {
				t.Fatalf("missing schema generated DROP TABLE SQL: %s", w.Body.String())
			}
		}
	}
}
