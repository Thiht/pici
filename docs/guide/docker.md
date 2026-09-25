# Running with Docker

pici ships with a self-contained [Docker-in-Docker](https://hub.docker.com/_/docker) image: it starts its own Docker daemon and runs pici on top of it.

## docker-compose

`PICI_SECRET_KEY` and `PICI_API_TOKEN` are **required** — the compose file fails fast if they are unset.

```sh
export PICI_SECRET_KEY=$(openssl rand -hex 32)
export PICI_API_TOKEN=$(openssl rand -hex 24)

docker compose -f docker/docker-compose.yml up --build
```

The server is then reachable at `http://localhost:8080`. Data (SQLite DB + workspaces) lives in the `pici-data` volume.

## Plain docker

```sh
docker build -f docker/Dockerfile -t pici:dev .

docker run -d --privileged \
  -p 8080:8080 \
  -v pici-data:/data \
  -e PICI_DB_DSN=/data/pici.db \
  -e PICI_WORKSPACE_DIR=/data/workspaces \
  -e PICI_SECRET_KEY=$(openssl rand -hex 32) \
  -e PICI_API_TOKEN=$(openssl rand -hex 24) \
  pici:dev
```

## Notes

- `--privileged` (or `privileged: true`) is required for nested Docker.
- The image starts `dockerd` in the background, then runs pici; pici talks to it over the local Unix socket.
- On `docker stop`, pici drains in-flight steps gracefully (see `-shutdown-timeout`); keep `stop_grace_period` (compose: `2m`) larger than your longest step.
- The workspace and database are stored in `/data`, so mount a volume there to persist state and caches.

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
