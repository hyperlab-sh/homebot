#!/usr/bin/env bash
# Fake-release install tests. The server and clients share only this run's volume.
set -euo pipefail
# shellcheck source-path=SCRIPTDIR source=../lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/../lib.sh"
rt_require_uid 1000
rt_require_env RT_SRC RT_VOL
site=$RT_VOL/install-site
rm -rf "$site" "$RT_VOL/install-test"
mkdir -p "$site" "$RT_VOL/install-test"
sh "$RT_SRC/scripts/release-test/fake-release/make.sh" "$site"
cp "$RT_SRC/install.sh" "$RT_VOL/install-test/install.sh"
cp "$RT_SRC/scripts/release-test/fake-release/cosign" "$RT_VOL/install-test/cosign"
cp "$RT_SRC/scripts/release-test/fake-release/check.sh" "$RT_VOL/install-test/check.sh"
cp "$RT_SRC/scripts/release-test/fake-release/root-check.sh" "$RT_VOL/install-test/root-check.sh"
cp "$RT_SRC/scripts/release-test/fake-release/timeout-check.sh" "$RT_VOL/install-test/timeout-check.sh"
cp "$RT_SRC/scripts/release-test/fake-release/slow-server.py" "$RT_VOL/install-test/slow-server.py"
chmod +x "$RT_VOL/install-test/cosign"
rt_http_start install-site "$site" 8026
trap 'rt_sibling_stop install-site' EXIT
for shell in sh dash bash busybox; do
  echo "--- install.sh with $shell"
  rt_sibling_run --net-of install-site --env "INSTALL_SHELL=$shell" -- \
    /bin/bash /release/install-test/check.sh
done
rt_sibling_run --net-of install-site --user 0 -- \
  /bin/bash /release/install-test/root-check.sh
rt_sibling_start --name install-slow -- \
  python3 /release/install-test/slow-server.py
trap 'rt_sibling_stop install-slow; rt_sibling_stop install-site' EXIT
for _ in $(seq 1 30); do
  if rt_sibling_exec install-slow -- python3 -c \
    'import socket; s=socket.create_connection(("127.0.0.1",8027),1); s.close()' 2>/dev/null; then
    break
  fi
  sleep 0.2
done
for shell in sh dash bash busybox; do
  rt_sibling_run --net-of install-slow --env "INSTALL_SHELL=$shell" -- \
    /bin/bash /release/install-test/timeout-check.sh
done
