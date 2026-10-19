-- +goose Up
CREATE TABLE IF NOT EXISTS users (
    id                uuid PRIMARY KEY,
    provider          text NOT NULL,
    provider_user_id  text NOT NULL,
    login             text NOT NULL,
    name              text NOT NULL DEFAULT '',
    avatar_url        text NOT NULL DEFAULT '',
    oauth_token       text NOT NULL DEFAULT '',
    created_at        timestamptz NOT NULL,
    last_login_at     timestamptz NOT NULL,
    last_org_check_at timestamptz,
    disabled_at       timestamptz,
    UNIQUE (provider, provider_user_id)
);

CREATE TABLE IF NOT EXISTS sessions (
    id           text PRIMARY KEY,
    user_id      uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    csrf_token   text NOT NULL,
    created_at   timestamptz NOT NULL,
    last_seen_at timestamptz NOT NULL,
    expires_at   timestamptz NOT NULL,
    user_agent   text NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_sessions_expires ON sessions(expires_at);

CREATE TABLE IF NOT EXISTS api_tokens (
    id           uuid PRIMARY KEY,
    user_id      uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name         text NOT NULL DEFAULT '',
    prefix       text NOT NULL DEFAULT '',
    token_hash   text NOT NULL UNIQUE,
    created_at   timestamptz NOT NULL,
    last_used_at timestamptz,
    expires_at   timestamptz
);

CREATE INDEX IF NOT EXISTS idx_api_tokens_user ON api_tokens(user_id);

ALTER TABLE executions ADD COLUMN created_by uuid REFERENCES users(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_executions_created_by ON executions(created_by);

-- +goose Down
DROP INDEX IF EXISTS idx_executions_created_by;
ALTER TABLE executions DROP COLUMN created_by;
DROP TABLE IF EXISTS api_tokens;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS users;
