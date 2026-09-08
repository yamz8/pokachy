#!/usr/bin/env bash
# Install a prebuilt Pokachy CLI and its user service. This script never
# downloads software, uses sudo, or starts a service.
set -euo pipefail

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
root_dir=$(CDPATH= cd -- "$script_dir/.." && pwd)
home_dir=${HOME:?HOME is required when POKACHY_INSTALL_PREFIX is not set}
install_prefix=${POKACHY_INSTALL_PREFIX:-"$home_dir/.local"}
config_root=${XDG_CONFIG_HOME:-"$home_dir/.config"}

case "$install_prefix" in
  /*) ;;
  *) echo "pokachy install: POKACHY_INSTALL_PREFIX must be an absolute path" >&2; exit 2 ;;
esac
case "$install_prefix" in
  *$'\n'*|*$'\r'*) echo "pokachy install: POKACHY_INSTALL_PREFIX cannot contain a newline" >&2; exit 2 ;;
esac
case "$config_root" in
  /*) ;;
  *) echo "pokachy install: XDG_CONFIG_HOME must be an absolute path" >&2; exit 2 ;;
esac

binary=""
for candidate in "$root_dir/bin/pokachy" "$root_dir/cli/bin/pokachy"; do
  if [ -f "$candidate" ]; then
    binary=$candidate
    break
  fi
done
if [ -z "$binary" ]; then
  echo "pokachy install: no compiled binary found (expected bin/pokachy or cli/bin/pokachy)" >&2
  exit 1
fi

service="$root_dir/packaging/systemd/pokachy.service"
if [ ! -f "$service" ]; then
  echo "pokachy install: missing packaging/systemd/pokachy.service" >&2
  exit 1
fi

mkdir -p "$install_prefix/bin" "$config_root/systemd/user"
install -m 0755 "$binary" "$install_prefix/bin/pokachy"

# ExecStart is parsed by systemd, not a shell. Quote spaces and escape the
# characters systemd treats specially while retaining a literal absolute path.
binary_destination="$install_prefix/bin/pokachy"
unit_path=$(printf '%s' "$binary_destination" | sed 's/\\/\\\\/g; s/"/\\"/g; s/%/%%/g')
{
  while IFS= read -r line || [ -n "$line" ]; do
    if [ "${line#ExecStart=}" != "$line" ]; then
      printf 'ExecStart="%s" daemon\n' "$unit_path"
    else
      printf '%s\n' "$line"
    fi
  done < "$service"
} | install -m 0644 /dev/stdin "$config_root/systemd/user/pokachy.service"

printf 'Installed Pokachy to %s\n' "$install_prefix/bin/pokachy"
printf '\nNext, connect this computer:\n  %s/bin/pokachy init\n' "$install_prefix"
printf '\nThen enable notifications:\n  systemctl --user daemon-reload\n  systemctl --user enable --now pokachy.service\n'
