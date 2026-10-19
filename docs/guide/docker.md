# Running with Docker

pici ships with a self-contained [Docker-in-Docker](https://hub.docker.com/_/docker) image: it starts its own Docker daemon and runs pici on top of it.

## docker-compose

`PICI_SECRET_KEY`, `PICI_PUBLIC_URL` and the GitHub OAuth settings are
**required** — the compose file fails fast if they are unset. pici also
requires at least one of `PICI_GITHUB_TEAMS` or `PICI_GITHUB_USERS`.

```sh
export PICI_SECRET_KEY=$(openssl rand -hex 32)
export PICI_PUBLIC_URL=https://pici.example.com
export PICI_GITHUB_CLIENT_ID=Iv1.abc123
export PICI_GITHUB_CLIENT_SECRET=ghs_...
export PICI_GITHUB_TEAMS=acme/pici-users

docker compose -f docker/docker-compose.yml up --build
```

For a personal instance with no organization, use
`PICI_GITHUB_USERS=your-github-login` instead of `PICI_GITHUB_TEAMS`.

Create the OAuth App with callback URL
`${PICI_PUBLIC_URL}/auth/github/callback` (see
[Authentication](/guide/authentication)). The server is then reachable at the
public URL; locally, `http://localhost:8080`. Data (SQLite DB + workspaces)
lives in the `pici-data` volume.

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

Use `-e PICI_GITHUB_USERS=your-github-login` instead of
`PICI_GITHUB_TEAMS` for a personal instance.

## Notes

- `--privileged` (or `privileged: true`) is required for nested Docker.
- The image starts `dockerd` in the background, then runs pici; pici talks to it over the local Unix socket.
- On `docker stop`, pici drains in-flight steps gracefully (see `-shutdown-timeout`); keep `stop_grace_period` (compose: `2m`) larger than your longest step.
- The workspace and database are stored in `/data`, so mount a volume there to persist state and caches.
- Put pici behind an HTTPS reverse proxy: the OAuth callback and `Secure`
  session cookies assume a stable HTTPS public URL.

## Alternative: sidecar DinD

If you prefer to keep pici unprivileged, run `docker:dind` as a separate service and point pici at it:

```yaml
services:
  dind:
    image: docker:dind
    privileged: true
  pici:
    build:
      context: ..
      dockerfile: docker/Dockerfile
    environment:
      DOCKER_HOST: tcp://dind:2375
```

In that case use the `docker/Dockerfile`'s build stage only, or a slim runtime image (Alpine) with the binary.
