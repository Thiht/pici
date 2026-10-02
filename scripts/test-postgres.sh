#!/bin/sh
# Runs the store tests against the throwaway Postgres declared in
# docker/docker-compose.test.yml.
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

# A pici step runs in its own container: a published port would land on the
# host, out of the step's reach, so the database is reached by its container IP
# instead.
if [ -f /.dockerenv ]; then
  container=$(docker compose -f "$file" ps -q postgres)
  host=$(docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "$container")
  port=5432
else
  host=127.0.0.1
  port=$(docker compose -f "$file" port postgres 5432 | head -1 | sed 's/.*://')
fi

echo "test-postgres: postgres ready on $host:$port"
PICI_TEST_POSTGRES_DSN="postgres://postgres:pici@$host:$port/pici_test?sslmode=disable" \
  go test ./internal/stores/...
