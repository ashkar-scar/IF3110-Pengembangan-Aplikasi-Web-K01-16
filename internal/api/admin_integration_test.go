package api

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/auth"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/core"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/store"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/testutil"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Menyimpan dependency dan data yang dipakai setiap integration test admin
type adminFixture struct {
	api    *API
	pool   *pgxpool.Pool
	router http.Handler
	users  map[string]core.User
	target core.DatabaseTarget
}

// Menyimpan cookie session dan token CSRF hasil login
type credentials struct {
	cookie *http.Cookie
	csrf   string
}

// Menyiapkan platform database, user, registry, dan router untuk test
func backendA(t *testing.T) adminFixture {
	t.Helper()
	pool := testutil.Platform(t)
	users := store.NewUserRepository(pool)
	hash, err := auth.CreatePasswordHash("testing-password")
	if err != nil {
		t.Fatal(err)
	}
	f := adminFixture{
		pool:  pool,
		users: map[string]core.User{},
	}
	for _, name := range []string{"admin", "editor", "viewer"} {
		user := core.User{
			Username:      name,
			PasswordHash:  hash,
			Active:        true,
			PlatformAdmin: name == "admin",
		}
		u, err := users.Create(context.Background(), user)
		if err != nil {
			t.Fatal(err)
		}
		f.users[name] = u
	}
	registry, err := store.NewTargetRepository(pool, bytes.Repeat([]byte{4}, 32))
	if err != nil {
		t.Fatal(err)
	}

	record := core.DatabaseTarget{
		Name:     "sales",
		Driver:   "postgres",
		Host:     "127.0.0.1",
		Port:     1,
		Database: "sales",
		Username: "target_user",
		SSLMode:  "disable",
		Password: "target-secret",
		Active:   true,
	}
	// Gunakan target PostgreSQL nyata ketika DSN test tersedia
	if dsn := os.Getenv("TEST_TARGET_DSN"); dsn != "" {
		u, err := url.Parse(dsn)
		if err != nil {
			t.Fatal(err)
		}
		record.Host = u.Hostname()
		record.Port, _ = strconv.Atoi(u.Port())
		record.Database = strings.TrimPrefix(u.Path, "/")
		record.Username = u.User.Username()
		record.Password, _ = u.User.Password()
	}
	f.target, err = registry.Create(context.Background(), record)
	if err != nil {
		t.Fatal(err)
	}

	// Pasang seluruh dependency Backend A pada API test
	f.api = Create(auth.NewService(users, store.NewSessionRepository(pool)), false)
	f.api.UseUsers(users)
	f.api.UseGroups(store.NewGroupRepository(pool))
	permissions := store.NewPermissionRepository(pool)
	f.api.UseGrants(permissions)
	f.api.UseDatabaseRegistry(registry)
	f.api.UseTargets(NewRegistryTargets(registry, permissions), t.TempDir())
	f.api.UseMigrations(store.NewMigrationRepository(pool))
	mux := f.api.Router().(*http.ServeMux)
	mux.HandleFunc("POST /permission-test/{id}", f.api.RequireDatabasePermission(core.PermissionEdit, func(w http.ResponseWriter, r *http.Request, s auth.AuthenticationResult) {
		w.WriteHeader(http.StatusNoContent)
	}))
	f.router = mux
	return f
}

// Mengirim request melalui router dengan cookie dan CSRF opsional
func (f adminFixture) request(method, path, body string, session credentials) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if session.cookie != nil {
		r.AddCookie(session.cookie)
	}
	if session.csrf != "" {
		r.Header.Set("X-CSRF-Token", session.csrf)
	}
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, r)
	return w
}

