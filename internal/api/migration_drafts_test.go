package api

import (
	"testing"

	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/core"
)

func TestValidateDraftSQL(t *testing.T) {
	validPG := draftRequest{
		SQLUp:         `CREATE TABLE "public"."notes" ("id" BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY, "body" TEXT NOT NULL);`,
		SQLDown:       `DROP TABLE IF EXISTS "public"."notes";`,
		ExecutionMode: core.ExecutionNormal,
	}
	if msg, ok := validateDraftSQL("postgres", validPG); !ok {
		t.Fatalf("valid postgres draft rejected: %s", msg)
	}

	badUpPG := validPG
	badUpPG.SQLUp = "asal asalan bukan sql"
	if _, ok := validateDraftSQL("postgres", badUpPG); ok {
		t.Fatal("invalid SQL up should be rejected for postgres")
	}

	badDownPG := validPG
	badDownPG.SQLDown = "rollback sembarang"
	if _, ok := validateDraftSQL("postgres", badDownPG); ok {
		t.Fatal("invalid SQL down should be rejected for postgres")
	}

	irreversiblePG := validPG
	irreversiblePG.SQLUp = `DROP TABLE IF EXISTS "public"."notes";`
	irreversiblePG.SQLDown = "tidak dipakai"
	irreversiblePG.IsIrreversible = true
	if msg, ok := validateDraftSQL("postgres", irreversiblePG); !ok {
		t.Fatalf("irreversible draft should skip SQLDown validation: %s", msg)
	}

	validSQLite := draftRequest{
		SQLUp:         `CREATE TABLE "notes" ("id" INTEGER PRIMARY KEY AUTOINCREMENT, "body" TEXT NOT NULL);`,
		SQLDown:       `DROP TABLE IF EXISTS "notes";`,
		ExecutionMode: core.ExecutionNormal,
	}
	if msg, ok := validateDraftSQL("sqlite", validSQLite); !ok {
		t.Fatalf("valid sqlite draft rejected: %s", msg)
	}

	badSQLite := validSQLite
	badSQLite.SQLDown = "teks acak"
	if _, ok := validateDraftSQL("sqlite", badSQLite); ok {
		t.Fatal("invalid SQL down should be rejected for sqlite")
	}

	badBatch := draftRequest{
		SQLUp:         `UPDATE "public"."orders" SET amount = 0`,
		SQLDown:       `SELECT 1`,
		ExecutionMode: core.ExecutionBatch,
		BatchColumn:   "id",
	}
	if _, ok := validateDraftSQL("postgres", badBatch); ok {
		t.Fatal("non-cursor batch SQL should be rejected")
	}
}
