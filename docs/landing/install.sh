#!/usr/bin/env bash
# homebot front door: https://homebot.sh/install
#
#   curl -fsSL https://homebot.sh/install | bash
#   curl -fsSL https://homebot.sh/install | bash -s -- --yes --no-setup
#
# Read it before you run it. It asks before it installs Homebrew, runs anything
# with sudo, or edits your shell's startup files (--yes answers yes for you),
# it ends by listing what it installed or edited, and it does these things, in
# this order:
#
#   1. downloads the latest release's install.sh and checksums.txt from GitHub
#      and checks install.sh's sha256 against checksums.txt; nothing on your
#      machine has changed yet, and with no release it stops here
#   2. finds Homebrew, or (with your OK) installs it with Homebrew's own installer
#   3. does what Homebrew's "Next steps" say: puts brew on your PATH in your
#      shell's startup file and, on Linux, installs the build tools
#   4. brew install git age gitleaks, whichever you lack
#   5. runs that install.sh as you: it installs the release binary into
#      ~/.local/bin, checking the archive's checksum and, if cosign is
#      installed, the signature
#   6. puts ~/.local/bin on your PATH if it is not there
#   7. runs `homebot setup`
#
# Everything lives in functions inside one { } block whose last line starts
# them, so a download that stops half way is a syntax error and runs nothing.
# It needs bash 3.2 or newer (macOS ships 3.2). Not for root: Homebrew refuses
# root too, and so does the release installer.
set -euo pipefail

