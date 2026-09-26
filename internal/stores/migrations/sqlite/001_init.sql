-- +goose Up
CREATE TABLE IF NOT EXISTS projects (
    id             TEXT PRIMARY KEY,
    name           TEXT NOT NULL UNIQUE,
    repo_url       TEXT NOT NULL,
    provider       TEXT NOT NULL DEFAULT 'generic' CHECK (provider IN ('github', 'gitlab', 'generic')),
    auth_type      TEXT NOT NULL DEFAULT 'none' CHECK (auth_type IN ('none', 'token', 'ssh')),
    auth_user      TEXT NOT NULL DEFAULT '',
    auth_secret    TEXT NOT NULL DEFAULT '',
    webhook_secret TEXT NOT NULL DEFAULT '',
    default_branch TEXT NOT NULL DEFAULT '',
    created_at     INTEGER NOT NULL,
    updated_at     INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS variables (
    project_id TEXT REFERENCES projects(id) ON DELETE CASCADE,
    key        TEXT NOT NULL,
    value      TEXT NOT NULL,
    secret     INTEGER NOT NULL DEFAULT 0
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_variables_global_key ON variables(key) WHERE project_id IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_variables_project_key ON variables(project_id, key) WHERE project_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS executions (
    id                TEXT PRIMARY KEY,
    project_id        TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    workflow          TEXT NOT NULL,
    ref               TEXT NOT NULL DEFAULT '',
    commit_sha        TEXT NOT NULL DEFAULT '',
    status            TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'running', 'success', 'failed', 'canceled')),
    trigger           TEXT NOT NULL DEFAULT 'manual' CHECK (trigger IN ('manual', 'webhook', 'cron', 'rebuild')),
    steps_json        TEXT NOT NULL DEFAULT '[]',
    error             TEXT NOT NULL DEFAULT '',
    started_at        INTEGER,
    finished_at       INTEGER,
    created_at        INTEGER NOT NULL,
    claimed_by        TEXT NOT NULL DEFAULT '',
    cancel_requested  INTEGER NOT NULL DEFAULT 0,
    concurrency_group TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS schedules (
    project_id  TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    workflow    TEXT NOT NULL,
    cron_expr   TEXT NOT NULL,
    next_run_at INTEGER NOT NULL,
    PRIMARY KEY (project_id, workflow)
);

CREATE INDEX IF NOT EXISTS idx_executions_project ON executions(project_id);
CREATE INDEX IF NOT EXISTS idx_executions_status ON executions(status);

-- +goose Down
DROP TABLE IF EXISTS schedules;
DROP TABLE IF EXISTS executions;
DROP TABLE IF EXISTS variables;
DROP TABLE IF EXISTS projects;
