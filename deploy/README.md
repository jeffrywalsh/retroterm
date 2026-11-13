# Deployment samples

Examples for running RetroTerm, not a description of any real server. Fill in
your own hosts and paths.

## Dialing boards through a proxy

RetroTerm can reach boards through a SOCKS5 proxy, so they see the proxy's
address instead of the server's. Set it in `config.json`:

```json
"proxy": { "enabled": true, "type": "socks5", "host": "vpn", "port": 1080 }
```

With `"type": "tor"`, each connect gets its own Tor circuit, and a slow or
failed connect is retried up to 3 times on a new one.

- [`tor/`](tor/): a small Tor client image. Some boards block Tor exits.
- [`vpn/`](vpn/): a commercial VPN through [gluetun](https://github.com/qdm12/gluetun),
  with [microsocks](https://github.com/rofl0r/microsocks) sharing its network
  so RetroTerm can dial through it, plus a script that moves the VPN to a new
  server when no one is connected and skips servers that are down.

## Metrics

Set `METRICS_ADDR=:9188` and point Prometheus at it
([`monitoring/prometheus.example.yml`](monitoring/prometheus.example.yml)).
[`monitoring/retroterm.json`](monitoring/retroterm.json) is a Grafana
dashboard for those metrics. Keep the metrics port off the public internet.
