# pici

A minimal, self-hosted CI system. It focuses on _running_ workflows (build/test) and stays out of the way of your runtime.

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

### Running on local changes

Upload the current worktree — uncommitted and untracked (non-ignored) files
included — and run a workflow on it without pushing:

```sh
pici-cli run demo build --local
```

The CLI archives the worktree plus `.git/` (never `.git/config`, which may
contain credentials) and the server extracts it instead of cloning. Snapshot
executions are marked `local`, cannot be rebuilt, and do not post check runs.

Snapshot uploads require the API process and the execution worker to share the
same workspace directory (single node, or a shared volume): the upload is
written under `<workspace>/uploads` by the API and read back by the worker.

Snapshots work with both SQLite and Postgres (they share the same execution
code path). The store tests run against both: `go test -tags=postgres
./internal/stores` expects a Postgres on `127.0.0.1:55432`, which
`task test:postgres` starts for you (one database per test).

### ci.yml format

```yaml
name: build # optional (defaults to the folder name)

image: node:20 # optional: use this image instead of building the Dockerfile

steps:
  - name: install
    script: install.sh # path relative to .ci/<workflow>/
    timeout: 5m

  - name: test
    run: pytest -q # alternative to `script`: an arbitrary shell command
    depends_on: [install]
    env:
      FOO: bar
```

- A step must define either `script` or `run`.
- `script` runs through its shebang (`#!...`) if present, otherwise its extension is used to pick an interpreter (`.sh`, `.py`, `.js`, `.rb`, `.go`, ...).
- `timeout` is per-step; default comes from `PICI_STEP_TIMEOUT` (30m).
- `depends_on` controls ordering; a failure skips downstream steps.

### Built-in environment variables

| Variable            | Description                                   |
| ------------------- | --------------------------------------------- |
| `CI`                | always `true`                                 |
| `PICI_PROJECT`      | project name                                  |
| `PICI_PROJECT_ID`   | project id                                    |
| `PICI_REPO_URL`     | repo clone URL                                |
| `PICI_REPO_SLUG`    | repo path (`owner/repo`) without `.git`       |
| `PICI_WORKFLOW`     | workflow name                                 |
| `PICI_EXECUTION_ID` | execution number (per project)                |
| `PICI_REF`          | the ref being built                           |
| `PICI_VERSION`      | tag name when building a tag, else short SHA  |
| `PICI_COMMIT_SHA`   | resolved commit SHA                           |
| `PICI_SOURCE`       | `git` or `snapshot`                           |
| `PICI_REPO_DIR`     | mount path of the repo (default `/workspace`) |
| `PICI_WORKFLOW_DIR` | mount path of the workflow folder             |

The mounted repo is trusted automatically (`safe.directory`), so `git` and Go's VCS stamping work without any setup in your steps.

## Configuration

