package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/auth"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/core"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/store"
)

func page(handler func(http.ResponseWriter, *http.Request, auth.AuthenticationResult), user int64, target string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", target, nil)
	r.SetPathValue("id", strings.Split(strings.TrimPrefix(target, "/databases/"), "?")[0])
	w := httptest.NewRecorder()
	handler(w, r, session(user, false))
	return w
}

func expectPage(t *testing.T, w *httptest.ResponseRecorder, status int, texts ...string) {
	t.Helper()
	if w.Code != status {
		t.Fatalf("status %d, want %d", w.Code, status)
	}
	for _, text := range texts {
		if !strings.Contains(w.Body.String(), text) {
			t.Errorf("page does not contain %q", text)
		}
	}
}

func TestPagesRedirectAnonymousVisitors(t *testing.T) {
	a := testAPI(t)
	a.migrations = &store.MigrationRepository{}
	router := a.Router()
	for _, path := range []string{"/", "/databases", "/databases/1", "/databases/1/migrations", "/admin"} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != http.StatusSeeOther {
			t.Errorf("%s returned %d, want redirect", path, w.Code)
		}
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("POST", "/logout", nil))
	if w.Header().Get("Location") != "/login" {
		t.Errorf("logout without a session must go to /login, got %q", w.Header().Get("Location"))
	}

	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", "/login", nil))
	expectPage(t, w, http.StatusOK, `<form method="post" action="/login">`, `name="password"`)
	if strings.Contains(w.Body.String(), "<script") {
		t.Error("pages must not depend on JavaScript")
	}

	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", "/static/app.css", nil))
	expectPage(t, w, http.StatusOK, ".topbar")
}

func TestFormTokenMustMatchSession(t *testing.T) {
	s := session(1, false)
	for token, want := range map[string]bool{"csrf-token": true, "": false, "wrong": false} {
		r := httptest.NewRequest("POST", "/logout", strings.NewReader(url.Values{"csrf_token": {token}}.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if got := validFormToken(r, s); got != want {
			t.Errorf("token %q: got %v, want %v", token, got, want)
		}
	}
}

func TestDatabaseListAndLayout(t *testing.T) {
	a := testAPI(t)
	w := page(a.databasesPage, 10, "/databases")
	expectPage(t, w, http.StatusOK, `href="/databases/1"`, "shop", "broken", `name="csrf_token" value="csrf-token"`)
	if strings.Contains(w.Body.String(), "super-secret") || strings.Contains(w.Body.String(), `href="/admin"`) {
		t.Fatal("list leaked a password or showed the admin link to a regular user")
	}
	expectPage(t, page(a.databasesPage, 99, "/databases"), http.StatusOK, "Belum ada database")

	r := httptest.NewRequest("GET", "/admin", nil)
	w = httptest.NewRecorder()
	a.adminPage(w, r, session(10, false))
	expectPage(t, w, http.StatusForbidden, "hanya untuk admin")
	w = httptest.NewRecorder()
	a.adminPage(w, r, session(30, true))
	expectPage(t, w, http.StatusOK, "Administrasi", `href="/admin"`)
}

func TestExplorerPage(t *testing.T) {
	a := testAPI(t)
	expectPage(t, page(a.explorerPage, 10, "/databases/1"), http.StatusOK,
		"View Only", `href="/databases/1?schema=main&table=orders"`, "Pilih tabel")

	w := page(a.explorerPage, 10, "/databases/1?schema=main&table=orders")
	expectPage(t, w, http.StatusOK, "<th>customer_id</th>", "<td>a</td>", "Baris 1 sampai 3")
	if strings.Contains(w.Body.String(), "Selanjutnya") {
		t.Error("three rows must fit on one page")
	}

	expectPage(t, page(a.explorerPage, 10, "/databases/1?schema=main&table=orders&view=columns"), http.StatusOK, "<td>customer_id</td>", "<td>INTEGER</td>")
	expectPage(t, page(a.explorerPage, 10, "/databases/1?schema=main&table=orders&view=constraints"), http.StatusOK, "FOREIGN KEY", "PRIMARY KEY")
	expectPage(t, page(a.explorerPage, 10, "/databases/1?schema=main&table=customers&view=indexes"), http.StatusOK, "Tidak ada index")

	expectPage(t, page(a.explorerPage, 10, "/databases/1?table=missing"), http.StatusNotFound, "Tabel tidak ditemukan")
	expectPage(t, page(a.explorerPage, 10, "/databases/1?table=schema_migrations"), http.StatusNotFound)
	expectPage(t, page(a.explorerPage, 20, "/databases/1"), http.StatusForbidden, "tidak memiliki akses")
	expectPage(t, page(a.explorerPage, 10, "/databases/9"), http.StatusNotFound)

	w = page(a.explorerPage, 10, "/databases/2")
	expectPage(t, w, http.StatusServiceUnavailable, "tidak dapat diakses")
	if strings.Contains(w.Body.String(), "super-secret") || strings.Contains(w.Body.String(), "127.0.0.1") {
		t.Fatal("error page leaked connection details")
	}
}

func TestExplorerHeaderShowsPermission(t *testing.T) {
	a := testAPI(t)
	a.targets.(fakeTargets).grants[40] = map[int64]core.Permission{1: core.PermissionEdit}
	expectPage(t, page(a.explorerPage, 40, "/databases/1"), http.StatusOK, "View + Edit")
}
