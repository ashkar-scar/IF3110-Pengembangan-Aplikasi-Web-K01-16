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

// Buka file SQLite terkelola dengan foreign key dan immediate lock aktif.
func OpenSQLite(ctx context.Context, dataDir, file string) (*Connection, error) {
	// Wajib 32-hex supaya nama file tidak bisa keluar folder atau menyelipkan parameter URI.
	if !sqliteFile.MatchString(file) {
		return nil, errors.New("nama file SQLite tidak valid")
	}

	dir, err := filepath.Abs(dataDir)
	if err != nil {
		return nil, err
	}
	err = os.MkdirAll(dir, 0o700)
	if err != nil {
		return nil, err
	}

	path := filepath.Join(dir, file)
	query := url.Values{}
	query.Add("_pragma", "foreign_keys(1)")
	query.Add("_pragma", "busy_timeout(5000)")
	// Kunci write diambil sejak BEGIN supaya migrasi bersamaan tidak gagal di tengah jalan.
	query.Set("_txlock", "immediate")

	db, err := sql.Open("sqlite", sqliteURI(path, query))
	if err != nil {
		return nil, err
	}
	// Batasi satu koneksi per pool untuk mencegah SQLITE_BUSY di proses yang sama.
	db.SetMaxOpenConns(1)

	err = db.PingContext(ctx)
	if err != nil {
		db.Close()
		return nil, err
	}

	return &Connection{DB: db, Driver: "sqlite"}, nil
}

// Susun URI file SQLite beserta parameter pragma.
func sqliteURI(path string, query url.Values) string {
	slashed := filepath.ToSlash(path)
	if !strings.HasPrefix(slashed, "/") {
		slashed = "/" + slashed
	}
	return (&url.URL{Scheme: "file", Path: slashed, RawQuery: query.Encode()}).String()
}

// Ambil struktur semua tabel pengguna di SQLite target.
func (conn *Connection) snapshotSQLite(ctx context.Context, queryer rowsQueryer) (Snapshot, error) {
	snapshot := Snapshot{
		Driver:      conn.Driver,
		SchemaNames: []string{"main"},
		Tables:      []Table{},
	}

	const tablesQuery = `
		SELECT name, sql
		FROM sqlite_schema
		WHERE type = 'table'
		  AND name NOT LIKE 'sqlite_%'
		  AND name <> 'schema_migrations'
		ORDER BY name
	`
	rows, err := queryer.QueryContext(ctx, tablesQuery)
	if err != nil {
		return snapshot, err
	}
	for rows.Next() {
		table := Table{Schema: "main"}
		err = rows.Scan(&table.Name, &table.Definition)
		if err != nil {
			rows.Close()
			return snapshot, err
		}
		snapshot.Tables = append(snapshot.Tables, table)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return snapshot, err
	}

	for i := range snapshot.Tables {
		err = conn.fillSQLite(ctx, queryer, &snapshot.Tables[i])
		if err != nil {
			return snapshot, err
		}
	}

	return snapshot, nil
}

// Isi kolom, PK, FK, dan index satu tabel SQLite.
func (conn *Connection) fillSQLite(ctx context.Context, queryer rowsQueryer, table *Table) error {
	table.Columns = []Column{}
	table.Constraints = []Constraint{}
	table.Indexes = []Index{}

	const columnsQuery = `
		SELECT name, type, "notnull", dflt_value, pk, hidden
		FROM pragma_table_xinfo(?)
		ORDER BY cid
	`
	rows, err := queryer.QueryContext(ctx, columnsQuery, table.Name)
	if err != nil {
		return err
	}

	primaryKeys := map[int]string{}
	for rows.Next() {
		var col Column
		var notNull, pk, hidden int
		err = rows.Scan(&col.Name, &col.Type, &notNull, &col.Default, &pk, &hidden)
		if err != nil {
			rows.Close()
			return err
		}
		col.Nullable = notNull == 0
		if hidden == 2 || hidden == 3 {
			col.Generated = "s"
		}
		if pk > 0 {
			primaryKeys[pk] = col.Name
		}
		table.Columns = append(table.Columns, col)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}

	if len(primaryKeys) > 0 {
		columns := make([]string, len(primaryKeys))
		for position, name := range primaryKeys {
			columns[position-1] = name
		}
		table.Constraints = append(table.Constraints, Constraint{
			Name:       table.Name + "_pkey",
			Kind:       "p",
			Definition: "PRIMARY KEY (" + joinQuoted(columns) + ")",
			Columns:    columns,
		})
	}

	err = conn.foreignKeysSQLite(ctx, queryer, table)
	if err != nil {
		return err
	}
	return conn.indexesSQLite(ctx, queryer, table)
}

