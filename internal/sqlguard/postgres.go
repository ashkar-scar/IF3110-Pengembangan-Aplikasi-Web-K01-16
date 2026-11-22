package sqlguard

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/wasilibs/go-pgquery/parser"
)

type PostgresStatement struct{ query string }

func (s PostgresStatement) SQL() string { return s.query }

func PostgresMigration(query string) ([]PostgresStatement, error) {
	statements, err := Migration(query)
	if err != nil {
		return nil, err
	}
	result := make([]PostgresStatement, 0, len(statements))
	for i, statement := range statements {
		compiled, err := validatePostgresStatement(statement.SQL)
		if err != nil {
			return nil, fmt.Errorf("statement %d: %w", i+1, err)
		}
		result = append(result, compiled)
	}
	return result, nil
}

func validatePostgresStatement(query string) (PostgresStatement, error) {
	treeJSON, err := parser.ParseToJSON(query)
	if err != nil {
		return PostgresStatement{}, errors.New("sintaks PostgreSQL migration tidak valid")
	}
	var tree struct {
		Statements []struct {
			Statement map[string]any `json:"stmt"`
		} `json:"stmts"`
	}
	if err = json.Unmarshal([]byte(treeJSON), &tree); err != nil {
		return PostgresStatement{}, errors.New("AST PostgreSQL tidak dapat dibaca")
	}
	if len(tree.Statements) != 1 || len(tree.Statements[0].Statement) != 1 {
		return PostgresStatement{}, errors.New("eksekusi membutuhkan tepat satu statement PostgreSQL")
	}
	for kind := range tree.Statements[0].Statement {
		switch kind {
		case "SelectStmt", "InsertStmt", "UpdateStmt", "DeleteStmt", "CreateStmt", "CreateTableAsStmt", "IndexStmt", "AlterTableStmt", "DropStmt", "RenameStmt", "AlterObjectSchemaStmt":
		default:
			return PostgresStatement{}, errors.New("jenis statement PostgreSQL tidak diizinkan untuk migration")
		}
	}
	if err = validatePostgresAST(tree.Statements[0].Statement); err != nil {
		return PostgresStatement{}, err
	}
	return PostgresStatement{query: query}, nil
}

func validatePostgresAST(value any) error {
	switch node := value.(type) {
	case map[string]any:
		for kind, child := range node {
			if kind == "A_Const" {
				continue
			}
			if kind == "ParamRef" {
				return errors.New("parameter SQL hanya tersedia dalam mode batch")
			}
			if kind == "FuncCall" {
				call, ok := child.(map[string]any)
				if !ok {
					return errors.New("AST pemanggilan fungsi tidak valid")
				}
				if err := validatePostgresFunction(call["funcname"]); err != nil {
					return err
				}
			}
			if err := validatePostgresAST(child); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range node {
			if err := validatePostgresAST(child); err != nil {
				return err
			}
		}
	case string:
		if strings.EqualFold(node, "schema_migrations") || strings.EqualFold(node, "ruangdb_internal") {
			return errors.New("identifier internal platform tidak diperbolehkan dalam migration")
		}
	}
	return nil
}

func validatePostgresFunction(value any) error {
	switch node := value.(type) {
	case map[string]any:
		for _, child := range node {
			if err := validatePostgresFunction(child); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range node {
			if err := validatePostgresFunction(child); err != nil {
				return err
			}
		}
	case string:
		name := strings.ToUpper(node)
		if err := checkTokens([]string{name}); err != nil {
			return err
		}
		for _, prefix := range []string{"QUERY_TO_XML", "TABLE_TO_XML", "SCHEMA_TO_XML", "DATABASE_TO_XML"} {
			if strings.HasPrefix(name, prefix) {
				return errors.New("fungsi yang mengeksekusi SQL dinamis tidak diperbolehkan")
			}
		}
	}
	return nil
}
