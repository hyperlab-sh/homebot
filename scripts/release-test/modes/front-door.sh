#!/usr/bin/env bash
# front-door: the install script served at https://homebot.sh/install
# (docs/landing/install.sh).
#
#   front-door.sh          offline and fast: shellcheck, then the stubbed cases
#                          (scripts/release-test/front-door/run-stubbed.sh) in a
#                          Debian sibling, as tester and as root. `all` runs this.
#   front-door.sh --live   also, because they need the network and take a few
#                          minutes, so they are opt-in:
#                          - the stubbed cases again on bash 3.2.57 (the
#                            `bash:3.2` image, with python3, git, coreutils,
#                            util-linux and GNU grep added by apk), the oldest bash the
#                            script promises to work with (macOS /bin/bash);
#                          - the script for real, once: a fresh Debian with a
#                            sudo user and no git, no compiler, no Homebrew,
#                            against GitHub. With a release out: real
#                            Homebrew, real `brew install git age gitleaks`,
#                            the real release installer, and `homebot --help`
#                            in a new login shell. With no release yet it
#                            checks the refusal instead: GitHub's real 404,
#                            exit 1, nothing installed or edited.
#
# Not covered, on purpose or for now: macOS itself (BSD sed, readlink and ls are
# read, never run; the Darwin paths run only against stubs), the interactive
# Homebrew installer with a real terminal, the release installer's own checks
# (the install-sh mode covers them), and a release that is signed.
#
# Never runs the script on the machine that runs this: only in siblings.
set -euo pipefail
# shellcheck source-path=SCRIPTDIR source=../lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/../lib.sh"
cd "$RT_SRC"

# Debian 13 slim, by digest (the sibling helpers insist on a pinned image).
LIVE_IMAGE=debian:trixie-slim@sha256:a29215f6a35e51e22adffa17f89e9d2ef06214e64a2bad10d765c46aea49f11f
# Alpine with bash 3.2.57 as the only bash.
BASH32_IMAGE=bash:3.2@sha256:0fd7cb8499c63a3c9345e7088a9cd83bb69f6e895e83833859aff838a0312091

live=0
for a in "$@"; do
  case $a in
    --live) live=1 ;;
    *) rt_die "front-door: unknown argument '$a' (only --live)" ;;
  esac
done

trap 'rt_cleanup_siblings' EXIT

script=docs/landing/install.sh
fixtures=scripts/release-test/front-door
[[ -f $script ]] || rt_die "front-door: $script not found"