// Login sebagai fixture user dan memeriksa keamanan cookie
func (f adminFixture) login(t *testing.T, name string) credentials {
	t.Helper()
	w := f.request("POST", "/api/auth/login", fmt.Sprintf(`{"username":%q,"password":"testing-password"}`, name), credentials{})
	var response loginResponse
	decode(t, w, http.StatusOK, &response)
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteLaxMode || cookies[0].Secure {
		t.Fatal("invalid development session cookie", cookies)
	}
	if strings.Contains(w.Body.String(), cookies[0].Value) || strings.Contains(w.Body.String(), "testing-password") {
		t.Fatal("login leaked secrets")
	}
	return credentials{
		cookie: cookies[0],
		csrf:   response.CSRFToken,
	}
}

// Menguji alur group, membership, grant, registry, dan perubahan session
func TestAdminGroupGrantAndRegistryFlow(t *testing.T) {
	f := backendA(t)
	admin := f.login(t, "admin")
	var group core.Group
	decode(t, f.request("POST", "/api/admin/groups", `{"name":"developers"}`, admin), 201, &group)
	membersPath := fmt.Sprintf("/api/admin/groups/%d/members", group.ID)
	decode(t, f.request("GET", membersPath, "", admin), 200, nil)
	decode(t, f.request("POST", membersPath, fmt.Sprintf(`{"user_id":%d}`, f.users["editor"].ID), admin), 204, nil)
	decode(t, f.request("POST", membersPath, fmt.Sprintf(`{"user_id":%d}`, f.users["editor"].ID), admin), 409, nil)
	var members []core.User
	w := f.request("GET", membersPath, "", admin)
	decode(t, w, 200, &members)
	if len(members) != 1 || strings.Contains(w.Body.String(), "password") {
		t.Fatal(w.Body.String())
	}
	grantPath := fmt.Sprintf("/api/admin/groups/%d/grants/%d", group.ID, f.target.ID)
	decode(t, f.request("PUT", grantPath, `{"permission":"VIEW"}`, admin), 204, nil)
	editor := f.login(t, "editor")
	decode(t, f.request("POST", fmt.Sprintf("/permission-test/%d", f.target.ID), "", editor), 403, nil)
	var targets []Target
	decode(t, f.request("GET", "/api/databases", "", editor), 200, &targets)
	if len(targets) != 1 || targets[0].ID != f.target.ID || targets[0].Permission != core.PermissionView {
		t.Fatal(targets)
	}
	decode(t, f.request("GET", "/api/databases", "", admin), 200, &targets)
	if len(targets) != 0 {
		t.Fatal("platform admin must not bypass group grants")
	}
	decode(t, f.request("GET", fmt.Sprintf("/api/databases/%d/schema", f.target.ID), "", admin), 403, nil)
	if os.Getenv("TEST_TARGET_DSN") != "" {
		decode(t, f.request("GET", fmt.Sprintf("/api/databases/%d/schema", f.target.ID), "", editor), 200, nil)
	}
	decode(t, f.request("PUT", grantPath, `{"permission":"EDIT"}`, admin), 204, nil)
	w = f.request("GET", "/api/auth/current-user", "", editor)
	decode(t, w, 200, nil)
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Value == editor.cookie.Value {
		t.Fatal("privilege increase must rotate token")
	}
	old := editor
	editor.cookie = cookies[0]
	decode(t, f.request("POST", fmt.Sprintf("/permission-test/%d", f.target.ID), "", editor), 204, nil)
	decode(t, f.request("GET", "/api/auth/current-user", "", old), 200, nil)
	decode(t, f.request("GET", "/api/admin/grants", "", admin), 200, nil)
	decode(t, f.request("DELETE", grantPath, "", admin), 204, nil)
	decode(t, f.request("GET", "/api/auth/current-user", "", editor), 401, nil)
	decode(t, f.request("GET", "/api/auth/current-user", "", old), 401, nil)
	decode(t, f.request("DELETE", membersPath+"/"+fmt.Sprint(f.users["editor"].ID), "", admin), 204, nil)
	decode(t, f.request("DELETE", membersPath+"/"+fmt.Sprint(f.users["editor"].ID), "", admin), 404, nil)
	w = f.request("GET", "/api/admin/databases", "", admin)
	decode(t, w, 200, nil)
	if strings.Contains(w.Body.String(), "password") || strings.Contains(w.Body.String(), "ciphertext") {
		t.Fatal("registry leaked credentials")
	}
}

