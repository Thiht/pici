-- +goose Up
CREATE TYPE execution_source_enum AS ENUM ('git', 'snapshot');
ALTER TABLE executions ADD COLUMN source execution_source_enum NOT NULL DEFAULT 'git';
ALTER TABLE executions ADD COLUMN snapshot_id uuid;

-- +goose Down
ALTER TABLE executions DROP COLUMN snapshot_id;
ALTER TABLE executions DROP COLUMN source;
DROP TYPE IF EXISTS execution_source_enum;
