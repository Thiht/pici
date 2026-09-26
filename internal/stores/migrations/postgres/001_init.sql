-- +goose Up
CREATE TYPE provider_enum AS ENUM ('github', 'gitlab', 'generic');
CREATE TYPE auth_type_enum AS ENUM ('none', 'token', 'ssh');
CREATE TYPE execution_status_enum AS ENUM ('pending', 'running', 'success', 'failed', 'canceled');
CREATE TYPE execution_trigger_enum AS ENUM ('manual', 'webhook', 'cron', 'rebuild');

CREATE TABLE IF NOT EXISTS projects (
    id             uuid PRIMARY KEY,
    name           text NOT NULL UNIQUE,
    repo_url       text NOT NULL,
    provider       provider_enum NOT NULL DEFAULT 'generic',
    auth_type      auth_type_enum NOT NULL DEFAULT 'none',
    auth_user      text NOT NULL DEFAULT '',
    auth_secret    text NOT NULL DEFAULT '',
    webhook_secret text NOT NULL DEFAULT '',
    default_branch text NOT NULL DEFAULT '',
    created_at     timestamptz NOT NULL,
    updated_at     timestamptz NOT NULL
);

CREATE TABLE IF NOT EXISTS variables (
    project_id uuid REFERENCES projects(id) ON DELETE CASCADE,
    key        text NOT NULL,
    value      text NOT NULL,
    secret     boolean NOT NULL DEFAULT false
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_variables_global_key ON variables(key) WHERE project_id IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_variables_project_key ON variables(project_id, key) WHERE project_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS executions (
    id                uuid PRIMARY KEY,
    project_id        uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    workflow          text NOT NULL,
    ref               text NOT NULL DEFAULT '',
    commit_sha        text NOT NULL DEFAULT '',
    status            execution_status_enum NOT NULL DEFAULT 'pending',
    trigger           execution_trigger_enum NOT NULL DEFAULT 'manual',
    steps_json        jsonb NOT NULL DEFAULT '[]',
    error             text NOT NULL DEFAULT '',
    started_at        timestamptz,
    finished_at       timestamptz,
    created_at        timestamptz NOT NULL,
    claimed_by        text NOT NULL DEFAULT '',
    cancel_requested  boolean NOT NULL DEFAULT false,
    concurrency_group text NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS schedules (
    project_id  uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    workflow    text NOT NULL,
    cron_expr   text NOT NULL,
    next_run_at timestamptz NOT NULL,
    PRIMARY KEY (project_id, workflow)
);

CREATE INDEX IF NOT EXISTS idx_executions_project ON executions(project_id);
CREATE INDEX IF NOT EXISTS idx_executions_status ON executions(status);

-- +goose Down
DROP TABLE IF EXISTS schedules;
DROP TABLE IF EXISTS executions;
DROP TABLE IF EXISTS variables;
DROP TABLE IF EXISTS projects;
DROP TYPE IF EXISTS execution_trigger_enum;
DROP TYPE IF EXISTS execution_status_enum;
DROP TYPE IF EXISTS auth_type_enum;
DROP TYPE IF EXISTS provider_enum;
