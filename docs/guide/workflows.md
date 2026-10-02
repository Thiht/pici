# Workflows (.ci)

A workflow is a folder under `.ci/`. One folder = one workflow.

```
.ci/
  build/
    Dockerfile
    ci.yml
    install.sh
    test.py
  deploy/
    Dockerfile
    ci.yml
    deploy.sh
```

## How a run works

1. pici clones the repo (branch, tag, or SHA).
2. It builds the workflow's `Dockerfile` (pre-built and cached by tag).
3. Each step runs as a container from that image, with the repo bind-mounted at `PICI_REPO_DIR`.
4. Variables & secrets are injected as environment variables.

## ci.yml

```yaml
name: build # optional (defaults to folder name)
image: node:20 # optional: use this image instead of the Dockerfile

env: # workflow-level environment variables
  NODE_ENV: test

on: # optional: what triggers the workflow (see "Triggers")
  push:
    branches: [main]
    tags: ["v*.*.*"]
  pull_request:
    branches: [main]
  manual:
    branches: [main] # restrict manual runs to these refs (optional)
  schedule: "0 4 * * *"

paths: # optional: only run when these files change (push and pull_request)
  - src/**
paths_ignore:
  - src/generated/**

concurrency: deploy # optional: cancel running builds in this group when a new one starts

cache: # optional: paths (relative to repo root) persisted between runs
  - node_modules
  - .cache

steps:
  - name: install
    script: install.sh # path relative to .ci/<workflow>/
    timeout: 5m
    retry: 2

  - name: build
    run: npm run build
    depends_on: [install]
    artifacts: # optional: files to collect (glob, relative to repo root)
      - dist/**
    env:
      FOO: bar

  - name: image
    run: docker build -t myapp .
    docker: true # optional: give the step access to the Docker daemon
```

### Steps

