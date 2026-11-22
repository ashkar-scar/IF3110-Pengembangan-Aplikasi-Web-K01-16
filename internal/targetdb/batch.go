package targetdb

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/core"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/observability"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/sqlguard"
)

type BatchCheckpoint struct {
	BatchNo     int
	Start       int64
	End         int64
	Rows        int
	Status      string
	CommittedAt time.Time
	RevertedAt  *time.Time
}

type BatchPlan struct {
	Table  string
	Column string
	Up     string
	Down   string
}

type BatchExecutionError struct {
	BatchNo int
	SQL     string
	Err     error
}

func (e *BatchExecutionError) Error() string { return observability.SafeError(e.Err) }
func (e *BatchExecutionError) Unwrap() error { return e.Err }

var batchUpdate = regexp.MustCompile(`(?is)^\s*UPDATE\s+([a-z_][a-z0-9_]*(?:\.[a-z_][a-z0-9_]*)?)\s+SET\s+(.+?)\s+WHERE\s+([a-z_][a-z0-9_]*)\s+IN\s*\(\s*SELECT\s+([a-z_][a-z0-9_]*)\s+FROM\s+([a-z_][a-z0-9_]*(?:\.[a-z_][a-z0-9_]*)?)\s+WHERE\s+([a-z_][a-z0-9_]*)\s*>\s*\$1\s+(?:AND\s+([a-z_][a-z0-9_]*)\s*<=\s*\$2\s+)?ORDER\s+BY\s+([a-z_][a-z0-9_]*)(?:\s+ASC)?\s+LIMIT\s+\$([23])\s*\)\s*;?\s*$`)
var assignments = regexp.MustCompile(`(?is)^\s*[a-z_][a-z0-9_]*\s*=\s*(?:'(?:[^']|'')*'|-?[0-9]+(?:\.[0-9]+)?|TRUE|FALSE|NULL)\s*(?:,\s*[a-z_][a-z0-9_]*\s*=\s*(?:'(?:[^']|'')*'|-?[0-9]+(?:\.[0-9]+)?|TRUE|FALSE|NULL)\s*)*$`)

func ParseBatch(up, down, column string, irreversible bool) (BatchPlan, error) {
	plan := BatchPlan{Column: column, Up: strings.TrimSuffix(strings.TrimSpace(up), ";"), Down: strings.TrimSuffix(strings.TrimSpace(down), ";")}
	validate := func(query string, isDown bool) error {
		statements, err := sqlguard.Migration(query)
		if err != nil || len(statements) != 1 {
			return batchError("gunakan satu UPDATE cursor")
		}
		m := batchUpdate.FindStringSubmatch(query)
		if m == nil || !assignments.MatchString(m[2]) {
			return batchError("bentuk UPDATE atau assignment tidak didukung")
		}
		table := strings.ToLower(m[1])
		if plan.Table == "" {
			plan.Table = table
		}
		if plan.Table != table || !strings.EqualFold(m[5], table) {
			return batchError("tabel UP dan DOWN harus sama")
		}
		for _, index := range []int{3, 4, 6, 8} {
			if m[index] != column {
				return batchError("semua cursor harus memakai batch_column")
			}
		}
		if isDown {
			if m[7] != column || m[9] != "3" {
				return batchError("DOWN memakai batas $1, $2 dan ukuran $3")
			}
		} else if m[7] != "" || m[9] != "2" {
			return batchError("UP memakai cursor $1 dan ukuran $2")
		}
		tokens := statements[0].Tokens
		for i, t := range tokens {
			if strings.EqualFold(t, column) && i+1 < len(tokens) && tokens[i+1] == "=" {
				return batchError("cursor tidak boleh diubah")
			}
		}
		return nil
	}
	if err := validate(plan.Up, false); err != nil {
		return plan, err
	}
	if !irreversible {
		if err := validate(plan.Down, true); err != nil {
			return plan, err
		}
	}
	return plan, nil
}

func (p BatchPlan) tableSQL() string {
	parts := strings.Split(p.Table, ".")
	if len(parts) == 1 {
		parts = append([]string{"public"}, parts...)
	}
	return Quote(parts[0]) + "." + Quote(parts[1])
}

