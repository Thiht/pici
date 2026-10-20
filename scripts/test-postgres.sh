#!/bin/sh
# Runs the store tests against the throwaway Postgres declared in
# docker/docker-compose.test.yml, on the fixed port they expect.
set -e

if ! command -v docker >/dev/null 2>&1; then
  echo "test-postgres: docker is required" >&2
  exit 1
fi

file=docker/docker-compose.test.yml
export COMPOSE_PROJECT_NAME="pici-test-${PICI_EXECUTION_ID:-$$}"

cleanup() {
  docker compose -f "$file" down --volumes --remove-orphans >/dev/null 2>&1 || true
}
trap cleanup EXIT INT TERM

stale_after=$(($(date +%s) - 900))
if date -u -d @0 +%s >/dev/null 2>&1; then
  cutoff=$(date -u -d "@$stale_after" +%Y-%m-%dT%H:%M:%SZ)
else
  cutoff=$(date -u -r "$stale_after" +%Y-%m-%dT%H:%M:%SZ)
fi
docker ps -a --filter label=pici.test=1 --format '{{.ID}}' | while read -r id; do
  created=$(docker inspect -f '{{.Created}}' "$id")
  if awk -v created="$created" -v cutoff="$cutoff" 'BEGIN { exit created >= cutoff }'; then
    docker inspect -f '{{index .Config.Labels "com.docker.compose.project"}}' "$id"
  fi
done | sort -u | while read -r project; do
  docker compose -p "$project" down --volumes --remove-orphans >/dev/null 2>&1 || true
done

docker compose -f "$file" up -d --wait

# A pici step runs in its own container, where 127.0.0.1 is not the host: point
# the tests at the database container instead of the published port.
if [ -f /.dockerenv ]; then
  container=$(docker compose -f "$file" ps -q postgres)
  PGHOST=$(docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "$container")
  PGPORT=55432
else
  PGPORT=$(docker compose -f "$file" port postgres 55432 | sed 's/.*://')
fi
export PGHOST PGPORT

mkdir -p dist
go test -tags=postgres -covermode=atomic -coverprofile=dist/cover-postgres.out ./internal/stores/...
go tool cover -func=dist/cover-postgres.out | tail -1