{

usage() {
  cat <<-'EOF'
	homebot front door: installs homebot and what it needs.

	  curl -fsSL https://homebot.sh/install | bash
	  curl -fsSL https://homebot.sh/install | bash -s -- [options]

	Options:
	  --yes         answer yes to every question (Homebrew installs unattended, so
	                it needs passwordless sudo where it uses sudo)
	  --no-setup    stop before 'homebot setup' and print the command to run
	  --persona ID  the robot to set up with, passed on to 'homebot setup'
	  --name NAME   the name to give your homebot, passed on to 'homebot setup'
	                (ID and NAME: lowercase letters, digits and '-', starting with
	                a letter, up to 32 characters; NAME at least 2. --persona=ID
	                and --name=NAME work too)
	  --version VERSION
	                install release VERSION (vX.Y.Z) instead of the latest
	  --help        show this

	Without a terminal and without --yes it changes nothing: it prints what it
	would do, and the manual steps, and exits with an error.
	EOF
}

config() {
  : "${HOME:?HOME is not set}"
  REPO_WEB="https://github.com/hyperlab-sh/homebot"
  BREW_INSTALLER_URL="https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh"
  MIN_GIT_MAJOR=2
  MIN_GIT_MINOR=40
  BIN_DIR="$HOME/.local/bin"
  BIN="$BIN_DIR/homebot"

  YES=0
  NO_SETUP=0
  VERSION=""
  TMP=""
  PERSONA=""
  NAME=""

  OS=""
  BREW=""
  BREW_PREFIX=""
  BREW_ON_PATH=0
  PKG=""
  RC_FILE=""
  RC_SUFFIX=""
  CHANGES=""
  SUDO_FLAGS=""
  BOLD=""
  RED=""
  YELLOW=""
  RESET=""
}

# --- output ------------------------------------------------------------------

colors() {
  if [ -t 1 ] && [ -t 2 ]; then
    BOLD=$'\033[1m'
    RED=$'\033[31m'
    YELLOW=$'\033[33m'
    RESET=$'\033[0m'
  fi
}

say() { printf '%s\n' "$*"; }
step() { printf '%s%s%s: %s\n' "$BOLD" "$1" "$RESET" "$2"; }
warn() { printf '%s%s%s\n' "$YELLOW" "$*" "$RESET" >&2; }
die() {
  printf '%sError:%s %s\n' "$RED" "$RESET" "$*" >&2
  exit 1
}

# tilde PATH: prints PATH with $HOME shortened to ~, for messages.
tilde() {
  case $1 in
    "$HOME"/*) printf '~%s' "${1#"$HOME"}" ;;
    *) printf '%s' "$1" ;;
  esac
}

# has_tty: true when /dev/tty can be opened (the script's stdin is the script
# itself when it is piped to bash, so questions are read from the terminal).
has_tty() { (exec </dev/tty) 2>/dev/null; }

# ask QUESTION: yes unless the answer starts with n. --yes answers for you.
ask() {
  local answer
  if [ "$YES" = 1 ]; then
    return 0
  fi
  while :; do
    printf '%s%s%s [Y/n] ' "$BOLD" "$1" "$RESET" >&2
    if ! read -r answer </dev/tty; then
      printf '\n' >&2
      return 1
    fi
    case $answer in
      "" | y | Y | yes | YES | Yes) return 0 ;;
      n | N | no | NO | No) return 1 ;;
    esac
  done
}

# ask_no QUESTION: no unless the answer starts with y. Callers decide what --yes
# means (for the questions that use this one it means stop).
ask_no() {
  local answer
  while :; do
    printf '%s%s%s [y/N] ' "$BOLD" "$1" "$RESET" >&2
    if ! read -r answer </dev/tty; then
      printf '\n' >&2
      return 1
    fi
    case $answer in
      "" | n | N | no | NO | No) return 1 ;;
      y | Y | yes | YES | Yes) return 0 ;;
    esac
  done
}

# note_change TEXT: remembers one thing this run changed, for the list at the end.
note_change() {
  CHANGES="$CHANGES$1"$'\n'
}

print_plan() {
  setup_command
  cat <<-EOF
	It would:
	  1. download the latest release's install.sh and checksums.txt, and check the one against the other
	  2. install Homebrew if it is missing and put it on your PATH (asking first)
	  3. brew install git age gitleaks, whichever you lack
	  4. run the release's install.sh, which puts homebot in ~/.local/bin
	  5. put ~/.local/bin on your PATH if it is not there (asking first)
	  6. run 'homebot ${SETUP_CMD[*]}'
	EOF
}

print_manual() {
  setup_command
  cat <<-EOF
	By hand:
	  1. Install Homebrew from https://brew.sh, then run the "Next steps" it prints
	     (the PATH lines and, on Linux, the build tools).
	  2. brew install git age gitleaks
	  3. Install the release: $REPO_WEB/releases/latest
	     (download install.sh and checksums.txt, check install.sh's sha256 in
	     checksums.txt, then run: sh install.sh)
	  4. Put ~/.local/bin on your PATH, then run: homebot ${SETUP_CMD[*]}
	EOF
}

# --- arguments and guards ------------------------------------------------------

# valid_id VALUE MINLEN: 0 when VALUE is [a-z][a-z0-9-]* with MINLEN to 32
# characters. Spelled out as a case pattern (no ranges, which follow the locale
# and so can let capitals in), and a newline fails like any other odd character.
valid_id() {
  case $1 in
    '' | [!abcdefghijklmnopqrstuvwxyz]* | *[!abcdefghijklmnopqrstuvwxyz0123456789-]*) return 1 ;;
  esac
  [ "${#1}" -ge "$2" ] && [ "${#1}" -le 32 ]
}

# bad_value FLAG VALUE MINLEN: says why VALUE is refused and exits 2. The value
# is shown with unprintable characters replaced and cut short, so what a person
# typed (or a pipe fed in) cannot move the cursor or rewrite the screen.
bad_value() {
  local shown=${2//[![:print:]]/?}
  if [ "${#shown}" -gt 40 ]; then shown="${shown:0:40}..."; fi
  printf 'Error: %s needs lowercase letters, digits and -, starting with a letter, %s to 32 characters (got: %s)\n' "$1" "$3" "$shown" >&2
  exit 2
}

# take_id FLAG VALUE MINLEN: checks VALUE and returns it in ID.
take_id() {
  if [ -z "$2" ]; then
    printf 'Error: %s needs a value\n' "$1" >&2
    exit 2
  fi
  if ! valid_id "$2" "$3"; then
    bad_value "$1" "$2" "$3"
  fi
  ID=$2
}

# setup_command: the command that finishes the job, as it is run and printed.
# Only the flags the person gave; both values passed the check above, and each
# is one word of the form --flag=value, so it cannot become a flag of its own.
setup_command() {
  SETUP_CMD=(setup)
  if [ -n "$PERSONA" ]; then SETUP_CMD+=("--persona=$PERSONA"); fi
  if [ -n "$NAME" ]; then SETUP_CMD+=("--name=$NAME"); fi
}

parse_args() {
  while [ $# -gt 0 ]; do
    case $1 in
      --yes) YES=1 ;;
      --no-setup) NO_SETUP=1 ;;
      --persona | --name)
        # The next word is the value, whatever it looks like: --name -x is a
        # bad name, not a flag.
        if [ $# -lt 2 ]; then
          printf 'Error: %s needs a value\n' "$1" >&2
          exit 2
        fi
        if [ "$1" = --persona ]; then
          take_id "$1" "$2" 1
          PERSONA=$ID
        else
          take_id "$1" "$2" 2
          NAME=$ID
        fi
        shift
        ;;
      --persona=*)
        take_id --persona "${1#--persona=}" 1
        PERSONA=$ID
        ;;
      --name=*)
        take_id --name "${1#--name=}" 2
        NAME=$ID
        ;;
      --version | --version=*)
        if [ "$1" = --version ]; then
          if [ $# -lt 2 ]; then
            printf 'Error: --version needs a value\n' >&2
            exit 2
          fi
          VERSION=$2
          shift
        else
          VERSION=${1#--version=}
        fi
        # a v is optional on the command line, the release tags have one
        case $VERSION in [0-9]*) VERSION=v$VERSION ;; esac
        if ! valid_version "$VERSION"; then
          printf 'Error: --version needs a release like v0.1.0 (got: %s)\n' "${VERSION//[![:print:]]/?}" >&2
          exit 2
        fi
        ;;
      -h | --help)
        usage
        exit 0
        ;;
      *)
        printf 'Error: unknown option: %s (see --help)\n' "$1" >&2
        exit 2
        ;;
    esac
    shift
  done
}

refuse_root() {
  if [ "$(id -u)" = 0 ] || [ -n "${SUDO_USER:-}" ]; then
    die "run this as yourself, not as root and not with sudo. It asks for sudo itself where it needs it, and Homebrew refuses to install as root."
  fi
}

detect_platform() {
  local sys machine
  sys=$(uname -s)
  machine=$(uname -m)
  case $sys in
    Darwin) OS=macOS ;;
    Linux) OS=Linux ;;
    *) die "homebot runs on macOS and Linux; this is $sys." ;;
  esac
  case $machine in
    arm64 | aarch64 | x86_64 | amd64) ;;
    *) die "homebot runs on 64-bit Intel and ARM; this machine is $machine." ;;
  esac
}

no_terminal() {
  local retry='curl -fsSL https://homebot.sh/install | bash -s -- --yes'
  if [ -n "$VERSION" ]; then retry="$retry --version $VERSION"; fi
  if [ -n "$PERSONA" ]; then retry="$retry --persona=$PERSONA"; fi
  if [ -n "$NAME" ]; then retry="$retry --name=$NAME"; fi
  say "There is no terminal to ask questions on, and no --yes, so nothing was changed."
  say ""
  print_plan
  say ""
  print_manual
  say ""
  say "Run it from a terminal, or answer yes to everything:"
  say "  $retry"
  exit 1
}

# --- Homebrew ------------------------------------------------------------------

# find_brew: sets BREW (and BREW_ON_PATH); false when there is no Homebrew.
find_brew() {
  local candidate
  BREW=""
  BREW_ON_PATH=0
  candidate=$(command -v brew 2>/dev/null || true)
  if [ -n "$candidate" ] && [ -x "$candidate" ]; then
    BREW=$candidate
    BREW_ON_PATH=1
    return 0
  fi
  for candidate in /opt/homebrew/bin/brew /usr/local/bin/brew \
    /home/linuxbrew/.linuxbrew/bin/brew "$HOME/.linuxbrew/bin/brew"; do
    if [ -x "$candidate" ]; then
      BREW=$candidate
      return 0
    fi
  done
  return 1
}

# choose_rc: sets RC_FILE and RC_SUFFIX the way Homebrew's own installer does
# (Homebrew/install install.sh, "Next steps": the shell comes from $SHELL).
choose_rc() {
  case ${SHELL:-} in
    */bash*)
      RC_SUFFIX=" bash"
      if [ "$OS" = Linux ]; then RC_FILE="$HOME/.bashrc"; else RC_FILE="$HOME/.bash_profile"; fi
      ;;
    */zsh*)
      RC_SUFFIX=" zsh"
      if [ "$OS" = Linux ]; then RC_FILE="${ZDOTDIR:-$HOME}/.zshrc"; else RC_FILE="${ZDOTDIR:-$HOME}/.zprofile"; fi
      ;;
    */fish*)
      RC_SUFFIX=" fish"
      RC_FILE="$HOME/.config/fish/config.fish"
      ;;
    *)
      RC_SUFFIX=""
      RC_FILE="${ENV:-$HOME/.profile}"
      ;;
  esac
}

