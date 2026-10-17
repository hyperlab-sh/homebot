#!/bin/sh
# Install a verified homebot release into the current user's bin directory.
# Keep all work inside functions: an incomplete curl | sh download does nothing.

usage() {
  cat <<'EOF'
Usage: install.sh [--version vX.Y.Z] [--bin-dir DIR] [--require-signature]
                  [--base-url URL] [--max-time SECONDS] [--help]
The base URL flag is for mirrors and tests. Run this script without sudo.
EOF
}

fail() { printf 'install.sh: %s\n' "$2" >&2; exit "$1"; }
cleanup() {
  [ -z "${install_tmp:-}" ] || rm -rf "$install_tmp"
  [ -z "${install_new:-}" ] || rm -f "$install_new"
  [ -z "${receipt_tmp:-}" ] || rm -f "$receipt_tmp"
}
valid_version() {
  case $1 in *'
'*) return 1 ;; esac
  printf '%s\n' "$1" | LC_ALL=C grep -Eq '^v?[0-9]+\.[0-9]+\.[0-9]+(-rc\.[0-9]+)?$'
}
version_number() { printf '%s\n' "${1#v}"; }

check_user() {
  if [ -z "${HOME:-}" ] || [ ! -d "$HOME" ]; then
    fail 7 'HOME must name your home directory; run without sudo'
  fi
  [ -z "${SUDO_USER:-}" ] || [ "$SUDO_USER" = root ] || fail 7 'run without sudo'
  if [ "$(id -u)" = 0 ]; then
    # Follow a symlinked HOME: the target directory, not the link, owns the
    # files this script would create. Quoting HOME preserves spaces.
    # shellcheck disable=SC2012
    owner=$(ls -Ldn "$HOME" | awk '{print $3}')
    [ "$owner" = 0 ] || fail 7 'HOME belongs to another user; run without sudo'
  fi
}

platform() {
  case $(uname -s) in
    Linux) os=linux ;;
    Darwin) os=darwin ;;
    *) fail 2 'unsupported operating system' ;;
  esac
  case $(uname -m) in
    x86_64|amd64) arch=amd64 ;;
    aarch64|arm64) arch=arm64 ;;
    *) fail 2 'unsupported architecture' ;;
  esac
  if [ "$os" = darwin ] && [ "$arch" = amd64 ]; then
    translated=$(sysctl -n sysctl.proc_translated 2>/dev/null || echo 0)
    [ "$translated" != 1 ] || arch=arm64
  fi
}

