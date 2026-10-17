#!/usr/bin/env bash
# release-test: not-in-all
# Needs --siblings and reaches the network (Homebrew), so it runs by name.
# Exercise the generated cask in the official Linux Homebrew image. A Linux
# cask refusal is a real result for release planning, not a passing test.
set -euo pipefail
# shellcheck source-path=SCRIPTDIR source=../lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/../lib.sh"
rt_require_uid 1000
_rt_sibling_guard
trap 'rt_cleanup_siblings' EXIT
rt_need_snapshots
[[ $(uname -m) == x86_64 ]] || rt_die 'brew-linux: amd64 execution requires an x86_64 host'

# Homebrew's official 7.0.7 multi-platform image, rather than mutable :main.
brew_image=ghcr.io/homebrew/brew:7.0.7@sha256:63f9a03880f954b2dc05ee0b8e93903dae2f161883af0072d77f476303fc834a
port=8765
work=$RT_VOL/task9-brew
mkdir -p "$work/tap/Casks" "$work/home" "$work/stubs"
cp "$RT_SRC/scripts/release-test/fixtures/brew-check.sh" "$work/brew-check.sh"
chmod 755 "$work/brew-check.sh"

one_cask() {
  local version=$1 found=()
  while IFS= read -r -d '' file; do found+=("$file"); done \
    < <(find "$RT_VOL/snapshots/v$version" -type f -name homebot.rb -print0)
  (( ${#found[@]} == 1 )) || rt_die "brew-linux: want one cask for v$version, found ${#found[@]}"
  printf '%s\n' "${found[0]}"
}

rewrite_cask() {
  local version=$1 cask
  cask=$(one_cask "$version")
  cp "$cask" "$work/tap/Casks/homebot.rb"
  rt_cask_local_origin "$work/tap/Casks/homebot.rb" "$port"
}

rewrite_cask 0.0.1
(
  cd "$work/tap"
  GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null git init -q -b main
  GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null \
    git -c user.name='homebot release test' -c user.email='release-test@example.invalid' \
    add Casks/homebot.rb
  GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null \
    git -c user.name='homebot release test' -c user.email='release-test@example.invalid' \
    commit -qm 'cask v0.0.1'
)

rt_http_start task9-http "$RT_VOL/snapshots" "$port"
rt_sibling_start --name task9-brew --image "$brew_image" --net-of task9-http \
  --env HOMEBREW_NO_AUTO_UPDATE=1 --env HOMEBREW_NO_ANALYTICS=1 -- \
  bash -c 'sleep infinity'
rt_sibling_exec task9-brew -- bash /release/task9-brew/brew-check.sh prepare
rt_sibling_exec task9-brew -- bash /release/task9-brew/brew-check.sh install

rewrite_cask 0.0.2
(
  cd "$work/tap"
  GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null git add Casks/homebot.rb
  GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null \
    git -c user.name='homebot release test' -c user.email='release-test@example.invalid' \
    commit -qm 'cask v0.0.2'
)
rt_sibling_exec task9-brew -- bash /release/task9-brew/brew-check.sh upgrade
rt_sibling_exec task9-brew -- bash /release/task9-brew/brew-check.sh uninstall
