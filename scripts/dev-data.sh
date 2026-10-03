#!/usr/bin/env bash
# Creates a private sandpitd data directory that shares the big read-only
# artifacts (firecracker, kernel, images) with the main one, so several
# isolated stacks can run side by side:
#
#   ./scripts/dev-data.sh /tmp/sp-a              # keep the path SHORT: it holds unix sockets (108-byte limit)
#   SANDPIT_DATA=/tmp/sp-a ./scripts/build-initrd.sh
#   ./bin/sandpitd --data /tmp/sp-a --listen 127.0.0.1:7801 --net=false
#
# The main data directory is $SANDPIT_MAIN_DATA, default ~/.local/share/sandpit.
set -euo pipefail
DEST="${1:?usage: dev-data.sh <short-dir>}"
MAIN="${SANDPIT_MAIN_DATA:-${XDG_DATA_HOME:-$HOME/.local/share}/sandpit}"
mkdir -p "$DEST"
for d in bin kernel images; do
  [ -e "$MAIN/$d" ] || { echo "missing $MAIN/$d (run: make deps image)" >&2; exit 1; }
  ln -sfn "$MAIN/$d" "$DEST/$d"
done
echo "$DEST ready; token will be generated on first start at $DEST/token"
