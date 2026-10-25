package api

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/store"
	"github.com/jackc/pgx/v5/pgconn"
)

// Membaca satu objek JSON dengan batas ukuran satu megabyte
func readJSON(w http.ResponseWriter, r *http.Request, value any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(value); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "Format request tidak valid")
		return false
	}

	// Tolak data tambahan setelah objek JSON pertama
	if err := decoder.Decode(new(any)); err != io.EOF {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "Request harus berisi satu objek JSON")
		return false
	}

	return true
}

// Membaca ID positif dari parameter path
func pathID(w http.ResponseWriter, r *http.Request, name string) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)

	if err != nil || id < 1 {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Resource tidak ditemukan")
		return 0, false
	}

	return id, true
}

// Mengubah error repository menjadi response API yang aman
func adminError(w http.ResponseWriter, err error) {
	var databaseError *pgconn.PgError

	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Resource tidak ditemukan")

	case errors.As(err, &databaseError) && databaseError.Code == "23505":
		writeError(w, http.StatusConflict, "CONFLICT", "Nama atau keanggotaan sudah digunakan")
	
	case errors.As(err, &databaseError) && databaseError.Code == "23503":
		writeError(w, http.StatusNotFound, "NOT_FOUND", "User, group, atau database tidak ditemukan")
	
	default:
		log.Printf("Operasi admin gagal: %v", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Operasi admin gagal")
	}
}
