#!/usr/bin/env bash
# Restauro da stack de produção do Beskos a partir de um backup de scripts/backup.sh.
#
# DESTRUTIVO: sobrescreve Postgres, MinIO e Stalwart pelo conteúdo do backup. Pede confirmação
# (salte com FORCE=1). Pára api/worker durante o restauro para evitar escrita concorrente.
#
# Uso:   scripts/restore.sh <dir-do-backup>   (ex.: backups/20260719-031500)
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
COMPOSE_FILE="${COMPOSE_FILE:-$ROOT/deploy/docker-compose.yml}"
ENV_FILE="${ENV_FILE:-$ROOT/deploy/.env}"
DC=(docker compose -f "$COMPOSE_FILE" --env-file "$ENV_FILE")

SRC="${1:-}"
[[ -n "$SRC" && -d "$SRC" ]] || { echo "Uso: scripts/restore.sh <dir-do-backup>"; exit 1; }
[[ -f "$SRC/MANIFEST.txt" ]] || { echo "ERRO: $SRC não parece um backup (sem MANIFEST.txt)"; exit 1; }

PG_CONTAINER="mailin-postgres"
MINIO_VOL="mailin_minio_data"
STALWART_VOL="mailin_stalwart_data"
PG_USER="$(grep -E '^POSTGRES_USER=' "$ENV_FILE" | tail -1 | cut -d= -f2-)"
PG_DB="$(grep -E '^POSTGRES_DB=' "$ENV_FILE" | tail -1 | cut -d= -f2-)"
: "${PG_USER:?}" "${PG_DB:?}"

echo "== Restauro Beskos a partir de $SRC =="
cat "$SRC/MANIFEST.txt"
if [[ "${FORCE:-0}" != "1" ]]; then
  read -r -p "Isto SOBRESCREVE os dados atuais. Continuar? (escreva 'sim'): " ok
  [[ "$ok" == "sim" ]] || { echo "Abortado."; exit 1; }
fi

# 1) Pára os consumidores de dados (mantém os datastores de pé para o restauro).
echo "[1/4] A parar api e worker…"
"${DC[@]}" stop api worker >/dev/null 2>&1 || true

# 2) Postgres: aplica o dump lógico (o dump já tem DROP/CREATE por --clean --if-exists).
echo "[2/4] Postgres…"
gunzip -c "$SRC/postgres.sql.gz" | docker exec -i "$PG_CONTAINER" psql -U "$PG_USER" -d "$PG_DB" -q

# 3) MinIO: pára o serviço, esvazia e reextrai o volume, sobe de novo.
echo "[3/4] MinIO…"
"${DC[@]}" stop minio >/dev/null 2>&1 || true
docker run --rm -v "$MINIO_VOL:/data" -v "$SRC:/backup:ro" alpine \
  sh -c 'rm -rf /data/* /data/..?* /data/.[!.]* 2>/dev/null; tar xzf /backup/minio_data.tar.gz -C /data'
"${DC[@]}" start minio >/dev/null

# 4) Stalwart: idem.
echo "[4/4] Stalwart…"
"${DC[@]}" stop stalwart >/dev/null 2>&1 || true
docker run --rm -v "$STALWART_VOL:/data" -v "$SRC:/backup:ro" alpine \
  sh -c 'rm -rf /data/* /data/..?* /data/.[!.]* 2>/dev/null; tar xzf /backup/stalwart_data.tar.gz -C /data'
"${DC[@]}" start stalwart >/dev/null

echo "== A subir api e worker de novo =="
"${DC[@]}" start api worker >/dev/null
echo "OK. Confirme com:  ${DC[*]} ps  e  curl -fsS http://localhost:8000/healthz"
