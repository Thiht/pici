#!/bin/sh
set -e

repo="$PICI_REPO_SLUG"
image="ghcr.io/$(printf '%s' "$repo" | tr '[:upper:]' '[:lower:]'):$PICI_VERSION"

echo "building $image..."
task docker:build IMAGE="$image" VERSION="$PICI_VERSION"
