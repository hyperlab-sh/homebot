# shellcheck shell=bash
# Helpers for the release-test modes. Source it from a mode script:
#
#   # shellcheck source-path=SCRIPTDIR source=../lib.sh
#   . "$(dirname "${BASH_SOURCE[0]}")/../lib.sh"
#
# Inside the container scripts/release-test.sh starts, these are set:
#
#   RT_SRC      the unpacked worktree (/src)
#   RT_VOL      where the run's volume is mounted (/release)
#   RT_RUN_ID   the run id; RT_VOLUME the volume's name; RT_IMAGE this image
#   RT_LIMIT_ARGS   the run's docker limits, applied to every sibling too
#   RT_SIBLINGS 1 only when the caller passed --siblings to release-test.sh
#
# Siblings (a distro, a Homebrew image, a second copy of this image as root)
# are started through the Docker host's socket, mounted into the container
# only when the caller passed --siblings: there is no Docker in Docker. The
# socket is root on the host, so every sibling helper refuses without it.
# Mode scripts reach Docker only through these helpers (the check mode fails a
# mode file that calls docker itself), and a sibling image other than this one
# must be pinned by digest. Every sibling carries the run's label and
# mounts the run's volume at /release, and scripts/release-test.sh removes them
# all, and the volume, when the run ends however it ends.

RT_SRC=${RT_SRC:-/src}
RT_VOL=${RT_VOL:-/release}
RT_LABEL=homebot-release-test

rt_log() { printf 'release-test: %s\n' "$*" >&2; }
rt_die() { rt_log "$*"; exit 1; }

# rt_cask_local_origin FILE PORT rewrites a GoReleaser cask in place so its
# release downloads come from http://127.0.0.1:PORT/. Only the origin changes:
# the cask's URLs are Ruby (`v#{version}/homebot_#{version}_...`), so the tag
# and asset names stay for Homebrew to interpolate. Dies when no URL was
# rewritten or a GitHub release URL is left.
rt_cask_local_origin() {
  local file=$1 port=$2 tmp
  tmp=$(mktemp "$file.XXXXXX") || return 1
  if sed "s|https://github.com/hyperlab-sh/homebot/releases/download/|http://127.0.0.1:$port/|g" "$file" >"$tmp"; then
    mv "$tmp" "$file"
  else
    rm -f "$tmp"
    return 1
  fi
  # shellcheck disable=SC2016 # #{version} is Ruby, not a shell expansion
  grep -Fq "http://127.0.0.1:$port/v#{version}/" "$file" \
    || rt_die "cask has no release URL to rewrite: $file"
  ! grep -Fq 'github.com/hyperlab-sh/homebot/releases/download/' "$file" \
    || rt_die "remote release URL remains in the cask: $file"
}

# rt_require_env NAME... fails when a variable is unset or empty.
rt_require_env() {
  local v
  for v in "$@"; do
    [[ -n ${!v:-} ]] || rt_die "$v is not set (this runs inside scripts/release-test.sh)"
  done
}

# rt_require_uid N fails unless the caller runs as uid N. A test that needs
# root, or needs not to be root, calls this first, so it fails rather than
# passes when it runs as the wrong user.
rt_require_uid() {
  local want=$1 got
  got=$(id -u)
  [[ $got == "$want" ]] || rt_die "this step must run as uid $want, not $got"
}

# rt_need_snapshots builds the two snapshot releases into $RT_VOL/snapshots
# unless an earlier mode of this run did (`all` runs snapshot early).
rt_need_snapshots() {
  if [[ -f $RT_VOL/snapshots/.complete ]]; then return 0; fi
  bash "$RT_SRC/scripts/release-test/modes/snapshot.sh"
}

# --- siblings ---------------------------------------------------------------

rt_sibling_name() { printf 'homebot-release-%s-%s' "$RT_RUN_ID" "$1"; }

# _rt_sibling_args fills RT_ARGS with the docker run arguments shared by every
# sibling. Options: --user U, --image IMG, --net-of NAME (share the network of
# the named sibling, so 127.0.0.1 reaches its server), --env K=V (repeatable),
# --name NAME (detached siblings only), --platform PLATFORM. Whatever follows
# -- is the command.
_rt_sibling_guard() {
  if [[ ${RT_SIBLINGS:-0} != 1 || ! -S /var/run/docker.sock ]]; then
    rt_die "this mode starts sibling containers; it needs scripts/release-test.sh --siblings (the Docker socket is not mounted)"
  fi
}

_rt_sibling_parse() {
  _rt_sibling_guard
  rt_require_env RT_RUN_ID RT_VOLUME RT_IMAGE
  RT_ARGS=(--init --label "$RT_LABEL=$RT_RUN_ID" -v "$RT_VOLUME:/release")
  RT_IMG=$RT_IMAGE
  RT_NAME=""
  local limits=()
  if [[ -n ${RT_LIMIT_ARGS:-} ]]; then read -r -a limits <<<"$RT_LIMIT_ARGS"; fi
  if (( ${#limits[@]} )); then RT_ARGS+=("${limits[@]}"); fi
  while (( $# )); do
    case $1 in
      --user) RT_ARGS+=(--user "$2"); shift 2 ;;
      --image) RT_IMG=$2; shift 2 ;;
      --platform) RT_ARGS+=(--platform "$2"); shift 2 ;;
      --net-of) RT_ARGS+=(--network "container:$(rt_sibling_name "$2")"); shift 2 ;;
      --env) RT_ARGS+=(-e "$2"); shift 2 ;;
      --name) RT_NAME=$2; shift 2 ;;
      --) shift; break ;;
      *) rt_die "rt_sibling: unknown option '$1'" ;;
    esac
  done
  RT_CMD=("$@")
  if [[ $RT_IMG != "$RT_IMAGE" && $RT_IMG != *@sha256:* ]]; then
    rt_die "sibling image '$RT_IMG' is not pinned by digest (name@sha256:...)"
  fi
}

