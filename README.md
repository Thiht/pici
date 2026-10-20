# pici

A minimal, self-hosted CI system. It focuses on _running_ workflows (build, test) and stays out of the way of your runtime.

[![Deploy to Fly.io](https://img.shields.io/badge/Deploy%20to-Fly.io-8B5CF6?logo=flydotio&logoColor=white)](docs/guide/fly.md)

The server exposes a small HTTP API, and execution happens in Docker. Workflows live in a `.ci/` directory at the root of the repository.

- **Workflows as code** — a `.ci/<workflow>/` folder, with a `Dockerfile` for the runner and a `ci.yml` for the steps.
- **No runtime lock-in** — any language, any tool; steps are just commands in a container.
- **Web UI and CLI** — projects, runs, logs, artifacts, and Docker cache from the browser or the terminal.
- **Automation** — GitHub and GitLab webhooks, pull request checks, cron schedules, status badges.
- **Simple operations** — SQLite or Postgres, a single binary, no external queue or worker.
- **Access control** — GitHub, GitLab, or OpenID Connect sign-in restricted to teams/groups or an allowlist.

## Quick start

```sh
export PICI_SECRET_KEY=$(openssl rand -hex 32)
export PICI_PUBLIC_URL=http://localhost:8080
export PICI_GITHUB_CLIENT_ID=Iv1.abc123
export PICI_GITHUB_CLIENT_SECRET=ghs_...
export PICI_GITHUB_TEAMS=acme/pici-users

pici
```

Open `http://localhost:8080`, sign in, and create a personal API token from the **Account** page. Then register a project and trigger a run:

```sh
export PICI_TOKEN=pici_pat_...

pici-cli projects add demo https://github.com/acme/demo.git
pici-cli run demo build
pici-cli logs demo 1 --follow
```

The OAuth app callback URL is `http://localhost:8080/auth/github/callback`. For GitLab, OpenID Connect, personal instances, and the full walkthrough, see [Getting started](docs/guide/getting-started.md) and [Authentication](docs/guide/authentication.md).

## Concepts

- **Project** — a git repository (GitHub, GitLab, or any git URL, public or private).
- **Workflow** — a folder under `.ci/`. One folder = one workflow.
- **Execution** — one run of one workflow on one project.
- **Variable / secret** — injected into every execution as environment variables. Secrets are encrypted at rest and masked in logs and API responses.

## A workflow

```
.ci/
  build/            <- workflow "build"
    Dockerfile      <- the runner image (any base, install any tools)
    ci.yml          <- step orchestration
    install.sh      <- a step (any language)
    test.py         <- a step (any language)
```

```yaml
name: build
steps:
  - name: install
    script: install.sh

  - name: test
    run: python3 test.py
    depends_on: [install]
```

A step runs either a script (`script`, relative to the workflow folder) or a shell command (`run`). Independent steps run in parallel. See [Workflows](docs/guide/workflows.md) for triggers, caching, artifacts, retries, and built-in variables.

## Documentation

- [Getting started](docs/guide/getting-started.md)
- [Authentication](docs/guide/authentication.md)
- [Projects](docs/guide/projects.md)
- [Workflows](docs/guide/workflows.md) — [ci.yml schema](docs/guide/ci-schema.md)
- [Variables & secrets](docs/guide/variables.md)
- [Webhooks & schedules](docs/guide/webhooks.md)
- [Docker](docs/guide/docker.md) — [Fly.io](docs/guide/fly.md)
- [API](docs/guide/api.md) — [CLI](docs/guide/cli.md) — [Configuration](docs/guide/configuration.md)

## Deployment

Fly.io is the recommended hosted target. The deploy script creates the app, volume, and secrets, and prints the OAuth callback URL:

```sh
sh -c "$(curl -fsSL https://raw.githubusercontent.com/Thiht/pici/main/scripts/deploy-fly.sh)"
```

A Docker-in-Docker image is also available; see [Running with Docker](docs/guide/docker.md).

## Development

```sh
task build         # pici and pici-cli in dist/
task test          # go test ./...
task test:postgres # store tests against a throwaway Postgres
task lint
task fmt
task docs:dev      # serve the documentation
```

The web UI is server-rendered Go templates with htmx, styled with Tailwind + daisyUI. Embedded assets are committed, so building the Go binary never needs Node; refresh them with `task ui:build`.

Releases are built by the [`.ci/release`](.ci/release) workflow when a `vX.Y.Z` tag is pushed. It needs a `GH_TOKEN` secret with `contents: write`.
