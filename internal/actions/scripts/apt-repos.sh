#!/usr/bin/env bash
# Add third-party apt repositories and their signing keys.   - apt-repos: linux/apt-repos
#
# The folder holds sources.list.d files (.list or .sources), their keys in
# keyrings/, and `origin`: the release codename they were copied from.
# `apt-repos --export DIR` writes such a folder from this machine; homebot.yml
# runs it as an export. Installing skips a repo that is already configured, and
# one written for another release (it names the origin codename).
# HOMEBOT_APT_ROOT puts /etc and the keyrings under another folder (tests).
set -euo pipefail
shopt -s nullglob

ROOT=${HOMEBOT_APT_ROOT:-}
SOURCES=$ROOT/etc/apt/sources.list.d

# shellcheck source=/dev/null
codename() { (. "$ROOT/etc/os-release" 2>/dev/null && echo "${VERSION_CODENAME:-${UBUNTU_CODENAME:-}}") || true; }

# Lines that are not comments or blank.
active() { sed -e 's/^[[:space:]]*#.*//' "$1" | grep -v '^[[:space:]]*$' || true; }

# Repository URIs of a .list or .sources file, without trailing slashes.
uris() {
  case $1 in
    *.sources) active "$1" | sed -n 's/^URIs:[[:space:]]*//p' | tr ' \t' '\n' ;;
    *) active "$1" | sed -E -n 's/^deb(-src)?[[:space:]]+(\[[^]]*\][[:space:]]+)?([^[:space:]]+).*/\3/p' ;;
  esac | sed 's|/*$||' | grep -v '^$' || true
}

# Keyring files a repo is signed by. An inline key needs no file.
keys() {
  case $1 in
    *.sources) active "$1" | sed -n 's|^Signed-By:[[:space:]]*\(/[^[:space:]]*\).*|\1|p' ;;
    *) active "$1" | grep -o 'signed-by=[^] ]*' | sed 's/^signed-by=//' || true ;;
  esac | sort -u
}

# Mirrors of the distribution itself; this machine's install brought those.
distro_host() {
  case $1 in
    ubuntu.com | *.ubuntu.com | debian.org | *.debian.org | *.canonical.com | pop-os.org | *.pop-os.org) return 0 ;;
  esac
  return 1
}

third_party() {
  local uri host
  for uri in "$@"; do
    host=$(sed -E 's|^[a-z0-9+.-]+://([^/]*).*|\1|' <<<"$uri")
    distro_host "$host" && return 1
  done
  return 0
}

writable() {
  local d=$1
  while [[ ! -e $d ]]; do d=$(dirname "$d"); done
  [[ -w $d ]]
}

# sudo only when this user can't write the sources folder.
as_root() { if ((EUID == 0)) || writable "$SOURCES"; then "$@"; else sudo "$@"; fi; }

export_to() {
  local dir=$1 f name key found
  mkdir -p "$dir"
  codename >"$dir/origin"
  for f in "$SOURCES"/*.list "$SOURCES"/*.sources; do
    name=${f##*/}
    [[ $f == *.sources ]] && active "$f" | grep -qi '^Enabled:[[:space:]]*no' && continue
    mapfile -t found < <(uris "$f")
    ((${#found[@]})) || continue
    third_party "${found[@]}" || continue
    cp "$f" "$dir/$name"
    while read -r key; do
      [[ -n $key && -f $ROOT$key ]] || continue
      mkdir -p "$dir/keyrings"
      cp "$ROOT$key" "$dir/keyrings/${key##*/}"
    done < <(keys "$f")
  done
}

# True when another file already points apt at one of this file's URIs.
configured() {
  local uri other list
  while read -r uri; do
    for other in "$ROOT/etc/apt/sources.list" "$SOURCES"/*.list "$SOURCES"/*.sources; do
      [[ -f $other ]] || continue
      list=$(uris "$other")
      grep -qxF -- "$uri" <<<"$list" && return 0
    done
  done < <(uris "$1")
  return 1
}

added=0
install_from() {
  local dir=$1 origin="" here f name key missing present=0
  [[ -d $dir ]] || { echo "apt-repos: folder not found: manifests/$dir" >&2; exit 1; }
  [[ -f $dir/origin ]] && origin=$(<"$dir/origin")
  here=$(codename)
  for f in "$dir"/*.list "$dir"/*.sources; do
    name=${f##*/}
    if [[ -e $SOURCES/$name ]] || configured "$f"; then
      present=$((present + 1))
      continue
    fi
    if [[ -n $origin && -n $here && $origin != "$here" ]] && active "$f" | grep -qw -- "$origin"; then
      echo "apt-repos: skipped $name: it is for $origin and this machine runs $here" >&2
      continue
    fi
    missing=""
    while read -r key; do
      [[ -z $key || -e $ROOT$key || -f $dir/keyrings/${key##*/} ]] || { missing=${key##*/}; break; }
    done < <(keys "$f")
    if [[ -n $missing ]]; then
      echo "apt-repos: skipped $name: its key $missing is not in $dir/keyrings" >&2
      continue
    fi
    while read -r key; do
      [[ -z $key || -e $ROOT$key ]] || as_root install -D -m 644 "$dir/keyrings/${key##*/}" "$ROOT$key"
    done < <(keys "$f")
    as_root install -D -m 644 "$f" "$SOURCES/$name"
    echo "apt-repos: added $name"
    added=$((added + 1))
  done
  ((present == 0)) || echo "apt-repos: $present already set up in $dir"
}

if [[ ${1:-} == --export ]]; then
  [[ -n ${2:-} ]] || { echo "usage: apt-repos --export DIR" >&2; exit 2; }
  export_to "$2"
  exit 0
fi

command -v apt-get >/dev/null || { echo "apt-get not found: this action is for Debian/Ubuntu systems" >&2; exit 127; }
(($#)) || { echo "usage: - apt-repos: <folder>" >&2; exit 2; }
for dir in "$@"; do install_from "$dir"; done
if ((added)); then as_root env DEBIAN_FRONTEND=noninteractive apt-get update; fi
