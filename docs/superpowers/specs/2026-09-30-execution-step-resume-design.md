# Partial Re-run of Failed Steps (`resume`) — Design

## Context

When a workflow fails, the only recovery today is `rebuild`: a fresh clone that
re-runs every step from scratch (`internal/ci/runner.go`, `TriggerRebuild`).
That is wasteful when a late step (e.g. `lint`) fails after expensive earlier
steps (install, build) succeeded, and it discards the state those steps
produced.

Per-step `retry:` already exists (`steps[].retry`, `internal/ci/step.go`,
`attempts := step.Retry + 1`) and handles transient, in-run flakiness. This spec
adds the missing **manual, post-failure** recovery: re-run only the failed steps
of a finished run, reusing that run's workspace.

The feature is modeled as a new execution derived from the failed one, so
history, logs and status reporting follow the existing execution pipeline.

## Goals

- After a failed run, re-run **only the steps that failed or were skipped**
  because of a failure, in **the same workspace** (repo worktree, installed
  dependencies, build outputs preserved).
- Do **not** re-clone, do **not** rebuild the runner image from scratch (Docker
  cache), do **not** re-run successful steps.
- Record the re-run as a **new execution linked to its parent**, with its own
  id, logs and status.
- Keep the workspace alive long enough to be re-runnable, and fail clearly once
  it is gone.
- Expose the action on the API, the CLI and the web UI.

## Non-Goals

- Re-running snapshots (`pici-cli run --local`); git executions only.
- Automatic retry of a workflow/job (job-level `retry:`); the per-step `retry:`
  already covers the flaky-step case.
- Re-running a single selected step from the UI (all failed/skipped steps are
  re-run together).
- Multi-node workspaces (a resume needs the same workspace directory, so it
  stays single-node / shared-volume, like snapshot uploads today).
- Resuming an execution whose failure happened during setup (clone, `ci.yml`,
  image build) with no step-level failure.

## Decisions validated during brainstorming

- **State.** Reuse the failed run's workspace in place; do not re-clone, do not
  copy.
- **Representation.** A new execution linked to the parent. The original failed
  execution is untouched.
- **Scope.** Git executions only. Snapshots are rejected (like `rebuild`).
- **GC.** In-place reuse, protected by a GC guard: a workspace is not deleted
  while a linked execution is non-terminal or finished within `gc-keep`.
- **Job-level retry.** Not added; per-step `retry:` already exists.
- **Lineage.** `ParentID` is a pure lineage link. Both `rebuild` and `resume`
  set it; only `resume` also reuses the workspace. `Trigger` distinguishes the
  two.
- **Check run / status.** `resume` reuses the `rebuild` behavior (creates a new
  GitHub check run / posts a GitLab status for the new execution).

## Data model

`stores.Execution` gains two nullable fields:

- `ParentID *int64` (`json:"parent_id,omitempty"`) — the execution this one was
  derived from (`rebuild` or `resume`).
- `WorkspaceID *int64` (`json:"-"`) — the execution that owns the workspace
  directory this execution runs in. `nil` means "its own id". Set for every
  execution at creation (self, unless resuming).

`Trigger` gains `resume`: `manual, webhook, cron, rebuild, resume`.

### Schema / migrations

SQLite (`005_execution_resume.sql`), following the table-rebuild pattern already
used by `003_execution_number.sql`:

- Recreate `executions` with `trigger CHECK (..., 'resume')` and new columns
  `parent_id INTEGER`, `workspace_id INTEGER`.
- `CREATE INDEX idx_executions_workspace ON executions(project_id, workspace_id);`

Postgres (`005_execution_resume.sql`):

- `ALTER TYPE execution_trigger_enum ADD VALUE IF NOT EXISTS 'resume';`
- `ALTER TABLE executions ADD COLUMN parent_id bigint;`
- `ALTER TABLE executions ADD COLUMN workspace_id bigint;`
- `CREATE INDEX idx_executions_workspace ON executions(project_id, workspace_id);`

`internal/stores/enums.go` enum list updated; regenerate `enums_enum.go` with
`go generate ./internal/stores`.

### Store changes

- `CreateExecution`: if `WorkspaceID == nil`, set it to the execution's own id
  (both stores already assign `e.ID` before the `INSERT`); persist `parent_id`
  and `workspace_id`.
- `GetExecution` / `ListExecutions`: scan the two new columns (both `*int64`).
- New `WorkspaceBusy(ctx, projectID, workspaceID) (bool, error)`: true if any
  execution with that `workspace_id` is non-terminal (`pending`/`running`).
- New `WorkspaceRetained(ctx, projectID, workspaceID, cutoff) (bool, error)`,
  used by GC: true if any execution with that `workspace_id` is non-terminal or
  has `finished_at >= cutoff`.

Both methods apply to SQLite and Postgres.

## Eligibility

`EnqueueResume(ctx, project, previous)` is rejected with a typed error when:

- `previous.Source != SourceGit` → snapshots cannot be resumed;
- `previous.Status != StatusFailed` → only failed runs;
- `previous.Steps` has no `failed` or `skipped` step → setup-only failures have
  nothing to re-run;
