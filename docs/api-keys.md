# API keys and serving the API in public

Every request to the API carries `Authorization: Bearer <token>`. Two kinds of token work:

- the **root token** in `<data>/token`, written on first start. It is the break-glass key:
  always admin, never revoked except by replacing the file and restarting;
- **API keys**, made and revoked one by one, each with a name and a scope.

Hand out keys, not the root token: an app, a CI job or a person each get their own, and losing
one means revoking that one.

## Scopes

| scope | may |
|---|---|
| `admin` | everything the root token can: the whole API, private sprite URLs (`url_settings.auth: "sprite"`), the dashboard |
| `read` | `GET` and `HEAD` only, and no WebSockets: list and inspect sprites, checkpoints, policies, the event stream, and read a sprite's files. Not exec, the TCP proxy, `/control`, or any change. Private sprite URLs refuse it |

A read key that tries more gets `403 {"error":"forbidden"}`; a missing or unknown key gets
`401` with `WWW-Authenticate: Bearer realm="sandpit"`.

## Managing keys

On the host, over the operator socket (so it works whatever has leaked, and needs a running
daemon):

```sh
$ sandpitd keys create preview-bot                # admin by default
created admin key "preview-bot" (id 3f9c21ab). This is the only time it is shown:
wisp_3f9c21ab_5d0e…
$ sandpitd keys create grafana --scope read
$ sandpitd keys list
ID        NAME          SCOPE  CREATED           LAST USED
3f9c21ab  preview-bot   admin  2026-09-25 13:25  2026-09-25 13:40
$ sandpitd keys revoke preview-bot                # by name or id
```

The key goes to stdout and the message to stderr, so `KEY=$(sandpitd keys create ci)` works. Or
from the dashboard's **Keys** page, when signed in with an admin key or the root token. The
bearer API itself cannot make or revoke keys, so one leaked key cannot mint another.

A key reads `wisp_<id>_<secret>` (the prefix, like the root token's `wisproot_`, is still
wisp's, so keys made by a wisp `sandboxd` keep working when sandpitd
[takes it over](operations.md#beside-wisp)). Only a SHA-256 of it is kept, in `<data>/keys.json`
(mode 0600); the id is what lists and logs show. `last_used_at` is written at most once a
minute per key. If `keys.json` cannot be read, the daemon logs it, no key works and none can
be made (so the file is not overwritten), and the root token still does.

Revoking a key refuses its next request and signs out the dashboard sessions made with it.
A connection already open with it, such as an exec session or the event stream, carries on
until it closes.

## Serving the API in public

The API listener (`--listen`, `127.0.0.1:7900` by default) also serves the dashboard and sprite
URLs, told apart by the `Host` header. Do not publish it. Give the proxy a listener of its own:

```sh
sandpitd --api-listen 127.0.0.1:7909 --api-host sprites.example.com ...
```

`--api-listen` serves the bearer API and nothing else, whatever the `Host`: no dashboard, no
dashboard cookie, no sprite URLs. What the proxy publishes then does not depend on the proxy
passing `Host` through unchanged. Put a TLS-terminating reverse proxy in front of it.

`--api-host` covers a proxy that is still pointed at `--listen`. On that listener an
`--api-host` name is also the bearer API and nothing else:

- never a sprite, even when it sits under a `--url-domain` (`sprites.example.com` under
  `example.com` is not the sprite `sprites`);
- never the dashboard: `/` and `/ui/` answer 401 there, and the dashboard's cookie counts for
  nothing on that name. The dashboard stays on the names the proxy does not serve: localhost, a
  tailnet address, an SSH tunnel.

That protection holds only while the proxy passes the `Host` header through unchanged, which
is why `--api-listen` is the one to rely on.

Either way the name must be kept out of any SNI passthrough route for the wildcard, so the
proxy terminates its TLS rather than handing it to the public listener. With Traefik (as in
[public URLs](public-urls.md)), that is `&& !HostSNI(`sprites.example.com`)` on the passthrough
match, plus an ordinary `IngressRoute` for `Host(`sprites.example.com`)` to the API listener
with its own certificate.

Clients then use `SPRITES_API_URL=https://sprites.example.com` and a key as the token.

## Not built

Keys scoped to particular sprites, expiry, cutting off open connections at revocation, and
throttling failed attempts per client (keys carry 256 random bits, so guessing is not the
risk; behind a proxy every request also arrives from the proxy's address). See item 5 of the
[feature brief](feature-brief.md).
