package targetdb

import (
	"context"
	"database/sql"

	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/sqlguard"
)

func executePostgresStatement(ctx context.Context, tx *sql.Tx, statement sqlguard.PostgresStatement) (sql.Result, error) {
	prepared, err := tx.PrepareContext(ctx, statement.SQL())
	if err != nil {
		return nil, err
	}
	defer prepared.Close()
	return prepared.ExecContext(ctx)
}
