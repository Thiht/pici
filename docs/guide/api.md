# API

Base URL: `http://localhost:8080`.

For day-to-day operations, prefer the [CLI](/guide/cli) (`pici-cli`). This page documents the underlying HTTP endpoints.

## Authentication

The API token is **required** (set via `-api-token` or `PICI_API_TOKEN`; the server refuses to start without it). Send it either as `Authorization: Bearer <token>` or `X-API-Token: <token>`:

```sh
curl -H 'Authorization: Bearer <token>' localhost:8080/api/projects
```

`/health` and `/api/webhooks/*` are exempt (webhooks use their own signature/token). The [CLI](/guide/cli) reads `PICI_TOKEN`.

The full API is also available as an [interactive OpenAPI reference](/guide/api-reference).

## Projects

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

Execution `{executionID}` is a per-project auto-incrementing number.

| Method | Path                                                            | Description                 |
| ------ | --------------------------------------------------------------- | --------------------------- |
| `POST` | `/api/projects/{id}/executions`                                 | trigger a run               |
| `GET`  | `/api/projects/{id}/executions`                                 | list executions             |
| `GET`  | `/api/projects/{id}/executions/{executionID}`                   | get an execution            |
| `GET`  | `/api/projects/{id}/executions/{executionID}/logs`              | combined logs               |
| `GET`  | `/api/projects/{id}/executions/{executionID}/logs/stream`       | stream logs over SSE        |
| `GET`  | `/api/projects/{id}/executions/{executionID}/steps/{step}/logs` | a single step's logs        |
| `POST` | `/api/projects/{id}/executions/{executionID}/cancel`            | cancel a running execution  |
| `POST` | `/api/projects/{id}/executions/{executionID}/rebuild`           | re-run with the same commit |
| `POST` | `/api/projects/{id}/executions/{executionID}/retry`            | retry failed steps          |

`POST .../executions` also accepts `multipart/form-data` (fields `workflow`, `ref`, and a `snapshot` `.tar.gz` of the worktree) to run local changes; such executions are `source: snapshot` and cannot be rebuilt.

## Artifacts

| Method | Path                                                                  | Description              |
| ------ | --------------------------------------------------------------------- | ------------------------ |
| `GET`  | `/api/projects/{id}/executions/{executionID}/artifacts`               | list collected artifacts |
| `GET`  | `/api/projects/{id}/executions/{executionID}/artifacts/{step}/{path}` | download an artifact     |

```sh
pici-cli artifacts demo 42
pici-cli artifacts get demo 42 build/dist/app.tar.gz
```

## Cache

Docker resources tied to a project: workflow images built by pici and the cross-run
cache volumes declared in `ci.yml`. Sizes are read from the Docker daemon; listing
returns `503` when Docker is unavailable.

| Method   | Path                               | Description                          |
| -------- | ---------------------------------- | ------------------------------------ |
| `GET`    | `/api/projects/{id}/cache`         | list images and cache volumes        |
| `DELETE` | `/api/projects/{id}/cache/images`  | remove a built image (`?reference=`) |
| `DELETE` | `/api/projects/{id}/cache/volumes` | remove a cache volume (`?name=`)     |

```sh
pici-cli cache demo
pici-cli cache rm-image demo pici/<project-id>-build
pici-cli cache rm-volume demo pici-cache-<project-id>-node_modules
```

## Streaming logs (SSE)

```sh
pici-cli logs demo 42 --follow
```

Log events are labeled by source (`setup` or the step name). A `state` event carries the execution and per-step statuses as JSON whenever they change, and a final `done` event carries the terminal status.

## Status badge

A public SVG badge for a project, embeddable in a README. It does not require the
API token.

| Method | Path          | Description                                                                            |
| ------ | ------------- | -------------------------------------------------------------------------------------- |
| `GET`  | `/badge/{id}` | shields-style badge (`?workflow=` selects the workflow, `?label=` overrides the label) |

```md
![build](http://<your-host>:8080/badge/demo?workflow=build)
```

## Other

| Method | Path                        | Description                                       |
| ------ | --------------------------- | ------------------------------------------------- |
| `GET`  | `/health`                   | healthcheck (docker + queue depth)                |
| `GET`  | `/version`                  | server build version + VCS revision/time/modified |
| `POST` | `/api/validate`             | validate a `ci.yml`                               |
| `POST` | `/api/webhooks/github/{id}` | GitHub webhook                                    |
| `POST` | `/api/webhooks/gitlab/{id}` | GitLab webhook                                    |

An execution looks like:

```json
{
  "id": 42,
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
  "created_at": "2026-09-26T14:00:00Z"
}
```

Timestamps are RFC 3339 (UTC). `started_at`/`finished_at` are omitted while an execution is still pending.

`status` is one of `pending`, `running`, `success`, `failed`, `canceled`. `trigger` is one of `manual`, `webhook`, `cron`, `rebuild`, `retry`. `source` is `git` or `snapshot`.

`POST .../retry` retries the failed steps of a git execution, reusing its
workspace. It returns `409` when the execution is not retryable (not failed,
a snapshot, or already running) and `410` when the workspace is gone.
