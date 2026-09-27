# Unified Workflow Triggers (`on:`) — Design

## Context

A workflow's trigger configuration is currently spread across four flat keys:
`schedule` (cron), `tags`, `branches` (ref filters) and `paths`/`paths_ignore`
(file filters), plus the recently added `triggers` list. Their interaction is
implicit and surprising:

- **No filters means "everything".** A workflow with only a `schedule` is
  triggered by every push (observed: the `deps` workflow ran on push).
- **No filter can express "on this branch".** `continuous-integration` ran on a
  push to a non-default branch.
- **Pull requests ignore the target branch.** Matching uses the PR ref
  (`refs/pull/N/head`), so `branches` cannot express "PRs into `main`".
- The defaults are per-key and not composable.

This spec replaces the flat keys with a single `on:` block, GitHub
Actions-shaped, with sane defaults and a clear reset rule. Decisions validated
during brainstorming are listed below.

## Goals

- One place to declare what triggers a workflow: events and their ref filters.
- A sane implicit default: **push on the project's default branch** and
  **pull requests targeting the default branch**.
- An explicit rule: as soon as `on:` is present, no default applies — only the
  listed events run.
- Pull requests can be filtered by **target (base)** branch.
- File filters (`paths`/`paths_ignore`) stay orthogonal (global) and do not
  participate in the trigger reset.
- Strict validation: a typo fails the config instead of silently disabling a
  workflow.
- A migration path with a clear error for the removed keys.

## Non-Goals

- Multiple cron expressions per workflow (single expression only).
- Per-event path filters (paths stay global).
- Filtering PRs by source branch.
- Filtering by PR action/type (still `opened`/`synchronize`/`reopened`).
- Making manual runs or rebuilds (they are always allowed) filterable.

## Decisions validated during brainstorming

- **Shape.** A single `on:` block (GitHub Actions-like), with shorthand forms.
- **Backward compatibility.** Hard cut: legacy keys are a config error with a
  migration hint to `on:`.
- **Implicit default.** Push on the default branch + pull requests targeting the
  default branch. No tags, no path restriction beyond the global `paths`.
- **Manual.** Always allowed and implicit, never listed (`on: manual` is an
  error).
- **Default branch.** `project.default_branch` is always populated
  (auto-detected on project create/update via API and CLI, like the web form);
  resolve the remote HEAD as a defensive fallback when empty.
- **Paths.** Global (`paths`, `paths_ignore`), applied to `push` and
  `pull_request`, never to the schedule. Presence of `paths` does not reset the
  implicit default.
- **Explicit event without filters.** Unrestricted (all branches and tags for
  `push`; all base branches for `pull_request`).
- **Validation.** Strict: reject unknown events, unknown keys, invalid types,
  legacy root keys, `on.pull_request.tags`, `on: manual`, `on.push.paths`.
- **Ref matrix.** Keep the current semantics for `branches`/`tags` under
  `push`.
- **Schedule.** A single cron expression, run on the default branch,
  independent from push/PR and unfiltered by `paths`.
- **Empty filter lists.** `branches: []` / `tags: []` are treated as "not set"
  (no filter), matching the current matcher.
- **Default-branch comparison.** Exact equality (not a glob).
- **Scope.** Migrate the three `.ci/` workflows, update the docs, and remove
  the `triggers` field in the same change.

## Schema

Root of `ci.yml` (unchanged keys omitted):

```yaml
name: build
image: node:20
env: { ... }
paths: [src/**] # global, applies to push and pull_request
paths_ignore: [src/generated/**]
cache: [node_modules]
concurrency: deploy
on: ... # new
steps: [...]
```

`on` accepts four forms:

```yaml
on: push # scalar: a single event, unrestricted
on: [push, pull_request] # list of events
on: {} # empty: no automatic trigger (manual only)
on: # mapping
  push:
    branches: [main]
    tags: ["v*.*.*"]
  pull_request:
    branches: [main]
  schedule: "0 0 * * 0"
```

Grammar:

- **Events:** `push`, `pull_request`, `schedule`.
- **`push`:** `null`/empty (unrestricted) or `{ branches?: [glob], tags?: [glob] }`.
- **`pull_request`:** `null`/empty (unrestricted) or `{ branches?: [glob] }`.
  `tags` is rejected.
- **`schedule`:** a single cron string. `null` is rejected (an expression is
  required).
- The scalar/list shorthand may only contain `push` and `pull_request`
  (`schedule` needs an expression).

## Matching semantics

Let `D` be `project.default_branch`.

