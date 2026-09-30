-- +goose Up
CREATE TABLE executions_new (
    project_id        TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    id                INTEGER NOT NULL,
    workflow          TEXT NOT NULL,
    ref               TEXT NOT NULL DEFAULT '',
    commit_sha        TEXT NOT NULL DEFAULT '',
    status            TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'running', 'success', 'failed', 'canceled')),
    trigger           TEXT NOT NULL DEFAULT 'manual' CHECK (trigger IN ('manual', 'webhook', 'cron', 'rebuild', 'retry')),
    steps_json        TEXT NOT NULL DEFAULT '[]',
    error             TEXT NOT NULL DEFAULT '',
    started_at        INTEGER,
    setup_finished_at INTEGER,
    finished_at       INTEGER,
    created_at        INTEGER NOT NULL,
    claimed_by        TEXT NOT NULL DEFAULT '',
    cancel_requested  INTEGER NOT NULL DEFAULT 0,
    concurrency_group TEXT NOT NULL DEFAULT '',
    source            TEXT NOT NULL DEFAULT 'git' CHECK (source IN ('git', 'snapshot')),
    snapshot_id       TEXT,
    parent_id         INTEGER,
    workspace_id      INTEGER,
    PRIMARY KEY (project_id, id)
);

INSERT INTO executions_new (project_id, id, workflow, ref, commit_sha, status, trigger, steps_json, error, started_at, setup_finished_at, finished_at, created_at, claimed_by, cancel_requested, concurrency_group, source, snapshot_id)
SELECT project_id, id, workflow, ref, commit_sha, status, trigger, steps_json, error, started_at, setup_finished_at, finished_at, created_at, claimed_by, cancel_requested, concurrency_group, source, snapshot_id
FROM executions;

DROP TABLE executions;
ALTER TABLE executions_new RENAME TO executions;

CREATE INDEX IF NOT EXISTS idx_executions_status ON executions(status);
CREATE INDEX IF NOT EXISTS idx_executions_workspace ON executions(project_id, workspace_id);

-- +goose Down
CREATE TABLE executions_old (
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
    source            TEXT NOT NULL DEFAULT 'git' CHECK (source IN ('git', 'snapshot')),
    snapshot_id       TEXT,
    PRIMARY KEY (project_id, id)
);

INSERT INTO executions_old (project_id, id, workflow, ref, commit_sha, status, trigger, steps_json, error, started_at, setup_finished_at, finished_at, created_at, claimed_by, cancel_requested, concurrency_group, source, snapshot_id)
SELECT project_id, id, workflow, ref, commit_sha, status, trigger, steps_json, error, started_at, setup_finished_at, finished_at, created_at, claimed_by, cancel_requested, concurrency_group, source, snapshot_id
FROM executions;

DROP TABLE executions;
ALTER TABLE executions_old RENAME TO executions;

CREATE INDEX IF NOT EXISTS idx_executions_status ON executions(status);
