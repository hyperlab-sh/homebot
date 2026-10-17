#!/usr/bin/env bash
# Generate every non-binary release payload before goreleaser runs.
set -euo pipefail

if [[ $# -ne 3 || -z $1 || -z $2 || -z $3 ]]; then
  echo 'usage: extras.sh VERSION RFC3339_DATE OUT' >&2
  exit 64
fi
version=$1 date=$2 out=$3
mkdir -p "$out/completions" "$out/man"
out=$(cd "$out" && pwd)

go run ./tools/gendocs completions -out "$out/completions"
go run ./tools/gendocs man -out "$out/man" -version "$version" -date "$date"
go run github.com/google/go-licenses/v2@v2.0.1 report ./cmd/homebot \
  --template scripts/release/notices.tpl >"$out/THIRD_PARTY_NOTICES"

for f in THIRD_PARTY_NOTICES completions/homebot.bash completions/_homebot \
  completions/homebot.fish man/homebot.1 man/homebot.1.gz; do
  [[ -s $out/$f ]] || { echo "extras: missing or empty $f" >&2; exit 1; }
done
grep -q 'github.com/spf13/cobra' "$out/THIRD_PARTY_NOTICES" \
  || { echo 'extras: cobra is absent from THIRD_PARTY_NOTICES' >&2; exit 1; }
if grep -q 'MISSING_LICENSE_TEXT:' "$out/THIRD_PARTY_NOTICES"; then
  echo 'extras: a dependency has no full license text' >&2
  exit 1
fi
