-- database_targets is managed through the Platform Admin surface (module C).
CREATE TABLE IF NOT EXISTS database_targets (
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

CREATE TABLE IF NOT EXISTS migrations (
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
