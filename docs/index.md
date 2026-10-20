# pici

A minimal, self-hosted CI system. It focuses on **running** workflows (build, test) and stays out of the way of your runtime.

The server exposes a small HTTP API, and execution happens in Docker. Each repository describes its workflows in a `.ci` directory at the root of the repo.

## Highlights

- **Workflows as code**: a folder under `.ci/` is one workflow, with a `Dockerfile` as the runner.
- **Full control**: variables & secrets injected as env vars, parallel steps, retries, path filters.
- **Automation**: GitHub & GitLab webhooks (push / PR / MR / tag), cron schedules, GitHub check runs and GitLab commit statuses, README status badges.
- **Access control**: GitHub, GitLab, or OpenID Connect login restricted to configured teams/groups or a user allowlist, revocable personal API tokens, per-execution attribution.

## Quick start

```sh
export PICI_SECRET_KEY=$(openssl rand -hex 32)
export PICI_PUBLIC_URL=http://localhost:8080
export PICI_GITHUB_CLIENT_ID=Iv1.abc123
export PICI_GITHUB_CLIENT_SECRET=ghs_...
export PICI_GITHUB_TEAMS=acme/pici-users

pici
```

For a personal instance with no organization, replace `PICI_GITHUB_TEAMS` with
`PICI_GITHUB_USERS=your-github-login` (no `read:org` scope needed).

To sign in with GitLab instead, set `PICI_AUTH_PROVIDER=gitlab`,
`PICI_GITLAB_CLIENT_ID`, `PICI_GITLAB_CLIENT_SECRET`, and
`PICI_GITLAB_GROUPS` (or `PICI_GITLAB_USERS`). For a generic OpenID Connect
provider, use `PICI_AUTH_PROVIDER=oidc` with `PICI_OIDC_ISSUER`,
`PICI_OIDC_CLIENT_ID`, `PICI_OIDC_CLIENT_SECRET`, and `PICI_OIDC_GROUPS` (or
`PICI_OIDC_USERS`). An instance uses one provider at a time.

Sign in at `http://localhost:8080`, create a personal token from the Account
page, then register a project and trigger a run:

```sh
export PICI_TOKEN=pici_pat_...

pici-cli projects add demo https://github.com/acme/demo.git

pici-cli run demo build
```

See [Authentication](/guide/authentication) and [Getting started](/guide/getting-started) for full walkthroughs.
