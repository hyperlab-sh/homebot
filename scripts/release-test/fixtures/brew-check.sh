#!/usr/bin/env bash
# Runs in the pinned Homebrew Linux sibling. The parent supplies a local cask
# tap and a loopback release server sharing this container's network namespace.
set -euo pipefail
# fail MESSAGE...: name the failed check, since a bare failing `[[ ]]` under
# `set -e` would end the run silently.
fail() { echo "brew-check: $*" >&2; exit 1; }
[[ $(id -u) != 0 ]] || fail 'must not run as root'
[[ $(id -u) == 1000 ]] || fail 'volume needs uid 1000'
export HOMEBREW_NO_ANALYTICS=1 HOMEBREW_NO_ENV_HINTS=1 HOMEBREW_NO_INSTALL_CLEANUP=1
work=/release/task9-brew
tap=hyperlab-sh/tap
token=$tap/homebot
prefix=$(brew --prefix)
stable=$prefix/bin/homebot

version() {
  local got
  got=$(HOMEBOT_PLAIN=1 "$stable" version)
  [[ $got == "$1" ]] || fail "version is '$got', want $1"
}

check_links() {
  [[ -L $stable ]] || fail "$stable is not a link"
  [[ $(readlink -f "$stable") == *"/Caskroom/homebot/"* ]] \
    || fail "stable link $stable does not target the Caskroom"
  [[ -e $prefix/share/man/man1/homebot.1 || -e $prefix/share/man/man1/homebot.1.gz ]] \
    || fail "no man page at $prefix/share/man/man1/homebot.1[.gz]"
  [[ -e $prefix/etc/bash_completion.d/homebot || -e $prefix/share/bash-completion/completions/homebot ]] \
    || fail "no bash completion under $prefix/etc/bash_completion.d or $prefix/share/bash-completion/completions"
  [[ -e $prefix/share/zsh/site-functions/_homebot ]] \
    || fail "no zsh completion at $prefix/share/zsh/site-functions/_homebot"
  [[ -e $prefix/share/fish/vendor_completions.d/homebot.fish ]] \
    || fail "no fish completion at $prefix/share/fish/vendor_completions.d/homebot.fish"
}

case ${1:-} in
  prepare)
    printf 'brew-check: %s (uid %s)\n' "$(brew --version | head -1)" "$(id -u)"
    # A local path is preferred. Retry with file:// only for versions that
    # reject plain paths; both refer solely to this test's volume.
    if ! brew tap "$tap" "$work/tap"; then
      brew tap "$tap" "file://$work/tap"
    fi
    ;;
  install)
    export HOMEBREW_NO_AUTO_UPDATE=1
    brew install --cask "$token"
    check_links
    version 0.0.1
    # The cask depends on gitleaks and git: installing it must have pulled both in.
    for dep in gitleaks git; do
      brew list --formula "$dep" >/dev/null 2>&1 || fail "the cask did not install its $dep dependency"
      [[ -x $prefix/bin/$dep ]] || fail "no $prefix/bin/$dep after installing the cask"
    done
    mkdir -p "$work/home/repo" "$work/home/.config" "$work/stubs"
    : > "$work/home/repo/homebot.yml"
    git -C "$work/home/repo" init -q
    git -C "$work/home/repo" config user.name 'homebot release test'
    git -C "$work/home/repo" config user.email 'release-test@example.invalid'
    printf '#!/bin/sh\nexit 0\n' > "$work/stubs/systemctl"
    printf '#!/bin/sh\necho Linger=yes\n' > "$work/stubs/loginctl"
    chmod 755 "$work/stubs/systemctl" "$work/stubs/loginctl"
    HOME=$work/home HOMEBOT_ROOT=$work/home/repo PATH="$work/stubs:$prefix/bin:$PATH" \
      "$stable" setup </dev/null
    unit=$work/home/.config/systemd/user/homebot-tidy.service
    [[ -s $unit ]] || fail "setup wrote no unit at $unit"
    grep -Fq "ExecStart=\"$stable\"" "$unit" \
      || fail "unit does not start with $stable"
    ;;
  upgrade)
    git -C "$(brew --repo "$tap")" pull --ff-only
    # HOMEBREW_NO_AUTO_UPDATE stays set (the sibling's environment sets it): the
    # tap was just refreshed by the git pull above, so an auto-update would only
    # add a network dependency.
    export HOMEBREW_NO_AUTO_UPDATE=1
    brew upgrade --cask "$token"
    check_links
    version 0.0.2
    [[ ! -e $prefix/Caskroom/homebot/0.0.1 ]] || fail 'old Caskroom version 0.0.1 remains after upgrade'
    unit=$work/home/.config/systemd/user/homebot-tidy.service
    grep -Fq "ExecStart=\"$stable\"" "$unit" \
      || fail "unit no longer starts with $stable after upgrade"
    ;;
  uninstall)
    export HOMEBREW_NO_AUTO_UPDATE=1
    brew uninstall --cask "$token"
    [[ ! -e $stable && ! -L $stable ]] || fail "$stable remains after uninstall"
    ;;
  *) echo 'brew-check: expected prepare|install|upgrade|uninstall' >&2; exit 64 ;;
esac