# rt_sibling_run [options] -- COMMAND...   runs a sibling to completion and
# returns its exit status. The default image is this one, as tester.
rt_sibling_run() {
  _rt_sibling_parse "$@"
  docker run --rm "${RT_ARGS[@]}" "$RT_IMG" "${RT_CMD[@]}"
}

# rt_sibling_start --name NAME [options] -- COMMAND...   starts a detached
# sibling that rt_sibling_stop or the run's cleanup removes.
rt_sibling_start() {
  _rt_sibling_parse "$@"
  [[ -n $RT_NAME ]] || rt_die "rt_sibling_start needs --name"
  docker run -d --name "$(rt_sibling_name "$RT_NAME")" "${RT_ARGS[@]}" "$RT_IMG" "${RT_CMD[@]}" >/dev/null
}

# rt_sibling_exec NAME [--user U] [--env K=V] ... -- COMMAND...   runs a command
# inside the running sibling NAME (started by rt_sibling_start) and returns its
# exit status. State persists between calls, so one container can take an
# install, an upgrade and an uninstall in turn. The sibling's own --env and
# --user are the defaults; these override them for this one command.
rt_sibling_exec() {
  _rt_sibling_guard
  local name=${1:?rt_sibling_exec NAME [options] -- COMMAND...}
  shift
  local args=()
  while (( $# )); do
    case $1 in
      --user) args+=(--user "$2"); shift 2 ;;
      --env) args+=(-e "$2"); shift 2 ;;
      --) shift; break ;;
      *) rt_die "rt_sibling_exec: unknown option '$1'" ;;
    esac
  done
  (( $# )) || rt_die "rt_sibling_exec: no command after --"
  docker exec "${args[@]+"${args[@]}"}" "$(rt_sibling_name "$name")" "$@"
}

rt_sibling_stop() { docker rm -f "$(rt_sibling_name "$1")" >/dev/null 2>&1 || true; }
rt_sibling_logs() { docker logs "$(rt_sibling_name "$1")" 2>&1; }

# rt_http_start NAME DIR [PORT]   serves DIR (a path inside the volume) over
# HTTP from a detached sibling, bound to 127.0.0.1 in that sibling's network.
# Other siblings reach it, as http://127.0.0.1:PORT, with --net-of NAME. The
# install script accepts that URL form for plain HTTP. Returns once it answers.
rt_http_start() {
  local name=$1 dir=$2 port=${3:-8000}
  rt_sibling_start --name "$name" -- \
    python3 -m http.server "$port" --bind 127.0.0.1 --directory "$dir"
  # Poll from inside the server's own container: one exec per try, not one
  # new container.
  for _ in $(seq 1 50); do
    if docker exec "$(rt_sibling_name "$name")" curl -fsS -o /dev/null "http://127.0.0.1:$port/" 2>/dev/null; then
      return 0
    fi
    sleep 0.2
  done
  rt_sibling_logs "$name" >&2
  rt_die "the http server '$name' did not come up on port $port"
}

# rt_lint_no_docker FILE...   prints every line of the files that runs docker
# itself (a mention in a comment is fine) and returns 1 if there is any. The
# check mode runs it over the mode scripts: they reach Docker only through the
# rt_sibling_* helpers, which insist on --siblings and on digest-pinned images.
# A lint, not a security boundary: the boundary is that the socket is not
# mounted without --siblings, and that the wrapper passes --siblings only on
# the host meant for it. It matches the word `docker` in any command position
# (after if, then, !, command, exec, xargs, timeout, a pipe, &&, $( ...), so a
# string that says "docker" in lower case also trips it (write Docker).
rt_lint_no_docker() {
  local hits
  hits=$(grep -HnE '(^|[^[:alnum:]_./-])docker([[:space:]]|$)' "$@" | grep -vE '^[^:]+:[0-9]+:[[:space:]]*#' || true)
  if [[ -z $hits ]]; then return 0; fi
  printf '%s\n' "$hits"
  return 1
}

# rt_cleanup_siblings removes every container of this run except the one
# running this script. Modes that start siblings call it from an EXIT trap;
# scripts/release-test.sh removes the rest.
rt_cleanup_siblings() {
  [[ ${RT_SIBLINGS:-0} == 1 ]] || return 0
  local ids self
  self=$(hostname)
  ids=$(docker ps -aq --filter "label=$RT_LABEL=$RT_RUN_ID" 2>/dev/null | grep -v "^${self:0:12}" || true)
  if [[ -n $ids ]]; then
    # shellcheck disable=SC2086 # one id per word
    docker rm -f $ids >/dev/null 2>&1 || true
  fi
}
