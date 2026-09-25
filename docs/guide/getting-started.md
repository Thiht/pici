# Getting started

## Prerequisites

- Go 1.27+
- Docker (running, with the Engine API reachable)
- SQLite (default) or Postgres

## Run the server

Two settings are **required** (the server refuses to start without them): a secret key and an API token.

```sh
export PICI_SECRET_KEY=$(openssl rand -hex 32)
export PICI_API_TOKEN=$(openssl rand -hex 24)

go run ./cmd/pici
```

Configuration is flags-first. See [Configuration](/guide/configuration) for the full list.

```sh
go run ./cmd/pici -http-addr :8080 -db-driver sqlite -db-dsn pici.db
```

## Add a project

Register a repository (GitHub, GitLab, or any git URL). Include the API token on every request:

```sh
curl -X POST localhost:8080/api/projects \
  -H "Authorization: Bearer $PICI_API_TOKEN" \
  -H 'content-type: application/json' \
  -d '{"name":"demo","repo_url":"https://github.com/acme/demo.git","default_branch":"main"}'
```

## Write a workflow

In your repository, create a `.ci/build` folder:

```
.ci/
  build/
    Dockerfile
    ci.yml
    install.sh
    test.py
```

`Dockerfile`:

```dockerfile
FROM alpine:3.20
RUN apk add --no-cache python3
```

`ci.yml`:

```yaml
name: build
steps:
  - name: install
    script: install.sh
  - name: test
    script: test.py
    depends_on: [install]
```

## Trigger a run

```sh
curl -X POST localhost:8080/api/projects/demo/executions \
  -H "Authorization: Bearer $PICI_API_TOKEN" \
  -H 'content-type: application/json' \
  -d '{"workflow":"build","ref":"main"}'
```

Then watch the logs:

```sh
curl -H "Authorization: Bearer $PICI_API_TOKEN" localhost:8080/api/executions/<id>/logs
```

Or use the [CLI](/guide/cli): `PICI_TOKEN=$PICI_API_TOKEN pici-cli run demo build`.
