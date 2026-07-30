# Backups

Três vezes por dia — **02:00, 10:00 e 18:00 UTC** — mais uma corrida a cada arranque do serviço,
para que um erro de configuração apareça no minuto do deploy e não às duas da manhã.

Destino: `r2://beskos-backup/<base>/AAAA/MM/DD/<base>-<carimbo>.dump.age`, em **jurisdição UE**.
Retenção 30 dias. **Cifrados** — ver [Cifra](#cifra) já a seguir.

## O que está coberto, e o que não está

| | Estado |
|---|---|
| Base de dados da aplicação (`beskos`) — contas, ficheiros, calendário, chat, wiki, faturação | **Coberta** |
| Base de dados do correio (`stalwart`) — mensagens, anexos, caixas e a configuração do servidor | **Coberta** |
| Ficheiros do Drive e anexos grandes (R2) | Vivem no R2, com durabilidade do fornecedor. **Não há segunda cópia nossa** — um apagamento acidental do balde não é recuperável |

O Stalwart guarda tudo em **PostgreSQL** (ver [DIVERGENCES.md](../DIVERGENCES.md) §D9): mensagens,
anexos, contas e a própria configuração dele. Por isso o correio entra aqui como qualquer outra
base — **a quente, sem parar nada**. Se algum dia voltar a um armazenamento embutido, isto deixa de
ser verdade e o backup do correio passa a exigir paragem: é uma decisão a tomar com essa conta
feita.

## O que o script faz que a maioria não faz

1. **Verifica o ficheiro antes de o enviar** (`pg_restore --list`, e conta as tabelas com dados).
   Um dump que não se consegue ler não é um backup, é um ficheiro — e isso descobre-se sempre no
   pior dia possível.
2. **Confirma do outro lado.** Compara o tamanho no R2 com o local. Enviar sem confirmar é assumir
   que a rede nunca mente.
3. **Avisa quando corre bem.** Se `HEARTBEAT_URL` estiver definido, faz ping a um monitor de *push*
   da página de estado; se um backup não acontecer dentro da janela, o monitor cai e nós sabemos.
   O modo de falha clássico dos backups não é falharem — é terem deixado de correr há três semanas
   sem ninguém reparar.
4. **Falha alto.** Qualquer erro aborta com mensagem. Nada de continuar como se nada fosse.
5. **Cifra antes de enviar**, com chave assimétrica. Ponto seguinte.

## Cifra

Os dumps saem daqui cifrados com [`age`](https://age-encryption.org), para uma chave **assimétrica**.
O servidor leva só a **chave pública**: pode escrever cópias novas e **não consegue ler as antigas**.

Não é um detalhe de configuração — é a diferença entre dois cenários. Com cifra simétrica (ou sem
cifra), quem comprometesse este contentor, ou a chave do R2, lia o histórico inteiro. E o histórico
aqui **inclui as mensagens de correio**, porque o Stalwart guarda tudo em Postgres. Com chave
assimétrica, o mesmo atacante consegue no máximo escrever lixo por cima de cópias futuras.

A **chave privada vive fora desta infraestrutura** — não está no contentor, nem no balde, nem no
CapRover. Só aparece no momento do restauro, passada em `AGE_IDENTITY_FILE`.

> **Perder a chave privada torna todas as cópias inúteis.** É o preço, e é consciente: sem ela, o
> backup é uma cópia legível dos dados de todos os clientes num balde de um fornecedor americano.
> Guarde-a em dois sítios independentes do resto (cofre de senhas + papel, por exemplo).

Gerar o par:

```sh
age-keygen -o beskos-backup.key     # a privada; guardar FORA daqui
# a pública sai no stderr e no comentário do ficheiro: "age1..."
```

A pública vai para `BACKUP_AGE_RECIPIENT`. **Sem ela o backup aborta** — enviar em claro é
exatamente o que esta cifra existe para evitar, e uma variável esquecida não pode degradar isso em
silêncio.

## Restaurar

```sh
# 1. Escolher a cópia. --files-only é OBRIGATÓRIO: sem ele o rclone também lista as PASTAS e
#    um `tail -1` distraído devolve "2026/07/29/" em vez do ficheiro. (Aconteceu no primeiro teste.)
rclone lsf r2:beskos-backup/beskos --recursive --files-only | sort | tail -5

# 2. Restaurar para uma base NOVA (nunca por cima da que está a servir, sem antes olhar para ela).
#    O restore.sh trata de descarregar, decifrar e verificar antes de escrever seja o que for.
createdb beskos_restaurado
AGE_IDENTITY_FILE=/caminho/para/beskos-backup.key BACKUP_BUCKET=beskos-backup \
  restore.sh beskos beskos/2026/07/29/beskos-<carimbo>.dump.age \
  --to "postgres://.../beskos_restaurado"

# 3. Conferir antes de trocar seja o que for
psql "postgres://.../beskos_restaurado" -c "select count(*) from tenants;"
```

Sem `--to`, o `restore.sh` decifra, verifica e deixa o dump em claro em disco — que é o que se quer
quando o destino ainda não existe ou quando só se quer espreitar lá para dentro.

O `--no-owner --no-privileges` no dump e no restauro é de propósito: o restauro deixa de depender
de os mesmos papéis existirem no destino, que é o que estraga metade dos restauros de emergência.

### Ensaio

```sh
deploy/backup/test-restore.sh
```

Levanta um Postgres descartável, corre o **`backup.sh` e o `restore.sh` verdadeiros** (não cópias
adaptadas para teste — um ensaio que exercita um script parecido não prova nada sobre o que corre às
duas da manhã), e compara o conteúdo no fim. O R2 é substituído por um remoto `local` do rclone: o
mesmo comando, o mesmo caminho de código, a escrever num diretório.

Verifica, por esta ordem: que o ficheiro enviado começa pelo cabeçalho do `age`; que o `pg_restore`
**não** o consegue ler; que não há texto do conteúdo lá dentro; que não sobrou dump em claro em
disco; que uma **chave privada diferente é rejeitada**; e que o restauro com a chave certa devolve as
linhas com o conteúdo idêntico (comparação por *hash* do conteúdo, não por contagem de linhas — uma
contagem deixaria passar um restauro que trouxesse as linhas certas com o conteúdo trocado).

Correr sempre que se mexer no `backup.sh`, no `restore.sh` ou na imagem. A cifra assimétrica faz com
que um erro do lado da chave só se manifeste no dia do restauro — o pior dia possível para o
descobrir.

## Configuração

App `backup` no CapRover, imagem `beskos-backup`. Variáveis:

| Variável | Para quê |
|---|---|
| `DATABASE_URL` | A mesma da API |
| `R2_ENDPOINT` | `<conta>.eu.r2.cloudflarestorage.com` — o endpoint da **jurisdição UE** |
| `R2_ACCESS_KEY` / `R2_SECRET_KEY` | Credenciais do R2 |
| `BACKUP_BUCKET` | `beskos-backup` |
| `BACKUP_AGE_RECIPIENT` | Chave **pública** `age1…`. **Obrigatória** — sem ela o backup aborta |
| `BACKUP_DATABASES` | `beskos stalwart` (por omissão) |
| `RETENTION_DAYS` | 30 |
| `HEARTBEAT_URL` | Monitor de *push* da página de estado (opcional, mas é o que nos avisa) |

## Por melhorar

- **Credencial dedicada.** Hoje o serviço usa a mesma chave do R2 que a aplicação, que também lê os
  ficheiros dos clientes. Um token com escrita apenas no balde de backups reduz o estrago de uma
  fuga. Menos grave desde a cifra — o que lá está já não se lê — mas continua a permitir apagar.
- **Cópia fora do R2.** Tudo num fornecedor só é um risco de conta, não de disco.
- **Ensaio de restauro periódico.** O `test-restore.sh` prova a ida e volta em qualquer máquina, mas
  correr à mão. Agendá-lo apanharia uma degradação silenciosa — é a diferença entre "foi provado uma
  vez" e "sabemos que funciona hoje".
