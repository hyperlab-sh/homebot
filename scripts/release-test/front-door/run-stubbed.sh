#!/usr/bin/env bash
# The stubbed front-door tests. Runs inside a sibling container that
# scripts/release-test/modes/front-door.sh starts:
#
#   run-stubbed.sh            as tester: the whole suite
#   run-stubbed.sh root       as root: only the "refuses root" check
#
# docs/landing/install.sh runs for real, piped into bash like `curl | bash`,
# under a pseudo-terminal where a person would answer (pty-run.py), with a
# PATH that holds only stubs (curl, uname, sudo, the package managers, git,
# brew...) plus a short list of real basics. Every case gets its own HOME and
# stub directory under /tmp. Nothing here touches a real home.
#
# Needs: /home/linuxbrew, /opt/homebrew and /usr/local/bin writable by tester
# (the mode prepares them), and /etc/os-release writable for the dnf cases.
# The single quotes below hold text for the script under test, on purpose.
# shellcheck disable=SC2016,SC2088
set -uo pipefail

# This suite empties /opt/homebrew and /home/linuxbrew between cases. It is
# for the throwaway containers scripts/release-test/modes/front-door.sh makes,
# never a real machine. Refuse unless this is a container (Docker and podman
# leave a marker file) or FD_REAL_MACHINE=1 says the caller knows.
if [ "${FD_REAL_MACHINE:-}" != 1 ] && [ ! -e /.dockerenv ] && [ ! -e /run/.containerenv ]; then
  echo "run-stubbed.sh wipes /opt/homebrew and /home/linuxbrew; run it in a container" \
    "(scripts/release-test.sh front-door) or set FD_REAL_MACHINE=1 on a throwaway machine." >&2
  exit 2
fi

FD=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
INSTALL=${FD_INSTALL:-$FD/../install.sh}
PYTHON=/usr/bin/python3
LIN=/home/linuxbrew/.linuxbrew
MAC=/opt/homebrew

pass=0
fail=0
FAILED=0
CASE=""
OUT=""
RC=0

# --- one case's world ------------------------------------------------------------

# fresh: a new HOME, stub directory and log, and a Linux/apt machine with
# nothing installed. Sets T HOME_ BIN SYS STATE LOG and the arrays ENVV
# (VAR=value pairs for the script's environment) and PTY (pty-run options).
fresh() {
  T=$(mktemp -d /tmp/fd.XXXXXX)
  HOME_=$T/home
  BIN=$T/bin
  SYS=$T/sys
  STATE=$T/state
  LOG=$T/log
  EXTRA=""
  mkdir -p "$HOME_" "$BIN" "$SYS" "$STATE" "$T/tmp"
  : >"$LOG"
  local c
  for c in bash sh mktemp sha256sum cat grep sed awk dirname basename mkdir ls mv rm cp id readlink head tail tr env sort wc date touch chmod ln tee sleep true false uniq cut find; do
    ln -s "$(command -v "$c")" "$SYS/$c"
  done
  # leftovers of an earlier case's fake Homebrew
  find "$LIN" "$MAC" -mindepth 1 -delete 2>/dev/null || true
  rm -f /usr/local/bin/brew
  ENVV=("SHELL=/bin/bash" "USER=tester" "TMPDIR=$T/tmp")
  PTY=(--no-tty)
  ARGS=(--yes --no-setup)
  FEED=$INSTALL
  for c in curl uname sudo; do cp "$FD/stubs/$c" "$BIN/$c"; done
  use_pm apt-get
}

# use_pm NAME: the machine's package manager (and its query tool).
use_pm() {
  local c
  rm -f "$BIN/apt-get" "$BIN/dpkg" "$BIN/dnf" "$BIN/yum" "$BIN/rpm" "$BIN/pacman" "$BIN/apk"
  case $1 in
    apt-get) for c in apt-get dpkg; do cp "$FD/stubs/pkgmgr" "$BIN/$c"; done ;;
    dnf) for c in dnf rpm; do cp "$FD/stubs/pkgmgr" "$BIN/$c"; done ;;
    yum) for c in yum rpm; do cp "$FD/stubs/pkgmgr" "$BIN/$c"; done ;;
    *) cp "$FD/stubs/pkgmgr" "$BIN/$1" ;;
  esac
}

env_set() { ENVV+=("$@"); }
have_tools_pkg() { : >"$STATE/tools"; }
have_git() { cp "$FD/stubs/git" "$BIN/git"; }
have_tool() { local t; for t in "$@"; do cp "$FD/stubs/tool" "$BIN/$t"; done; }

# have_brew PREFIX [path]: a Homebrew already there; "path" puts it on PATH.
have_brew() {
  mkdir -p "$1/bin"
  sed "s|@PREFIX@|$1|" "$FD/stubs/brew.tmpl" >"$1/bin/brew"
  chmod +x "$1/bin/brew"
  if [ "${2:-}" = path ]; then EXTRA="$1/bin"; fi
}

# ready: everything installed and brew on PATH. A release is always on offer.
ready() {
  have_tools_pkg
  have_git
  have_tool age gitleaks
  have_brew "$LIN" path
  ARGS=(--yes --no-setup)
}

macos() {
  env_set STUB_OS=Darwin STUB_ARCH=arm64
}