// Muat foreign key tabel SQLite beserta aksi ON DELETE-nya.
func (conn *Connection) foreignKeysSQLite(ctx context.Context, queryer rowsQueryer, table *Table) error {
	const fkQuery = `
		SELECT id, "table", "from", COALESCE("to", ''), on_delete
		FROM pragma_foreign_key_list(?)
		ORDER BY id, seq
	`
	rows, err := queryer.QueryContext(ctx, fkQuery, table.Name)
	if err != nil {
		return err
	}
	defer rows.Close()

	type foreignKey struct {
		table    string
		onDelete string
		from     []string
		to       []string
	}
	keys := map[int]*foreignKey{}
	order := []int{}

	for rows.Next() {
		var id int
		var refTable, fromCol, toCol, onDelete string
		err = rows.Scan(&id, &refTable, &fromCol, &toCol, &onDelete)
		if err != nil {
			return err
		}
		if keys[id] == nil {
			keys[id] = &foreignKey{table: refTable, onDelete: onDelete}
			order = append(order, id)
		}
		keys[id].from = append(keys[id].from, fromCol)
		if toCol != "" {
			keys[id].to = append(keys[id].to, toCol)
		}
	}

	for _, id := range order {
		fk := keys[id]
		definition := "FOREIGN KEY (" + joinQuoted(fk.from) + ") REFERENCES " + Quote(fk.table)
		if len(fk.to) > 0 {
			definition += "(" + joinQuoted(fk.to) + ")"
		}
		if fk.onDelete != "NO ACTION" {
			definition += " ON DELETE " + fk.onDelete
		}
		table.Constraints = append(table.Constraints, Constraint{
			Name:       table.Name + "_" + strings.Join(fk.from, "_") + "_fkey",
			Kind:       "f",
			Definition: definition,
			Columns:    fk.from,
		})
	}

	return rows.Err()
}

// Muat daftar index dan constraint UNIQUE dari pragma SQLite.
func (conn *Connection) indexesSQLite(ctx context.Context, queryer rowsQueryer, table *Table) error {
	const indexListQuery = `
		SELECT l.name, l."unique", l.origin, l.partial, COALESCE(s.sql, '')
		FROM pragma_index_list(?) AS l
		LEFT JOIN sqlite_schema AS s
			ON s.type = 'index'
			AND s.name = l.name
		ORDER BY l.name
	`
	rows, err := queryer.QueryContext(ctx, indexListQuery, table.Name)
	if err != nil {
		return err
	}

	type indexEntry struct {
		index  Index
		origin string
	}
	entries := []indexEntry{}
	for rows.Next() {
		var entry indexEntry
		var unique, partial int
		err = rows.Scan(&entry.index.Name, &unique, &entry.origin, &partial, &entry.index.Definition)
		if err != nil {
			rows.Close()
			return err
		}
		entry.index.Unique = unique == 1
		entry.index.Method = "btree"
		entry.index.Constraint = entry.origin != "c"
		entries = append(entries, entry)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}

	for _, entry := range entries {
		columns, err := conn.indexColumnsSQLite(ctx, queryer, entry.index.Name)
		if err != nil {
			return err
		}
		entry.index.Columns = columns
		if entry.index.Definition == "" {
			label := map[string]string{"pk": "PRIMARY KEY", "u": "UNIQUE"}[entry.origin]
			entry.index.Definition = fmt.Sprintf("%s (%s)", label, joinQuoted(columns))
		}
		if entry.origin == "u" {
			table.Constraints = append(table.Constraints, Constraint{
				Name:       table.Name + "_" + strings.Join(columns, "_") + "_key",
				Kind:       "u",
				Definition: "UNIQUE (" + joinQuoted(columns) + ")",
				Columns:    columns,
			})
		}
		table.Indexes = append(table.Indexes, entry.index)
	}

	sort.Slice(table.Constraints, func(i, j int) bool {
		return table.Constraints[i].Name < table.Constraints[j].Name
	})
	return nil
}

// Ambil urutan nama kolom satu index SQLite.
func (conn *Connection) indexColumnsSQLite(ctx context.Context, queryer rowsQueryer, indexName string) ([]string, error) {
	const query = `
		SELECT COALESCE(name, '<expr>')
		FROM pragma_index_info(?)
		ORDER BY seqno
	`
	rows, err := queryer.QueryContext(ctx, query, indexName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	columns := []string{}
	for rows.Next() {
		var name string
		err = rows.Scan(&name)
		if err != nil {
			return nil, err
		}
		columns = append(columns, name)
	}

	return columns, rows.Err()
}

// Kutip setiap identifier lalu gabungkan dengan koma.
func joinQuoted(names []string) string {
	quoted := make([]string, len(names))
	for i, name := range names {
		quoted[i] = Quote(name)
	}
	return strings.Join(quoted, ", ")
}