// Memastikan seluruh route admin menolak session dan CSRF yang tidak valid
func TestAdminRoutesDenyAnonymousNonAdminAndInvalidCSRF(t *testing.T) {
	f := backendA(t)
	admin := f.login(t, "admin")
	viewer := f.login(t, "viewer")
	routes := [][2]string{
		{"GET", "/api/admin/users"},
		{"POST", "/api/admin/users"},
		{"GET", "/api/admin/groups"},
		{"POST", "/api/admin/groups"},
		{"GET", "/api/admin/groups/1/members"},
		{"POST", "/api/admin/groups/1/members"},
		{"DELETE", "/api/admin/groups/1/members/1"},
		{"GET", "/api/admin/grants"},
		{"PUT", "/api/admin/groups/1/grants/1"},
		{"DELETE", "/api/admin/groups/1/grants/1"},
		{"GET", "/api/admin/databases"},
		{"POST", "/api/admin/databases"},
		{"PUT", "/api/admin/databases/1"},
		{"DELETE", "/api/admin/databases/1"},
		{"POST", "/api/admin/databases/1/test"},
	}
	for _, route := range routes {
		t.Run(route[0]+route[1], func(t *testing.T) {
			decode(t, f.request(route[0], route[1], `{}`, credentials{}), 401, nil)
			decode(t, f.request(route[0], route[1], `{}`, viewer), 403, nil)
			if requiresCSRF(route[0]) {
				for _, token := range []string{"", "wrong"} {
					decode(t, f.request(route[0], route[1], `{}`, credentials{admin.cookie, token}), 403, nil)
				}
			}
		})
	}
	decode(t, f.request("GET", "/api/databases/99999/schema", "", viewer), 404, nil)
	for _, test := range []struct {
		method, path, body string
		status             int
	}{
		{"POST", "/api/admin/groups", `{"name":" "}`, 422},
		{"POST", "/api/admin/groups", `{"name":"x","extra":1}`, 400},
		{"POST", "/api/admin/groups", `{"name":"x"} {}`, 400},
		{"POST", "/api/admin/groups", `invalid`, 400},
		{"GET", "/api/admin/groups/no/members", ``, 404},
		{"GET", "/api/admin/groups/99999/members", ``, 404},
		{"POST", "/api/admin/groups/1/members", `{"user_id":0}`, 422},
		{"PUT", "/api/admin/groups/1/grants/1", `{"permission":"OWNER"}`, 422},
		{"POST", "/api/admin/databases", `{}`, 422},
	} {
		decode(t, f.request(test.method, test.path, test.body, admin), test.status, nil)
	}
	const countGroupsQuery = `
		SELECT count(*)
		FROM groups
	`

	var count int
	if err := f.pool.QueryRow(context.Background(), countGroupsQuery).Scan(&count); err != nil || count != 0 {
		t.Fatal("denied requests changed state", count, err)
	}
}

