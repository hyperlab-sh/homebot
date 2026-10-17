#!/usr/bin/env bash
# Runs inside the test container; scripts/docker-test.sh passes it to
# `bash -c`. stdin is a tar of the worktree; arg 1 is the subcommand.
# For `update` and `generate` the files the run created or changed leave as a
# tar on stdout (everything else, logs included, goes to stderr).
set -uo pipefail

mode=${1:?subcommand}
shift

exec 3>&1 1>&2
cd /src || exit 97
tar -x -C /src || { echo "could not unpack the source tar" >&2; exit 97; }

# manifest prints "sha256  path" for every file outside node_modules and .git.
manifest() {
  find . \( -name node_modules -o -name .git \) -prune -o -type f -print0 \
    | sort -z | xargs -0 -r sha256sum | LC_ALL=C sort
}

before=$(mktemp)
after=$(mktemp)
changed=$(mktemp)
removed=$(mktemp)

snapshot() { manifest > "$before"; }

# diff_tree fills $changed (created or modified) and $removed with paths.
diff_tree() {
  manifest > "$after"
  LC_ALL=C comm -13 "$before" "$after" | cut -c67- | LC_ALL=C sort > "$changed"
  LC_ALL=C comm -23 <(cut -c67- "$before" | LC_ALL=C sort) <(cut -c67- "$after" | LC_ALL=C sort) > "$removed"
}

# emit_changed writes the changed files to the real stdout as a tar.
emit_changed() {
  if [[ -s $removed ]]; then
    echo "note: the run removed these files; they are not removed in the worktree:" >&2
    sed 's/^/  /' "$removed" >&2
  fi
  [[ -s $changed ]] || return 0
  tr '\n' '\0' < "$changed" | tar -c --null -T - >&3
}

go_test() {
  if (( $# == 0 )); then set -- ./...; fi
  HOME=/nonexistent go test "$@"
}

step_charm() {
  if grep -rnE --include='*.go' '"github\.com/charmbracelet/(lipgloss|bubbletea|bubbles|huh|log|fang|glamour)[/"]' .; then
    echo "Import Charm v2 from charm.land/..., not github.com/charmbracelet/..."
    return 1
  fi
}

step_generate_check() {
  snapshot
  go generate ./... || return 1
  diff_tree
  if [[ -s $changed || -s $removed ]]; then
    echo "go generate left a diff; run scripts/docker-test.sh --context NAME generate and commit:"
    cat "$changed" "$removed" | sed 's/^/  /'
    return 1
  fi
}

# step_test runs the whole suite and notes the package counts in $detail.
step_test() {
  local out rc
  out=$(mktemp)
  HOME=/nonexistent go test ./... 2>&1 | tee "$out"
  rc=${PIPESTATUS[0]}
  detail="packages ok=$(grep -cE '^ok[[:space:]]' "$out") FAIL=$(grep -cE '^FAIL[[:space:]]+[^[:space:]]' "$out") no-test-files=$(grep -cE '^\?[[:space:]]' "$out")"
  return "$rc"
}

# step_shellcheck mirrors the ci.yml shellcheck job: default severity, the
# POSIX installer with -s sh, every other script by its own shebang.
step_shellcheck() {
  local files=(internal/actions/scripts/*.sh internal/guard/pre-commit.sh docs/landing/install.sh scripts/release-test.sh)
  local dirs=(scripts/release-test scripts/release)
  local file
  shellcheck -s sh install.sh || return 1
  while IFS= read -r -d '' file; do files+=("$file"); done \
    < <(find "${dirs[@]}" -type f -name '*.sh' -print0)
  shellcheck "${files[@]}"
}

results=()
run_step() {
  local name=$1 rc
  shift
  detail=""
  printf '\n=== %s\n' "$name"
  "$@"
  rc=$?
  if (( rc == 0 )); then
    results+=("PASS  $name${detail:+  ($detail)}")
  else
    results+=("FAIL  $name  (exit $rc${detail:+; $detail})")
  fi
  return 0
}

summary() {
  local failed=0 r
  printf '\n=== summary\n'
  for r in "${results[@]}"; do
    printf '%s\n' "$r"
    [[ $r == FAIL* ]] && failed=1
  done
  return "$failed"
}

case $mode in
  all)
    run_step charm-imports step_charm
    run_step generate step_generate_check
    run_step build go build ./...
    run_step vet go vet ./...
    run_step go-test step_test
    run_step lint golangci-lint run
    run_step shellcheck step_shellcheck
    summary
    exit $?
    ;;
  go-test)
    go_test "$@"
    exit $?
    ;;
  lint)
    golangci-lint run
    exit $?
    ;;
  generate)
    snapshot
    go generate ./...
    rc=$?
    diff_tree
    emit_changed
    exit "$rc"
    ;;
  update)
    snapshot
    HOME=/nonexistent go test "$@" -update
    rc=$?
    diff_tree
    emit_changed
    exit "$rc"
    ;;
  *)
    echo "unknown subcommand: $mode" >&2
    exit 64
    ;;
esac
