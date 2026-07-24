#!/usr/bin/env bash
# ─────────────────────────────────────────────────────────────────────────────
# Beskos · Fase 1 — provisiona UM nó (hardening + Docker). Idempotente: correr as
# vezes que forem precisas. Alvo: Ubuntu 22.04/24.04 LTS acabada de instalar.
#
# Correr COMO ROOT no nó (ex.: ssh root@<IP> 'bash -s' < setup-node.sh, com env).
#
# Variáveis (env):
#   ROLE          data | app | media        (define as regras de firewall)     [obrigatório]
#   ADMIN_USER    utilizador sudo a criar    (default: deploy)
#   SSH_PUBKEY    chave pública a autorizar  [obrigatório na 1ª vez]
#   NODE_IPS      IPs dos 3 nós, separados por espaço, para abrir as portas do
#                 Swarm SÓ entre eles         (ex.: "1.2.3.4 1.2.3.5 1.2.3.6")   [obrigatório]
#
# ⚠️ No fim, o login de ROOT e por PASSWORD ficam DESLIGADOS. Garante que a
#    SSH_PUBKEY funciona ANTES — reconecta como $ADMIN_USER noutra sessão.
# ─────────────────────────────────────────────────────────────────────────────
set -euo pipefail

ROLE="${ROLE:?define ROLE=data|app|media}"
ADMIN_USER="${ADMIN_USER:-deploy}"
NODE_IPS="${NODE_IPS:?define NODE_IPS='ip1 ip2 ip3'}"
SSH_PUBKEY="${SSH_PUBKEY:-}"

log() { echo -e "\n\033[1;36m▶ $*\033[0m"; }

[[ $EUID -eq 0 ]] || { echo "correr como root"; exit 1; }
case "$ROLE" in data|app|media) ;; *) echo "ROLE inválido: $ROLE"; exit 1 ;; esac

export DEBIAN_FRONTEND=noninteractive

log "1/8 · pacotes base"
apt-get update -qq
apt-get install -yqq ca-certificates curl gnupg ufw fail2ban unattended-upgrades \
  chrony jq git rsync htop

log "2/8 · utilizador sudo '$ADMIN_USER' + chave SSH"
if ! id "$ADMIN_USER" &>/dev/null; then
  adduser --disabled-password --gecos "" "$ADMIN_USER"
  usermod -aG sudo "$ADMIN_USER"
  echo "$ADMIN_USER ALL=(ALL) NOPASSWD:ALL" > /etc/sudoers.d/90-$ADMIN_USER
fi
if [[ -n "$SSH_PUBKEY" ]]; then
  install -d -m 700 -o "$ADMIN_USER" -g "$ADMIN_USER" "/home/$ADMIN_USER/.ssh"
  KEYS="/home/$ADMIN_USER/.ssh/authorized_keys"
  touch "$KEYS"; grep -qxF "$SSH_PUBKEY" "$KEYS" || echo "$SSH_PUBKEY" >> "$KEYS"
  chown "$ADMIN_USER:$ADMIN_USER" "$KEYS"; chmod 600 "$KEYS"
fi

log "3/8 · SSH: sem root, sem password (só chave)"
install -d /etc/ssh/sshd_config.d
cat > /etc/ssh/sshd_config.d/10-beskos.conf <<'EOF'
PermitRootLogin no
PasswordAuthentication no
PubkeyAuthentication yes
KbdInteractiveAuthentication no
X11Forwarding no
MaxAuthTries 3
EOF
systemctl reload ssh 2>/dev/null || systemctl reload sshd

log "4/8 · atualizações de segurança automáticas"
cat > /etc/apt/apt.conf.d/20auto-upgrades <<'EOF'
APT::Periodic::Update-Package-Lists "1";
APT::Periodic::Unattended-Upgrade "1";
EOF

log "5/8 · fail2ban (sshd)"
cat > /etc/fail2ban/jail.d/beskos.conf <<'EOF'
[sshd]
enabled = true
maxretry = 4
bantime = 1h
EOF
systemctl enable --now fail2ban

log "6/8 · Docker CE 27.x (FIXADO — o CapRover não fala a API do Docker ≥28)"
# O CapRover usa um cliente Docker antigo (API 1.38); o Docker 28+ recusa-o. Fixamos o 27.5.1 e
# travamos updates. (Ubuntu 24.04 / noble.)
DOCKER_VER='5:27.5.1-1~ubuntu.24.04~noble'
if ! command -v docker &>/dev/null; then
  curl -fsSL https://get.docker.com | sh -s -- --version 27.5 || curl -fsSL https://get.docker.com | sh
fi
apt-get install -y --allow-downgrades \
  docker-ce="$DOCKER_VER" docker-ce-cli="$DOCKER_VER" containerd.io docker-buildx-plugin docker-compose-plugin || true
apt-mark hold docker-ce docker-ce-cli >/dev/null 2>&1 || true
usermod -aG docker "$ADMIN_USER" || true
systemctl enable --now docker
# journald com limite (evita disco cheio por logs)
mkdir -p /etc/docker
echo '{ "log-driver":"json-file", "log-opts":{"max-size":"20m","max-file":"5"} }' > /etc/docker/daemon.json
systemctl restart docker

log "7/8 · sysctl (memória/rede)"
cat > /etc/sysctl.d/90-beskos.conf <<'EOF'
vm.swappiness = 10
vm.overcommit_memory = 1
net.core.somaxconn = 1024
EOF
sysctl --system >/dev/null

log "8/8 · firewall (ufw) — papel: $ROLE"
ufw --force reset >/dev/null
ufw default deny incoming
ufw default allow outgoing
ufw allow 22/tcp comment 'ssh'
# portas do Swarm SÓ entre os nós do cluster
for ip in $NODE_IPS; do
  ufw allow from "$ip" to any port 2377 proto tcp comment 'swarm-mgmt'
  ufw allow from "$ip" to any port 7946           comment 'swarm-net'
  ufw allow from "$ip" to any port 4789 proto udp comment 'swarm-overlay'
done
case "$ROLE" in
  data)  # CapRover ingress + mail (Stalwart)
    ufw allow 80,443/tcp comment 'http/ingress'
    ufw allow 25,587,465,143,993/tcp comment 'mail'
    # 3000 (painel CapRover) NÃO é aberto — acede-se por túnel SSH até fecharmos o setup
    ;;
  app)   # tráfego web chega pela overlay a partir do ingress; nada direto
    : ;;
  media) # LiveKit + TURN (WebRTC precisa de UDP direto)
    ufw allow 7880,7881/tcp comment 'livekit'
    ufw allow 50000:60000/udp comment 'livekit-rtc'
    ufw allow 3478/udp comment 'turn'
    ;;
esac
ufw --force enable
ufw status verbose | sed 's/^/    /'

log "✅ nó '$ROLE' pronto. Reconecta como: ssh $ADMIN_USER@<este-IP>"
echo "   (root e password já estão desligados)"
