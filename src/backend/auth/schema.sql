-- Skema DDL Database Internal Platform IF3110 Milestone 1
-- Urutan DDL: users, sessions, groups, user_groups, database_targets, group_database_access, migrations

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
    is_admin      BOOLEAN NOT NULL DEFAULT FALSE
);

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
    id   BIGSERIAL PRIMARY KEY,
    name VARCHAR(100) UNIQUE NOT NULL
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

CREATE TABLE migrations (
    id            BIGSERIAL PRIMARY KEY,
    database_id   BIGINT NOT NULL REFERENCES database_targets(id) ON DELETE CASCADE,
    version       INT NOT NULL,
    title         TEXT NOT NULL,
    sql           TEXT NOT NULL,
    status        TEXT NOT NULL CHECK (status IN ('draft', 'running', 'success', 'failed')),
    error_message TEXT,
    created_by    BIGINT NOT NULL REFERENCES users(id),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    executed_by   BIGINT REFERENCES users(id),
    executed_at   TIMESTAMPTZ,
    duration_ms   INT,
    UNIQUE (database_id, version)
);

CREATE INDEX IF NOT EXISTS idx_migrations_db ON migrations(database_id, version);

-- Seed awal akun admin
INSERT INTO users (username, password_hash, is_admin)
VALUES ('admin', '$2a$12$m/iwaqxYV/lXODTazaSZP.UN7EISVMnIvnzWnlfB5FMmCxELQGv.q', TRUE)
ON CONFLICT (username) DO NOTHING;
