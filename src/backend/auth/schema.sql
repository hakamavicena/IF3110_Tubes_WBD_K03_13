DROP DATABASE IF EXISTS milestone1;
CREATE DATABASE milestone1;
\connect milestone1

DROP TABLE IF EXISTS group_database_access CASCADE;
DROP TABLE IF EXISTS user_groups CASCADE;
DROP TABLE IF EXISTS groups CASCADE;
DROP TABLE IF EXISTS databases CASCADE;
DROP TABLE IF EXISTS sessions CASCADE;
DROP TABLE IF EXISTS users CASCADE;

CREATE TABLE users (
    id SERIAL PRIMARY KEY,
    username VARCHAR(50) UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    is_admin BOOLEAN NOT NULL DEFAULT FALSE
);

CREATE TABLE sessions (
    session_id UUID PRIMARY KEY,
    user_id INT REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMP NOT NULL,
    last_activity TIMESTAMP NOT NULL,
    absolute_expiration TIMESTAMP NOT NULL
);

CREATE TABLE groups (
    id SERIAL PRIMARY KEY,
    name VARCHAR(100) UNIQUE NOT NULL
);

CREATE TABLE user_groups (
    user_id INT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    group_id INT NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, group_id)
);

CREATE TABLE database_targets (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    host TEXT NOT NULL,
    port INT NOT NULL DEFAULT 5432,
    database_name TEXT NOT NULL,
    username TEXT NOT NULL,
    password_enc BYTEA NOT NULL,
    ssl_mode TEXT NOT NULL DEFAULT 'prefer',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE group_database_access (
    group_id INT NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    database_id BIGINT NOT NULL REFERENCES database_targets(id) ON DELETE CASCADE,
    access_level INT NOT NULL CHECK (access_level BETWEEN 0 AND 2),
    PRIMARY KEY (group_id, database_id)
);

INSERT INTO users (username, password_hash, is_admin)
VALUES ('admin', '$2a$12$m/iwaqxYV/lXODTazaSZP.UN7EISVMnIvnzWnlfB5FMmCxELQGv.q', TRUE);
