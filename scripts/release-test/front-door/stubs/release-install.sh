#!/bin/sh
# The fake release installer the stub curl serves as the release's install.sh.
# Like the real one it refuses a sudo caller, and it puts the stub homebot in
# ~/.local/bin. It records how it was started. STUB_RELEASE_INSTALL_FAIL makes
# it fail with that exit status; STUB_RELEASE_INSTALL_NOOP makes it succeed
# without installing anything.
tty=0
if [ -t 0 ]; then tty=1; fi
echo "release-install args=$* user=$(id -u) SUDO_USER=${SUDO_USER-} stdin_tty=$tty" >>"${STUB_LOG:?}"
[ -z "${SUDO_USER:-}" ] || { echo 'install.sh: run without sudo' >&2; exit 7; }
if [ -n "${STUB_RELEASE_INSTALL_FAIL:-}" ]; then
  echo 'install.sh: archive checksum mismatch' >&2
  exit "$STUB_RELEASE_INSTALL_FAIL"
fi
[ -z "${STUB_RELEASE_INSTALL_NOOP:-}" ] || exit 0
mkdir -p "$HOME/.local/bin"
cp "${STUB_FIXTURES:?}/stubs/homebot" "$HOME/.local/bin/homebot"
chmod 0755 "$HOME/.local/bin/homebot"
echo 'Installed homebot 0.0.0-stub. Next: homebot setup'
