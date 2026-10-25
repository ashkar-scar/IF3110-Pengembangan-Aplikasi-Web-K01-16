package targetdb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	_ "modernc.org/sqlite"
)

var sqliteFile = regexp.MustCompile(`^[a-f0-9]{32}\.db$`)

func OpenSQLite(ctx context.Context, dataDir, file string) (*Connection, error) {
	if !sqliteFile.MatchString(file) {
		return nil, errors.New("nama file SQLite tidak valid")
	}
	dir, err := filepath.Abs(dataDir)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, file)
	query := url.Values{}
	query.Add("_pragma", "foreign_keys(1)")
	query.Add("_pragma", "busy_timeout(5000)")
	query.Set("_txlock", "immediate")
	db, err := sql.Open("sqlite", sqliteURI(path, query))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err = db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return &Connection{DB: db, Driver: "sqlite"}, nil
}

func sqliteURI(path string, query url.Values) string {
	slashed := filepath.ToSlash(path)
	if !strings.HasPrefix(slashed, "/") {
		slashed = "/" + slashed
	}
	return (&url.URL{Scheme: "file", Path: slashed, RawQuery: query.Encode()}).String()
}

func (c *Connection) snapshotSQLite(ctx context.Context, q rowsQueryer) (Snapshot, error) {
	s := Snapshot{Driver: c.Driver, Tables: []Table{}}
	rows, err := q.QueryContext(ctx, "SELECT name, sql FROM sqlite_schema WHERE type='table' AND name NOT LIKE 'sqlite_%' AND name <> 'schema_migrations' ORDER BY name")
	if err != nil {
		return s, err
	}
	for rows.Next() {
		t := Table{Schema: "main"}
		if err = rows.Scan(&t.Name, &t.Definition); err != nil {
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
		if err = c.fillSQLite(ctx, q, &s.Tables[i]); err != nil {
			return s, err
		}
	}
	return s, nil
}

func (c *Connection) fillSQLite(ctx context.Context, q rowsQueryer, t *Table) error {
	t.Columns, t.Constraints, t.Indexes = []Column{}, []Constraint{}, []Index{}
	rows, err := q.QueryContext(ctx, "SELECT name, type, \"notnull\", dflt_value, pk, hidden FROM pragma_table_xinfo(?) ORDER BY cid", t.Name)
	if err != nil {
		return err
	}
	primary := map[int]string{}
	for rows.Next() {
		var col Column
		var notNull, pk, hidden int
		if err = rows.Scan(&col.Name, &col.Type, &notNull, &col.Default, &pk, &hidden); err != nil {
			rows.Close()
			return err
		}
		col.Nullable = notNull == 0
		if hidden == 2 || hidden == 3 {
			col.Generated = "s"
		}
		if pk > 0 {
			primary[pk] = col.Name
		}
		t.Columns = append(t.Columns, col)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if len(primary) > 0 {
		columns := make([]string, len(primary))
		for position, name := range primary {
			columns[position-1] = name
		}
		t.Constraints = append(t.Constraints, Constraint{Name: t.Name + "_pkey", Kind: "p", Definition: "PRIMARY KEY (" + joinQuoted(columns) + ")", Columns: columns})
	}
	if err = c.foreignKeysSQLite(ctx, q, t); err != nil {
		return err
	}
	return c.indexesSQLite(ctx, q, t)
}

func (c *Connection) foreignKeysSQLite(ctx context.Context, q rowsQueryer, t *Table) error {
	rows, err := q.QueryContext(ctx, `SELECT id, "table", "from", COALESCE("to", ''), on_delete FROM pragma_foreign_key_list(?) ORDER BY id, seq`, t.Name)
	if err != nil {
		return err
	}
	defer rows.Close()
	type key struct {
		table, onDelete string
		from, to        []string
	}
	keys := map[int]*key{}
	order := []int{}
	for rows.Next() {
		var id int
		var table, from, to, onDelete string
		if err = rows.Scan(&id, &table, &from, &to, &onDelete); err != nil {
			return err
		}
		if keys[id] == nil {
			keys[id] = &key{table: table, onDelete: onDelete}
			order = append(order, id)
		}
		keys[id].from = append(keys[id].from, from)
		if to != "" {
			keys[id].to = append(keys[id].to, to)
		}
	}
	for _, id := range order {
		k := keys[id]
		definition := "FOREIGN KEY (" + joinQuoted(k.from) + ") REFERENCES " + Quote(k.table)
		if len(k.to) > 0 {
			definition += "(" + joinQuoted(k.to) + ")"
		}
		if k.onDelete != "NO ACTION" {
			definition += " ON DELETE " + k.onDelete
		}
		t.Constraints = append(t.Constraints, Constraint{Name: t.Name + "_" + strings.Join(k.from, "_") + "_fkey", Kind: "f", Definition: definition, Columns: k.from})
	}
	return rows.Err()
}

func (c *Connection) indexesSQLite(ctx context.Context, q rowsQueryer, t *Table) error {
	rows, err := q.QueryContext(ctx, `SELECT l.name, l."unique", l.origin, l.partial, COALESCE(s.sql, '') FROM pragma_index_list(?) l LEFT JOIN sqlite_schema s ON s.type='index' AND s.name=l.name ORDER BY l.name`, t.Name)
	if err != nil {
		return err
	}
	type entry struct {
		index  Index
		origin string
	}
	entries := []entry{}
	for rows.Next() {
		var e entry
		var unique, partial int
		if err = rows.Scan(&e.index.Name, &unique, &e.origin, &partial, &e.index.Definition); err != nil {
			rows.Close()
			return err
		}
		e.index.Unique, e.index.Method = unique == 1, "btree"
		e.index.Constraint = e.origin != "c"
		entries = append(entries, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, e := range entries {
		columns, err := c.indexColumnsSQLite(ctx, q, e.index.Name)
		if err != nil {
			return err
		}
		e.index.Columns = columns
		if e.index.Definition == "" {
			label := map[string]string{"pk": "PRIMARY KEY", "u": "UNIQUE"}[e.origin]
			e.index.Definition = fmt.Sprintf("%s (%s)", label, joinQuoted(columns))
		}
		if e.origin == "u" {
			t.Constraints = append(t.Constraints, Constraint{Name: t.Name + "_" + strings.Join(columns, "_") + "_key", Kind: "u", Definition: "UNIQUE (" + joinQuoted(columns) + ")", Columns: columns})
		}
		t.Indexes = append(t.Indexes, e.index)
	}
	sort.Slice(t.Constraints, func(i, j int) bool { return t.Constraints[i].Name < t.Constraints[j].Name })
	return nil
}

func (c *Connection) indexColumnsSQLite(ctx context.Context, q rowsQueryer, index string) ([]string, error) {
	rows, err := q.QueryContext(ctx, "SELECT COALESCE(name, '<expr>') FROM pragma_index_info(?) ORDER BY seqno", index)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	columns := []string{}
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			return nil, err
		}
		columns = append(columns, name)
	}
	return columns, rows.Err()
}

func joinQuoted(names []string) string {
	quoted := make([]string, len(names))
	for i, name := range names {
		quoted[i] = Quote(name)
	}
	return strings.Join(quoted, ", ")
}
