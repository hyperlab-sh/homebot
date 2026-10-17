#!/usr/bin/env bash
# Offline test of local-cask.py against a cask shaped like GoReleaser's output
# for the four-target build (darwin/linux x amd64/arm64).
set -euo pipefail
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
tool=$here/local-cask.py fixture=$here/fixtures/goreleaser-cask.rb
base=http://127.0.0.1:9
tmp=$(mktemp -d "${TMPDIR:-/tmp}/local-cask-test.XXXXXX")
trap 'rm -rf "$tmp"' EXIT
fail() { echo "FAIL: $*" >&2; exit 1; }

python3 "$tool" "$fixture" "$tmp/with.rb" "$base" with-hook
[[ $(grep -c "url \"$base/releases/download/" "$tmp/with.rb") == 4 ]] || fail 'not all four urls rewritten'
! grep -q 'github.com' "$tmp/with.rb" || fail 'remote url survived'
grep -q 'postflight do' "$tmp/with.rb" || fail 'with-hook lost the hook'

python3 "$tool" "$fixture" "$tmp/without.rb" "$base" without-hook
! grep -qE 'postflight|xattr' "$tmp/without.rb" || fail 'hook not removed'
[[ $(grep -c '^  end$' "$tmp/without.rb") == 3 && $(tail -1 "$tmp/without.rb") == end ]] || fail 'cask structure damaged'

grep -v 'releases/download' "$fixture" >"$tmp/none.rb"
! python3 "$tool" "$tmp/none.rb" "$tmp/o.rb" "$base" with-hook 2>/dev/null || fail 'cask without urls accepted'
! python3 "$tool" "$fixture" "$tmp/o.rb" "$base" bogus 2>/dev/null || fail 'unknown hook mode accepted'
sed 's|^  version|  comment "https://github.com/hyperlab-sh/homebot/releases/download/v0.0.1/stray"\n  version|' "$fixture" >"$tmp/stray.rb"
grep -q 'stray' "$tmp/stray.rb" || fail 'stray fixture not built'
! python3 "$tool" "$tmp/stray.rb" "$tmp/o.rb" "$base" with-hook 2>/dev/null || fail 'release url outside a url stanza accepted'
echo 'local-cask-test: ok'
