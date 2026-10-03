# Development

```sh
make test     # unit tests, race detector. The vmm tests boot a real microVM; they skip without /dev/kvm.
              # The ACME test runs against pebble if it is on PATH (go install github.com/letsencrypt/pebble/v2/cmd/pebble@latest).
make e2e      # official Sprites Go SDK against a running sandpitd
make initrd   # rebuild the agent and sprite-env; sprites pick them up on their next *cold* boot
make netd     # build the network-policy helper that setup-host.sh installs

./scripts/test-netpolicy-netns.sh     # the real nft ruleset + helper, in a rootless podman netns
./scripts/verify-network-policy.sh    # network policy on the real host, from inside real guests
./scripts/verify-backup.sh            # backs a sprite up, deletes its whole data directory, restores it
./scripts/verify-custom-domains.sh /tmp/sandpit-x   # custom domain + TLS-ALPN-01 against a local pebble; E2E_RUN=. for the whole suite
```

e2e knobs: `SPRITES_E2E_IDLE_TIMEOUT=<the daemon's --idle-timeout>` enables the lifecycle
subtests (tasks, watch); `SPRITES_SDK_DEBUG=1` shows which connection mode the SDK used.
`SPRITES_E2E_GO_CONTROL=1` (with a daemon started `--control-for-go-sdk`) runs the tests that
drive `/control` with the Go SDK. `./scripts/probe-sdks.sh` runs the official JS and Python
SDKs against a running daemon: exec with control mode off and on, port proxying, and an exec
whose VM is restored away.
`SPRITES_E2E_IMAGES=1` runs `TestCreateFromImage`, which pulls alpine from Docker Hub (the
daemon's user needs working rootless podman and e2fsprogs >= 1.47.1).
The backup suite skips itself unless the daemon under test was started with a reachable
`--backup-bucket`.

Only one daemon per host may own a tap pool. For extra dev/test stacks:

```sh
./scripts/dev-data.sh /tmp/sandpit-x            # keep it short: the dir holds unix sockets (108-byte limit)
SANDPIT_DATA=/tmp/sandpit-x ./scripts/build-initrd.sh
./bin/sandpitd --data /tmp/sandpit-x --listen 127.0.0.1:7801 --net=false
```

A stack with `--net=false` has sprites without a NIC, so the network policy tests and
anything that fetches from inside a guest skip or fail. For a networked test stack beside
another daemon, create a second network pool once ([host setup](host-setup.md#a-second-network-pool))
and point the stack at it:

```sh
make netd && sudo WISP_POOL=1 ./scripts/setup-host.sh   # msbr1, 10.210.0.0/16, wisp-netd1 (host names are still wisp's)
./scripts/dev-data.sh ~/ws/1
SANDPIT_DATA=~/ws/1 ./scripts/build-initrd.sh
./bin/sandpitd --net-pool 1 --data ~/ws/1 --listen 127.0.0.1:7802
WISP_POOL=1 SPRITES_API_URL=http://127.0.0.1:7802 SPRITE_TOKEN=$(cat ~/ws/1/token) \
  ./scripts/verify-network-policy.sh
```

`./bin/sandpitd status --data ~/ws/1 --net-pool 1` finds that pool's helper when the daemon is
down.

## Code map

- `cmd/sandpitd`: the daemon, and the operator commands (`status`, `images`, `keys`,
  `restore`, `backups`). `internal/daemon` is what runs it: the flags, the data directory, the
  engine (`engine.New`), the Sprites API over it (`server.New`, on `--listen`) and the
  listeners. `cmd/sandpitd` adds the other front ends, each on a listener of its own
  (`daemon.Frontend`): E2B on `--e2b-listen`, Vercel Sandbox on `--vercel-listen`, Daytona on
  `--daytona-listen`, and Modal (partial) on `--modal-listen`.
- `cmd/sandpit-netd`: the root helper behind restrictive network policies; `cmd/sprite-env`:
  the Sprites-compatible tool inside the guest.
- `frontend/e2b`: the E2B front end ([e2b-sdk.md](e2b-sdk.md)): E2B's REST API, and a proxy
  that routes envd and port traffic into the guest over `engine.DialPort`. It keeps its
  metadata in `store.Record.Ext["e2b"]` and initializes envd from an `OnBoot` hook.
- `frontend/modal`: the Modal front end, partial ([modal-client.md](modal-client.md), [plan](plans/modal-parity.md)): two gRPC
  services on one h2c listener, generated from the modal 1.6.0 wheel's descriptors
  (`frontend/modal/modalpb`, cut down to the RPCs served). Exec goes through sandpit-agent's
  `POST /exec`. Metadata is in `store.Record.Ext["modal"]`, and apps and results are in
  `<data>/modal/state.json`.
- `frontend/vercel`: the Vercel Sandbox front end ([vercel-sdk.md](vercel-sdk.md)): Vercel's
  REST API, with commands and files translated into sandpit-agent's exec and filesystem API
  (no daemon in the guest) and routes proxied over `engine.DialPort`. Its metadata (sessions,
  routes, snapshots) is in `store.Record.Ext["vercel"]`; a session's timeout is the engine
  deadline (action stop), and snapshots are engine checkpoints.
- `frontend/daytona`: the Daytona front end ([daytona-sdk.md](daytona-sdk.md)): Daytona's
  REST API under `/api`; the toolbox (`/toolbox/<id>/...`), served host-side by translating
  each call to sandpit-agent's exec and filesystem API over `engine.AgentDial`, including
  sessions; and preview URLs by Host over `engine.DialPort`. Its metadata is in
  `store.Record.Ext["daytona"]`; auto-stop is the engine's idle rule, stop is `engine.Stop`.
- `engine/`: the sandbox engine, `*engine.Engine`, with no API of its own: VMs, disks and
  the sprite volume, checkpoints, network policy, admission and the disk guard, memory
  autoscale, backups, deadlines and leases, the idle rule, the image cache, the event bus
  and the guest's host channel. It knows a sandbox by its record (`store.Record`); a front
  end names sandboxes and adds its own behaviour through hooks (`SetDescriber`, `OnDelete`,
  `OnBoot`, `SetGuestAPI`, `SetBackupFilter`). The lock order is at the top of `engine/lifecycle.go`.
  It must not import `internal/server` or a front end (`go list -deps ./engine | grep -E 'internal/server|frontend'`).
- `internal/server`: the Sprites front end: the REST/WebSocket API, API keys, sprite URLs
  and custom domains, the web UI, the event stream and webhooks, the operator socket, and
  the API a guest reaches over its host channel.
- `internal/store` (records on disk), `internal/vmm` (Firecracker), `internal/agent` and
  `cmd/sandpit-agent` (the guest side), `internal/backup` and `internal/s3` (the bucket),
  `internal/netd` and `internal/netpolicy` (network policy enforcement).
