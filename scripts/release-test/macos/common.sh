#!/usr/bin/env bash
# Shared, throwaway localhost release server for the two macOS Actions jobs.
set -euo pipefail

die() { echo "release-check: $*" >&2; exit 1; }
work='' base='' port='' server_pid=''

snapshot_root() {
  [[ $# == 1 && -d $1/v0.0.1 && -d $1/v0.0.2 ]] || die 'expected v0.0.1 and v0.0.2 snapshot directories'
  (cd "$1" && pwd)
}

start_server() {
  local snapshots=$1 version
  work=$(mktemp -d "${TMPDIR:-/tmp}/homebot-macos-release.XXXXXX")
  mkdir -p "$work/web/releases/download"
  for version in 0.0.1 0.0.2; do
    [[ -s $snapshots/v$version/checksums.txt ]] || die "missing v$version snapshot"
    local served="$work/web/releases/download/v$version" file
    mkdir -p "$served"
    for file in "$snapshots/v$version"/*; do ln -s "$file" "$served/$(basename "$file")"; done
    # Snapshot signing is deliberately skipped. install.sh still downloads a
    # bundle before it decides whether cosign is available; this sentinel is
    # never accepted as a signature and the test asserts the unsigned note.
    printf '{}\n' >"$served/checksums.txt.sigstore.json"
  done
  # Bind to the loopback address; the picked port is used only by this job.
  port=$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()')
  python3 -m http.server "$port" --bind 127.0.0.1 --directory "$work/web" >"$work/http.log" 2>&1 &
  server_pid=$!
  local n
  for ((n=0; n<30; n++)); do
    if curl --noproxy '*' -fsS "http://127.0.0.1:$port/releases/download/v0.0.1/checksums.txt" >/dev/null; then
      # shellcheck disable=SC2034 # consumed by the scripts that source this file
      base="http://127.0.0.1:$port"
      return
    fi
    kill -0 "$server_pid" 2>/dev/null || { cat "$work/http.log" >&2; die 'snapshot server exited'; }
    sleep 1
  done
  die 'snapshot server did not become ready'
}

stop_server() {
  if [[ -n ${server_pid:-} ]]; then kill "$server_pid" 2>/dev/null || true; wait "$server_pid" 2>/dev/null || true; fi
  if [[ -n ${work:-} ]]; then rm -rf "$work"; fi
}

must_equal() { [[ $1 == "$2" ]] || die "$3: got '$1', want '$2'"; }
