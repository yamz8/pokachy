#!/usr/bin/env bash
# Build distributable Linux CLI archives locally. Publishing is intentionally
# left to CI or the caller.
set -euo pipefail

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
root_dir=$(CDPATH= cd -- "$script_dir/.." && pwd)
requested_tag=${1:-}
source_version=$(sed -n 's/^\(const\|var\) version = "\([^"]*\)"/\2/p' "$root_dir/cli/cmd/pokachy/main.go")
package_version=$(sed -n 's/^[[:space:]]*"version": "\([^"]*\)",*/\1/p' "$root_dir/package.json" | head -n 1)
server_version=$(sed -n 's/^[[:space:]]*"version": "\([^"]*\)",*/\1/p' "$root_dir/server/package.json" | head -n 1)
root_manifest_version=$(sed -n 's/^[[:space:]]*"version": "\([^"]*\)",*/\1/p' "$root_dir/manifest.json" | head -n 1)
plugin_manifest_version=$(sed -n 's/^[[:space:]]*"version": "\([^"]*\)",*/\1/p' "$root_dir/plugins/omarchy/manifest.json" | head -n 1)

if [ -z "$source_version" ]; then
  echo "pokachy package: could not determine CLI version" >&2
  exit 1
fi
for candidate in "$package_version" "$server_version" "$root_manifest_version" "$plugin_manifest_version"; do
  if [ "$candidate" != "$source_version" ]; then
    echo "pokachy package: CLI, package, server, and Omarchy versions must all match $source_version" >&2
    exit 1
  fi
done
if [ -n "$requested_tag" ] && [ "${requested_tag#v}" != "$source_version" ]; then
  echo "pokachy package: tag $requested_tag does not match CLI version $source_version" >&2
  exit 1
fi

dist_dir=${POKACHY_DIST_DIR:-"$root_dir/dist"}
mkdir -p "$dist_dir"
dist_dir=$(CDPATH= cd -- "$dist_dir" && pwd)
if [ "$dist_dir" = / ] || [ "$dist_dir" = "$root_dir" ]; then
  echo "pokachy package: refusing unsafe distribution directory: $dist_dir" >&2
  exit 2
fi
work_dir=$(mktemp -d "${TMPDIR:-/tmp}/pokachy-release.XXXXXX")
trap 'rm -rf "$work_dir"' EXIT
build_flags=()
# Go can stamp a string variable with -X, but the current CLI deliberately
# declares its version as a constant. Keep this conditional for that future
# compatible shape without passing an invalid linker flag today.
if grep -q '^var version ' "$root_dir/cli/cmd/pokachy/main.go"; then
  build_flags=(-ldflags "-X main.version=$source_version")
fi

for arch in amd64 arm64; do
  name="pokachy_${source_version}_linux_${arch}"
  archive_name="pokachy_linux_${arch}.tar.gz"
  stage="$work_dir/$name"
  mkdir -p "$stage/bin" "$stage/packaging/systemd" "$stage/scripts" "$stage/plugins"
  (
    cd "$root_dir/cli"
    GOOS=linux GOARCH="$arch" CGO_ENABLED=0 go build -trimpath "${build_flags[@]}" -o "$stage/bin/pokachy" ./cmd/pokachy
  )
  cp "$root_dir/packaging/systemd/pokachy.service" "$stage/packaging/systemd/pokachy.service"
  cp "$root_dir/scripts/install.sh" "$stage/scripts/install.sh"
  cp "$root_dir/manifest.json" "$stage/manifest.json"
  cp "$root_dir/LICENSE" "$root_dir/README.md" "$stage/"
  cp -R "$root_dir/plugins/omarchy" "$stage/plugins/omarchy"
  mkdir -p "$stage/assets"
  cp -R "$root_dir/assets/brand" "$stage/assets/brand"
  tar -C "$work_dir" -czf "$dist_dir/$archive_name" "$name"
done

plugin_destination="$dist_dir/pokachy-omarchy"
"$root_dir/scripts/export-omarchy-plugin.sh" "$plugin_destination"
cp "$root_dir/server/public/install.sh" "$dist_dir/install.sh"

(
  cd "$dist_dir"
  sha256sum "pokachy_linux_amd64.tar.gz" "pokachy_linux_arm64.tar.gz" "install.sh" > SHA256SUMS
)
printf 'Release archives written to %s\n' "$dist_dir"