- the workspace directory no longer exists → "workspace no longer available";
- `WorkspaceBusy(...)` is true → another attempt is already running.

## Runner

`run()` computes `repoDir` from `WorkspaceID` instead of `exec.ID`:

```go
workspaceID := exec.ID
if exec.WorkspaceID != nil { workspaceID = *exec.WorkspaceID }
repoDir := filepath.Join(r.WorkspaceDir, project.ID.String(), strconv.FormatInt(workspaceID, 10))
```

For a resume (`exec.Trigger == TriggerResume`, equivalently `ParentID != nil`):

1. **Skip `materializeSource`** — the worktree already exists at `repoDir`.
2. Read `.ci/<workflow>/ci.yml` from `repoDir` as usual.
3. Build the runner image from `repoDir/.ci/<workflow>` as usual (Docker cache
   makes it cheap; guarantees the image is present even if GC pruned it).
4. Load the parent execution and compute the **carry-over set**: every step
   whose parent result is `success`. Carried steps are reported with their
   parent's result (status, timestamps, exit code, env).
5. Persist the planned step list (full list; carried steps pre-filled) so the UI
   can render immediately.
6. Run `executeSteps` with a `carryOver map[string]stores.StepResult`:
   - carried steps are never launched; they count as `success` for dependency
     resolution, so a re-run step whose deps were all successes starts
     immediately;
   - every non-carried step (failed, skipped, or otherwise unfinished) is
     executed normally.
7. For each carried step, to keep the child self-contained:
   - copy the parent's per-step log file into the child's log directory
     (match by step name, using the parent's step index);
   - re-run `collectArtifacts` for the step against the reused `repoDir`.

Everything else (env, caches, concurrency group, check run, GitLab status,
SSE logs, cancellation, finish) flows through the existing path. The manual-ref
validation (`cfg.MatchesManual`) only applies to `TriggerManual`, so it is
skipped for resume; `SyncSchedule` remains idempotent.

### `executeSteps` change

Add a `carryOver map[string]stores.StepResult` parameter. `progress[i]` and
`results[i]`/`statuses[i]` are pre-filled for carried steps (`success`), and
those steps are excluded from the initial launch set. The existing
dependency-ready logic already treats non-empty statuses as satisfied, so no
further change is needed.

## GC

`cleanupWorkspaces` keeps a directory `<ownerID>` when the workspace is still
retained. Replace the current per-directory `expired(projectID, id, cutoff)`
with a workspace-aware check:

- delete `<ownerID>` only when **no** execution with
  `workspace_id = ownerID` is non-terminal or finished within `Keep`
  (`WorkspaceRetained` returns false);
- the workspace-owner execution itself always has `workspace_id = ownerID`, so
  the existing behavior (delete once the run is old enough) is preserved for
  non-resumed workspaces.

`cleanupLogs` is unchanged: each attempt (including resumed children) owns its
own log directory keyed by its execution id.

## API / CLI / UI

- **API**: `POST /api/projects/{id}/executions/{executionID}/resume` →
  `202` with the new execution. `409` when ineligible (not failed, snapshot,
  busy), `410` when the workspace is gone. Mirrors `Rebuild`
  (`internal/handlers/executions.go`).
- **CLI**: `pici-cli resume <project> <execution-id>` (client method
  `ResumeExecution`), parallel to `pici-cli rebuild`.
- **Web**: a `Retry failed steps` button on the execution page, shown only when
  `status == failed && source == git`; `POST /projects/{id}/executions/{executionID}/resume`.
  History shows lineage: "rebuild of #n" / "retry of #n" for children, and links
  to children from the parent.
- **`rebuild`** now sets `ParentID = previous.ID` (but not `WorkspaceID`).

## Error handling

- Rejected eligibility returns a specific message the API maps to 409/410 and
  the UI surfaces as a flash error.
- If the workspace disappears between enqueue and execution (GC race), the
  runner fails the resume execution during setup with a clear message, exactly
  like a failed clone does today.

## Testing

- **Runner**: resume reuses the parent workspace; only failed/skipped steps
  re-run; successful steps are carried and their side effects (files written by
  the step) are present; carried steps' logs/artifacts are exposed on the child.
- **Eligibility**: snapshot rejected; non-failed rejected; setup-only failure
  rejected; missing workspace rejected; busy workspace rejected; nominal creates
  a `TriggerResume` execution with `ParentID` and `WorkspaceID` set.
- **GC**: a workspace referenced by a recent/non-terminal child is not deleted;
  it is deleted once every referencing execution is expired.
- **Store**: `ParentID`/`WorkspaceID` round-trip; `WorkspaceBusy` /
  `WorkspaceRetained`; `rebuild` now stores `ParentID`.
- **Web/CLI**: button visibility, endpoint wiring, command parsing.

## Docs

- `docs/guide/workflows.md`: a "Re-running failed steps" section.
- `docs/guide/api.md` and `docs/guide/cli.md`: the new endpoint / command.
- `README.md`: mention the feature in the API/UI summary.
