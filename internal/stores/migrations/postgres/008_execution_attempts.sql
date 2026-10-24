-- +goose Up
ALTER TABLE executions ADD COLUMN execution_id bigint;
ALTER TABLE executions ADD COLUMN attempt integer NOT NULL DEFAULT 1;
ALTER TABLE executions ADD COLUMN check_run_id bigint NOT NULL DEFAULT 0;

-- Rebuilds are roots; retries belong to the root of their parent chain.
WITH RECURSIVE chain(project_id, id, root_id) AS (
    SELECT project_id, id, id
    FROM executions
    WHERE trigger != 'retry'
    UNION
    SELECT e.project_id, e.id, c.root_id
    FROM executions e
    JOIN chain c ON c.project_id = e.project_id AND c.id = e.parent_id
    WHERE e.trigger = 'retry'
),
roots AS (
    SELECT DISTINCT ON (project_id, id) project_id, id, root_id
    FROM chain
    ORDER BY project_id, id, root_id
)
UPDATE executions e
SET execution_id = r.root_id
FROM roots r
WHERE r.project_id = e.project_id AND r.id = e.id;

UPDATE executions SET execution_id = id WHERE execution_id IS NULL;

-- Number attempts by row order within each execution. This also handles
-- legacy chains that forked instead of being strictly linear.
WITH numbered AS (
    SELECT project_id, id, ROW_NUMBER() OVER (PARTITION BY project_id, execution_id ORDER BY id) AS attempt
    FROM executions
)
UPDATE executions e
SET attempt = n.attempt
FROM numbered n
WHERE n.project_id = e.project_id AND n.id = e.id;

ALTER TABLE executions ALTER COLUMN execution_id SET NOT NULL;
CREATE UNIQUE INDEX idx_executions_attempt ON executions(project_id, execution_id, attempt);
CREATE INDEX idx_executions_execution ON executions(project_id, execution_id);

-- +goose Down
DROP INDEX IF EXISTS idx_executions_execution;
DROP INDEX IF EXISTS idx_executions_attempt;
ALTER TABLE executions DROP COLUMN check_run_id;
ALTER TABLE executions DROP COLUMN attempt;
ALTER TABLE executions DROP COLUMN execution_id;
