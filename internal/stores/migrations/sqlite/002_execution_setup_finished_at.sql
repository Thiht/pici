-- +goose Up
ALTER TABLE executions ADD COLUMN setup_finished_at INTEGER;

-- +goose Down
ALTER TABLE executions DROP COLUMN setup_finished_at;
