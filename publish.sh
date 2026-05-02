#!/usr/bin/env bash
set -euo pipefail

VERSION="${1:?Usage: ./publish.sh <version> (e.g. v0.1.0)}"

if [[ ! "$VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "Error: version must match vX.Y.Z (e.g. v0.1.0)"
  exit 1
fi

DIST="dist"
rm -rf "$DIST"
mkdir -p "$DIST"

echo "Building $VERSION..."

LDFLAGS="-s -w -X main.version=${VERSION}"
GOOS=darwin  GOARCH=arm64 go build -ldflags="$LDFLAGS" -o "$DIST/clip-darwin-arm64"   .
GOOS=linux   GOARCH=amd64 go build -ldflags="$LDFLAGS" -o "$DIST/clip-linux-amd64"    .

echo "Creating GitHub release $VERSION..."

gh release create "$VERSION" \
  "$DIST/clip-darwin-arm64" \
  "$DIST/clip-linux-amd64" \
  --title "$VERSION" \
  --generate-notes

echo "Done: https://github.com/magicmark/clip/releases/tag/$VERSION"
