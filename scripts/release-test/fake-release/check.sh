#!/usr/bin/env bash
# Runs in a client sibling sharing the fake HTTP server's network namespace.
set -euo pipefail
base=http://127.0.0.1:8026
fix=/release/install-test
src=$fix/install.sh
root=$fix/case-$INSTALL_SHELL
rm -rf "$root"
mkdir -p "$root/home" "$root/bin" "$root/tmp" "$root/stubs"
export HOME=$root/home TMPDIR=$root/tmp XDG_DATA_HOME=$root/home/.local/share
export PATH=$root/stubs:/usr/bin:/bin
export FAKE_COSIGN_ARGS=$root/cosign-args
ln -s "$fix/cosign" "$root/stubs/cosign"

run() {
  case $INSTALL_SHELL in
    sh) /bin/sh "$src" "$@" ;;
    dash) /bin/dash "$src" "$@" ;;
    bash) /bin/bash --posix "$src" "$@" ;;
    busybox) /bin/busybox sh "$src" "$@" ;;
  esac
}
expect() {
  local want=$1 got=0
  shift
  run "$@" >"$root/out" 2>"$root/err" || got=$?
  [[ $got == "$want" ]] || { echo "exit $got, wanted $want: $*"; cat "$root/err"; exit 1; }
}
fresh() {
  rm -rf "${root:?}/home/.local" "${root:?}/bin" "${root:?}/tmp"/*
  mkdir -p "$root/bin"
}
args=(--base-url "$base" --bin-dir "$root/bin")

# A truncated pipe cannot reach the final main call, even when cut in a body.
last=$(wc -l <"$src")
for cut in 4 17 85 $((last - 1)); do
  fresh
  head -n "$cut" "$src" | /bin/sh >/dev/null 2>&1 || true
  [[ ! -e $root/home/.local && ! -e $root/bin/homebot ]]
done

# Latest, pinned with and without v, prerelease and receipt.
expect 0 "${args[@]}"
[[ $("$root/bin/homebot" --version) == 'voiced version' ]]
[[ $(HOMEBOT_PLAIN=1 "$root/bin/homebot" --version) == 0.0.2 ]]
grep -qx "bin=$root/bin/homebot" "$HOME/.local/share/homebot/install-receipt"
grep -qx 'version=0.0.2' "$HOME/.local/share/homebot/install-receipt"
grep -qx 'method=script' "$HOME/.local/share/homebot/install-receipt"
[[ $(wc -l <"$FAKE_COSIGN_ARGS") == 7 ]]
[[ $(sed -n '1p' "$FAKE_COSIGN_ARGS") == --certificate-identity ]]
[[ $(sed -n '2p' "$FAKE_COSIGN_ARGS") == 'https://github.com/hyperlab-sh/homebot/.github/workflows/release.yml@refs/tags/v0.0.2' ]]
[[ $(sed -n '3p' "$FAKE_COSIGN_ARGS") == --certificate-oidc-issuer ]]
[[ $(sed -n '4p' "$FAKE_COSIGN_ARGS") == https://token.actions.githubusercontent.com ]]
[[ $(sed -n '5p' "$FAKE_COSIGN_ARGS") == --bundle ]]
[[ $(sed -n '6p' "$FAKE_COSIGN_ARGS") == */checksums.txt.sigstore.json ]]
[[ $(sed -n '7p' "$FAKE_COSIGN_ARGS") == */checksums.txt ]]
if grep -q -- '--new-bundle-format' "$FAKE_COSIGN_ARGS"; then exit 1; fi
expect 0 "${args[@]}" --version 0.0.1
[[ $(HOMEBOT_PLAIN=1 "$root/bin/homebot" --version) == 0.0.1 ]]
expect 0 "${args[@]}" --version v0.0.2
expect 0 "${args[@]}" --version 0.0.2-rc.1
[[ $(HOMEBOT_PLAIN=1 "$root/bin/homebot" --version) == 0.0.2-rc.1 ]]
expect 0 "${args[@]}" --version v0.0.2 # idempotent

# Rename-over-install keeps a running old executable's inode alive.
cat >"$root/bin/homebot" <<EOF
#!/bin/sh
echo started >"$root/old-started"
sleep 2
echo old-finished >"$root/old-finished"
EOF
chmod 0755 "$root/bin/homebot"
"$root/bin/homebot" --version & old_pid=$!
for _ in 1 2 3 4 5 6 7 8 9 10; do
  [[ -f $root/old-started ]] && break
  sleep 0.1
done
[[ -f $root/old-started ]]
expect 0 "${args[@]}" --version 0.0.2
wait "$old_pid"
[[ -f $root/old-finished ]]

