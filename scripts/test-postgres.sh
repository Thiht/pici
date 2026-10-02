#!/bin/sh
# Runs the store tests against a throwaway Postgres.
set -e

if ! command -v docker >/dev/null 2>&1; then
  echo "test-postgres: docker is required" >&2
  exit 1
fi

name="pici-test-postgres-$$"
cleanup() {
  docker rm -f "$name" >/dev/null 2>&1 || true
}
trap cleanup EXIT INT TERM

# A pici step runs in its own container: a published port would land on the host,
# out of the step's reach, so the database is reached by its bridge IP instead.
if [ -f /.dockerenv ]; then
  set --
else
  set -- -p 127.0.0.1::5432
fi

docker run -d --rm --name "$name" "$@" \
  -e POSTGRES_PASSWORD=pici -e POSTGRES_DB=pici_test \
  postgres:16-alpine >/dev/null

attempts=0
until docker exec "$name" pg_isready -U postgres >/dev/null 2>&1; do
  attempts=$((attempts + 1))
  if [ "$attempts" -gt 100 ]; then
    echo "test-postgres: postgres did not become ready" >&2
    docker logs "$name" 2>&1 | tail -20 >&2
    exit 1
  fi
  sleep 0.3
done

if [ "$#" -eq 0 ]; then
  host=$(docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "$name")
  port=5432
else
  host=127.0.0.1
  port=$(docker port "$name" 5432/tcp | head -1 | sed 's/.*://')
fi

echo "test-postgres: postgres ready on $host:$port"
PICI_TEST_POSTGRES_DSN="postgres://postgres:pici@$host:$port/pici_test?sslmode=disable" \
  go test ./internal/stores/...
