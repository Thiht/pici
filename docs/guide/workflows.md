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

Each workflow has a `Dockerfile` (the runner image) and a `ci.yml` (the steps). Files used by steps live in the same folder.

## How a run works

1. pici clones the repository (branch, tag, or SHA).
2. It builds the workflow's `Dockerfile`, cached by tag.
3. Each step runs as a container from that image, with the repo bind-mounted at `PICI_REPO_DIR`.
4. Variables and secrets are injected as environment variables.

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

concurrency: deploy # optional: cancel running executions in this group when a new one starts

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

## Steps

- A step must define either `script` or `run`.
- `script` runs via its shebang (`#!...`) if present, otherwise its extension picks the interpreter (`.sh`, `.py`, `.js`, `.rb`, `.go`, ...).
- `run` is executed with `sh -c`.
- `timeout` is per step; the default comes from `PICI_STEP_TIMEOUT` (30m).
- `retry` is the number of additional attempts after the first failure.
- `depends_on` controls ordering. Independent steps run **in parallel**; a failed step skips its dependents.
- `artifacts` collects matching files after a successful step; download them via the [API](/guide/api#artifacts).
- `docker: true` gives the step access to the Docker daemon pici uses: it sets `DOCKER_HOST` and, for a Unix socket, bind-mounts it into the step. Images built by a step are visible to the other steps of the run. With a `tcp://` daemon, the step must reach it over the network.

## Triggers

`on:` declares which events run a workflow. It accepts a single event, a list, or a mapping with per-event filters:

```yaml
on: push # a single event, unrestricted
on: [push, pull_request] # a list of events
on: # a mapping with filters
  push:
    branches: [main]
    tags: ["v*.*.*"]
  pull_request:
    branches: [main]
  schedule: "0 4 * * *"
```

| Event          | Runs on                          | Filter                                              |
| -------------- | -------------------------------- | --------------------------------------------------- |
| `push`         | branch and tag pushes            | `branches` / `tags` globs                           |
| `pull_request` | pull/merge requests              | `branches` matches the **target** branch; no `tags` |
| `manual`       | explicit runs from the CLI or UI | `branches` / `tags` restrict the allowed refs       |
| `schedule`     | one cron expression              | on the default branch                               |

**Default.** Without `on:`, a workflow runs on **push to the default branch** and on **pull requests targeting it**. Nothing else — no tags. Manual runs always work.

**When `on:` is present**, only the listed events run; it replaces the default entirely. `on: {}` (or an empty `on:`) runs on nothing automatically: manual only.

**Event details.**

- `push`: if only `branches` is set, tag pushes don't run (and vice versa). If both are set, each is filtered independently. If neither is set (e.g. `on: push`), every branch and tag push runs.
- `pull_request`: `branches` matches the base branch, so `pull_request: {branches: [main]}` runs on PRs into `main` only. A pull request opened from a fork never triggers a run; the owner starts it by hand (see [Webhooks](/guide/webhooks#pull-requests-from-forks)).
- `manual`: `pici-cli run`, the web UI, and `--local` are explicit user actions and always run. `on.manual` optionally restricts the refs they may use, with the same `branches`/`tags` matching as `push`. The filter is enforced when the run starts; rebuilds reuse the original ref and are not re-validated.
- `schedule`: a single 5-field cron expression, run on the default branch. It is independent from `push`/`pull_request` and never filtered by `paths`.

```yaml
on:
  schedule: "0 0 * * 0"
  manual:
    branches: [main] # a cron-only workflow, runnable by hand on the default branch
```

**Paths.** `paths` and `paths_ignore` are global and apply to `push` and `pull_request` after the ref match. They do not affect `schedule`, and setting them does not change the implicit default.

**Validation is strict:** an unknown event or key, `on.pull_request.tags`, a scalar `on: manual` (use `on: {}` for manual-only), or any of the removed top-level keys (`schedule`, `tags`, `branches`, `triggers`) is a config error.

## Cache

`cache` mounts a persistent Docker volume at each path (under `PICI_REPO_DIR`), so dependencies survive between runs. The volume is shared across executions of the same project.

Dependency caches are also **detected automatically** from files at the repo root — no `env`/`cache` needed. They are mounted outside the repo (at `/pici/cache`), so they never show up while scanning the working tree:

| Marker                                | Env vars                | Cached path        |
| ------------------------------------- | ----------------------- | ------------------ |
| `go.mod`                              | `GOMODCACHE`, `GOCACHE` | `gomod`, `gobuild` |
| `.golangci.{yml,yaml,toml,json}`      | `GOLANGCI_LINT_CACHE`   | `golangci-lint`    |
| `package.json`                        | `npm_config_cache`      | `npm`              |
| `yarn.lock`                           | `YARN_CACHE_FOLDER`     | `yarn`             |
| `pnpm-lock.yaml`                      | `npm_config_store_dir`  | `pnpm`             |
| `bun.lock`, `bun.lockb`               | `BUN_INSTALL`           | `bun`              |
| `deno.json`, `deno.jsonc`             | `DENO_DIR`              | `deno`             |
| `Cargo.toml`                          | `CARGO_HOME`            | `cargo`            |
| `requirements.txt` / `pyproject.toml` | `PIP_CACHE_DIR`         | `pip`              |
| `Gemfile`                             | `BUNDLE_PATH`           | `bundle`           |
| `composer.json`                       | `COMPOSER_CACHE_DIR`    | `composer`         |
| `pom.xml`                             | `MAVEN_OPTS`            | `m2`               |
| `build.gradle`, `build.gradle.kts`    | `GRADLE_USER_HOME`      | `gradle`           |
| `*.csproj`, `*.sln`                   | `NUGET_PACKAGES`        | `nuget`            |
| `*.tf`                                | `TF_PLUGIN_CACHE_DIR`   | `terraform`        |

Explicit `cache` paths still work and are added alongside the detected ones. Explicit `env` takes precedence over the detected variables.

## Re-running failed steps

When a git run fails, you can re-run **only the steps that failed or were skipped**, without redoing the successful ones. pici reuses the failed run's workspace (repository, installed dependencies, build outputs) and records the attempt as a new execution linked to the original.

From the web UI, use **Retry failed steps** on the failed execution. From the CLI:

```sh
pici-cli retry demo 42
```

A retry is possible while the workspace still exists — it is kept for `gc-keep` (default 24h) after the last linked execution finishes. Snapshot (`--local`) runs cannot be retried.

## Concurrency groups

`concurrency` cancels any currently-running execution in the same group (same project) when a new one starts, so only the latest execution of a branch or deployment keeps running.

## Built-in environment variables

| Variable            | Description                                                  |
| ------------------- | ------------------------------------------------------------ |
| `CI`                | always `true`                                                |
| `PICI`              | always `true`                                                |
| `PICI_SOURCE`       | `git` or `snapshot`                                          |
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

Git is pre-configured to trust the mounted repo (`safe.directory`) and to commit with a default identity (`user.name=pici`, `user.email=pici@localhost`), so `git` commands, commits, and Go's VCS stamping work out of the box. Override the identity with `GIT_AUTHOR_NAME`/`GIT_AUTHOR_EMAIL` if needed.

## Validate a ci.yml

```sh
pici-cli validate .ci/build/ci.yml
```

Editors can validate and autocomplete `ci.yml` as you type with the [ci.yml schema](/guide/ci-schema).
