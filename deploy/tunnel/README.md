# retroterm.com Cloudflare Tunnel

`retroterm.com` and `www.retroterm.com` reach dellcon **only** through the
Cloudflare Tunnel `retroterm-dellcon` (live since 2026-10-24). Nothing for
these hosts listens publicly. Caddy serves them on its internal
`http://…:8080` listener, which isn't published on the host, and a request for
them straight to the home IP gets Caddy's catch-all 404.

Outbound BBS connections are unchanged: the app still dials boards through
the `tor` container.

Built from the cross-project playbook
(`~/workspace/local/discussion/cloudflare-tunnels/README.md`). The files are
copied from `jamtastic-backend/deploy/tunnel`.

## Pieces

| File | Where it runs | What it is |
|---|---|---|
| `compose.yaml` | dellcon `/data/retroterm/tunnel/` | The `cloudflared` connector (`retroterm-cloudflared`), digest-pinned and hardened, as its own Compose project on the external `data_default` network. |
| `cloudflare.json` | — | Zone, tunnel name, hosts, the previous origin IP, and the ingress rules (everything → `http://caddy:8080`). |
| `cloudflare.mjs` | the admin Mac (Node 22+) | Helper that drives the Cloudflare API. Never prints credentials. |

The Caddy side lives in dellcon's `/data/caddy/Caddyfile`: a
`(retroterm_site)` snippet (`reverse_proxy retroterm:8080`), imported by the
`http://retroterm.com:8080, http://www.retroterm.com:8080` block.

## The helper

```
export CF_API_TOKEN_FILE=~/workspace/cf-key   # owner-only; never print it
cd deploy/tunnel
node cloudflare.mjs                           # read-only status
```

The token needs Account → Cloudflare Tunnel: Edit, and Zone: Read plus DNS:
Edit on retroterm.com. Only the value after `Bearer` (or a token-only file)
is used.

| Flags | Effect |
|---|---|
| *(none)* | Status: each host's DNS (`tunnel`, `origin`, `none`), the check host, tunnel health and connection count. |
| `--apply` | Create the tunnel if missing; set its routing if it has none. **No DNS changes.** |
| `--apply --sync-config` | Replace the tunnel's routing with `cloudflare.json`. |
| `--start` | With `RETROTERM_SSH_HOST=jeffryw@192.168.1.64 RETROTERM_REMOTE_SUDO=1`: fetch the tunnel token and start the connector over SSH stdin. The token never touches disk on the host. |
| `--check-up` / `--check-down` | Create or remove `tunnel-check.retroterm.com`, which reaches the site through the tunnel (its ingress rule sets the Host header). |
| `--cutover` | Point every host at the tunnel. Replaces a record only if it is exactly a proxied A at `previous_origin` (or missing). Anything else is refused. |
| `--rollback` | Undo `--cutover`: `retroterm.com` goes back to an A record at `previous_origin`, and the `www` CNAME is deleted (the zone's `*.retroterm.com` A record covers it again). |

Only address records (A/AAAA/CNAME) are ever read or written. The zone's
`*.retroterm.com` wildcard A record is left alone.

## Routine operations

- **Connector health:** `node cloudflare.mjs` should say `status: healthy;
  connections: 4`. On dellcon: `docker ps | grep retroterm-cloudflared`.
- **Restart the connector:** `docker restart retroterm-cloudflared` keeps its
  token. Recreating it (image bump, compose change) needs
  `node cloudflare.mjs --start` from the admin Mac.
- **Never** paste `docker inspect retroterm-cloudflared` or expanded
  `docker compose config` output anywhere: the tunnel token is in container
  metadata.
- A connector restart drops live BBS sessions. Users see "Connection lost"
  and use Reconnect; the page doesn't reconnect by itself.
- The WebSocket handler accepts a browser only when its `Origin` matches the
  `Host`, so the check host can't run the app in a browser. Test it with
  `curl` and a WebSocket client that sends no `Origin`.

## Rollback (back to direct-to-origin)

1. Put retroterm back on Caddy's public listener: restore
   `/data/caddy/Caddyfile.bak.20261024-tunnel-dualpath` (public + tunnel
   listeners) with `cat backup > Caddyfile` (not `mv`; the file is
   bind-mounted), then
   `docker exec data-caddy-1 caddy reload --config /etc/caddy/Caddyfile --adapter caddyfile`.
2. `node cloudflare.mjs --rollback` to restore the DNS records.
3. Optionally stop the connector: `docker stop retroterm-cloudflared`.

The Caddyfile from before any tunnel change is
`/data/caddy/Caddyfile.bak.20261024-pretunnel-retroterm`.
