#!/bin/sh
# homebot commit guard. git runs it before every commit of the home repo
# (core.hooksPath), and homebot writes it: edit homebot, not this file.
# It seals secrets, then refuses plaintext secrets, oversized files and
# anything gitleaks flags. Bypass once with: git commit --no-verify

# Where homebot was when this file was written.
recorded=@RECORDED@

# homebot on PATH first, then where it was, then where package managers put
# it: GUI git clients start hooks with a minimal PATH.
homebot=
if command -v homebot >/dev/null 2>&1; then
  homebot=homebot
else
  for candidate in "$recorded" /opt/homebrew/bin/homebot /usr/local/bin/homebot "$HOME/.local/bin/homebot"; do
    if [ -n "$candidate" ] && [ -f "$candidate" ] && [ -x "$candidate" ]; then
      homebot=$candidate
      break
    fi
  done
  # A PATH too thin to find homebot is too thin to find gitleaks: add
  # homebot's directory, and on macOS Homebrew's, after the caller's own,
  # so the caller's tools still win.
  if [ -n "$homebot" ]; then
    PATH="$PATH:${homebot%/*}"
    if [ "$(uname -s)" = Darwin ]; then
      PATH="$PATH:/opt/homebrew/bin:/usr/local/bin"
    fi
    export PATH
  fi
fi
if [ -z "$homebot" ]; then
  printf '%s\n' @REFUSAL@ >&2
  exit 1
fi

"$homebot" hook pre-commit || exit

# The repo's own pre-commit hook, which core.hooksPath would otherwise skip.
own="$(git rev-parse --git-common-dir)/hooks/pre-commit" || exit
if [ -f "$own" ] && [ -x "$own" ]; then
  exec "$own" "$@"
fi