- A step must define either `script` or `run`.
- `script` runs via its shebang (`#!...`) if present, else its extension picks the interpreter (`.sh`, `.py`, `.js`, `.rb`, `.go`, ...).
- `run` is executed with `sh -c`.
- `timeout` is per-step (default `PICI_STEP_TIMEOUT`, 30m).
- `retry` is the number of additional attempts after the first failure.
- `depends_on` controls ordering. Independent steps run **in parallel**. A failed step skips its dependents.
- `artifacts` collects matching files after a successful step; download them via the [API](/guide/api#artifacts).
- `docker: true` gives the step access to the same Docker daemon pici uses: it sets `DOCKER_HOST` and, when the daemon is a unix socket, bind-mounts that socket into the step. Images built by a step are visible to the other steps of the run (and to pici's cache). Use it for steps that run `docker build`/`docker push`. With a `tcp://` daemon the step must be able to reach it over the network.

### Cache

`cache` mounts a persistent Docker volume at each path (under `PICI_REPO_DIR`), so dependencies survive between runs. The volume is shared across executions of the same project.

Dependency caches are also **detected automatically** from files at the repo root — no `env`/`cache` needed. They are mounted outside the repo (at `/pici/cache`), so they never show up while scanning the working tree:

| Marker                                | Env vars                | Cached path        |
| ------------------------------------- | ----------------------- | ------------------ |
| `go.mod`                              | `GOMODCACHE`, `GOCACHE` | `gomod`, `gobuild` |
| `package.json`                        | `npm_config_cache`      | `npm`              |
| `Cargo.toml`                          | `CARGO_HOME`            | `cargo`            |
| `requirements.txt` / `pyproject.toml` | `PIP_CACHE_DIR`         | `pip`              |

Explicit `env`/`cache` in `ci.yml` still work and take precedence (or add extra paths).

### Re-running failed steps

When a git run fails, you can re-run **only the steps that failed or were
skipped**, without redoing the successful ones. pici reuses the failed run's
workspace (repo, installed dependencies, build outputs) and records the attempt
as a new execution linked to the original.

From the web UI, use **Retry failed steps** on the failed execution. From the
CLI:

```sh
pici-cli retry demo 42
```

A retry is possible while the workspace still exists — it is kept for `gc-keep`
(default 24h) after the last linked execution finishes. Snapshot (`--local`)
runs cannot be retried.

### Concurrency groups

`concurrency` cancels any currently-running execution in the same group (same project) when a new one starts, so only the latest build of a branch/deployment keeps running.

### Triggers

`on:` declares which events run a workflow. It accepts three forms:

```yaml
on: push # a single event, unrestricted
on: [push, pull_request] # a list of events
on: # a mapping with per-event filters
  push:
    branches: [main]
    tags: ["v*.*.*"]
  pull_request:
    branches: [main]
  schedule: "0 4 * * *"
```

**Default.** When `on:` is absent, the workflow runs on **push to the project's
default branch** and on **pull requests targeting the default branch**. Nothing
else — no tags. Manual runs always work.

**Reset.** As soon as `on:` is present, the default no longer applies: only the
listed events run. `on: {}` (or `on:` empty) runs on nothing automatically —
manual only, e.g. for a workflow triggered from the CLI or the UI.

**Events.**

- `push` — branch pushes. Filters:
  - `branches` (globs) — branch pushes matching the pattern;
  - `tags` (globs) — tag pushes matching the pattern;
  - if only `branches` is set, tag pushes don't run (and vice versa);
  - if both are set, each side is filtered independently;
  - if neither is set (`on: push` or `on: {push: {}}`), every branch and tag push runs.
- `pull_request` — pull/merge requests. `branches` (globs) matches the **target**
  (base) branch, so `pull_request: {branches: [main]}` runs on PRs into `main`
  only. `tags` is not allowed. A pull request opened from a fork never triggers a
  run; the owner starts it by hand (see [Webhooks](/guide/webhooks#pull-requests-from-forks)).
- `manual` — optional filter for manual runs (see below). It accepts
  `branches`/`tags` like `push`, and only a mapping (not a scalar/list).
- `schedule` — a single cron expression, run on the default branch. It is
  independent from `push`/`pull_request` and is never filtered by `paths`.

**Manual runs.** `pici-cli run`, the web UI and `--local` are explicit user
actions and always run the workflow. `on.manual` optionally restricts the refs
they may use, with the same `branches`/`tags` matching as `push`:

```yaml
on:
  schedule: "0 0 * * 0"
  manual:
    branches: [main] # a cron-only workflow runnable by hand on the default branch
```

Without `on.manual`, a manual run accepts any ref. The filter is enforced when
the run starts (git and `--local`), and a run on a disallowed ref fails with a
clear message. Rebuilds reuse the original ref and are not re-validated.

A pull request ref (`refs/pull/<n>/head` on GitHub, `refs/merge-requests/<n>/head`
on GitLab) is fetched explicitly from the project's repository, which is how a
fork pull request is built by hand:

```sh
pici-cli run demo build --ref refs/pull/42/head
```

`on.manual.branches` also restricts those refs, so allow them explicitly (e.g.
`branches: ["refs/pull/*"]`) if the workflow declares a manual filter.

**Paths.** `paths`/`paths_ignore` are global and apply to `push` and
`pull_request` after the ref match. They do not affect `schedule`, and setting
them does not change the implicit default.

**Validation is strict:** an unknown event (`on: merge_request`), an unknown key
under an event, `on.pull_request.tags`, `on: manual` (use `on: {}` for
manual-only), or any of the removed top-level keys (`schedule`, `tags`,
`branches`) is a config error.

> **Migrating from the flat keys.** `schedule`, `tags`, `branches` and
> `triggers` at the top level are replaced by `on:`. A workflow that had no
> filter previously ran on **every** push; it now runs on the default branch
> only — add `on: push` to keep running on all branches.

## Built-in environment variables

| Variable            | Description                                                  |
| ------------------- | ------------------------------------------------------------ |
| `CI`                | always `true`                                                |
| `PICI_PROJECT`      | project name                                                 |
| `PICI_PROJECT_ID`   | project id                                                   |
| `PICI_REPO_URL`     | repo clone URL                                               |
| `PICI_REPO_SLUG`    | repo path (`owner/repo`) without `.git`                      |
| `PICI_WORKFLOW`     | workflow name                                                |
| `PICI_EXECUTION_ID` | execution number (per project)                               |
| `PICI_REF`          | the ref being built                                          |
| `PICI_VERSION`      | tag name when building a tag, otherwise the short commit SHA |
| `PICI_COMMIT_SHA`   | resolved commit SHA                                          |
| `PICI_REPO_DIR`     | mount path of the repo (default `/workspace`)                |
| `PICI_WORKFLOW_DIR` | mount path of the workflow folder                            |

Git is pre-configured to trust the mounted repo (`safe.directory`) and to commit with a default identity (`user.name=pici`, `user.email=pici@localhost`), so `git` commands, commits and Go's VCS stamping work out of the box regardless of uid/gid. Override the identity with `GIT_AUTHOR_NAME`/`GIT_AUTHOR_EMAIL` if needed.

## Validate a ci.yml

```sh
pici-cli validate .ci/build/ci.yml
```
