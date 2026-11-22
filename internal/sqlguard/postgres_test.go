package sqlguard

import (
	"strings"
	"testing"
)

func TestPostgresMigrationRejectsEscapedInternalIdentifiers(t *testing.T) {
	for name, query := range map[string]string{
		"ledger":                   `DELETE FROM public.U&"schema\005fmigrations"`,
		"worker schema":            `DELETE FROM U&"ruangdb\005finternal".jobs`,
		"unicode alternate escape": `DELETE FROM public.U&"schema!005fmigrations" UESCAPE '!'`,
		"rename ledger":            `ALTER TABLE customers RENAME TO U&"schema\005fmigrations"`,
		"schema move":              `ALTER TABLE customers SET SCHEMA U&"ruangdb\005finternal"`,
		"function":                 `SELECT U&"set\005fconfig"('search_path','public',false)`,
		"dynamic query":            `SELECT query_to_xml('DELETE FROM public.schema_migrations RETURNING *',false,false,'')`,
		"escaped dynamic query":    `SELECT U&"query\005fto_xml"('SELECT 1',false,false,'')`,
		"parameters":               `UPDATE customers SET tier=$1`,
		"broken syntax":            `UPDATE customers SET`,
		"transaction":              `SELECT 1; COMMIT`,
		"copy program":             `COPY customers TO PROGRAM 'whoami'`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := PostgresMigration(query); err == nil {
				t.Fatal("unsafe PostgreSQL migration accepted")
			}
		})
	}
}

func TestPostgresMigrationPreservesLiteralDataAndDDL(t *testing.T) {
	statements, err := PostgresMigration(`CREATE TABLE demo(id bigint PRIMARY KEY,note text); INSERT INTO demo VALUES(1,'schema_migrations; DROP TABLE demo; --'); UPDATE demo SET note=$tag$ruangdb_internal; COMMIT$tag$ WHERE id=1`)
	if err != nil {
		t.Fatal(err)
	}
	if len(statements) != 3 {
		t.Fatalf("statements=%d", len(statements))
	}
	if !strings.Contains(statements[1].SQL(), "schema_migrations; DROP TABLE demo; --") {
		t.Fatal("literal data was changed")
	}
	for _, statement := range statements {
		parsed, err := Split(statement.SQL())
		if err != nil || len(parsed) != 1 {
			t.Fatalf("validated SQL is not a single statement: %v", err)
		}
	}
	if _, err := PostgresMigration(`WITH x AS (SELECT 1 AS id) SELECT * FROM x`); err != nil {
		t.Fatal(err)
	}
}
