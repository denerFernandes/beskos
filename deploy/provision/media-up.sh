#!/usr/bin/env bash
# Video/recording pipeline on the "media" node (VPS-3). Outside the CapRover overlay: LiveKit and
# Egress need host networking (direct WebRTC UDP), so they run as plain `docker run` containers.
#   Local Redis  -- coordinates LiveKit <-> Egress (recording).
#   LiveKit       -- WebRTC SFU (signaling on 7880, media on 7881/tcp + 50000-60000/udp).
#   Egress        -- records the room (RoomComposite) and uploads the MP4 to R2 (creds come
#                    per request from the API).
#   Caddy         -- terminates TLS on meet.<domain> (443) and proxies wss -> 127.0.0.1:7880.
# Idempotent: recreates the containers on every run. Configs are generated from the environment
# (no secrets in git).
set -euo pipefail

: "${LIVEKIT_API_KEY:?}"; : "${LIVEKIT_API_SECRET:?}"
MEET_HOST="${MEET_HOST:-meet.beskos.com}"
PUBLIC_IP="${PUBLIC_IP:-$(curl -s https://api.ipify.org || true)}"
API_WEBHOOK_URL="${API_WEBHOOK_URL:-https://app.beskos.com/meet/webhook}"
DIR=/opt/beskos/livekit
# Server updated to v1.13 (25/07/2026): the livekit-client 2.20.x client uses the /rtc/v1 signaling
# path, which v1.8 did not have (it returned 404 -> "negotiation timed out", Meet would not connect).
LK_IMG='livekit/livekit-server:v1.13'
EG_IMG='livekit/egress:v1.13'  # in sync with the server (25/07/2026)

sudo mkdir -p "$DIR"

# --- livekit.yaml ---
sudo tee "$DIR/livekit.yaml" >/dev/null <<YAML
port: 7880
redis:
  address: 127.0.0.1:6379
room:
  empty_timeout: 120
  departure_timeout: 20
rtc:
  tcp_port: 7881
  port_range_start: 50000
  port_range_end: 60000
  use_external_ip: true
  node_ip: ${PUBLIC_IP}
keys:
  ${LIVEKIT_API_KEY}: ${LIVEKIT_API_SECRET}
webhook:
  api_key: ${LIVEKIT_API_KEY}
  urls:
    - ${API_WEBHOOK_URL}
logging:
  level: info
YAML

# --- egress.yaml (S3/R2 comes per request from the API) ---
sudo tee "$DIR/egress.yaml" >/dev/null <<YAML
redis:
  address: 127.0.0.1:6379
api_key: ${LIVEKIT_API_KEY}
api_secret: ${LIVEKIT_API_SECRET}
ws_url: ws://127.0.0.1:7880
insecure: false
log_level: info
YAML

# --- Caddyfile (automatic Let's Encrypt TLS for wss) ---
sudo tee "$DIR/Caddyfile" >/dev/null <<CADDY
${MEET_HOST} {
  reverse_proxy 127.0.0.1:7880
}
CADDY

echo "== redis (local, só loopback) =="
sudo docker rm -f beskos-lk-redis 2>/dev/null || true
sudo docker run -d --restart=always --name beskos-lk-redis \
  -p 127.0.0.1:6379:6379 redis:7-alpine >/dev/null

echo "== livekit (host network) =="
sudo docker rm -f beskos-livekit 2>/dev/null || true
sudo docker run -d --restart=always --name beskos-livekit --network host \
  -v "$DIR/livekit.yaml:/etc/livekit.yaml:ro" \
  "$LK_IMG" --config /etc/livekit.yaml >/dev/null

echo "== egress (host network) =="
sudo docker rm -f beskos-egress 2>/dev/null || true
sudo docker run -d --restart=always --name beskos-egress --network host \
  -e EGRESS_CONFIG_FILE=/etc/egress.yaml \
  -v "$DIR/egress.yaml:/etc/egress.yaml:ro" \
  "$EG_IMG" >/dev/null

echo "== caddy (TLS 80/443 -> livekit 7880) =="
# Host network: it must reach LiveKit at 127.0.0.1:7880 (also host network) and bind 80/443.
sudo docker rm -f beskos-caddy 2>/dev/null || true
sudo docker run -d --restart=always --name beskos-caddy --network host \
  -v "$DIR/Caddyfile:/etc/caddy/Caddyfile:ro" \
  -v beskos_caddy_data:/data -v beskos_caddy_config:/config \
  caddy:2 >/dev/null

sleep 4
echo "== estado =="
sudo docker ps --filter name=beskos- --format '  {{.Names}}  {{.Status}}'
echo "== livekit health (7880) =="
curl -s -o /dev/null -w '  http://localhost:7880 -> %{http_code}\n' http://localhost:7880 || true
