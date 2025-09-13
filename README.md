# RetroTerm

RetroTerm is a minimal, stateless web app that serves a curated BBS directory and connects to remote BBSes over telnet/SSH from your browser. It supports optional:

- ZMODEM receive via the external `lrzsz` tools (uses `rz`)
- Outbound connections over Tor via a local SOCKS5 proxy


## Build & Run

Prerequisites:

- Go (version matching `go.mod`, currently `go 1.24`)

First-time setup (downloads modules):

```bash
go mod download
```

Build:

```bash
go build -o retroterm .
```

Run:

```bash
./retroterm
# or for quick testing
go run .
```

The server listens on the port from `config.json` (default 8080) and serves the UI from `./static`.


## Go Build Gotchas

- Go toolchain version: ensure Go >= the version in `go.mod` (currently 1.24). Check with `go version`. If you use a newer Go (e.g., 1.25), it still builds fine.
- Module downloads blocked: if your environment blocks `proxy.golang.org`, set a proxy or go direct: `go env -w GOPROXY=https://proxy.golang.org,direct`.
- Missing go.sum entries: if you see errors about missing checksums, run `go mod tidy` (or `go mod download`) to sync dependencies.
- CI/sandbox write permissions: `go build` needs writable caches. If the default cache path isn’t writable, set:
  - `GOCACHE=$(pwd)/.gocache` and `GOMODCACHE=$(pwd)/.gomodcache`
  - Example: `GOCACHE=.gocache GOMODCACHE=.gomodcache go build -o retroterm .`
- Optional static-ish builds: `CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o retroterm .` reduces size and removes debug info. Note: only use `CGO_ENABLED=0` if you don’t rely on C libraries.
- Corporate proxy: export `HTTPS_PROXY`/`HTTP_PROXY` if required by your network so `go mod download` can reach upstream.

## Vendoring (optional)

If you need offline or reproducible builds without network access, vendor your dependencies into the repo.

- Create/update `vendor/` from `go.mod`:
  
  ```bash
  go mod vendor
  ```

- Build using vendored deps (Go 1.14+ auto-detects `vendor/`; forcing is explicit):
  
  ```bash
  go build -mod=vendor -o retroterm .
  ```

- Keep vendor in sync when dependencies change:
  
  ```bash
  go mod tidy && go mod vendor
  ```

Notes:

- Vendoring removes network fetches for modules, but the Go build cache still needs a writable location. If needed:
  
  ```bash
  GOCACHE=.gocache GOMODCACHE=.gomodcache go build -mod=vendor -o retroterm .
  ```


## ZMODEM (Receive) Support via lrzsz

RetroTerm detects ZMODEM transfers and spawns the external `rz` program to receive files.

Install lrzsz:

- Debian/Ubuntu: `sudo apt-get install -y lrzsz`
- Fedora/RHEL: `sudo dnf install -y lrzsz`
- Arch: `sudo pacman -S lrzsz`
- macOS (Homebrew): `brew install lrzsz`

Verify `rz` is on PATH:

```bash
which rz
```

Usage notes:

- When a BBS initiates ZMODEM, RetroTerm launches `rz` and streams received files back to your browser for download.
- Files are received into a temporary directory that is cleaned up automatically.


## Tor (SOCKS5) Proxy Support

RetroTerm can route outbound BBS connections over Tor via a local SOCKS5 proxy.

1) Install Tor and start the service

- Debian/Ubuntu: `sudo apt-get install -y tor && sudo systemctl enable --now tor`  
- Fedora/RHEL: `sudo dnf install -y tor && sudo systemctl enable --now tor`  
- Arch: `sudo pacman -S tor && sudo systemctl enable --now tor`  
- macOS (Homebrew): `brew install tor && brew services start tor`

2) Ensure Tor exposes a local SOCKS port

On most installs, Tor listens on `127.0.0.1:9050` by default. To be explicit, add or confirm this in your Tor config:

- Linux/BSD: `/etc/tor/torrc`
- macOS (Homebrew): `/opt/homebrew/etc/tor/torrc` (ARM) or `/usr/local/etc/tor/torrc` (Intel)

Add/ensure:

```
SocksPort 9050
```

Restart Tor after changes, e.g. `sudo systemctl restart tor` or `brew services restart tor`.

3) Configure RetroTerm to use Tor

Edit `config.json`:

```json
{
  "server": {
    "port": 8080,
    "useCuratedList": true,
    "externalBaseURL": "https://your.domain"  
  },
  "proxy": {
    "enabled": true,
    "type": "tor",
    "host": "127.0.0.1",
    "port": 9050,
    "username": "",
    "password": ""
  }
}
```

Notes:

- Set `type` to `tor` for longer timeouts suitable for Tor; use `socks5` for a generic SOCKS proxy.
- Restart RetroTerm after editing `config.json`.


## Configuration Reference (brief)

- `server.port` — HTTP port (default 8080)
- `server.externalBaseURL` — optional; loosens WebSocket origin checks to this host
- `proxy.enabled` — enable/disable proxying
- `proxy.type` — `tor` or `socks5`
- `proxy.host`, `proxy.port` — proxy endpoint
- `proxy.username`, `proxy.password` — optional auth


## Troubleshooting

- ZMODEM: "failed to start rz" — ensure `lrzsz` is installed and `rz` is on PATH.
- Tor: connection timeouts — confirm Tor is running (`systemctl status tor`) and `SocksPort 9050` is enabled; check that `config.json` points to the correct host/port.
