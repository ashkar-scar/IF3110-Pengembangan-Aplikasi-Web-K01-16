package api

import (
	"net/http"

	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/auth"
)

// TODO(faiz): isi halaman ini dengan form pengguna, group, grant, dan koneksi database
// setelah penyimpanannya di database platform tersedia.
func (a *API) adminPage(w http.ResponseWriter, r *http.Request, session auth.AuthenticationResult) {
	if !session.User.CanManagePlatform() {
		a.showError(w, http.StatusForbidden, session, "Halaman ini hanya untuk admin platform.")
		return
	}
	
	render(w, http.StatusOK, "admin", newBase("Administrasi", session))
}
