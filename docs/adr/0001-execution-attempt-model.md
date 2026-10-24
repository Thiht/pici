# Execution/attempt model for retries

Pici used to store every retry as a new top-level execution (`trigger=retry`, `parent_id`, shared `workspace_id`), so a build that eventually passed appeared as several unrelated executions and its final state was hard to find. We decided that an Execution is the logical run identified by its first attempt, and that retries add numbered attempts to that Execution. Rebuilds remain separate Executions because they re-materialize the source instead of continuing a run.

Attempts live in the existing `executions` table with a denormalized `execution_id` (the root attempt's id) and a 1-based `attempt`, plus a unique `(project_id, execution_id, attempt)` constraint. The API and UI are execution-scoped (`?attempt=N` selects an attempt), the execution list shows one row per Execution ordered by latest activity, and retry is only allowed on the latest failed attempt. Existing retry chains are backfilled.

Considered options: separate `executions`/`attempts` tables (rejected: heavy migration and API churn for little gain); grouping by the `parent_id` chain at query time (rejected: fragile and expensive); treating rebuild as an attempt (rejected: mixes "continue this run" with "start over").

Consequences: Execution ids are root attempt ids, so ids in the list are sparse; attempt row ids stay internal and are not addressable. GitHub check runs become one per Execution and are updated on each attempt. Attempt-id URLs from before this change are not kept as aliases.
