#!/bin/sh
set -e

if [ -z "$GH_TOKEN" ]; then
  echo "GH_TOKEN is not set, skipping release"
  exit 1
fi

repo="$PICI_REPO_SLUG"
case "$PICI_VERSION" in
  v[0-9]*.[0-9]*.[0-9]*) ;;
  *) echo "ref '$PICI_VERSION' is not a release tag (vX.Y.Z), skipping"; exit 0 ;;
esac

if [ -z "$repo" ]; then
  echo "could not determine repo slug from '$PICI_REPO_URL'"
  exit 1
fi

image="ghcr.io/$(printf '%s' "$repo" | tr '[:upper:]' '[:lower:]'):$PICI_VERSION"

echo "$GH_TOKEN" | docker login ghcr.io -u "${GHCR_USER:-${repo%%/*}}" --password-stdin

echo "pushing $image..."
docker push "$image"

echo "creating release $PICI_VERSION on $repo..."
gh release create "$PICI_VERSION" \
  --repo "$repo" \
  --title "$PICI_VERSION" \
  --generate-notes \
  dist/*

body="$(gh release view "$PICI_VERSION" --repo "$repo" --json body -q .body)"
gh release edit "$PICI_VERSION" \
  --repo "$repo" \
  --notes "${body}

## Docker image

\`${image}\`"
