-- +goose NO TRANSACTION
-- +goose Up
ALTER TYPE execution_trigger_enum ADD VALUE IF NOT EXISTS 'retry';
ALTER TABLE executions ADD COLUMN parent_id bigint;
ALTER TABLE executions ADD COLUMN workspace_id bigint;
CREATE INDEX IF NOT EXISTS idx_executions_workspace ON executions(project_id, workspace_id);

-- +goose Down
DROP INDEX IF EXISTS idx_executions_workspace;
ALTER TABLE executions DROP COLUMN workspace_id;
ALTER TABLE executions DROP COLUMN parent_id;
