#!/usr/bin/env bash
# Install Flatpak apps from Flathub, one app ID per line.   - flatpak: linux/flatpak.txt
set -euo pipefail

command -v flatpak >/dev/null || { echo "flatpak is not installed" >&2; exit 127; }

for file in "$@"; do
  [[ -f $file ]] || { echo "app list not found: manifests/$file" >&2; exit 1; }
  mapfile -t apps < <(sed -e 's/#.*//' -e 's/[[:space:]]//g' "$file" | grep -v '^$' || true)
  ((${#apps[@]})) || continue
  flatpak install -y --noninteractive flathub "${apps[@]}"
done
