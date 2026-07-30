#!/bin/bash
# Ida e volta completa, corrida DENTRO da imagem de backup. Chamado pelo test-restore.sh.
#
# Corre o backup.sh e o restore.sh verdadeiros, sem cópias adaptadas para teste: um ensaio que
# exercita uma versão parecida do script não prova nada sobre o que corre às duas da manhã.
#
# O R2 é substituído por um remoto `local` do rclone — o mesmo comando, o mesmo caminho de código,
# a escrever num diretório em vez de na Cloudflare. O que se quer provar aqui é a cifra e o
# restauro, não o HTTP da Cloudflare.

set -euo pipefail

PG="postgres://postgres:testpw@pg:5432"
BUCKET=/tmp/bucket
ok() { echo "  ✓ $*"; }
fail() { echo "  ✗ FALHOU: $*"; exit 1; }

echo "1. a semear as duas bases"
for BASE in beskos stalwart; do
  psql "$PG/postgres" -qtc "CREATE DATABASE $BASE" >/dev/null
  psql "$PG/$BASE" -qc "
    CREATE TABLE mensagens (id INT PRIMARY KEY, corpo TEXT NOT NULL);
    INSERT INTO mensagens SELECT i, 'segredo do $BASE nº ' || i FROM generate_series(1, 5000) i;
  " >/dev/null
done
psql "$PG/postgres" -qtc "CREATE DATABASE beskos_restaurado" >/dev/null
# A impressão digital do conteúdo. É o que vai ser comparado no fim — comparar contagens de linhas
# deixaria passar um restauro que trouxesse as linhas certas com o conteúdo trocado.
IMPRESSAO=$(psql "$PG/beskos" -qtAc "SELECT md5(string_agg(corpo, ',' ORDER BY id)) FROM mensagens")
ok "5000 linhas em cada base, impressão ${IMPRESSAO:0:12}…"

echo "2. a gerar o par de chaves age"
age-keygen -o /tmp/chave.txt 2>/dev/null
PUBLICA=$(grep -o 'age1[a-z0-9]*' /tmp/chave.txt | head -1)
[ -n "$PUBLICA" ] || fail "age-keygen não deu chave pública"
ok "pública $PUBLICA"

echo "3. a correr o backup.sh (o verdadeiro)"
export DATABASE_URL="$PG/beskos"
export BACKUP_BUCKET="$BUCKET"
export BACKUP_DATABASES="beskos stalwart"
export BACKUP_AGE_RECIPIENT="$PUBLICA"
export RCLONE_CONFIG_R2_TYPE=local
/usr/local/bin/backup.sh || fail "backup.sh"

CIFRADO=$(find "$BUCKET/beskos" -name '*.dump.age' | head -1)
[ -n "$CIFRADO" ] || fail "não apareceu ficheiro cifrado no balde"
ok "$(basename "$CIFRADO")"

echo "4. a verificar que o que foi enviado NÃO se lê"
# Esta é a asserção que interessa. Sem ela, o ensaio passaria na mesma se o backup.sh tivesse
# enviado o dump em claro por engano — e ninguém daria por isso até ao balde ser copiado.
head -c 21 "$CIFRADO" | grep -q "age-encryption.org" \
  || fail "o ficheiro no balde não começa pelo cabeçalho do age — foi enviado em claro?"
ok "cabeçalho age presente"

pg_restore --list "$CIFRADO" >/dev/null 2>&1 \
  && fail "o pg_restore conseguiu ler o ficheiro cifrado — não está cifrado"
ok "o pg_restore não o consegue ler"

grep -qa "segredo do beskos" "$CIFRADO" \
  && fail "encontrei texto do conteúdo dentro do ficheiro cifrado"
ok "nenhum texto em claro lá dentro"

[ -z "$(find /tmp -maxdepth 2 -name '*.dump' 2>/dev/null)" ] \
  || fail "ficou um dump em claro em disco depois do backup"
ok "não sobrou dump em claro em disco"

echo "5. a verificar que a chave ERRADA não abre"
age-keygen -o /tmp/outra.txt 2>/dev/null
if AGE_IDENTITY_FILE=/tmp/outra.txt BACKUP_BUCKET="$BUCKET" \
   /usr/local/bin/restore.sh beskos "${CIFRADO#"$BUCKET"/}" --to "$PG/beskos_restaurado" >/dev/null 2>&1; then
  fail "uma chave privada diferente conseguiu restaurar"
fi
ok "chave errada rejeitada"

echo "6. a restaurar com a chave certa (o restore.sh verdadeiro)"
AGE_IDENTITY_FILE=/tmp/chave.txt BACKUP_BUCKET="$BUCKET" \
  /usr/local/bin/restore.sh beskos "${CIFRADO#"$BUCKET"/}" --to "$PG/beskos_restaurado" \
  || fail "restore.sh"

echo "7. a comparar o conteúdo restaurado com o original"
VOLTA=$(psql "$PG/beskos_restaurado" -qtAc "SELECT md5(string_agg(corpo, ',' ORDER BY id)) FROM mensagens")
[ "$VOLTA" = "$IMPRESSAO" ] || fail "impressão diferente: $VOLTA vs $IMPRESSAO"
LINHAS=$(psql "$PG/beskos_restaurado" -qtAc "SELECT count(*) FROM mensagens")
[ "$LINHAS" = "5000" ] || fail "$LINHAS linhas restauradas, esperava 5000"
ok "5000 linhas, conteúdo idêntico byte a byte"

echo
echo "ida e volta completa: dump → cifra → balde → descarga → decifra → restauro → conteúdo igual"
