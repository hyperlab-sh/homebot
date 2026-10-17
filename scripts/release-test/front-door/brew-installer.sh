#!/bin/bash
# The fake Homebrew installer the stub curl serves. Like the real one it
# refuses to start on Linux without git, and it writes a stub brew under the
# default prefix. It records how it was started.
os=$(uname)
if [ "$os" = Darwin ]; then prefix=/opt/homebrew; else prefix=/home/linuxbrew/.linuxbrew; fi
tty=0
if [ -t 0 ]; then tty=1; fi
echo "installer NONINTERACTIVE=${NONINTERACTIVE:-} stdin_tty=$tty prefix=$prefix" >>"${STUB_LOG:?}"
if [ "$os" = Linux ] && ! command -v git >/dev/null 2>&1; then
  echo "You must install Git before installing Homebrew." >&2
  exit 1
fi
if [ -n "${STUB_INSTALLER_FAIL:-}" ]; then
  echo "Failed during: stub installer" >&2
  exit "$STUB_INSTALLER_FAIL"
fi
mkdir -p "$prefix/bin"
sed "s|@PREFIX@|$prefix|" "${STUB_FIXTURES:?}/stubs/brew.tmpl" >"$prefix/bin/brew"
chmod +x "$prefix/bin/brew"
echo "==> Installation successful!"
