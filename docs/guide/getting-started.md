# Getting started

## Prerequisites

- Docker (running, with the Engine API reachable)
- SQLite (default) or Postgres

## Run the server

pici signs people in with GitHub. You need a GitHub OAuth App, a secret key,
and either a team whose members may use pici or a list of allowed GitHub
logins. The server refuses to start without them.

```sh
export PICI_SECRET_KEY=$(openssl rand -hex 32)
export PICI_PUBLIC_URL=http://localhost:8080
export PICI_GITHUB_CLIENT_ID=Iv1.abc123
export PICI_GITHUB_CLIENT_SECRET=ghs_...
export PICI_GITHUB_TEAMS=acme/pici-users

pici
```

For a personal instance with no organization, replace the team with an
allowlist:

```sh
export PICI_GITHUB_USERS=your-github-login
```

Create the OAuth App with callback URL
`http://localhost:8080/auth/github/callback`, then open
`http://localhost:8080` and sign in. See
[Authentication](/guide/authentication) for the full setup, including
organization approval in team mode.

Configuration is flags-first. See [Configuration](/guide/configuration) for the full list.

```sh
pici -http-addr :8080 -db-driver sqlite -db-dsn pici.db
```

## Create an API token

Open the **Account** page and create a personal token. Copy it now: it is shown
once. Then export it for the CLI:

```sh
export PICI_TOKEN=pici_pat_...
```

## Add a project

Register a repository (GitHub, GitLab, or any git URL):

```sh
pici-cli projects add demo https://github.com/acme/demo.git
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
pici-cli run demo build
```

Then watch the logs:

```sh
pici-cli logs <id> --follow
```
