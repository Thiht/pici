# API

Base URL: your `PICI_PUBLIC_URL` (e.g. `https://pici.example.com`), or `http://localhost:8080` locally.

For day-to-day operations, prefer the [CLI](/guide/cli). This page documents the underlying HTTP endpoints; the full interactive reference is available in [API reference](/guide/api-reference).

## Authentication

The API requires a **personal API token**, created from the Account page. Send it as `Authorization: Bearer <token>` or `X-API-Token: <token>`:

```sh
curl -H 'Authorization: Bearer pici_pat_...' https://pici.example.com/api/projects
```

Personal tokens inherit the full access of their owner and can be revoked at any time. The JSON API never accepts the browser session cookie.

`/health`, `/badge/*`, and `/api/webhooks/*` are exempt. The CLI reads `PICI_TOKEN`.

## Projects

Project `{id}` can be a project name or a UUID.

| Method   | Path                         | Description               |
| -------- | ---------------------------- | ------------------------- |
| `POST`   | `/api/projects`              | register a project        |
| `GET`    | `/api/projects`              | list projects             |
| `GET`    | `/api/projects/{id}`         | get a project             |
| `PUT`    | `/api/projects/{id}`         | update a project          |
| `DELETE` | `/api/projects/{id}`         | delete a project          |
| `GET`    | `/api/projects/{id}/configs` | list discovered workflows |

## Variables

| Method   | Path                                 | Description               |
| -------- | ------------------------------------ | ------------------------- |
| `POST`   | `/api/variables`                     | set a global variable     |
| `GET`    | `/api/variables`                     | list global variables     |
| `DELETE` | `/api/variables/{key}`               | delete a global variable  |
| `POST`   | `/api/projects/{id}/variables`       | set a project variable    |
| `GET`    | `/api/projects/{id}/variables`       | list project variables    |
| `DELETE` | `/api/projects/{id}/variables/{key}` | delete a project variable |

## Executions

Execution `{executionID}` is a per-project auto-incrementing number. An Execution is a logical run: it groups every attempt made to complete it. Endpoints read the latest attempt by default; pass `?attempt=N` (1-based) to read a specific attempt.

| Method | Path                                                            | Description                 |
| ------ | --------------------------------------------------------------- | --------------------------- |
| `POST` | `/api/projects/{id}/executions`                                 | trigger a run               |
| `GET`  | `/api/projects/{id}/executions`                                 | list executions             |
| `GET`  | `/api/projects/{id}/executions/{executionID}`                   | get an execution            |
| `GET`  | `/api/projects/{id}/executions/{executionID}/attempts`          | list the attempts           |
| `GET`  | `/api/projects/{id}/executions/{executionID}/logs`              | combined logs (text)        |
| `GET`  | `/api/projects/{id}/executions/{executionID}/logs/stream`       | stream logs over SSE        |
| `GET`  | `/api/projects/{id}/executions/{executionID}/steps/{step}/logs` | a single step's logs        |
| `POST` | `/api/projects/{id}/executions/{executionID}/cancel`            | cancel a running execution  |
| `POST` | `/api/projects/{id}/executions/{executionID}/rebuild`           | re-run with the same commit |
| `POST` | `/api/projects/{id}/executions/{executionID}/retry`             | retry failed steps          |

`POST .../executions` also accepts `multipart/form-data` (fields `workflow`, `ref`, and a `snapshot` `.tar.gz` of the worktree) to run local changes. Such executions have `source: snapshot` and cannot be rebuilt.

`POST .../retry` adds an attempt to the latest failed attempt of a git execution, reusing its workspace. Pass `?attempt=N` to target a specific attempt; only the latest attempt can be retried, so an older attempt returns `409`. It also returns `409` when the execution is not retryable (not failed, a snapshot, or already running) and `410` when the workspace is gone. `POST .../rebuild` starts a new Execution instead of adding an attempt.

`GET .../logs`, `GET .../logs/stream`, `GET .../steps/{step}/logs`, and the artifact endpoints accept `?attempt=N` to target a specific attempt.

## Artifacts

| Method | Path                                                                  | Description              |
| ------ | --------------------------------------------------------------------- | ------------------------ |
| `GET`  | `/api/projects/{id}/executions/{executionID}/artifacts`               | list collected artifacts |
| `GET`  | `/api/projects/{id}/executions/{executionID}/artifacts/{step}/{path}` | download an artifact     |

```sh
pici-cli artifacts demo 42
pici-cli artifacts demo 42 --attempt 1
pici-cli artifacts get demo 42 build/dist/app.tar.gz
```

Each artifact carries a `hash` object with the `sha256` digest of its bytes, omitted when the hash is unknown (for example an execution collected before hashes were recorded). Downloads send that digest as a strong `ETag`, so clients can reuse cached copies with `If-None-Match`.

