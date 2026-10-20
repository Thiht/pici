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

Open `http://localhost:8080`, sign in, create a personal token from the **Account** page, then install a workflow in your repository and run it:

```sh
export PICI_TOKEN=pici_pat_...

pici-cli projects add demo https://github.com/acme/demo.git
pici-cli run demo build
```

- Personal instance without an organization? Use `PICI_GITHUB_USERS=your-login` instead of a team.
- Signing in with GitLab or OpenID Connect? See [Authentication](/guide/authentication).
- Want the full walkthrough? Start with [Getting started](/guide/getting-started).
