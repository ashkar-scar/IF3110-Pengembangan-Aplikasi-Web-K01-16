package api

import (
	"context"
	"net/http"
	"time"

	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/auth"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/core"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/sqlguard"
)

func (a *API) planRoutes(mux *http.ServeMux) {
	if a.targets == nil {
		return
	}

	mux.HandleFunc("POST /api/databases/{id}/explain", a.signedIn(a.explain))
}

func (a *API) explain(w http.ResponseWriter, r *http.Request, session auth.AuthenticationResult) {
	if !validCSRF(w, r, session) {
		return
	}

	target, ok := a.allowedTarget(w, r, session.User, core.PermissionEdit)
	if !ok {
		return
	}

	var input struct {
		SQL string `json:"sql"`
	}
	if !readJSON(w, r, &input) {
		return
	}
	if _, err := sqlguard.Plan(input.SQL); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "UNSUPPORTED_PLAN", err.Error())
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

	plan, err := conn.Explain(ctx, input.SQL)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "PLAN_FAILED", "Query tidak dapat direncanakan; periksa SQL dan objek yang dirujuk")
		return
	}
	
	writeJSON(w, http.StatusOK, plan)
}
