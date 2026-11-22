CREATE TABLE IF NOT EXISTS migration_jobs (
    id TEXT PRIMARY KEY,
    request_id BIGINT NOT NULL REFERENCES migration_requests(id),
    target_id BIGINT NOT NULL REFERENCES database_targets(id),
    action_type TEXT NOT NULL CHECK (action_type IN ('UP','ROLLBACK','FORWARD','CANCEL_BATCH')),
    execution_mode TEXT NOT NULL CHECK (execution_mode IN ('NORMAL','BATCH')),
    status TEXT NOT NULL CHECK (status IN ('PENDING','RUNNING','SUCCESS','FAILED','PARTIAL','PARTIALLY_REVERTED','REVERTED','UNKNOWN')),
    total_batches INTEGER NOT NULL DEFAULT 0,
    completed_batches INTEGER NOT NULL DEFAULT 0,
    rows_processed BIGINT NOT NULL DEFAULT 0,
    last_cursor TEXT NOT NULL DEFAULT '',
    retry_count INTEGER NOT NULL DEFAULT 0,
    max_retries INTEGER NOT NULL DEFAULT 3,
    next_retry_at TIMESTAMPTZ,
    locked_by TEXT NOT NULL DEFAULT '',
    lock_expires_at TIMESTAMPTZ,
    trace_context TEXT NOT NULL DEFAULT '',
    safe_error TEXT NOT NULL DEFAULT '',
    created_by BIGINT NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ
);
CREATE UNIQUE INDEX IF NOT EXISTS migration_jobs_active_request_uidx
    ON migration_jobs(request_id) WHERE status IN ('PENDING','RUNNING');
CREATE INDEX IF NOT EXISTS migration_jobs_pickup_idx ON migration_jobs(status,next_retry_at,created_at);
CREATE TABLE IF NOT EXISTS target_locks (
    target_id BIGINT PRIMARY KEY REFERENCES database_targets(id),
    job_id TEXT NOT NULL REFERENCES migration_jobs(id),
    worker_id TEXT NOT NULL,
    acquired_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL,
    blocked_reason TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS batch_checkpoints (
    job_id TEXT NOT NULL REFERENCES migration_jobs(id) ON DELETE CASCADE,
    batch_no INTEGER NOT NULL,
    cursor_start TEXT NOT NULL,
    cursor_end TEXT NOT NULL,
    rows_affected INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL CHECK (status IN ('COMMITTED','REVERTED','FAILED')),
    committed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    reverted_at TIMESTAMPTZ,
    PRIMARY KEY(job_id,batch_no)
);
ALTER TABLE migration_attempts DROP CONSTRAINT IF EXISTS migration_attempts_draft_id_draft_revision_key;
DO $$ BEGIN
 IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='migration_attempts'::regclass AND conname='migration_attempts_job_id_fkey') THEN
  ALTER TABLE migration_attempts ADD CONSTRAINT migration_attempts_job_id_fkey FOREIGN KEY(job_id) REFERENCES migration_jobs(id);
 END IF;
END $$;