Configuration is flags-first ([ff](https://github.com/peterbourgon/ff)), with three sources in priority order: **flags**, then **environment variables** (`PICI_` prefix), then a **JSON config file** (`-config`).

| Flag                    | Env                         | Default              | Description                                    |
| ----------------------- | --------------------------- | -------------------- | ---------------------------------------------- |
| `-http-addr`            | `PICI_HTTP_ADDR`            | `:8080`              | HTTP listen address                            |
| `-db-driver`            | `PICI_DB_DRIVER`            | `sqlite`             | `sqlite` or `postgres`                         |
| `-db-dsn`               | `PICI_DB_DSN`               | `pici.db`            | SQLite path, or Postgres DSN                   |
| `-workspace-dir`        | `PICI_WORKSPACE_DIR`        | `~/.pici/workspaces` | where clones and logs live                     |
| `-repo-mount-path`      | `PICI_REPO_MOUNT_PATH`      | `/workspace`         | in-container mount point of the repo           |
| `-concurrency`          | `PICI_CONCURRENCY`          | `4`                  | max concurrent executions                      |
| `-step-timeout`         | `PICI_STEP_TIMEOUT`         | `30m`                | default step timeout                           |
| `-secret-key`           | `PICI_SECRET_KEY`           | —                    | **required** — 32-byte key for secrets at rest |
| `-public-url`           | `PICI_PUBLIC_URL`           | —                    | **required** — public base URL                 |
| `-github-client-id`     | `PICI_GITHUB_CLIENT_ID`     | —                    | **required** — OAuth App client id             |
| `-github-client-secret` | `PICI_GITHUB_CLIENT_SECRET` | —                    | **required** — OAuth App client secret         |
| `-github-teams`         | `PICI_GITHUB_TEAMS`         | —                    | allowed teams (`org/team`)                     |
| `-github-users`         | `PICI_GITHUB_USERS`         | —                    | allowed GitHub logins                          |
| `-session-ttl`          | `PICI_SESSION_TTL`          | `720h`               | browser session lifetime (sliding)             |
| `-config`               | `PICI_CONFIG`               | —                    | path to a JSON config file                     |

Docker is reached via the standard Docker environment (`DOCKER_HOST`, `~/.docker`, ...).

```sh
# flags
pici -http-addr :9090 -concurrency 8

# env
PICI_DB_DSN=/var/lib/pici/pici.db \
PICI_PUBLIC_URL=https://pici.example.com \
PICI_GITHUB_CLIENT_ID=Iv1.abc123 \
PICI_GITHUB_CLIENT_SECRET=ghs_... \
PICI_GITHUB_TEAMS=acme/pici-users \
PICI_SECRET_KEY=$(openssl rand -hex 32) \
pici

# personal instance, no organization
# PICI_GITHUB_USERS=Thiht

# config file (JSON)
pici -config /etc/pici.json
```

At least one of `PICI_GITHUB_TEAMS` or `PICI_GITHUB_USERS` is required. See
[Authentication](docs/guide/authentication.md) for the GitHub OAuth App setup
and both access models.

## API

The API requires a **personal API token**, created from the Account page after
signing in with GitHub. Send it as `Authorization: Bearer <token>` or
`X-API-Token: <token>`. The browser session cookie is not accepted by the API.

```sh
curl -H 'Authorization: Bearer pici_pat_...' https://pici.example.com/api/projects
```

`/health`, `/badge/*` and `/api/webhooks/*` are exempt.

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
POST  /api/projects/{id}/executions                        trigger a run
GET   /api/projects/{id}/executions                        list executions
GET   /api/projects/{id}/executions/{executionID}          get an execution
GET   /api/projects/{id}/executions/{executionID}/logs     stream logs
POST  /api/projects/{id}/executions/{executionID}/cancel   cancel a running execution
POST  /api/projects/{id}/executions/{executionID}/rebuild  re-run with the same commit
POST  /api/projects/{id}/executions/{executionID}/retry   retry failed steps (reuses the workspace)
```

```sh
pici-cli run demo build

pici-cli logs demo 42

pici-cli retry demo 42
```

### Cache

The Docker cache tied to a project: workflow images built by pici and the
cross-run cache volumes declared in `ci.yml`. Sizes come from the Docker daemon.

```
GET    /api/projects/{id}/cache                     list images and cache volumes
DELETE /api/projects/{id}/cache/images?reference=…  remove a built image
DELETE /api/projects/{id}/cache/volumes?name=…      remove a cross-run cache volume
```

```sh
pici-cli cache demo
pici-cli cache rm-image demo pici/<project-id>-build
pici-cli cache rm-volume demo pici-cache-<project-id>-node_modules
```

### Badge

A public SVG status badge (shields-style) for a project, suitable for a README.
`workflow` selects the workflow (newest execution overall otherwise); `label`
overrides the text shown on the left.

```
GET    /badge/{id}?workflow=build&label=build
```

```md
![build](https://pici.example.com/badge/demo?workflow=build)
```

## Features

- Projects (public/private, GitHub/GitLab/generic), variables & secrets.
- Workflows from `.ci/` (Dockerfile runner, `ci.yml` orchestration).
- Parallel steps, retries, per-step logs, tag/branch filters, path filters, workflow-level `env`.
- GitHub & GitLab webhooks (push/PR/MR/tag), GitHub check runs, GitLab commit statuses, cron schedules; pull requests opened from a fork are never built automatically.
- Artifacts, cross-run cache (Docker volumes), concurrency groups.
- Partial re-run of failed steps, reusing the failed run's workspace.
- GitHub OAuth login restricted to configured teams or a user allowlist, revocable personal API tokens, server-side sessions, and per-execution user attribution.
- Secrets encrypted at rest + masked in logs, DB-backed queue with crash recovery.
- Streaming logs (SSE) in the UI and on the API, public README status badges, graceful shutdown, garbage collection, healthcheck/version endpoints, and a CLI (`cmd/pici-cli`).
- CLI shell completion (bash/zsh/fish/powershell) for commands, flags, and dynamic values (projects, workflows, variable keys).

Full documentation (Vitepress) lives in [`docs/`](docs/).

## Releases

Pushing a `vX.Y.Z` tag triggers the [`.ci/release`](.ci/release) workflow, which builds `pici` and `pici-cli` with `main.buildVersion` injected via `-ldflags`, then creates a GitHub release with both binaries attached. It requires a `GH_TOKEN` secret with `contents: write`:

```sh
pici-cli vars set GH_TOKEN <token> --secret
```

Check the running build with `pici-cli version` (local) or `pici-cli version --server`, or `GET /version`.

## Web UI

An embedded management UI is served at `/` by the same binary (server-rendered Go
templates + htmx, styled with Tailwind + daisyUI, no Node at runtime). It covers
projects, variables & secrets, triggering/cancelling/rebuilding/retrying executions, per-step
and full logs, artifacts, and per-project Docker cache (images and cache volumes).

Humans sign in with **GitHub OAuth**; access is restricted to the teams listed
in `PICI_GITHUB_TEAMS`, the logins listed in `PICI_GITHUB_USERS`, or both.
Browser sessions are stored server-side and kept in an `HttpOnly`, `Secure`
cookie. The Account page lets each user create,
rename and revoke personal API tokens, and revoke browser sessions.

The JSON API only accepts personal tokens (`Bearer` / `X-API-Token`); it never
accepts the session cookie. Static assets, the login page and the OAuth callback
are the only unauthenticated UI routes.

The execution page streams logs live (SSE) while a run is in progress and falls
back to a static render once it finishes.

Embedded assets are committed, so building the Go binary never needs Node. To refresh
them (Tailwind, daisyUI and htmx via npm, build-time only):

```sh
task ui:build   # npm install, build app.css, copy htmx
```
