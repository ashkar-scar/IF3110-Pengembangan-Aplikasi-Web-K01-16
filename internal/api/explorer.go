package api

import (
	"context"
	"errors"
	"log"
	"net/http"
	"path"
	"strconv"
	"time"

	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/auth"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/core"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/targetdb"
)

var ErrTargetNotFound = errors.New("database tidak ditemukan")

type Target struct {
	ID         int64           `json:"id"`
	Name       string          `json:"name"`
	Driver     string          `json:"driver"`
	Permission core.Permission `json:"permission"`
	Config     targetdb.Config `json:"-"`
	Password   string          `json:"-"`
}

// TODO(faiz): implementasikan dari registry koneksi, grup, dan grant di database platform.
// Password sudah didekripsi, dan Permission adalah izin tertinggi user dari semua grupnya.
type TargetSource interface {
	Targets(ctx context.Context, userID int64) ([]Target, error)
	Target(ctx context.Context, userID, targetID int64) (Target, error)
}

// TODO(faiz): panggil dari main.go sebelum Router() setelah registry koneksi tersedia.
func (a *API) UseTargets(source TargetSource, dataDir string) {
	a.targets = source
	a.dataDir = dataDir
}

func (a *API) explorerRoutes(mux *http.ServeMux) {
	if a.targets == nil {
		return
	}
	mux.HandleFunc("GET /api/databases", a.signedIn(a.databases))
	mux.HandleFunc("POST /api/admin/databases/{id}/test", a.signedIn(a.testTarget))
	for _, view := range []string{"schema", "schemas", "tables", "table", "rows"} {
		mux.HandleFunc("GET /api/databases/{id}/"+view, a.RequireDatabasePermission(core.PermissionView, a.explore))
	}
}

// TODO(faiz): ganti dengan middleware autentikasi bersama jika sudah ada.
func (a *API) signedIn(next func(http.ResponseWriter, *http.Request, auth.AuthenticationResult)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(SESSION_COOKIE_NAME)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Session tidak ditemukan")
			return
		}
		result, err := a.authService.Authenticate(r.Context(), cookie.Value)
		if errors.Is(err, auth.ErrInvalidSession) {
			a.clearSessionCookie(w)
			writeError(w, http.StatusUnauthorized, "INVALID_SESSION", "Session tidak valid")
			return
		}
		if err != nil {
			log.Printf("Gagal memeriksa session: %v", err)
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Terjadi kesalahan pada server")
			return
		}
		if result.NewSessionToken != "" {
			a.setSessionCookie(w, result.NewSessionToken, result.Session.AbsoluteExpiresAt)
		}
		next(w, r, result)
	}
}

func (a *API) allowedTarget(w http.ResponseWriter, r *http.Request, user core.User, required core.Permission) (Target, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Database tidak ditemukan")
		return Target{}, false
	}
	target, err := a.targets.Target(r.Context(), user.ID, id)
	if errors.Is(err, ErrTargetNotFound) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Database tidak ditemukan")
		return Target{}, false
	}
	if err != nil {
		log.Printf("Gagal membaca database target: %v", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Terjadi kesalahan pada server")
		return Target{}, false
	}
	if !target.Permission.Can(required) {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "Anda tidak memiliki akses ke database ini")
		return Target{}, false
	}
	return target, true
}

func (a *API) open(ctx context.Context, target Target) (*targetdb.Connection, error) {
	switch target.Driver {
	case "postgres":
		return targetdb.OpenPostgres(ctx, target.Config, target.Password)
	case "sqlite":
		return targetdb.OpenSQLite(ctx, a.dataDir, target.Config.Database)
	}
	return nil, errors.New("driver tidak dikenal")
}

func targetError(w http.ResponseWriter, err error) {
	log.Printf("Database target tidak dapat diakses: %v", err)
	writeError(w, http.StatusServiceUnavailable, "TARGET_UNAVAILABLE", "Database target tidak dapat diakses; periksa koneksi dan konfigurasi")
}

