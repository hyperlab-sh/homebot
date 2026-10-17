#!/usr/bin/env bash
# Install everything in one or more Brewfiles.   - brew: "{os}/Brewfile"
set -euo pipefail

command -v brew >/dev/null || { echo "brew is not installed: see https://brew.sh" >&2; exit 127; }

for file in "$@"; do
  [[ -f $file ]] || { echo "Brewfile not found: manifests/$file" >&2; exit 1; }
  brew bundle install --file="$file"
done
