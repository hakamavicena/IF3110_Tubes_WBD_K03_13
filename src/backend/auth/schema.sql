CREATE DATABASE milestone1;

CREATE TABLE users (
    id SERIAL PRIMARY KEY,
    username VARCHAR(50) UNIQUE NOT NULL,
    password_hash TEXT NOT NULL
);

CREATE TABLE sessions (
    session_id UUID PRIMARY KEY,
    user_id INT REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMP NOT NULL,
    last_activity TIMESTAMP NOT NULL,
    absolute_expiration TIMESTAMP NOT NULL
);

INSERT INTO users (username, password_hash) 
VALUES ('admin', '$2a$12$m/iwaqxYV/lXODTazaSZP.UN7EISVMnIvnzWnlfB5FMmCxELQGv.q'); 
