package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/store"
	"github.com/jackc/pgx/v5/pgconn"
)

// Memastikan request JSON dan error database menghasilkan status yang tepat
func TestAdminInputAndErrorResponses(t *testing.T) {
	invalidBodies := []string{
		`{`,
		`{"extra":1}`,
		`{} {}`,
		strings.Repeat("x", (1<<20)+1),
	}
	for _, body := range invalidBodies {
		r := httptest.NewRequest("POST", "/", strings.NewReader(body))
		w := httptest.NewRecorder()
		var request struct {
			Name string `json:"name"`
		}
		if readJSON(w, r, &request) || w.Code != 400 {
			t.Fatal("invalid request accepted", w.Code)
		}
	}

	errorCases := []struct {
		err    error
		status int
	}{
		{store.ErrNotFound, http.StatusNotFound},
		{&pgconn.PgError{Code: "23505"}, http.StatusConflict},
		{&pgconn.PgError{Code: "23503"}, http.StatusNotFound},
		{errors.New("private-connection-error"), http.StatusInternalServerError},
	}
	for _, test := range errorCases {
		w := httptest.NewRecorder()
		adminError(w, test.err)
		if w.Code != test.status || strings.Contains(w.Body.String(), "private-connection-error") {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}

// Memastikan mutasi hanya menerima origin browser yang sesuai
func TestMutationOriginChecks(t *testing.T) {
	a := Create(nil, false)
	testCases := []struct {
		origin, site string
		valid        bool
	}{
		{"", "", true},
		{"http://example.com", "same-origin", true},
		{"https://example.com", "same-origin", true},
		{"http://other.example", "", false},
		{"null", "", false},
		{"http://example.com/path", "", false},
		{"http://example.com", "cross-site", false},
	}
	for _, test := range testCases {
		r := httptest.NewRequest("POST", "http://example.com/login", nil)
		r.Header.Set("Origin", test.origin)
		r.Header.Set("Sec-Fetch-Site", test.site)
		if a.validOrigin(r) != test.valid {
			t.Fatal(test)
		}
	}

	// Production menolak origin HTTP untuk request HTTPS
	a.secureCookies = true
	r := httptest.NewRequest("POST", "https://example.com/login", nil)
	r.Header.Set("Origin", "http://example.com")
	if a.validOrigin(r) {
		t.Fatal("production accepted insecure browser origin")
	}
}
