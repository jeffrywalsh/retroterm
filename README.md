# RetroTerm

RetroTerm is a minimal, stateless web app that serves a curated BBS directory and connects to remote BBSes over telnet from your browser. It runs at [retroterm.com](https://retroterm.com).

- Decodes CP437 (ANSI), CP866 (Cyrillic), Big5 and GBK (Chinese), ISO-8859-1 (Amiga), UTF-8, PETSCII (Commodore) and ATASCII (Atari)
- Picks each board's encoding from the directory, so users never have to choose one
- ZMODEM receive via the external `lrzsz` tools (uses `rz`)
- Outbound connections over Tor via a SOCKS5 proxy

## Build & Run

Prerequisites:

- Go (version matching `go.mod`, currently `go 1.24`)

Build and run from the repository root:

```bash
go build -o retroterm .
./retroterm
# or for quick testing
go run .
```

The server listens on the port from `config.json` (default 8080) and serves the UI from `./static`. It reads `bbs.csv` and `charset_bbs.csv` from the working directory.

Release builds stamp the date shown as "Updated …" in the page footer:

```bash
CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.buildDate=$(date -u +%Y-%m-%d)" -o retroterm .
```

Without it, the footer shows the executable's modification date.

### Fonts

The terminal draws PETSCII, ATASCII and box-drawing characters with GNU Unifont, which isn't in the repository. Download the two files `static/style.css` loads:

```bash
mkdir -p static/fonts && cd static/fonts
curl -fLO https://ftpmirror.gnu.org/unifont/unifont-17.0.01/unifont-17.0.01.otf
curl -fLO https://ftpmirror.gnu.org/unifont/unifont-17.0.01/unifont_upper-17.0.01.otf
```

Without them, the browser falls back to a system font and the graphics characters won't line up.

## BBS Directory and Encodings

- `bbs.csv` is the directory and the connection allowlist: the server only connects to hosts listed there. Columns: `Name`, `Telnet` (`host` or `host:port`, default port 23), `Location`, `Software`. It's generated from the [Telnet BBS Guide](https://www.telnetbbsguide.com/) list.
- Each board's encoding is guessed from its software (Color 64, Image BBS, C-Base and VIC-BBS are PETSCII; Atari BBSes are ATASCII; everything else is CP437).
- `charset_bbs.csv` overrides the guess, one `name, charset` row per board. A row matches the board's name (case-insensitive) or a substring of its host. A third column (a terminal size such as `40x25`) is read but not used yet. Every override in the file was checked against bytes captured from the board.

Charset names: `CP437`, `CP866`, `BIG5`, `GBK`, `ISO-8859-1`, `UTF-8`, `PETSCIIU` (upper case/graphics), `PETSCIIL` (lower/upper case), `ATASCII`.

## Tests

```bash
go test -race .
```

`decoders_test.go` holds fast offline regression tests for the decoders, the telnet parser and the session plumbing.

`bbs_probe_test.go` is an opt-in probe that connects to every board in `bbs.csv`, saves what each one sends, and reports boards whose configured encoding doesn't match their bytes:

```bash
BBS_PROBE=1 go test -run TestProbeDirectory -timeout 90m -v .   # collect (about 20 s per board, 24 at a time)
BBS_PROBE_DIR=<dir> go test -run TestProbeReport -v .           # re-analyze saved captures offline
```

Knobs: `BBS_PROBE_OUT` (output directory), `BBS_PROBE_FILTER` (comma-separated name/host terms), `BBS_PROBE_WORKERS`, `BBS_PROBE_SECONDS`, `BBS_PROBE_PROXY=1` (go through the configured proxy instead of connecting directly).

## Go Build Gotchas

- Go toolchain version: ensure Go >= the version in `go.mod` (currently 1.24). Check with `go version`. Newer versions build fine.
- Module downloads blocked: if your environment blocks `proxy.golang.org`, set a proxy or go direct: `go env -w GOPROXY=https://proxy.golang.org,direct`.
- Missing go.sum entries: if you see errors about missing checksums, run `go mod tidy` (or `go mod download`) to sync dependencies.
- CI/sandbox write permissions: `go build` needs writable caches. If the default cache path isn't writable, set `GOCACHE=$(pwd)/.gocache` and `GOMODCACHE=$(pwd)/.gomodcache`.
- Offline builds: `go mod vendor`, then `go build -mod=vendor -o retroterm .`. Rerun `go mod tidy && go mod vendor` when dependencies change.
- Corporate proxy: export `HTTPS_PROXY`/`HTTP_PROXY` if required by your network so `go mod download` can reach upstream.

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

RetroTerm can route outbound BBS connections over Tor via a SOCKS5 proxy. Some boards take 30–60 seconds to answer through Tor.

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
- `server.externalBaseURL` — optional; WebSocket connections are also accepted from pages served at this URL's host
- `proxy.enabled` — enable/disable proxying
- `proxy.type` — `tor` or `socks5`
- `proxy.host`, `proxy.port` — proxy endpoint
- `proxy.username`, `proxy.password` — optional auth

Environment variables:

- `BBS_IMPORT_TOKEN` — enables `POST /api/import-bbs-guide`, which regenerates `bbs.csv` from the Telnet BBS Guide's text list; callers send `Authorization: Bearer <token>`. Without it the endpoint returns 404.
- `TERM_ANSWERS=true` — answer the board's terminal queries (DA, cursor position) for ANSI sessions. PETSCII and ATASCII sessions never answer, like a real C64 or Atari.
- `CURSOR_TRACK=true` — track the cursor so cursor-position answers are accurate; `CPR_REPLY=true` answers `1;1` instead when tracking is off.
- `ANSI_NORMALIZE=false` — turn off the ANSI pre-processor (it repairs split and malformed escape sequences).
- `METRICS_ADDR` — serve Prometheus metrics at `http://<addr>/metrics` on their own listener, for example `METRICS_ADDR=:9188`. They aren't on the public port. They count browsers, sessions, connections per board, connect and session times, bytes, and ZMODEM downloads.

## Deployment

retroterm.com runs in Docker on a home server and is reachable only through a Cloudflare Tunnel. The tunnel kit, its helper and the rollback steps are in [`deploy/tunnel/README.md`](deploy/tunnel/README.md).

Cloudflare caches `.js` and `.css` for 4 hours, so bump the `?v=` on each changed file's tag in `static/index.html` when you change it.

## Troubleshooting

- ZMODEM: "failed to start rz" — ensure `lrzsz` is installed and `rz` is on PATH.
- Tor: connection timeouts — confirm Tor is running (`systemctl status tor`) and `SocksPort 9050` is enabled; check that `config.json` points to the correct host/port.
- Graphics characters misaligned — download the Unifont files (see Fonts).
