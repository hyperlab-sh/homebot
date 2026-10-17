#!/usr/bin/env bash
# release-test: not-in-all
# Verify a real v0.3 Sigstore bundle with pinned cosign releases.
set -euo pipefail
# shellcheck source-path=SCRIPTDIR source=../lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/../lib.sh"

rt_require_uid 1000
fixture=$RT_SRC/scripts/release-test/cosign-matrix/goreleaser-v2.18.3
checksums=$fixture/checksums.txt
bundle=$fixture/checksums.txt.sigstore.json
echo 'a40282e5419a5e7176c78afae243777ae0da7376e165d8cf59805910d2c84e1e  '"$checksums" | sha256sum -c -
echo '390efe4ed2d12857fc9c6aab35a493a361dad6adca9bded4bd27aff11004dd54  '"$bundle" | sha256sum -c -

identity='https://github.com/goreleaser/goreleaser/.github/workflows/release.yml@refs/tags/v2.18.3'
issuer='https://token.actions.githubusercontent.com'
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
printf 'version\targv\tresult\tfirst error\n'
minimum=''

# v3.0.0 has no published binary and v3.0.1 lacks artifact signatures. The
# first published signed v3 binary is v3.0.2, which replaces v3.0.0 here.
while read -r version digest; do
  [[ -n $version ]] || continue
  url="https://github.com/sigstore/cosign/releases/download/v$version/cosign-linux-amd64"
  rt_log "fetching pinned $url"
  curl -fsSL -o "$tmp/cosign" "$url"
  echo "$digest  $tmp/cosign" | sha256sum -c -
  chmod 0755 "$tmp/cosign"
  for variant in flagged plain; do
    args=(verify-blob --certificate-identity "$identity" --certificate-oidc-issuer "$issuer" --bundle "$bundle")
    if [[ $version == 2.* && $variant == flagged ]]; then args+=(--new-bundle-format); fi
    if [[ $version == 3.* && $variant == flagged ]]; then continue; fi
    args+=("$checksums")
    output=$("$tmp/cosign" "${args[@]}" 2>&1) && rc=0 || rc=$?
    if (( rc == 0 )); then
      printf '%s\t%s\tPASS\t-\n' "$version" "$variant"
      if [[ -z $minimum && ( $variant == flagged || $version == 3.* ) ]]; then
        minimum=$version
      fi
    else
      first=$(printf '%s\n' "$output" | sed -n '/[^[:space:]]/{p;q;}')
      printf '%s\t%s\tFAIL\t%s\n' "$version" "$variant" "$first"
    fi
    if [[ $version == 2.2.4 && $variant == flagged && $rc == 0 ]]; then
      rt_die "v2.2.4 control unexpectedly passed"
    fi
    if [[ $version == 3.1.3 && $rc != 0 ]]; then
      rt_die "v3.1.3 failed to verify the public bundle"
    fi
  done
done <<'VERSIONS'
2.2.4 97a6a1e15668a75fc4ff7a4dc4cb2f098f929cbea2f12faa9de31db6b42b17d7
2.4.0 cd7636b3586a3bdac2d9c8f3b421ed119edcb20499107887fd929211110e8418
2.4.3 caaad125acef1cb81d58dcdc454a1e429d09a750d1e9e2b3ed1aed8964454708
2.5.3 783b5d6c74105401c63946c68d9b2a4e1aab3c8abce043e06b8510b02b623ec9
2.6.1 064954c5d8c7e3b28188eee5b1727b31c411550bc5fefd41aa672d3c761d103a
3.0.2 46dbdcb5467a3dfec2526923d0b3365e40c8d9dc00ec23d5aca3437449e8cbfd
3.0.6 c956e5dfcac53d52bcf058360d579472f0c1d2d9b69f55209e256fe7783f4c74
3.1.3 4629c757b7618056f8ddd7e2625ae9fdd94c0372a65049520bc7d9df9efc7f71
VERSIONS

[[ -n $minimum ]] || rt_die 'no cosign version verified the bundle'
echo "Oldest passing version with install.sh argv: $minimum"
[[ -s $RT_SRC/install.sh ]] || rt_die 'install.sh is missing; merge Task 6 before checking COSIGN_MIN'
configured=$(sed -n 's/^[[:space:]]*COSIGN_MIN=//p' "$RT_SRC/install.sh" | head -1)
[[ $configured == "$minimum" ]] || rt_die "install.sh COSIGN_MIN=$configured, matrix minimum=$minimum"

echo 'Packaged cosign versions (absence is recorded, not a matrix failure):'
rt_log 'fetching Debian trixie package metadata'
rt_sibling_run --user 0 -- bash -lc 'apt-get update -qq && apt-cache policy cosign' || true
rt_log 'fetching Ubuntu 26.04 LTS package metadata'
rt_sibling_run --user 0 --image 'ubuntu:26.04@sha256:f144425ff09be612d6d9ad965196e9cdc23dae1f42110a8a11a3e9a8198759f7' -- \
  bash -lc 'apt-get update -qq && apt-cache policy cosign' || true
rt_log 'fetching Fedora 44 package metadata'
rt_sibling_run --user 0 --image 'fedora:44@sha256:43b29f65a41eb9c35e1cd5323e3bdf3b655c2357a9f4f1ff2f9c2798e5045d80' -- \
  bash -lc 'dnf -q makecache && dnf info cosign' || true
