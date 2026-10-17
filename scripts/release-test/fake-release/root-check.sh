#!/usr/bin/env bash
# A disposable root sibling proves that root's own HOME is allowed while
# sudo/another user's HOME are refused, even with --bin-dir.
set -euo pipefail
[[ $(id -u) == 0 ]] || { echo 'root-check must run as root' >&2; exit 1; }
base=http://127.0.0.1:8026
src=/release/install-test/install.sh
foreign=/release/install-test/case-sh/home
root_home=/release/install-test/root-home
mkdir -p "$root_home"
chown 0:0 "$root_home"
mkdir -p /release/install-test/root-stubs
ln -sf /release/install-test/cosign /release/install-test/root-stubs/cosign
export PATH=/release/install-test/root-stubs:/usr/bin:/bin
export FAKE_COSIGN_ARGS=/release/install-test/root-cosign-args
args=(--base-url "$base" --version 0.0.2 --bin-dir /release/install-test/root-bin)
got=0
HOME=$foreign /bin/sh "$src" "${args[@]}" >/dev/null 2>&1 || got=$?
[[ $got == 7 && ! -e /release/install-test/root-bin/homebot ]]
got=0
HOME=$foreign /bin/sh "$src" --base-url "$base" --version 0.0.2 \
  --bin-dir /release/install-test/other-bin >/dev/null 2>&1 || got=$?
[[ $got == 7 && ! -e /release/install-test/other-bin/homebot ]]
foreign_target=/release/install-test/foreign-home-target
foreign_link=/release/install-test/root-owned-home-link
mkdir -p "$foreign_target"
chown 1000:1000 "$foreign_target"
ln -s "$foreign_target" "$foreign_link"
# shellcheck disable=SC2012 # Prove ls sees the root-owned link itself.
[[ $(ls -ldn "$foreign_link" | awk '{print $3}') == 0 ]]
got=0
HOME=$foreign_link /bin/sh "$src" --base-url "$base" --version 0.0.2 \
  --bin-dir "$foreign_target/.local/bin" >/dev/null 2>&1 || got=$?
[[ $got == 7 && ! -e $foreign_target/.local ]]
got=0
HOME=$root_home SUDO_USER=tester /bin/sh "$src" "${args[@]}" >/dev/null 2>&1 || got=$?
[[ $got == 7 ]]
HOME=$root_home SUDO_USER=root /bin/sh "$src" "${args[@]}" >/dev/null
[[ -x /release/install-test/root-bin/homebot ]]

# Use Debian's real sudo/env_reset, not a SUDO_USER simulation: piping to
# sudo sh must refuse before creating anything in root's or tester's HOME.
apt-get update -qq
DEBIAN_FRONTEND=noninteractive apt-get install -y -qq --no-install-recommends sudo >/dev/null
printf 'tester ALL=(ALL) NOPASSWD:ALL\n' >/etc/sudoers.d/homebot-release-test
chmod 0440 /etc/sudoers.d/homebot-release-test
got=0
/usr/sbin/runuser -u tester -- /bin/sh -c \
  'cat /release/install-test/install.sh | sudo sh -s -- --base-url http://127.0.0.1:8026 --version 0.0.2 --bin-dir /release/install-test/sudo-bin' \
  >/dev/null 2>&1 || got=$?
[[ $got == 7 && ! -e /release/install-test/sudo-bin/homebot ]]
[[ ! -e /root/.local/bin/homebot ]]
echo 'install-sh: root ownership passed'
