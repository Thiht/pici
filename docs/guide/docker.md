# Running with Docker

pici ships with a self-contained [Docker-in-Docker](https://hub.docker.com/_/docker) image: it starts its own Docker daemon and runs pici on top of it.

For a hosted one-command deployment, see [Fly.io](/guide/fly).

## docker compose

`PICI_SECRET_KEY` and `PICI_PUBLIC_URL` are required, plus the OAuth settings for one provider (see [Authentication](/guide/authentication)). The server refuses to start without them.

```sh
export PICI_SECRET_KEY=$(openssl rand -hex 32)
export PICI_PUBLIC_URL=https://pici.example.com
export PICI_GITHUB_CLIENT_ID=Iv1.abc123
export PICI_GITHUB_CLIENT_SECRET=ghs_...
export PICI_GITHUB_TEAMS=acme/pici-users

docker compose -f docker/docker-compose.yml up --build
```

Use `PICI_GITHUB_USERS=your-github-login` instead of `PICI_GITHUB_TEAMS` for a personal instance. For GitLab or OIDC, set `PICI_AUTH_PROVIDER` and the matching `PICI_GITLAB_*` or `PICI_OIDC_*` variables.

Create the OAuth app with callback URL `${PICI_PUBLIC_URL}/auth/<provider>/callback`. The server is reachable at the public URL (locally, `http://localhost:8080`), and data (SQLite database + workspaces) lives in the `pici-data` volume.

## Plain docker

```sh
docker build -f docker/Dockerfile -t pici:dev .

docker run -d --privileged \
  -p 8080:8080 \
  -v pici-data:/data \
  -e PICI_DB_DSN=/data/pici.db \
  -e PICI_WORKSPACE_DIR=/data/workspaces \
  -e PICI_SECRET_KEY=$(openssl rand -hex 32) \
  -e PICI_PUBLIC_URL=https://pici.example.com \
  -e PICI_GITHUB_CLIENT_ID=Iv1.abc123 \
  -e PICI_GITHUB_CLIENT_SECRET=ghs_... \
  -e PICI_GITHUB_TEAMS=acme/pici-users \
  pici:dev
```

As with compose, swap the GitHub variables for the provider you use.

## Notes

- `--privileged` (or `privileged: true`) is required for nested Docker.
- The image starts `dockerd` in the background, then runs pici; pici talks to it over the local Unix socket.
- Set `PICI_LOG_FORMAT=json` to emit structured JSON logs (for example to feed Loki/Grafana) instead of the default text format.
- On `docker stop`, pici drains in-flight steps gracefully (see `-shutdown-timeout`). Keep `stop_grace_period` (compose: `2m`) larger than your longest step.
- The workspace and database live under `/data`, so mount a volume there to persist state and caches.
- Put pici behind an HTTPS reverse proxy: the OAuth callback and `Secure` session cookies assume a stable HTTPS public URL.

## Alternative: sidecar DinD

To keep pici unprivileged, run `docker:dind` as a separate service and point pici at it:

```yaml
services:
  dind:
    image: docker:dind
    privileged: true
  pici:
    image: your-pici-image
    environment:
      DOCKER_HOST: tcp://dind:2375
```

The bundled image starts its own daemon, so this setup needs a slim runtime image containing only the `pici` binary.
