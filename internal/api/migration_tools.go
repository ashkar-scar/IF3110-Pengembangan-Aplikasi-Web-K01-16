package api

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/auth"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/core"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/schemadiff"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/sqlguard"
)

// Mengatur request dari frontend
type diffRequest struct {
	SourceID         int64  `json:"source_id"`
	CurrentSchema    string `json:"current_schema"`
	DesiredSchema    string `json:"desired_schema"`
	AllowDestructive bool   `json:"allow_destructive"`
}

// Mengatur response dari frontend
type diffResponse struct {
	Changes     []schemadiff.Change `json:"changes"`
	SQL         string              `json:"sql"`
	Manual      bool                `json:"manual"`
	Fingerprint string              `json:"fingerprint"`
}

type explainRequest struct {
	SQL string `json:"sql"`
}

type explainResponse struct {
	Driver string     `json:"driver"`
	Nodes  []planNode `json:"nodes"`
	Raw    any        `json:"raw"`
}

// migrationToolRoutes mendaftarkan endpoint JSON schema diff dan execution plan.
func (a *API) migrationToolRoutes(mux *http.ServeMux) {
	if a.targets == nil {
		return
	}

	mux.HandleFunc("POST /api/databases/{id}/diff", a.RequireDatabasePermission(core.PermissionEdit, a.schemaDiffJSON))
	mux.HandleFunc("POST /api/databases/{id}/explain", a.RequireDatabasePermission(core.PermissionEdit, a.explainJSON))
}

// schemaDiffJSON membandingkan dua schema yang dapat diakses pengguna dan mengembalikan SQL perubahannya.
func (a *API) schemaDiffJSON(w http.ResponseWriter, r *http.Request, session auth.AuthenticationResult) {
	target, ok := a.allowedTarget(w, r, session.User, core.PermissionEdit)
	if !ok {
		return
	}

	var request diffRequest
	if !readJSON(w, r, &request) {
		return
	}

	request.CurrentSchema = strings.TrimSpace(request.CurrentSchema)
	request.DesiredSchema = strings.TrimSpace(request.DesiredSchema)
	if request.SourceID < 1 || request.CurrentSchema == "" || request.DesiredSchema == "" {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION", "source_id dan nama schema wajib diisi")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	source, err := a.targets.Target(ctx, session.User.ID, request.SourceID)
	if errors.Is(err, ErrTargetNotFound) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Database pembanding tidak ditemukan")
		return
	}

	if err != nil {
		log.Printf("Gagal membaca database pembanding: %v", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Database pembanding tidak dapat dibaca")
		return
	}

	if !source.Permission.Can(core.PermissionView) {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "Anda tidak memiliki akses ke database pembanding")
		return
	}

	if source.ID == target.ID && request.CurrentSchema == request.DesiredSchema {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION", "Pilih database atau schema pembanding yang berbeda")
		return
	}

	targetConnection, err := a.open(ctx, target)
	if err != nil {
		targetError(w, err)
		return
	}

	defer targetConnection.Close()
	sourceConnection, err := a.open(ctx, source)
	if err != nil {
		targetError(w, err)
		return
	}

	defer sourceConnection.Close()
	current, err := targetConnection.Snapshot(ctx)
	if err != nil {
		targetError(w, err)
		return
	}

	desired, err := sourceConnection.Snapshot(ctx)
	if err != nil {
		targetError(w, err)
		return
	}

	result, err := schemadiff.Compare(current, desired, request.CurrentSchema, request.DesiredSchema, request.AllowDestructive)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, diffResponse{
		Changes:     result.Changes,
		SQL:         result.SQL,
		Manual:      result.Manual,
		Fingerprint: current.Fingerprint(),
	})
}

// explainJSON memvalidasi SQL lalu mengembalikan execution plan tanpa menjalankan mutasinya.
func (a *API) explainJSON(w http.ResponseWriter, r *http.Request, session auth.AuthenticationResult) {
	target, ok := a.allowedTarget(w, r, session.User, core.PermissionEdit)
	if !ok {
		return
	}

	var request explainRequest
	if !readJSON(w, r, &request) {
		return
	}

	request.SQL = strings.TrimSpace(request.SQL)
	if request.SQL == "" || len(request.SQL) > 65536 {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION", "SQL wajib diisi dan maksimal 64 KiB")
		return
	}

	if _, err := sqlguard.Plan(request.SQL); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION", err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	connection, err := a.open(ctx, target)
	
	if err != nil {
		targetError(w, err)
		return
	}

	defer connection.Close()
	plan, err := connection.Explain(ctx, request.SQL)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION", "Query tidak dapat direncanakan")
		return
	}

	nodes := []planNode{}
	if rawItems, ok := plan.Raw.([]any); ok {
		for _, item := range rawItems {
			if root, ok := item.(map[string]any); ok {
				nodes = append(nodes, newPlanNode(root["Plan"]))
			}
		}
	}
	writeJSON(w, http.StatusOK, explainResponse{Driver: plan.Driver, Nodes: nodes, Raw: plan.Raw})
}