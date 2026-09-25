# pici

A minimal, self-hosted CI system. It focuses on **running** workflows (build, test) and stays out of the way of your runtime.

The server exposes a small HTTP API, and execution happens in Docker. Each repository describes its workflows in a `.ci` directory at the root of the repo.

## Highlights

- **Simple**: plain `net/http`, SQLite or Postgres, `go-git`, Docker Engine API.
- **Workflows as code**: a folder under `.ci/` is one workflow, with a `Dockerfile` as the runner.
- **Full control**: variables & secrets injected as env vars, parallel steps, retries, path filters.
- **Automation**: GitHub webhooks (push / PR / tag), cron schedules, GitHub check runs.

## Quick start

```sh
go run ./cmd/pici
```

Then register a project and trigger a run:

```sh
curl -X POST localhost:8080/api/projects \
  -H 'content-type: application/json' \
  -d '{"name":"demo","repo_url":"https://github.com/acme/demo.git"}'

curl -X POST localhost:8080/api/projects/demo/executions \
  -H 'content-type: application/json' \
  -d '{"workflow":"build","ref":"main"}'
```

See [Getting started](/guide/getting-started) for a full walkthrough.
