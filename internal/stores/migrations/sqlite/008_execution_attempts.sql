-- +goose Up
ALTER TABLE executions ADD COLUMN execution_id INTEGER;
ALTER TABLE executions ADD COLUMN attempt INTEGER NOT NULL DEFAULT 1;
ALTER TABLE executions ADD COLUMN check_run_id INTEGER NOT NULL DEFAULT 0;

-- Rebuilds are roots; retries belong to the root of their parent chain. The
-- recursive CTE walks the parent links, and INSERT OR IGNORE keeps one root
-- per row even if legacy data contains a fork.
CREATE TEMP TABLE execution_roots (
    project_id TEXT NOT NULL,
    id         INTEGER NOT NULL,
    root_id    INTEGER NOT NULL,
    PRIMARY KEY (project_id, id)
);

INSERT OR IGNORE INTO execution_roots (project_id, id, root_id)
WITH RECURSIVE chain(project_id, id, root_id) AS (
    SELECT project_id, id, id
    FROM executions
    WHERE trigger != 'retry'
    UNION
    SELECT e.project_id, e.id, c.root_id
    FROM executions e
    JOIN chain c ON c.project_id = e.project_id AND c.id = e.parent_id
    WHERE e.trigger = 'retry'
)
SELECT project_id, id, root_id FROM chain;

UPDATE executions
SET execution_id = (
    SELECT r.root_id FROM execution_roots r
    WHERE r.project_id = executions.project_id AND r.id = executions.id
)
WHERE EXISTS (
    SELECT 1 FROM execution_roots r
    WHERE r.project_id = executions.project_id AND r.id = executions.id
);

UPDATE executions SET execution_id = id WHERE execution_id IS NULL;

-- Number attempts by row order within each execution. This also handles
-- legacy chains that forked instead of being strictly linear.
CREATE TEMP TABLE attempt_numbers AS
SELECT project_id, id, ROW_NUMBER() OVER (PARTITION BY project_id, execution_id ORDER BY id) AS attempt
FROM executions;

UPDATE executions
SET attempt = (
    SELECT a.attempt FROM attempt_numbers a
    WHERE a.project_id = executions.project_id AND a.id = executions.id
);

DROP TABLE execution_roots;
DROP TABLE attempt_numbers;

CREATE UNIQUE INDEX IF NOT EXISTS idx_executions_attempt ON executions(project_id, execution_id, attempt);
CREATE INDEX IF NOT EXISTS idx_executions_execution ON executions(project_id, execution_id);

-- +goose Down
DROP INDEX IF EXISTS idx_executions_execution;
DROP INDEX IF EXISTS idx_executions_attempt;
ALTER TABLE executions DROP COLUMN check_run_id;
ALTER TABLE executions DROP COLUMN attempt;
ALTER TABLE executions DROP COLUMN execution_id;
