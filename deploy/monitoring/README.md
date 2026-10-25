# Metrics on dellcon's shared Grafana

retroterm.com reports to the shared Prometheus and Grafana on dellcon
(`/data/monitoring`, Grafana at http://192.168.1.64:3000, anonymous viewing
on the LAN). The stack's own README there says how projects plug in.

- **Metrics:** the server runs with `METRICS_ADDR=:9188`, and the `retroterm`
  service in `/data/docker-compose.yml` publishes it on the LAN address only
  (`"192.168.1.64:9188:9188"`). The public port has no `/metrics`, so the
  tunnel never exposes them. Raw numbers: http://192.168.1.64:9188/metrics.
- **`retroterm.yml`** is the scrape job, installed as
  `/data/monitoring/scrape.d/retroterm.yml`.
- **`retroterm.json`** is the "RetroTerm" dashboard, installed in
  `/data/monitoring/dashboards/retroterm/`. Grafana reloads it within 30 s.

To update them:

```sh
rsync -a deploy/monitoring/retroterm.yml jeffryw@192.168.1.64:/data/monitoring/scrape.d/
rsync -a deploy/monitoring/retroterm.json jeffryw@192.168.1.64:/data/monitoring/dashboards/retroterm/
ssh jeffryw@192.168.1.64 sudo docker kill -s HUP monitoring-prometheus   # after a scrape change
```

Per-board counters start at 1 on a board's first visit, which `increase()`
can't see, so the board panels add a new series' first value back.
