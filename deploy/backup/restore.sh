#!/bin/bash
# Restauro de uma cópia de segurança cifrada.
#
# Este ficheiro existe porque um backup que nunca foi restaurado não é um backup — é uma esperança.
# A cifra assimétrica torna isso pior: o servidor que escreve as cópias NÃO as consegue ler, por
# isso um erro no lado da chave privada só aparece no dia em que ela faz falta. Daqui a hipótese de
# ensaiar o caminho todo, com o test-restore.sh, sem tocar em produção.
#
# A CHAVE PRIVADA NÃO VIVE AQUI. Passa-se no momento do restauro (AGE_IDENTITY_FILE), a partir de
# onde quer que ela esteja guardada — cofre, papel, hardware. Se ela estivesse no contentor, a cifra
# assimétrica não valia nada: quem entrasse no servidor lia o histórico todo.
#
#   restore.sh <base> <objeto-no-r2> [--to <url-de-destino>]
#
# Sem --to, escreve o dump decifrado no disco e fica-se por aí — que é o que se quer quando o
# destino ainda não existe ou quando só se quer inspecionar o conteúdo.

set -euo pipefail

: "${AGE_IDENTITY_FILE:?AGE_IDENTITY_FILE em falta (ficheiro com a chave privada age)}"
[ -r "$AGE_IDENTITY_FILE" ] || { echo "restore: não consigo ler $AGE_IDENTITY_FILE"; exit 1; }

BASE="${1:?uso: restore.sh <base> <objeto-no-r2> [--to <url>]}"
OBJETO="${2:?uso: restore.sh <base> <objeto-no-r2> [--to <url>]}"
DESTINO=""
if [ "${3:-}" = "--to" ]; then DESTINO="${4:?--to precisa de um URL}"; fi

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

log() { echo "$(date -u +%Y-%m-%dT%H:%M:%SZ) restore: $*"; }
fail() { log "FALHOU: $*"; exit 1; }

ENC="$TMP/$(basename "$OBJETO")"
DUMP="${ENC%.age}"

# 1. Trazer. O objeto pode vir do R2 ou já estar em disco (é assim que o ensaio corre sem rede).
if [ -f "$OBJETO" ]; then
  cp "$OBJETO" "$ENC"
else
  log "a descarregar $OBJETO"
  rclone copyto "copias:${BACKUP_BUCKET:?BACKUP_BUCKET em falta}/$OBJETO" "$ENC" \
    --s3-no-check-bucket --stats-one-line || fail "descarregar"
fi

# 2. Decifrar.
age --decrypt --identity "$AGE_IDENTITY_FILE" --output "$DUMP" "$ENC" \
  || fail "decifrar — a chave privada corresponde à pública com que foi cifrado?"
rm -f "$ENC"

# 3. VERIFICAR ANTES DE ESCREVER. O mesmo teste do backup, agora do outro lado: um dump que o
# pg_restore não consegue indexar não vai lá ter com uma base de dados de produção.
TABLES=$(pg_restore --list "$DUMP" | grep -c "TABLE DATA" || true)
[ "$TABLES" -gt 0 ] || fail "o dump decifrado não tem dados de tabela nenhuma"
SIZE=$(stat -c %s "$DUMP" 2>/dev/null || stat -f %z "$DUMP")
log "[$BASE] decifrado e verificado: $((SIZE / 1024)) KiB, $TABLES tabelas com dados"

if [ -z "$DESTINO" ]; then
  GUARDA="${RESTORE_OUTPUT_DIR:-$PWD}/$(basename "$DUMP")"
  cp "$DUMP" "$GUARDA"
  log "dump em claro em $GUARDA — APAGUE-O quando acabar"
  exit 0
fi

# 4. Restaurar. --clean --if-exists deixa o destino no estado do dump em vez de o misturar com o que
# lá estivesse: um restauro por cima de dados parciais é como se descobre, meses depois, que ficaram
# linhas de duas épocas diferentes na mesma tabela.
log "[$BASE] a restaurar para o destino"
pg_restore --dbname="$DESTINO" --no-owner --no-privileges --clean --if-exists \
  --exit-on-error "$DUMP" || fail "pg_restore"
log "[$BASE] restaurado"
