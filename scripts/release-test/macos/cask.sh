#!/usr/bin/env bash
# GitHub Actions macOS only: exercise an unsigned snapshot through a local tap.
set -euo pipefail
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=scripts/release-test/macos/common.sh
source "$here/common.sh"
[[ $(uname -s) == Darwin ]] || die 'cask check requires macOS'
[[ -n ${CI:-} || -n ${HOMEBOT_RELEASE_TEST_ALLOW_LOCAL:-} ]] \
  || die 'refusing to tap hyperlab-sh/tap outside CI; set HOMEBOT_RELEASE_TEST_ALLOW_LOCAL=1 on a disposable Mac'
snapshots=$(snapshot_root "${1:-}")
[[ -s $snapshots/v0.0.1/homebrew/Casks/homebot.rb && -s $snapshots/v0.0.2/homebrew/Casks/homebot.rb ]] \
  || die 'Task 5 snapshot casks are absent'

start_server "$snapshots"
tap_repo="$work/tap-source"
mkdir -p "$tap_repo/Casks"
git -C "$tap_repo" init -q -b main
git -C "$tap_repo" config user.name 'homebot release test'
git -C "$tap_repo" config user.email 'release-test@example.invalid'
brew_bin="$(brew --prefix)/bin/brew"
prefix=$(brew --prefix)
installed=0
tapped=0
if "$brew_bin" tap | grep -qx 'hyperlab-sh/tap'; then die 'hyperlab-sh/tap is already tapped; refusing to replace it'; fi
cleanup() {
  if (( installed )); then "$brew_bin" uninstall --cask --zap hyperlab-sh/tap/homebot >/dev/null 2>&1 || true; fi
  "$brew_bin" uninstall --cask --zap hyperlab-sh/tap/homebot-noquarantine >/dev/null 2>&1 || true
  if (( tapped )); then "$brew_bin" untap hyperlab-sh/tap >/dev/null 2>&1 || true; fi
  stop_server
}
trap cleanup EXIT

python3 "$here/local-cask.py" "$snapshots/v0.0.1/homebrew/Casks/homebot.rb" \
  "$tap_repo/Casks/homebot.rb" "$base" with-hook
python3 "$here/local-cask.py" "$snapshots/v0.0.1/homebrew/Casks/homebot.rb" \
  "$tap_repo/Casks/homebot-noquarantine.rb" "$base" without-hook
python3 - "$tap_repo/Casks/homebot-noquarantine.rb" <<'PY'
import pathlib, sys
p = pathlib.Path(sys.argv[1])
s = p.read_text()
old = 'cask "homebot" do'
if s.count(old) != 1:
    raise SystemExit('unexpected cask token; cannot construct negative control')
p.write_text(s.replace(old, 'cask "homebot-noquarantine" do', 1))
PY
git -C "$tap_repo" add Casks
git -C "$tap_repo" commit -qm 'test casks v0.0.1'
tapped=1
"$brew_bin" tap hyperlab-sh/tap "$tap_repo"

# Negative control first: the hook-free cask must carry the attribute, or a
# later absence on the real cask would prove nothing about the xattr hook.
"$brew_bin" install --cask hyperlab-sh/tap/homebot-noquarantine 2>&1 | tee "$work/nohook.log"
negative="$prefix/Caskroom/homebot-noquarantine/0.0.1/homebot"
[[ -f $negative ]] || die "negative-control binary missing: $negative"
xattr -p com.apple.quarantine "$negative" >/dev/null \
  || die 'negative control lacks quarantine; this runner cannot validate the hook'
"$brew_bin" uninstall --cask --zap hyperlab-sh/tap/homebot-noquarantine

"$brew_bin" install --cask hyperlab-sh/tap/homebot 2>&1 | tee "$work/install.log"
installed=1
binary="$prefix/Caskroom/homebot/0.0.1/homebot"
[[ -f $binary ]] || die "cask binary missing: $binary"
if xattr -p com.apple.quarantine "$binary" >/dev/null 2>&1; then
  die 'installed cask binary is still quarantined'
