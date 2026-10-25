package targetdb

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrPagination = errors.New("pagination tidak valid")

type RowPage struct {
	Columns []string `json:"columns"`
	Rows    [][]any  `json:"rows"`
	Limit   int      `json:"limit"`
	Offset  int      `json:"offset"`
	HasMore bool     `json:"has_more"`
}

func (c *Connection) Rows(ctx context.Context, schema, name string, limit, offset int) (RowPage, error) {
	page := RowPage{Rows: [][]any{}, Limit: limit, Offset: offset}
	if limit < 1 || limit > 200 || offset < 0 || offset > 1000000 {
		return page, ErrPagination
	}
	snapshot, err := c.Snapshot(ctx)
	if err != nil {
		return page, err
	}
	table, err := snapshot.Find(schema, name)
	if err != nil {
		return page, err
	}
	order := []string{}
	for _, con := range table.Constraints {
		if con.Kind == "p" {
			for _, column := range con.Columns {
				order = append(order, Quote(column))
			}
		}
	}
	query := "SELECT * FROM " + Quote(schema) + "." + Quote(name)
	if len(order) > 0 {
		query += " ORDER BY " + strings.Join(order, ",")
	}
	query += " LIMIT $1 OFFSET $2"
	rows, err := c.DB.QueryContext(ctx, query, limit+1, offset)
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
		if err = rows.Scan(pointers...); err != nil {
			return page, err
		}
		if len(page.Rows) == limit {
			page.HasMore = true
			break
		}
		for i, value := range values {
			switch v := value.(type) {
			case []byte:
				values[i] = string(v)
			case time.Time:
				values[i] = v.Format(time.RFC3339Nano)
			case int64:
				if v > 9007199254740991 || v < -9007199254740991 {
					values[i] = fmt.Sprint(v)
				}
			}
		}
		page.Rows = append(page.Rows, values)
	}
	return page, rows.Err()
}
