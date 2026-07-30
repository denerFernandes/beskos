#!/bin/bash
# Ensaio de restauro. Levanta um Postgres descartável, corre o backup e o restauro de verdade, e
# compara o conteúdo no fim.
#
#   deploy/backup/test-restore.sh
#
# Não toca em produção nem na rede: nem a base de dados nem o "balde" saem daqui. Corre-o sempre que
# se mexer no backup.sh, no restore.sh ou na imagem — a cifra assimétrica faz com que um erro do
# lado da chave só se manifeste no dia do restauro, que é o pior dia possível para o descobrir.

set -euo pipefail
cd "$(dirname "$0")"

REDE=mailin-backup-test
PG=mailin-backup-test-pg
IMG=mailin-backup:test

limpar() { docker rm -f "$PG" >/dev/null 2>&1 || true; docker network rm "$REDE" >/dev/null 2>&1 || true; }
trap limpar EXIT
limpar

echo "→ a construir a imagem de backup"
docker build -q -t "$IMG" . >/dev/null

echo "→ a levantar um Postgres descartável"
docker network create "$REDE" >/dev/null
docker run -d --name "$PG" --network "$REDE" --network-alias pg \
  -e POSTGRES_PASSWORD=testpw postgres:16-alpine >/dev/null

for _ in $(seq 30); do
  docker exec "$PG" pg_isready -q && break
  sleep 1
done
docker exec "$PG" pg_isready -q || { echo "o Postgres não arrancou"; exit 1; }

echo "→ ida e volta"
echo
docker run --rm --network "$REDE" -e PGPASSWORD=testpw \
  -v "$PWD/round-trip.sh:/round-trip.sh:ro" \
  --entrypoint bash "$IMG" /round-trip.sh
