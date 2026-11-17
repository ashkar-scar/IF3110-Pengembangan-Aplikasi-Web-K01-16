package targetdb

import (
	"context"
	"encoding/json"

	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/sqlguard"
)

// Hasil mentah execution plan dari database target.
type Plan struct {
	Driver string `json:"driver"`
	Raw    any    `json:"raw"`
}

// Satu baris langkah EXPLAIN QUERY PLAN di SQLite.
type PlanStep struct {
	ID     int    `json:"id"`
	Parent int    `json:"parent"`
	Detail string `json:"detail"`
}

// Ambil execution plan query tanpa mengeksekusi mutasinya.
func (conn *Connection) Explain(ctx context.Context, query string) (Plan, error) {
	result := Plan{Driver: conn.Driver}

	statement, err := sqlguard.Plan(query)
	if err != nil {
		return result, err
	}

	// Transaksi tidak pernah di-commit agar preview selalu dibatalkan oleh rollback.
	tx, err := conn.DB.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()

	if conn.Driver == "sqlite" {
		rows, err := tx.QueryContext(ctx, "EXPLAIN QUERY PLAN "+statement.SQL)
		if err != nil {
			return result, err
		}
		defer rows.Close()

		steps := []PlanStep{}
		for rows.Next() {
			var step PlanStep
			var unused int
			err = rows.Scan(&step.ID, &step.Parent, &unused, &step.Detail)
			if err != nil {
				return result, err
			}
			steps = append(steps, step)
		}

		result.Raw = steps
		return result, rows.Err()
	}

	_, err = tx.ExecContext(ctx, "SET LOCAL statement_timeout = '10s'")
	if err != nil {
		return result, err
	}

	var data []byte
	err = tx.QueryRowContext(ctx, "EXPLAIN (FORMAT JSON) "+statement.SQL).Scan(&data)
	if err != nil {
		return result, err
	}

	err = json.Unmarshal(data, &result.Raw)
	return result, err
}
