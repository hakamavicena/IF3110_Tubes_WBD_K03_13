-- Skema DDL Database Internal Platform IF3110 (Milestone 1 & Fondasi Milestone 2)
-- Urutan DDL: users, sessions, groups, user_groups, database_targets, group_database_access,
--             migrations, migration_jobs, migration_approvals, migration_history, database_migration_locks

DROP TABLE IF EXISTS database_migration_locks CASCADE;
DROP TABLE IF EXISTS migration_history CASCADE;
DROP TABLE IF EXISTS migration_approvals CASCADE;
DROP TABLE IF EXISTS migration_jobs CASCADE;
DROP TABLE IF EXISTS migrations CASCADE;
DROP TABLE IF EXISTS group_database_access CASCADE;
DROP TABLE IF EXISTS database_targets CASCADE;
DROP TABLE IF EXISTS user_groups CASCADE;
DROP TABLE IF EXISTS groups CASCADE;
DROP TABLE IF EXISTS sessions CASCADE;
DROP TABLE IF EXISTS users CASCADE;

CREATE TABLE users (
    id            BIGSERIAL PRIMARY KEY,
    username      VARCHAR(50) UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    is_admin      BOOLEAN NOT NULL DEFAULT FALSE,
    is_active     BOOLEAN NOT NULL DEFAULT TRUE
);
ALTER TABLE users ADD COLUMN IF NOT EXISTS is_active BOOLEAN NOT NULL DEFAULT TRUE;


CREATE TABLE sessions (
    session_id          TEXT PRIMARY KEY,
    user_id             BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    csrf_token          TEXT NOT NULL,
    created_at          TIMESTAMPTZ NOT NULL,
    last_activity       TIMESTAMPTZ NOT NULL,
    absolute_expiration TIMESTAMPTZ NOT NULL,
    previous_id         TEXT,
    previous_valid_until TIMESTAMPTZ
);

CREATE TABLE groups (
    id        BIGSERIAL PRIMARY KEY,
    name      VARCHAR(100) UNIQUE NOT NULL,
    leader_id BIGINT REFERENCES users(id) ON DELETE SET NULL
);

CREATE TABLE user_groups (
    user_id  BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    group_id BIGINT NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, group_id)
);

