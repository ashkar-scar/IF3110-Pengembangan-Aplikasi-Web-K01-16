CREATE TABLE IF NOT EXISTS public.schema_migrations (
 version BIGINT PRIMARY KEY, attempt_id TEXT UNIQUE NOT NULL, sql_text TEXT NOT NULL,
 actor_id BIGINT NOT NULL, actor_name TEXT NOT NULL, executed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
ALTER TABLE public.schema_migrations
 ADD COLUMN IF NOT EXISTS draft_id BIGINT NOT NULL DEFAULT 0,
 ADD COLUMN IF NOT EXISTS draft_revision INTEGER NOT NULL DEFAULT 1,
 ADD COLUMN IF NOT EXISTS action_type TEXT NOT NULL DEFAULT 'UP' CHECK(action_type IN ('UP','ROLLBACK','FORWARD')),
 ADD COLUMN IF NOT EXISTS reverts_attempt_id TEXT,
 ADD COLUMN IF NOT EXISTS is_active BOOLEAN NOT NULL DEFAULT TRUE;
CREATE SCHEMA IF NOT EXISTS ruangdb_internal;
CREATE TABLE IF NOT EXISTS ruangdb_internal.jobs (
 job_id TEXT PRIMARY KEY,
 execution_id TEXT NOT NULL,
 draft_id BIGINT NOT NULL,
 draft_revision INTEGER NOT NULL,
 action_type TEXT NOT NULL,
 sql_up TEXT NOT NULL,
 sql_down TEXT NOT NULL,
 irreversible BOOLEAN NOT NULL,
 parent_execution_id TEXT NOT NULL DEFAULT '',
 source_job_id TEXT NOT NULL DEFAULT '',
 status TEXT NOT NULL DEFAULT 'RUNNING',
 version BIGINT NOT NULL DEFAULT 0,
 initial_cursor BIGINT NOT NULL DEFAULT 0,
 last_cursor BIGINT NOT NULL DEFAULT 0,
 upper_cursor BIGINT NOT NULL DEFAULT 0,
 total_rows BIGINT NOT NULL DEFAULT 0,
 total_batches INTEGER NOT NULL DEFAULT 0,
 completed_batches INTEGER NOT NULL DEFAULT 0,
 rows_processed BIGINT NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS ruangdb_internal.batches (
 job_id TEXT NOT NULL REFERENCES ruangdb_internal.jobs(job_id),
 batch_no INTEGER NOT NULL,
 cursor_start BIGINT NOT NULL,
 cursor_end BIGINT NOT NULL,
 row_ids BIGINT[] NOT NULL,
 before_rows JSONB NOT NULL,
 after_rows JSONB NOT NULL,
 rows_affected INTEGER NOT NULL,
 status TEXT NOT NULL CHECK(status IN ('COMMITTED','REVERTED')),
 committed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 reverted_at TIMESTAMPTZ,
 PRIMARY KEY(job_id,batch_no)
);
CREATE TABLE IF NOT EXISTS ruangdb_internal.work_items (
 job_id TEXT NOT NULL REFERENCES ruangdb_internal.jobs(job_id),
 row_id BIGINT NOT NULL,
 PRIMARY KEY(job_id,row_id)
);
