package targetdb

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

const jsMaxSafeInteger = 9007199254740991

var ErrPagination = errors.New("pagination tidak valid")

// Satu halaman isi tabel beserta status halaman berikutnya.
type RowPage struct {
	Columns []string `json:"columns"`
	Rows    [][]any  `json:"rows"`
	Limit   int      `json:"limit"`
	Offset  int      `json:"offset"`
	HasMore bool     `json:"has_more"`
}

// Ambil satu halaman baris dari tabel yang ada di snapshot.
func (conn *Connection) Rows(ctx context.Context, schema, name string, limit, offset int) (RowPage, error) {
	page := RowPage{
		Rows:   [][]any{},
		Limit:  limit,
		Offset: offset,
	}

	if limit < 1 || limit > 200 || offset < 0 || offset > 1000000 {
		return page, ErrPagination
	}

	snapshot, err := conn.Snapshot(ctx)
	if err != nil {
		return page, err
	}
	table, err := snapshot.Find(schema, name)
	if err != nil {
		return page, err
	}

	query := "SELECT * FROM " + Quote(schema) + "." + Quote(name)
	if order := orderColumns(conn.Driver, table); len(order) > 0 {
		query += " ORDER BY " + strings.Join(order, ", ")
	}
	query += " LIMIT $1 OFFSET $2"

	// Ambil limit+1 baris supaya tahu masih ada halaman berikut tanpa query COUNT terpisah.
	rows, err := conn.DB.QueryContext(ctx, query, limit+1, offset)
	if err != nil {
		return page, err
	}
	defer rows.Close()

	page.Columns, err = rows.Columns()
	if err != nil {
		return page, err
	}

	for rows.Next() {
		values := make([]any, len(page.Columns))
		pointers := make([]any, len(values))
		for i := range values {
			pointers[i] = &values[i]
		}

		err = rows.Scan(pointers...)
		if err != nil {
			return page, err
		}

		if len(page.Rows) == limit {
			page.HasMore = true
			break
		}

		normalizeRowValues(values)
		page.Rows = append(page.Rows, values)
	}

	return page, rows.Err()
}

// Pilih kolom urut pagination (primary key atau row ID bawaan).
func orderColumns(driver string, table Table) []string {
	var order []string
	for _, con := range table.Constraints {
		if con.Kind == "p" {
			for _, column := range con.Columns {
				order = append(order, Quote(column))
			}
		}
	}
	if len(order) > 0 {
		return order
	}

	if driver == "postgres" {
		return []string{"ctid"}
	}

	for _, alias := range []string{"rowid", "_rowid_", "oid"} {
		if !hasColumn(table.Columns, alias) {
			return []string{Quote(alias)}
		}
	}
	return nil
}

// Cek apakah nama kolom sudah ada (case-insensitive).
func hasColumn(columns []Column, target string) bool {
	for _, col := range columns {
		if strings.EqualFold(col.Name, target) {
			return true
		}
	}
	return false
}

// Konversi byte, waktu, dan int64 besar supaya aman untuk JSON.
func normalizeRowValues(values []any) {
	for i, value := range values {
		switch v := value.(type) {
		case []byte:
			values[i] = string(v)
		case time.Time:
			values[i] = v.Format(time.RFC3339Nano)
		case int64:
			// Bilangan di atas batas aman integer JavaScript diubah ke string agar presisi tidak hilang.
			if v > jsMaxSafeInteger || v < -jsMaxSafeInteger {
				values[i] = fmt.Sprint(v)
			}
		}
	}
}
