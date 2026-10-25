package targetdb

import (
	"context"
	"encoding/json"

	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/sqlguard"
)

type Plan struct {
	Driver string `json:"driver"`
	Raw    any    `json:"raw"`
}

type PlanStep struct {
	ID     int    `json:"id"`
	Parent int    `json:"parent"`
	Detail string `json:"detail"`
}

func (c *Connection) Explain(ctx context.Context, query string) (Plan, error) {
	result := Plan{Driver: c.Driver}

	statement, err := sqlguard.Plan(query)
	if err != nil {
		return result, err
	}

	tx, err := c.DB.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()

	if c.Driver == "sqlite" {
		rows, err := tx.QueryContext(ctx, "EXPLAIN QUERY PLAN " + statement.SQL)
		if err != nil {
			return result, err
		}
		defer rows.Close()

		steps := []PlanStep{}
		for rows.Next() {
			var step PlanStep
			var unused int
			if err = rows.Scan(&step.ID, &step.Parent, &unused, &step.Detail); err != nil {
				return result, err
			}
			steps = append(steps, step)
		}

		result.Raw = steps
		return result, rows.Err()
	}
	if _, err = tx.ExecContext(ctx, "SET LOCAL statement_timeout='10s'"); err != nil {
		return result, err
	}
	
	var data []byte
	if err = tx.QueryRowContext(ctx, "EXPLAIN (FORMAT JSON) " + statement.SQL).Scan(&data); err != nil {
		return result, err
	}
	err = json.Unmarshal(data, &result.Raw)
	
	return result, err
}