// Menguji pembuatan user serta siklus hidup koneksi database
func TestAdminUsersAndTargetLifecycle(t *testing.T) {
	f := backendA(t)
	admin := f.login(t, "admin")
	var created core.User
	w := f.request("POST", "/api/admin/users", `{"username":"new-user","password":"new-user-password"}`, admin)
	decode(t, w, 201, &created)
	if !created.Active || created.PlatformAdmin || strings.Contains(w.Body.String(), "password") {
		t.Fatal(w.Body.String())
	}
	decode(t, f.request("POST", "/api/admin/users", `{"username":"new-user","password":"new-user-password"}`, admin), 409, nil)
	decode(t, f.request("POST", "/api/admin/users", `{"username":"bad","password":"short"}`, admin), 422, nil)
	decode(t, f.request("GET", "/api/admin/users", "", admin), 200, nil)
	var record core.DatabaseTarget
	body := `{"name":"another","driver":"postgres","host":"127.0.0.1","port":1,"database":"hr","username":"target","password":"stored-secret","sslmode":"disable"}`
	w = f.request("POST", "/api/admin/databases", body, admin)
	decode(t, w, 201, &record)
	if strings.Contains(w.Body.String(), "stored-secret") {
		t.Fatal("password leaked")
	}
	body = strings.ReplaceAll(body, `"name":"another"`, `"name":"updated"`)
	body = strings.ReplaceAll(body, `"password":"stored-secret"`, `"password":""`)
	path := fmt.Sprintf("/api/admin/databases/%d", record.ID)
	decode(t, f.request("PUT", path, body, admin), 200, nil)
	read, err := f.api.registry.FindByID(context.Background(), record.ID)
	if err != nil || read.Password != "stored-secret" {
		t.Fatal("credential not preserved", err)
	}
	decode(t, f.request("POST", path+"/test", `{}`, admin), 503, nil)
	decode(t, f.request("DELETE", path, "", admin), 204, nil)
	decode(t, f.request("POST", path+"/test", `{}`, admin), 404, nil)
	if _, err := f.api.targets.Target(context.Background(), f.users["admin"].ID, record.ID); err != ErrTargetNotFound {
		t.Fatal(err)
	}
}

// Memastikan origin asing ditolak dan cookie production memakai Secure
func TestOriginAndProductionCookies(t *testing.T) {
	f := backendA(t)
	for _, path := range []string{"/login", "/api/auth/login"} {
		r := httptest.NewRequest("POST", path, strings.NewReader(`{}`))
		r.Header.Set("Origin", "https://other.example")
		w := httptest.NewRecorder()
		f.router.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatal(path, w.Code)
		}
	}
	f.api.secureCookies = true
	w := f.request("POST", "/api/auth/login", `{"username":"admin","password":"testing-password"}`, credentials{})
	decode(t, w, 200, nil)
	if !w.Result().Cookies()[0].Secure {
		t.Fatal("production cookie must be secure")
	}
}

// Memastikan form admin memakai permission dan validasi yang sama dengan API
func TestAdminFormsUseTheSamePermissionsAndValidation(t *testing.T) {
	f := backendA(t)
	admin := f.login(t, "admin")
	viewer := f.login(t, "viewer")
	submit := func(session credentials, values url.Values) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/admin", strings.NewReader(values.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.AddCookie(session.cookie)
		w := httptest.NewRecorder()
		f.router.ServeHTTP(w, r)
		return w
	}

	values := url.Values{
		"action":     {"create-group"},
		"name":       {"web-group"},
		"csrf_token": {admin.csrf},
	}
	decode(t, submit(admin, values), 303, nil)
	decode(t, submit(admin, values), 409, nil)
	values.Set("name", "")
	w := submit(admin, values)
	expectPage(t, w, 422, "Nama group wajib diisi")
	values.Set("csrf_token", viewer.csrf)
	decode(t, submit(viewer, values), 403, nil)
	values.Set("csrf_token", "invalid")
	decode(t, submit(admin, values), 403, nil)
	w = f.request("GET", "/admin", "", admin)
	expectPage(t, w, 200, "web-group", "Tambah pengguna", "Tambah anggota", "Simpan grant", "Simpan koneksi")
	if strings.Contains(w.Body.String(), "testing-password") || strings.Contains(w.Body.String(), "target-secret") {
		t.Fatal("admin page leaked secrets")
	}
	w = f.request("GET", fmt.Sprintf("/admin?target=%d", f.target.ID), "", admin)
	expectPage(t, w, 200, "Ubah koneksi", "mempertahankan password lama")
}