# resolve_link FILE: prints the file a chain of symlinks ends at.
resolve_link() {
  local f=$1 target n=0
  while [ -L "$f" ] && [ "$n" -lt 20 ]; do
    target=$(readlink "$f")
    case $target in
      /*) f=$target ;;
      *) f=$(dirname "$f")/$target ;;
    esac
    n=$((n + 1))
  done
  printf '%s' "$f"
}

# add_to_rc FILE LINE WHAT: appends a blank line and LINE, never rewrites the
# file. A symlinked file is appended through (a home repo may manage it).
add_to_rc() {
  local file=$1 line=$2 what=$3 target new_profile=0
  if [ ! -L "$file" ]; then
    mkdir -p "$(dirname "$file")" || die "could not create $(dirname "$file")"
  fi
  # bash on macOS reads ~/.bash_profile and then ignores ~/.profile; Homebrew's
  # own installer makes the same file, so say so.
  if [ "$file" = "$HOME/.bash_profile" ] && [ ! -e "$file" ] && [ -e "$HOME/.profile" ]; then
    new_profile=1
  fi
  printf '\n%s\n' "$line" >>"$file" || die "could not write to $(tilde "$file")"
  if [ "$new_profile" = 1 ]; then
    warn "Note: $(tilde "$file") is new, so bash no longer reads ~/.profile. Add this line to it if you need that: . ~/.profile"
  fi
  target=$(resolve_link "$file")
  note_change "$(tilde "$target") (added $what to PATH)"
  if [ "$target" != "$file" ]; then
    step "PATH" "added $what to $(tilde "$target") (the link $(tilde "$file") points there)"
  else
    step "PATH" "added $what to $(tilde "$file")"
  fi
}

# step_brew_path ASKED: Homebrew's first "Next step", done for you. Skipped when
# brew is already on PATH (someone set that up) or the file already has brew
# shellenv. ASKED is 1 when the question that came before (installing Homebrew)
# already named the file; otherwise this asks first.
step_brew_path() {
  local asked=${1:-0} line
  BREW_PREFIX=$("$BREW" --prefix 2>/dev/null) || BREW_PREFIX=$(dirname "$(dirname "$BREW")")
  if [ "$BREW_ON_PATH" = 1 ]; then
    step "PATH" "brew is already on it"
  else
    choose_rc
    if grep -qs 'brew shellenv' "$RC_FILE"; then
      step "PATH" "$(tilde "$RC_FILE") already sets up Homebrew"
    else
      line="eval \"\$($BREW_PREFIX/bin/brew shellenv$RC_SUFFIX)\""
      if [ "$asked" = 1 ] || ask "Add Homebrew to your PATH in $(tilde "$RC_FILE")?"; then
        add_to_rc "$RC_FILE" "$line" "Homebrew"
      else
        say "Not added. For brew in new terminals, add this line to $(tilde "$RC_FILE") yourself:"
        say "  $line"
      fi
    fi
  fi
  # This process is bash whatever the user's shell is, so ask for bash syntax.
  eval "$("$BREW_PREFIX/bin/brew" shellenv bash)"
  hash -r
}

install_homebrew() {
  local installer rc=0
  if [ "$OS" = macOS ] && [ "$(uname -m)" = x86_64 ]; then
    warn "Homebrew's installer no longer supports Intel Macs, so it cannot be installed here."
    say ""
    print_manual
    exit 1
  fi
  choose_rc
  say "Homebrew is the package manager homebot uses: it installs git, age and"
  if [ "$OS" = macOS ]; then
    say "gitleaks now. It goes in /opt/homebrew; the installer may ask for your password."
  else
    say "gitleaks now. It goes in /home/linuxbrew; the installer may ask for your password."
  fi
  if ! grep -qs 'brew shellenv' "$RC_FILE"; then
    say "It also adds Homebrew to your PATH in $(tilde "$RC_FILE")."
  fi
  if ! ask "Install Homebrew?"; then
    say ""
    say "Homebrew not installed, so nothing was changed."
    print_manual
    exit 1
  fi
  # Download first, so a failed download changes nothing; then the system
  # packages its installer needs; then run it.
  command -v curl >/dev/null 2>&1 || die "curl is needed to download Homebrew's installer."
  installer=$(curl -fsSL "$BREW_INSTALLER_URL") || die "could not download Homebrew's installer. See https://brew.sh to install it by hand."
  ensure_system_packages 1
  step "Homebrew" "installing"
  if [ "$YES" = 1 ]; then
    # NONINTERACTIVE=1 makes Homebrew use sudo -n; on a terminal, sudo -v lets
    # you type the password once so that works without NOPASSWD.
    if has_tty && command -v sudo >/dev/null 2>&1; then
      sudo -v || warn "sudo did not accept a password; Homebrew may stop for lack of it."
    fi
    NONINTERACTIVE=1 /bin/bash -c "$installer" </dev/null || rc=$?
  else
    /bin/bash -c "$installer" </dev/tty || rc=$?
  fi
  if [ "$rc" -ne 0 ]; then
    printf '%sError:%s the Homebrew installer failed (exit %s). See https://brew.sh to install it by hand, then run this again.\n' "$RED" "$RESET" "$rc" >&2
    exit "$rc"
  fi
  say "Ignore the \"Next steps\" Homebrew just printed: this script does them now."
  find_brew || die "Homebrew installed, but brew is not in any of the usual places. Open a new terminal and run this again."
  note_change "Homebrew installed in $(dirname "$(dirname "$BREW")")"
}

# Linux only. Homebrew's second "Next step" is the build tools; its installer
# also refuses to start without git (WANT_GIT=1 is that case).
detect_pkg() {
  local p
  PKG=""
  for p in apt-get dnf yum pacman apk; do
    if command -v "$p" >/dev/null 2>&1; then
      PKG=$p
      return 0
    fi
  done
}

build_tools_missing() {
  case $PKG in
    apt-get) ! dpkg -s build-essential >/dev/null 2>&1 ;;
    dnf | yum) ! rpm -q gcc make >/dev/null 2>&1 ;;
    pacman) ! pacman -Q base-devel >/dev/null 2>&1 ;;
    apk) ! apk info -e build-base >/dev/null 2>&1 ;;
    *) return 1 ;;
  esac
}

# dnf_group: Homebrew's choice of group id (Fedora and RHEL-likes, not Amazon Linux).
dnf_group() {
  local id like
  # shellcheck source=/dev/null
  id=$( (. /etc/os-release 2>/dev/null && printf '%s' "${ID:-}") || true)
  # shellcheck source=/dev/null
  like=$( (. /etc/os-release 2>/dev/null && printf '%s' "${ID_LIKE:-}") || true)
  if [ "$id" != amzn ]; then
    case "$id $like" in
      *fedora* | *rhel*)
        printf 'development-tools'
        return 0
        ;;
    esac
  fi
  printf "'Development Tools'"
}

# sys_pkg_commands TOOLS GIT: one command per line (without sudo).
sys_pkg_commands() {
  local tools=$1 git=$2 pkgs=""
  case $PKG in
    apt-get)
      if [ "$tools" = 1 ]; then pkgs="$pkgs build-essential"; fi
      if [ "$git" = 1 ]; then pkgs="$pkgs git"; fi
      printf 'apt-get update\n'
      printf 'apt-get install -y%s\n' "$pkgs"
      ;;
    dnf)
      if [ "$tools" = 1 ]; then printf 'dnf group install -y %s\n' "$(dnf_group)"; fi
      if [ "$git" = 1 ]; then printf 'dnf install -y git\n'; fi
      ;;
    yum)
      if [ "$tools" = 1 ]; then printf "yum groupinstall -y 'Development Tools'\n"; fi
      if [ "$git" = 1 ]; then printf 'yum install -y git\n'; fi
      ;;
    pacman)
      if [ "$tools" = 1 ]; then pkgs="$pkgs base-devel"; fi
      if [ "$git" = 1 ]; then pkgs="$pkgs git"; fi
      printf 'pacman -S --needed --noconfirm%s\n' "$pkgs"
      ;;
    apk)
      if [ "$tools" = 1 ]; then pkgs="$pkgs build-base"; fi
      if [ "$git" = 1 ]; then pkgs="$pkgs git"; fi
      printf 'apk add%s\n' "$pkgs"
      ;;
  esac
}

# ensure_system_packages WANT_GIT: asks, then runs Homebrew's suggested command
# with sudo. A "no", or no sudo, prints the command and carries on, except when
# git is missing and Homebrew is about to be installed: its installer needs it.
ensure_system_packages() {
  local want_git=$1 need_tools=0 need_git=0 cmds line failed=0
  if [ "$OS" != Linux ]; then
    return 0
  fi
  detect_pkg
  if [ "$want_git" = 1 ] && ! command -v git >/dev/null 2>&1; then
    need_git=1
  fi
  if [ -n "$PKG" ] && build_tools_missing; then
    need_tools=1
  fi
  if [ "$need_tools" = 0 ] && [ "$need_git" = 0 ]; then
    if [ -n "$PKG" ]; then step "Build tools" "installed"; fi
    return 0
  fi
  if [ -z "$PKG" ]; then
    if [ "$need_git" = 1 ]; then
      die "Homebrew needs git before it can install. Install git with your package manager, then run this again."
    fi
    warn "Could not tell which package manager this is. Homebrew lists the build tools it wants at https://docs.brew.sh/Homebrew-on-Linux"
    return 0
  fi
  if [ "$need_git" = 1 ] && [ "$need_tools" = 1 ]; then
    say "Homebrew on Linux needs git, and a compiler for the few packages it builds from source."
  elif [ "$need_git" = 1 ]; then
    say "Homebrew on Linux needs git before it can install."
  else
    say "Homebrew on Linux needs a compiler for the few packages it builds from source."
  fi
  cmds=$(sys_pkg_commands "$need_tools" "$need_git")
  while IFS= read -r line; do
    say "  sudo $line"
  done <<<"$cmds"
  if has_tty; then SUDO_FLAGS=""; else SUDO_FLAGS=" -n"; fi
  if ! command -v sudo >/dev/null 2>&1; then
    warn "There is no sudo here, so run those as root yourself."
    failed=1
  elif ask "Run that with sudo?"; then
    while IFS= read -r line; do
      case $line in
        "apt-get update") eval "sudo$SUDO_FLAGS $line" </dev/null || warn "apt-get update failed; carrying on" ;;
        *) eval "sudo$SUDO_FLAGS $line" </dev/null || failed=1 ;;
      esac
    done <<<"$cmds"
    if [ "$failed" = 1 ]; then warn "That did not finish."; fi
  else
    say "Skipped. Run it yourself when you can."
    failed=1
  fi
  hash -r
  if [ "$need_git" = 1 ] && ! command -v git >/dev/null 2>&1; then
    die "Homebrew's installer needs git first. Install it, then run this again."
  fi
  if [ "$failed" = 1 ]; then
    warn "Going on without the build tools: Homebrew still works for packages that come ready-built."
  else
    step "Build tools" "installed"
    note_change "system packages installed with $PKG (sudo)"
  fi
}

step_homebrew() {
  if find_brew; then
    step "Homebrew" "installed ($BREW)"
    step_brew_path
    ensure_system_packages 0
  else
    install_homebrew
    step "Homebrew" "installed ($BREW)"
    step_brew_path 1
  fi
}

# --- tools ---------------------------------------------------------------------

# git_is_new_enough: git 2.40 or newer on PATH (Apple's git is 2.39).
git_is_new_enough() {
  local out major minor
  command -v git >/dev/null 2>&1 || return 1
  out=$(git --version 2>/dev/null | sed -n 's/^git version \([0-9][0-9]*\)\.\([0-9][0-9]*\).*/\1 \2/p')
  [ -n "$out" ] || return 1
  major=${out%% *}
  minor=${out##* }
  [ "$major" -gt "$MIN_GIT_MAJOR" ] || { [ "$major" -eq "$MIN_GIT_MAJOR" ] && [ "$minor" -ge "$MIN_GIT_MINOR" ]; }
}

step_tools() {
  local tool missing=""
  for tool in git age gitleaks; do
    if [ "$tool" = git ]; then
      if git_is_new_enough; then
        step "git" "ok"
      elif command -v git >/dev/null 2>&1; then
        step "git" "too old (homebot needs $MIN_GIT_MAJOR.$MIN_GIT_MINOR or newer), installing Homebrew's"
        missing="$missing git"
      else
        step "git" "installing"
        missing="$missing git"
      fi
    elif command -v "$tool" >/dev/null 2>&1; then
      step "$tool" "ok"
    else
      step "$tool" "installing"
      missing="$missing $tool"
    fi
  done
  if [ -n "$missing" ]; then
    # shellcheck disable=SC2086 # one tool name per word
    "$BREW" install $missing </dev/null || die "brew install$missing failed. Fix that, then run this again."
    hash -r
    for tool in $missing; do
      command -v "$tool" >/dev/null 2>&1 || die "$tool is still not on PATH after brew install."
    done
    if ! git_is_new_enough; then
      die "git on PATH is older than $MIN_GIT_MAJOR.$MIN_GIT_MINOR even after brew install git."
    fi
    note_change "installed with Homebrew:$missing"
  fi
}

# --- homebot -------------------------------------------------------------------
# The release's own installer does the install; this script only fetches it,
# checks it and runs it, as you (it refuses sudo, and so does this script).

# fetch URL DEST: downloads URL to DEST and prints the HTTP status ("000" when
# there was no answer at all; curl's own error message then shows on stderr).
fetch() {
  local code
  code=$(curl -sSL --proto '=https' --tlsv1.2 --connect-timeout 10 --max-time 120 --retry 2 -o "$2" -w '%{http_code}' "$1") || code=000
  printf '%s' "$code"
}

# file_hash FILE: the file's sha256 in hex.
file_hash() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}'
  else
    openssl dgst -sha256 "$1" | awk '{print $NF}'
  fi
}

