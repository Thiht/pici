# Getting started

## Prerequisites

- Docker, running, with the Engine API reachable.
- SQLite (default) or Postgres.

## 1. Start the server

pici signs people in with GitHub, GitLab, or any OpenID Connect provider. Create an OAuth application, set a secret key, and list who may sign in. The server refuses to start without them.

```sh
export PICI_SECRET_KEY=$(openssl rand -hex 32)
export PICI_PUBLIC_URL=http://localhost:8080
export PICI_GITHUB_CLIENT_ID=Iv1.abc123
export PICI_GITHUB_CLIENT_SECRET=ghs_...
export PICI_GITHUB_TEAMS=acme/pici-users

pici
```

Create the GitHub OAuth App with callback URL `http://localhost:8080/auth/github/callback`. For a personal instance with no organization, use `PICI_GITHUB_USERS=your-github-login` instead of `PICI_GITHUB_TEAMS`.

To use GitLab or OpenID Connect instead, set `PICI_AUTH_PROVIDER` and the matching variables. The callback is `PICI_PUBLIC_URL/auth/<provider>/callback`. Choose one of:

```sh
# GitLab
export PICI_AUTH_PROVIDER=gitlab
export PICI_GITLAB_CLIENT_ID=0123456789abcdef
export PICI_GITLAB_CLIENT_SECRET=gloas-...
export PICI_GITLAB_GROUPS=acme/pici-users
```

```sh
# OpenID Connect
export PICI_AUTH_PROVIDER=oidc
export PICI_OIDC_ISSUER=https://keycloak.example.com/realms/pici
export PICI_OIDC_CLIENT_ID=pici
export PICI_OIDC_CLIENT_SECRET=...
export PICI_OIDC_GROUPS=acme/pici-users
```

See [Authentication](/guide/authentication) for the full setup.

Configuration is flags-first: flags, then `PICI_*` environment variables, then an optional JSON config file.

```sh
pici -http-addr :8080 -db-driver sqlite -db-dsn pici.db
```

See [Configuration](/guide/configuration) for every setting.

## 2. Create an API token

Open `http://localhost:8080`, sign in, go to **Account**, and create a personal token. Copy it now: it is shown once.

```sh
export PICI_TOKEN=pici_pat_...
```

## 3. Register a project

A project is a git repository: GitHub, GitLab, or any git URL.

```sh
pici-cli projects add demo https://github.com/acme/demo.git
```

## 4. Write a workflow

A workflow is a folder under `.ci/` in the repository. It contains a `Dockerfile` (the runner image) and a `ci.yml` (the steps).

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

## 5. Trigger a run

```sh
pici-cli run demo build
pici-cli logs demo 1 --follow
```

## Next steps

- [Workflows](/guide/workflows): triggers, cache, artifacts, retries.
- [Webhooks & schedules](/guide/webhooks): run on push, pull requests, and on a cron.
- [Deployment](/guide/docker): run pici in production.
