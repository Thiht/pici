# Configuration

Configuration is flags-first ([ff](https://github.com/peterbourgon/ff)), with three sources in priority order: **flags**, then **environment variables** (`PICI_` prefix), then a **JSON config file** (`-config`).

| Flag | Env | Default | Description |
|---|---|---|---|
| `-http-addr` | `PICI_HTTP_ADDR` | `:8080` | HTTP listen address |
| `-db-driver` | `PICI_DB_DRIVER` | `sqlite` | `sqlite` or `postgres` |
| `-db-dsn` | `PICI_DB_DSN` | `pici.db` | SQLite path or Postgres DSN |
| `-workspace-dir` | `PICI_WORKSPACE_DIR` | `~/.pici/workspaces` | clones and logs |
| `-repo-mount-path` | `PICI_REPO_MOUNT_PATH` | `/workspace` | in-container mount point |
| `-concurrency` | `PICI_CONCURRENCY` | `4` | max concurrent executions |
| `-step-timeout` | `PICI_STEP_TIMEOUT` | `30m` | default step timeout |
| `-secret-key` | `PICI_SECRET_KEY` | — | **required** — 32-byte key (hex/base64) for encrypting secrets |
| `-api-token` | `PICI_API_TOKEN` | — | **required** — API token required to call the API |
| `-public-url` | `PICI_PUBLIC_URL` | — | base URL for links in GitHub check runs |
| `-gc-interval` | `PICI_GC_INTERVAL` | `10m` | garbage collection interval |
| `-gc-keep` | `PICI_GC_KEEP` | `24h` | keep finished workspaces/logs for this long |
| `-scheduler-interval` | `PICI_SCHEDULER_INTERVAL` | `1m` | scheduled build polling interval |
| `-shutdown-timeout` | `PICI_SHUTDOWN_TIMEOUT` | `30s` | grace period for in-flight steps on shutdown |
| `-config` | `PICI_CONFIG` | — | path to a JSON config file |

## Example config file

```json
{
  "http-addr": ":9090",
  "db-driver": "postgres",
  "db-dsn": "postgres://user:pass@localhost:5432/pici?sslmode=disable",
  "concurrency": 8,
  "secret-key": "<32-byte hex>",
  "api-token": "<random token>"
}
```

```sh
pici -config /etc/pici.json
```

## Garbage collection

pici periodically:

- removes finished workspaces and logs older than `-gc-keep`
- prunes unused Docker images built by pici (labeled `pici=1`)

## Crash recovery

On startup, pici requeues any execution left in `running` state (e.g. after a crash), so no build is lost.

## Graceful shutdown

On `SIGTERM`/`SIGINT`, pici stops accepting new work, waits up to `-shutdown-timeout` for in-flight steps to finish, then force-cancels anything still running.

## Multi-instance

Because the queue is database-backed, multiple instances can share a Postgres database and consume the queue concurrently.
