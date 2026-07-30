#!/bin/bash
# Agenda: 02:00, 10:00 e 18:00 UTC. Corre também UMA VEZ ao arrancar — assim um erro de
# configuração aparece no minuto do deploy e não às duas da manhã, e um serviço acabado de
# instalar já deixa um backup atrás de si.

set -euo pipefail

# O rclone lê a configuração do ambiente (RCLONE_CONFIG_<REMOTO>_*), o que evita ter um ficheiro
# com credenciais dentro da imagem.
export RCLONE_CONFIG_R2_TYPE=s3
export RCLONE_CONFIG_R2_PROVIDER=Cloudflare
export RCLONE_CONFIG_R2_ENDPOINT="${R2_ENDPOINT:?R2_ENDPOINT em falta}"
export RCLONE_CONFIG_R2_ACCESS_KEY_ID="${R2_ACCESS_KEY:?R2_ACCESS_KEY em falta}"
export RCLONE_CONFIG_R2_SECRET_ACCESS_KEY="${R2_SECRET_KEY:?R2_SECRET_KEY em falta}"
export RCLONE_CONFIG_R2_REGION=auto
export RCLONE_CONFIG_R2_NO_CHECK_BUCKET=true

# Segundo remoto: o balde da APLICAÇÃO, de onde se lê o prefixo do correio. É só de leitura para
# este serviço — o backup nunca escreve no balde de produção, e credenciais separadas tornam isso
# verificável em vez de prometido.
if [ -n "${BLOB_BUCKET:-}" ]; then
  export RCLONE_CONFIG_BLOBS_TYPE=s3
  export RCLONE_CONFIG_BLOBS_PROVIDER=Cloudflare
  export RCLONE_CONFIG_BLOBS_ENDPOINT="${BLOB_ENDPOINT:?BLOB_ENDPOINT em falta}"
  export RCLONE_CONFIG_BLOBS_ACCESS_KEY_ID="${BLOB_ACCESS_KEY:?BLOB_ACCESS_KEY em falta}"
  export RCLONE_CONFIG_BLOBS_SECRET_ACCESS_KEY="${BLOB_SECRET_KEY:?BLOB_SECRET_KEY em falta}"
  export RCLONE_CONFIG_BLOBS_REGION=auto
  export RCLONE_CONFIG_BLOBS_NO_CHECK_BUCKET=true
fi

echo "backup: serviço iniciado — 02:00, 10:00 e 18:00 UTC (+ uma corrida agora)"

/usr/local/bin/backup.sh || echo "backup: a corrida inicial falhou — ver acima"

# O crond do busybox não herda o ambiente, por isso guarda-se num ficheiro que a tarefa carrega.
env | grep -E '^(DATABASE_URL|BACKUP_BUCKET|BACKUP_DATABASES|BACKUP_AGE_RECIPIENT|BLOB_BUCKET|BLOB_PREFIX|BLOB_MAX_KIB|RETENTION_DAYS|HEARTBEAT_URL|RCLONE_CONFIG_)' \
  | sed 's/^/export /; s/=/="/; s/$/"/' > /etc/backup.env

mkdir -p /etc/crontabs
cat > /etc/crontabs/root <<'CRON'
0 2,10,18 * * * . /etc/backup.env && /usr/local/bin/backup.sh
CRON

exec crond -f -l 8