# play [pty-run options] -- ARGS: runs the script the way `curl | bash` does.
# Sets OUT and RC.
play() {
  local ptyargs=()
  while [ $# -gt 0 ] && [ "$1" != -- ]; do ptyargs+=("$1"); shift; done
  local explicit=0
  if [ "${1:-}" = -- ]; then shift; explicit=1; fi
  local a=("$@")
  # no `--` at all: the case's default arguments; a bare `--` means none
  if [ "$explicit" = 0 ]; then a=("${ARGS[@]}"); fi
  local p=(${PTY[@]+"${PTY[@]}"}) # an empty PTY is fine on bash 3.2 too
  if [ ${#ptyargs[@]} -gt 0 ]; then p=("${ptyargs[@]}"); fi
  OUT=$(env -i PATH="$BIN:${EXTRA:+$EXTRA:}$SYS" HOME="$HOME_" \
    STUB_LOG="$LOG" STUB_STATE="$STATE" STUB_FIXTURES="$FD" \
    "${ENVV[@]}" \
    "$PYTHON" "$FD/pty-run.py" ${p[@]+"${p[@]}"} -- bash -c 'cat "$0" | bash -s -- "$@"' "$FEED" ${a[@]+"${a[@]}"} 2>&1)
  RC=$?
}

# --- assertions --------------------------------------------------------------------

bad() {
  FAILED=1
  printf '      %s\n' "$*"
}
rc_is() { if [ "$RC" != "$1" ]; then bad "exit $RC, wanted $1"; fi; }
rc_nonzero() { if [ "$RC" = 0 ]; then bad "exit 0, wanted non-zero"; fi; }
out_has() { if ! grep -qE -- "$1" <<<"$OUT"; then bad "output lacks /$1/"; fi; }
out_lacks() { if grep -qE -- "$1" <<<"$OUT"; then bad "output has /$1/"; fi; }
log_has() { if ! grep -qE -- "$1" "$LOG"; then bad "log lacks /$1/"; fi; }
log_lacks() { if grep -qE -- "$1" "$LOG"; then bad "log has /$1/"; fi; }
log_count() { # log_count REGEX N
  local n
  n=$(grep -cE -- "$1" "$LOG" || true)
  if [ "$n" != "$2" ]; then bad "log has $n lines of /$1/, wanted $2"; fi
}
log_empty() { if [ -s "$LOG" ]; then bad "a stub ran, but nothing should have: $(head -3 "$LOG" | tr '\n' '|')"; fi; }
file_has() { if ! grep -qF -- "$2" "$1" 2>/dev/null; then bad "$1 lacks: $2"; fi; }
file_count() { # file_count FILE FIXED N
  local n
  n=$(grep -cF -- "$2" "$1" 2>/dev/null || true)
  if [ "$n" != "$3" ]; then bad "$1 has $n lines of '$2', wanted $3"; fi
}
home_empty() {
  local n
  n=$(find "$HOME_" -mindepth 1 | wc -l)
  if [ "$n" != 0 ]; then bad "HOME is not empty: $(find "$HOME_" -mindepth 1 | head -3 | tr '\n' ' ')"; fi
}
# tmp_clean: the script's temporary folder (under TMPDIR) is gone.
tmp_clean() {
  if [ -n "$(ls -A "$T/tmp" 2>/dev/null)" ]; then bad "a temporary folder was left behind: $(find "$T/tmp" -mindepth 1 | head -3 | tr '\n' ' ')"; fi
}
# nothing_changed: the machine is as it was: no HOME files, no Homebrew, no
# binary, and none of the stubs that change things ran.
nothing_changed() {
  home_empty
  no_brew_installed
  log_lacks '^(installer|brew|sudo|apt-get|release-install)'
  tmp_clean
}
no_brew_installed() {
  if [ -e "$LIN/bin/brew" ] || [ -e "$MAC/bin/brew" ]; then bad "a Homebrew got installed"; fi
}

# sub_done NAME: closes one row of a case that loops over rows.
sub_done() {
  SUB=1
  if [ "$FAILED" = 0 ]; then
    pass=$((pass + 1))
    printf 'PASS  %s\n' "$1"
  else
    fail=$((fail + 1))
    printf 'FAIL  %s\n' "$1"
    printf '%s\n' "$OUT" | tail -12 | sed 's/^/        | /'
  fi
  FAILED=0
}

run_case() {
  CASE=$1
  FAILED=0
  SUB=0
  "$1"
  if [ "$SUB" = 1 ]; then
    return 0
  fi
  if [ "$FAILED" = 0 ]; then
    pass=$((pass + 1))
    printf 'PASS  %s\n' "$CASE"
  else
    fail=$((fail + 1))
    printf 'FAIL  %s\n' "$CASE"
    printf '%s\n' "$OUT" | tail -25 | sed 's/^/        | /'
    if [ -s "${LOG:-/nonexistent}" ]; then printf '        log:\n'; sed 's/^/        > /' "$LOG" | tail -15; fi
  fi
}

# --- cases: the shape of the script -------------------------------------------------

t_shape_only_functions_and_main() {
  fresh
  OUT=""
  if ! bash -n "$INSTALL" 2>&1; then bad "bash -n fails"; fi
  # Everything is inside one { } block that ends with the call to main, so a cut
  # download is a syntax error (bash reads the whole block before running it).
  if [ "$(tail -n 1 "$INSTALL")" != '}' ]; then bad 'last line is not the closing brace'; fi
  if [ "$(tail -n 2 "$INSTALL" | head -n 1)" != 'main "$@"' ]; then bad 'the line before the closing brace is not: main "$@"'; fi
  local opens
  opens=$(grep -c '^{$' "$INSTALL" || true)
  if [ "$opens" != 1 ]; then bad "$opens lines of just { at column 0, wanted 1"; fi
  # Outside a function body (column 0) only comments, blank lines, the set line,
  # the block's braces, function openings and closings (and one-line functions)
  # and the final call.
  local stray
  stray=$(grep -nvE '^(#.*|)$|^set -euo pipefail$|^\{$|^[a-z_]+\(\) \{$|^[a-z_]+\(\) \{ .* \}$|^\}$|^main "\$@"$|^[[:space:]]' "$INSTALL" || true)
  if [ -n "$stray" ]; then bad "top-level lines that are not a function or the last call:"$'\n'"$stray"; fi
  local calls
  calls=$(grep -c '^main "\$@"$' "$INSTALL" || true)
  if [ "$calls" != 1 ]; then bad "main is called $calls times"; fi
  # bash 3.2: no associative arrays, mapfile, ${x,,}, ${x^^}, |&, &>>, coproc, negative array index
  local modern
  modern=$(grep -nE 'declare -A|local -A|mapfile|readarray|\$\{[A-Za-z_]+(,,|\^\^)\}|\|&|&>>|coproc|read -N|local -n|declare -n' "$INSTALL" || true)
  if [ -n "$modern" ]; then bad "bash 4+ syntax:"$'\n'"$modern"; fi
}

t_truncated_download_runs_nothing() {
  fresh
  ARGS=(--yes)
  local size offsets tail_cuts o bad_cuts=0 f=$T/cut.sh brace
  size=$(wc -c <"$INSTALL")
  # byte offset of the { that opens the block: a cut after it is a syntax error
  brace=$(grep -b '^{$' "$INSTALL" | head -n 1 | cut -d: -f1)
  offsets=""
  # every 12th line boundary, a spread of mid-line offsets, and the tail.
  offsets="$offsets $(awk 'NR % 12 == 0 { print off + length($0) + 1 } { off += length($0) + 1 }' "$INSTALL")"
  local k
  for k in $(seq 1 60); do offsets="$offsets $((size * k / 61))"; done
  # the end cut short: no closing brace, then main "$@" cut at every byte
  # (main "$@" / main "$@ / main "$ / main " / main / mai) and the lines before it
  tail_cuts=""
  for k in $(seq 2 14); do tail_cuts="$tail_cuts $((size - k))"; done
  tail_cuts="$tail_cuts "
  offsets="$offsets$tail_cuts"
  for o in $offsets; do
    # A line-boundary sample can equal the full file when its line count is
    # divisible by 12. That is a complete download, not a truncation.
    if [ "$o" -ge "$size" ]; then continue; fi
    head -c "$o" "$INSTALL" >"$f"
    FEED=$f
    : >"$LOG"
    play
    # a cut anywhere inside the block leaves it unclosed: a syntax error, non-zero
    if [ "$o" -gt "$brace" ] && [ "$RC" = 0 ]; then bad "cut at byte $o of $size (inside the block) exited 0"; fi
    if [ -s "$LOG" ] || [ -n "$(find "$HOME_" -mindepth 1 | head -1)" ] || grep -q 'installing on' <<<"$OUT"; then
      bad "cut at byte $o of $size: exit $RC, log '$(head -1 "$LOG")', home '$(find "$HOME_" -mindepth 1 | head -1)'"
      bad_cuts=$((bad_cuts + 1))
      if [ "$bad_cuts" -ge 3 ]; then break; fi
    fi
  done
  OUT="(cut-offs tried: $(wc -w <<<"$offsets"))"
}

t_colour_only_on_a_terminal() {
  fresh
  ready
  play
  rc_is 0
  if grep -q $'\033' <<<"$OUT"; then bad "escape codes in output that is not a terminal"; fi
  fresh
  ready
  play --raw --answer 'Put ~/.local/bin=' -- --no-setup
  rc_is 0
  if ! grep -q $'\033\\[1m' <<<"$OUT"; then bad "no bold on a terminal"; fi
}

# --- cases: refusals -----------------------------------------------------------------

t_refuses_root() {
  # runs as root (the "root" argument)
  fresh
  play
  rc_nonzero
  out_has 'not as root'
  log_empty
}

t_refuses_sudo_user() {
  fresh
  env_set SUDO_USER=somebody
  play
  rc_nonzero
  out_has 'not as root and not with sudo'
  log_empty
  home_empty
}

t_unsupported_os_and_arch() {
  fresh
  env_set STUB_OS=FreeBSD
  play
  rc_nonzero
  out_has 'macOS and Linux; this is FreeBSD'
  log_empty
  fresh
  env_set STUB_ARCH=armv7l
  play
  rc_nonzero
  out_has '64-bit Intel and ARM; this machine is armv7l'
  log_empty
}

t_no_tty_no_yes_changes_nothing() {
  fresh
  have_git
  play --no-tty -- --no-setup
  rc_is 1
  out_has 'nothing was changed'
  out_has 'It would:'
  out_has 'By hand:'
  out_has 'bash -s -- --yes'
  home_empty
  no_brew_installed
  log_empty
}

t_help_and_bad_flag() {
  fresh
  play --no-tty -- --help
  rc_is 0
  out_has '--no-setup'
  out_has '--version VERSION'
  log_empty
  play --no-tty -- --bogus
  rc_is 2
  out_has 'unknown option: --bogus'
  play --no-tty -- --source
  rc_is 2
  out_has 'unknown option: --source'
  play --no-tty -- --version
  rc_is 2
  out_has '--version needs a value'
  home_empty
}

# --- cases: Homebrew -----------------------------------------------------------------

t_decline_homebrew_prints_manual_steps() {
  fresh
  have_tools_pkg
  have_git
  play --answer 'Install Homebrew\? \[Y/n\]=n' -- --no-setup
  rc_is 1
  out_has 'Homebrew not installed'
  out_has 'By hand:'
  out_has 'brew install git age gitleaks'
  log_lacks 'raw.githubusercontent|installer|brew'
  no_brew_installed
  home_empty
}

t_accept_homebrew_asks_and_installs() {
  fresh
  have_tools_pkg
  have_git
  have_tool age
  play --answer 'Install Homebrew\? \[Y/n\]=' \
    --answer 'Put ~/.local/bin on your PATH in ~/.bashrc\? \[Y/n\]=y' -- --no-setup
  rc_is 0
  out_has 'It also adds Homebrew to your PATH in ~/.bashrc'
  out_has 'Ignore the "Next steps" Homebrew just printed'
  log_has '^curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh$'
  # interactive: no NONINTERACTIVE, stdin is the terminal
  log_has "^installer NONINTERACTIVE= stdin_tty=1 prefix=$LIN$"
  log_has '^brew install gitleaks$'
  log_lacks 'brew install .*(git |age)'
  file_has "$HOME_/.bashrc" "eval \"\$($LIN/bin/brew shellenv bash)\""
  file_has "$HOME_/.bashrc" 'export PATH="$HOME/.local/bin:$PATH"'
  out_has 'new terminal'
  out_has 'Next: ~/.local/bin/homebot setup'
  # the summary says what it installed or edited, and that the caches exist
  out_has 'Changed on this machine:'
  out_has '~/.bashrc \(added Homebrew to PATH\)'
  out_has '~/.bashrc \(added ~/.local/bin to PATH\)'
    out_has "Homebrew installed in $LIN"
  out_has 'installed with Homebrew: gitleaks'
  out_has 'installed ~/.local/bin/homebot from the release'
  out_has 'Homebrew also keeps caches'
  out_lacks 'every file'
}

t_yes_installs_homebrew_unattended() {
  fresh
  have_tools_pkg
  have_git
  play -- --yes --no-setup
  rc_is 0
  log_has '^installer NONINTERACTIVE=1 stdin_tty=0 prefix='
  out_lacks '\[Y/n\]'
  log_has '^brew install age gitleaks$'
}

t_homebrew_installer_failure_stops_with_its_code() {
  fresh
  have_tools_pkg
  have_git
  env_set STUB_INSTALLER_FAIL=7
  play
  rc_is 7
  out_has 'Homebrew installer failed \(exit 7\)'
  out_has 'brew.sh'
  log_lacks 'brew install|release-install'
  [ ! -e "$HOME_/.bashrc" ] || bad "the rc file was written after the installer failed"
}

t_homebrew_download_failure() {
  fresh
  have_tools_pkg
  have_git
  env_set STUB_CURL_FAIL=1
  play
  rc_nonzero
  out_has "could not download Homebrew's installer"
  log_lacks 'installer'
}

t_intel_mac_without_homebrew_stops() {
  fresh
  macos
  env_set STUB_ARCH=x86_64
  play
  rc_is 1
  out_has 'no longer supports Intel Macs'
  out_has 'By hand:'
  log_lacks 'raw.githubusercontent|installer'
}

t_intel_mac_with_homebrew_goes_on() {
  fresh
  macos
  env_set STUB_ARCH=x86_64
  ready
  rm -rf "${LIN:?}/bin"
  have_brew /usr/local path
  play
  rc_is 0
  out_lacks 'Intel Macs'
  log_lacks 'installer'
}

t_brew_found_off_path_still_gets_the_path_line() {
  fresh
  ready
  EXTRA=""
  have_brew "$LIN"
  play
  rc_is 0
  out_has "Homebrew: installed \\($LIN/bin/brew\\)"
  log_lacks 'installer'
  file_has "$HOME_/.bashrc" "eval \"\$($LIN/bin/brew shellenv bash)\""
}

t_brew_in_home_is_found() {
  fresh
  ready
  EXTRA=""
  rm -rf "${LIN:?}/bin"
  have_brew "$HOME_/.linuxbrew"
  play
  rc_is 0
  out_has "installed \\($HOME_/.linuxbrew/bin/brew\\)"
  log_lacks 'installer'
  file_has "$HOME_/.bashrc" "eval \"\$($HOME_/.linuxbrew/bin/brew shellenv bash)\""
}

t_brew_on_path_leaves_the_rc_file_alone() {
  fresh
  ready
  play
  rc_is 0
  out_has 'PATH: brew is already on it'
  [ ! -e "$HOME_/.bashrc" ] || file_count "$HOME_/.bashrc" 'brew shellenv' 0
}

t_path_line_per_shell_and_os() {
  local row os shell rc suffix prefix
  for row in \
    "linux|/bin/bash|.bashrc| bash" \
    "linux|/usr/bin/zsh|.zshrc| zsh" \
    "linux|/usr/bin/fish|.config/fish/config.fish| fish" \
    "linux|/bin/dash|.profile|" \
    "linux||.profile|" \
    "mac|/bin/bash|.bash_profile| bash" \
    "mac|/bin/zsh|.zprofile| zsh" \
    "mac|/opt/homebrew/bin/fish|.config/fish/config.fish| fish" \
    "mac|/bin/sh|.profile|"; do
    IFS='|' read -r os shell rc suffix <<<"$row"
    fresh
    ready
    EXTRA=""
    rm -rf "${LIN:?}/bin"
    prefix=$LIN
    if [ "$os" = mac ]; then macos; prefix=$MAC; fi
    have_brew "$prefix"
    ENVV=("SHELL=$shell" "USER=tester")
    if [ "$os" = mac ]; then macos; fi
    play
    rc_is 0
    file_has "$HOME_/$rc" "eval \"\$($prefix/bin/brew shellenv$suffix)\""
    file_count "$HOME_/$rc" 'brew shellenv' 1
    # a blank line before it, and only this one line added by that step
    if [ "$(head -n 1 "$HOME_/$rc")" != "" ]; then bad "no blank line before the PATH line in $rc"; fi
    file_count "$HOME_/$rc" '.local/bin' 1
    sub_done "t_path_line_per_shell_and_os[$os ${shell:-no SHELL}]"
  done
}

t_path_line_zdotdir_and_env() {
  fresh
  ready
  EXTRA=""
  have_brew "$LIN"
  mkdir -p "$HOME_/zd"
  ENVV=("SHELL=/bin/zsh" "ZDOTDIR=$HOME_/zd" "USER=tester")
  play
  rc_is 0
  file_has "$HOME_/zd/.zshrc" "brew shellenv zsh"
  [ ! -e "$HOME_/.zshrc" ] || bad "wrote ~/.zshrc although ZDOTDIR is set"
  fresh
  ready
  EXTRA=""
  have_brew "$LIN"
  ENVV=("SHELL=/bin/sh" "ENV=$HOME_/shrc" "USER=tester")
  play
  rc_is 0
  file_has "$HOME_/shrc" "brew shellenv)"
}

t_second_run_adds_nothing() {
  fresh
  ready
  EXTRA=""
  have_brew "$LIN"
  play
  rc_is 0
  file_count "$HOME_/.bashrc" 'brew shellenv' 1
  local before after
  before=$(cksum <"$HOME_/.bashrc")
  play
  rc_is 0
  after=$(cksum <"$HOME_/.bashrc")
  if [ "$before" != "$after" ]; then bad "the second run changed ~/.bashrc"; fi
  file_count "$HOME_/.bashrc" 'brew shellenv' 1
  file_count "$HOME_/.bashrc" '.local/bin' 1
  out_has 'already sets up Homebrew'
}

t_second_run_after_a_real_install_does_not_reinstall() {
  fresh
  have_tools_pkg
  have_git
  play -- --yes --no-setup
  rc_is 0
  : >"$LOG"
  play -- --yes --no-setup
  rc_is 0
  log_lacks '^installer|Homebrew/install'
  # the release is looked at again, and its installer runs again
  log_count '^release-install ' 1
  file_count "$HOME_/.bashrc" 'brew shellenv' 1
}

t_symlinked_rc_file_is_appended_through() {
  fresh
  ready
  EXTRA=""
  have_brew "$LIN"
  mkdir -p "$HOME_/homerepo"
  printf '# managed\n' >"$HOME_/homerepo/bashrc"
  ln -s "$HOME_/homerepo/bashrc" "$HOME_/.bashrc"
  play
  rc_is 0
  [ -L "$HOME_/.bashrc" ] || bad "~/.bashrc is no longer a symlink"
  file_has "$HOME_/homerepo/bashrc" "brew shellenv bash"
  file_has "$HOME_/homerepo/bashrc" '# managed'
  out_has 'PATH: added Homebrew to ~/homerepo/bashrc \(the link ~/.bashrc points there\)'
}

t_existing_rc_content_is_only_appended_to() {
  fresh
  ready
  EXTRA=""
  have_brew "$LIN"
  printf 'alias a=b\nexport X=1' >"$HOME_/.bashrc"
  play
  rc_is 0
  if [ "$(head -c 20 "$HOME_/.bashrc")" != $'alias a=b\nexport X=1' ]; then
    bad "the original bytes of ~/.bashrc changed: $(head -c 40 "$HOME_/.bashrc")"
  fi
  file_has "$HOME_/.bashrc" 'alias a=b'
  file_count "$HOME_/.bashrc" 'brew shellenv' 1
}

t_rc_with_brew_shellenv_already_is_left_alone() {
  fresh
  ready
  EXTRA=""
  have_brew "$LIN"
  printf '# mine\neval "$(/opt/elsewhere/bin/brew shellenv)"\nexport PATH="$HOME/.local/bin:$PATH"\n' >"$HOME_/.bashrc"
  local before
  before=$(cksum <"$HOME_/.bashrc")
  play
  rc_is 0
  if [ "$before" != "$(cksum <"$HOME_/.bashrc")" ]; then bad "an rc file that already had both lines changed"; fi
}

# --- cases: Linux build tools ------------------------------------------------------------

t_build_tools_present_ask_nothing() {
  fresh
  ready
  play
  rc_is 0
  out_has 'Build tools: installed'
  log_lacks 'sudo'
}

t_build_tools_missing_yes_runs_sudo() {
  fresh
  ready
  rm -f "$STATE/tools"
  play
  rc_is 0
  out_has 'sudo apt-get install -y build-essential'
  # --yes with no terminal must not wait for a password: sudo -n
  log_has '^sudo -n apt-get update$'
  log_has '^sudo -n apt-get install -y build-essential$'
  out_has 'Build tools: installed'
}

t_build_tools_missing_asks_and_runs() {
  fresh
  ready
  rm -f "$STATE/tools"
  play --answer 'Run that with sudo\? \[Y/n\]=y' --answer 'Put ~/.local/bin=' -- --no-setup
  rc_is 0
  log_has '^sudo apt-get install -y build-essential$'
  log_lacks 'sudo -n'
}

t_build_tools_declined_prints_command_and_continues() {
  fresh
  ready
  rm -f "$STATE/tools"
  play --answer 'Run that with sudo\? \[Y/n\]=n' --answer 'Put ~/.local/bin=' -- --no-setup
  rc_is 0
  out_has 'Skipped. Run it yourself'
  out_has 'sudo apt-get install -y build-essential'
  out_has 'Going on without the build tools'
  log_lacks '^sudo'
  log_has '^release-install '
}

t_build_tools_without_sudo_prints_command_and_continues() {
  fresh
  ready
  rm -f "$STATE/tools" "$BIN/sudo"
  play
  rc_is 0
  out_has 'no sudo here'
  out_has 'sudo apt-get install -y build-essential'
  log_has '^release-install '
}

t_build_tools_sudo_failure_continues() {
  fresh
  ready
  rm -f "$STATE/tools"
  env_set STUB_SUDO_FAIL=1
  play
  rc_is 0
  out_has 'That did not finish'
  log_has '^release-install '
}

t_git_missing_before_homebrew_is_installed_with_the_tools() {
  fresh
  have_tools_pkg
  rm -f "$STATE/tools"
  play --answer 'Install Homebrew\? \[Y/n\]=' --answer 'Run that with sudo\? \[Y/n\]=y' \
    --answer 'Put ~/.local/bin=' -- --no-setup
  rc_is 0
  out_has 'needs git, and a compiler'
  log_has '^sudo apt-get install -y build-essential git$'
  # the installer only ran after git was there
  log_has '^installer '
  local a b
  a=$(grep -n '^sudo apt-get install' "$LOG" | head -1 | cut -d: -f1)
  b=$(grep -n '^installer ' "$LOG" | head -1 | cut -d: -f1)
  if [ -z "$a" ] || [ -z "$b" ] || [ "$a" -ge "$b" ]; then bad "the installer ran before the packages (lines $a, $b)"; fi
}

t_git_missing_and_declined_stops_before_the_installer() {
  fresh
  have_tools_pkg
  play --answer 'Install Homebrew\? \[Y/n\]=' --answer 'Run that with sudo\? \[Y/n\]=n' -- --no-setup
  rc_nonzero
  out_has "Homebrew's installer needs git first"
  log_lacks '^installer|^brew|^sudo'
  no_brew_installed
}

t_git_missing_with_brew_present_is_brew_installed() {
  fresh
  ready
  rm -f "$BIN/git"
  play
  rc_is 0
  log_has '^brew install git$'
  log_lacks 'sudo'
}

t_other_package_managers() {
  local row pm cmd
  for row in \
    "dnf|sudo dnf group install -y 'Development Tools'" \
    "yum|sudo yum groupinstall -y 'Development Tools'" \
    "pacman|sudo pacman -S --needed --noconfirm base-devel" \
    "apk|sudo apk add build-base"; do
    IFS='|' read -r pm cmd <<<"$row"
    fresh
    ready
    use_pm "$pm"
    rm -f "$STATE/tools"
    play
    rc_is 0
    if ! grep -qF -- "$cmd" <<<"$OUT"; then bad "output lacks: $cmd"; fi
    sub_done "t_other_package_managers[$pm]"
  done
}

t_dnf_group_id_follows_homebrew() {
  # Homebrew: development-tools on Fedora and RHEL-likes, 'Development Tools' on Amazon Linux.
  fresh
  ready
  use_pm dnf
  local saved=$T/os-release.saved row want
  cp /etc/os-release "$saved"
  for row in \
    'ID=fedora|development-tools' \
    'ID=rocky\nID_LIKE="rhel centos fedora"|development-tools' \
    'ID=amzn\nID_LIKE="fedora"|'"'Development Tools'" \
    'ID=opensuse|'"'Development Tools'"; do
    printf '%b\n' "${row%%|*}" >/etc/os-release
    want=${row#*|}
    rm -f "$STATE/tools"
    play
    rc_is 0
    out_has "sudo dnf group install -y $want"
  done
  cp "$saved" /etc/os-release
}

t_macos_asks_no_build_tools() {
  fresh
  macos
  ready
  rm -rf "${LIN:?}/bin"
  have_brew "$MAC" path
  rm -f "$STATE/tools"
  play
  rc_is 0
  out_lacks 'Build tools'
  log_lacks 'sudo|apt-get|dpkg'
}

# --- cases: tools -----------------------------------------------------------------------

t_all_tools_present_no_brew_install() {
  fresh
  ready
  play
  rc_is 0
  out_has 'git: ok'
  out_has 'age: ok'
  out_has 'gitleaks: ok'
  log_lacks '^brew install'
}

t_only_missing_tools_are_installed() {
  fresh
  ready
  rm -f "$BIN/age" "$BIN/gitleaks"
  play
  rc_is 0
  out_has 'age: installing'
  out_has 'gitleaks: installing'
  log_count '^brew install' 1
  log_has '^brew install age gitleaks$'
}

t_old_git_counts_as_missing() {
  fresh
  ready
  env_set "STUB_GIT_VERSION=2.39.5 (Apple Git-154)"
  play
  rc_is 0
  out_has 'git: too old'
  log_has '^brew install git$'
  # the brew git is first on PATH afterwards, and it is new enough
  log_has '^git --version'
  out_lacks 'older than'
}

t_git_2_40_is_enough() {
  fresh
  ready
  env_set "STUB_GIT_VERSION=2.40.0"
  play
  rc_is 0
  log_lacks '^brew install'
  fresh
  ready
  env_set "STUB_GIT_VERSION=3.0.1"
  play
  rc_is 0
  log_lacks '^brew install'
  fresh
  ready
  env_set "STUB_GIT_VERSION=2.9.5"
  play
  rc_is 0
  log_has '^brew install git$'
}

t_brew_install_failure_stops() {
  fresh
  ready
  rm -f "$BIN/age"
  env_set STUB_BREW_INSTALL_FAIL=1
  play
  rc_nonzero
  out_has 'brew install age failed'
  log_lacks 'release-install'
}

# --- cases: the release ------------------------------------------------------------------

t_latest_release_is_downloaded_checked_and_run() {
  fresh
  ready
  play
  rc_is 0
  # the latest is looked up once; everything else is for the tag it names
  log_count 'releases/latest/' 1
  log_has '^curl .*https://github.com/hyperlab-sh/homebot/releases/latest/download/checksums.txt$'
  log_has '^curl .*https://github.com/hyperlab-sh/homebot/releases/download/v0.1.0/checksums.txt$'
  log_has '^curl .*https://github.com/hyperlab-sh/homebot/releases/download/v0.1.0/install.sh$'
  log_lacks 'releases/latest/download/install.sh'
  # https only, TLS 1.2 or newer, two retries, and the status asked for
  log_has "^curl -sSL --proto =https --tlsv1.2 .* --retry 2 .* -w %\\{http_code\\} https://github.com/hyperlab-sh/homebot/releases/download/v0.1.0/install.sh$"
  out_has 'Release: install.sh matches checksums.txt'
  # as the user, with no terminal input, and nothing but what the person gave
  log_has '^release-install args=--version v0.1.0 user=[1-9][0-9]* SUDO_USER= stdin_tty=0$'
  log_count '^release-install ' 1
  [ -x "$HOME_/.local/bin/homebot" ] || bad "no binary at ~/.local/bin/homebot"
  out_has 'homebot: installed \(~/.local/bin/homebot, homebot version 0.0.0-stub\)'
  out_has 'installed ~/.local/bin/homebot from the release'
  out_lacks 'private until|go build|git clone|build homebot|from source'
  log_lacks '^git clone|^go '
  tmp_clean
}

t_the_release_is_checked_before_anything_changes() {
  fresh
  have_tools_pkg
  have_git
  play
  rc_is 0
  local a b c
  a=$(grep -n 'releases/download/v0.1.0/install.sh$' "$LOG" | head -1 | cut -d: -f1)
  b=$(grep -n '^installer ' "$LOG" | head -1 | cut -d: -f1)
  c=$(grep -n '^release-install ' "$LOG" | head -1 | cut -d: -f1)
  if [ -z "$a" ] || [ -z "$b" ] || [ -z "$c" ] || [ "$a" -ge "$b" ] || [ "$b" -ge "$c" ]; then
    bad "wanted: download, Homebrew installer, release installer; got lines $a, $b, $c"
  fi
}

t_release_installer_runs_after_the_tools() {
  fresh
  ready
  rm -f "$BIN/age"
  play
  rc_is 0
  local a b
  a=$(grep -n '^brew install age$' "$LOG" | head -1 | cut -d: -f1)
  b=$(grep -n '^release-install ' "$LOG" | head -1 | cut -d: -f1)
  if [ -z "$a" ] || [ -z "$b" ] || [ "$a" -ge "$b" ]; then bad "the release installer did not come after brew install (lines $a, $b)"; fi
}

t_pinned_version_is_downloaded_and_passed_on() {
  local row
  for row in "--version v0.1.0" "--version=v0.1.0" "--version 0.1.0" "--version=0.1.0"; do
    fresh
    ready
    # shellcheck disable=SC2086 # the row is one flag and its value, split on purpose
    play -- --yes --no-setup $row
    rc_is 0
    log_has '^curl .*https://github.com/hyperlab-sh/homebot/releases/download/v0.1.0/checksums.txt$'
    log_has '^curl .*https://github.com/hyperlab-sh/homebot/releases/download/v0.1.0/install.sh$'
    log_lacks 'releases/latest'
    log_has '^release-install args=--version v0.1.0 '
    sub_done "t_pinned_version[$row]"
  done
  fresh
  ready
  env_set STUB_TAG=v0.2.0-rc.1
  play -- --yes --no-setup --version v0.2.0-rc.1
  rc_is 0
  log_has '^release-install args=--version v0.2.0-rc.1 '
}

t_a_release_published_meanwhile_changes_nothing() {
  fresh
  ready
  env_set STUB_LATEST_FLIP=1
  play
  rc_is 0
  # "latest" is asked once; the script and checksums are the first release's,
  # and the installer is told which release to install
  log_count 'releases/latest/' 1
  log_lacks 'latest moved on'
  log_has '^release-install args=--version v0.1.0 '
  out_has 'Release: install.sh matches checksums.txt'
}

t_archive_names_must_name_one_release() {
  local kind
  for kind in mixedver noarchive; do
    fresh
    have_tools_pkg
    have_git
    env_set "STUB_RELEASE=$kind"
    play
    rc_is 1
    out_has 'no single release version in its archive names'
    nothing_changed
    sub_done "t_archive_names[$kind]"
  done
}

t_pinned_version_that_does_not_exist_changes_nothing() {
  fresh
  have_tools_pkg
  have_git
  play -- --yes --no-setup --version v9.9.9
  rc_is 1
  out_has 'homebot has no release v9.9.9'
  nothing_changed
}

t_no_release_yet_refuses_and_changes_nothing() {
  local where
  for where in bare ready macos; do
    fresh
    case $where in
      bare) have_tools_pkg; have_git ;;
      ready) ready ;;
      macos) macos; ready ;;
    esac
    env_set STUB_RELEASE=none
    play
    rc_is 1
    out_has 'homebot has no release yet'
    out_has 'Nothing was changed'
    # no fall-back to anything else
    out_lacks 'building|cloning|go build'
    log_lacks 'git clone|^go |installer|brew |sudo'
    log_lacks '^release-install '
    home_empty
    if [ "$where" = bare ]; then no_brew_installed; fi
    tmp_clean
    [ ! -e "$HOME_/.local/bin/homebot" ] || bad "a binary exists"
    sub_done "t_no_release_yet[$where]"
  done
  # on a terminal, too: nothing is asked first
  fresh
  ready
  env_set STUB_RELEASE=none
  PTY=()
  play -- --no-setup
  rc_is 1
  out_has 'homebot has no release yet'
  out_lacks '\[Y/n\]'
  home_empty
}

t_a_checksum_that_does_not_match_stops_before_anything_changes() {
  local kind
  for kind in mismatch tampered; do
    fresh
    have_tools_pkg
    have_git
    mkdir -p "$HOME_/.local/bin"
    echo old >"$HOME_/.local/bin/homebot"
    env_set "STUB_RELEASE=$kind"
    play
    rc_is 1
    out_has 'install.sh does not match its checksum in checksums.txt'
    log_lacks '^(installer|brew|sudo|apt-get|release-install|tampered)'
    no_brew_installed
    tmp_clean
    if [ "$(cat "$HOME_/.local/bin/homebot")" != old ]; then bad "the old binary was touched"; fi
    sub_done "t_checksum_does_not_match[$kind]"
  done
  for kind in nosum twosum; do
    fresh
    have_tools_pkg
    have_git
    env_set "STUB_RELEASE=$kind"
    play
    rc_is 1
    out_has 'has no single line for install.sh'
    nothing_changed
    sub_done "t_checksum_line_is_not_unique[$kind]"
  done
}

t_a_failed_download_changes_nothing() {
  local kind want
  for row in "error|HTTP 500" "offline|HTTP 000"; do
    kind=${row%%|*}
    want=${row#*|}
    fresh
    have_tools_pkg
    have_git
    env_set "STUB_RELEASE=$kind"
    play
    rc_is 1
    out_has "could not download .*checksums.txt \\($want\\)"
    out_lacks 'no release'
    if [ "$kind" = offline ]; then out_has 'Could not resolve host'; fi
    nothing_changed
    sub_done "t_failed_download[$kind]"
  done
}

t_no_curl_stops_before_anything_changes() {
  fresh
  have_tools_pkg
  have_git
  rm -f "$BIN/curl"
  play
  rc_is 1
  out_has 'curl is needed to download the release'
  nothing_changed
}

t_a_failing_release_installer_stops_the_run() {
  fresh
  ready
  env_set STUB_RELEASE_INSTALL_FAIL=3
  play
  rc_is 1
  out_has 'archive checksum mismatch'
  out_has 'the release installer failed'
  log_has '^release-install '
  # nothing after it: no PATH line for ~/.local/bin, no setup, no list of changes
  [ ! -e "$HOME_/.bashrc" ] || file_count "$HOME_/.bashrc" '.local/bin' 0
  log_lacks 'homebot setup'
  out_lacks 'Changed on this machine'
  [ ! -e "$HOME_/.local/bin/homebot" ] || bad "a binary exists"
  tmp_clean
}

t_a_release_installer_that_leaves_no_binary_stops_the_run() {
  fresh
  ready
  env_set STUB_RELEASE_INSTALL_NOOP=1
  play
  rc_is 1
  out_has 'there is no .*/.local/bin/homebot'
  log_lacks 'homebot setup'
}

t_bad_versions_are_refused() {
  local row
  # VALUE, as one word each; the empty value and look-alikes are rows too
  for row in '1.2' 'latest' 'v1' 'v1.0' 'v1.0.0.0' 'v1.0.0;id' 'v1.0.0-beta' 'v1.0.0-rc' '$(id)' 'V1.0.0' ' v1.0.0' 'v1.0.0 ' '../v1.0.0' 'v1.0.0/../x' $'v1.0.0\nid' ''; do
    fresh
    play -- --version "$row" --yes
    rc_is 2
    out_has 'Error: --version needs'
    out_lacks 'installing on'
    nothing_changed
    sub_done "bad version: $(printf '%q' "$row")"
  done
  fresh
  play -- --version
  rc_is 2
  out_has 'Error: --version needs a value'
  nothing_changed
}

t_the_source_build_is_gone() {
  fresh
  OUT=""
  local left
  left=$(grep -nE 'go build|git clone|git pull|private until|SWAP POINT|--source|GO_CMD|ls-remote' "$INSTALL" || true)
  if [ -n "$left" ]; then bad "the script still has the source build or its messages:"$'\n'"$left"; fi
  play --no-tty -- --yes --source /tmp/x
  rc_is 2
  out_has 'unknown option: --source'
  nothing_changed
}

t_no_terminal_plan_names_the_release_and_keeps_the_version() {
  fresh
  have_git
  play --no-tty -- --version v0.1.0 --persona neatpik
  rc_is 1
  out_has "download the latest release's install.sh"
  out_has 'bash -s -- --yes --version v0.1.0 --persona=neatpik$'
  out_has 'Install the release: https://github.com/hyperlab-sh/homebot/releases/latest'
  out_lacks 'go build|git clone'
  nothing_changed
}

# --- cases: ~/.local/bin ----------------------------------------------------------------

t_local_bin_on_path_asks_nothing() {
  fresh
  ready
  EXTRA="$LIN/bin:$HOME_/.local/bin"
  play
  rc_is 0
  out_has "PATH: ~/.local/bin is on it"
  [ ! -e "$HOME_/.bashrc" ] || file_count "$HOME_/.bashrc" '.local/bin' 0
}

t_local_bin_line_per_shell() {
  fresh
  ready
  play
  rc_is 0
  file_has "$HOME_/.bashrc" 'export PATH="$HOME/.local/bin:$PATH"'
  fresh
  ready
  ENVV=("SHELL=/usr/bin/fish" "USER=tester")
  play
  rc_is 0
  file_has "$HOME_/.config/fish/config.fish" 'fish_add_path $HOME/.local/bin'
  out_lacks 'export PATH'
}

t_local_bin_declined() {
  fresh
  ready
  play --answer 'Put ~/.local/bin on your PATH in ~/.bashrc\? \[Y/n\]=n' -- --no-setup
  rc_is 0
  out_has 'Not added'
  [ ! -e "$HOME_/.bashrc" ] || file_count "$HOME_/.bashrc" '.local/bin' 0
}

t_local_bin_already_in_rc_is_not_added_twice() {
  fresh
  ready
  printf 'export PATH="$HOME/.local/bin:$PATH"\n' >"$HOME_/.bashrc"
  play
  rc_is 0
  out_has 'already puts ~/.local/bin on it'
  file_count "$HOME_/.bashrc" '.local/bin' 1
}

# --- cases: the end ----------------------------------------------------------------------

t_no_setup_stops_and_says_the_next_command() {
  fresh
  ready
  play
  rc_is 0
  out_has 'Next: ~/.local/bin/homebot setup'
  log_lacks 'homebot setup'
  # the new-terminal note exactly once
  local n
  n=$(grep -c 'A new terminal' <<<"$OUT" || true)
  if [ "$n" != 1 ]; then bad "the new-terminal note was printed $n times"; fi
}

t_setup_is_exec_ed_with_the_terminal() {
  fresh
  ready
  play --answer 'Put ~/.local/bin on your PATH in ~/.bashrc\? \[Y/n\]=' --
  rc_is 0
  log_has '^homebot setup args=setup stdin_tty=1$'
  # exec: the stub took the script's place, so its parent is not a `bash -s`
  log_has '^homebot parent=bash -c '
  log_lacks '^homebot parent=bash -s'
  out_has 'A new terminal'
  out_has 'Starting homebot setup'
}

t_setup_exit_code_is_the_scripts() {
  fresh
  ready
  env_set STUB_SETUP_RC=3
  play --answer 'Put ~/.local/bin on your PATH in ~/.bashrc\? \[Y/n\]=' --
  rc_is 3
}

t_yes_without_a_terminal_skips_setup() {
  fresh
  ready
  play --no-tty -- --yes
  rc_is 0
  log_lacks 'homebot setup'
  out_has 'no terminal for the setup'
}

# --- cases: --persona and --name -----------------------------------------------------------

# setup_args LINE: the exact arguments the stub setup recorded.
setup_args() { log_has "^homebot setup args=$1 stdin_tty=1\$"; }

# flags_run ARGS...: a ready machine, a terminal, the script run with ARGS and
# the PATH question answered.
flags_run() {
  fresh
  ready
  play --answer 'Put ~/.local/bin on your PATH in ~/.bashrc\? \[Y/n\]=' -- "$@"
}

t_flags_both_reach_setup_as_equals_forms() {
  flags_run --persona neatpik --name zork
  rc_is 0
  setup_args 'setup --persona=neatpik --name=zork'
  flags_run --persona=neatpik --name=zork
  rc_is 0
  setup_args 'setup --persona=neatpik --name=zork'
  # mixed spellings, and the flags in the other order
  flags_run --name=zork --persona neatpik
  rc_is 0
  setup_args 'setup --persona=neatpik --name=zork'
}

t_flags_each_alone() {
  flags_run --persona neatpik
  rc_is 0
  setup_args 'setup --persona=neatpik'
  flags_run --name=zork
  rc_is 0
  setup_args 'setup --name=zork'
}

t_no_flags_means_a_bare_setup() {
  flags_run
  rc_is 0
  setup_args 'setup'
}

t_flags_edge_values_pass() {
  flags_run --persona a --name zz
  rc_is 0
  setup_args 'setup --persona=a --name=zz'
  flags_run --persona=a1-b --name=z0-
  rc_is 0
  setup_args 'setup --persona=a1-b --name=z0-'
  local v32
  v32="a$(printf 'b%.0s' $(seq 1 31))"
  flags_run --persona "$v32" --name "$v32"
  rc_is 0
  setup_args "setup --persona=$v32 --name=$v32"
}

t_flags_last_one_wins() {
  flags_run --name one --name=two
  rc_is 0
  setup_args 'setup --name=two'
}

t_no_setup_prints_the_full_command() {
  fresh
  ready
  play -- --yes --no-setup --persona neatpik --name zork
  rc_is 0
  out_has 'Next: ~/.local/bin/homebot setup --persona=neatpik --name=zork$'
  log_lacks 'homebot setup'
  play -- --yes --no-setup --name=zork
  out_has 'Next: ~/.local/bin/homebot setup --name=zork$'
  play -- --yes --no-setup
  out_has 'Next: ~/.local/bin/homebot setup$'
}

t_no_terminal_with_flags_prints_the_full_command() {
  fresh
  ready
  play --no-tty -- --yes --persona neatpik --name zork
  rc_is 0
  out_has 'no terminal for the setup. Next: ~/.local/bin/homebot setup --persona=neatpik --name=zork$'
  log_lacks 'homebot setup'
}

t_no_terminal_without_yes_retains_the_choices() {
  fresh
  ready
  play --no-tty -- --persona neatpik --name=zork
  rc_is 1
  out_has "curl -fsSL https://homebot.sh/install \| bash -s -- --yes --persona=neatpik --name=zork$"
  out_has "run 'homebot setup --persona=neatpik --name=zork'$"
  out_has 'then run: homebot setup --persona=neatpik --name=zork$'
  out_has 'nothing was changed'
  log_empty
  home_empty
}

t_bad_flag_values_stop_before_anything_changes() {
  local row flag val
  # FLAG<TAB>VALUE; the empty value and the one-letter name are rows too
  for row in \
    $'--name\tZork' $'--name\t-x' $'--name\tz' $'--name\ta;b' $'--name\t$(id)' $'--name\t' \
    $'--name\t1zork' $'--name\tzo_rk' $'--name\tzo rk' $'--name\tzo.rk' $'--name\t--persona' \
    $'--name\t`id`' $'--name\ta&b' $'--name\ta|b' $'--name\t../x' $'--name\tzo/rk' \
    $'--name\té' $'--name\tzork\nid' $'--name\tzork\n' $'--name\t\033[2Jzork' \
    $'--persona\tNeatpik' $'--persona\t-x' $'--persona\ta;b' $'--persona\t$(id)' $'--persona\t' \
    $'--persona\t9a' $'--persona\ta b' $'--persona\t--name'; do
    flag=${row%%$'\t'*}
    val=${row#*$'\t'}
    # the same value in both spellings
    for form in space equals; do
      fresh
      if [ "$form" = space ]; then
        play -- "$flag" "$val" --yes
      else
        play -- "$flag=$val" --yes
      fi
      rc_is 2
      out_has "Error: $flag needs"
      out_lacks 'installing on'
      out_lacks $'\033\\[2J'
      log_empty
      home_empty
      no_brew_installed
      sub_done "bad value: $flag $(printf '%q' "$val") ($form)"
    done
  done
}

t_long_values_are_refused() {
  local v33 v60
  v33="a$(printf 'b%.0s' $(seq 1 32))"
  v60="a$(printf 'b%.0s' $(seq 1 59))"
  fresh
  ready
  play -- --name "$v33"
  rc_is 2
  out_has 'Error: --name needs'
  log_empty
  play -- --name "$v60"
  rc_is 2
  out_has '\.\.\.\)'  # long values are cut in the message
  out_lacks "$v60"
  play -- --persona="$v33"
  rc_is 2
  log_empty
  home_empty
}

t_missing_value_at_the_end() {
  fresh
  ready
  play -- --name
  rc_is 2
  out_has 'Error: --name needs a value'
  play -- --persona
  rc_is 2
  out_has 'Error: --persona needs a value'
  play -- --name
  rc_is 2
  out_has 'Error: --name needs a value'
  log_empty
  home_empty
}

t_bad_value_is_checked_even_with_no_terminal_and_help_is_last() {
  fresh
  play --no-tty -- --name Zork
  rc_is 2
  out_lacks 'nothing was changed'
  # a good value does not hide the normal no-terminal stop
  play --no-tty -- --name zork
  rc_is 1
  out_has 'nothing was changed'
  log_empty
  home_empty
}

t_help_lists_the_flags() {
  fresh
  play --no-tty -- --help
  rc_is 0
  out_has '--persona ID'
  out_has '--name NAME'
}

t_hostile_values_never_reach_setup_or_a_shell() {
  fresh
  ready
  # a name that passed the check is one word; nothing in it is special to a shell
  play --answer 'Put ~/.local/bin on your PATH in ~/.bashrc\? \[Y/n\]=' -- --name 'zork' --persona 'neatpik'
  rc_is 0
  local n
  n=$(grep -c '^homebot setup args=' "$LOG" || true)
  if [ "$n" != 1 ]; then bad "setup ran $n times"; fi
  # an option-looking value is never handed on
  fresh
  ready
  play -- --name --persona=x
  rc_is 2
  log_empty
}

# --- cases: asking before the startup file ---------------------------------------------------

t_found_brew_asks_before_editing_the_rc_file() {
  fresh
  ready
  EXTRA=""
  have_brew "$LIN"
  # "no": the file stays as it is, the line is shown, the rest goes on
  play --answer 'Add Homebrew to your PATH in ~/.bashrc\? \[Y/n\]=n' \
    --answer 'Put ~/.local/bin on your PATH in ~/.bashrc\? \[Y/n\]=n' -- --no-setup
  rc_is 0
  out_has 'Not added'
  out_has "eval \"\\\$\\($LIN/bin/brew shellenv bash\\)\""
  [ ! -e "$HOME_/.bashrc" ] || bad "~/.bashrc was written after a no"
  # brew still works for the rest of the run (the in-process eval)
  log_has '^release-install '
  # "yes" writes it
  fresh
  ready
  EXTRA=""
  have_brew "$LIN"
  play --answer 'Add Homebrew to your PATH in ~/.bashrc\? \[Y/n\]=' \
    --answer 'Put ~/.local/bin on your PATH in ~/.bashrc\? \[Y/n\]=n' -- --no-setup
  rc_is 0
  file_count "$HOME_/.bashrc" 'brew shellenv bash' 1
}

t_found_brew_with_yes_writes_without_asking() {
  fresh
  ready
  EXTRA=""
  have_brew "$LIN"
  play
  rc_is 0
  out_lacks 'Add Homebrew to your PATH'
  file_count "$HOME_/.bashrc" 'brew shellenv bash' 1
  out_has '~/.bashrc \(added Homebrew to PATH\)'
}

t_brew_on_path_asks_nothing_about_the_rc_file() {
  fresh
  ready
  play --answer 'Put ~/.local/bin on your PATH in ~/.bashrc\? \[Y/n\]=n' -- --no-setup
  rc_is 0
  out_lacks 'Add Homebrew to your PATH'
}

t_new_bash_profile_hides_profile_on_macos() {
  fresh
  macos
  ready
  EXTRA=""
  rm -rf "${LIN:?}/bin"
  have_brew "$MAC"
  echo 'export A=1' >"$HOME_/.profile"
  play
  rc_is 0
  file_has "$HOME_/.bash_profile" 'brew shellenv bash'
  out_has 'bash no longer reads ~/.profile'
}

# --- cases: sudo, ordering and messages ----------------------------------------------------

t_yes_on_a_terminal_warms_sudo_for_homebrew() {
  fresh
  have_tools_pkg
  have_git
  PTY=()
  play -- --yes --no-setup
  rc_is 0
  log_has '^sudo -v$'
  log_has '^installer NONINTERACTIVE=1 '
  local a b
  a=$(grep -n '^sudo -v$' "$LOG" | head -1 | cut -d: -f1)
  b=$(grep -n '^installer ' "$LOG" | head -1 | cut -d: -f1)
  if [ -z "$a" ] || [ -z "$b" ] || [ "$a" -ge "$b" ]; then bad "sudo -v did not come before the installer (lines $a, $b)"; fi
}

t_yes_without_a_terminal_does_not_ask_sudo_for_a_password() {
  fresh
  have_tools_pkg
  have_git
  play --no-tty -- --yes --no-setup
  rc_is 0
  log_lacks '^sudo -v$'
}

t_failed_download_changes_nothing_on_the_system() {
  fresh
  # neither git nor build tools; the Homebrew download fails
  env_set STUB_CURL_FAIL=1
  play
  rc_nonzero
  out_has "could not download Homebrew's installer"
  log_lacks 'sudo|apt-get|installer'
}

# --- cases: ~/.local/bin line, strictly ----------------------------------------------------

t_local_bin_check_wants_a_path_assignment() {
  local row name content
  for row in \
    'sources a file|. "$HOME/.local/bin/env"' \
    'a comment|# export PATH="$HOME/.local/bin:$PATH"' \
    'an alias|alias hb=~/.local/bin/homebot' \
    'another folder|export PATH="$HOME/.local/binaries:$PATH"'; do
    name=${row%%|*}
    content=${row#*|}
    fresh
    ready
    printf '%s\n' "$content" >"$HOME_/.bashrc"
    play
    rc_is 0
    out_lacks 'already puts'
    file_has "$HOME_/.bashrc" 'export PATH="$HOME/.local/bin:$PATH"'
    sub_done "t_local_bin_check_wants_a_path_assignment[$name]"
  done
  fresh
  ready
  ENVV=("SHELL=/usr/bin/fish" "USER=tester")
  mkdir -p "$HOME_/.config/fish"
  printf 'fish_add_path $HOME/.local/bin\n' >"$HOME_/.config/fish/config.fish"
  play
  rc_is 0
  out_has 'already puts'
  file_count "$HOME_/.config/fish/config.fish" '.local/bin' 1
}

# --- cases: big startup files -------------------------------

# 200 KB of ordinary, non-comment lines that name nothing of interest (a pipe
# of them is what made the old check lose its match).
big_rc() { # big_rc FILE FIRST_LINE LAST_LINE
  {
    [ -n "$2" ] && printf '%s\n' "$2"
    local i
    for i in $(seq 1 4000); do printf 'alias a%s=true  # an alias in a long startup file, nothing about the thing we look for\n' "$i"; done
    [ -n "$3" ] && printf '%s\n' "$3"
  } >"$1"
}

t_big_rc_file_with_the_line_is_not_added_again() {
  local where
  for where in top bottom; do
    fresh
    ready
    if [ "$where" = top ]; then
      big_rc "$HOME_/.bashrc" 'export PATH="$HOME/.local/bin:$PATH"' ''
    else
      big_rc "$HOME_/.bashrc" '' 'export PATH="$HOME/.local/bin:$PATH"'
    fi
    [ "$(wc -c <"$HOME_/.bashrc")" -ge 100000 ] || bad "the rc file is under 100 KB"
    local before
    before=$(wc -c <"$HOME_/.bashrc")
    play
    rc_is 0
    out_has 'already puts ~/.local/bin on it'
    out_lacks 'added ~/.local/bin to PATH'
    play
    rc_is 0
    out_has 'already puts ~/.local/bin on it'
    [ "$(wc -c <"$HOME_/.bashrc")" = "$before" ] || bad "the rc file ($where) grew on a rerun"
    sub_done "t_big_rc_file_with_the_line_is_not_added_again[$where]"
  done
  # interactive too: no question is asked (an unanswered one would fail the case)
  fresh
  ready
  big_rc "$HOME_/.bashrc" 'export PATH="$HOME/.local/bin:$PATH"' ''
  play --answer 'zzz-never=n' -- --no-setup
  rc_is 0
  out_lacks 'Put ~/.local/bin'
}

t_big_rc_file_with_only_comments_still_gets_the_line() {
  fresh
  ready
  big_rc "$HOME_/.bashrc" '' '# export PATH="$HOME/.local/bin:$PATH"'
  play
  rc_is 0
  out_has 'added ~/.local/bin to PATH'
  # the commented one that was there, and the one added
  file_count "$HOME_/.bashrc" 'export PATH="$HOME/.local/bin:$PATH"' 2
}

# --- run ---------------------------------------------------------------------------------

main() {
  if [ "${1:-}" = root ]; then
    if [ "$(id -u)" != 0 ]; then echo "root mode must run as root" >&2; exit 1; fi
    run_case t_refuses_root
  else
    local fn
    for fn in $(declare -F | awk '{print $3}' | grep '^t_' | grep -v '^t_refuses_root$'); do
      run_case "$fn"
    done
  fi
  printf '\nfront-door stubbed: %s passed, %s failed\n' "$pass" "$fail"
  [ "$fail" = 0 ]
}

main "$@"
