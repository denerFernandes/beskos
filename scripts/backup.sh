#!/usr/bin/env bash
# Backup da stack de produção do Beskos (compose em deploy/).
#
# Faz um backup consistente dos três datastores com estado:
#   - Postgres  → pg_dump lógico (restauro previsível, independente da versão do binário)
#   - MinIO     → tar do volume de objetos (dedup por hash: ficheiros imutáveis, cópia a quente segura)
#   - Stalwart  → tar do volume (caixas de correio + config)
# Grava tudo em backups/<timestamp>/ e aplica retenção (RETENTION_DAYS, default 7).
#
# Uso:   scripts/backup.sh [destino]
#   destino          diretório-raiz dos backups (default: ./backups)
#   RETENTION_DAYS   dias a manter (default 7); 0 = não apagar nada
#   COMPOSE_FILE     compose a usar (default: deploy/docker-compose.yml)
#
# Restaurar:  scripts/restore.sh <destino>/<timestamp>
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
COMPOSE_FILE="${COMPOSE_FILE:-$ROOT/deploy/docker-compose.yml}"
ENV_FILE="${ENV_FILE:-$ROOT/deploy/.env}"
DEST_ROOT="${1:-$ROOT/backups}"
RETENTION_DAYS="${RETENTION_DAYS:-7}"

# Nomes reais dos recursos do compose (projeto "mailin" → prefixo nos volumes).
PG_CONTAINER="mailin-postgres"
PG_VOL="mailin_postgres_data"   # não usado (Postgres via pg_dump), mantido para referência
MINIO_VOL="mailin_minio_data"
STALWART_VOL="mailin_stalwart_data"

[[ -f "$ENV_FILE" ]] || { echo "ERRO: $ENV_FILE não encontrado (copie de deploy/.env.example)"; exit 1; }
# Lê POSTGRES_USER/POSTGRES_DB do .env sem exportar o resto.
PG_USER="$(grep -E '^POSTGRES_USER=' "$ENV_FILE" | tail -1 | cut -d= -f2-)"
PG_DB="$(grep -E '^POSTGRES_DB=' "$ENV_FILE" | tail -1 | cut -d= -f2-)"
: "${PG_USER:?POSTGRES_USER ausente no .env}" "${PG_DB:?POSTGRES_DB ausente no .env}"

TS="$(date +%Y%m%d-%H%M%S)"
OUT="$DEST_ROOT/$TS"
mkdir -p "$OUT"
echo "== Backup Beskos → $OUT =="

# 1) Postgres: dump lógico comprimido. --clean para o restauro poder correr sobre uma base existente.
echo "[1/3] Postgres (pg_dump $PG_DB)…"
docker exec "$PG_CONTAINER" pg_dump -U "$PG_USER" --clean --if-exists "$PG_DB" | gzip > "$OUT/postgres.sql.gz"

# 2) MinIO: tar do volume de objetos, via container alpine efémero que monta o volume.
echo "[2/3] MinIO (volume $MINIO_VOL)…"
docker run --rm -v "$MINIO_VOL:/data:ro" -v "$OUT:/backup" alpine \
  tar czf /backup/minio_data.tar.gz -C /data .

# 3) Stalwart: tar do volume (caixas + config).
echo "[3/3] Stalwart (volume $STALWART_VOL)…"
docker run --rm -v "$STALWART_VOL:/data:ro" -v "$OUT:/backup" alpine \
  tar czf /backup/stalwart_data.tar.gz -C /data .

# Manifesto: o que foi salvo e com que ferramentas (o restore lê isto como sanity check).
cat > "$OUT/MANIFEST.txt" <<EOF
mailin-backup
timestamp=$TS
postgres_db=$PG_DB
postgres=postgres.sql.gz (pg_dump --clean --if-exists | gzip)
minio=minio_data.tar.gz (tar do volume $MINIO_VOL)
stalwart=stalwart_data.tar.gz (tar do volume $STALWART_VOL)
EOF

echo "== Concluído. Tamanhos: =="
du -h "$OUT"/* 2>/dev/null || true

# Retenção: apaga diretórios de backup mais antigos que RETENTION_DAYS.
if [[ "$RETENTION_DAYS" -gt 0 ]]; then
  echo "== Retenção: a remover backups > ${RETENTION_DAYS} dias em $DEST_ROOT =="
  find "$DEST_ROOT" -mindepth 1 -maxdepth 1 -type d -mtime "+$RETENTION_DAYS" -print -exec rm -rf {} +
fi

echo "OK. Restaurar com:  scripts/restore.sh $OUT"