func validateBatchTable(ctx context.Context, tx *sql.Tx, p BatchPlan) error {
	var valid bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_attribute a JOIN pg_index i ON i.indrelid=a.attrelid
 WHERE a.attrelid=$1::regclass AND a.attname=$2 AND a.attnotnull AND a.atttypid IN ('int2'::regtype,'int4'::regtype,'int8'::regtype)
 AND i.indisunique AND i.indisvalid AND i.indpred IS NULL AND i.indexprs IS NULL AND i.indnkeyatts=1 AND i.indkey[0]=a.attnum)
 AND NOT EXISTS(SELECT 1 FROM pg_trigger WHERE tgrelid=$1::regclass AND NOT tgisinternal AND tgenabled<>'D')
 AND EXISTS(SELECT 1 FROM pg_class WHERE oid=$1::regclass AND relkind='r' AND NOT relrowsecurity)
 AND NOT EXISTS(SELECT 1 FROM pg_inherits WHERE inhparent=$1::regclass OR inhrelid=$1::regclass)
 AND NOT EXISTS(SELECT 1 FROM pg_constraint WHERE confrelid=$1::regclass AND contype='f' AND confupdtype NOT IN ('a','r'))`, p.tableSQL(), p.Column).Scan(&valid)
	if err != nil {
		return err
	}
	if !valid {
		return batchError("gunakan tabel biasa tanpa trigger/RLS/inheritance dan cursor integer UNIQUE NOT NULL")
	}
	return nil
}

func (s *WorkerSession) beginBatch(ctx context.Context, p BatchPlan) (*sql.Tx, error) {
	tx, err := s.begin(ctx)
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, "SET LOCAL search_path=pg_catalog,public"); err == nil {
		_, err = tx.ExecContext(ctx, "LOCK TABLE "+p.tableSQL()+" IN SHARE ROW EXCLUSIVE MODE")
	}
	if err == nil {
		err = validateBatchTable(ctx, tx, p)
	}
	if err != nil {
		tx.Rollback()
		return nil, err
	}
	return tx, nil
}

func (s *WorkerSession) initializeBatch(ctx context.Context, e WorkerExecution, p BatchPlan) error {
	tx, err := s.beginBatch(ctx, p)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	parent, err := s.validate(ctx, tx, e)
	if err != nil {
		return err
	}
	if err = insertWorkerState(ctx, tx, e, parent); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO ruangdb_internal.work_items(job_id,row_id) SELECT $1,`+Quote(p.Column)+` FROM `+p.tableSQL(), e.JobID)
	if err != nil {
		return err
	}
	var low, high, total int64
	err = tx.QueryRowContext(ctx, `SELECT COALESCE(MIN(row_id),1),COALESCE(MAX(row_id),0),COUNT(*) FROM ruangdb_internal.work_items WHERE job_id=$1`, e.JobID).Scan(&low, &high, &total)
	if err != nil {
		return err
	}
	if low == math.MinInt64 {
		return batchError("cursor minimum BIGINT tidak didukung")
	}
	batches := (total + int64(e.Request.BatchSize) - 1) / int64(e.Request.BatchSize)
	_, err = tx.ExecContext(ctx, `UPDATE ruangdb_internal.jobs SET initial_cursor=$2,last_cursor=$2,upper_cursor=$3,total_rows=$4,total_batches=$5 WHERE job_id=$1`, e.JobID, low-1, high, total, batches)
	if err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return ErrUnknown
	}
	return nil
}

func (s *WorkerSession) ExecuteBatch(ctx context.Context, e WorkerExecution, progress func(WorkerResult) error) (WorkerResult, error) {
	p, err := ParseBatch(e.Request.SQLSnapshotUp, e.Request.SQLSnapshotDown, e.Request.BatchColumn, e.Request.IsIrreversible)
	if err != nil {
		return WorkerResult{}, err
	}
	if e.Request.Action == core.ActionRollback || e.Request.Action == core.ActionCancelBatch {
		return s.revertBatches(ctx, e, p, progress)
	}
	if e.Request.BatchSize < 1 || e.Request.BatchSize > 100000 {
		return WorkerResult{}, ErrBatchUnsafe
	}
	state, err := s.State(ctx, e.JobID)
	if errors.Is(err, sql.ErrNoRows) {
		if err = s.initializeBatch(ctx, e, p); err != nil {
			return state, err
		}
		state, err = s.State(ctx, e.JobID)
	}
	if err != nil {
		return state, err
	}
	if state.Status == "SUCCESS" {
		return state, nil
	}
	for state.CompletedBatches < state.TotalBatches {
		if err = progress(state); err != nil {
			return state, err
		}
		if err = ctx.Err(); err != nil {
			return state, err
		}
		if err = s.applyBatch(ctx, e, p, state); err != nil {
			return state, err
		}
		state, err = s.State(ctx, e.JobID)
		if err != nil {
			return state, err
		}
		if err = progress(state); err != nil {
			return state, err
		}
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return state, err
	}
	defer tx.Rollback()
	if _, err = finishLedger(ctx, tx, e); err != nil {
		return state, err
	}
	if err = tx.Commit(); err != nil {
		return state, ErrUnknown
	}
	return s.State(ctx, e.JobID)
}

