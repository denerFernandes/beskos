#!/usr/bin/env bash
# ─────────────────────────────────────────────────────────────────────────────
# Beskos -- starts CapRover on the LEADER node (VPS-1). Initialises Docker
# Swarm as manager. Run AFTER setup-node.sh, as the sudo user.
#
# Usage:  LEADER_IP=<public IPv4 of VPS-1> ./caprover-leader.sh
#
# Then: open the panel through an SSH TUNNEL (do not expose 3000):
#   ssh -L 3000:localhost:3000 deploy@<LEADER_IP>
#   -> http://localhost:3000   (initial password: captain42 -- change it at once)
#   -> set the root domain: captain.beskos.com  (A record proxied in Cloudflare)
#   -> enable HTTPS (Let's Encrypt) and force it; close 3000 to the public at the end.
# ─────────────────────────────────────────────────────────────────────────────
set -euo pipefail
LEADER_IP="${LEADER_IP:?define LEADER_IP=<IPv4 do VPS-1>}"

command -v docker &>/dev/null || { echo "Docker em falta — corre o setup-node.sh primeiro"; exit 1; }

# The CapRover installer runs a self-test on 3000 (and uses 996 for internal HTTPS). Open them during
# setup; CLOSE 3000 after enabling HTTPS (the panel moves to captain.<domain>:443).
sudo ufw allow 3000/tcp comment 'caprover-setup' >/dev/null 2>&1 || true
sudo ufw allow 996/tcp  comment 'caprover-https' >/dev/null 2>&1 || true

echo "▶ a instalar CapRover (manager em $LEADER_IP)…"
sudo docker run -d --restart=always \
  -e ACCEPTED_TERMS=true \
  -e MAIN_NODE_IP_ADDRESS="$LEADER_IP" \
  -p 80:80 -p 443:443 -p 3000:3000 \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v /captain:/captain \
  --name caprover \
  caprover/caprover

cat <<EOF

✅ CapRover a arrancar. Próximos passos:
 1) Túnel:  ssh -L 3000:localhost:3000 deploy@$LEADER_IP   →  http://localhost:3000
 2) Trocar a password (default: captain42).
 3) Root domain: captain.beskos.com  (criar A record no Cloudflare → $LEADER_IP, proxied).
 4) Enable HTTPS + Force HTTPS.
 5) Cluster → adicionar VPS-2 e VPS-3 (o painel dá o 'docker swarm join'; correr lá).
 6) Etiquetar os nós (no leader):
      docker node ls
      docker node update --label-add beskos.role=data  <id-VPS-1>
      docker node update --label-add beskos.role=app    <id-VPS-2>
      docker node update --label-add beskos.role=media  <id-VPS-3>
 7) Rede overlay encriptada para os serviços à-mão (mail/vídeo):
      docker network create --driver overlay --attachable --opt encrypted beskos_net
EOF
