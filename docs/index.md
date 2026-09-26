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
pici
```

Then register a project and trigger a run:

```sh
pici-cli projects add demo https://github.com/acme/demo.git

pici-cli run demo build
```

See [Getting started](/guide/getting-started) for a full walkthrough.
