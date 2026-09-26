-- +goose Up
ALTER TABLE executions ADD COLUMN source TEXT NOT NULL DEFAULT 'git';
ALTER TABLE executions ADD COLUMN snapshot_id TEXT;

-- +goose Down
ALTER TABLE executions DROP COLUMN snapshot_id;
ALTER TABLE executions DROP COLUMN source;
