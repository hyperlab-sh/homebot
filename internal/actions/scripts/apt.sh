#!/usr/bin/env bash
# Install apt packages listed one per line (# comments allowed).   - apt: linux/apt.txt
set -euo pipefail

command -v apt-get >/dev/null || { echo "apt-get not found: this action is for Debian/Ubuntu systems" >&2; exit 127; }

# No sudo when already root; keep debconf quiet either way.
apt() { if ((EUID == 0)); then DEBIAN_FRONTEND=noninteractive apt-get "$@"; else sudo DEBIAN_FRONTEND=noninteractive apt-get "$@"; fi; }

for file in "$@"; do
  [[ -f $file ]] || { echo "package list not found: manifests/$file" >&2; exit 1; }
  mapfile -t wanted < <(sed -e 's/#.*//' -e 's/[[:space:]]//g' "$file" | grep -v '^$' || true)
  missing=()
  for pkg in "${wanted[@]}"; do
    dpkg-query -W -f='${Status}' "$pkg" 2>/dev/null | grep -q 'install ok installed' || missing+=("$pkg")
  done
  if ((${#missing[@]} == 0)); then
    echo "apt: all ${#wanted[@]} packages from $file are installed"
    continue
  fi
  apt update
  # Packages no repository here offers (another distro's, a removed vendor repo,
  # a local .deb) are reported and left out instead of failing the whole install.
  available=()
  unavailable=()
  while IFS=$'\t' read -r status pkg; do
    if [[ $status == ok ]]; then available+=("$pkg"); else unavailable+=("$pkg"); fi
  done < <(apt-cache pkgnames | awk 'NR == FNR { known[$0]; next } { name = $0; sub(/:.*/, "", name); print ((name in known) ? "ok" : "no") "\t" $0 }' - <(printf '%s\n' "${missing[@]}"))
  if ((${#unavailable[@]})); then
    echo "apt: skipped ${#unavailable[@]} that no repository here offers: ${unavailable[*]}" >&2
  fi
  ((${#available[@]})) || continue
  echo "apt: installing ${#available[@]} of ${#wanted[@]} packages from $file"
  apt install -y "${available[@]}"
done