func (a *API) databases(w http.ResponseWriter, r *http.Request, session auth.AuthenticationResult) {
	targets, err := a.targets.Targets(r.Context(), session.User.ID)
	if err != nil {
		log.Printf("Gagal membaca daftar database: %v", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Terjadi kesalahan pada server")
		return
	}
	if targets == nil {
		targets = []Target{}
	}
	writeJSON(w, http.StatusOK, targets)
}

func (a *API) testTarget(w http.ResponseWriter, r *http.Request, session auth.AuthenticationResult) {
	if !a.validOrigin(r) {
		writeError(w, http.StatusForbidden, "INVALID_CSRF", "Origin request tidak valid")
		return
	}
	if !session.User.CanManagePlatform() {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "Hanya admin platform yang dapat menguji koneksi")
		return
	}
	token := r.Header.Get("X-CSRF-Token")
	if token == "" || token != session.Session.CSRFToken {
		writeError(w, http.StatusForbidden, "INVALID_CSRF", "CSRF token tidak valid")
		return
	}
	target, ok := a.allowedTarget(w, r, session.User, core.PermissionNone)
	if !ok {
		return
	}
	conn, err := a.open(r.Context(), target)
	if err != nil {
		targetError(w, err)
		return
	}
	conn.Close()
	writeJSON(w, http.StatusOK, map[string]bool{"connected": true})
}

func (a *API) explore(w http.ResponseWriter, r *http.Request, session auth.AuthenticationResult) {
	target, ok := a.allowedTarget(w, r, session.User, core.PermissionView)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	conn, err := a.open(ctx, target)
	if err != nil {
		targetError(w, err)
		return
	}
	defer conn.Close()
	query := r.URL.Query()
	schema, name := query.Get("schema"), query.Get("table")
	if schema == "" {
		schema = "public"
		if target.Driver == "sqlite" {
			schema = "main"
		}
	}
	view := path.Base(r.URL.Path)
	if view == "rows" {
		limit, offset := 50, 0
		if value := query.Get("limit"); value != "" {
			if limit, err = strconv.Atoi(value); err != nil {
				writeError(w, http.StatusUnprocessableEntity, "VALIDATION", "Limit tidak valid")
				return
			}
		}
		if value := query.Get("offset"); value != "" {
			if offset, err = strconv.Atoi(value); err != nil {
				writeError(w, http.StatusUnprocessableEntity, "VALIDATION", "Offset tidak valid")
				return
			}
		}
		page, err := conn.Rows(ctx, schema, name, limit, offset)
		switch {
		case errors.Is(err, targetdb.ErrPagination):
			writeError(w, http.StatusUnprocessableEntity, "VALIDATION", "Limit harus 1-200 dan offset 0-1000000")
		case errors.Is(err, targetdb.ErrTableNotFound):
			writeError(w, http.StatusNotFound, "NOT_FOUND", "Tabel tidak ditemukan")
		case err != nil:
			targetError(w, err)
		default:
			writeJSON(w, http.StatusOK, page)
		}
		return
	}
	snapshot, err := conn.Snapshot(ctx)
	if err != nil {
		targetError(w, err)
		return
	}
	switch view {
	case "schemas":
		writeJSON(w, http.StatusOK, snapshot.Schemas())
	case "tables":
		tables := []targetdb.Table{}
		for _, table := range snapshot.Tables {
			if table.Schema == schema {
				tables = append(tables, table)
			}
		}
		writeJSON(w, http.StatusOK, tables)
	case "table":
		table, err := snapshot.Find(schema, name)
		if err != nil {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "Tabel tidak ditemukan")
			return
		}
		writeJSON(w, http.StatusOK, table)
	default:
		version, err := conn.Version(ctx)
		if err != nil {
			targetError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"snapshot": snapshot, "fingerprint": snapshot.Fingerprint(), "version": version})
	}
}
