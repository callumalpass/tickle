#!/usr/bin/env bash
set -euo pipefail

ROOT=$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
VERSION_INPUT=${1:-$(tr -d '[:space:]' < "$ROOT/VERSION")}
VERSION=${VERSION_INPUT#v}
DIST="$ROOT/dist"
SKILL_SRC="$ROOT/skills/tickle"

platforms=(
  "linux amd64"
  "linux arm64"
  "darwin amd64"
  "darwin arm64"
  "windows amd64"
)

if ! command -v zip >/dev/null 2>&1; then
  echo "zip is required to build skill bundles" >&2
  exit 1
fi

mkdir -p "$DIST"
tmp=$(mktemp -d)
cleanup() {
  rm -rf "$tmp"
}
trap cleanup EXIT

build_binary() {
  local goos=$1
  local goarch=$2
  local suffix=$3
  local output="$DIST/tickle-$goos-$goarch$suffix"

  echo "building $output"
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
    go build -trimpath -ldflags "-s -w -X github.com/callumalpass/tickle/internal/version.Version=$VERSION" -o "$output" ./cmd/tickle
}

write_manifest() {
  local path=$1
  local platform=$2
  local binary=$3

  cat > "$path" <<JSON
{
  "version": "$VERSION",
  "platform": "$platform",
  "binary": "$binary",
  "schema": 1
}
JSON
}

package_skill() {
  local goos=$1
  local goarch=$2
  local suffix=$3
  local platform="$goos-$goarch"
  local binary_name="tickle$suffix"
  local binary_path="$DIST/tickle-$platform$suffix"
  local bundle_root="$tmp/$platform/tickle"
  local zip_tmp="$tmp/tickle-skill-$platform.zip"
  local zip_out="$DIST/tickle-skill-$platform.zip"

  mkdir -p "$bundle_root/scripts/bin"
  cp -R "$SKILL_SRC/." "$bundle_root/"
  cp "$binary_path" "$bundle_root/scripts/bin/$binary_name"
  chmod +x "$bundle_root/scripts/tickle" || true
  if [ "$goos" != "windows" ]; then
    chmod +x "$bundle_root/scripts/bin/$binary_name"
  fi

  printf '%s\n' "$VERSION" > "$bundle_root/VERSION"
  write_manifest "$bundle_root/tickle.json" "$platform" "scripts/bin/$binary_name"

  echo "packaging $zip_out"
  (
    cd "$tmp/$platform"
    zip -qr "$zip_tmp" tickle
  )
  mv "$zip_tmp" "$zip_out"
}

for platform in "${platforms[@]}"; do
  read -r goos goarch <<< "$platform"
  suffix=""
  if [ "$goos" = "windows" ]; then
    suffix=".exe"
  fi
  build_binary "$goos" "$goarch" "$suffix"
  package_skill "$goos" "$goarch" "$suffix"
done

(
  cd "$DIST"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum tickle-* > SHA256SUMS
  else
    shasum -a 256 tickle-* > SHA256SUMS
  fi
)

echo "release artifacts written to $DIST"
