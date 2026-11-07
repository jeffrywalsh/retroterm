# Tor client for RetroTerm outbound connections (replaces dperson/torproxy,
# which was stuck on Tor 0.4.3.5 from 2020). Rebuild to pick up Tor updates:
#   cd /data && sudo docker compose build --pull tor && sudo docker compose up -d tor
FROM debian:trixie-slim
RUN apt-get update \
 && apt-get install -y --no-install-recommends tor ca-certificates \
 && rm -rf /var/lib/apt/lists/*
COPY torrc /etc/tor/torrc
RUN mkdir -p /var/lib/tor && chown debian-tor:debian-tor /var/lib/tor && chmod 700 /var/lib/tor
USER debian-tor
EXPOSE 9050
HEALTHCHECK --interval=60s --timeout=10s --start-period=60s \
  CMD tor-resolve -5 check.torproject.org 127.0.0.1:9050 >/dev/null || exit 1
CMD ["tor", "-f", "/etc/tor/torrc"]
