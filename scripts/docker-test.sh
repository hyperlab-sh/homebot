#!/usr/bin/env bash
# Runs homebot's test gate in a Docker container, never on this machine.
# Host neutral: the caller names the Docker context and the limits; there is
# no default host and no slot lock. See "Running the tests" in the README.
#
#   scripts/docker-test.sh --context NAME [--cpus N] [--memory SIZE]
#                          [--cpu-shares N] [--pids-limit N] [MODE [ARGS...]]
#   scripts/docker-test.sh --context NAME --ensure-image
#
#   MODE all (default)    the whole gate
#   MODE go-test [args]   go test, args passed through
#   MODE lint             golangci-lint
#   MODE generate         go generate, changes copied back
#   MODE update [args]    go test [args] -update, changes copied back
#
# --ensure-image only builds the image when it is missing (a caller can take a
# lock around that, so two first runs do not build it twice).
#
# Exit codes: 64 for a usage error (unknown mode, no --context), otherwise
# the mode's own.
set -euo pipefail

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
root=$(cd "$here/.." && git rev-parse --show-toplevel)
dockerfile=$here/docker-test/Dockerfile
runner=$here/docker-test/in-container.sh

usage() {
  sed -n '2,20p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//' >&2
}

die_usage() {
  echo "docker-test: $*" >&2
  usage
  exit 64
}

docker_ctx=""
cpus=""
memory=""
cpu_shares=""
pids_limit=""
ensure_image=0

need_value() { if (( $# < 2 )) || [[ -z $2 ]]; then die_usage "$1 needs a value"; fi; }

while (( $# )); do
  case $1 in
    -h | --help | help) usage; exit 0 ;;
    --context) need_value "$@"; docker_ctx=$2; shift 2 ;;
    --context=*) docker_ctx=${1#*=}; shift ;;
    --cpus) need_value "$@"; cpus=$2; shift 2 ;;
    --cpus=*) cpus=${1#*=}; shift ;;
    --memory) need_value "$@"; memory=$2; shift 2 ;;
    --memory=*) memory=${1#*=}; shift ;;
    --cpu-shares) need_value "$@"; cpu_shares=$2; shift 2 ;;
    --cpu-shares=*) cpu_shares=${1#*=}; shift ;;
    --pids-limit) need_value "$@"; pids_limit=$2; shift 2 ;;
    --pids-limit=*) pids_limit=${1#*=}; shift ;;
    --ensure-image) ensure_image=1; shift ;;
    --) shift; break ;;
    -*) die_usage "unknown option '$1'" ;;
    *) break ;;
  esac
done

mode=all
case ${1:-} in
  all | go-test | lint | generate | update) mode=$1; shift ;;
  "") ;;
  *) die_usage "unknown subcommand '$1'" ;;
esac

if [[ -z $docker_ctx ]]; then
  die_usage "--context NAME is required: there is no default Docker host (see 'docker context ls')"
fi

# An explicit context on every call; DOCKER_HOST / DOCKER_CONTEXT would
# override it.
unset DOCKER_HOST DOCKER_CONTEXT

tmp=$(mktemp -d "${TMPDIR:-/tmp}/homebot-docker-test.XXXXXX")
container=""
client=""

docker_cmd() { docker --context "$docker_ctx" "$@"; }

# shellcheck disable=SC2317,SC2329 # invoked by the traps below
cleanup() {
  local rc=$?
  trap - EXIT
  if [[ -n ${client:-} ]]; then kill "$client" 2>/dev/null || true; fi
  if [[ -n $container ]]; then
    docker_cmd rm -f "$container" >/dev/null 2>&1 || true
  fi
  rm -rf "$tmp"
  exit "$rc"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

limits=()
if [[ -n $cpus ]]; then limits+=(--cpus "$cpus"); fi
if [[ -n $memory ]]; then limits+=(--memory "$memory"); fi
if [[ -n $cpu_shares ]]; then limits+=(--cpu-shares "$cpu_shares"); fi
if [[ -n $pids_limit ]]; then limits+=(--pids-limit "$pids_limit"); fi

# --- the image ---------------------------------------------------------------
# Tagged by a hash of the Dockerfile; built on the Docker host when missing.

hash=$(shasum -a 256 "$dockerfile" | cut -c1-12)
image=homebot-test:$hash
if ! docker_cmd image inspect "$image" >/dev/null 2>&1; then
  echo "docker-test: building $image on context $docker_ctx (first run for this Dockerfile)..." >&2
  docker_cmd build --tag "$image" - <"$dockerfile" >&2
fi
if (( ensure_image )); then exit 0; fi

# --- ship the tree -----------------------------------------------------------
# Tracked plus untracked-not-ignored files that exist now, so uncommitted work
# is tested. Never .git or .worktrees.

src_tar=$tmp/src.tar
tar_flags=()
# bsdtar (macOS) writes extended attributes GNU tar in the container warns about.
if tar --version 2>/dev/null | grep -q bsdtar; then tar_flags=(--no-xattrs --no-mac-metadata); fi
(
  cd "$root"
  git ls-files -z --cached --others --exclude-standard --deduplicate \
    | while IFS= read -r -d '' f; do
        case $f in
          .git/* | .worktrees/*) continue ;;
        esac
        if [[ -e $f || -L $f ]]; then printf '%s\0' "$f"; fi
      done \
    | COPYFILE_DISABLE=1 tar "${tar_flags[@]+"${tar_flags[@]}"}" -cf "$src_tar" --null -T -
)

# --- run ---------------------------------------------------------------------

container=homebot-test-$(date +%s)-$$
out_tar=$tmp/out.tar

run_args=(
  run --rm -i --init --name "$container"
  -v homebot-test-gomod:/home/tester/go/pkg/mod
  -v homebot-test-cache:/home/tester/.cache
)
if (( ${#limits[@]} )); then run_args+=("${limits[@]}"); fi
run_args+=("$image" bash -c "$(<"$runner")" in-container "$mode" "$@")

# The client runs in the background and the script waits on it, so Ctrl-C or a
# TERM reaches the traps at once instead of after the run; cleanup then kills
# the client and removes the container.
rc=0
case $mode in
  generate | update)
    docker_cmd "${run_args[@]}" <"$src_tar" >"$out_tar" &
    ;;
  *)
    docker_cmd "${run_args[@]}" <"$src_tar" &
    ;;
esac
client=$!
wait "$client" || rc=$?
client=""
container=""

if [[ $mode == generate || $mode == update ]]; then
  if [[ -s $out_tar ]]; then
    echo "docker-test: files copied back into $root:"
    tar -tf "$out_tar" | sed 's/^/  /'
    tar -xf "$out_tar" -C "$root"
  else
    echo "docker-test: the run changed no files"
  fi
fi

exit "$rc"
