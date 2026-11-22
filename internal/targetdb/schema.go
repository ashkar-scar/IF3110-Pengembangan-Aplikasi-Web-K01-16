package targetdb

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
)

var ErrTableNotFound = errors.New("table tidak ditemukan")

// Metadata kolom tabel PostgreSQL dan SQLite.
type Column struct {
	Name      string  `json:"name"`
	Type      string  `json:"type"`
	Nullable  bool    `json:"nullable"`
	Default   *string `json:"default"`
	Identity  string  `json:"identity,omitempty"`
	Generated string  `json:"generated,omitempty"`
}

// Metadata constraint tabel (PK, FK, UNIQUE, CHECK).
type Constraint struct {
	Name       string   `json:"name"`
	Kind       string   `json:"kind"`
	Definition string   `json:"definition"`
	Columns    []string `json:"columns"`
}

// Metadata index pada tabel.
type Index struct {
	Name       string   `json:"name"`
	Definition string   `json:"definition"`
	Columns    []string `json:"columns"`
	Unique     bool     `json:"unique"`
	Constraint bool     `json:"constraint"`
	Method     string   `json:"method"`
	Predicate  string   `json:"predicate,omitempty"`
}

// Detail kolom, constraint, dan index satu tabel.
type Table struct {
	Schema      string       `json:"schema"`
	Name        string       `json:"name"`
	Definition  string       `json:"definition,omitempty"`
	Columns     []Column     `json:"columns"`
	Constraints []Constraint `json:"constraints"`
	Indexes     []Index      `json:"indexes"`
}

// Hasil pembacaan skema database target.
type Snapshot struct {
	Driver      string   `json:"driver"`
	SchemaNames []string `json:"schemas"`
	Tables      []Table  `json:"tables"`
}

// Hash SHA-256 snapshot untuk mendeteksi schema drift.
func (s Snapshot) Fingerprint() string {
	data, _ := json.Marshal(s)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// Cari tabel berdasarkan nama schema dan tabel.
func (s Snapshot) Find(schema, name string) (Table, error) {
	for _, table := range s.Tables {
		if table.Schema == schema && table.Name == name {
			return table, nil
		}
	}
	return Table{}, ErrTableNotFound
}

// Daftar unik nama schema terurut, termasuk yang kosong.
func (s Snapshot) Schemas() []string {
	seen := map[string]bool{}
	result := []string{}

	for _, name := range s.SchemaNames {
		if !seen[name] {
			result = append(result, name)
			seen[name] = true
		}
	}
	for _, table := range s.Tables {
		if !seen[table.Schema] {
			result = append(result, table.Schema)
			seen[table.Schema] = true
		}
	}

	sort.Strings(result)
	return result
}

type rowsQueryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// Ambil snapshot schema dan tabel dari database target.
func (conn *Connection) Snapshot(ctx context.Context) (Snapshot, error) {
	return conn.snapshot(ctx, conn.DB)
}

// Baca snapshot memakai koneksi utama atau transaksi aktif.
func (conn *Connection) snapshot(ctx context.Context, queryer rowsQueryer) (Snapshot, error) {
	if conn.Driver == "sqlite" {
		return conn.snapshotSQLite(ctx, queryer)
	}

	snapshot := Snapshot{Driver: conn.Driver, Tables: []Table{}}

	// Abaikan schema bawaan sistem, toast, dan temporary.
	const schemasQuery = `
		SELECT nspname
		FROM pg_namespace
		WHERE nspname NOT IN ('pg_catalog', 'information_schema', 'ruangdb_internal')
		  AND nspname NOT LIKE 'pg_toast%'
		  AND nspname NOT LIKE 'pg_temp%'
		ORDER BY nspname
	`
	schemaRows, err := queryer.QueryContext(ctx, schemasQuery)
	if err != nil {
		return snapshot, err
	}
	for schemaRows.Next() {
		var name string
		err = schemaRows.Scan(&name)
		if err != nil {
			schemaRows.Close()
			return snapshot, err
		}
		snapshot.SchemaNames = append(snapshot.SchemaNames, name)
	}
	err = schemaRows.Err()
	schemaRows.Close()
	if err != nil {
		return snapshot, err
	}

	// Abaikan tabel ledger internal agar tidak ikut tampil di explorer maupun diff.
	const tablesQuery = `
		SELECT n.nspname, t.relname
		FROM pg_class AS t
		JOIN pg_namespace AS n
			ON n.oid = t.relnamespace
		WHERE t.relkind IN ('r', 'p')
		  AND n.nspname NOT IN ('pg_catalog', 'information_schema', 'ruangdb_internal')
		  AND n.nspname NOT LIKE 'pg_toast%'
		  AND n.nspname NOT LIKE 'pg_temp%'
		  AND t.relname <> 'schema_migrations'
		ORDER BY n.nspname, t.relname
	`
	tableRows, err := queryer.QueryContext(ctx, tablesQuery)
	if err != nil {
		return snapshot, err
	}
	for tableRows.Next() {
		var table Table
		err = tableRows.Scan(&table.Schema, &table.Name)
		if err != nil {
			tableRows.Close()
			return snapshot, err
		}
		snapshot.Tables = append(snapshot.Tables, table)
	}
	err = tableRows.Err()
	tableRows.Close()
	if err != nil {
		return snapshot, err
	}

	for i := range snapshot.Tables {
		err = conn.fillPostgres(ctx, queryer, &snapshot.Tables[i])
		if err != nil {
			return snapshot, err
		}
	}

	return snapshot, nil
}

// Isi kolom, constraint, dan index untuk satu tabel PostgreSQL.
func (conn *Connection) fillPostgres(ctx context.Context, queryer rowsQueryer, table *Table) error {
	table.Columns = []Column{}
	table.Constraints = []Constraint{}
	table.Indexes = []Index{}

	err := conn.loadPostgresColumns(ctx, queryer, table)
	if err != nil {
		return err
	}
	err = conn.loadPostgresConstraints(ctx, queryer, table)
	if err != nil {
		return err
	}
	return conn.loadPostgresIndexes(ctx, queryer, table)
}

// Muat daftar kolom tabel PostgreSQL.
func (conn *Connection) loadPostgresColumns(ctx context.Context, queryer rowsQueryer, table *Table) error {
	const query = `
		SELECT
			a.attname,
			format_type(a.atttypid, a.atttypmod),
			NOT a.attnotnull,
			pg_get_expr(d.adbin, d.adrelid),
			a.attidentity::text,
			a.attgenerated::text
		FROM pg_attribute AS a
		JOIN pg_class AS r
			ON r.oid = a.attrelid
		JOIN pg_namespace AS n
			ON n.oid = r.relnamespace
		LEFT JOIN pg_attrdef AS d
			ON d.adrelid = r.oid
			AND d.adnum = a.attnum
		WHERE n.nspname = $1
		  AND r.relname = $2
		  AND a.attnum > 0
		  AND NOT a.attisdropped
		ORDER BY a.attnum
	`

	rows, err := queryer.QueryContext(ctx, query, table.Schema, table.Name)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var col Column
		err = rows.Scan(&col.Name, &col.Type, &col.Nullable, &col.Default, &col.Identity, &col.Generated)
		if err != nil {
			return err
		}
		table.Columns = append(table.Columns, col)
	}

	return rows.Err()
}