# Failed smoke tests cannot replace the installed binary or leave the staged
# same-filesystem file behind. A symlink archive member is never installed.
before=$(sha256sum "$root/bin/homebot" | awk '{print $1}')
expect 6 "${args[@]}" --version 0.0.3
[[ $(sha256sum "$root/bin/homebot" | awk '{print $1}') == "$before" ]]
[[ -z $(find "$root/bin" -maxdepth 1 -name '.homebot.new.*' -print) ]]
expect 6 "${args[@]}" --version 0.0.4
[[ $(sha256sum "$root/bin/homebot" | awk '{print $1}') == "$before" ]]
expect 5 "${args[@]}" --version 0.0.5
[[ $(sha256sum "$root/bin/homebot" | awk '{print $1}') == "$before" ]]
expect 0 "${args[@]}" --version 0.0.7
[[ ! -e $root/tmp/evil ]]
expect 0 "${args[@]}" --version 0.0.2
case $INSTALL_SHELL in
  sh) shell_cmd=(/bin/sh) ;;
  dash) shell_cmd=(/bin/dash) ;;
  bash) shell_cmd=(/bin/bash --posix) ;;
  busybox) shell_cmd=(/bin/busybox sh) ;;
esac
got=0
timeout -s TERM -k 2s 2s "${shell_cmd[@]}" "$src" "${args[@]}" --version 0.0.6 \
  >"$root/interrupt-out" 2>"$root/interrupt-err" || got=$?
[[ $got != 0 ]]
[[ $(sha256sum "$root/bin/homebot" | awk '{print $1}') == "$before" ]]
[[ -z $(find "$root/bin" -maxdepth 1 -name '.homebot.new.*' -print) ]]

# A corrupt archive and a missing exact-name line both fail verification.
asset=/release/install-site/releases/download/v0.0.2/homebot_0.0.2_linux_amd64.tar.gz
cp "$asset" "$root/archive.backup"
printf x >>"$asset"
expect 5 "${args[@]}" --version 0.0.2
cp "$root/archive.backup" "$asset"
checksums=/release/install-site/releases/download/v0.0.2/checksums.txt
cp "$checksums" "$root/checksums.backup"
sed '/homebot_0\.0\.2_linux_amd64\.tar\.gz/d' "$checksums" >"$checksums.tmp"
mv "$checksums.tmp" "$checksums"
expect 5 "${args[@]}" --version 0.0.2
cp "$root/checksums.backup" "$checksums"

export FAKE_COSIGN_VERSION=v2.4.3
expect 0 "${args[@]}" --version 0.0.2
grep -qx -- '--new-bundle-format' "$FAKE_COSIGN_ARGS"
[[ $(wc -l <"$FAKE_COSIGN_ARGS") == 8 ]]
export FAKE_COSIGN_VERSION=v2.4.2
expect 4 "${args[@]}" --version 0.0.2 --require-signature
expect 0 "${args[@]}" --version 0.0.2
grep -q 'cosign 2.4.3 or newer' "$root/err"
export FAKE_COSIGN_VERSION=v2.4.0
expect 4 "${args[@]}" --version 0.0.2 --require-signature
export FAKE_COSIGN_VERSION=v2.3.9
expect 4 "${args[@]}" --version 0.0.2 --require-signature
expect 0 "${args[@]}" --version 0.0.2
grep -q 'signature not checked' "$root/err"
export FAKE_COSIGN_VERSION=unknown
expect 4 "${args[@]}" --version 0.0.2 --require-signature
export FAKE_COSIGN_VERSION=v2.4.0-rc.1
expect 4 "${args[@]}" --version 0.0.2 --require-signature
unset FAKE_COSIGN_VERSION
export FAKE_COSIGN_FAIL=1
expect 5 "${args[@]}" --version 0.0.2
unset FAKE_COSIGN_FAIL
expect 3 "${args[@]}" --version 9.9.9

# Invalid versions are rejected before any release URL is requested.
for value in ../evil 'a b' 0.1 1.2.3-beta; do
  expect 1 "${args[@]}" --version "$value"
done
expect 1 "${args[@]}" --version $'0.0.2\n../evil'
# None of these unsafe HTTP authorities may make even one network request.
cat >"$root/stubs/curl" <<'EOF'
#!/bin/sh
printf 'called\n' >>"${FAKE_CURL_CALLS:?}"
exec /usr/bin/curl "$@"
EOF
chmod +x "$root/stubs/curl"
export FAKE_CURL_CALLS=$root/unsafe-curl-calls
for bad_base in \
  http://example.com \
  http://127.0.0.1:80@remote.invalid \
  http://localhost:80@remote.invalid \
  http://127.0.0.1: \
  http://localhost:abc \
  http://127.0.0.1:65536 \
  http://localhost:80?x \
  http://localhost:80#fragment; do
  expect 1 --base-url "$bad_base" --version 0.0.2
done
[[ ! -e $FAKE_CURL_CALLS ]]
rm "$root/stubs/curl"
expect 1 "${args[@]}" --max-time nope --version 0.0.2
expect 1 "${args[@]}" --version 0.0.2 --bogus

