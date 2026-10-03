# Using the E2B SDKs

`sandpitd` serves the [E2B](https://e2b.dev) API beside the Sprites API, from the same engine,
so the official, unmodified E2B SDKs (Python `e2b`, JS `e2b`) create and drive sandboxes on
your machine: commands with streaming output, background processes, PTYs, files, signed
upload and download URLs, ports, timeouts, pause and resume. Inside, a sandbox runs E2B's own
in-guest daemon, envd, so what the SDK talks to is the real thing rather than a copy. What
differs from hosted E2B is in [providers/e2b-differences.md](providers/e2b-differences.md).
The same daemon serves the Sprites, Vercel, Daytona and Modal APIs too ([README](../README.md#the-apis)).

## Run it

The E2B API is off until `--e2b-listen` gives it an address (7901 is the suggested port):

```sh
make build                          # bin/sandpitd
./scripts/build-image.sh e2b        # <data>/images/e2b.ext4: E2B's base userland with envd (make images builds it too)
./bin/sandpitd --e2b-listen 127.0.0.1:7901
make install-service FLAGS='--e2b-listen 127.0.0.1:7901'   # or as the service
```

The Sprites API stays on `--listen` (127.0.0.1:7900), the E2B API is on
`--e2b-listen`. Both use one data directory, one store and the same API keys. A sandbox belongs
to the API that made it: E2B sandboxes are not in the Sprites API's lists, nor sprites in E2B's.
`sandpitd status`, `sandpitd keys` and the other operator commands cover E2B sandboxes too.

| Flag | Default | |
| --- | --- | --- |
| `--e2b-listen` | off | the E2B API and sandbox traffic, e.g. `127.0.0.1:7901` |
| `--e2b-domain` | `e2b.localhost` | sandboxes report `<this>:<listen port>` as their `domain`, so the SDK's `getHost(port)` is `<port>-<id>.e2b.localhost:7901`, which the listener serves |
| `--e2b-image` | `<data>/images/e2b.ext4` | the disk every E2B sandbox starts as |
| `--e2b-vcpus`, `--e2b-mem-mib` | 2, 512 | each sandbox's VM, as hosted E2B's `base` template |
| `--e2b-max-timeout` | 24h | the longest `timeout` a sandbox may have |

E2B sandboxes are never suspended for being idle: like hosted ones they run until their
timeout and are then killed, or paused with `onTimeout: 'pause'`.

## Point the SDK at it

```sh
export E2B_API_KEY=$(cat ~/.local/share/sandpit/token)   # the root token, or a key from `sandpitd keys create`
export E2B_API_URL=http://127.0.0.1:7901
export E2B_SANDBOX_URL=http://127.0.0.1:7901
```

```python
from e2b import Sandbox

sbx = Sandbox.create(timeout=300)
print(sbx.commands.run("echo hello from $(hostname); whoami").stdout)   # hello from e2b / user
sbx.files.write("/home/user/hello.txt", "hi")
h = sbx.commands.run("python3 -m http.server 8080", background=True)
print(sbx.get_host(8080))      # 8080-<id>.e2b.localhost:7901 -> curl http://<that>/
sbx.pause(); sbx.connect()     # a memory snapshot: files and processes survive
sbx.kill()
```

```js
import { Sandbox } from 'e2b'
const sbx = await Sandbox.create()
await sbx.commands.run('ls /', { onStdout: (d) => process.stdout.write(d) })
await sbx.kill()
```

`E2B_SANDBOX_URL` matters: without it the SDK sends envd traffic to
`https://49983-<id>.<domain>`, which needs wildcard DNS and a certificate, and this listener
speaks plain HTTP. With it, every envd call carries the sandbox's ID in a header and is routed
on that; the signed URLs from `upload_url()` / `download_url()` carry no ID at all and are
routed by their signature. `E2B_DOMAIN` is not needed: the server reports its own `domain`.

Port traffic is `http://<port>-<id>.<domain>/`: plain HTTP, on the E2B listener.

## Inside a sandbox

The disk is `images/e2b` ([images.md](images.md#the-e2b-image)): Ubuntu 24.04 with Python 3,
Node.js 22 and the build tools of E2B's `base` template, user `user` (uid 1000, passwordless
sudo), home `/home/user`, hostname `e2b`. envd runs there as a sandpit-agent system service on
port 49983, and the front end hands it the sandbox's access token, env vars and default user
with `POST /init` after every boot and resume, before any request reaches it.

## Testing it

The E2B probe suite runs both SDKs through every step above:

```sh
cd e2e/providers/e2b
E2B_API_URL=http://127.0.0.1:7901 E2B_SANDBOX_URL=http://127.0.0.1:7901 \
  E2B_PROBE_PORT_SCHEME=http E2B_API_KEY=$(cat <data>/token) ./run.sh
```

`E2B_RECORD_UPSTREAM=http://127.0.0.1:7901 E2B_RECORD_OUT=<dir> ./run.sh --record` (with the
same variables) records the traffic for comparison with hosted E2B's traces in `golden/`.
