# API

Base URL: `http://localhost:8080`.

## Authentication

The API token is **required** (set via `-api-token` or `PICI_API_TOKEN`; the server refuses to start without it). Send it either as `Authorization: Bearer <token>` or `X-API-Token: <token>`:

```sh
curl -H 'Authorization: Bearer <token>' localhost:8080/api/projects
```

`/health` and `/api/webhooks/*` are exempt (webhooks use their own signature/token). The [CLI](/guide/cli) reads `PICI_TOKEN`.

## Projects

| Method | Path | Description |
|---|---|---|
| `POST` | `/api/projects` | register a project |
| `GET` | `/api/projects` | list projects |
| `GET` | `/api/projects/{id}` | get a project |
| `PUT` | `/api/projects/{id}` | update a project |
| `DELETE` | `/api/projects/{id}` | delete a project |
| `GET` | `/api/projects/{id}/configs` | list discovered workflows |

## Variables

| Method | Path | Description |
|---|---|---|
| `POST` | `/api/variables` | set a global variable |
| `GET` | `/api/variables` | list global variables |
| `DELETE` | `/api/variables/{key}` | delete a global variable |
| `POST` | `/api/projects/{id}/variables` | set a project variable |
| `GET` | `/api/projects/{id}/variables` | list project variables |
| `DELETE` | `/api/projects/{id}/variables/{key}` | delete a project variable |

## Executions

| Method | Path | Description |
|---|---|---|
| `POST` | `/api/projects/{id}/executions` | trigger a run |
| `GET` | `/api/projects/{id}/executions` | list executions |
| `GET` | `/api/executions/{id}` | get an execution |
| `GET` | `/api/executions/{id}/logs` | combined logs |
| `GET` | `/api/executions/{id}/logs/stream` | stream logs over SSE |
| `GET` | `/api/executions/{id}/steps/{step}/logs` | a single step's logs |
| `POST` | `/api/executions/{id}/cancel` | cancel a running execution |
| `POST` | `/api/executions/{id}/rebuild` | re-run with the same commit |

## Artifacts

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/executions/{id}/artifacts` | list collected artifacts |
| `GET` | `/api/executions/{id}/artifacts/{step}/{path}` | download an artifact |

```sh
curl localhost:8080/api/executions/<id>/artifacts
curl -o app.tar.gz localhost:8080/api/executions/<id>/artifacts/build/dist/app.tar.gz
```

## Streaming logs (SSE)

```sh
curl -N localhost:8080/api/executions/<id>/logs/stream
```

Events are labeled by source (`setup` or the step name); a final `done` event carries the execution status.

## Other

| Method | Path | Description |
|---|---|---|
| `GET` | `/health` | healthcheck (docker + queue depth) |
| `POST` | `/api/validate` | validate a `ci.yml` |
| `POST` | `/api/webhooks/github/{id}` | GitHub webhook |
| `POST` | `/api/webhooks/gitlab/{id}` | GitLab webhook |

An execution looks like:

```json
{
  "id": "…",
  "project_id": "…",
  "workflow": "build",
  "ref": "main",
  "commit_sha": "…",
  "status": "success",
  "trigger": "manual",
  "steps": [
    { "name": "test", "status": "success", "exit_code": 0, "started_at": 0, "finished_at": 0 }
  ],
  "started_at": 0,
  "finished_at": 0,
  "created_at": 0
}
```

`status` is one of `pending`, `running`, `success`, `failed`, `canceled`. `trigger` is one of `manual`, `webhook`, `cron`, `rebuild`.