# A malformed latest entry is rejected before a version can enter a URL.
latest=/release/install-site/releases/latest/download/checksums.txt
cp "$latest" "$root/latest.backup"
for bad in ..evil 'a b' 0.1 1.2.3-beta; do
  sed "s/homebot_0\\.0\\.2_linux_amd64\\.tar\\.gz/homebot_${bad}_linux_amd64.tar.gz/" \
    "$root/latest.backup" >"$latest.tmp"
  mv "$latest.tmp" "$latest"
  expect 1 "${args[@]}"
done
cp "$root/latest.backup" "$latest"

# Curl receives the timeout/retry flags. A PATH with only BusyBox wget as
# downloader must work; with neither downloader the script returns usage 1.
cat >"$root/stubs/curl" <<'EOF'
#!/bin/sh
printf '%s\n' "$@" >"${FAKE_CURL_ARGS:?}"
exec /usr/bin/curl "$@"
EOF
chmod +x "$root/stubs/curl"
export FAKE_CURL_ARGS=$root/curl-args
expect 0 "${args[@]}" --max-time 3 --version 0.0.2
grep -qx -- '--connect-timeout' "$FAKE_CURL_ARGS"
grep -qx -- '--max-time' "$FAKE_CURL_ARGS"
grep -qx -- '--retry' "$FAKE_CURL_ARGS"
grep -qx 3 "$FAKE_CURL_ARGS"
rm "$root/stubs/curl"
mkdir -p "$root/minimal"
for tool in id ls awk uname grep mktemp sed head rm mkdir tar gzip cp chmod mv date sha256sum cat; do
  ln -s "$(command -v "$tool")" "$root/minimal/$tool"
done
ln -s "$fix/cosign" "$root/minimal/cosign"
ln -s /bin/busybox "$root/minimal/wget"
saved_path=$PATH
# shellcheck disable=SC2123 # Deliberately hide curl to exercise BusyBox wget.
PATH=$root/minimal
expect 0 "${args[@]}" --version 0.0.2
rm "$root/minimal/cosign"
expect 4 "${args[@]}" --version 0.0.2 --require-signature
expect 0 "${args[@]}" --version 0.0.2
grep -q 'signature not checked' "$root/err"
rm "$root/minimal/wget"
expect 1 "${args[@]}" --version 0.0.2
grep -q 'curl or wget is required' "$root/err"
PATH=$saved_path

# Platform outcomes are driven by the same PATH calls as production.
cat >"$root/stubs/uname" <<'EOF'
#!/bin/sh
case $1 in -s) echo "${FAKE_OS:-Linux}" ;; -m) echo "${FAKE_ARCH:-x86_64}" ;; esac
EOF
cat >"$root/stubs/sysctl" <<'EOF'
#!/bin/sh
[ "${FAKE_SYSCTL_FAIL:-0}" = 0 ] || exit 1
echo "${FAKE_TRANSLATED:-0}"
EOF
chmod +x "$root/stubs/uname" "$root/stubs/sysctl"
for os in Linux Darwin; do
  for arch in x86_64 arm64; do
    export FAKE_OS=$os FAKE_ARCH=$arch
    expect 0 "${args[@]}" --version 0.0.2
  done
done
export FAKE_OS=Darwin FAKE_ARCH=x86_64 FAKE_TRANSLATED=1
expect 0 "${args[@]}" --version 0.0.2
export FAKE_SYSCTL_FAIL=1
expect 0 "${args[@]}" --version 0.0.2
unset FAKE_SYSCTL_FAIL FAKE_TRANSLATED
export FAKE_OS=FreeBSD
expect 2 "${args[@]}" --version 0.0.2
export FAKE_OS=Linux
for arch in i686 armv7l; do
  export FAKE_ARCH=$arch
  expect 2 "${args[@]}" --version 0.0.2
done
unset FAKE_OS FAKE_ARCH

# Sudo refusal, absent HOME, path with spaces, and relative XDG fallback.
export SUDO_USER=tester
expect 7 "${args[@]}" --version 0.0.2
unset SUDO_USER
mkdir -p "$root/unwritable"
chmod 0555 "$root/unwritable"
expect 7 --base-url "$base" --bin-dir "$root/unwritable" --version 0.0.2
chmod 0755 "$root/unwritable"
unset HOME
expect 7 "${args[@]}" --version 0.0.2
export HOME=$root/home
bin_spaces="$root/bin with spaces"
expect 0 --base-url "$base" --bin-dir "$bin_spaces" --version 0.0.2
[[ -x $bin_spaces/homebot ]]
XDG_DATA_HOME=relative expect 0 "${args[@]}" --version 0.0.2
[[ -f $HOME/.local/share/homebot/install-receipt ]]
echo "install-sh: $INSTALL_SHELL passed"
