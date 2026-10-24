# Configuration

Configuration is flags-first ([ff](https://github.com/peterbourgon/ff)), with three sources in priority order: **flags**, then **environment variables** (`PICI_` prefix), then a **JSON config file** (`-config`).

## Settings

| Flag                       | Env                            | Default                               | Description                                                     |
| -------------------------- | ------------------------------ | ------------------------------------- | --------------------------------------------------------------- |
| `-http-addr`               | `PICI_HTTP_ADDR`               | `:8080`                               | HTTP listen address                                             |
| `-db-driver`               | `PICI_DB_DRIVER`               | `sqlite`                              | `sqlite` or `postgres`                                          |
| `-db-dsn`                  | `PICI_DB_DSN`                  | `pici.db`                             | SQLite path or Postgres DSN                                     |
| `-workspace-dir`           | `PICI_WORKSPACE_DIR`           | `~/.pici/workspaces`                  | clones and logs                                                 |
| `-repo-mount-path`         | `PICI_REPO_MOUNT_PATH`         | `/workspace`                          | in-container mount point (must not overlap `/pici/cache`)       |
| `-concurrency`             | `PICI_CONCURRENCY`             | `4`                                   | max concurrent executions                                       |
| `-step-timeout`            | `PICI_STEP_TIMEOUT`            | `30m`                                 | default step timeout                                            |
| `-max-snapshot-size`       | `PICI_MAX_SNAPSHOT_SIZE`       | `536870912` (512 MiB)                 | maximum size of an uploaded local snapshot                      |
| `-log-format`              | `PICI_LOG_FORMAT`              | `text`                                | log output format: `text` or `json` (for Loki/Grafana, ...)     |
| `-secret-key`              | `PICI_SECRET_KEY`              | —                                     | **required** — 32-byte key (hex/base64) for encrypting secrets  |
| `-public-url`              | `PICI_PUBLIC_URL`              | —                                     | **required** — public base URL, e.g. `https://pici.example.com` |
| `-auth-provider`           | `PICI_AUTH_PROVIDER`           | `github`                              | sign-in provider: `github`, `gitlab` or `oidc`                  |
| `-github-client-id`        | `PICI_GITHUB_CLIENT_ID`        | —                                     | GitHub OAuth App client id                                      |
| `-github-client-secret`    | `PICI_GITHUB_CLIENT_SECRET`    | —                                     | GitHub OAuth App client secret                                  |
| `-github-teams`            | `PICI_GITHUB_TEAMS`            | —                                     | comma-separated teams allowed to sign in (`org/team`)           |
| `-github-users`            | `PICI_GITHUB_USERS`            | —                                     | comma-separated GitHub logins allowed to sign in                |
| `-gitlab-url`              | `PICI_GITLAB_URL`              | `https://gitlab.com`                  | GitLab instance base URL                                        |
| `-gitlab-client-id`        | `PICI_GITLAB_CLIENT_ID`        | —                                     | GitLab OAuth application id                                     |
| `-gitlab-client-secret`    | `PICI_GITLAB_CLIENT_SECRET`    | —                                     | GitLab OAuth application secret                                 |
| `-gitlab-groups`           | `PICI_GITLAB_GROUPS`           | —                                     | comma-separated groups allowed to sign in (`group/path`)        |
| `-gitlab-users`            | `PICI_GITLAB_USERS`            | —                                     | comma-separated GitLab usernames allowed to sign in             |
| `-oidc-issuer`             | `PICI_OIDC_ISSUER`             | —                                     | OpenID Connect issuer URL                                       |
| `-oidc-client-id`          | `PICI_OIDC_CLIENT_ID`          | —                                     | OIDC client id                                                  |
| `-oidc-client-secret`      | `PICI_OIDC_CLIENT_SECRET`      | —                                     | OIDC client secret                                              |
| `-oidc-groups`             | `PICI_OIDC_GROUPS`             | —                                     | comma-separated groups allowed to sign in (`groups` claim)      |
| `-oidc-users`              | `PICI_OIDC_USERS`              | —                                     | comma-separated usernames or emails allowed to sign in          |
| `-oidc-scopes`             | `PICI_OIDC_SCOPES`             | `openid profile email offline_access` | OAuth scopes for the OIDC provider                              |
| `-oidc-name`               | `PICI_OIDC_NAME`               | `SSO`                                 | display name on the sign-in page                                |
| `-session-ttl`             | `PICI_SESSION_TTL`             | `720h`                                | browser session lifetime (sliding)                              |
| `-gc-interval`             | `PICI_GC_INTERVAL`             | `10m`                                 | garbage collection interval                                     |
| `-gc-keep`                 | `PICI_GC_KEEP`                 | `24h`                                 | keep finished workspaces/logs for this long                     |
| `-scheduler-interval`      | `PICI_SCHEDULER_INTERVAL`      | `1m`                                  | scheduled run polling interval                                  |
| `-shutdown-timeout`        | `PICI_SHUTDOWN_TIMEOUT`        | `30s`                                 | grace period for in-flight steps on shutdown                    |
| `-sandbox-image`           | `PICI_SANDBOX_IMAGE`           | `docker:28.5.2-dind`                  | throwaway daemon image used by `docker: true` workflows         |
| `-registry-cache-max-size` | `PICI_REGISTRY_CACHE_MAX_SIZE` | `10737418240` (10 GiB)                | image cache size before the collector purges it                 |
| `-config`                  | `PICI_CONFIG`                  | —                                     | path to a JSON config file                                      |

Docker is reached via the standard Docker environment (`DOCKER_HOST`, `~/.docker`, ...).

## Requirements

`PICI_SECRET_KEY` and `PICI_PUBLIC_URL` are required. The public URL must be an origin (no path, query, or credentials) and use HTTPS, except for localhost.

For the selected provider, set the client credentials and at least one of the groups/teams or users allowlists. Settings for other providers are ignored. See [Authentication](/guide/authentication) for the OAuth setup.

## Config file

Keys are flag names. Arguments passed on the command line take precedence.

```json
{
  "http-addr": ":9090",
  "db-driver": "postgres",
  "db-dsn": "postgres://user:pass@localhost:5432/pici?sslmode=disable",
  "concurrency": 8,
  "secret-key": "<32-byte hex>",
  "public-url": "https://pici.example.com",
  "github-client-id": "Iv1.abc123",
  "github-client-secret": "ghs_...",
  "github-users": "Thiht"
}
```

```sh
pici -config /etc/pici.json
```

## Runtime behavior

- **Garbage collection** runs every `-gc-interval`: it removes finished workspaces and logs older than `-gc-keep`, prunes unused Docker images built by pici (labeled `pici=1`), removes the sandboxes of executions that are no longer running, and purges the shared image cache (workflow images and Docker Hub pulls) once it exceeds `-registry-cache-max-size`.
- **Docker sandboxes**: `-sandbox-image` selects the image of the throwaway daemon started for workflows that declare a `docker: true` step (default `docker:28.5.2-dind`). Each such execution pays one daemon start, one extra privileged container, and pulling its workflow image from pici's local registry.
- **Crash recovery**: on startup, pici requeues executions left in `running` state, so no run is lost.
- **Graceful shutdown**: on `SIGTERM`/`SIGINT`, pici stops accepting new work, waits up to `-shutdown-timeout` for in-flight steps, then cancels anything still running.
- **Multi-instance**: the queue is database-backed, so multiple instances can share a Postgres database and consume it concurrently. Local snapshot uploads also require the API and the worker to share the same workspace directory (single node, or a shared volume).
