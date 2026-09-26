-- +goose Up
ALTER TABLE projects ADD COLUMN execution_seq INTEGER NOT NULL DEFAULT 0;

DROP TABLE executions;
CREATE TABLE executions (
    project_id        TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    id                INTEGER NOT NULL,
    workflow          TEXT NOT NULL,
    ref               TEXT NOT NULL DEFAULT '',
    commit_sha        TEXT NOT NULL DEFAULT '',
    status            TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'running', 'success', 'failed', 'canceled')),
    trigger           TEXT NOT NULL DEFAULT 'manual' CHECK (trigger IN ('manual', 'webhook', 'cron', 'rebuild')),
    steps_json        TEXT NOT NULL DEFAULT '[]',
    error             TEXT NOT NULL DEFAULT '',
    started_at        INTEGER,
    setup_finished_at INTEGER,
    finished_at       INTEGER,
    created_at        INTEGER NOT NULL,
    claimed_by        TEXT NOT NULL DEFAULT '',
    cancel_requested  INTEGER NOT NULL DEFAULT 0,
    concurrency_group TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (project_id, id)
);

CREATE INDEX IF NOT EXISTS idx_executions_status ON executions(status);

-- +goose Down
DROP TABLE executions;
CREATE TABLE executions (
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
    setup_finished_at INTEGER,
    finished_at       INTEGER,
    created_at        INTEGER NOT NULL,
    claimed_by        TEXT NOT NULL DEFAULT '',
    cancel_requested  INTEGER NOT NULL DEFAULT 0,
    concurrency_group TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_executions_project ON executions(project_id);
CREATE INDEX IF NOT EXISTS idx_executions_status ON executions(status);
ALTER TABLE projects DROP COLUMN execution_seq;
