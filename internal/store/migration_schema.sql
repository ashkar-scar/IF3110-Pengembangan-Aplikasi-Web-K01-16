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

CREATE TABLE IF NOT EXISTS migration_attempts (
    id TEXT PRIMARY KEY,
    target_id BIGINT NOT NULL,
    draft_id BIGINT NOT NULL REFERENCES migration_drafts(id),
    draft_revision INTEGER NOT NULL,
    description TEXT NOT NULL,
    sql_text TEXT NOT NULL,
    actor_id BIGINT NOT NULL,
    actor_name TEXT NOT NULL,
    base_version BIGINT NOT NULL,
    version BIGINT,
    status TEXT NOT NULL CHECK (status IN ('RUNNING', 'SUCCESS', 'FAILED', 'UNKNOWN')),
    safe_error TEXT NOT NULL DEFAULT '',
    started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at TIMESTAMPTZ,
    UNIQUE (draft_id, draft_revision)
);

CREATE INDEX IF NOT EXISTS migration_drafts_target_idx ON migration_drafts (target_id, updated_at);
CREATE INDEX IF NOT EXISTS migration_attempts_target_idx ON migration_attempts (target_id, started_at);