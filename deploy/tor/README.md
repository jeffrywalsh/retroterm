# Tor client for outbound BBS connections

The `tor` service in dellcon's `/data/docker-compose.yml` dials boards for
RetroTerm. It's built from these files, which live on dellcon in
`/data/tor/image/`; its state (guards, consensus) persists in `/data/tor/data`.

It replaced `dperson/torproxy` on 2026-11-07, which was stuck on Tor 0.4.3.5
(2020) and had no congestion control or conflux. The Debian trixie package
gave Tor 0.4.9.15.

```
compose service:
  tor:
    build: ./tor/image
    image: data-tor:latest
    restart: unless-stopped
    volumes:
      - ./tor/data:/var/lib/tor
```

Pick up Tor updates (drops live BBS sessions; check `retroterm_sessions` first):

```
cd /data && sudo docker compose build --pull tor && sudo docker compose up -d --no-deps tor
```

RetroTerm sends a random SOCKS username per try, so Tor gives each try its
own circuit (`IsolateSOCKSAuth` is a default); see `proxy.go`.

Rollback: `/data/docker-compose.yml.bak.20261107-pretor049` (the old
`dperson/torproxy` service), then `sudo docker compose up -d --no-deps tor`.