func rowSnapshot(ctx context.Context, tx *sql.Tx, p BatchPlan, ids []int64) ([]byte, error) {
	var data []byte
	err := tx.QueryRowContext(ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY t.`+Quote(p.Column)+`),'[]'::jsonb) FROM `+p.tableSQL()+` t WHERE t.`+Quote(p.Column)+`=ANY($1::bigint[])`, ids).Scan(&data)
	return data, err
}

func (s *WorkerSession) applyBatch(ctx context.Context, e WorkerExecution, p BatchPlan, state WorkerResult) (err error) {
	ctx, span := observability.Start(ctx, "db.batch")
	defer func() { observability.End(span, err) }()
	ctx = observability.WithFields(ctx, "batch_no", state.CompletedBatches+1)
	tx, err := s.beginBatch(ctx, p)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT row_id FROM ruangdb_internal.work_items WHERE job_id=$1 AND row_id>$2 ORDER BY row_id LIMIT $3`, e.JobID, state.LastCursor, e.Request.BatchSize)
	if err != nil {
		return err
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return ErrBatchUnsafe
	}
	before, err := rowSnapshot(ctx, tx, p, ids)
	if err != nil {
		return err
	}
	var snapshots []json.RawMessage
	if err = json.Unmarshal(before, &snapshots); err != nil {
		return err
	}
	if len(snapshots) != len(ids) {
		return batchError("cakupan row berubah sejak job dimulai")
	}
	query := p.Up + " RETURNING " + Quote(p.Column)
	rows, err = tx.QueryContext(ctx, query, state.LastCursor, len(ids))
	if err != nil {
		return &BatchExecutionError{state.CompletedBatches + 1, query, err}
	}
	actual := []int64{}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		actual = append(actual, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return &BatchExecutionError{state.CompletedBatches + 1, query, err}
	}
	slices.Sort(actual)
	if !slices.Equal(ids, actual) {
		return batchError("UPDATE mengubah cakupan row yang berbeda")
	}
	after, err := rowSnapshot(ctx, tx, p, ids)
	if err != nil {
		return err
	}
	end := ids[len(ids)-1]
	batch := state.CompletedBatches + 1
	_, err = tx.ExecContext(ctx, `INSERT INTO ruangdb_internal.batches(job_id,batch_no,cursor_start,cursor_end,row_ids,before_rows,after_rows,rows_affected,status)
 VALUES($1,$2,$3,$4,$5,$6::jsonb,$7::jsonb,$8,'COMMITTED')`, e.JobID, batch, state.LastCursor, end, ids, string(before), string(after), len(ids))
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE ruangdb_internal.jobs SET last_cursor=$2,completed_batches=completed_batches+1,rows_processed=rows_processed+$3 WHERE job_id=$1`, e.JobID, end, len(ids))
	if err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return ErrUnknown
	}
	observability.Event(ctx, "batch_commit", "SUCCESS", "batch_no", batch, "rows_affected", len(ids))
	return nil
}

func (s *WorkerSession) revertBatches(ctx context.Context, e WorkerExecution, p BatchPlan, progress func(WorkerResult) error) (WorkerResult, error) {
	if e.Request.IsIrreversible || e.SourceJobID == "" {
		return WorkerResult{}, ErrBatchUnsafe
	}
	state, err := s.State(ctx, e.JobID)
	if errors.Is(err, sql.ErrNoRows) {
		var tx *sql.Tx
		tx, err = s.begin(ctx)
		if err != nil {
			return state, err
		}
		defer tx.Rollback()
		if err = checkTargetBlockers(ctx, tx, e); err != nil {
			return state, err
		}
		parent := ""
		if e.Request.Action == core.ActionRollback {
			parent, err = s.validate(ctx, tx, e)
			if err != nil {
				return state, err
			}
		} else {
			var up, down, status string
			var draftID int64
			var revision int
			err = tx.QueryRowContext(ctx, `SELECT sql_up,sql_down,status,draft_id,draft_revision FROM ruangdb_internal.jobs WHERE job_id=$1`, e.SourceJobID).Scan(&up, &down, &status, &draftID, &revision)
			if err != nil || up != e.Request.SQLSnapshotUp || down != e.Request.SQLSnapshotDown || status == "REVERTED" || draftID != e.Request.DraftID || revision != e.Request.DraftRevision {
				return state, ErrBatchUnsafe
			}
			var later bool
			err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM public.schema_migrations m
 JOIN ruangdb_internal.jobs source ON source.job_id=$1
 WHERE m.is_active AND m.version>CASE WHEN source.status='SUCCESS' THEN source.version
 ELSE COALESCE((SELECT parent.version FROM public.schema_migrations parent WHERE parent.attempt_id=source.parent_execution_id),0) END)`, e.SourceJobID).Scan(&later)
			if err != nil {
				return state, err
			}
			if later {
				return state, ErrLinear
			}
		}
		if err = insertWorkerState(ctx, tx, e, parent); err != nil {
			return state, err
		}
		_, err = tx.ExecContext(ctx, `UPDATE ruangdb_internal.jobs SET total_batches=(SELECT COUNT(*) FROM ruangdb_internal.batches WHERE job_id=$2 AND status='COMMITTED') WHERE job_id=$1`, e.JobID, e.SourceJobID)
		if err != nil {
			return state, err
		}
		if err = tx.Commit(); err != nil {
			return state, ErrUnknown
		}
		state, err = s.State(ctx, e.JobID)
	}
	if err != nil {
		return state, err
	}
	if state.Status == "SUCCESS" || state.Status == "REVERTED" {
		return state, nil
	}
	for state.CompletedBatches < state.TotalBatches {
		if err = progress(state); err != nil {
			return state, err
		}
		if err = s.revertBatch(ctx, e, p); err != nil {
			return state, err
		}
		state, err = s.State(ctx, e.JobID)
		if err != nil {
			return state, err
		}
		if err = progress(state); err != nil {
			return state, err
		}
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return state, err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `UPDATE ruangdb_internal.jobs SET status='REVERTED' WHERE job_id=$1`, e.SourceJobID)
	if err != nil {
		return state, err
	}
	if e.Request.Action == core.ActionRollback {
		_, err = finishLedger(ctx, tx, e)
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE public.schema_migrations SET is_active=false WHERE attempt_id=(SELECT execution_id FROM ruangdb_internal.jobs WHERE job_id=$1)`, e.SourceJobID)
		if err == nil {
			_, err = tx.ExecContext(ctx, `UPDATE ruangdb_internal.jobs SET status='REVERTED',execution_id=$2 WHERE job_id=$1`, e.JobID, e.ExecutionID)
		}
	}
	if err != nil {
		return state, err
	}
	if err = tx.Commit(); err != nil {
		return state, ErrUnknown
	}
	return s.State(ctx, e.JobID)
}

func (s *WorkerSession) revertBatch(ctx context.Context, e WorkerExecution, p BatchPlan) (err error) {
	ctx, span := observability.Start(ctx, "db.batch.compensate")
	defer func() { observability.End(span, err) }()
	tx, err := s.beginBatch(ctx, p)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var batch, affected int
	var start, end int64
	var idsJSON, before, after []byte
	err = tx.QueryRowContext(ctx, `SELECT batch_no,cursor_start,cursor_end,to_json(row_ids),before_rows,after_rows,rows_affected FROM ruangdb_internal.batches
 WHERE job_id=$1 AND status='COMMITTED' ORDER BY batch_no DESC LIMIT 1 FOR UPDATE`, e.SourceJobID).Scan(&batch, &start, &end, &idsJSON, &before, &after, &affected)
	if err != nil {
		return err
	}
	var ids []int64
	ctx = observability.WithFields(ctx, "batch_no", batch)
	if err = json.Unmarshal(idsJSON, &ids); err != nil {
		return err
	}
	current, err := rowSnapshot(ctx, tx, p, ids)
	if err != nil {
		return err
	}
	if string(current) != string(after) {
		return batchError("row berubah setelah batch; buat migration perbaikan")
	}
	result, err := tx.ExecContext(ctx, p.Down, start, end, affected)
	if err != nil {
		return &BatchExecutionError{batch, p.Down, err}
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != int64(affected) {
		return ErrBatchUnsafe
	}
	restored, err := rowSnapshot(ctx, tx, p, ids)
	if err != nil {
		return err
	}
	if string(restored) != string(before) {
		return batchError("SQL DOWN tidak memulihkan data asli")
	}
	_, err = tx.ExecContext(ctx, `UPDATE ruangdb_internal.batches SET status='REVERTED',reverted_at=now() WHERE job_id=$1 AND batch_no=$2`, e.SourceJobID, batch)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO ruangdb_internal.batches(job_id,batch_no,cursor_start,cursor_end,row_ids,before_rows,after_rows,rows_affected,status,reverted_at)
 VALUES($1,$2,$3,$4,$5,$6::jsonb,$7::jsonb,$8,'REVERTED',now())`, e.JobID, batch, start, end, ids, string(before), string(after), affected)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE ruangdb_internal.jobs SET completed_batches=completed_batches+1,rows_processed=rows_processed+$2,last_cursor=$3 WHERE job_id=$1`, e.JobID, affected, start)
	if err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return ErrUnknown
	}
	observability.Event(ctx, "batch_compensation_commit", "SUCCESS", "batch_no", batch, "rows_affected", affected)
	return nil
}

func (c BatchCheckpoint) CursorStart() string { return strconv.FormatInt(c.Start, 10) }
func (c BatchCheckpoint) CursorEnd() string   { return strconv.FormatInt(c.End, 10) }
func (p BatchPlan) String() string            { return fmt.Sprintf("%s by %s", p.Table, p.Column) }
