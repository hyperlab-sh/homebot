#!/usr/bin/env bash
# Runs inside the release-test container; scripts/release-test.sh passes it to
# `bash -c`. stdin is a tar of the worktree; arg 1 is the mode, the rest go to
# the mode's script. Nothing is copied back.
set -uo pipefail

mode=${1:?mode}
shift

cd /src || exit 97
tar -x -C /src || { echo "could not unpack the source tar" >&2; exit 97; }

modes_dir=/src/scripts/release-test/modes

# all runs check and snapshot first, then every other mode in name order,
# except a mode whose header (its first 15 lines) carries the tag
#
#   # release-test: not-in-all
#
# Such a mode is heavy or reaches the network (cosign-matrix downloads cosign
# releases and the Sigstore trust root) and runs on demand, by name. The tag
# lives in the mode's own file, so adding one touches no shared file. Tags are
# separated by spaces or commas.
mode_has_tag() { # mode_has_tag FILE TAG
  head -n 15 "$1" | grep -qE "^#[[:space:]]*release-test:.*[[:space:],:]$2([[:space:],]|\$)"
}

all_modes() {
  local f m
  for m in check snapshot; do
    if [[ -f $modes_dir/$m.sh ]]; then echo "$m"; fi
  done
  for f in "$modes_dir"/*.sh; do
    m=$(basename "$f" .sh)
    case $m in check | snapshot) continue ;; esac
    if mode_has_tag "$f" not-in-all; then continue; fi
    echo "$m"
  done
}

run_mode() {
  local m=$1
  shift
  if [[ ! -f $modes_dir/$m.sh ]]; then
    echo "release-test: no mode '$m'" >&2
    return 64
  fi
  bash "$modes_dir/$m.sh" "$@"
}

case $mode in
  all)
    results=()
    failed=0
    for m in $(all_modes); do
      printf '\n=== %s\n' "$m"
      run_mode "$m"
      rc=$?
      if (( rc == 0 )); then results+=("PASS  $m"); else results+=("FAIL  $m  (exit $rc)"); failed=1; fi
    done
    printf '\n=== summary\n'
    printf '%s\n' "${results[@]}"
    exit "$failed"
    ;;
  *)
    run_mode "$mode" "$@"
    exit $?
    ;;
esac
