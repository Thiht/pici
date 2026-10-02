#!/bin/sh
set -e

if ! command -v task >/dev/null 2>&1; then
  echo "pre-push: 'task' not found, skipping checks" >&2
  exit 0
fi

# Stash uncommitted and untracked changes so the checks only run on what is
# being pushed. Ignored files (node_modules, venvs, ...) are left in place.
stashed=0
if [ -n "$(git status --porcelain)" ]; then
  echo "pre-push: stashing local changes"
  git stash push --include-untracked --quiet
  stashed=1
fi

restore() {
  status=$?
  if [ "$stashed" -eq 1 ]; then
    if ! git stash pop --quiet; then
      echo "pre-push: failed to restore local changes, run 'git stash pop'" >&2
      exit 1
    fi
  fi
  exit "$status"
}
trap restore EXIT

echo "pre-push: task lint"
task lint

echo "pre-push: task fmt:check"
task fmt:check

echo "pre-push: task test"
task test
