#!/usr/bin/env bash
# Exercises the privileged half of network policy WITHOUT root on the host: inside a
# rootless podman container (its own network namespace, where we are "root") it loads
# the exact nftables ruleset setup-host.sh installs, runs the real sandpit-netd
# against the real nft, starts sandpitd's policy DNS + transparent proxy, and drives
# two fake "sprites" (network namespaces on an msbr0 bridge) through the policy.
# WISP_POOL=N runs it as network pool N instead (msbrN, inet wispN, --pool N).
# WISP_SETUP, WISP_NETD and WISP_POOL keep wisp's names: the first two are what
# engine/netns_test.go reads, and the pool names are host-level ones sandpit
# shares with wisp (see setup-host.sh).
# See engine/netns_test.go for what is and is not covered; the real-host
# check is scripts/verify-network-policy.sh.
set -euo pipefail
cd "$(dirname "$0")/.."
IMG=localhost/sandpit-netpolicy-test
OUT=$(mktemp -d)
trap 'rm -rf "$OUT"' EXIT

podman image exists "$IMG" || podman build -q -t "$IMG" - <<'CONTAINERFILE'
FROM docker.io/library/ubuntu:24.04
RUN apt-get update -qq && DEBIAN_FRONTEND=noninteractive apt-get install -y -qq --no-install-recommends \
    nftables iproute2 dnsutils curl ca-certificates iputils-ping netcat-openbsd >/dev/null && rm -rf /var/lib/apt/lists/*
CONTAINERFILE

CGO_ENABLED=0 go build -o "$OUT/sandpit-netd" ./cmd/sandpit-netd
CGO_ENABLED=0 go test -c -tags netns -o "$OUT/engine.test" ./engine
cp scripts/setup-host.sh "$OUT/"

podman run --rm --cap-add NET_ADMIN,SYS_ADMIN,NET_RAW --sysctl net.ipv4.ip_forward=1 \
  -v "$OUT:/sandpit:ro" -e WISP_SETUP=/sandpit/setup-host.sh -e WISP_NETD=/sandpit/sandpit-netd -e WISP_POOL="${WISP_POOL:-0}" \
  "$IMG" /sandpit/engine.test -test.run TestNetworkPolicyInNamespaces -test.v -test.count=1
