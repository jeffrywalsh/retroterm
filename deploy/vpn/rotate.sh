#!/bin/bash
# Move RetroTerm's PIA VPN to a new server, so outbound BBS connections get a
# new exit IP, and bring it back when it's down. Run by
# retroterm-vpn-rotate.timer every 15 minutes.
#
# 1. Rotate only when no BBS session is open (a reconnect drops them all) and
#    the last rotation was at least MIN_INTERVAL ago. If the tunnel is down,
#    skip the interval check: rotating is how it comes back.
# 2. Fetch PIA's current server list. PIA retires servers, and a stale list
#    left the VPN cycling through dead ones (2026-11-11).
# 3. Ping every address in SERVER_REGIONS and drop the ones that don't answer,
#    and the current one, from the list gluetun reads.
# 4. Restart the VPN (gluetun reads the list only at start; vpn-socks shares
#    its network, so it is recreated too). If the SOCKS proxy doesn't work
#    through the new tunnel, drop the address it tried and restart again.
#
# FORCE=1 skips the interval check. DRY_RUN=1 stops before touching the VPN
# and puts the list back. Never prints the PIA credentials.
set -u
MIN_INTERVAL=${MIN_INTERVAL:-5400}
TRIES=${TRIES:-4}
MIN_ALIVE=${MIN_ALIVE:-10}
STATE=/var/lib/retroterm-vpn-rotate
METRICS=http://192.168.1.64:9188/metrics
COMPOSE_DIR=/data
LIST=/data/vpn/gluetun/servers.json
CURL=curlimages/curl:8.10.1
GLUETUN=qmcgaw/gluetun:v3.40.0
PROVIDER="private internet access"
mkdir -p "$STATE"

exit_ip() {
  docker run --rm --network container:data-vpn-1 "$CURL" -s -m 10 \
    --socks5-hostname 127.0.0.1:1080 https://ipinfo.io/ip 2>/dev/null
}

old=$(exit_ip)
now=$(date +%s)
last=$(cat "$STATE/last" 2>/dev/null || echo 0)
if [ -n "$old" ] && [ "${FORCE:-0}" != 1 ] && [ $((now - last)) -lt "$MIN_INTERVAL" ]; then
  exit 0
fi

sessions=$(curl -s -m 5 "$METRICS" | awk '$1 == "retroterm_sessions" {print $2}')
if [ -z "$sessions" ]; then echo "skip: can't read retroterm_sessions"; exit 0; fi
if [ "$sessions" != 0 ]; then echo "skip: $sessions BBS session(s) open"; exit 0; fi
[ -z "$old" ] && echo "tunnel is down; rotating to bring it back"

# 2. Fresh list from PIA
cp "$LIST" "$STATE/servers.prev.json"
if ! docker run --rm -v "$(dirname "$LIST")":/gluetun "$GLUETUN" \
    update -enduser -providers "$PROVIDER" >"$STATE/update.log" 2>&1; then
  echo "couldn't refresh the list from PIA; using the last one"
  cp "$STATE/servers.prev.json" "$LIST"
fi
cp "$LIST" "$STATE/servers.full.json"

# 3. Ping every address in our regions
regions=$(sed -n 's/^ *SERVER_REGIONS: *//p' "$COMPOSE_DIR/docker-compose.yml" | head -1)
python3 - "$STATE/servers.full.json" "$regions" "$PROVIDER" >"$STATE/candidates" <<'EOF'
import json, sys
servers = json.load(open(sys.argv[1]))[sys.argv[3]]["servers"]
regions = {r.strip() for r in sys.argv[2].split(",")}
for s in servers:
    if s["region"] in regions and s.get("udp"):
        print("\n".join(s["ips"]))
EOF
xargs -P 64 -I{} sh -c 'ping -c1 -W2 -q {} >/dev/null 2>&1 && echo {}' \
  <"$STATE/candidates" | grep -vxF "${old:-none}" >"$STATE/alive"
total=$(wc -l <"$STATE/candidates")
alive=$(wc -l <"$STATE/alive")
echo "ping: $alive of $total addresses answered"
if [ "$alive" -lt "$MIN_ALIVE" ]; then
  echo "too few answered (is ping blocked?); leaving the VPN as it is"
  exit 1
fi

# Write the list gluetun reads: our regions' servers, live addresses only
prune() {
  python3 - "$STATE/servers.full.json" "$STATE/alive" "$regions" "$PROVIDER" "$LIST" <<'EOF'
import json, os, sys
full, alive_file, regions, provider, out = sys.argv[1:6]
alive = set(open(alive_file).read().split())
regions = {r.strip() for r in regions.split(",")}
data = json.load(open(full))
kept = []
for s in data[provider]["servers"]:
    if s["region"] in regions:
        s["ips"] = [ip for ip in s["ips"] if ip in alive]
        if not s["ips"]:
            continue
    kept.append(s)
data[provider]["servers"] = kept
tmp = out + ".tmp"
json.dump(data, open(tmp, "w"))
os.replace(tmp, out)
EOF
}

if [ "${DRY_RUN:-0}" = 1 ]; then
  prune
  python3 -c 'import json,sys; s=json.load(open(sys.argv[1]))[sys.argv[2]]["servers"]; print("dry run: pruned list has", len(s), "servers")' "$LIST" "$PROVIDER"
  cp "$STATE/servers.prev.json" "$LIST"
  echo "dry run: list restored, VPN untouched"
  exit 0
fi

# 4. Restart onto a live server; drop any address that doesn't work
new=""
for try in $(seq 1 "$TRIES"); do
  [ -s "$STATE/alive" ] || { echo "no live addresses left"; break; }
  prune
  since=$(date -u +%Y-%m-%dT%H:%M:%SZ)
  (cd "$COMPOSE_DIR" && docker compose restart vpn >/dev/null 2>&1 &&
    docker compose up -d --force-recreate --no-deps vpn-socks >/dev/null 2>&1)
  for i in $(seq 1 12); do
    sleep 5
    new=$(exit_ip)
    [ -n "$new" ] && break
  done
  [ -n "$new" ] && break
  tried=$(docker logs --since "$since" data-vpn-1 2>&1 |
    sed -n 's/.*link remote: \[AF_INET\]\([0-9.]*\):.*/\1/p' | sort -u)
  echo "try $try: no tunnel after 60s (tried: ${tried:-unknown})"
  for ip in $tried; do grep -vxF "$ip" "$STATE/alive" >"$STATE/alive.new"; mv "$STATE/alive.new" "$STATE/alive"; done
done

date +%s >"$STATE/last"
if [ -z "$new" ]; then
  echo "rotate FAILED: ${old:-down} -> no working server after $TRIES tries"
  exit 1
fi
echo "rotated: ${old:-down} -> $new"
