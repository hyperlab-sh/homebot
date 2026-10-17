#!/usr/bin/env bash
# Tests of the release-test harness itself.
#
#   scripts/release-test/selftest.sh CONTEXT
#
# Runs on the machine that has Docker access, against the Docker context
# CONTEXT. Everything that needs a project (a .goreleaser.yaml, a Go module)
# uses a throwaway copy of the harness in a temp repository with a throwaway
# config; nothing of it is committed, and nothing of this repository's own
# release config is needed. Exits non-zero when any check fails.
# shellcheck disable=SC2031 # ids is only appended to, at top level
set -uo pipefail

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
root=$(cd "$here/../.." && pwd)

if (( $# != 1 )) || [[ $1 == -h || $1 == --help ]]; then
  sed -n '2,10p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//' >&2
  exit 64
fi
ctx=$1
unset DOCKER_HOST DOCKER_CONTEXT

tmp=$(mktemp -d "${TMPDIR:-/tmp}/homebot-release-selftest.XXXXXX")
ids=()
cleanup() {
  local id
  for id in "${ids[@]+"${ids[@]}"}"; do
    local c
    c=$(docker --context "$ctx" ps -aq --filter "label=homebot-release-test=$id" 2>/dev/null || true)
    # shellcheck disable=SC2086 # one id per word
    if [[ -n $c ]]; then docker --context "$ctx" rm -f $c >/dev/null 2>&1 || true; fi
    docker --context "$ctx" volume rm -f "homebot-release-$id" >/dev/null 2>&1 || true
  done
  rm -rf "$tmp"
}
trap cleanup EXIT

pass=0
fail=0
ok() { pass=$((pass + 1)); printf 'PASS  %s\n' "$1"; }
bad() { fail=$((fail + 1)); printf 'FAIL  %s\n' "$1"; if [[ -n ${2:-} ]]; then printf '%s\n' "$2" | sed 's/^/        /'; fi; }

# expect NAME WANT_RC GREP -- CMD...: runs CMD, checks its exit status and that
# its output matches GREP (an ERE; empty skips the match).
expect() {
  local name=$1 want=$2 re=$3 out rc
  shift 4
  out=$("$@" 2>&1)
  rc=$?
  if (( rc != want )); then bad "$name" "exit $rc, want $want; output:"$'\n'"$out"; return; fi
  if [[ -n $re ]] && ! grep -qE -- "$re" <<<"$out"; then bad "$name" "output does not match /$re/:"$'\n'"$out"; return; fi
  ok "$name"
}

# --- the fixture: a copy of the harness, a tiny Go module, a throwaway config

fix=$tmp/fixture
mkdir -p "$fix/scripts" "$fix/cmd/homebot"
cp "$root/scripts/release-test.sh" "$fix/scripts/"
cp -R "$root/scripts/release-test" "$fix/scripts/"
rm -f "$fix/scripts/release-test/selftest.sh"
cat > "$fix/go.mod" <<'GOMOD'
module example.com/fixture

go 1.26
GOMOD
cat > "$fix/cmd/homebot/main.go" <<'GOSRC'
package main

import "fmt"

var version = "dev"

func main() { fmt.Println("fixture", version) }
GOSRC
echo "fixture" > "$fix/README.md"
(cd "$fix" && git init -q -b main)
rt() { (cd "$fix" && scripts/release-test.sh --context "$ctx" "$@"); }

write_config() { # write_config [BEFORE_HOOK_SECONDS]
  {
    echo "version: 2"
    echo "project_name: homebot"
    if [[ -n ${1:-} ]]; then printf 'before:\n  hooks:\n    - sleep %s\n' "$1"; fi
    cat <<'YAML'
builds:
  - id: homebot
    main: ./cmd/homebot
    env: [CGO_ENABLED=0]
    goos: [linux]
    goarch: [amd64]
    flags: [-trimpath]
    ldflags: ["-s -w -X main.version={{ .Version }}"]
archives:
  - formats: [tar.gz]
    name_template: "homebot_{{ .Version }}_{{ .Os }}_{{ .Arch }}"
checksum:
  name_template: checksums.txt
YAML
  } > "$fix/.goreleaser.yaml"
}

# --- usage errors touch no Docker host ------------------------------------------

expect "unknown mode exits 64" 64 "unknown mode 'nope'" -- \
  "$root/scripts/release-test.sh" --context does-not-exist nope
expect "no mode exits 64" 64 "no mode given" -- \
  "$root/scripts/release-test.sh" --context does-not-exist
expect "missing --context exits 64 and names the flag" 64 "--context NAME is required" -- \
  "$root/scripts/release-test.sh" check
expect "--context without a value exits 64" 64 "--context needs a value" -- \
  "$root/scripts/release-test.sh" --context
expect "a bad --cpus exits 64" 64 "--cpus wants a number" -- \
  "$root/scripts/release-test.sh" --context x --cpus lots check
expect "an unsafe run id exits 64" 64 "RELEASE_TEST_RUN_ID" -- \
  env RELEASE_TEST_RUN_ID='a b' "$root/scripts/release-test.sh" --context x check

# The usage text says which modes `all` leaves out: the sentence about `all`
# (not the rest of the text, which names some modes for other reasons) names
# every mode tagged not-in-all, and the whole text is printed (it once stopped
# before its last line). The tag is matched as in-container.sh does.
usage_text=$("$root/scripts/release-test.sh" --help 2>&1)
all_sentence=$(tr '\n' ' ' <<<"$usage_text" | sed -n 's/.*runs every mode \(except[^)]*)\).*/\1/p')
unnamed=""
for f in "$root"/scripts/release-test/modes/*.sh; do
  if head -n 15 "$f" | grep -qE '^#[[:space:]]*release-test:.*[[:space:],:]not-in-all([[:space:],]|$)'; then
    grep -q -- "$(basename "$f" .sh)" <<<"$all_sentence" || unnamed+=" $(basename "$f" .sh)"
  fi
done
if [[ -n $all_sentence && -z $unnamed ]] && grep -q "otherwise the mode's own" <<<"$usage_text"; then
  ok "the usage names every mode that all leaves out, and ends on the exit codes"
else
  bad "the usage names every mode that all leaves out, and ends on the exit codes" "sentence: ${all_sentence:-<none>}; not named:$unnamed"$'\n'"$usage_text"
fi

id=selftest-ensure-$$; ids+=("$id")
out=$(RELEASE_TEST_RUN_ID=$id rt --ensure-image 2>&1); rc=$?
left=$(docker --context "$ctx" ps -aq --filter "label=homebot-release-test=$id"; docker --context "$ctx" volume ls -q --filter "label=homebot-release-test=$id")
if (( rc == 0 )) && [[ -z $left ]]; then ok "--ensure-image builds nothing but the image, needs no mode, starts nothing"; else bad "--ensure-image builds nothing but the image, needs no mode, starts nothing" "exit $rc"$'\n'"$out"; fi

# --- host neutrality ------------------------------------------------------------

# (the pattern is spelled in pieces so this file does not match itself)
pattern='for''ge|hyper''ion|ts\.n''et'
hits=$(cd "$root" || exit; grep -nE "$pattern" scripts/release-test.sh scripts/release-test/Dockerfile \
  scripts/release-test/*.sh scripts/release-test/modes/*.sh 2>/dev/null || true)
if [[ -z $hits ]]; then ok "the harness names no host"; else bad "the harness names a host" "$hits"; fi

# --- check ----------------------------------------------------------------------

id=selftest-check-$$; ids+=("$id")
out=$(RELEASE_TEST_RUN_ID=$id rt check 2>&1); rc=$?
if (( rc != 0 )) && grep -q '\.goreleaser\.yaml not found' <<<"$out"; then
  ok "check without a .goreleaser.yaml fails and names the file"
else
  bad "check without a .goreleaser.yaml fails and names the file" "exit $rc"$'\n'"$out"
fi

write_config
id=selftest-check2-$$; ids+=("$id")
out=$(RELEASE_TEST_RUN_ID=$id rt check 2>&1); rc=$?
if (( rc == 0 )) && grep -q 'shellcheck: .* bash and 0 sh files' <<<"$out"; then
  ok "check passes goreleaser check and shellcheck on the harness"
else
  bad "check passes goreleaser check and shellcheck on the harness" "exit $rc"$'\n'"$out"
fi

# --- snapshot -------------------------------------------------------------------

id=selftest-snap-$$; ids+=("$id")
out=$(RELEASE_TEST_RUN_ID=$id rt snapshot 2>&1); rc=$?
if (( rc == 0 )) && grep -q 'homebot_0.0.1_linux_amd64.tar.gz' <<<"$out" \
  && grep -q 'homebot_0.0.2_linux_amd64.tar.gz' <<<"$out" && grep -q 'checksums.txt' <<<"$out"; then
  ok "snapshot builds v0.0.1 and v0.0.2 from a throwaway config"
else
  bad "snapshot builds v0.0.1 and v0.0.2 from a throwaway config" "exit $rc"$'\n'"$out"
fi
left=$(docker --context "$ctx" ps -aq --filter "label=homebot-release-test=$id"; docker --context "$ctx" volume ls -q --filter "label=homebot-release-test=$id")
if [[ -z $left ]]; then ok "a finished run leaves no container and no volume"; else bad "a finished run leaves no container and no volume" "$left"; fi

id=selftest-all-$$; ids+=("$id")
out=$(RELEASE_TEST_RUN_ID=$id rt all 2>&1); rc=$?
if (( rc == 0 )) && grep -q '^PASS  check$' <<<"$out" && grep -q '^PASS  snapshot$' <<<"$out"; then
  ok "all runs check and snapshot, and sums them up"
else
  bad "all runs check and snapshot, and sums them up" "exit $rc"$'\n'"$out"
fi

# --- the sibling helpers --------------------------------------------------------

cat > "$fix/scripts/release-test/modes/siblings.sh" <<'MODE'
#!/usr/bin/env bash
set -euo pipefail
# shellcheck source-path=SCRIPTDIR source=../lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/../lib.sh"
trap rt_cleanup_siblings EXIT
rt_require_uid 1000
mkdir -p "$RT_VOL/www"
echo hello > "$RT_VOL/www/hello.txt"
rt_http_start web /release/www 8123
[[ $(rt_sibling_run --net-of web -- curl -fsS http://127.0.0.1:8123/hello.txt) == hello ]]
rt_sibling_logs web | grep -q 'GET /hello.txt'
[[ $(rt_sibling_run -- id -u) == 1000 ]]
[[ $(rt_sibling_run --user 0 -- id -u) == 0 ]]
rt_sibling_run --user 0 -- sh -c 'echo root > /release/rootfile'
[[ $(cat "$RT_VOL/rootfile") == root ]]
rt_sibling_stop web
# rt_sibling_exec: one long-lived sibling takes several commands, and what the
# first one leaves behind is there for the next.
rt_sibling_start --name box -- sleep 300
rt_sibling_exec box -- sh -c 'echo state > /tmp/state'
[[ $(rt_sibling_exec box -- cat /tmp/state) == state ]]
[[ $(rt_sibling_exec box -- id -u) == 1000 ]]
[[ $(rt_sibling_exec box --user 0 -- id -u) == 0 ]]
[[ $(rt_sibling_exec box --env RT_X=42 -- sh -c 'echo "$RT_X"') == 42 ]]
rc=0
rt_sibling_exec box -- sh -c 'exit 7' || rc=$?
[[ $rc == 7 ]]
rc=0
(rt_sibling_exec nosuch -- true) >/dev/null 2>&1 || rc=$?
[[ $rc != 0 ]]
(rt_sibling_exec box --bogus -- true) >/dev/null 2>&1 && exit 1
(rt_sibling_exec box) >/dev/null 2>&1 && exit 1
rt_sibling_stop box
echo "siblings ok"
MODE
cat > "$fix/scripts/release-test/modes/hang.sh" <<'MODE'
#!/usr/bin/env bash
set -euo pipefail
# shellcheck source-path=SCRIPTDIR source=../lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/../lib.sh"
rt_http_start web /release 8124
echo "hang ready"
sleep 300
MODE
chmod +x "$fix/scripts/release-test/modes/"*.sh

id=selftest-sib-$$; ids+=("$id")
out=$(RELEASE_TEST_RUN_ID=$id rt --siblings siblings 2>&1); rc=$?
if (( rc == 0 )) && grep -q 'siblings ok' <<<"$out"; then
  ok "siblings: a user, root, a shared volume, a local http server and exec into one container"
else
  bad "siblings: a user, root, a shared volume, a local http server and exec into one container" "exit $rc"$'\n'"$out"
fi

cat > "$fix/scripts/release-test/modes/nosib.sh" <<'MODE'
#!/usr/bin/env bash
set -euo pipefail
# shellcheck source-path=SCRIPTDIR source=../lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/../lib.sh"
# Without --siblings the Docker socket is not in the container and the helpers refuse.
[[ ! -e /var/run/docker.sock ]]
out=$(rt_sibling_run -- true 2>&1) && { echo "the helper ran without --siblings"; exit 1; }
grep -q -- '--siblings' <<<"$out"
out=$(rt_sibling_exec box -- true 2>&1) && { echo "exec ran without --siblings"; exit 1; }
grep -q -- '--siblings' <<<"$out"
# A stray fetch of this module goes direct, never through the public proxy.
[[ $(go env GOPRIVATE) == 'github.com/hyperlab-sh/*' ]]
echo "nosib ok"
MODE
cat > "$fix/scripts/release-test/modes/pin.sh" <<'MODE'
#!/usr/bin/env bash
set -euo pipefail
# shellcheck source-path=SCRIPTDIR source=../lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/../lib.sh"
out=$(_rt_sibling_parse --image debian:trixie -- true 2>&1) && { echo "an unpinned image was accepted"; exit 1; }
grep -q 'not pinned by digest' <<<"$out"
_rt_sibling_parse --image "debian:trixie@sha256:$(printf 'a%.0s' $(seq 64))" -- true
_rt_sibling_parse -- true
echo "pin ok"
MODE
chmod +x "$fix/scripts/release-test/modes/"*.sh

id=selftest-nosib-$$; ids+=("$id")
out=$(RELEASE_TEST_RUN_ID=$id rt nosib 2>&1); rc=$?
if (( rc == 0 )) && grep -q 'nosib ok' <<<"$out"; then
  ok "without --siblings: no socket, the helpers refuse, GOPRIVATE set"
else
  bad "without --siblings: no socket, the helpers refuse, GOPRIVATE set" "exit $rc"$'\n'"$out"
fi
id=selftest-pin-$$; ids+=("$id")
out=$(RELEASE_TEST_RUN_ID=$id rt --siblings pin 2>&1); rc=$?
if (( rc == 0 )) && grep -q 'pin ok' <<<"$out"; then
  ok "a sibling image must be pinned by digest, except this one"
else
  bad "a sibling image must be pinned by digest, except this one" "exit $rc"$'\n'"$out"
fi

# The check mode fails a mode file that calls docker itself.
cat > "$fix/scripts/release-test/modes/direct.sh" <<'MODE'
#!/usr/bin/env bash
# a comment mentioning docker is fine
set -euo pipefail
docker ps
MODE
id=selftest-direct-$$; ids+=("$id")
out=$(RELEASE_TEST_RUN_ID=$id rt check 2>&1); rc=$?
if (( rc != 0 )) && grep -q 'runs the Docker CLI itself' <<<"$out"; then
  ok "check fails a mode that calls docker directly"
else
  bad "check fails a mode that calls docker directly" "exit $rc"$'\n'"$out"
fi
rm -f "$fix/scripts/release-test/modes/direct.sh"

# --- the no-direct-docker lint ---------------------------------------------------
# rt_lint_no_docker catches docker in any command position, and not a mention
# in a comment, with one file or many.

lintdir=$tmp/lint
mkdir -p "$lintdir"
lint() { # lint FILE...: rt_lint_no_docker as the check mode runs it
  (
    # shellcheck source-path=SCRIPTDIR source=lib.sh
    . "$here/lib.sh"
    rt_lint_no_docker "$@" >/dev/null
  )
}
lint_hits() { # lint_hits LINE: does a mode file holding LINE fail the lint?
  printf '#!/usr/bin/env bash\n%s\n' "$1" > "$lintdir/m.sh"
  lint "$lintdir/m.sh"
}
lint_ok=1
# shellcheck disable=SC2016 # the lines are test input, not meant to expand
for line in 'docker ps' '  docker run x' 'if docker ps; then :; fi' 'x && docker ps' 'x || docker ps' \
  'x; docker ps' 'x | docker load' 'y=$(docker ps)' 'y=`docker ps`' '! docker ps' 'command docker ps' \
  'exec docker ps' 'timeout 5 docker ps' 'xargs docker rm' 'sudo docker ps' 'f() { docker ps; }' \
  'while docker ps; do :; done' 'then docker ps' 'docker'; do
  if lint_hits "$line"; then bad "the lint misses: $line"; lint_ok=0; fi
done
# shellcheck disable=SC2016 # test input again
for line in '# docker ps is not called here' '   # docker' 'echo "$RT_DOCKER_HOST"' 'cat /var/run/docker.sock' \
  'rt_sibling_run -- true' 'x=./docker-compose' 'echo Docker' 'dockerd-rootless'; do
  if ! lint_hits "$line"; then bad "the lint trips on: $line"; lint_ok=0; fi
done
# One file, and a comment that mentions docker: no false positive.
printf '# docker is only mentioned here\ntrue\n' > "$lintdir/c.sh"
if ! lint "$lintdir/c.sh"; then bad "the lint trips on a comment in a single file"; lint_ok=0; fi
if (( lint_ok )); then ok "the no-direct-docker lint: command positions caught, comments and lookalikes not"; fi

# --- not-in-all -----------------------------------------------------------------
# A mode with `# release-test: not-in-all` in its header stays out of `all` and
# still runs by name. The tag only counts in the header, and only as that word.

fix2=$tmp/fixture2
mkdir -p "$fix2/scripts"
cp "$root/scripts/release-test.sh" "$fix2/scripts/"
cp -R "$root/scripts/release-test" "$fix2/scripts/"
rm -f "$fix2/scripts/release-test/selftest.sh" "$fix2/scripts/release-test/modes/"*.sh
mk_mode() { # mk_mode NAME BODY...
  local n=$1
  shift
  printf '%s\n' '#!/usr/bin/env bash' "$@" > "$fix2/scripts/release-test/modes/$n.sh"
}
mk_mode light 'echo "light ran"'
mk_mode heavy '# release-test: not-in-all' 'echo "heavy ran"'
mk_mode multi '# release-test: slow, not-in-all' 'echo "multi ran"'
mk_mode other '# release-test: slow' 'echo "other ran"'
mk_mode lookalike '# release-test: not-in-all-ever' 'echo "lookalike ran"'
mk_mode deep 'true' 'true' 'true' 'true' 'true' 'true' 'true' 'true' 'true' 'true' 'true' 'true' 'true' 'true' 'true' 'true' '# release-test: not-in-all' 'echo "deep ran"'
(cd "$fix2" && git init -q -b main)
rt2() { (cd "$fix2" && scripts/release-test.sh --context "$ctx" "$@"); }
id=selftest-notall-$$; ids+=("$id")
out=$(RELEASE_TEST_RUN_ID=$id rt2 all 2>&1); rc=$?
if (( rc == 0 )) && grep -q 'PASS  light' <<<"$out" && grep -q 'PASS  other' <<<"$out" \
  && grep -q 'PASS  lookalike' <<<"$out" && grep -q 'PASS  deep' <<<"$out" \
  && ! grep -qE 'heavy|multi' <<<"$out"; then
  ok "all skips the modes tagged not-in-all (in the header, as a whole word) and runs the rest"
else
  bad "all skips the modes tagged not-in-all" "exit $rc"$'\n'"$out"
fi
id=selftest-heavy-$$; ids+=("$id")
out=$(RELEASE_TEST_RUN_ID=$id rt2 heavy 2>&1); rc=$?
if (( rc == 0 )) && grep -q 'heavy ran' <<<"$out"; then
  ok "a mode tagged not-in-all runs by name"
else
  bad "a mode tagged not-in-all runs by name" "exit $rc"$'\n'"$out"
fi

# --- Ctrl-C ---------------------------------------------------------------------
# interrupt MODE ID READY_RE: starts the run under job control (so SIGINT is not
# ignored), waits for READY_RE in its output, sends SIGINT and checks that the
# script exits 130 and that no container and no volume of the run remain.

interrupt() {
  local mode=$1 id=$2 ready=$3 log pid rc left
  local flags=("${@:4}")
  log=$tmp/$id.log
  ids+=("$id")
  set -m
  (cd "$fix" && RELEASE_TEST_RUN_ID=$id exec scripts/release-test.sh --context "$ctx" "${flags[@]+"${flags[@]}"}" "$mode") >"$log" 2>&1 &
  pid=$!
  set +m
  for _ in $(seq 1 240); do
    if grep -qE -- "$ready" "$log"; then break; fi
    if ! kill -0 "$pid" 2>/dev/null; then break; fi
    sleep 0.5
  done
  if ! kill -0 "$pid" 2>/dev/null; then
    bad "Ctrl-C during $mode" "the run ended before it could be interrupted:"$'\n'"$(cat "$log")"
    return
  fi
  kill -INT "$pid"
  for _ in $(seq 1 120); do
    if ! kill -0 "$pid" 2>/dev/null; then break; fi
    sleep 0.5
  done
  if kill -0 "$pid" 2>/dev/null; then
    kill -KILL "$pid" 2>/dev/null
    bad "Ctrl-C during $mode" "the script did not exit within 60s"
    return
  fi
  wait "$pid"; rc=$?
  left=$(docker --context "$ctx" ps -aq --filter "label=homebot-release-test=$id"; docker --context "$ctx" volume ls -q --filter "label=homebot-release-test=$id")
  if (( rc == 130 )) && [[ -z $left ]]; then
    ok "Ctrl-C during $mode leaves no container and no volume"
  else
    bad "Ctrl-C during $mode leaves no container and no volume" "exit $rc; left: $left"
  fi
}

write_config 120
interrupt snapshot "selftest-int1-$$" 'make-snapshots: building v0.0.1|running 1 hooks|sleep 120'
interrupt hang "selftest-int2-$$" 'hang ready' --siblings

# An interrupt during the image build kills the docker client that runs it, not
# only the shell around it: with the build lock released by the wrapper, a
# surviving client would go on building without limits. A Dockerfile that
# differs from the cached image forces a build, and a RUN that sleeps keeps it
# going.
printf '\nRUN sleep 200\n' >> "$fix/scripts/release-test/Dockerfile"
if command -v sha256sum >/dev/null 2>&1; then
  dfhash=$(sha256sum "$fix/scripts/release-test/Dockerfile" | cut -c1-12)
else
  dfhash=$(shasum -a 256 "$fix/scripts/release-test/Dockerfile" | cut -c1-12)
fi
interrupt check "selftest-int3-$$" 'building homebot-release-test:'
sleep 2
if stray=$(pgrep -f "build --tag homebot-release-test:$dfhash" || true) && [[ -n $stray ]]; then
  # shellcheck disable=SC2086 # one pid per word
  kill $stray 2>/dev/null
  bad "Ctrl-C during the image build kills the docker client" "still running: $stray"
else
  ok "Ctrl-C during the image build kills the docker client"
fi
cp "$root/scripts/release-test/Dockerfile" "$fix/scripts/release-test/Dockerfile"

# --- shellcheck on the harness itself ---------------------------------------------

if command -v shellcheck >/dev/null 2>&1; then
  sc=$(cd "$root" && shellcheck -x scripts/release-test.sh scripts/release-test/*.sh scripts/release-test/modes/*.sh 2>&1)
  if [[ -z $sc ]]; then ok "shellcheck is clean"; else bad "shellcheck is clean" "$sc"; fi
else
  echo "SKIP  shellcheck is not installed here; the check mode runs it in the container"
fi

printf '\n%d passed, %d failed\n' "$pass" "$fail"
(( fail == 0 ))