// Muat constraint tabel PostgreSQL beserta urutan kolomnya.
func (conn *Connection) loadPostgresConstraints(ctx context.Context, queryer rowsQueryer, table *Table) error {
	const query = `
		SELECT
			c.conname,
			c.contype::text,
			pg_get_constraintdef(c.oid),
			COALESCE((
				SELECT json_agg(a.attname ORDER BY k.ordinality)
				FROM unnest(c.conkey) WITH ORDINALITY AS k(num, ordinality)
				JOIN pg_attribute AS a
					ON a.attrelid = c.conrelid
					AND a.attnum = k.num
			), '[]'::json)::text
		FROM pg_constraint AS c
		JOIN pg_class AS r
			ON r.oid = c.conrelid
		JOIN pg_namespace AS n
			ON n.oid = r.relnamespace
		WHERE n.nspname = $1
		  AND r.relname = $2
		ORDER BY c.conname
	`

	rows, err := queryer.QueryContext(ctx, query, table.Schema, table.Name)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var constraint Constraint
		var colsJSON string
		err = rows.Scan(&constraint.Name, &constraint.Kind, &constraint.Definition, &colsJSON)
		if err != nil {
			return err
		}
		err = json.Unmarshal([]byte(colsJSON), &constraint.Columns)
		if err != nil {
			return err
		}
		table.Constraints = append(table.Constraints, constraint)
	}

	return rows.Err()
}

// Muat daftar index PostgreSQL beserta predikat dan kolomnya.
func (conn *Connection) loadPostgresIndexes(ctx context.Context, queryer rowsQueryer, table *Table) error {
	const query = `
		SELECT
			x.relname,
			pg_get_indexdef(i.indexrelid),
			i.indisunique,
			EXISTS (
				SELECT FROM pg_constraint AS c
				WHERE c.conindid = i.indexrelid
			),
			am.amname,
			COALESCE(pg_get_expr(i.indpred, i.indrelid), ''),
			COALESCE((
				SELECT json_agg(pg_get_indexdef(i.indexrelid, k, true) ORDER BY k)
				FROM generate_series(1, i.indnkeyatts) AS k
			), '[]'::json)::text
		FROM pg_index AS i
		JOIN pg_class AS r
			ON r.oid = i.indrelid
		JOIN pg_namespace AS n
			ON n.oid = r.relnamespace
		JOIN pg_class AS x
			ON x.oid = i.indexrelid
		JOIN pg_am AS am
			ON am.oid = x.relam
		WHERE n.nspname = $1
		  AND r.relname = $2
		ORDER BY x.relname
	`

	rows, err := queryer.QueryContext(ctx, query, table.Schema, table.Name)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var index Index
		var colsJSON string
		err = rows.Scan(
			&index.Name,
			&index.Definition,
			&index.Unique,
			&index.Constraint,
			&index.Method,
			&index.Predicate,
			&colsJSON,
		)
		if err != nil {
			return err
		}
		err = json.Unmarshal([]byte(colsJSON), &index.Columns)
		if err != nil {
			return err
		}
		table.Indexes = append(table.Indexes, index)
	}

	return rows.Err()
}
