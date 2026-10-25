package api

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/auth"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/core"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/targetdb"
)

const rowsPerPage = 50

type databasesPage struct {
	base
	Targets []Target
}

type cell struct {
	Value string
	Null  bool
}

type explorerPage struct {
	base
	Target  Target
	Error   string
	Schemas []string
	Schema  string
	Tables  []targetdb.Table
	Table   *targetdb.Table
	View    string
	Columns []string
	Rows    [][]cell
	Offset  int
	HasMore bool
}

func (p explorerPage) PrevOffset() int { return max(p.Offset-rowsPerPage, 0) }
func (p explorerPage) NextOffset() int { return p.Offset + rowsPerPage }
func (p explorerPage) FirstRow() int   { return p.Offset + 1 }
func (p explorerPage) LastRow() int    { return p.Offset + len(p.Rows) }

func (a *API) databasesPage(w http.ResponseWriter, r *http.Request, session auth.AuthenticationResult) {
	targets, err := a.targets.Targets(r.Context(), session.User.ID)
	if err != nil {
		log.Printf("Gagal membaca daftar database: %v", err)
		a.showError(w, http.StatusInternalServerError, session, "Daftar database tidak dapat dimuat.")
		return
	}

	render(w, http.StatusOK, "databases", databasesPage{base: newBase("Database", session), Targets: targets})
}

func (a *API) pageTarget(w http.ResponseWriter, r *http.Request, session auth.AuthenticationResult, required core.Permission) (Target, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		a.showError(w, http.StatusNotFound, session, "Database tidak ditemukan.")
		return Target{}, false
	}
	target, err := a.targets.Target(r.Context(), session.User.ID, id)
	switch {
	case errors.Is(err, ErrTargetNotFound):
		a.showError(w, http.StatusNotFound, session, "Database tidak ditemukan.")
		return Target{}, false
	case err != nil:
		log.Printf("Gagal membaca database target: %v", err)
		a.showError(w, http.StatusInternalServerError, session, "Tidak dapat memeriksa akses.")
		return Target{}, false
	case !target.Permission.Can(required):
		a.showError(w, http.StatusForbidden, session, "Anda tidak memiliki akses untuk tindakan ini.")
		return Target{}, false
	}
	return target, true
}

func (a *API) explorerPage(w http.ResponseWriter, r *http.Request, session auth.AuthenticationResult) {
	target, ok := a.pageTarget(w, r, session, core.PermissionView)
	if !ok {
		return
	}

	page := explorerPage{base: newBase(target.Name, session), Target: target}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	conn, err := a.open(ctx, target)
	if err != nil {
		log.Printf("Database target tidak dapat diakses: %v", err)
		page.Error = "Database tidak dapat diakses. Periksa koneksi atau hubungi admin."
		render(w, http.StatusServiceUnavailable, "explorer", page)
		return
	}
	defer conn.Close()

	snapshot, err := conn.Snapshot(ctx)
	if err != nil {
		log.Printf("Gagal membaca struktur database: %v", err)
		page.Error = "Struktur database tidak dapat dibaca."
		render(w, http.StatusServiceUnavailable, "explorer", page)
		return
	}

	query := r.URL.Query()
	page.Schemas = snapshot.Schemas()
	page.Schema = query.Get("schema")

	if page.Schema == "" && len(page.Schemas) > 0 {
		page.Schema = page.Schemas[0]
	}
	if page.Schema != "" && !slices.Contains(page.Schemas, page.Schema) {
		page.Error = "Schema tidak ditemukan."
		render(w, http.StatusNotFound, "explorer", page)
		return
	}

	for _, table := range snapshot.Tables {
		if table.Schema == page.Schema {
			page.Tables = append(page.Tables, table)
		}
	}

	name := query.Get("table")
	if name == "" {
		render(w, http.StatusOK, "explorer", page)
		return
	}

	table, err := snapshot.Find(page.Schema, name)
	if err != nil {
		page.Error = "Tabel tidak ditemukan."
		render(w, http.StatusNotFound, "explorer", page)
		return
	}

	page.Table = &table
	page.View = query.Get("view")
	if page.View != "columns" && page.View != "constraints" && page.View != "indexes" {
		page.View = "data"
	}
	if page.View != "data" {
		render(w, http.StatusOK, "explorer", page)
		return
	}

	page.Offset, _ = strconv.Atoi(query.Get("offset"))
	rows, err := conn.Rows(ctx, page.Schema, name, rowsPerPage, page.Offset)
	if err != nil {
		page.Error = "Data tabel tidak dapat dibaca."
		render(w, http.StatusUnprocessableEntity, "explorer", page)
		return
	}

	page.Columns = rows.Columns
	page.HasMore = rows.HasMore
	for _, row := range rows.Rows {
		cells := make([]cell, len(row))
		for i, value := range row {
			if value == nil {
				cells[i] = cell{Null: true}
			} else {
				cells[i] = cell{Value: fmt.Sprint(value)}
			}
		}
		page.Rows = append(page.Rows, cells)
	}

	render(w, http.StatusOK, "explorer", page)
}
