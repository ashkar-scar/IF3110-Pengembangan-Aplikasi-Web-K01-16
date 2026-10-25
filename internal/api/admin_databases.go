package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/auth"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/core"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/store"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/targetdb"
)

// Menyediakan operasi registry database untuk endpoint admin
type AdminDatabaseSource interface {
	FindAll(context.Context) ([]core.DatabaseTarget, error)
	FindByID(context.Context, int64) (core.DatabaseTarget, error)
	Create(context.Context, core.DatabaseTarget) (core.DatabaseTarget, error)
	Update(context.Context, core.DatabaseTarget) (core.DatabaseTarget, error)
	Disable(context.Context, int64) error
}

// Menyediakan effective permission user untuk setiap database
type DatabasePermissionSource interface {
	EffectivePermission(context.Context, int64, int64) (core.Permission, error)
}

// Menghubungkan registry platform dengan database explorer
type registryTargets struct {
	registry    AdminDatabaseSource
	permissions DatabasePermissionSource
}

// Membuat sumber target dari registry dan repository permission
func NewRegistryTargets(registry AdminDatabaseSource, permissions DatabasePermissionSource) TargetSource {
	return registryTargets{registry: registry, permissions: permissions}
}

// Mengubah model registry menjadi target yang dapat dibuka explorer
func asTarget(record core.DatabaseTarget, permission core.Permission) Target {
	return Target{
		ID:         record.ID,
		Name:       record.Name,
		Driver:     record.Driver,
		Permission: permission,
		Config: targetdb.Config{
			Host:     record.Host,
			Port:     record.Port,
			Database: record.Database,
			Username: record.Username,
			SSLMode:  record.SSLMode,
		},
		Password: record.Password,
	}
}

// Mengambil target aktif yang dapat dilihat user
func (s registryTargets) Targets(ctx context.Context, userID int64) ([]Target, error) {
	records, err := s.registry.FindAll(ctx)
	if err != nil {
		return nil, err
	}
	
	targets := []Target{}
	for _, record := range records {
		if !record.Active {
			continue
		}

		permission, err := s.permissions.EffectivePermission(ctx, userID, record.ID)
		if err != nil {
			return nil, err
		}

		if permission.Can(core.PermissionView) {
			targets = append(targets, asTarget(record, permission))
		}
	}

	return targets, nil
}

// Mengambil satu target beserta permission user
func (s registryTargets) Target(ctx context.Context, userID, targetID int64) (Target, error) {
	record, err := s.registry.FindByID(ctx, targetID)
	if errors.Is(err, store.ErrNotFound) || (err == nil && !record.Active) {
		return Target{}, ErrTargetNotFound
	}

	if err != nil {
		return Target{}, err
	}

	permission, err := s.permissions.EffectivePermission(ctx, userID, targetID)

	if err != nil {
		return Target{}, err
	}

	return asTarget(record, permission), nil
}

// Memasang registry database pada API
func (a *API) UseDatabaseRegistry(source AdminDatabaseSource) {
	a.registry = source
}

// Mendaftarkan route pengelolaan database untuk admin
func (a *API) databaseRoutes(mux *http.ServeMux) {
	if a.registry == nil {
		return
	}
	mux.HandleFunc("GET /api/admin/databases", a.adminOnly(a.listDatabaseTargets))
	mux.HandleFunc("POST /api/admin/databases", a.adminOnly(a.createDatabaseTarget))
	mux.HandleFunc("PUT /api/admin/databases/{id}", a.adminOnly(a.updateDatabaseTarget))
	mux.HandleFunc("DELETE /api/admin/databases/{id}", a.adminOnly(a.disableDatabaseTarget))
}

// Menampung konfigurasi target dari request admin
type targetRequest struct {
	Name     string `json:"name"`
	Driver   string `json:"driver"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Database string `json:"database"`
	Username string `json:"username"`
	Password string `json:"password"`
	SSLMode  string `json:"sslmode"`
	Active   *bool  `json:"is_active"`
}

// Mengubah request menjadi model database target
func (request targetRequest) target(id int64) core.DatabaseTarget {
	active := true
	if request.Active != nil {
		active = *request.Active
	}

	driver := strings.TrimSpace(request.Driver)

	if driver == "" {
		driver = "postgres"
	}

	return core.DatabaseTarget{
		ID:       id,
		Name:     strings.TrimSpace(request.Name),
		Driver:   driver,
		Host:     strings.TrimSpace(request.Host),
		Port:     request.Port,
		Database: strings.TrimSpace(request.Database),
		Username: strings.TrimSpace(request.Username),
		Password: request.Password,
		SSLMode:  request.SSLMode,
		Active:   active,
	}
}

// Mengirim seluruh koneksi database kepada admin
func (a *API) listDatabaseTargets(w http.ResponseWriter, r *http.Request, _ auth.AuthenticationResult) {
	targets, err := a.registry.FindAll(r.Context())
	if err != nil {
		adminError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, targets)
}

// Menyimpan koneksi database baru
func (a *API) createDatabaseTarget(w http.ResponseWriter, r *http.Request, _ auth.AuthenticationResult) {
	a.saveDatabaseTarget(w, r, 0)
}

// Memperbarui koneksi database yang sudah terdaftar
func (a *API) updateDatabaseTarget(w http.ResponseWriter, r *http.Request, _ auth.AuthenticationResult) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}

	a.saveDatabaseTarget(w, r, id)
}

// Memvalidasi lalu menyimpan konfigurasi target
func (a *API) saveDatabaseTarget(w http.ResponseWriter, r *http.Request, id int64) {
	var request targetRequest
	if !readJSON(w, r, &request) {
		return
	}

	target := request.target(id)
	if err := store.ValidateTarget(target); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION", err.Error())
		return
	}

	var err error
	status := http.StatusOK

	if id == 0 {
		target, err = a.registry.Create(r.Context(), target)
		status = http.StatusCreated
	} else {
		target, err = a.registry.Update(r.Context(), target)
	}
	
	if err != nil {
		adminError(w, err)
		return
	}

	// SQLite langsung dibuka agar file dan koneksinya siap digunakan
	if target.Driver == "sqlite" && target.Active {
		conn, openErr := a.open(r.Context(), asTarget(target, core.PermissionNone))
		if openErr != nil {
			targetError(w, openErr)
			return
		}
		conn.Close()
	}

	writeJSON(w, status, target)
}

// Menonaktifkan target tanpa menghapus riwayatnya
func (a *API) disableDatabaseTarget(w http.ResponseWriter, r *http.Request, _ auth.AuthenticationResult) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}

	if err := a.registry.Disable(r.Context(), id); err != nil {
		adminError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
