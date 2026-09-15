#!/usr/bin/env bash
# Export the minimal, standalone git repository contents consumed by
# `omarchy plugin add`. plugins/omarchy remains the source of truth.
set -euo pipefail

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
root_dir=$(CDPATH= cd -- "$script_dir/.." && pwd)
destination=${1:-"$root_dir/dist/pokachy-omarchy"}

case "$destination" in
  /*) ;;
  *) destination="$PWD/$destination" ;;
esac

if [ "$destination" = "$root_dir" ] || [ "$destination" = / ]; then
  echo "pokachy plugin export: refusing unsafe destination: $destination" >&2
  exit 2
fi
if [ -e "$destination" ]; then
  echo "pokachy plugin export: destination already exists: $destination" >&2
  exit 2
fi

source_dir="$root_dir/plugins/omarchy"
required=(manifest.json README.md LICENSE BarWidget.qml BrandIcon.qml Panel.qml PokeButton.qml PokeHand.qml SquareKeyboardPanel.qml pokachy-bar-icon.png)
for file in "${required[@]}"; do
  if [ ! -f "$source_dir/$file" ]; then
    echo "pokachy plugin export: missing $source_dir/$file" >&2
    exit 1
  fi
done

mkdir -p "$destination"
for file in "${required[@]}"; do
  install -m 0644 "$source_dir/$file" "$destination/$file"
done
printf 'Standalone Omarchy plugin written to %s\n' "$destination"
