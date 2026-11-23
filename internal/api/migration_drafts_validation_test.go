package api

import (
	"testing"

	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/core"
)

// TestValidateDraftRequest menguji default draft valid dan seluruh aturan input draft tidak valid.
func TestValidateDraftRequest(t *testing.T) {
	// Pastikan draft normal yang lengkap diterima dan mendapat batch size default.
	valid := draftRequest{
		Description: "Tambah email", 
		SQLUp: "ALTER TABLE users ADD COLUMN email TEXT", 
		SQLDown: "ALTER TABLE users DROP COLUMN email", 
		ExecutionMode: core.ExecutionNormal,
	}
	
	if message, ok := validateDraftRequest(&valid); !ok {
		t.Fatalf("valid draft rejected: %s", message)
	}

	if valid.BatchSize != 1000 {
		t.Fatalf("default batch size = %d", valid.BatchSize)
	}

	// Susun kasus untuk SQL wajib, irreversible reason, kolom batch, dan batas batch size.
	tests := []struct {
		name  string
		input draftRequest
	}{
		{"missing sql up", draftRequest{Description: "Valid description", SQLDown: "SELECT 1"}},
		{"missing sql down", draftRequest{Description: "Valid description", SQLUp: "SELECT 1"}},
		{"irreversible without reason", draftRequest{Description: "Valid description", SQLUp: "DELETE FROM users", IsIrreversible: true}},
		{"batch without column", draftRequest{Description: "Valid description", SQLUp: "UPDATE users SET active=true", SQLDown: "UPDATE users SET active=false", ExecutionMode: core.ExecutionBatch}},
		{"batch too large", draftRequest{Description: "Valid description", SQLUp: "SELECT 1", SQLDown: "SELECT 1", BatchSize: 100001}},
	}

	// Jalankan setiap input invalid sebagai subtest agar kegagalan mudah dikenali.
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, ok := validateDraftRequest(&test.input); ok {
				t.Fatal("invalid draft accepted")
			}
		})
	}
}