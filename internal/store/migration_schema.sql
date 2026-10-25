-- TODO(faiz): tambahkan REFERENCES ke users(id) dan tabel registry koneksi setelah DDL-nya tersedia.
CREATE TABLE IF NOT EXISTS migration_drafts (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    target_id BIGINT NOT NULL,
    description TEXT NOT NULL,
    sql_text TEXT NOT NULL,
    created_by BIGINT NOT NULL,
    revision INTEGER NOT NULL DEFAULT 1,
    base_version BIGINT NOT NULL,
    fingerprint TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

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
