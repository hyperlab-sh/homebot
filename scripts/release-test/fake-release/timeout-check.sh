#!/usr/bin/env bash
# Runs in a disposable sibling sharing the slow server's network namespace.
set -euo pipefail
[[ $(id -u) == 1000 ]] || { echo 'timeout-check must run as tester' >&2; exit 1; }
root=/release/install-test/timeout-$INSTALL_SHELL
src=/release/install-test/install.sh
mkdir -p "$root/home" "$root/bin" "$root/tmp"
export HOME=$root/home TMPDIR=$root/tmp
base=http://127.0.0.1:8027
case $INSTALL_SHELL in
  sh) shell_cmd=(/bin/sh) ;;
  dash) shell_cmd=(/bin/dash) ;;
  bash) shell_cmd=(/bin/bash --posix) ;;
  busybox) shell_cmd=(/bin/busybox sh) ;;
esac
started=$SECONDS
got=0
"${shell_cmd[@]}" "$src" --base-url "$base" --version 0.0.2 \
  --bin-dir "$root/bin" --max-time 3 >"$root/out" 2>"$root/err" || got=$?
[[ $got == 3 ]] || { echo "timeout exit $got, wanted 3" >&2; cat "$root/err" >&2; exit 1; }
(( SECONDS - started < 25 )) || { echo 'download exceeded retry bound' >&2; exit 1; }
[[ ! -e $root/bin/homebot && -z $(find "$root/tmp" -mindepth 1 -print) ]]

# SIGTERM during the blocked request removes the mktemp directory. An old
# binary in the target folder is never touched.
printf 'old binary\n' >"$root/bin/homebot"
old=$(sha256sum "$root/bin/homebot" | awk '{print $1}')
got=0
timeout -s TERM -k 2s 2s "${shell_cmd[@]}" "$src" --base-url "$base" \
  --version 0.0.2 --bin-dir "$root/bin" --max-time 120 \
  >"$root/term-out" 2>"$root/term-err" || got=$?
[[ $got != 0 ]]
[[ $(sha256sum "$root/bin/homebot" | awk '{print $1}') == "$old" ]]
[[ -z $(find "$root/tmp" -mindepth 1 -print) ]]
[[ -z $(find "$root/bin" -maxdepth 1 -name '.homebot.new.*' -print) ]]
echo "install-sh: $INSTALL_SHELL timeout and download interrupt passed"
