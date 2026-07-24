#!/usr/bin/env bash
# Pipeline de vídeo/gravação no nó "media" (VPS-3). Fora do overlay do CapRover: LiveKit e Egress
# precisam de rede host (WebRTC UDP direto), por isso correm como containers `docker run` normais.
#   Redis local  — coordena LiveKit <-> Egress (gravação).
#   LiveKit       — SFU WebRTC (sinalização em 7880, media em 7881/tcp + 50000-60000/udp).
#   Egress        — grava a sala (RoomComposite) e sobe o MP4 para o R2 (creds vêm por-requisição da API).
#   Caddy         — termina TLS em meet.<dominio> (443) e faz proxy wss -> 127.0.0.1:7880.
# Idempotente: recria os containers a cada execução. Configs geradas a partir do ambiente (sem segredos no git).
set -euo pipefail

: "${LIVEKIT_API_KEY:?}"; : "${LIVEKIT_API_SECRET:?}"
MEET_HOST="${MEET_HOST:-meet.beskos.com}"
PUBLIC_IP="${PUBLIC_IP:-$(curl -s https://api.ipify.org || true)}"
API_WEBHOOK_URL="${API_WEBHOOK_URL:-https://app.beskos.com/meet/webhook}"
DIR=/opt/beskos/livekit
LK_IMG='livekit/livekit-server:v1.8'
EG_IMG='livekit/egress:v1.8'

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

# --- egress.yaml (S3/R2 vai por-requisição a partir da API) ---
sudo tee "$DIR/egress.yaml" >/dev/null <<YAML
redis:
  address: 127.0.0.1:6379
api_key: ${LIVEKIT_API_KEY}
api_secret: ${LIVEKIT_API_SECRET}
ws_url: ws://127.0.0.1:7880
insecure: false
log_level: info
YAML

# --- Caddyfile (TLS automático Let's Encrypt para wss) ---
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
# Rede host: precisa de alcançar o LiveKit em 127.0.0.1:7880 (também host network) e ligar 80/443.
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
