#!/bin/sh
set -e

if [ -z "$GH_TOKEN" ]; then
  echo "GH_TOKEN is not set, skipping dependency update"
  exit 0
fi

case "$PICI_REPO_SLUG" in
  */*) ;;
  *)
    echo "could not determine repo slug from '$PICI_REPO_URL'"
    exit 0
    ;;
esac

remote="https://x-access-token:${GH_TOKEN}@github.com/${PICI_REPO_SLUG}.git"

# lookup_pr prints "<number> <state>" for the PR targeting branch $1, preferring
# an open PR over a closed one. Prints nothing when there is no PR at all.
lookup_pr() {
  gh pr list --repo "$PICI_REPO_SLUG" --head "$1" --state all \
    --json number,state \
    -q '([.[] | select(.state=="OPEN")][0] // .[0]) | select(.) | "\(.number) \(.state)"' \
    2>/dev/null || true
}

# publish_pr creates the PR for branch $1, or refreshes it in place when it is
# still open. A PR that was closed is reopened; one that was merged is left
# alone and a fresh PR is opened from the same (already pushed) branch.
publish_pr() {
  branch=$1
  title=$2
  body=$3
  info=$(lookup_pr "$branch")
  number=${info%% *}
  state=${info##* }
  if [ -z "$number" ] || [ "$state" = "MERGED" ]; then
    echo "opening PR for $branch"
    gh pr create --repo "$PICI_REPO_SLUG" --title "$title" \
      --base "$PICI_REF" --head "$branch" --body "$body" >/dev/null || true
    return
  fi
  if [ "$state" = "OPEN" ]; then
    echo "updating PR #$number for $branch"
  else
    echo "reopening PR #$number for $branch"
    gh pr reopen "$number" --repo "$PICI_REPO_SLUG" >/dev/null || true
  fi
  gh pr edit "$number" --repo "$PICI_REPO_SLUG" --title "$title" --body "$body" >/dev/null || true
}

# close_pr closes the open PR for branch $1, if any, and deletes its branch.
close_pr() {
  info=$(lookup_pr "$1")
  number=${info%% *}
  state=${info##* }
  if [ -n "$number" ] && [ "$state" = "OPEN" ]; then
    echo "closing obsolete PR #$number for $1"
    gh pr close "$number" --repo "$PICI_REPO_SLUG" --delete-branch \
      --comment "Closing: this update is no longer applicable." >/dev/null || true
  fi
}

# close_superseded closes open PRs whose head branch starts with prefix $1,
# except the branch $2 that we keep.
close_superseded() {
  prefix=$1
  keep=$2
  gh pr list --repo "$PICI_REPO_SLUG" --state open --json number,headRefName \
    -q '.[] | "\(.number) \(.headRefName)"' 2>/dev/null |
    while IFS=' ' read -r number head; do
      [ -n "$head" ] || continue
      case "$head" in
        "$prefix"*) [ "$head" != "$keep" ] || continue ;;
        *) continue ;;
      esac
      echo "closing superseded PR #$number ($head)"
      gh pr close "$number" --repo "$PICI_REPO_SLUG" --delete-branch \
        --comment "Superseded by $keep." >/dev/null || true
    done || true
}

# --- minor updates: a single, always-updated PR -----------------------------

minor_branch="deps/go-minor"

echo "checking for outdated dependencies..."
updates=$(go list -m -u -f '{{if and (not .Indirect) .Update}}{{.Path}}: {{.Version}} -> {{.Update.Version}}{{end}}' all | grep . || true)

if [ -z "$updates" ]; then
  echo "all dependencies are up to date"
  close_pr "$minor_branch"
else
  echo "updates available:"
  echo "$updates"

  git checkout -f "$PICI_REF"
  git checkout -B "$minor_branch"

  go get -u ./...
  go mod tidy

  if git diff --quiet; then
    echo "no changes after update"
    close_pr "$minor_branch"
  else
    git add go.mod go.sum
    git commit -m "chore(deps): update dependencies"
    git push --force "$remote" "$minor_branch"

    # migrate away from the old timestamped branches that used to spawn duplicate PRs
    close_superseded "deps/pici-" "$minor_branch"

    publish_pr "$minor_branch" \
      "chore(deps): update dependencies" \
      "$(printf 'Updated dependencies:\n\n%s' "$updates")"
  fi
fi

git checkout -f "$PICI_REF"

# --- major updates: one always-updated PR per dependency --------------------

echo "checking for major dependency updates..."
gomajor list -major -cached=false -json 2>/dev/null |
  jq -r 'select(.Err == null) | [.Module.Path, .Latest.Version] | @tsv' \
    >/tmp/pici-major-updates || true

while IFS="$(printf '\t')" read -r path latest; do
  if [ -z "$path" ] || [ -z "$latest" ]; then
    continue
  fi

  safe=$(printf '%s' "$path" | tr '/.' '--')
  major=$(printf '%s' "$latest" | sed 's/\..*//')
  branch="deps/major-${safe}-${major}"

  echo "major update: $path -> $latest"
  git checkout -f "$PICI_REF"
  git checkout -B "$branch"

  if ! gomajor get -cached=false "$path@latest"; then
    echo "$path: failed to apply major update"
    close_pr "$branch"
    git checkout -f "$PICI_REF"
    git branch -D "$branch" 2>/dev/null || true
    continue
  fi

  go mod tidy || true

  if git diff --quiet; then
    echo "$path: no changes after update"
    close_pr "$branch"
    git checkout -f "$PICI_REF"
    git branch -D "$branch" 2>/dev/null || true
    continue
  fi

  git add -A
  if ! git commit -m "chore(deps): upgrade $path to $latest"; then
    echo "$path: failed to commit"
    close_pr "$branch"
    git checkout -f "$PICI_REF"
    git branch -D "$branch" 2>/dev/null || true
    continue
  fi

  if ! git push --force "$remote" "$branch"; then
    echo "$path: failed to push $branch"
    close_pr "$branch"
    git checkout -f "$PICI_REF"
    git branch -D "$branch" 2>/dev/null || true
    continue
  fi

  # close PRs for the same dependency targeting an older major version
  gh pr list --repo "$PICI_REPO_SLUG" --state open --json number,headRefName \
    -q '.[] | "\(.number) \(.headRefName)"' 2>/dev/null |
    while IFS=' ' read -r number head; do
      [ -n "$head" ] || continue
      case "$head" in
        "deps/major-${safe}-"*) ;;
        *) continue ;;
      esac
      rest=${head#deps/major-"${safe}"-}
      case "$rest" in
        "" | *[!0-9]*) continue ;;
      esac
      [ "$head" != "$branch" ] || continue
      echo "closing superseded PR #$number ($head)"
      gh pr close "$number" --repo "$PICI_REPO_SLUG" --delete-branch \
        --comment "Superseded by $branch." >/dev/null || true
    done || true

  publish_pr "$branch" \
    "chore(deps): upgrade $path to $latest (major version)" \
    "Upgrades $path to $latest (major version). go.mod, go.sum and import paths were updated automatically. Review the changelog for breaking changes."

  git checkout -f "$PICI_REF"
  git branch -D "$branch" 2>/dev/null || true
done </tmp/pici-major-updates
