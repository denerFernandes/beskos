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
#  4. CIFRA antes de enviar, com chave ASSIMÉTRICA (age). O servidor leva só a chave pública: pode
#     escrever cópias novas e NÃO consegue ler as antigas. Com cifra simétrica, quem comprometesse
#     este contentor lia o histórico todo — que aqui inclui as mensagens de correio, porque o
#     Stalwart guarda tudo em Postgres. A chave privada vive fora desta infraestrutura.

set -euo pipefail

: "${DATABASE_URL:?DATABASE_URL em falta}"
: "${BACKUP_BUCKET:?BACKUP_BUCKET em falta}"
: "${RETENTION_DAYS:=30}"
# Duas bases no mesmo servidor: a da aplicação e a do CORREIO. O Stalwart guarda tudo em Postgres
# — mensagens, anexos, contas e a própria configuração — por isso o correio entra aqui como
# qualquer outra base, a quente e sem parar nada.
: "${BACKUP_DATABASES:=beskos stalwart}"
# Destinatário age (chave pública, "age1..."). Sem ela o backup ABORTA: enviar em claro para um
# balde de um fornecedor americano é exatamente o que esta cifra existe para evitar, e uma variável
# esquecida não pode degradar isso em silêncio.
: "${BACKUP_AGE_RECIPIENT:?BACKUP_AGE_RECIPIENT em falta (chave pública age do destinatário)}"

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
  # Feita ANTES da cifra, de propósito: depois de cifrado já não se consegue olhar lá para dentro,
  # e um dump partido cifrado continua partido.
  TABLES=$(pg_restore --list "$DUMP" | grep -c "TABLE DATA" || true)
  [ "$TABLES" -gt 0 ] || fail "[$BASE] o dump não tem dados de tabela nenhuma"
  log "[$BASE] verificado: $((SIZE / 1024)) KiB, $TABLES tabelas com dados"

  # Cifra para o destinatário. O que sai daqui não se abre sem a chave privada, que não está neste
  # servidor — nem no balde, nem no CapRover.
  ENC="$DUMP.age"
  age --encrypt --recipient "$BACKUP_AGE_RECIPIENT" --output "$ENC" "$DUMP" || fail "[$BASE] cifra"
  rm -f "$DUMP" # o dump em claro não sobrevive a esta linha
  ENC_SIZE=$(stat -c %s "$ENC" 2>/dev/null || stat -f %z "$ENC")
  [ "$ENC_SIZE" -gt 1024 ] || fail "[$BASE] cifrado com $ENC_SIZE bytes — não pode ser"

  rclone copyto "$ENC" "r2:$BACKUP_BUCKET/$BASE/$DAY/$BASE-$STAMP.dump.age" \
    --s3-no-check-bucket --stats-one-line || fail "[$BASE] envio para o R2"

  # Confirma do outro lado: enviar sem confirmar é assumir que a rede não mente.
  REMOTE=$(rclone size "r2:$BACKUP_BUCKET/$BASE/$DAY/$BASE-$STAMP.dump.age" --json | sed 's/.*"bytes":\([0-9]*\).*/\1/')
  [ "$REMOTE" = "$ENC_SIZE" ] || fail "[$BASE] tamanho no R2 ($REMOTE) diferente do local ($ENC_SIZE)"
  log "[$BASE] confirmado no R2, cifrado"

  # Sem --rmdirs: no R2 não há diretórios, só chaves com barras. Pedir para os apagar enche o
  # registo de erros que não são erros — e um registo cheio de erros ignorados é onde o erro a sério
  # se esconde.
  rclone delete "r2:$BACKUP_BUCKET/$BASE/" --min-age "${RETENTION_DAYS}d" || true
  rm -f "$ENC"
done

# ---------------------------------------------------------------------------------------------
# Blocos do correio.
#
# O Stalwart DIVIDE cada mensagem: no Postgres fica a versão analisada (remetente, assunto, termos
# de pesquisa, texto do corpo) e no armazenamento de objetos fica a mensagem CRUA com os anexos —
# que é o que o IMAP entrega quando um cliente pede o original. Medido a 30/07/2026: 115 KiB na base
# contra 637 KiB no balde, com um único anexo de 540 KiB que não cabe numa base cuja maior linha tem
# 7,9 KiB.
#
# Ou seja: o pg_dump sozinho NÃO restaura o correio. Devolve caixas, contas e conteúdo pesquisável,
# e perde os anexos. Por isso os blocos entram aqui.
if [ -n "${BLOB_BUCKET:-}" ]; then
  PREFIXO="${BLOB_PREFIX:-mail}"
  TAR="$TMP/blobs-$STAMP.tar"
  DIR="$TMP/blobs"
  mkdir -p "$DIR"

  log "[blobs] a trazer $PREFIXO/ do armazenamento"
  rclone copy "blobs:$BLOB_BUCKET/$PREFIXO" "$DIR" --stats-one-line || fail "[blobs] descarregar"

  # TETO. Isto copia o prefixo INTEIRO a cada corrida — barato com centenas de KiB, absurdo com
  # dezenas de GiB. Falhar alto quando passar do teto é melhor do que uma fatura a crescer em
  # silêncio: quando isto disparar, a cópia passa a incremental (`rclone sync` dos objetos já
  # cifrados pelo gateway, que aí não precisam do age).
  BYTES=$(du -sk "$DIR" | cut -f1)
  [ "$BYTES" -le "${BLOB_MAX_KIB:-2097152}" ] \
    || fail "[blobs] $((BYTES / 1024)) MiB passa do teto — está na hora da cópia incremental"

  tar -cf "$TAR" -C "$DIR" . || fail "[blobs] arquivar"
  FICHEIROS=$(tar -tf "$TAR" | grep -c . || true)
  [ "$FICHEIROS" -gt 0 ] || fail "[blobs] arquivo vazio"
  log "[blobs] $FICHEIROS ficheiros, $((BYTES / 1024)) MiB"

  age --encrypt --recipient "$BACKUP_AGE_RECIPIENT" --output "$TAR.age" "$TAR" || fail "[blobs] cifra"
  rm -f "$TAR"
  ENC_SIZE=$(stat -c %s "$TAR.age" 2>/dev/null || stat -f %z "$TAR.age")

  rclone copyto "$TAR.age" "r2:$BACKUP_BUCKET/blobs/$DAY/blobs-$STAMP.tar.age" \
    --s3-no-check-bucket --stats-one-line || fail "[blobs] envio"
  REMOTE=$(rclone size "r2:$BACKUP_BUCKET/blobs/$DAY/blobs-$STAMP.tar.age" --json | sed 's/.*"bytes":\([0-9]*\).*/\1/')
  [ "$REMOTE" = "$ENC_SIZE" ] || fail "[blobs] tamanho no R2 ($REMOTE) diferente do local ($ENC_SIZE)"
  log "[blobs] confirmado no R2, cifrado"

  rclone delete "r2:$BACKUP_BUCKET/blobs/" --min-age "${RETENTION_DAYS}d" || true
  rm -rf "$DIR" "$TAR.age"
fi

if [ -n "${HEARTBEAT_URL:-}" ]; then
  curl -fsS --max-time 20 "$HEARTBEAT_URL" >/dev/null && log "estado avisado" || log "aviso de estado falhou (o backup está feito)"
fi

log "concluído em $(date -u +%Y-%m-%dT%H:%M:%SZ)"
