#!/usr/bin/env bash
# Builds images/base and checks what a sprite's agent runtimes need from it, as
# `sprite`, twice: under `bash -lc`, and under `env -i` with the environment
# sandpit's agent gives a non-login exec session (internal/agent/session.go
# baseEnv). The second run is the one that matters: `bash -lc` sources the
# profile, which an exec session never does.
#
#   ./scripts/test-image.sh [docker|podman]     # default: docker, else podman
#
# This builds and runs the container image only: no KVM, and not the ext4 that
# `make image` makes (that needs Linux, rootless podman and e2fsprogs 1.47.1+).
set -euo pipefail
cd "$(dirname "$0")/.."

rt=${1:-}
if [ -z "$rt" ]; then
  for c in docker podman; do command -v "$c" >/dev/null && rt=$c && break; done
fi
case "$rt" in docker | podman) command -v "$rt" >/dev/null || { echo "test-image: $rt not found" >&2; exit 2; } ;;
  *) echo "usage: $0 [docker|podman]" >&2; exit 2 ;;
esac

IMG=sandpit-base-test
NODE_VERSION=$(sed -n 's/^ARG NODE_VERSION=//p' images/base/Containerfile)
"$rt" build -q -t "$IMG" -f images/base/Containerfile images/base >/dev/null
echo "built $IMG ($("$rt" image inspect --format '{{.Size}}' "$IMG") bytes)"

# What runs inside the container, as sprite. Each run is a fresh container.
read -r -d '' CHECK <<'EOS' || true
fails=0
ok() { echo "  ok   $*"; }
bad() { echo "  FAIL $*"; fails=$((fails + 1)); }
check() { local what=$1; shift; if "$@" >/dev/null 2>&1; then ok "$what"; else bad "$what"; fi; }
expect() { local what=$1 want=$2 got; shift 2; got=$("$@" 2>&1) || true; if [ "$got" = "$want" ]; then ok "$what: $got"; else bad "$what: got '$got', want '$want'"; fi; }
cd "$HOME"
expect "whoami" sprite whoami
expect "node --version" "v$NODE_VERSION" node --version
expect "command -v node" /usr/local/bin/node bash -c 'command -v node'
check "npm --version" npm --version
expect "node -p process.execPath" /usr/local/bin/node node -p process.execPath
for d in .local .local/bin .local/share; do
  expect "owner of ~/$d" sprite stat -c %U "$HOME/$d"
done
case ":$PATH:" in *":$HOME/.local/bin:"*) ok "~/.local/bin is on PATH" ;; *) bad "~/.local/bin is not on PATH: $PATH" ;; esac

# npm install -g of a tiny package, no sudo; its bin must resolve by name.
t=$(mktemp -d); mkdir "$t/pkg"
cat > "$t/pkg/package.json" <<'EOP'
{"name":"sandpit-image-probe","version":"1.0.0","bin":{"sandpit-image-probe":"cli.js"}}
EOP
printf '#!/usr/bin/env node\nconsole.log("probe-ok")\n' > "$t/pkg/cli.js"
(cd "$t" && npm pack --silent ./pkg >/dev/null 2>&1)
if npm install -g --no-audit --no-fund "$t"/sandpit-image-probe-1.0.0.tgz >/dev/null 2>&1; then
  ok "npm install -g, no sudo"
  expect "npm -g bin on PATH" "probe-ok" sandpit-image-probe
  npm uninstall -g sandpit-image-probe >/dev/null 2>&1 || true
else
  bad "npm install -g, no sudo"
fi

# The adapter install: symlink into ~/.local/bin, mv -Tf into place, found by name.
printf '#!/bin/sh\necho adapter-ok\n' > "$t/real"; chmod +x "$t/real"
if ln -sfn "$t/real" "$HOME/.local/bin/.claude-agent-acp.new" &&
   mv -Tf "$HOME/.local/bin/.claude-agent-acp.new" "$HOME/.local/bin/claude-agent-acp"; then
  expect "symlink in ~/.local/bin found by name" adapter-ok claude-agent-acp
else
  bad "ln -sfn + mv -Tf into ~/.local/bin"
fi

check "sudo -n true" sudo -n true
check "xargs -d" bash -c "printf 'a\nb\n' | xargs -d '\n' -n1 true"
check "mv -T" bash -c "mkdir -p $t/m1 $t/m2 && mv -T $t/m1 $t/m3"
check "base64 -w0" bash -c 'echo x | base64 -w0'
check "date +%s%N" bash -c '[ "$(date +%s%N | wc -c)" -gt 11 ]'
check "mkfifo" mkfifo "$t/fifo"
check "flock" flock -n "$t" true
check "ldd" ldd /bin/sh
check "openssl" openssl version
check "curl" curl --version
for c in git tar jq rg; do check "$c" command -v "$c"; done
exit "$fails"
EOS

# Exactly sandpit's exec environment for sprite (home /home/sprite).
EXEC_PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:/home/sprite/.local/bin
run() { "$rt" run --rm -u sprite -w /home/sprite "$IMG" "$@"; }
status=0
echo "== as sprite, bash -lc"
run bash -lc "NODE_VERSION=$NODE_VERSION; $CHECK" || status=1
echo "== as sprite, env -i bash -c (non-login exec session)"
run env -i PATH="$EXEC_PATH" HOME=/home/sprite USER=sprite bash -c "NODE_VERSION=$NODE_VERSION; $CHECK" || status=1
[ "$status" = 0 ] && echo "PASS" || echo "FAIL"
exit "$status"
