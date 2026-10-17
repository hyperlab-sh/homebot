#!/usr/bin/env bash
# release-test: not-in-all
# Exercise the two snapshot packages in fresh Debian, Ubuntu and Fedora systems.
# Needs --siblings and reaches the network (distro mirrors), so it runs by name.
# The release-test wrapper owns the siblings and their shared /release volume.
set -euo pipefail
# shellcheck source-path=SCRIPTDIR source=../lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/../lib.sh"
rt_require_uid 1000
_rt_sibling_guard
trap 'rt_cleanup_siblings' EXIT
rt_need_snapshots

[[ $(uname -m) == x86_64 ]] || rt_die "packages: amd64 execution requires an x86_64 host"

# Pinned image manifests, rather than mutable distribution tags.
# Debian's amd64 trixie manifest: docker-library/repo-info, 2026-08-24.
debian=debian:trixie@sha256:6788062a1b42ac281f053ac876170b79a3eaed5d61383b8ed7eaca6c6965f3b1
ubuntu=ubuntu:26.04@sha256:f144425ff09be612d6d9ad965196e9cdc23dae1f42110a8a11a3e9a8198759f7
fedora=fedora:44@sha256:43b29f65a41eb9c35e1cd5323e3bdf3b655c2357a9f4f1ff2f9c2798e5045d80
# Pinned arm64 manifests for the same three images (one digest per platform).
debian_arm=debian:trixie@sha256:0aa0908407cce3da2a90c1d80acc6ca5ca57401ed63eecfa8149b7ba3cc40829
ubuntu_arm=ubuntu:26.04@sha256:da6fc2be547864451aa253836dd926da33623312df4a9a243e35dc877c378a78
fedora_arm=fedora:44@sha256:e402cca673711fee025f9ce21c6c08b1bd25ee26b3c482119c8675fbaaedbc85

one_artifact() {
  local version=$1 suffix=$2 arch=$3 name found=()
  case $suffix:$arch in
    deb:amd64|deb:arm64) name="homebot_${version}_${arch}.deb" ;;
    rpm:amd64) name="homebot-${version}-1.x86_64.rpm" ;;
    rpm:arm64) name="homebot-${version}-1.aarch64.rpm" ;;
    *) rt_die "packages: unsupported $suffix/$arch artifact" ;;
  esac
  while IFS= read -r -d '' file; do found+=("$file"); done \
    < <(find "$RT_VOL/snapshots/v$version" -maxdepth 1 -type f \
      -name "$name" -print0)
  (( ${#found[@]} == 1 )) || rt_die "packages: want one $arch .$suffix for v$version, found ${#found[@]}"
  printf '%s\n' "${found[0]}"
}

mkdir -p "$RT_VOL/task9"
cp "$RT_SRC/scripts/release-test/fixtures/package-check.sh" "$RT_VOL/task9/package-check.sh"
chmod 755 "$RT_VOL/task9/package-check.sh"

for kind in deb rpm; do
  for version in 0.0.1 0.0.2; do
    package=$(one_artifact "$version" "$kind" amd64)
    ln -sf "$package" "$RT_VOL/task9/$kind-$version.$kind"
    # Structural checks for arm64 even when forge has no binfmt interpreter.
    arm=$(one_artifact "$version" "$kind" arm64)
    ln -sf "$arm" "$RT_VOL/task9/$kind-$version-arm64.$kind"
    [[ -s $arm ]] || rt_die "packages: empty arm64 .$kind: $arm"
    case $kind in
      deb) dpkg-deb -f "$arm" Architecture | grep -Fxq arm64 \
        || rt_die "packages: invalid arm64 deb: $arm" ;;
      rpm) rpm -qp --queryformat '%{ARCH}\n' "$arm" | grep -Fxq aarch64 \
        || rt_die "packages: invalid arm64 rpm: $arm" ;;
    esac
  done
done

# A working arm64 binfmt handler makes the runtime checks mandatory. An
# ordinary exec-format failure means this host can only inspect the packages.
arm_probe=$RT_VOL/task9/arm64-probe.log
arm_runtime=0
# shellcheck disable=SC2016 # uname must run in the sibling, not here.
if rt_sibling_run --user 0 --platform linux/arm64 --image "$debian_arm" -- \
  /bin/sh -c 'test "$(uname -m)" = aarch64' >"$arm_probe" 2>&1; then
  arm_runtime=1
elif grep -Eiq 'exec format error|binfmt.*(not found|not registered|unavailable)' "$arm_probe"; then
  rt_log "packages: arm64 runtime unavailable; structural checks passed ($(tail -1 "$arm_probe"))"
else
  cat "$arm_probe" >&2
  rt_die 'packages: arm64 runtime probe failed for a reason other than missing binfmt'
fi

for distro in debian ubuntu fedora; do
  case $distro in
    debian) image=$debian; kind=deb ;;
    ubuntu) image=$ubuntu; kind=deb ;;
    fedora) image=$fedora; kind=rpm ;;
  esac
  rt_log "packages: $distro ($image)"
  rt_sibling_run --user 0 --image "$image" \
    --env "PKG_KIND=$kind" --env "PKG_DISTRO=$distro" --env PKG_ARCH=amd64 -- \
    bash /release/task9/package-check.sh
  if (( arm_runtime )); then
    case $distro in
      debian) image=$debian_arm ;;
      ubuntu) image=$ubuntu_arm ;;
      fedora) image=$fedora_arm ;;
    esac
    rt_log "packages: $distro arm64 ($image)"
    rt_sibling_run --user 0 --platform linux/arm64 --image "$image" \
      --env "PKG_KIND=$kind" --env "PKG_DISTRO=$distro" --env PKG_ARCH=arm64 -- \
      bash /release/task9/package-check.sh
  fi
done
