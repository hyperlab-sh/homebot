#!/usr/bin/env bash
# completions: generate release shell scripts and man page, then use them in
# the release container. No host shell configuration or home is changed.
set -euo pipefail
# shellcheck source-path=SCRIPTDIR source=../lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/../lib.sh"
cd "$RT_SRC"

out=$RT_VOL/completions-check
rm -rf "$out"
mkdir -p "$out/bin" "$out/completions" "$out/man"
go build -o "$out/bin/homebot" ./cmd/homebot
go run ./tools/gendocs completions -out "$out/completions"
go run ./tools/gendocs man -out "$out/man" -version 0.1.0 -date 2026-10-13T00:00:00Z

for file in "$out/completions/homebot.bash" "$out/completions/_homebot" \
  "$out/completions/homebot.fish" "$out/man/homebot.1" "$out/man/homebot.1.gz"; do
  [[ -s $file ]] || rt_die "completions: missing $file"
done

echo '--- mandoc'
mandoc -Tlint -Wwarning "$out/man/homebot.1"

echo '--- bash'
export PATH="$out/bin:$PATH"
# Cobra's Bash V2 output uses functions supplied by the bash-completion package.
# shellcheck disable=SC1091 # installed in the release-test image
source /usr/share/bash-completion/bash_completion
# shellcheck disable=SC1090,SC1091 # generated during this mode
source "$out/completions/homebot.bash"
# This synthetic call is outside Bash's programmable-completion dispatcher;
# compopt has no active completion there. The generator detects this function
# and skips only completion-option changes while still asking homebot for words.
compopt() { :; }
COMP_WORDS=(homebot sa)
COMP_CWORD=1
COMP_LINE='homebot sa'
COMP_POINT=${#COMP_LINE}
COMP_TYPE=9
COMP_KEY=9
__start_homebot
[[ " ${COMPREPLY[*]} " == *' save '* ]] || rt_die "completions: bash did not offer save: ${COMPREPLY[*]}"

echo '--- fish'
fish_out=$(fish -c "set -gx PATH '$out/bin' \$PATH; source '$out/completions/homebot.fish'; complete -C 'homebot sa'")
grep -q '^save' <<<"$fish_out" || rt_die "completions: fish did not offer save: $fish_out"

echo '--- zsh structure'
grep -q '^_homebot()$' "$out/completions/_homebot" || rt_die 'completions: zsh function is missing'
grep -q 'requestComp=.* __complete' "$out/completions/_homebot" || rt_die 'completions: zsh does not call __complete'

echo 'completions: passed'