# valid_version VALUE: vX.Y.Z or vX.Y.Z-rc.N, the shapes the release installer takes.
valid_version() {
  case $1 in *$'\n'*) return 1 ;; esac
  printf '%s\n' "$1" | LC_ALL=C grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+(-rc\.[0-9]+)?$'
}

# release_tag FILE: the release tag the archive lines of a checksums.txt name
# (homebot_X.Y.Z_os_arch.tar.gz, one version for all of them), or nothing.
release_tag() {
  local versions
  versions=$(awk 'NF == 2 && $2 ~ /^homebot_[0-9]/ && $2 ~ /\.tar\.gz$/ {
    v = substr($2, 9); sub(/_.*/, "", v); print v }' "$1" | sort -u)
  case $versions in
    '' | *$'\n'*) return 1 ;;
  esac
  valid_version "v$versions" || return 1
  printf 'v%s' "$versions"
}

# fetch_release: downloads the release's install.sh and checksums.txt into TMP
# and checks the first against the second, before anything on this machine
# changes. The latest release is looked up once, and everything after that
# (these two files, and the installer's own downloads) is for that tag, so a
# release published meanwhile changes nothing. No release (GitHub answers 404)
# stops the run: there is nothing else to install from.
fetch_release() {
  local base code want have
  command -v curl >/dev/null 2>&1 || die "curl is needed to download the release."
  TMP=$(mktemp -d) || die "could not make a temporary folder."
  trap 'rm -rf "$TMP"' EXIT
  if [ -z "$VERSION" ]; then
    step "Release" "looking up the latest"
    base="$REPO_WEB/releases/latest/download"
    code=$(fetch "$base/checksums.txt" "$TMP/latest-checksums.txt")
    case $code in
      200) ;;
      404) die "homebot has no release yet, so there is nothing to install. Nothing was changed." ;;
      *) die "could not download $base/checksums.txt (HTTP $code). Nothing was changed." ;;
    esac
    VERSION=$(release_tag "$TMP/latest-checksums.txt") \
      || die "the latest checksums.txt has no single release version in its archive names. Nothing was changed."
  fi
  base="$REPO_WEB/releases/download/$VERSION"
  step "Release" "downloading $VERSION install.sh and checksums.txt"
  code=$(fetch "$base/checksums.txt" "$TMP/checksums.txt")
  case $code in
    200) ;;
    404) die "homebot has no release $VERSION. See $REPO_WEB/releases. Nothing was changed." ;;
    *) die "could not download $base/checksums.txt (HTTP $code). Nothing was changed." ;;
  esac
  code=$(fetch "$base/install.sh" "$TMP/install.sh")
  [ "$code" = 200 ] || die "could not download $base/install.sh (HTTP $code). Nothing was changed."
  want=$(awk '$2 == "install.sh" && NF == 2 {print $1}' "$TMP/checksums.txt")
  case $want in
    '' | *$'\n'*) die "checksums.txt has no single line for install.sh. Nothing was changed." ;;
  esac
  have=$(file_hash "$TMP/install.sh") || die "could not hash install.sh."
  [ "$have" = "$want" ] || die "install.sh does not match its checksum in checksums.txt. Nothing was changed."
  step "Release" "install.sh matches checksums.txt"
}

