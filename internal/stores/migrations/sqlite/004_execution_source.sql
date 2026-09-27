-- +goose Up
ALTER TABLE executions ADD COLUMN source TEXT NOT NULL DEFAULT 'git' CHECK (source IN ('git', 'snapshot'));
ALTER TABLE executions ADD COLUMN snapshot_id TEXT;

-- +goose Down
ALTER TABLE executions DROP COLUMN snapshot_id;
ALTER TABLE executions DROP COLUMN source;
