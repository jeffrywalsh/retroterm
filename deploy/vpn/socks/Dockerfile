# SOCKS5 proxy for RetroTerm outbound connections. It runs in the vpn
# (gluetun) container network namespace, so everything it dials leaves
# through PIA. Reachable on the Compose network as vpn:1080.
FROM debian:trixie-slim
RUN apt-get update \
 && apt-get install -y --no-install-recommends microsocks \
 && rm -rf /var/lib/apt/lists/*
USER nobody
EXPOSE 1080
CMD ["microsocks", "-i", "0.0.0.0", "-p", "1080"]