## Cache

Docker resources tied to a project: workflow images built by pici and cross-run cache volumes (declared in `ci.yml` or detected automatically). Sizes are read from the Docker daemon and refreshed in the background every five minutes, so a listing does not wait for its disk usage walk over every volume; listing returns `503` when Docker is unavailable.

| Method   | Path                               | Description                          |
| -------- | ---------------------------------- | ------------------------------------ |
| `GET`    | `/api/projects/{id}/cache`         | list images and cache volumes        |
| `DELETE` | `/api/projects/{id}/cache/images`  | remove a built image (`?reference=`) |
| `DELETE` | `/api/projects/{id}/cache/volumes` | remove a cache volume (`?name=`)     |
| `GET`    | `/api/cache/registry`              | shared image cache size (bytes)      |
| `DELETE` | `/api/cache/registry`              | purge the shared image cache         |

```sh
pici-cli cache demo
pici-cli cache rm-image demo pici/<project-id>-build
pici-cli cache rm-volume demo pici-cache-<project-id>-node_modules
```

The shared image cache holds the workflow images pushed for docker steps and the images pulled from Docker Hub; unlike the project cache, it is not scoped to a project. The collector purges it once it exceeds `-registry-cache-max-size`.

## Streaming logs (SSE)

```sh
pici-cli logs demo 42 --follow
```

Log events are labeled by source (`setup` or the step name). A `state` event carries the execution and per-step statuses as JSON whenever they change, and a final `done` event carries the terminal status. Pass `?attempt=N` to follow a specific attempt.

## Status badge

A public SVG badge for a project, embeddable in a README. It does not require a token.

| Method | Path          | Description                                                                            |
| ------ | ------------- | -------------------------------------------------------------------------------------- |
| `GET`  | `/badge/{id}` | shields-style badge (`?workflow=` selects the workflow, `?label=` overrides the label) |

```md
![build](https://pici.example.com/badge/demo?workflow=build)
```

## System

| Method | Path                        | Description                                       |
| ------ | --------------------------- | ------------------------------------------------- |
| `GET`  | `/health`                   | healthcheck (Docker + queue depth)                |
| `GET`  | `/version`                  | server build version + VCS revision/time/modified |
| `POST` | `/api/validate`             | validate a `ci.yml`                               |
| `POST` | `/api/webhooks/github/{id}` | GitHub webhook                                    |
| `POST` | `/api/webhooks/gitlab/{id}` | GitLab webhook                                    |

## Execution object

```json
{
  "id": 42,
  "attempt": 2,
  "attempts_count": 2,
  "project_id": "…",
  "workflow": "build",
  "ref": "main",
  "commit_sha": "…",
  "status": "success",
  "trigger": "manual",
  "source": "git",
  "steps": [
    {
      "name": "test",
      "status": "success",
      "exit_code": 0,
      "started_at": "2026-09-26T14:00:00Z",
      "finished_at": "2026-09-26T14:00:05Z"
    }
  ],
  "started_at": "2026-09-26T14:00:00Z",
  "finished_at": "2026-09-26T14:00:05Z",
  "created_at": "2026-09-26T13:55:00Z"
}
```

`id` is the Execution id. `status`, `steps`, `started_at`, `finished_at`, `setup_finished_at`, and `error` describe the selected attempt (the latest one unless `?attempt=N` is given). `workflow`, `ref`, `commit_sha`, `trigger`, `created_at`, and `created_by` describe the Execution itself, so a retried execution keeps its origin trigger and creation date. `attempt` is the selected attempt number and `attempts_count` the number of attempts. `parent_id` is only set on rebuilds and points to the Execution they were rebuilt from.

Timestamps are RFC 3339 (UTC). `started_at`/`finished_at` are omitted while an execution is still pending.

Each step exposes `timeout`, the effective timeout in nanoseconds, when it is bounded.

- `status`: `pending`, `running`, `success`, `failed`, `canceled`.
- `trigger`: `manual`, `webhook`, `cron`, `rebuild`, `retry`.
- `source`: `git`, `snapshot`.

## Attempt object

`GET .../executions/{executionID}/attempts` returns one object per attempt, oldest first:

```json
{
  "attempt": 1,
  "status": "failed",
  "trigger": "manual",
  "error": "step \"test\" failed: exit code 1",
  "created_at": "2026-09-26T13:55:00Z",
  "started_at": "2026-09-26T13:55:01Z",
  "finished_at": "2026-09-26T13:56:00Z",
  "created_by": "…",
  "created_by_login": "octocat"
}
```

The first attempt carries the Execution's origin trigger; later attempts use `trigger: "retry"`.
