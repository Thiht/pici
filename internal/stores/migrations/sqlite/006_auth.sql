-- +goose Up
CREATE TABLE IF NOT EXISTS users (
    id               TEXT PRIMARY KEY,
    provider         TEXT NOT NULL,
    provider_user_id TEXT NOT NULL,
    login            TEXT NOT NULL,
    name             TEXT NOT NULL DEFAULT '',
    avatar_url       TEXT NOT NULL DEFAULT '',
    oauth_token      TEXT NOT NULL DEFAULT '',
    created_at       INTEGER NOT NULL,
    last_login_at    INTEGER NOT NULL,
    last_org_check_at INTEGER,
    disabled_at      INTEGER,
    UNIQUE (provider, provider_user_id)
);

CREATE TABLE IF NOT EXISTS sessions (
    id           TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    csrf_token   TEXT NOT NULL,
    created_at   INTEGER NOT NULL,
    last_seen_at INTEGER NOT NULL,
    expires_at   INTEGER NOT NULL,
    user_agent   TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_sessions_expires ON sessions(expires_at);

CREATE TABLE IF NOT EXISTS api_tokens (
    id          TEXT PRIMARY KEY,
    user_id     TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name        TEXT NOT NULL DEFAULT '',
    prefix      TEXT NOT NULL DEFAULT '',
    token_hash  TEXT NOT NULL UNIQUE,
    created_at  INTEGER NOT NULL,
    last_used_at INTEGER,
    expires_at  INTEGER
);

CREATE INDEX IF NOT EXISTS idx_api_tokens_user ON api_tokens(user_id);

ALTER TABLE executions ADD COLUMN created_by TEXT REFERENCES users(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_executions_created_by ON executions(created_by);

-- +goose Down
DROP INDEX IF EXISTS idx_executions_created_by;
ALTER TABLE executions DROP COLUMN created_by;
DROP TABLE IF EXISTS api_tokens;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS users;
