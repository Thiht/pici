#!/bin/sh
# Runs the store tests against the throwaway Postgres declared in
# docker/docker-compose.test.yml, on the fixed port they expect.
set -e

if ! command -v docker >/dev/null 2>&1; then
  echo "test-postgres: docker is required" >&2
  exit 1
fi

file=docker/docker-compose.test.yml
# A project name per run keeps concurrent runs, and their leftovers, apart.
export COMPOSE_PROJECT_NAME="pici-test-$$"

cleanup() {
  docker compose -f "$file" down --volumes --remove-orphans >/dev/null 2>&1 || true
}
trap cleanup EXIT INT TERM

docker compose -f "$file" up -d --wait

# A pici step runs in its own container, where 127.0.0.1 is not the host: point
# the tests at the database container instead of the published port.
if [ -f /.dockerenv ]; then
  container=$(docker compose -f "$file" ps -q postgres)
  PGHOST=$(docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "$container")
  export PGHOST
fi

go test -tags=postgres ./internal/stores/...
