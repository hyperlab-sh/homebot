#!/usr/bin/env bash
# Runs a release check (goreleaser, packages, the install script) in a Docker
# container, never on this machine. Host neutral: the caller names the Docker
# context and the limits; there is no default host and no slot lock.
#
#   scripts/release-test.sh --context NAME [--cpus N] [--memory SIZE]
#                           [--cpu-shares N] [--pids-limit N] [--siblings]
#                           MODE [ARGS...]
#   scripts/release-test.sh --context NAME --ensure-image
#
# --siblings mounts the Docker host's socket into the run, which modes that
# start sibling containers need (lib.sh fails without it). The socket is root
# on the host, so the caller opts in; a host that must not hand it out never
# passes the flag, and the sibling helpers then refuse.
# --ensure-image only builds the image when it is missing (a caller can take a
# lock around that, so two first runs do not build it twice).
#
# MODE is a file in scripts/release-test/modes/ (without .sh), or `all`, which
# runs every mode except those tagged `# release-test: not-in-all`
# (today cosign-matrix, packages and brew-linux; they run by name). Siblings (the
# distro and Homebrew containers some modes start) share one labelled volume
# per run, and the run removes its containers and its volume on exit, on
# Ctrl-C and on TERM.
#
#   RELEASE_TEST_RUN_ID=ID   name the run (letters, digits, dashes), so a
#                            caller can find its containers by label
#
# Exit codes: 64 for a usage error (unknown mode, no --context), 130 and 143
# when interrupted, otherwise the mode's own.
set -euo pipefail

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
root=$(cd "$here/.." && pwd)
dockerfile=$here/release-test/Dockerfile
modes_dir=$here/release-test/modes

usage() {
  sed -n '2,29p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//' >&2
}

die_usage() {
  echo "release-test: $*" >&2
  usage
  exit 64
}

docker_ctx=""
cpus=""
memory=""
cpu_shares=""
pids_limit=""
siblings=0
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
    --siblings) siblings=1; shift ;;
    --ensure-image) ensure_image=1; shift ;;
    --) shift; break ;;
    -*) die_usage "unknown option '$1'" ;;
    *) break ;;
  esac
done

mode=${1:-}
if [[ -z $mode ]]; then
  if (( ensure_image )); then mode=-; else die_usage "no mode given"; fi
else
  shift
fi

# --- validate before touching Docker ------------------------------------------

mode_ok=0
if [[ $mode == - ]]; then
  mode_ok=1
elif [[ $mode == all ]]; then
  mode_ok=1
elif [[ $mode =~ ^[a-z0-9][a-z0-9-]*$ && -f $modes_dir/$mode.sh ]]; then
  mode_ok=1
fi
if (( ! mode_ok )); then
  available=$(cd "$modes_dir" 2>/dev/null && for f in *.sh; do [[ -e $f ]] && printf '%s ' "${f%.sh}"; done)
  die_usage "unknown mode '$mode' (modes: ${available}all)"
fi

if [[ -z $docker_ctx ]]; then
  die_usage "--context NAME is required: the Docker context to run on (there is no default host)"
fi
if [[ -n $cpus && ! $cpus =~ ^[0-9]+(\.[0-9]+)?$ ]]; then die_usage "--cpus wants a number, got '$cpus'"; fi
if [[ -n $memory && ! $memory =~ ^[0-9]+[bBkKmMgG]?$ ]]; then die_usage "--memory wants a size like 3g, got '$memory'"; fi
if [[ -n $cpu_shares && ! $cpu_shares =~ ^[0-9]+$ ]]; then die_usage "--cpu-shares wants a number, got '$cpu_shares'"; fi
if [[ -n $pids_limit && ! $pids_limit =~ ^[0-9]+$ ]]; then die_usage "--pids-limit wants a number, got '$pids_limit'"; fi

run_id=${RELEASE_TEST_RUN_ID:-$(date +%Y%m%d-%H%M%S)-$$}
if [[ ! $run_id =~ ^[A-Za-z0-9][A-Za-z0-9-]*$ ]]; then
  die_usage "RELEASE_TEST_RUN_ID may hold only letters, digits and dashes"
fi

# An explicit context on every call; DOCKER_HOST / DOCKER_CONTEXT would
# override it.
unset DOCKER_HOST DOCKER_CONTEXT

label=homebot-release-test
volume=homebot-release-$run_id
container=homebot-release-$run_id-main
tmp=$(mktemp -d "${TMPDIR:-/tmp}/homebot-release-test.XXXXXX")
client=""

# shellcheck disable=SC2317,SC2329 # used by cleanup, which only the traps call
docker_cmd() { docker --context "$docker_ctx" "$@"; }

