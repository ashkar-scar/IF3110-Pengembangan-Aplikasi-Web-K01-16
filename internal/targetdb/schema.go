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

type Column struct {
	Name      string  `json:"name"`
	Type      string  `json:"type"`
	Nullable  bool    `json:"nullable"`
	Default   *string `json:"default"`
	Identity  string  `json:"identity,omitempty"`
	Generated string  `json:"generated,omitempty"`
}

type Constraint struct {
	Name       string   `json:"name"`
	Kind       string   `json:"kind"`
	Definition string   `json:"definition"`
	Columns    []string `json:"columns"`
}

type Index struct {
	Name       string   `json:"name"`
	Definition string   `json:"definition"`
	Columns    []string `json:"columns"`
	Unique     bool     `json:"unique"`
	Constraint bool     `json:"constraint"`
	Method     string   `json:"method"`
	Predicate  string   `json:"predicate,omitempty"`
}

type Table struct {
	Schema      string       `json:"schema"`
	Name        string       `json:"name"`
	Columns     []Column     `json:"columns"`
	Constraints []Constraint `json:"constraints"`
	Indexes     []Index      `json:"indexes"`
}

type Snapshot struct {
	Driver string  `json:"driver"`
	Tables []Table `json:"tables"`
}

func (s Snapshot) Fingerprint() string {
	data, _ := json.Marshal(s)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (s Snapshot) Find(schema, name string) (Table, error) {
	for _, table := range s.Tables {
		if table.Schema == schema && table.Name == name {
			return table, nil
		}
	}
	return Table{}, errors.New("table tidak ditemukan")
}

func (s Snapshot) Schemas() []string {
	seen := map[string]bool{}
	result := []string{}
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

func (c *Connection) Snapshot(ctx context.Context) (Snapshot, error) {
	return c.snapshot(ctx, c.DB)
}

func (c *Connection) snapshot(ctx context.Context, q rowsQueryer) (Snapshot, error) {
	s := Snapshot{Driver: c.Driver, Tables: []Table{}}
	rows, err := q.QueryContext(ctx, `SELECT n.nspname,t.relname FROM pg_class t JOIN pg_namespace n ON n.oid=t.relnamespace
WHERE t.relkind IN ('r','p') AND n.nspname NOT IN ('pg_catalog','information_schema') AND n.nspname NOT LIKE 'pg_toast%' AND n.nspname NOT LIKE 'pg_temp%' AND t.relname <> 'schema_migrations' ORDER BY n.nspname,t.relname`)
	if err != nil {
		return s, err
	}
	for rows.Next() {
		var t Table
		if err = rows.Scan(&t.Schema, &t.Name); err != nil {
			rows.Close()
			return s, err
		}
		s.Tables = append(s.Tables, t)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return s, err
	}
	for i := range s.Tables {
		if err = c.fillPostgres(ctx, q, &s.Tables[i]); err != nil {
			return s, err
		}
	}
	return s, nil
}

func (c *Connection) fillPostgres(ctx context.Context, q rowsQueryer, t *Table) error {
	t.Columns = []Column{}
	t.Constraints = []Constraint{}
	t.Indexes = []Index{}
	rows, err := q.QueryContext(ctx, `SELECT a.attname,format_type(a.atttypid,a.atttypmod),NOT a.attnotnull,pg_get_expr(d.adbin,d.adrelid),a.attidentity::text,a.attgenerated::text
FROM pg_attribute a JOIN pg_class r ON r.oid=a.attrelid JOIN pg_namespace n ON n.oid=r.relnamespace LEFT JOIN pg_attrdef d ON d.adrelid=r.oid AND d.adnum=a.attnum
WHERE n.nspname=$1 AND r.relname=$2 AND a.attnum>0 AND NOT a.attisdropped ORDER BY a.attnum`, t.Schema, t.Name)
	if err != nil {
		return err
	}
	for rows.Next() {
		var col Column
		if err = rows.Scan(&col.Name, &col.Type, &col.Nullable, &col.Default, &col.Identity, &col.Generated); err != nil {
			rows.Close()
			return err
		}
		t.Columns = append(t.Columns, col)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	rows, err = q.QueryContext(ctx, `SELECT c.conname,c.contype::text,pg_get_constraintdef(c.oid),COALESCE((SELECT json_agg(a.attname ORDER BY k.ordinality) FROM unnest(c.conkey) WITH ORDINALITY k(num,ordinality) JOIN pg_attribute a ON a.attrelid=c.conrelid AND a.attnum=k.num),'[]'::json)::text
FROM pg_constraint c JOIN pg_class r ON r.oid=c.conrelid JOIN pg_namespace n ON n.oid=r.relnamespace WHERE n.nspname=$1 AND r.relname=$2 ORDER BY c.conname`, t.Schema, t.Name)
	if err != nil {
		return err
	}
	for rows.Next() {
		var con Constraint
		var cols string
		if err = rows.Scan(&con.Name, &con.Kind, &con.Definition, &cols); err != nil {
			rows.Close()
			return err
		}
		if err = json.Unmarshal([]byte(cols), &con.Columns); err != nil {
			rows.Close()
			return err
		}
		t.Constraints = append(t.Constraints, con)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	rows, err = q.QueryContext(ctx, `SELECT x.relname,pg_get_indexdef(i.indexrelid),i.indisunique,EXISTS(SELECT FROM pg_constraint c WHERE c.conindid=i.indexrelid),am.amname,COALESCE(pg_get_expr(i.indpred,i.indrelid),''),
COALESCE((SELECT json_agg(pg_get_indexdef(i.indexrelid,k,true) ORDER BY k) FROM generate_series(1,i.indnkeyatts) k),'[]'::json)::text
FROM pg_index i JOIN pg_class r ON r.oid=i.indrelid JOIN pg_namespace n ON n.oid=r.relnamespace JOIN pg_class x ON x.oid=i.indexrelid JOIN pg_am am ON am.oid=x.relam WHERE n.nspname=$1 AND r.relname=$2 ORDER BY x.relname`, t.Schema, t.Name)
	if err != nil {
		return err
	}
	for rows.Next() {
		var index Index
		var cols string
		if err = rows.Scan(&index.Name, &index.Definition, &index.Unique, &index.Constraint, &index.Method, &index.Predicate, &cols); err != nil {
			rows.Close()
			return err
		}
		if err = json.Unmarshal([]byte(cols), &index.Columns); err != nil {
			rows.Close()
			return err
		}
		t.Indexes = append(t.Indexes, index)
	}
	err = rows.Err()
	rows.Close()
	return err
}
