#!/usr/bin/env bash
# gosum: prove checkout builds have no Main.Sum and module-download builds do.
# This mode runs only inside the release-test container's throwaway filesystem.
set -euo pipefail
# shellcheck source-path=SCRIPTDIR source=../lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/../lib.sh"

tmp=$(mktemp -d)
cleanup() {
  # Go keeps downloaded module files read-only, including directories.
  chmod -R u+w "$tmp/modcache" 2>/dev/null || true
  rm -rf "$tmp"
}
trap cleanup EXIT
mkdir -p "$tmp/src" "$tmp/bin" "$tmp/gopath" "$tmp/modcache" "$tmp/gocache"
cp -a "$RT_SRC/." "$tmp/src/"
cd "$tmp/src"
export GIT_CONFIG_GLOBAL="$tmp/gitconfig" GIT_CONFIG_SYSTEM=/dev/null
export GIT_AUTHOR_NAME='homebot test' GIT_AUTHOR_EMAIL='test@example.invalid'
export GIT_COMMITTER_NAME="$GIT_AUTHOR_NAME" GIT_COMMITTER_EMAIL="$GIT_AUTHOR_EMAIL"
export GOPATH="$tmp/gopath" GOMODCACHE="$tmp/modcache" GOCACHE="$tmp/gocache"
export GOBIN="$tmp/bin" GOPRIVATE=github.com/hyperlab-sh/homebot
export GOPROXY=https://proxy.golang.org,direct
git init -q -b main
git add -A
git commit -qm 'throwaway release source'
git tag v0.0.1

echo '--- checkout build has no Main.Sum'
go install ./cmd/homebot
go version -m "$GOBIN/homebot" >"$tmp/local-build.txt"
if grep -Eq '^[[:space:]]*mod[[:space:]]+github.com/hyperlab-sh/homebot[[:space:]]+.*h1:' "$tmp/local-build.txt"; then
  rt_die 'checkout build unexpectedly has a main-module sum'
fi

echo '--- module download carries Main.Sum'
git config --global "url.file://$tmp/src.insteadOf" 'https://github.com/hyperlab-sh/homebot'
rm -f "$GOBIN/homebot"
cd "$tmp"
go install github.com/hyperlab-sh/homebot/cmd/homebot@v0.0.1
go version -m "$GOBIN/homebot" >"$tmp/module-build.txt"
if ! grep -Eq '^[[:space:]]*mod[[:space:]]+github.com/hyperlab-sh/homebot[[:space:]]+v0\.0\.1[[:space:]]+h1:' "$tmp/module-build.txt"; then
  cat "$tmp/module-build.txt"
  rt_die 'module-download build lacks a main-module sum'
fi
echo 'gosum: checkout=no sum; downloaded module=h1 sum'