CREATE TABLE database_targets (
    id            BIGSERIAL PRIMARY KEY,
    name          TEXT NOT NULL UNIQUE,
    host          TEXT NOT NULL,
    port          INT NOT NULL DEFAULT 5432 CHECK (port BETWEEN 1 AND 65535),
    database_name TEXT NOT NULL,
    username      TEXT NOT NULL,
    password_enc  BYTEA NOT NULL,
    ssl_mode      TEXT NOT NULL DEFAULT 'prefer',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE group_database_access (
    group_id     BIGINT NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    database_id  BIGINT NOT NULL REFERENCES database_targets(id) ON DELETE CASCADE,
    access_level INT NOT NULL CHECK (access_level BETWEEN 0 AND 2),
    PRIMARY KEY (group_id, database_id)
);

-- Tabel Definisi Migration (Kompatibel dengan M1 + Kolom Baru M2 untuk Up/Down, Irreversible, & Batch)
CREATE TABLE migrations (
    id                  BIGSERIAL PRIMARY KEY,
    database_id         BIGINT NOT NULL REFERENCES database_targets(id) ON DELETE CASCADE,
    version             INT NOT NULL,
    revision            INT NOT NULL DEFAULT 1,
    title               TEXT NOT NULL,
    sql                 TEXT NOT NULL, -- Kolom SQL up utama (kompatibel dengan M1)
    sql_down            TEXT,          -- M2: SQL untuk rollback (kosong jika is_irreversible = TRUE)
    is_irreversible     BOOLEAN NOT NULL DEFAULT FALSE,
    irreversible_reason TEXT,
    execution_mode      TEXT NOT NULL DEFAULT 'standard' CHECK (execution_mode IN ('standard', 'batch')),
    batch_column        TEXT,
    batch_size          INT CHECK (batch_size IS NULL OR batch_size > 0),
    is_immutable        BOOLEAN NOT NULL DEFAULT FALSE,
    active_state        TEXT NOT NULL DEFAULT 'not_applied'
                        CHECK (active_state IN ('not_applied', 'applied', 'rolled_back', 'partial', 'partially_reverted')),
    active_execution_id BIGINT,
    status              TEXT NOT NULL CHECK (status IN (
                            'draft', 'pending_review', 'approved', 'pending_queue',
                            'running', 'success', 'failed', 'partial', 'partially_reverted'
                        )),
    error_message       TEXT,
    created_by          BIGINT NOT NULL REFERENCES users(id),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    executed_by         BIGINT REFERENCES users(id),
    executed_at         TIMESTAMPTZ,
    duration_ms         INT,
    UNIQUE (database_id, version)
);

CREATE INDEX IF NOT EXISTS idx_migrations_db ON migrations(database_id, version);

-- M2: Tabel Job / Attempt Eksekusi Migration (Apply, Rollback, Forward, Retry Batch, Cancel Batch)
CREATE TABLE migration_jobs (
    id                   BIGSERIAL PRIMARY KEY,
    database_id          BIGINT NOT NULL REFERENCES database_targets(id) ON DELETE CASCADE,
    migration_id         BIGINT NOT NULL REFERENCES migrations(id) ON DELETE CASCADE,
    action               TEXT NOT NULL CHECK (action IN ('apply', 'rollback', 'forward', 'retry_batch', 'cancel_batch')),
    status               TEXT NOT NULL CHECK (status IN (
                             'draft', 'pending_review', 'approved', 'pending_queue',
                             'running', 'success', 'failed', 'partial', 'partially_reverted'
                         )),
    reverts_execution_id BIGINT,
    sql_snapshot         TEXT NOT NULL,
    execution_mode       TEXT NOT NULL DEFAULT 'standard' CHECK (execution_mode IN ('standard', 'batch')),
    batch_column         TEXT,
    batch_size           INT,
    committed_batches    INT NOT NULL DEFAULT 0,
    committed_rows       BIGINT NOT NULL DEFAULT 0,
    last_cursor          TEXT,
    retry_count          INT NOT NULL DEFAULT 0,
    error_message        TEXT,
    trace_parent         TEXT,
    requested_by         BIGINT NOT NULL REFERENCES users(id),
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    queued_at            TIMESTAMPTZ,
    started_at           TIMESTAMPTZ,
    finished_at          TIMESTAMPTZ,
    duration_ms          INT
);

CREATE INDEX IF NOT EXISTS idx_migration_jobs_db ON migration_jobs(database_id, status);

-- M2: Tabel Persetujuan (Approval) dari setiap Group Leader
CREATE TABLE migration_approvals (
    job_id      BIGINT NOT NULL REFERENCES migration_jobs(id) ON DELETE CASCADE,
    group_id    BIGINT NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    leader_id   BIGINT NOT NULL REFERENCES users(id),
    approved_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (job_id, group_id)
);

-- M2: Tabel Migration History (Append-Only Immutable, HANYA mencatat eksekusi yang berhasil commit)
CREATE TABLE migration_history (
    execution_id          BIGSERIAL PRIMARY KEY,
    database_id           BIGINT NOT NULL REFERENCES database_targets(id) ON DELETE CASCADE,
    migration_id          BIGINT NOT NULL REFERENCES migrations(id),
    job_id                BIGINT REFERENCES migration_jobs(id),
    version               INT NOT NULL,
    revision              INT NOT NULL DEFAULT 1,
    title                 TEXT NOT NULL,
    action                TEXT NOT NULL CHECK (action IN ('apply', 'rollback', 'forward')),
    sql_executed          TEXT NOT NULL,
    executed_by           BIGINT NOT NULL REFERENCES users(id),
    executed_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    duration_ms           INT NOT NULL DEFAULT 0,
    rows_affected         BIGINT NOT NULL DEFAULT 0,
    status                TEXT NOT NULL DEFAULT 'success' CHECK (status = 'success'),
    reverted_execution_id BIGINT REFERENCES migration_history(execution_id)
);

CREATE INDEX IF NOT EXISTS idx_migration_history_db ON migration_history(database_id, execution_id);

CREATE OR REPLACE FUNCTION prevent_history_modification()
RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'migration_history bersifat immutable dan tidak boleh diubah atau dihapus';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_immutable_migration_history
BEFORE UPDATE OR DELETE ON migration_history
FOR EACH ROW EXECUTE FUNCTION prevent_history_modification();

-- M2: Tabel Lock Eksekusi Migration Lintas Worker & Antar-Batch per Database
CREATE TABLE database_migration_locks (
    database_id  BIGINT PRIMARY KEY REFERENCES database_targets(id) ON DELETE CASCADE,
    job_id       BIGINT NOT NULL REFERENCES migration_jobs(id) ON DELETE CASCADE,
    worker_id    TEXT NOT NULL,
    lock_state   TEXT NOT NULL CHECK (lock_state IN ('running', 'holding_partial', 'uncertain')),
    acquired_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    heartbeat_at TIMESTAMPTZ NOT NULL DEFAULT now()
);



