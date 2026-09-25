CREATE TABLE IF NOT EXISTS projects (
    id             TEXT PRIMARY KEY,
    name           TEXT NOT NULL UNIQUE,
    repo_url       TEXT NOT NULL,
    provider       TEXT NOT NULL DEFAULT 'generic',
    auth_type      TEXT NOT NULL DEFAULT 'none',
    auth_user      TEXT NOT NULL DEFAULT '',
    auth_secret    TEXT NOT NULL DEFAULT '',
    webhook_secret TEXT NOT NULL DEFAULT '',
    default_branch TEXT NOT NULL DEFAULT '',
    created_at     BIGINT NOT NULL,
    updated_at     BIGINT NOT NULL
);

CREATE TABLE IF NOT EXISTS variables (
    project_id TEXT NOT NULL DEFAULT '',
    key        TEXT NOT NULL,
    value      TEXT NOT NULL,
    secret     BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (project_id, key)
);

CREATE TABLE IF NOT EXISTS executions (
    id               TEXT PRIMARY KEY,
    project_id       TEXT NOT NULL,
    workflow         TEXT NOT NULL,
    ref              TEXT NOT NULL DEFAULT '',
    commit_sha       TEXT NOT NULL DEFAULT '',
    status           TEXT NOT NULL DEFAULT 'pending',
    trigger          TEXT NOT NULL DEFAULT 'manual',
    steps_json       TEXT NOT NULL DEFAULT '',
    error            TEXT NOT NULL DEFAULT '',
    started_at       BIGINT NOT NULL DEFAULT 0,
    finished_at      BIGINT NOT NULL DEFAULT 0,
    created_at       BIGINT NOT NULL,
    claimed_by       TEXT NOT NULL DEFAULT '',
    cancel_requested BIGINT NOT NULL DEFAULT 0,
    concurrency_group TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS schedules (
    project_id  TEXT NOT NULL,
    workflow    TEXT NOT NULL,
    cron_expr   TEXT NOT NULL,
    next_run_at BIGINT NOT NULL,
    PRIMARY KEY (project_id, workflow)
);

CREATE INDEX IF NOT EXISTS idx_executions_project ON executions(project_id);
CREATE INDEX IF NOT EXISTS idx_executions_status ON executions(status);
CREATE INDEX IF NOT EXISTS idx_variables_project ON variables(project_id);
