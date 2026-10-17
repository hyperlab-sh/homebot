#!/usr/bin/env bash
# Builds two throwaway snapshot releases of this tree, v0.0.1 and v0.0.2, so
# the install and upgrade tests have two versions to move between.
#
#   scripts/release-test/make-snapshots.sh OUT
#
# It copies the shipped tree (tracked files plus untracked-not-ignored ones, so
# uncommitted work is built) into a throwaway git repository with a fixed
# author and date, tags it v0.0.1, runs
#   goreleaser release --snapshot --clean --skip=sign
# commits again, tags v0.0.2, and runs it again. Each run's dist/ ends up as
# OUT/v0.0.1 and OUT/v0.0.2. Signing is skipped; nothing is ever published.
# One more run, not a snapshot, passes .github/release-notes/v0.1.0.md to
#   goreleaser release --clean --skip=sign,publish,announce,validate \
#     --release-notes FILE
# because GoReleaser skips the changelog step in snapshot mode and never opens
# the file there. That run fails when the file is missing or empty. Its dist/ is
# discarded; it needs no token.
#
# Host agnostic: it needs only git, go, goreleaser and jq on PATH (and syft
# when .goreleaser.yaml asks for SBOMs). The release-test snapshot mode and the
# GitHub Actions snapshot job both call it.
set -euo pipefail

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
root=$(cd "$here/../.." && pwd)

if (( $# != 1 )) || [[ $1 == -h || $1 == --help ]]; then
  sed -n '2,22p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//' >&2
  exit 64
fi
mkdir -p "$1"
out=$(cd "$1" && pwd)

die() { echo "make-snapshots: $*" >&2; exit 1; }

for tool in git go goreleaser jq tar; do
  command -v "$tool" >/dev/null 2>&1 || die "$tool is not on PATH"
done
[[ -f $root/.goreleaser.yaml ]] || die ".goreleaser.yaml not found in $root"

tmp=$(mktemp -d "${TMPDIR:-/tmp}/homebot-snapshots.XXXXXX")
trap 'rm -rf "$tmp"' EXIT
repo=$tmp/repo
mkdir -p "$repo"

# --- stage the shipped tree -------------------------------------------------
# In a git checkout the file list is git's. In the release-test container the
# tree arrives as a tar without .git, and everything in it is shipped already.

top=$(git -C "$root" rev-parse --show-toplevel 2>/dev/null || true)
list_files() {
  if [[ $top == "$root" ]]; then
    git -C "$root" ls-files -z --cached --others --exclude-standard --deduplicate
  else
    (cd "$root" && find . \( -path ./.git -o -path ./.worktrees -o -path ./dist \) -prune -o \( -type f -o -type l \) -print0 | sed -z 's|^\./||')
  fi
}
(
  cd "$root"
  list_files \
    | while IFS= read -r -d '' f; do
        case $f in
          .git/* | .worktrees/* | dist/*) continue ;;
        esac
        if [[ -e $f || -L $f ]]; then printf '%s\0' "$f"; fi
      done \
    | COPYFILE_DISABLE=1 tar -cf - --null -T -
) | tar -xf - -C "$repo"

# --- the throwaway repository -----------------------------------------------
# Hermetic: no global or system git config (a signing key, hooks, templates).

export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null
export GIT_AUTHOR_NAME="homebot snapshot" GIT_AUTHOR_EMAIL="snapshot@example.invalid"
export GIT_COMMITTER_NAME="homebot snapshot" GIT_COMMITTER_EMAIL="snapshot@example.invalid"

cd "$repo"
git init -q -b main
# goreleaser reads the tag only when it can resolve a remote (without one, a
# snapshot silently falls back to v0.0.0). Nothing is ever fetched from it.
git remote add origin https://github.com/hyperlab-sh/homebot.git

# A snapshot's version is "<version>-SNAPSHOT-<commit>" unless the config says
# otherwise, and the tests need plain 0.0.1 and 0.0.2 in the file names. Add
# the override to the throwaway copy only, and only when the config has none.
if ! grep -q '^snapshot:' .goreleaser.yaml; then
  [[ -z $(tail -c1 .goreleaser.yaml) ]] || echo >> .goreleaser.yaml
  printf 'snapshot:\n  version_template: "{{ .Version }}"\n' >> .goreleaser.yaml
fi

git add -A
GIT_AUTHOR_DATE="2026-01-01T00:00:00Z" GIT_COMMITTER_DATE="2026-01-01T00:00:00Z" \
  git commit -q -m "snapshot: v0.0.1"
git tag v0.0.1

build() {
  local version=$1
  echo "make-snapshots: building v$version" >&2
  bash scripts/release/extras.sh "$version" \
    "$(TZ=UTC git log -1 --date=iso-strict-local --format=%cd)" dist-extra
  goreleaser release --snapshot --clean --skip=sign >&2
  local got
  got=$(jq -r .version dist/metadata.json)
  [[ $got == "$version" ]] \
    || die "goreleaser built version '$got', want '$version': set snapshot.version_template to {{ .Version }} in .goreleaser.yaml"
  rm -rf "${out:?}/v$version"
  mv dist "$out/v$version"
}

build 0.0.1

GIT_AUTHOR_DATE="2026-01-02T00:00:00Z" GIT_COMMITTER_DATE="2026-01-02T00:00:00Z" \
  git commit -q --allow-empty -m "snapshot: v0.0.2"
git tag v0.0.2
build 0.0.2

# --- the release notes ------------------------------------------------------
# GoReleaser skips the changelog step in --snapshot mode, so a snapshot run
# never opens a --release-notes file. One real (non-snapshot) run does: it fails
# when the file is missing, and the -s test below fails when it is empty. It
# needs no token: sign, publish, announce and validate are skipped, so nothing
# is signed or uploaded, the dirty tree (dist-extra) is not an error, and the
# cask is written without HOMEBREW_TAP_TOKEN. HEAD is tagged v0.0.2, so this
# run builds v0.0.2 again; its dist/ is thrown away. Only the notes file is
# under test.
notes=.github/release-notes/v0.1.0.md
echo "make-snapshots: checking $notes with a non-snapshot goreleaser run" >&2
[[ -s $notes ]] || die "$notes is missing or empty"
goreleaser release --clean --skip=sign,publish,announce,validate \
  --release-notes "$notes" >&2
rm -rf dist

echo "make-snapshots: wrote $out/v0.0.1 and $out/v0.0.2" >&2
