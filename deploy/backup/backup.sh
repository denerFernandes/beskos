#!/bin/bash
# Backup do Postgres para o R2 (jurisdição UE).
#
# Três coisas que este script faz e que a maioria não faz:
#  1. VERIFICA o ficheiro antes de o enviar (`pg_restore --list`). Um dump que não se consegue ler
#     não é um backup, é um ficheiro — e descobre-se sempre no pior dia.
#  2. AVISA quando corre bem. Ping a um monitor de "push" da página de estado: se um backup não
#     acontecer dentro da janela, o monitor cai e nós sabemos. Backup silencioso que deixou de
#     correr há três semanas é o modo de falha clássico.
#  3. FALHA ALTO. Qualquer erro aborta com mensagem; nada de continuar como se nada fosse.

set -euo pipefail

: "${DATABASE_URL:?DATABASE_URL em falta}"
: "${BACKUP_BUCKET:?BACKUP_BUCKET em falta}"
: "${RETENTION_DAYS:=30}"
# Duas bases no mesmo servidor: a da aplicação e a do CORREIO. O Stalwart guarda tudo em Postgres
# — mensagens, anexos, contas e a própria configuração — por isso o correio entra aqui como
# qualquer outra base, a quente e sem parar nada.
: "${BACKUP_DATABASES:=beskos stalwart}"

STAMP="$(date -u +%Y-%m-%dT%H%M%SZ)"
DAY="$(date -u +%Y/%m/%d)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

log() { echo "$(date -u +%Y-%m-%dT%H:%M:%SZ) backup: $*"; }
fail() { log "FALHOU: $*"; exit 1; }

# url_para <base> — troca o nome da base no DATABASE_URL, preservando credenciais e opções.
# O utilizador é o mesmo nas duas (é o que o Stalwart usa), por isso não há segunda credencial.
url_para() {
  echo "$DATABASE_URL" | sed -E "s#(://[^/]+/)[^?]+#\1$1#"
}

for BASE in $BACKUP_DATABASES; do
  DUMP="$TMP/$BASE-$STAMP.dump"

  log "[$BASE] a exportar"
  # -Fc: formato próprio, comprimido e restaurável seletivamente (tabela a tabela, se preciso).
  # --no-owner/--no-privileges: o restauro não depende de os mesmos papéis existirem no destino,
  # que é o que estraga metade dos restauros de emergência.
  pg_dump --dbname="$(url_para "$BASE")" --format=custom --no-owner --no-privileges --file="$DUMP" \
    || fail "[$BASE] pg_dump"

  SIZE=$(stat -c %s "$DUMP" 2>/dev/null || stat -f %z "$DUMP")
  [ "$SIZE" -gt 1024 ] || fail "[$BASE] dump com $SIZE bytes — demasiado pequeno para ser verdade"

  # Verificação: lê o índice do ficheiro. Apanha truncagem e corrupção antes de irmos dormir.
  TABLES=$(pg_restore --list "$DUMP" | grep -c "TABLE DATA" || true)
  [ "$TABLES" -gt 0 ] || fail "[$BASE] o dump não tem dados de tabela nenhuma"
  log "[$BASE] verificado: $((SIZE / 1024)) KiB, $TABLES tabelas com dados"

  rclone copyto "$DUMP" "r2:$BACKUP_BUCKET/$BASE/$DAY/$BASE-$STAMP.dump" \
    --s3-no-check-bucket --stats-one-line || fail "[$BASE] envio para o R2"

  # Confirma do outro lado: enviar sem confirmar é assumir que a rede não mente.
  REMOTE=$(rclone size "r2:$BACKUP_BUCKET/$BASE/$DAY/$BASE-$STAMP.dump" --json | sed 's/.*"bytes":\([0-9]*\).*/\1/')
  [ "$REMOTE" = "$SIZE" ] || fail "[$BASE] tamanho no R2 ($REMOTE) diferente do local ($SIZE)"
  log "[$BASE] confirmado no R2"

  rclone delete "r2:$BACKUP_BUCKET/$BASE/" --min-age "${RETENTION_DAYS}d" --rmdirs || true
  rm -f "$DUMP"
done

if [ -n "${HEARTBEAT_URL:-}" ]; then
  curl -fsS --max-time 20 "$HEARTBEAT_URL" >/dev/null && log "estado avisado" || log "aviso de estado falhou (o backup está feito)"
fi

log "concluído em $(date -u +%Y-%m-%dT%H:%M:%SZ)"
