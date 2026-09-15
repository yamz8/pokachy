#!/bin/sh
# Download and install the latest verified Pokachy Linux release. The packaged
# installer performs the actual local installation and never uses sudo.
set -eu

download_base=${POKACHY_DOWNLOAD_BASE_URL:-https://github.com/yamz8/pokachy/releases/latest/download}
machine=${POKACHY_UNAME_MACHINE:-$(uname -m)}
mode=${1:-}
if [ -n "$mode" ] && [ "$mode" != --update ]; then
  echo "pokachy install: unknown option: $mode" >&2
  exit 2
fi

case "$machine" in
  x86_64|amd64) architecture=amd64 ;;
  aarch64|arm64) architecture=arm64 ;;
  *)
    echo "pokachy install: unsupported architecture: $machine" >&2
    echo "Supported architectures: amd64 and arm64." >&2
    exit 1
    ;;
esac

for command in curl sha256sum tar mktemp bash; do
  if ! command -v "$command" >/dev/null 2>&1; then
    echo "pokachy install: required command not found: $command" >&2
    exit 1
  fi
done

work_dir=$(mktemp -d "${TMPDIR:-/tmp}/pokachy-install.XXXXXX")
trap 'rm -rf "$work_dir"' EXIT HUP INT TERM

archive_name="pokachy_linux_${architecture}.tar.gz"
archive="$work_dir/$archive_name"
checksums="$work_dir/SHA256SUMS"

echo "Downloading Pokachy for Linux $architecture..."
curl --proto '=https' --tlsv1.2 -fsSL "$download_base/$archive_name" -o "$archive"
curl --proto '=https' --tlsv1.2 -fsSL "$download_base/SHA256SUMS" -o "$checksums"

checksum_line=$(awk -v name="$archive_name" '$2 == name { print; found = 1 } END { if (!found) exit 1 }' "$checksums") || {
  echo "pokachy install: the release checksum does not list $archive_name" >&2
  exit 1
}
case "$checksum_line" in
  [0-9a-fA-F][0-9a-fA-F]*) ;;
  *) echo "pokachy install: invalid release checksum" >&2; exit 1 ;;
esac

(
  cd "$work_dir"
  printf '%s\n' "$checksum_line" | sha256sum -c -
)

contents="$work_dir/archive-contents"
tar -tzf "$archive" > "$contents"
release_root=$(sed -n '1{s|/.*||;p;}' "$contents")
case "$release_root" in
  pokachy_*_linux_"$architecture") ;;
  *) echo "pokachy install: release archive has an unexpected root" >&2; exit 1 ;;
esac
while IFS= read -r member; do
  case "$member" in
    "$release_root"|"$release_root"/*) ;;
    ""|/*|../*|*/../*|*/..|*)
      echo "pokachy install: unsafe archive path: $member" >&2
      exit 1
      ;;
  esac
done < "$contents"

listing="$work_dir/archive-listing"
tar -tvzf "$archive" > "$listing"
while IFS= read -r entry; do
  type=$(printf '%s' "$entry" | cut -c 1)
  case "$type" in
    -|d) ;;
    *) echo "pokachy install: release archive contains an unsupported entry type" >&2; exit 1 ;;
  esac
done < "$listing"

extract_dir="$work_dir/extracted"
mkdir "$extract_dir"
tar --no-same-owner --no-same-permissions -xzf "$archive" -C "$extract_dir"

release_dir="$extract_dir/$release_root"
if [ ! -d "$release_dir" ] || [ ! -f "$release_dir/scripts/install.sh" ]; then
  echo "pokachy install: release archive has an unexpected layout" >&2
  exit 1
fi

POKACHY_INSTALL_SKIP_NEXT=1 bash "$release_dir/scripts/install.sh"

install_prefix=${POKACHY_INSTALL_PREFIX:-"${HOME:?HOME is required}/.local"}
pokachy_binary="$install_prefix/bin/pokachy"
echo
if [ "$mode" = --update ]; then
  echo "Pokachy files are updated. Your local session was preserved."
else
  echo "Pokachy is installed. Connect this computer:"
  if command -v pokachy >/dev/null 2>&1; then
    echo "  pokachy init"
  else
    echo "  $pokachy_binary init"
    echo
    echo "Add $install_prefix/bin to PATH to run Pokachy as 'pokachy'."
  fi
fi
