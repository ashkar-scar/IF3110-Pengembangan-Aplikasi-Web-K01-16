CREATE TABLE IF NOT EXISTS migration_drafts (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    target_id BIGINT NOT NULL,
    description TEXT NOT NULL,
    sql_text TEXT NOT NULL DEFAULT '',
    sql_up TEXT NOT NULL DEFAULT '',
    sql_down TEXT NOT NULL DEFAULT '',
    is_irreversible BOOLEAN NOT NULL DEFAULT FALSE,
    irreversible_reason TEXT NOT NULL DEFAULT '',
    execution_mode TEXT NOT NULL DEFAULT 'NORMAL' CHECK (execution_mode IN ('NORMAL', 'BATCH')),
    batch_column TEXT NOT NULL DEFAULT '',
    batch_size INTEGER NOT NULL DEFAULT 1000 CHECK (batch_size BETWEEN 1 AND 100000),
    is_locked BOOLEAN NOT NULL DEFAULT FALSE,
    created_by BIGINT NOT NULL,
    revision INTEGER NOT NULL DEFAULT 1,
    base_version BIGINT NOT NULL,
    fingerprint TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE migration_drafts
    ADD COLUMN IF NOT EXISTS sql_up TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS sql_down TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS is_irreversible BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS irreversible_reason TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS execution_mode TEXT NOT NULL DEFAULT 'NORMAL' CHECK (execution_mode IN ('NORMAL', 'BATCH')),
    ADD COLUMN IF NOT EXISTS batch_column TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS batch_size INTEGER NOT NULL DEFAULT 1000 CHECK (batch_size BETWEEN 1 AND 100000),
    ADD COLUMN IF NOT EXISTS is_locked BOOLEAN NOT NULL DEFAULT FALSE;

UPDATE migration_drafts
SET sql_up = sql_text
WHERE sql_up = '' AND sql_text <> '';

CREATE TABLE IF NOT EXISTS migration_requests (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    target_id BIGINT NOT NULL REFERENCES database_targets(id),
    draft_id BIGINT NOT NULL REFERENCES migration_drafts(id),
    draft_revision INTEGER NOT NULL,
    action_type TEXT NOT NULL CHECK (action_type IN ('UP', 'ROLLBACK', 'FORWARD', 'CANCEL_BATCH')),
    target_execution_id TEXT,
    sql_snapshot_up TEXT NOT NULL,
    sql_snapshot_down TEXT NOT NULL,
    is_irreversible BOOLEAN NOT NULL,
    irreversible_reason TEXT NOT NULL DEFAULT '',
    execution_mode TEXT NOT NULL CHECK (execution_mode IN ('NORMAL', 'BATCH')),
    batch_column TEXT NOT NULL DEFAULT '',
    batch_size INTEGER NOT NULL DEFAULT 1000 CHECK (batch_size BETWEEN 1 AND 100000),
    base_version BIGINT NOT NULL,
    fingerprint TEXT NOT NULL DEFAULT '',
    requested_by BIGINT NOT NULL REFERENCES users(id),
    status TEXT NOT NULL CHECK (status IN ('PENDING_REVIEW', 'APPROVED', 'REJECTED', 'ENQUEUED', 'COMPLETED', 'FAILED')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Menyimpan keputusan leader setiap group yang memiliki akses ke target.
CREATE TABLE IF NOT EXISTS migration_approvals (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    request_id BIGINT NOT NULL REFERENCES migration_requests(id) ON DELETE CASCADE,
    group_id BIGINT NOT NULL REFERENCES groups(id),
    leader_id BIGINT NOT NULL REFERENCES users(id),
    decision TEXT NOT NULL DEFAULT 'PENDING' CHECK (decision IN ('PENDING', 'APPROVED', 'REJECTED')),
    note TEXT NOT NULL DEFAULT '',
    decided_at TIMESTAMPTZ,
    UNIQUE (request_id, group_id)
);

CREATE TABLE IF NOT EXISTS migration_attempts (
    id TEXT PRIMARY KEY,
    target_id BIGINT NOT NULL,
    draft_id BIGINT NOT NULL REFERENCES migration_drafts(id),
    draft_revision INTEGER NOT NULL,
    request_id BIGINT REFERENCES migration_requests(id),
    job_id TEXT,
    action_type TEXT NOT NULL DEFAULT 'UP' CHECK (action_type IN ('UP', 'ROLLBACK', 'FORWARD', 'CANCEL_BATCH')),
    reverts_execution_id TEXT,
    attempt_no INTEGER NOT NULL DEFAULT 1,
    batch_no INTEGER,
    description TEXT NOT NULL,
    sql_text TEXT NOT NULL,
    actor_id BIGINT NOT NULL,
    actor_name TEXT NOT NULL,
    base_version BIGINT NOT NULL,
    version BIGINT,
    status TEXT NOT NULL CHECK (status IN ('RUNNING', 'SUCCESS', 'FAILED', 'UNKNOWN')),
    safe_error TEXT NOT NULL DEFAULT '',
    started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS migration_drafts_target_idx ON migration_drafts (target_id, updated_at);
CREATE INDEX IF NOT EXISTS migration_requests_target_idx ON migration_requests (target_id, created_at DESC);
CREATE INDEX IF NOT EXISTS migration_approvals_request_idx ON migration_approvals (request_id, group_id);
CREATE INDEX IF NOT EXISTS migration_attempts_target_idx ON migration_attempts (target_id, started_at);
CREATE INDEX IF NOT EXISTS migration_attempts_reverts_idx ON migration_attempts (reverts_execution_id);