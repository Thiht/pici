#!/bin/sh
set -e

# DOCKER_DATA_ROOT lets a deployment keep Docker's image/build cache on the
# same persistent volume as pici's database and workspaces.
dockerd-entrypoint.sh --data-root "${DOCKER_DATA_ROOT:-/var/lib/docker}" >/proc/1/fd/1 2>/proc/1/fd/2 &

i=0
while ! docker info >/dev/null 2>&1; do
  i=$((i + 1))
  if [ "$i" -ge 60 ]; then
    echo "dockerd failed to start" >&2
    exit 1
  fi
  sleep 1
done

exec pici "$@"
