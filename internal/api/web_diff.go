package api

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/auth"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/core"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/schemadiff"
)

type diffPage struct {
	base
	Target                       Target
	Targets                      []Target
	SourceID                     int64
	CurrentSchema, DesiredSchema string
	Result                       *schemadiff.Result
	Fingerprint, Error           string
	AllowDestructive             bool
}

func (a *API) schemaDiffPage(w http.ResponseWriter, r *http.Request, session auth.AuthenticationResult) {
	target, ok := a.pageTarget(w, r, session, core.PermissionEdit)
	if !ok {
		return
	} 

	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	page := diffPage{base: newBase("Bandingkan schema", session), Target: target, CurrentSchema: "public", DesiredSchema: "public"}
	if target.Driver == "sqlite" {
		page.CurrentSchema = "main"
		page.DesiredSchema = "main"
	}

	var err error
	page.Targets, err = a.targets.Targets(ctx, session.User.ID)
	if err != nil {
		a.showError(w, 500, session, "Daftar target tidak dapat dimuat.")
		return
	}
	if r.Method == "GET" {
		render(w, 200, "diff", page)
		return
	}

	page.SourceID, _ = strconv.ParseInt(r.FormValue("source_id"), 10, 64)
	page.CurrentSchema = r.FormValue("current_schema")
	page.DesiredSchema = r.FormValue("desired_schema")
	page.AllowDestructive = r.FormValue("destructive") == "yes"
	source, err := a.targets.Target(ctx, session.User.ID, page.SourceID)
	if err != nil || !source.Permission.Can(core.PermissionView) {
		a.showError(w, 403, session, "Tidak memiliki akses ke database pembanding.")
		return
	}
	if page.SourceID == target.ID && page.CurrentSchema == page.DesiredSchema {
		page.Error = "Pilih database atau schema pembanding yang berbeda."
		render(w, 422, "diff", page)
		return
	}

	conn, err := a.open(ctx, target)
	if err != nil {
		a.showError(w, 503, session, "Database target tidak dapat diakses.")
		return
	}
	defer conn.Close()

	other, err := a.open(ctx, source)
	if err != nil {
		a.showError(w, 503, session, "Database pembanding tidak dapat diakses.")
		return
	}
	defer other.Close()

	current, err := conn.Snapshot(ctx)
	if err != nil {
		a.showError(w, 503, session, "Schema target tidak dapat dibaca.")
		return
	}

	desired, err := other.Snapshot(ctx)
	if err != nil {
		a.showError(w, 503, session, "Schema pembanding tidak dapat dibaca.")
		return
	}

	result, err := schemadiff.Compare(current, desired, page.CurrentSchema, page.DesiredSchema, page.AllowDestructive)
	if err != nil {
		page.Error = err.Error()
		render(w, 422, "diff", page)
		return
	}

	page.Result = &result
	page.Fingerprint = current.Fingerprint()
	if r.FormValue("action") == "draft" {
		if result.SQL == "" {
			page.Error = "Tidak ada SQL yang dapat dibuat. Periksa pilihan operasi destruktif dan perubahan manual."
		} else {
			draft, err := a.saveDraft(ctx, target, core.Draft{TargetID: target.ID, Description: fmt.Sprintf("Schema diff dari %s", source.Name), SQL: result.SQL, Fingerprint: page.Fingerprint, CreatedBy: session.User.ID})
			if err == nil {
				redirectToMigrations(w, r, target, fmt.Sprintf("draft=%d&saved=1", draft.ID))
				return
			}
			log.Printf("Draft schema diff gagal: %v", err)
			page.Error = "Draft tidak dapat disimpan."
		}
	}
	
	render(w, 200, "diff", page)
}
