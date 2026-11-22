package observability

import (
	"context"
	"strings"
	"time"
	"unicode"

	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/sqlguard"
	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

func MaskSQL(query string) string {
	statements, err := sqlguard.Split(query)
	if err != nil {
		return "[SQL masked: unsupported syntax]"
	}
	parts := []string{}
	for _, s := range statements {
		tokens := append([]string{}, s.Tokens...)
		for i, t := range tokens {
			if t == "<literal>" || (len(t) > 0 && unicode.IsDigit(rune(t[0]))) {
				tokens[i] = "?"
			}
		}
		parts = append(parts, strings.Join(tokens, " "))
	}
	return strings.Join(parts, "; ")
}

type queryKey struct{}
type queryState struct {
	start time.Time
	query string
	span  trace.Span
}
type SQLTracer struct{ Database string }

func (t SQLTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	ctx, span := Start(ctx, "db.query")
	query := MaskSQL(data.SQL)
	span.SetAttributes(attribute.String("db.system.name", "postgresql"), attribute.String("db.namespace", t.Database), attribute.String("db.query.text", query))
	Event(ctx, "sql_start", "RUNNING", "database", t.Database, "query", query, "params", "[REDACTED]")
	return context.WithValue(ctx, queryKey{}, queryState{time.Now(), query, span})
}
func (t SQLTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	state, ok := ctx.Value(queryKey{}).(queryState)
	if !ok {
		return
	}
	status := "SUCCESS"
	if data.Err != nil {
		status = "FAILED"
	}
	Event(ctx, "sql_result", status, "database", t.Database, "query", state.query, "params", "[REDACTED]",
		"duration_ms", float64(time.Since(state.start).Microseconds())/1000, "rows_affected", data.CommandTag.RowsAffected(), "error", SafeError(data.Err), "sqlstate", SQLState(data.Err))
	switch strings.TrimSpace(state.query) {
	case "COMMIT":
		Event(ctx, "tx_commit", status)
	case "ROLLBACK":
		Event(ctx, "tx_rollback", status)
	}
	End(state.span, data.Err)
}