install_homebot() {
  local out
  step "homebot" "running the release installer"
  # Its own stdout and stderr stay visible. It runs as you, from a file, with no
  # terminal input of its own.
  sh "$TMP/install.sh" --version "$VERSION" </dev/null || die "the release installer failed (see above)."
  rm -rf "$TMP"
  [ -x "$BIN" ] || die "the release installer finished, but there is no $BIN."
  out=$("$BIN" --version 2>&1) || die "$BIN is installed but does not run: $out"
  step "homebot" "installed ($(tilde "$BIN"), ${out%%$'\n'*})"
  note_change "installed $(tilde "$BIN") from the release"
}


# --- ~/.local/bin ----------------------------------------------------------------

# rc_puts_local_bin_on_path FILE: a PATH assignment (or fish_add_path) that names
# .local/bin, not a comment and not a file that merely lives under .local/bin.
rc_puts_local_bin_on_path() {
  # One grep, not a pipeline: with pipefail a grep -q that quits early made the
  # first grep die of SIGPIPE, and a big file then looked like it had no match.
  grep -Eqs '^[^#]*(PATH=|fish_add_path).*\.local/bin([^[:alnum:]_.-]|$)' "$1"
}

step_local_bin() {
  local line
  case ":$PATH:" in
    *":$BIN_DIR:"*)
      step "PATH" "$(tilde "$BIN_DIR") is on it"
      return 0
      ;;
  esac
  PATH="$BIN_DIR:$PATH"
  choose_rc
  if rc_puts_local_bin_on_path "$RC_FILE"; then
    step "PATH" "$(tilde "$RC_FILE") already puts ~/.local/bin on it"
    return 0
  fi
  if ! ask "Put ~/.local/bin on your PATH in $(tilde "$RC_FILE")?"; then
    warn "Not added. Add $(tilde "$BIN_DIR") to your PATH yourself to run homebot."
    return 0
  fi
  # The line goes into the file as written: $HOME expands when the shell starts.
  # shellcheck disable=SC2016
  case $RC_SUFFIX in
    " fish") line='fish_add_path $HOME/.local/bin' ;;
    *) line='export PATH="$HOME/.local/bin:$PATH"' ;;
  esac
  add_to_rc "$RC_FILE" "$line" "$(tilde "$BIN_DIR")"
}

# --- the end -----------------------------------------------------------------------

finish() {
  local line
  say ""
  say "Changed on this machine:"
  while IFS= read -r line; do
    if [ -n "$line" ]; then say "  $line"; fi
  done <<<"$CHANGES"
  say "Homebrew also keeps caches while it works (folders under ~/.cache or ~/Library/Caches)."
  say ""
  say "A new terminal (or: exec \$SHELL -l) picks up the PATH changes."
  setup_command
  if [ "$NO_SETUP" = 1 ]; then
    say "Next: $(tilde "$BIN") ${SETUP_CMD[*]}"
    return 0
  fi
  if ! has_tty; then
    say "There is no terminal for the setup. Next: $(tilde "$BIN") ${SETUP_CMD[*]}"
    return 0
  fi
  say "Starting homebot setup."
  say ""
  exec "$BIN" "${SETUP_CMD[@]}" </dev/tty
}

main() {
  config
  parse_args "$@"
  colors
  refuse_root
  detect_platform
  if [ "$YES" != 1 ] && ! has_tty; then
    no_terminal
  fi
  say "${BOLD}homebot${RESET}: installing on $OS"
  fetch_release
  step_homebrew
  step_tools
  install_homebot
  step_local_bin
  finish
}

main "$@"
}