check_base() {
  case $base in
    https://?*) secure=1 ;;
    http://*)
      # Inspect the whole authority. A prefix like 127.0.0.1:80@remote is
      # userinfo for a remote host, not a loopback URL.
      case $base in *'@'*|*'?'*|*'#'*) fail 1 'invalid loopback base URL' ;; esac
      authority=${base#http://}
      authority=${authority%%/*}
      case $authority in
        127.0.0.1|localhost) ;;
        127.0.0.1:*|localhost:*)
          port=${authority#*:}
          case $port in ''|*[!0-9]*|??????*) fail 1 'invalid loopback port' ;; esac
          if [ "$port" -lt 1 ] || [ "$port" -gt 65535 ]; then
            fail 1 'invalid loopback port'
          fi
          ;;
        *) fail 1 'HTTP is allowed only for an exact loopback host' ;;
      esac
      secure=0 ;;
    *) fail 1 'base URL must use HTTPS (HTTP is allowed only for loopback tests)' ;;
  esac
  base=${base%/}
}

download() {
  url=$1 dest=$2
  if command -v curl >/dev/null 2>&1; then
    if [ "$secure" = 1 ]; then
      curl -fsSL --connect-timeout 10 --max-time "$max_time" --retry 2 \
        --proto '=https' --tlsv1.2 -o "$dest" "$url" || fail 3 "download failed: $url"
    else
      curl -fsSL --connect-timeout 10 --max-time "$max_time" --retry 2 \
        -o "$dest" "$url" || fail 3 "download failed: $url"
    fi
  elif command -v wget >/dev/null 2>&1; then
    wget -q -T 30 -t 3 -O "$dest" "$url" || fail 3 "download failed: $url"
  else
    fail 1 'curl or wget is required'
  fi
}

latest_version() {
  download "$base/releases/latest/download/checksums.txt" "$install_tmp/latest-checksums.txt"
  # Read the version only from the archive line for this platform. Reject
  # ambiguous, malformed and attacker-controlled versions before making a URL.
  matches=$(awk -v suffix="_${os}_${arch}.tar.gz" \
    'NF == 2 && $2 ~ /^homebot_/ && substr($2, length($2)-length(suffix)+1) == suffix {print substr($2, 9, length($2)-8-length(suffix))}' \
    "$install_tmp/latest-checksums.txt")
  case $matches in ''|*'
'*) fail 1 'latest checksums have no unique archive version for this platform' ;; esac
  valid_version "$matches" || fail 1 'latest checksums contain an invalid version'
  version=$matches
}

cosign_major() {
  command -v cosign >/dev/null 2>&1 || return 1
  printed=$(cosign version 2>/dev/null) || return 1
  found=$(printf '%s\n' "$printed" | sed -n 's/.*GitVersion:[[:space:]]*v\{0,1\}\([0-9][0-9]*\.[0-9][0-9]*\.[0-9][0-9]*\)[[:space:]]*$/\1/p' | head -n 1)
  [ -n "$found" ] || return 1
  major=${found%%.*}; rest=${found#*.}; minor=${rest%%.*}; patch=${rest#*.}
  min_major=${COSIGN_MIN%%.*}; rest=${COSIGN_MIN#*.}; min_minor=${rest%%.*}; min_patch=${rest#*.}
  [ "$major" -gt "$min_major" ] || {
    [ "$major" -eq "$min_major" ] && {
      [ "$minor" -gt "$min_minor" ] || {
        [ "$minor" -eq "$min_minor" ] && [ "$patch" -ge "$min_patch" ];
      }
    }
  } || return 1
  printf '%s\n' "$major"
}

verify_signature() {
  if cosign_version_major=$(cosign_major); then
    if [ "$cosign_version_major" -eq 2 ]; then
      cosign verify-blob --new-bundle-format \
        --certificate-identity "https://github.com/hyperlab-sh/homebot/.github/workflows/release.yml@refs/tags/$tag" \
        --certificate-oidc-issuer https://token.actions.githubusercontent.com \
        --bundle "$install_tmp/checksums.txt.sigstore.json" "$install_tmp/checksums.txt" \
        >/dev/null || fail 5 'signature verification failed'
    else
      cosign verify-blob \
        --certificate-identity "https://github.com/hyperlab-sh/homebot/.github/workflows/release.yml@refs/tags/$tag" \
        --certificate-oidc-issuer https://token.actions.githubusercontent.com \
        --bundle "$install_tmp/checksums.txt.sigstore.json" "$install_tmp/checksums.txt" \
        >/dev/null || fail 5 'signature verification failed'
    fi
  else
    [ "$require_signature" = 0 ] || fail 4 "cosign $COSIGN_MIN or newer is required to check the signature"
    printf 'install.sh: signature not checked; install cosign %s or newer to verify it\n' "$COSIGN_MIN" >&2
  fi
}

sha256_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}'
  elif command -v openssl >/dev/null 2>&1; then
    openssl dgst -sha256 "$1" | awk '{print $NF}'
  else
    fail 1 'sha256sum, shasum or openssl is required'
  fi
}

verify_archive() {
  expected=$(awk -v name="$archive" '$2 == name && NF == 2 {print $1}' "$install_tmp/checksums.txt")
  case $expected in ''|*'
'*) fail 5 'archive checksum line is missing or ambiguous' ;; esac
  printf '%s\n' "$expected" | LC_ALL=C grep -Eq '^[0-9a-fA-F]{64}$' || fail 5 'archive checksum is malformed'
  actual=$(sha256_file "$install_tmp/$archive") || fail 5 'could not hash archive'
  [ "$actual" = "$expected" ] || fail 5 'archive checksum mismatch'
}

write_receipt() {
  case ${XDG_DATA_HOME:-} in
    /*) data=$XDG_DATA_HOME ;;
    *) data=$HOME/.local/share ;;
  esac
  receipt_dir=$data/homebot
  mkdir -p "$receipt_dir" || fail 1 'could not create receipt directory'
  receipt_tmp=$receipt_dir/.install-receipt.$$
  {
    printf 'method=script\nbin=%s\nversion=%s\ninstalled=%s\n' \
      "$bin_dir/homebot" "$number" "$(date +%s)"
  } >"$receipt_tmp" || fail 1 'could not write receipt'
  mv -f "$receipt_tmp" "$receipt_dir/install-receipt" || fail 1 'could not save receipt'
  receipt_tmp=
}

main() {
  set -eu
  umask 022
  COSIGN_MIN=2.4.3
  base=https://github.com/hyperlab-sh/homebot
  bin_dir=${HOME:-}/.local/bin
  version='' require_signature=0 max_time=120
  while [ "$#" -gt 0 ]; do
    case $1 in
      --version|--bin-dir|--base-url|--max-time)
        [ "$#" -ge 2 ] || fail 1 "$1 needs a value"
        case $1 in
          --version) version=$2 ;;
          --bin-dir) bin_dir=$2 ;;
          --base-url) base=$2 ;;
          --max-time) max_time=$2 ;;
        esac
        shift 2 ;;
      --require-signature) require_signature=1; shift ;;
      --help) usage; return 0 ;;
      *) fail 1 "unknown option: $1" ;;
    esac
  done
  check_user
  case $max_time in ''|*[!0-9]*) fail 1 '--max-time must be a positive integer' ;; esac
  [ "$max_time" -gt 0 ] || fail 1 '--max-time must be a positive integer'
  [ -n "$bin_dir" ] || fail 1 '--bin-dir must not be empty'
  check_base
  platform
  [ -z "$version" ] || valid_version "$version" || fail 1 'invalid version (want vX.Y.Z or vX.Y.Z-rc.N)'
  install_tmp=$(mktemp -d) || fail 1 'could not create temporary folder'
  install_new=
  trap cleanup 0
  trap 'exit 1' 1 2 3 15
  [ -n "$version" ] || latest_version
  number=$(version_number "$version")
  tag=v$number
  archive=homebot_${number}_${os}_${arch}.tar.gz
  release=$base/releases/download/$tag
  download "$release/checksums.txt" "$install_tmp/checksums.txt"
  download "$release/checksums.txt.sigstore.json" "$install_tmp/checksums.txt.sigstore.json"
  download "$release/$archive" "$install_tmp/$archive"
  verify_signature
  verify_archive
  mkdir "$install_tmp/extract" || fail 1 'could not prepare extraction folder'
  tar -xzf "$install_tmp/$archive" -C "$install_tmp/extract" homebot 2>/dev/null \
    || fail 5 'archive does not contain a safe homebot file'
  if [ ! -f "$install_tmp/extract/homebot" ] || [ -L "$install_tmp/extract/homebot" ]; then
    fail 5 'archive homebot is not a regular file'
  fi
  mkdir -p "$bin_dir" || fail 7 'bin directory is not writable'
  [ -w "$bin_dir" ] || fail 7 'bin directory is not writable'
  [ ! -d "$bin_dir/homebot" ] || fail 7 'bin directory has a homebot directory, not a binary'
  install_new=$bin_dir/.homebot.new.$$
  cp "$install_tmp/extract/homebot" "$install_new" || fail 7 'bin directory is not writable'
  chmod 0755 "$install_new" || fail 7 'could not make homebot executable'
  smoke=$(HOMEBOT_PLAIN=1 "$install_new" --version) || fail 6 'new homebot failed its smoke test'
  [ "$smoke" = "$number" ] || fail 6 "new homebot printed '$smoke', expected '$number'"
  mv -f "$install_new" "$bin_dir/homebot" || fail 1 'could not install homebot'
  install_new=
  write_receipt
  case :${PATH:-}: in
    *:"$bin_dir":*) ;;
    *) printf 'install.sh: add this directory to PATH: %s\n' "$bin_dir" >&2 ;;
  esac
  printf 'Installed homebot %s. Next: homebot setup\n' "$number"
}

main "$@"