echo "--- shellcheck"
stubs=()
for f in "$fixtures"/stubs/*; do stubs+=("$f"); done
shellcheck -x -s bash "$script" "$fixtures"/*.sh "${stubs[@]}"
python3 -c 'import ast, sys; ast.parse(open(sys.argv[1]).read())' "$fixtures/pty-run.py"
echo "ok"

# The script and the fixtures go into the run's volume, where the siblings see
# them at /release.
stage=$RT_VOL/front-door
rm -rf "$stage"
mkdir -p "$stage/fd"
cp "$script" "$stage/install.sh"
cp -R "$fixtures"/. "$stage/fd/"

echo "--- stubbed cases"
rt_sibling_start --name fd-stub --user root -- sleep infinity
# tester must be able to play Homebrew's prefixes and the distro's os-release.
rt_sibling_exec fd-stub --user root -- bash -c '
  mkdir -p /home/linuxbrew /opt/homebrew
  chown tester /home/linuxbrew /opt/homebrew /usr/local/bin
  chmod 666 /etc/os-release'
rc=0
rt_sibling_exec fd-stub --user tester --env FD_INSTALL=/release/front-door/install.sh -- \
  bash /release/front-door/fd/run-stubbed.sh || rc=$?
echo "--- as root"
rt_sibling_exec fd-stub --user root --env FD_INSTALL=/release/front-door/install.sh -- \
  bash /release/front-door/fd/run-stubbed.sh root || rc=$?
rt_sibling_stop fd-stub
if (( rc != 0 )); then rt_die "front-door: stubbed cases failed"; fi

if (( ! live )); then
  echo "front-door: stubbed cases passed (run with --live for the real install)"
  exit 0
fi

# --- live ----------------------------------------------------------------------
echo "--- bash 3.2: the stubbed cases again"
rt_sibling_start --name fd-b32 --image "$BASH32_IMAGE" --user root -- sleep infinity
# shellcheck disable=SC2016 # the script is for the sibling's shell
rt_sibling_exec fd-b32 --user root -- sh -c '
  set -e
  apk add --no-cache python3 git coreutils util-linux grep >/dev/null
  ln -sf /usr/local/bin/bash /bin/bash
  [ -x /usr/bin/uname ] || ln -s "$(command -v uname)" /usr/bin/uname
  adduser -D -s /bin/bash tester
  mkdir -p /home/linuxbrew /opt/homebrew
  chown tester /home/linuxbrew /opt/homebrew /usr/local/bin
  chmod 666 /etc/os-release
  /bin/bash --version | head -n 1 | grep -q "version 3\.2\."'
rc32=0
rt_sibling_exec fd-b32 --user tester --env FD_INSTALL=/release/front-door/install.sh -- \
  bash /release/front-door/fd/run-stubbed.sh || rc32=$?
rt_sibling_stop fd-b32
if (( rc32 != 0 )); then rt_die "front-door: the stubbed cases failed on bash 3.2"; fi

echo "--- live: fresh Debian, real GitHub"
t0=$(date +%s)
rt_sibling_start --name fd-live --image "$LIVE_IMAGE" --user root -- sleep infinity
rt_sibling_exec fd-live --user root -- bash -c '
  set -e
  apt-get update -qq
  apt-get install -y -qq --no-install-recommends sudo curl ca-certificates >/dev/null
  useradd --create-home --shell /bin/bash dev
  echo "dev ALL=(ALL) NOPASSWD:ALL" >/etc/sudoers.d/dev
  chmod 0440 /etc/sudoers.d/dev
  if command -v git >/dev/null; then echo "this image has git; the live case wants none" >&2; exit 1; fi'
t1=$(date +%s)
echo "    machine ready in $((t1 - t0)) s"

dev_env=(--env HOME=/home/dev --env USER=dev --env SHELL=/bin/bash)
rc=0
out=$(rt_sibling_exec fd-live --user dev "${dev_env[@]}" -- \
  bash /release/front-door/install.sh --yes --no-setup 2>&1) || rc=$?
printf '%s\n' "$out"
if (( rc != 0 )); then
  # No release yet: the real 404 must stop the run before anything changes.
  if (( rc == 1 )) && grep -q 'homebot has no release yet' <<<"$out"; then
    # shellcheck disable=SC2016 # for the sibling's shell
    rt_sibling_exec fd-live --user dev "${dev_env[@]}" -- bash -c '
      set -e
      if [ -e ~/.local ] || grep -qs "brew shellenv" ~/.bashrc || [ -e /home/linuxbrew ] || command -v brew; then
        echo "the refusal changed something"; exit 1
      fi'
    rt_sibling_stop fd-live
    echo "front-door: live case passed: there is no release yet, and the refusal changed nothing"
    exit 0
  fi
  rt_die "front-door: the live install failed (exit $rc)"
fi
t2=$(date +%s)
echo "    install took $((t2 - t1)) s"

echo "--- live: a new login shell finds homebot"
rt_sibling_exec fd-live --user dev "${dev_env[@]}" -- bash -lic 'command -v homebot && homebot --help | head -5'
echo "--- live: the PATH line is in .bashrc once, and a second run changes nothing"
# The script is for the sibling's shell: it must not expand here.
# shellcheck disable=SC2016
rt_sibling_exec fd-live --user dev "${dev_env[@]}" -- bash -c '
  set -e
  test "$(grep -c "brew shellenv" ~/.bashrc)" = 1
  test "$(grep -c "\.local/bin" ~/.bashrc)" = 1
  sum=$(cksum <~/.bashrc)
  bash /release/front-door/install.sh --yes --no-setup >/dev/null
  test "$(cksum <~/.bashrc)" = "$sum"
  echo "unchanged"'
t3=$(date +%s)
echo "front-door: live case passed (machine $((t1 - t0)) s, install $((t2 - t1)) s, checks $((t3 - t2)) s, total $((t3 - t0)) s)"
rt_sibling_stop fd-live
