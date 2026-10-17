#!/usr/bin/env bash
# Install font folders from furniture/fonts.   - fonts: [JetBrainsMono, FiraCode]
set -euo pipefail

src_root="$HOMEBOT_FURNITURE/fonts"
case "$HOMEBOT_OS" in
  macos) dest_root="$HOME/Library/Fonts" ;;
  linux) dest_root="${XDG_DATA_HOME:-$HOME/.local/share}/fonts" ;;
  *) echo "fonts: unsupported OS $HOMEBOT_OS" >&2; exit 1 ;;
esac

for name in "$@"; do
  src="$src_root/$name"
  [[ -d $src ]] || { echo "font folder not found: furniture/fonts/$name" >&2; exit 1; }
  # macOS only scans the top level of ~/Library/Fonts reliably; Linux is happy with subfolders.
  if [[ $HOMEBOT_OS == linux ]]; then dest="$dest_root/$name"; else dest="$dest_root"; fi
  mkdir -p "$dest"
  count=0
  while IFS= read -r -d '' font; do
    cp -f "$font" "$dest/"
    count=$((count + 1))
  done < <(find "$src" -type f \( -iname '*.ttf' -o -iname '*.otf' -o -iname '*.ttc' \) -print0)
  echo "fonts: $name ($count files)"
done

if [[ $HOMEBOT_OS == linux ]] && command -v fc-cache >/dev/null; then
  fc-cache -f "$dest_root" >/dev/null
fi