# run_bg ARGS...: runs `docker --context CTX ARGS...` in the background and
# waits on it, so an INT or TERM reaches the traps at once instead of after the
# call (an image build takes 25 s). The docker binary itself is what gets
# backgrounded, not a shell function: bash runs a backgrounded function in a
# subshell, and killing the subshell would leave the docker client running.
# cleanup kills the client.
run_bg() {
  docker --context "$docker_ctx" "$@" <&0 &
  client=$!
  local rc=0
  wait "$client" || rc=$?
  client=""
  return "$rc"
}

# shellcheck disable=SC2317,SC2329 # invoked by the traps below
# cleanup removes everything this run started: the main container first, so it
# can start no more siblings, then every container carrying the run's label
# (twice, in case one was being created), then the volume.
cleanup() {
  local rc=$?
  trap - EXIT
  if [[ -n $client ]]; then kill "$client" 2>/dev/null || true; fi
  docker_cmd rm -f "$container" >/dev/null 2>&1 || true
  local ids
  for _ in 1 2; do
    ids=$(docker_cmd ps -aq --filter "label=$label=$run_id" 2>/dev/null || true)
    if [[ -n $ids ]]; then
      # shellcheck disable=SC2086 # one id per word
      docker_cmd rm -f $ids >/dev/null 2>&1 || true
    fi
  done
  if ! docker_cmd volume rm -f "$volume" >/dev/null 2>&1; then
    if docker_cmd volume inspect "$volume" >/dev/null 2>&1; then
      echo "release-test: could not remove the volume $volume (label $label=$run_id); remove it by hand" >&2
    fi
  fi
  rm -rf "$tmp"
  exit "$rc"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

echo "release-test: context $docker_ctx, run $run_id, mode ${mode/#-/(ensure the image)}" >&2

# --- the image ----------------------------------------------------------------
# Tagged by a hash of the Dockerfile; built on the Docker host when missing.

if command -v sha256sum >/dev/null 2>&1; then
  hash=$(sha256sum "$dockerfile" | cut -c1-12)
else
  hash=$(shasum -a 256 "$dockerfile" | cut -c1-12)
fi
image=homebot-release-test:$hash
if ! run_bg image inspect "$image" >/dev/null 2>&1; then
  echo "release-test: building $image on $docker_ctx (first run for this Dockerfile)..." >&2
  run_bg build --tag "$image" - <"$dockerfile" >&2
fi
if (( ensure_image )) && [[ $mode == - ]]; then exit 0; fi

# --- the Docker socket (--siblings only) --------------------------------------
# Siblings are started through the host's socket (no Docker in Docker). The
# container runs as tester, so it joins the socket's group. --mount, unlike -v,
# fails instead of creating a directory when the socket is missing. Without
# --siblings the socket is neither probed nor mounted.

sock=/var/run/docker.sock
socket_args=()
if (( siblings )); then
  gid_file=$tmp/sock.gid
  run_bg run --rm --label "$label=$run_id" --entrypoint stat \
    --mount "type=bind,src=$sock,dst=$sock" "$image" -c %g "$sock" >"$gid_file" \
    || { echo "release-test: $sock is not available on the Docker host of context $docker_ctx" >&2; exit 1; }
  socket_args=(--group-add "$(<"$gid_file")" --mount "type=bind,src=$sock,dst=$sock")
fi

# --- ship the tree ------------------------------------------------------------
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

# --- run ----------------------------------------------------------------------

limits=()
[[ -z $cpus ]] || limits+=(--cpus "$cpus")
[[ -z $memory ]] || limits+=(--memory "$memory")
[[ -z $cpu_shares ]] || limits+=(--cpu-shares "$cpu_shares")
[[ -z $pids_limit ]] || limits+=(--pids-limit "$pids_limit")

run_bg volume create --label "$label=$run_id" "$volume" >/dev/null

run_args=(
  run --rm -i --init --name "$container" --label "$label=$run_id"
  -v "$volume:/release"
  -v homebot-release-gomod:/home/tester/go/pkg/mod
  -v homebot-release-cache:/home/tester/.cache
  -e "RT_SIBLINGS=$siblings" -e "RT_RUN_ID=$run_id" -e "RT_VOLUME=$volume" -e "RT_IMAGE=$image"
  -e "RT_LIMIT_ARGS=${limits[*]+"${limits[*]}"}"
)
if (( ${#limits[@]} )); then run_args+=("${limits[@]}"); fi
if (( ${#socket_args[@]} )); then run_args+=("${socket_args[@]}"); fi
run_args+=("$image" bash -c "$(<"$here/release-test/in-container.sh")" in-container "$mode" "$@")

# The client runs in the background and the script waits on it, so Ctrl-C or a
# TERM reaches the traps at once instead of after the run; cleanup then kills
# the client and removes the containers and the volume.
rc=0
docker --context "$docker_ctx" "${run_args[@]}" <"$src_tar" &
client=$!
wait "$client" || rc=$?
client=""

exit "$rc"
