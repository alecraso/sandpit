# Using the Modal client

`sandpitd` can serve a subset of [Modal](https://modal.com)'s API next to the other APIs, from
the same engine. The unmodified `modal` Python client (1.6.0) then creates sandboxes on your
machine and runs commands in them. **It is partial**: it started as a time-boxed spike and is
off unless `--modal-listen` is given (7904 is the suggested port). It covers sandboxes and exec only: no Functions, no image
builds, no files API. What works and what does not is in
[providers/modal-differences.md](providers/modal-differences.md). The protocol notes are in
[providers/modal.md](providers/modal.md). The plan to take it to the other providers' level is
[plans/modal-parity.md](plans/modal-parity.md).

## Run it

```sh
make build                            # bin/sandpitd
./scripts/build-image.sh modal        # <data>/images/modal.ext4: debian_slim, prebuilt
./bin/sandpitd --modal-listen 127.0.0.1:7904
```

| Flag | Default | |
| --- | --- | --- |
| `--modal-listen` | off | the Modal API: both gRPC services, over h2c |
| `--modal-image` | `<data>/images/modal.ext4` | the disk every Modal sandbox starts as |
| `--modal-router-url` | `http://<--modal-listen>` | where the client is told to reach the task command router (this listener) |

Modal sandboxes belong to the Modal API, and the Sprites and E2B APIs do not list them. Apps and finished sandboxes' results are kept
in `<data>/modal/state.json`.

## Point the client at it

```sh
export MODAL_SERVER_URL=http://127.0.0.1:7904
export MODAL_TOKEN_ID=sandpit                           # anything
export MODAL_TOKEN_SECRET=$(cat ~/.local/share/sandpit/token)   # the root token, or a key from `sandpitd keys create`
```

```python
import modal
app = modal.App.lookup("sandpit-demo", create_if_missing=True)
sb = modal.Sandbox.create("sleep", "infinity", app=app, image=modal.Image.debian_slim())
p = sb.exec("echo", "hi"); print(p.stdout.read())   # hi
sb.terminate()
```

The server has to be on `localhost` or `127.0.0.1`. Only then does the client accept the plain
`http://` command-router URL the server gives it. From another machine, use an SSH tunnel
(`ssh -L 7904:127.0.0.1:7904 host`). If you have a `~/.modal.toml`, a profile in it can override
these variables' defaults, so set all three explicitly.

## Inside a sandbox

The disk is `images/modal` ([images.md](images.md#the-modal-image)). It is
`python:3.14.2-slim-bookworm` with `debian_slim`'s build steps: gcc, gfortran and
build-essential, plus pip, wheel and uv. Commands run as root, in `/` unless `workdir=` says
otherwise, with `HOME=/root`.

## Testing it

```sh
MODAL_SERVER_URL=http://127.0.0.1:7904 MODAL_TOKEN_SECRET=$(cat <data>/token) e2e/providers/modal/run.sh
```

This installs the pinned client in a venv and runs the target script on V2 and on V1
(`MODAL_SANDBOX_V2=0`). It also runs 14 more checks: exit codes, stderr, env and workdir,
`from_id`, the timeouts, the entrypoint's exit, a large output, and an image the server
refuses. It prints one PASS or FAIL line per check, and the exit status is the number of
failures.
