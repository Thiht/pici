#!/bin/sh
set -e

if [ -z "$GH_TOKEN" ]; then
  echo "GH_TOKEN is not set, skipping release"
  exit 1
fi

version="$PICI_VERSION"
case "$version" in
  v[0-9]*.[0-9]*.[0-9]*) ;;
  *) echo "ref '$version' is not a release tag (vX.Y.Z), skipping"; exit 0 ;;
esac

if [ -z "$PICI_REPO_SLUG" ]; then
  echo "could not determine repo slug from '$PICI_REPO_URL'"
  exit 1
fi

echo "building version $version..."
mkdir -p dist
go build -trimpath -ldflags "-s -w -X main.buildVersion=${version}" -o dist/pici-linux-amd64 ./cmd/pici
go build -trimpath -ldflags "-s -w -X main.buildVersion=${version}" -o dist/pici-cli-linux-amd64 ./cmd/pici-cli

echo "creating release $version on $PICI_REPO_SLUG..."
gh release create "$version" \
  --repo "$PICI_REPO_SLUG" \
  --title "$version" \
  --generate-notes \
  dist/pici-linux-amd64 \
  dist/pici-cli-linux-amd64
