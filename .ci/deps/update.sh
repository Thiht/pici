#!/bin/sh
set -e

if [ -z "$GH_TOKEN" ]; then
  echo "GH_TOKEN is not set, skipping dependency update"
  exit 0
fi

url="${PICI_REPO_URL%.git}"
case "$url" in
  *github.com/*) owner_repo="${url#*github.com/}" ;;
  *github.com:*) owner_repo="${url#*github.com:}" ;;
  *) echo "unsupported repo URL: $url"; exit 0 ;;
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
git config user.email "pici@users.noreply.github.com"
git config user.name "pici"
git checkout -b "$branch"
git add go.mod go.sum
git commit -m "chore(deps): update dependencies"
git push "https://x-access-token:${GH_TOKEN}@github.com/${owner_repo}.git" "$branch"

body=$(printf 'Updated dependencies:\n\n%s' "$updates")
payload=$(jq -n \
  --arg title "chore(deps): update dependencies" \
  --arg head "$branch" \
  --arg base "$PICI_REF" \
  --arg body "$body" \
  '{title: $title, head: $head, base: $base, body: $body}')

curl -sS -X POST \
  -H "Authorization: Bearer ${GH_TOKEN}" \
  -H "Accept: application/vnd.github+json" \
  -H "X-GitHub-Api-Version: 2022-11-28" \
  -d "$payload" \
  "https://api.github.com/repos/${owner_repo}/pulls" | jq -r '.html_url // .message'
