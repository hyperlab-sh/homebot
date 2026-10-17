#!/usr/bin/env bash
# Runs inside a root distro sibling; paths in /release are prepared by packages.sh.
set -euo pipefail
# fail MESSAGE...: every check below reports what it was checking, because a
# bare failing command under `set -e` would end the run without a word.
fail() { echo "package-check: $*" >&2; exit 1; }
[[ $(id -u) == 0 ]] || fail 'requires root'
: "${PKG_KIND:?}" "${PKG_DISTRO:?}" "${PKG_ARCH:?}"
case $PKG_KIND in deb|rpm) ;; *) fail 'unknown package kind' ;; esac
case $PKG_ARCH in amd64|arm64) ;; *) fail 'unknown architecture' ;; esac
if [[ $PKG_ARCH == amd64 ]]; then
  [[ $(uname -m) == x86_64 ]] || fail "PKG_ARCH=amd64 but uname -m is $(uname -m), want x86_64"
  pkg_suffix=""
else
  [[ $(uname -m) == aarch64 ]] || fail "PKG_ARCH=arm64 but uname -m is $(uname -m), want aarch64"
  pkg_suffix=-arm64
fi

if [[ $PKG_KIND == deb ]]; then
  export DEBIAN_FRONTEND=noninteractive
  apt-get update -qq
  apt-get install -y -qq man-db bash-completion zsh fish
  install_pkg() { (cd /release/task9 && apt-get install -y "./deb-$1$pkg_suffix.deb"); }
  remove_pkg() { apt-get remove -y homebot; }
  list_pkg() { dpkg-query -L homebot; }
  # File list of the package file itself, which path-exclude does not trim.
  pkg_contents() { dpkg-deb -c "/release/task9/deb-$1$pkg_suffix.deb" | awk '{print substr($6, 2)}'; }
  zsh_dir=/usr/share/zsh/vendor-completions
  # Minimized images (Ubuntu's, debian-slim) exclude man pages in dpkg config.
  docs_stripped() { grep -rhsq '^path-exclude=/usr/share/man' /etc/dpkg/dpkg.cfg /etc/dpkg/dpkg.cfg.d; }
else
  dnf install -y man-db bash-completion zsh fish
  install_pkg() { (cd /release/task9 && dnf install -y "./rpm-$1$pkg_suffix.rpm"); }
  remove_pkg() { dnf remove -y homebot; }
  list_pkg() { rpm -ql homebot; }
  pkg_contents() { rpm -qpl "/release/task9/rpm-$1$pkg_suffix.rpm"; }
  zsh_dir=/usr/share/zsh/site-functions
  # Fedora container images set tsflags=nodocs, so rpm skips man pages.
  docs_stripped() { grep -rhsq '^[[:space:]]*tsflags=.*nodocs' /etc/dnf /etc/yum.conf /etc/rpm; }
fi

# The man page must be in the package file on every distro. It must also be
# on disk, except where the image strips documentation at install time: there
# it is an explicit, logged skip, not a silent pass and not a failure. (`man -w`
# is no help: it needs a configured MANPATH and a man database refresh.)
check_man_page() {
  pkg_contents "$1" | grep -Eq '^/usr/share/man/man1/homebot\.1(\.gz)?$' \
    || fail 'the package has no /usr/share/man/man1/homebot.1[.gz]'
  if compgen -G '/usr/share/man/man1/homebot.1*' >/dev/null; then
    return 0
  fi
  if docs_stripped; then
    echo "package-check: SKIP man page on disk: $PKG_DISTRO image strips documentation at install (the package ships it)"
    return 0
  fi
  fail 'man page is not installed at /usr/share/man/man1/homebot.1* and this image does not strip documentation'
}

check_version() {
  local got
  got=$(HOMEBOT_PLAIN=1 homebot version)
  [[ $got == "$1" ]] || fail "version is '$got', want $1"
}

install_pkg 0.0.1
check_version 0.0.1
check_man_page 0.0.1
[[ -s /usr/share/bash-completion/completions/homebot ]] \
  || fail 'bash completion /usr/share/bash-completion/completions/homebot is missing or empty'
# shellcheck disable=SC1091 # installed in the sibling by the package
source /usr/share/bash-completion/completions/homebot
complete -p homebot >/dev/null || fail 'sourcing the bash completion did not register one for homebot'
[[ -s $zsh_dir/_homebot ]] || fail "zsh completion $zsh_dir/_homebot is missing or empty"
zsh -fc 'print -l $fpath' | grep -Fxq "$zsh_dir" || fail "zsh fpath does not include $zsh_dir"
fish -c 'complete -C"homebot sa"' | grep -Eq '(^|[[:space:]])save([[:space:]]|$)' \
  || fail 'fish does not complete "homebot sa" to save'
if [[ $PKG_KIND == deb ]]; then
  [[ -s /usr/share/doc/homebot/copyright ]] || fail '/usr/share/doc/homebot/copyright is missing or empty'
else
  rpm -q --licensefiles homebot | grep -Fxq /usr/share/licenses/homebot/LICENSE \
    || fail 'rpm does not list /usr/share/licenses/homebot/LICENSE as a license file'
  [[ -s /usr/share/licenses/homebot/LICENSE ]] || fail '/usr/share/licenses/homebot/LICENSE is missing or empty'
fi

install_pkg 0.0.2
check_version 0.0.2
list_pkg | while IFS= read -r path; do
  # Package manifests include shared directories, which must remain.
  if [[ -f $path || -L $path ]]; then printf '%s\n' "$path"; fi
done > /tmp/homebot-package-files
remove_pkg
if command -v homebot >/dev/null 2>&1; then
  fail 'executable remains after removal'
fi
while IFS= read -r path; do
  [[ ! -e $path && ! -L $path ]] || fail "leftover $path"
done < /tmp/homebot-package-files
echo "package-check: $PKG_DISTRO install, upgrade, remove passed"