fi
must_equal "$(perl -e 'alarm 30; exec @ARGV' "$prefix/bin/homebot" version)" '0.0.1' 'installed version'
for f in share/zsh/site-functions/_homebot etc/bash_completion.d/homebot share/fish/vendor_completions.d/homebot.fish; do
  [[ -L $prefix/$f || -s $prefix/$f ]] || die "completion missing: $prefix/$f"
done
MANPATH="$prefix/share/man:${MANPATH:-}" man -w homebot >/dev/null || die 'man cannot find homebot'

# Setup writes only under this disposable HOME. Stub launchctl because the
# helper discovers it through PATH; no user LaunchAgent may be bootstrapped.
mkdir -p "$work/home/repo" "$work/stub"
git -C "$work/home/repo" init -q
: >"$work/home/repo/homebot.yml"
git -C "$work/home/repo" config user.name 'Release Test'
git -C "$work/home/repo" config user.email 'release-test@example.invalid'
printf '#!/bin/sh\nexit 0\n' >"$work/stub/launchctl"
chmod 755 "$work/stub/launchctl"
# The cask depends on gitleaks and git, so installing it must have pulled both
# in: setup refuses without gitleaks and without git 2.40+ (the system git of
# Xcode 16's command-line tools is 2.39.5). Not installed here on purpose.
[[ -x $prefix/bin/gitleaks ]] || die 'the cask did not install its gitleaks dependency'
[[ -x $prefix/bin/git ]] || die 'the cask did not install its git dependency'
HOME="$work/home" HOMEBOT_ROOT="$work/home/repo" PATH="$work/stub:$PATH" \
  HOMEBOT_PLAIN=1 "$prefix/bin/homebot" setup </dev/null >"$work/setup.log" 2>&1 \
  || { cat "$work/setup.log" >&2; die 'scratch HOME setup failed'; }
plist="$work/home/Library/LaunchAgents/sh.homebot.tidy.plist"
[[ -s $plist ]] || die "setup did not write $plist"
must_equal "$(/usr/libexec/PlistBuddy -c 'Print :ProgramArguments:0' "$plist")" \
  "$prefix/bin/homebot" 'setup helper path'

python3 "$here/local-cask.py" "$snapshots/v0.0.2/homebrew/Casks/homebot.rb" \
  "$tap_repo/Casks/homebot.rb" "$base" with-hook
git -C "$tap_repo" add Casks/homebot.rb
git -C "$tap_repo" commit -qm 'test cask v0.0.2'
git -C "$("$brew_bin" --repo hyperlab-sh/tap)" pull --ff-only
# S3 upgrade argv: absolute brew, fully qualified token and these environment
# pins. Deliberately no HOMEBREW_NO_AUTO_UPDATE here: the tap was pulled above.
env -u HOMEBREW_NO_AUTO_UPDATE HOMEBREW_NO_INSTALL_CLEANUP=1 \
  HOMEBREW_NO_ENV_HINTS=1 HOMEBREW_NO_ANALYTICS=1 \
  "$brew_bin" upgrade --cask hyperlab-sh/tap/homebot 2>&1 | tee "$work/upgrade.log"
binary="$prefix/Caskroom/homebot/0.0.2/homebot"
[[ -f $binary && ! -e $prefix/Caskroom/homebot/0.0.1 ]] || die 'Caskroom upgrade did not replace v0.0.1'
if xattr -p com.apple.quarantine "$binary" >/dev/null 2>&1; then die 'upgraded binary is quarantined'; fi
must_equal "$(perl -e 'alarm 30; exec @ARGV' "$prefix/bin/homebot" version)" '0.0.2' 'upgraded version'
must_equal "$(/usr/libexec/PlistBuddy -c 'Print :ProgramArguments:0' "$plist")" \
  "$prefix/bin/homebot" 'upgraded helper path'

if [[ -n ${GITHUB_STEP_SUMMARY:-} ]]; then
  { echo "### Homebrew $("$brew_bin" --version | head -1) ($RUNNER_OS $RUNNER_ARCH)";
    grep -ih warning "$work"/*.log || echo 'No Homebrew warnings captured.'; } >>"$GITHUB_STEP_SUMMARY"
fi
"$brew_bin" uninstall --cask --zap hyperlab-sh/tap/homebot
installed=0