| Situation                       | Result                                                  |
| ------------------------------- | ------------------------------------------------------- |
| `on:` absent                    | `push` on `D`, `pull_request` into `D`                  |
| `on: {push: {}}` / `on: push`   | every branch push and every tag push                    |
| `on.push.branches: [g]`         | branch pushes matching `g`; no tag pushes               |
| `on.push.tags: [g]`             | tag pushes matching `g`; no branch pushes               |
| `on.push` with both             | branch pushes match `branches`; tag pushes match `tags` |
| `on: {pull_request: {}}`        | PRs into any base branch                                |
| `on.pull_request.branches: [g]` | PRs whose target (base) branch matches `g`              |
| `on: {schedule: "c"}`           | cron `c` on `D`, nothing on push/PR                     |

- **Reset.** Any present `on:` disables the implicit default entirely. Only the
  listed events run.
- **Global `paths`.** Applied to `push` and `pull_request` after the ref match.
  Never applied to `schedule` (no changed files).
- **Manual and rebuild.** Always allowed, regardless of `on`.
- **Glob syntax.** `doublestar` (as today) for `branches`, `tags`, `paths`.

## Validation rules

`Parse` returns an error (with a migration hint where relevant) for:

- a removed root key (`schedule`, `tags`, `branches`, `triggers`) — message
  pointing to `on:`;
- an unknown event name under `on` (e.g. `merge_request`);
- an unknown key under an event: `on.push.foo`, or `paths`/`paths_ignore` under
  an event (they are global);
- an invalid type (e.g. `branches: main` instead of a list);
- `on.pull_request.tags`;
- `on: manual`;
- `on: schedule` (scalar) or `schedule: null` (no expression).

Unknown-key detection uses `yaml.UnmarshalWithOptions(..., yaml.Strict())`;
`on` parsing uses a custom unmarshaler for the scalar/list/mapping forms.

## Default branch

- `project.default_branch` is auto-detected on project create/update via the
  API and CLI when empty (same `git.ListRefs` HEAD detection as the web form).
- If it is still empty at trigger/schedule time (legacy projects), resolve the
  remote HEAD as a fallback.
- The implicit default and the schedule compare the pushed ref / PR base to the
  default branch by **exact equality**.

## Migration

- `.ci/release/ci.yml` → `on: { push: { tags: ["v*.*.*"] } }`
- `.ci/continuous-integration/ci.yml` →
  `on: { push: { branches: [main] }, pull_request: { branches: [main] } }`
- `.ci/deps/ci.yml` → `on: { schedule: "0 0 * * 0" }`
- `docs/guide/workflows.md` rewritten for `on:`; `README.md` example updated if
  needed.
- Remove the `triggers` field added earlier.

**Behavior change.** A workflow with no `on:` previously ran on every push. It
now runs on the default branch only (plus PRs into it). External users must add
`on: push` to keep running on all branches; the migration error surfaces this.

## Architecture

- `internal/ci/config.go`: new `On` type (custom `UnmarshalYAML`), keep global
  `Paths`/`PathsIgnore`, drop `Triggers`/`Schedule`/`Tags`/`Branches`; strict
  decoding; validation.
- `internal/ci/paths.go`: event/filters matching (`MatchesPush`,
  `MatchesPullRequest`, default resolution); `MatchesPaths` unchanged.
- `internal/ci/schedule.go`: schedule derived from `On.Schedule`.
- `internal/handlers/webhooks.go`: resolve the `on` events, pass the PR base
  branch, use the project default branch for the implicit default.
- `internal/scheduler/scheduler.go`: unchanged (one cron per workflow, default
  branch).
- `internal/handlers/projects.go`, `internal/web/projects.go`,
  `cmd/pici-cli/main.go`: auto-detect `default_branch` when empty.
- Docs and `.ci/` workflows.

## Testing

- **Parsing:** every `on` form; strict errors (unknown event/key, invalid type,
  `on.pull_request.tags`, `on: manual`, scalar `schedule`); legacy-key migration
  error.
- **Matching:** implicit default vs explicit; unrestricted explicit event;
  branches/tags matrix; PR base filter; reset (`on.schedule` disables push/PR);
  empty-list = unset.
- **Webhooks:** push on/off the default branch; PR into/outside the default
  branch; explicit `on: push` still fires for any branch.
- **Scheduler:** `on.schedule` registers one schedule; a workflow without
  `on.schedule` registers none.
- **Default branch:** auto-detection on project create/update when empty.

## Addendum: manual ref filter

Manual runs are always allowed, but `on.manual` restricts the refs they may use:

```yaml
on:
  schedule: "0 0 * * 0"
  manual:
    branches: [main]
```

- Same `branches`/`tags` shape and matching as `push`; `manual` accepts only a
  mapping (`on: manual` / `on: [manual]` are errors; use `on: {}` for
  manual-only).
- Absent or empty `on.manual` → unrestricted (backward compatible).
- Enforced in the runner once the config is parsed, so it covers git and
  `--local` snapshots; a disallowed ref fails the execution with a clear
  message. `isTag` comes from the clone (`git.IsTag`), `false` for snapshots.
- Rebuilds reuse the original ref and are not re-validated.
