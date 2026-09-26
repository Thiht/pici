-- +goose Up
ALTER TABLE projects ADD COLUMN execution_seq bigint NOT NULL DEFAULT 0;

DROP TABLE executions;
CREATE TABLE executions (
    project_id        uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    id                bigint NOT NULL,
    workflow          text NOT NULL,
    ref               text NOT NULL DEFAULT '',
    commit_sha        text NOT NULL DEFAULT '',
    status            execution_status_enum NOT NULL DEFAULT 'pending',
    trigger           execution_trigger_enum NOT NULL DEFAULT 'manual',
    steps_json        jsonb NOT NULL DEFAULT '[]',
    error             text NOT NULL DEFAULT '',
    started_at        timestamptz,
    setup_finished_at timestamptz,
    finished_at       timestamptz,
    created_at        timestamptz NOT NULL,
    claimed_by        text NOT NULL DEFAULT '',
    cancel_requested  boolean NOT NULL DEFAULT false,
    concurrency_group text NOT NULL DEFAULT '',
    PRIMARY KEY (project_id, id)
);

CREATE INDEX IF NOT EXISTS idx_executions_status ON executions(status);

-- +goose Down
DROP TABLE executions;
CREATE TABLE executions (
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
    setup_finished_at timestamptz,
    finished_at       timestamptz,
    created_at        timestamptz NOT NULL,
    claimed_by        text NOT NULL DEFAULT '',
    cancel_requested  boolean NOT NULL DEFAULT false,
    concurrency_group text NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_executions_project ON executions(project_id);
CREATE INDEX IF NOT EXISTS idx_executions_status ON executions(status);
ALTER TABLE projects DROP COLUMN execution_seq;
