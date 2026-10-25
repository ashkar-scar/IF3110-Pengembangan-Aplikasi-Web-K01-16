package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/auth"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/core"
)

type adminGroup struct {
	core.Group
	Members []core.User
}

type adminPageData struct {
	base
	Ready   bool
	Users   []core.User
	Groups  []adminGroup
	Grants  []core.DatabaseGrant
	Targets []core.DatabaseTarget
	Editing *core.DatabaseTarget
	Notice  string
	Error   string
}

func (a *API) adminPage(w http.ResponseWriter, r *http.Request, session auth.AuthenticationResult) {
	a.renderAdmin(w, r, session, http.StatusOK, "")
}

func (a *API) renderAdmin(w http.ResponseWriter, r *http.Request, session auth.AuthenticationResult, status int, message string) {
	if !session.User.CanManagePlatform() {
		a.showError(w, http.StatusForbidden, session, "Halaman ini hanya untuk admin platform.")
		return
	}

	page := adminPageData{base: newBase("Administrasi", session), Error: message, Ready: a.users != nil && a.groups != nil && a.grants != nil && a.registry != nil}
	if !page.Ready {
		render(w, status, "admin", page)
		return
	}

	var err error
	if page.Users, err = a.users.FindAll(r.Context()); err != nil {
		a.showError(w, 500, session, "Daftar pengguna tidak dapat dimuat.")
		return
	}

	groups, err := a.groups.FindAll(r.Context())
	if err != nil {
		a.showError(w, 500, session, "Daftar group tidak dapat dimuat.")
		return
	}

	for _, group := range groups {
		members, err := a.groups.FindMembers(r.Context(), group.ID)
		if err != nil {
			a.showError(w, 500, session, "Anggota group tidak dapat dimuat.")
			return
		}
		page.Groups = append(page.Groups, adminGroup{Group: group, Members: members})
	}
	if page.Grants, err = a.grants.FindAll(r.Context()); err != nil {
		a.showError(w, 500, session, "Daftar grant tidak dapat dimuat.")
		return
	}
	if page.Targets, err = a.registry.FindAll(r.Context()); err != nil {
		a.showError(w, 500, session, "Daftar koneksi tidak dapat dimuat.")
		return
	}
	if id, err := strconv.ParseInt(r.URL.Query().Get("target"), 10, 64); err == nil && id > 0 {
		target, err := a.registry.FindByID(r.Context(), id)
		if err != nil {
			a.showError(w, 404, session, "Koneksi tidak ditemukan.")
			return
		}
		target.Password = ""
		page.Editing = &target
	}
	if r.URL.Query().Get("saved") == "1" {
		page.Notice = "Perubahan disimpan."
	}
	if r.URL.Query().Get("tested") == "1" {
		page.Notice = "Koneksi berhasil."
	}

	render(w, status, "admin", page)
}

type adminFormResponse struct {
	header http.Header
	status int
	bytes.Buffer
}

func (w *adminFormResponse) Header() http.Header    { return w.header }
func (w *adminFormResponse) WriteHeader(status int) { w.status = status }
func (w *adminFormResponse) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = 200
	}
	return w.Buffer.Write(p)
}

// Form memakai handler dan validasi yang sama dengan JSON API.
func (a *API) submitAdmin(w http.ResponseWriter, r *http.Request, session auth.AuthenticationResult) {
	if !session.User.CanManagePlatform() {
		a.showError(w, 403, session, "Halaman ini hanya untuk admin platform.")
		return
	}

	if a.users == nil || a.groups == nil || a.grants == nil || a.registry == nil {
		a.showError(w, 503, session, "Pengelolaan platform belum tersedia.")
		return
	}
	action := r.FormValue("action")
	var handler authenticatedHandler
	var payload any = struct{}{}
	r.SetPathValue("id", r.FormValue("group_id"))
	r.SetPathValue("userID", r.FormValue("user_id"))
	r.SetPathValue("targetID", r.FormValue("target_id"))

	switch action {
	case "update-user":
		handler = a.updateUser
		r.SetPathValue("id", r.FormValue("user_id"))
		active := r.FormValue("is_active") == "yes"
		payload = createUserRequest{Username: r.FormValue("username"), Password: r.FormValue("password"), PlatformAdmin: r.FormValue("is_platform_admin") == "yes", Active: &active}
	case "rename-group":
		handler = a.renameGroup
		payload = map[string]string{"name": r.FormValue("name")}
	case "delete-group":
		handler = a.deleteGroup
	case "create-user":
		handler = a.createUser
		active := true
		payload = createUserRequest{Username: r.FormValue("username"), Password: r.FormValue("password"), PlatformAdmin: r.FormValue("is_platform_admin") == "yes", Active: &active}
	case "create-group":
		handler = a.createGroup
		payload = map[string]string{"name": r.FormValue("name")}
	case "add-member":
		handler = a.addMember
		userID, _ := strconv.ParseInt(r.FormValue("user_id"), 10, 64)
		payload = map[string]int64{"user_id": userID}
	case "remove-member":
		handler = a.removeMember
	case "set-grant":
		handler = a.setGrant
		payload = map[string]string{"permission": r.FormValue("permission")}
	case "remove-grant":
		handler = a.removeGrant
	case "create-target", "update-target":
		handler = a.createDatabaseTarget
		if action == "update-target" {
			handler = a.updateDatabaseTarget
		}
		r.SetPathValue("id", r.FormValue("target_id"))
		port, _ := strconv.Atoi(r.FormValue("port"))
		active := r.FormValue("is_active") == "yes"
		payload = targetRequest{Name: r.FormValue("name"), Driver: r.FormValue("driver"), Host: r.FormValue("host"), Port: port, Database: r.FormValue("database"), Username: r.FormValue("username"), Password: r.FormValue("password"), SSLMode: r.FormValue("sslmode"), Active: &active}
	case "disable-target":
		handler = a.disableDatabaseTarget
		r.SetPathValue("id", r.FormValue("target_id"))
	case "test-target":
		handler = a.testTarget
		r.SetPathValue("id", r.FormValue("target_id"))
	default:
		a.renderAdmin(w, r, session, 422, "Aksi tidak dikenal.")
		return
	}

	data, err := json.Marshal(payload)
	if err != nil {
		a.showError(w, 500, session, "Form tidak dapat diproses.")
		return
	}

	r.Body = io.NopCloser(bytes.NewReader(data))
	r.Header.Set("X-CSRF-Token", session.Session.CSRFToken)
	result := &adminFormResponse{header: make(http.Header)}
	handler(result, r, session)
	if result.status >= 200 && result.status < 300 {
		location := "/admin?saved=1"
		if action == "test-target" {
			location = "/admin?tested=1"
		}
		http.Redirect(w, r, location, http.StatusSeeOther)
		return
	}

	var response errorResponse
	if err := json.Unmarshal(result.Bytes(), &response); err != nil {
		a.showError(w, 500, session, "Form tidak dapat diproses.")
		return
	}
	
	a.renderAdmin(w, r, session, result.status, response.Error.Message)
}
