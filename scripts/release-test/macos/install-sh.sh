#!/usr/bin/env bash
# GitHub Actions macOS only: install.sh against two unsigned local snapshots.
set -euo pipefail
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=scripts/release-test/macos/common.sh
source "$here/common.sh"
[[ $(uname -s) == Darwin ]] || die 'install-sh check requires macOS'
snapshots=$(snapshot_root "${1:-}")
[[ -s install.sh ]] || die 'Task 6 install.sh is absent'
start_server "$snapshots"
trap stop_server EXIT
mkdir -p "$work/home" "$work/bin"
export HOME="$work/home"
# Use only system paths for the harness. /bin/sh is Bash 3.2 on macOS.
export PATH=/usr/bin:/bin:/usr/sbin:/sbin
[[ $(/bin/sh -c 'echo "${BASH_VERSION:-}"') == 3.2* ]] || die '/bin/sh is not Bash 3.2'
command -v shasum >/dev/null 2>&1 || die 'shasum is missing from system paths'
# The runner image can contain sha256sum in a system directory. Give the
# installer a controlled PATH so this check always exercises macOS shasum.
install_path="$work/install-path"
mkdir -p "$install_path"
for tool in awk chmod cp curl date grep id ls mkdir mktemp mv rm sysctl tar uname; do
  path=$(command -v "$tool") || die "missing system tool: $tool"
  ln -s "$path" "$install_path/$tool"
done
cat >"$install_path/shasum" <<'EOF'
#!/bin/sh
printf 'used\n' >>"$SHASUM_MARKER"
exec /usr/bin/shasum "$@"
EOF
chmod 0755 "$install_path/shasum"
export SHASUM_MARKER="$work/shasum-used"
PATH="$install_path" command -v sha256sum >/dev/null 2>&1 && die 'controlled PATH contains sha256sum'

PATH="$install_path" /bin/sh install.sh --version v0.0.1 --base-url "$base" --bin-dir "$work/bin" \
  >"$work/install.log" 2>&1 || { cat "$work/install.log" >&2; die 'unsigned snapshot install failed'; }
[[ -s $SHASUM_MARKER ]] || die 'installer did not use shasum fallback'
grep -q 'signature not checked' "$work/install.log" || die 'unsigned install omitted the cosign note'
must_equal "$(HOMEBOT_PLAIN=1 "$work/bin/homebot" version)" '0.0.1' 'script-installed version'

if [[ $(uname -m) == x86_64 ]]; then
  # No Rosetta probe key exists on the Intel runner. The installer must use
  # amd64 rather than treating sysctl's missing key as translation.
  file "$work/bin/homebot" | grep -q 'x86_64' || die 'Intel runner did not install amd64'
else
  # The translated shell must still select arm64. Rosetta installation can
  # fail on hosted runners; that case is a visible skipped check, not a pass.
  # shellcheck disable=SC2024 # capture the command's output as the runner user
  if sudo -n softwareupdate --install-rosetta --agree-to-license >"$work/rosetta.log" 2>&1; then
    mkdir -p "$work/rosetta-bin"
    if PATH="$install_path" /usr/bin/arch -x86_64 /bin/sh install.sh --version v0.0.1 --base-url "$base" \
      --bin-dir "$work/rosetta-bin" >"$work/rosetta-install.log" 2>&1; then
      file "$work/rosetta-bin/homebot" | grep -q arm64 \
        || die 'translated shell did not install arm64'
    else
      cat "$work/rosetta-install.log" >&2
      die 'translated-shell install failed after Rosetta was installed'
    fi
  elif [[ -n ${GITHUB_STEP_SUMMARY:-} ]]; then
    echo 'Rosetta installation failed; translated-shell check skipped.' >>"$GITHUB_STEP_SUMMARY"
  fi
fi

mkdir -p "$work/sudo-bin"
set +e
# shellcheck disable=SC2024 # capture the command's output as the runner user
sudo -n /bin/sh install.sh --version v0.0.1 --base-url "$base" \
  --bin-dir "$work/sudo-bin" >"$work/sudo.log" 2>&1
code=$?
set -e
must_equal "$code" '7' 'sudo refusal exit code'
[[ ! -e $work/sudo-bin/homebot ]] || die 'sudo created a binary under scratch HOME'
if find "$work/home" "$work/sudo-bin" -user root -print | grep -q .; then
  die 'sudo left a root-owned file under scratch HOME'
fi
