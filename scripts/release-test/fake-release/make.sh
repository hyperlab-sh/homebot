#!/bin/sh
# Build a local, unsigned GitHub-shaped release tree for install-sh tests.
set -eu
out=${1:?output directory required}
mkdir -p "$out/releases/latest/download"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' 0
for version in 0.0.1 0.0.2 0.0.2-rc.1 0.0.3 0.0.4 0.0.5 0.0.6 0.0.7; do
  dir=$out/releases/download/v$version
  mkdir -p "$dir"
  : >"$dir/checksums.txt"
  printf '{}\n' >"$dir/checksums.txt.sigstore.json"
  for os in linux darwin; do
    for arch in amd64 arm64; do
      mkdir -p "$tmp/payload"
      rm -f "$tmp/payload/homebot"
      cat >"$tmp/payload/homebot" <<EOF
#!/bin/sh
[ "\${1:-}" = --version ] || exit 1
[ "\${HOMEBOT_PLAIN:-}" = 1 ] || { echo 'voiced version'; exit 0; }
echo '$version'
EOF
      case $version in
        0.0.3) printf '#!/bin/sh\nexit 1\n' >"$tmp/payload/homebot" ;;
        0.0.4) printf '#!/bin/sh\necho wrong\n' >"$tmp/payload/homebot" ;;
        0.0.5) rm "$tmp/payload/homebot"; ln -s ../outside "$tmp/payload/homebot" ;;
        0.0.6) printf '#!/bin/sh\nsleep 60\necho 0.0.6\n' >"$tmp/payload/homebot" ;;
      esac
      [ -L "$tmp/payload/homebot" ] || chmod 755 "$tmp/payload/homebot"
      asset=homebot_${version}_${os}_${arch}.tar.gz
      if [ "$version" = 0.0.7 ]; then
        printf 'bad\n' >"$tmp/payload/evil"
        tar -czf "$dir/$asset" -C "$tmp/payload" homebot \
          --transform='s@^evil$@../../evil@' evil
      else
        tar -czf "$dir/$asset" -C "$tmp/payload" homebot
      fi
      sha256sum "$dir/$asset" | awk -v name="$asset" '{print $1 "  " name}' >>"$dir/checksums.txt"
    done
  done
done
cp "$out/releases/download/v0.0.2/checksums.txt" "$out/releases/latest/download/checksums.txt"
