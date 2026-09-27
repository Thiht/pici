#!/bin/sh
set -e

if [ -z "$GH_TOKEN" ]; then
  echo "GH_TOKEN is not set, skipping dependency update"
  exit 0
fi

case "$PICI_REPO_SLUG" in
  */*) ;;
  *) echo "could not determine repo slug from '$PICI_REPO_URL'"; exit 0 ;;
esac

echo "checking for outdated dependencies..."
updates=$(go list -m -u -f '{{if and (not .Indirect) .Update}}{{.Path}}: {{.Version}} -> {{.Update.Version}}{{end}}' all | grep . || true)

if [ -z "$updates" ]; then
  echo "all dependencies are up to date"
  exit 0
fi

echo "updates available:"
echo "$updates"

go get -u ./...
go mod tidy

if git diff --quiet; then
  echo "no changes after update"
  exit 0
fi

branch="deps/pici-$(date +%Y%m%d%H%M%S)"
git checkout -b "$branch"
git add go.mod go.sum
git commit -m "chore(deps): update dependencies"
git push "https://x-access-token:${GH_TOKEN}@github.com/${PICI_REPO_SLUG}.git" "$branch"

gh pr create \
  --repo "$PICI_REPO_SLUG" \
  --title "chore(deps): update dependencies" \
  --base "$PICI_REF" \
  --head "$branch" \
  --body "$(printf 'Updated dependencies:\n\n%s' "$updates")"
