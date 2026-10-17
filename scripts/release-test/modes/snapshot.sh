#!/usr/bin/env bash
# snapshot: builds the throwaway v0.0.1 and v0.0.2 releases into the run's
# volume (make-snapshots.sh) and checks that both exist. Other modes find them
# through rt_need_snapshots.
set -euo pipefail
# shellcheck source-path=SCRIPTDIR source=../lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/../lib.sh"
cd "$RT_SRC"

# The tagged release passes these notes to GoReleaser. GoReleaser skips its
# changelog step in --snapshot mode and never opens the file there, so this
# check only proves the file exists and is not empty; make-snapshots.sh then
# makes one non-snapshot run that really reads it.
[[ -s .github/release-notes/v0.1.0.md ]] || rt_die "snapshot: v0.1.0 release notes are missing"

out=$RT_VOL/snapshots
rm -rf "$out"
bash scripts/release-test/make-snapshots.sh "$out"

for v in 0.0.1 0.0.2; do
  dir=$out/v$v
  [[ -s $dir/metadata.json ]] || rt_die "snapshot: $dir/metadata.json is missing"
  got=$(jq -r .version "$dir/metadata.json")
  [[ $got == "$v" ]] || rt_die "snapshot: $dir holds version '$got', want $v"
  [[ -s $dir/artifacts.json ]] || rt_die "snapshot: $dir/artifacts.json is missing"
  echo "--- v$v"
  (cd "$dir" && find . -maxdepth 1 -type f ! -name '*.json' ! -name '*.yaml' | LC_ALL=C sort | sed 's|^\./||')
done
touch "$out/.complete"
