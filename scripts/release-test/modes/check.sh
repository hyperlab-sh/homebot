#!/usr/bin/env bash
# check: goreleaser check, and shellcheck over the release scripts.
set -euo pipefail
# shellcheck source-path=SCRIPTDIR source=../lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/../lib.sh"
cd "$RT_SRC"

rc=0

if [[ ! -f .goreleaser.yaml ]]; then
  rt_log "check: .goreleaser.yaml not found at the repository root"
  rc=1
else
  # goreleaser wants a git repository with a remote; the shipped tree has no
  # .git, so check a throwaway copy of it, committed.
  echo "--- goreleaser check"
  repo=$(mktemp -d)
  trap 'rm -rf "$repo"' EXIT
  cp -a . "$repo"
  (
    cd "$repo"
    export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null
    export GIT_AUTHOR_NAME="homebot check" GIT_AUTHOR_EMAIL="check@example.invalid"
    export GIT_COMMITTER_NAME="homebot check" GIT_COMMITTER_EMAIL="check@example.invalid"
    git init -q -b main
    git remote add origin https://github.com/hyperlab-sh/homebot.git
    git add -A
    git commit -q -m "check"
    goreleaser check
  ) || rc=1
fi

echo "--- shellcheck"
sh_files=()
bash_files=()
if [[ -f scripts/release-test.sh ]]; then bash_files+=(scripts/release-test.sh); fi
while IFS= read -r f; do
  bash_files+=("$f")
done < <(find scripts/release-test scripts/release -name '*.sh' -type f 2>/dev/null | LC_ALL=C sort)
# The front door (served at homebot.sh/install) is bash, and so are its test stubs
# (they have no .sh extension).
for f in docs/landing/install.sh scripts/release-test/front-door/stubs/*; do
  if [[ -f $f ]]; then bash_files+=("$f"); fi
done
# install.sh is POSIX sh; it arrives with the install script's task.
if [[ -f install.sh ]]; then sh_files+=(install.sh); fi

if (( ${#bash_files[@]} )); then
  shellcheck -x "${bash_files[@]}" || rc=1
fi
if (( ${#sh_files[@]} )); then
  shellcheck -x -s sh "${sh_files[@]}" || rc=1
fi
# Mode scripts reach Docker only through lib.sh's sibling helpers (see
# rt_lint_no_docker there).
echo "--- no direct Docker calls in the modes"
if ! rt_lint_no_docker scripts/release-test/modes/*.sh; then
  rt_log "check: a mode file runs the Docker CLI itself; use the rt_sibling_* helpers in lib.sh"
  rc=1
fi

echo "shellcheck: ${#bash_files[@]} bash and ${#sh_files[@]} sh files"

echo "--- actionlint"
# actionlint finds .github/actionlint.yaml only by locating a .git directory above
# the workflow files, and the shipped tree has none; name the config explicitly or
# the self-hosted runner labels (homebot-mac) are reported unknown.
actionlint -config-file .github/actionlint.yaml .github/workflows/*.yml || rc=1

exit "$rc"
