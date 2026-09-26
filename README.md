# pici

A minimal, self-hosted CI system. It focuses on *running* workflows (build/test) and stays out of the way of your runtime.

The server exposes a small HTTP API, and execution happens in Docker. Each repository describes its workflows in a `.ci` directory at the root of the repo.

## Concepts

- **Project** — a git repository (GitHub, GitLab, or any git URL, public or private).
- **Variable / Secret** — injected into every workflow execution as environment variables. Secrets are masked in API responses.
- **Execution** — one run of one workflow on one project.
- **Workflow** — a folder under `.ci/`. One folder = one workflow.

```
.ci/
  build/            <- workflow "build"
    Dockerfile      <- the CI runner image (any base, install any tools)
    ci.yml          <- step orchestration
    install.sh      <- a step (any language)
    test.py         <- a step (any language)
```

## How a workflow runs

1. pici clones the repo (the requested branch/tag/SHA).
2. It builds the workflow's `Dockerfile` (pre-built and cached by tag).
3. Each step runs as a container from that image, with the repo bind-mounted at `PICI_REPO_DIR`.
4. Project/global variables and secrets are injected as environment variables.

### ci.yml format

```yaml
name: build          # optional (defaults to the folder name)

image: node:20       # optional: use this image instead of building the Dockerfile

steps:
  - name: install
    script: install.sh       # path relative to .ci/<workflow>/
    timeout: 5m

  - name: test
    run: pytest -q           # alternative to `script`: an arbitrary shell command
    depends_on: [install]
    env:
      FOO: bar
```

- A step must define either `script` or `run`.
- `script` runs through its shebang (`#!...`) if present, otherwise its extension is used to pick an interpreter (`.sh`, `.py`, `.js`, `.rb`, `.go`, ...).
- `timeout` is per-step; default comes from `PICI_STEP_TIMEOUT` (30m).
- `depends_on` controls ordering; a failure skips downstream steps.

### Built-in environment variables

| Variable | Description |
|---|---|
| `CI` | always `true` |
| `PICI_PROJECT` | project name |
| `PICI_PROJECT_ID` | project id |
| `PICI_WORKFLOW` | workflow name |
| `PICI_EXECUTION_ID` | execution id |
| `PICI_REF` | the ref being built |
| `PICI_COMMIT_SHA` | resolved commit SHA |
| `PICI_REPO_DIR` | mount path of the repo (default `/workspace`) |
| `PICI_WORKFLOW_DIR` | mount path of the workflow folder |

## Configuration

Configuration is flags-first ([ff](https://github.com/peterbourgon/ff)), with three sources in priority order: **flags**, then **environment variables** (`PICI_` prefix), then a **JSON config file** (`-config`).

| Flag | Env | Default | Description |
|---|---|---|---|
| `-http-addr` | `PICI_HTTP_ADDR` | `:8080` | HTTP listen address |
| `-db-driver` | `PICI_DB_DRIVER` | `sqlite` | `sqlite` or `postgres` |
| `-db-dsn` | `PICI_DB_DSN` | `pici.db` | SQLite path, or Postgres DSN |
| `-workspace-dir` | `PICI_WORKSPACE_DIR` | `~/.pici/workspaces` | where clones and logs live |
| `-repo-mount-path` | `PICI_REPO_MOUNT_PATH` | `/workspace` | in-container mount point of the repo |
| `-concurrency` | `PICI_CONCURRENCY` | `4` | max concurrent executions |
| `-step-timeout` | `PICI_STEP_TIMEOUT` | `30m` | default step timeout |
| `-config` | `PICI_CONFIG` | — | path to a JSON config file |

Docker is reached via the standard Docker environment (`DOCKER_HOST`, `~/.docker`, ...).

```sh
# flags
pici -http-addr :9090 -concurrency 8

# env
PICI_DB_DSN=/var/lib/pici/pici.db pici

# config file (JSON)
pici -config /etc/pici.json
```

## API

### Projects

```
POST   /api/projects                     register a project
GET    /api/projects                     list projects
GET    /api/projects/{id}                get a project
PUT    /api/projects/{id}                update a project
DELETE /api/projects/{id}                delete a project
GET    /api/projects/{id}/configs        list workflows discovered in .ci
```

Register a public GitHub repo:

```sh
pici-cli projects add demo https://github.com/acme/demo.git
```

Register a private repo (token or SSH):

```sh
pici-cli projects add --auth-type token --auth-user x-access-token --auth-secret ghp_... private https://github.com/acme/private.git

pici-cli projects add --auth-type ssh --auth-secret '-----BEGIN OPENSSH PRIVATE KEY-----...' private git@github.com:acme/private.git
```

`auth_type` is `none`, `token`, or `ssh`. `auth_user` defaults to `oauth2` (token) / `git` (ssh).

### Variables and secrets

```
POST   /api/variables                     set a global variable
GET    /api/variables                     list global variables
DELETE /api/variables/{key}               delete a global variable

POST   /api/projects/{id}/variables       set a project variable
GET    /api/projects/{id}/variables       list project variables
DELETE /api/projects/{id}/variables/{key} delete a project variable
```

```sh
pici-cli vars set NPM_TOKEN secret --project demo --secret
```

### Executions

```
POST  /api/projects/{id}/executions       trigger a run
GET   /api/projects/{id}/executions       list executions
GET   /api/executions/{id}                get an execution
GET   /api/executions/{id}/logs           stream logs
POST  /api/executions/{id}/cancel         cancel a running execution
```

```sh
pici-cli run demo build

pici-cli logs <id>
```

## Design notes

- Plain `net/http` (Go 1.22+ routing), no framework.
- Storage: `sqlite` (default, no CGO via `modernc.org/sqlite`) or `postgres` (via `pgx`).
- Git: `go-git` (pure Go), supports HTTPS+token and SSH.
- Docker: official Engine API SDK.

## Features

- Projects (public/private, GitHub/GitLab/generic), variables & secrets.
- Workflows from `.ci/` (Dockerfile runner, `ci.yml` orchestration).
- Parallel steps, retries, per-step logs, path filters, workflow-level `env`.
- GitHub & GitLab webhooks (push/PR/tag), GitHub check runs, cron schedules.
- Artifacts, cross-run cache (Docker volumes), concurrency groups.
- Secrets encrypted at rest + masked in logs, DB-backed queue with crash recovery.
- Streaming logs (SSE), graceful shutdown, garbage collection, healthcheck, and a CLI (`cmd/pici-cli`).
- CLI shell completion (bash/zsh/fish/powershell) for commands, flags, and dynamic values (projects, workflows, variable keys).

Full documentation (Vitepress) lives in [`docs/`](docs/).

## Not yet implemented

- UI.
- GitLab MR pipelines (webhooks are supported).
