# Outbound VPN on dellcon

retroterm.com reaches BBSes through PIA, not the home IP. Production
`config.json` sets the proxy to `socks5` `vpn:1080`.

- `vpn`: gluetun (`qmcgaw/gluetun:v3.40.0`), PIA OpenVPN, servers limited to
  `SERVER_REGIONS` (14 US regions). Credentials are in root-only
  `/data/vpn/pia.env`; never print it. The server list is
  `/data/vpn/gluetun/servers.json`.
- `vpn-socks`: microsocks (`socks/Dockerfile`), in the `vpn` network namespace
  (`network_mode: service:vpn`). Restarting `vpn` alone breaks it: always
  follow with `docker compose up -d --force-recreate --no-deps vpn-socks`.

## Rotation and recovery

`rotate.sh` (installed as `/data/vpn/rotate.sh`, root, mode 700) runs from
`retroterm-vpn-rotate.timer` every 15 minutes. When no BBS session is open and
90 minutes have passed, or at once if the tunnel is down, it:

1. fetches PIA's current server list (gluetun's updater),
2. pings every address in `SERVER_REGIONS` and drops those that don't answer,
   and the current exit, from the list gluetun reads,
3. restarts the VPN and checks the SOCKS proxy through it, dropping the
   address and retrying (up to 4 times) if it doesn't work.

State and the full and previous lists are in `/var/lib/retroterm-vpn-rotate`.

```sh
sudo FORCE=1 /data/vpn/rotate.sh            # rotate now
journalctl -u retroterm-vpn-rotate -n 20    # what it did
```

Install:

```sh
sudo install -m 700 rotate.sh /data/vpn/rotate.sh
sudo cp retroterm-vpn-rotate.service retroterm-vpn-rotate.timer /etc/systemd/system/
sudo systemctl daemon-reload && sudo systemctl enable --now retroterm-vpn-rotate.timer
```

Back to Tor: restore `~/backup/config.json.tor-20261107` over production
`config.json`, then `docker compose restart retroterm`.
