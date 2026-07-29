# Backups

Três vezes por dia — **02:00, 10:00 e 18:00 UTC** — mais uma corrida a cada arranque do serviço,
para que um erro de configuração apareça no minuto do deploy e não às duas da manhã.

Destino: `r2://beskos-backup/postgres/AAAA/MM/DD/beskos-<carimbo>.dump`, em **jurisdição UE**.
Retenção 30 dias.

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

## Restaurar

Testado ponta a ponta (dump → R2 → base vazia → dados de volta) antes de isto entrar.

```sh
# 1. Escolher o backup. --files-only é OBRIGATÓRIO: sem ele o rclone também lista as PASTAS e
#    um `tail -1` distraído devolve "2026/07/29/" em vez do ficheiro. (Aconteceu no primeiro teste.)
rclone lsf r2:beskos-backup/postgres --recursive --files-only | sort | tail -5

# 2. Trazer
rclone copyto "r2:beskos-backup/postgres/2026/07/29/beskos-<carimbo>.dump" /tmp/r.dump

# 3. Restaurar para uma base NOVA (nunca por cima da que está a servir, sem antes olhar para ela)
createdb beskos_restore
pg_restore --dbname="postgres://.../beskos_restore" --no-owner --no-privileges /tmp/r.dump

# 4. Conferir antes de trocar seja o que for
psql "postgres://.../beskos_restore" -c "select count(*) from tenants;"
```

O `--no-owner --no-privileges` no dump e no restauro é de propósito: o restauro deixa de depender
de os mesmos papéis existirem no destino, que é o que estraga metade dos restauros de emergência.

## Configuração

App `backup` no CapRover, imagem `beskos-backup`. Variáveis:

| Variável | Para quê |
|---|---|
| `DATABASE_URL` | A mesma da API |
| `R2_ENDPOINT` | `<conta>.eu.r2.cloudflarestorage.com` — o endpoint da **jurisdição UE** |
| `R2_ACCESS_KEY` / `R2_SECRET_KEY` | Credenciais do R2 |
| `BACKUP_BUCKET` | `beskos-backup` |
| `RETENTION_DAYS` | 30 |
| `HEARTBEAT_URL` | Monitor de *push* da página de estado (opcional, mas é o que nos avisa) |

## Por melhorar

- **Credencial dedicada.** Hoje o serviço usa a mesma chave do R2 que a aplicação, que também lê os
  ficheiros dos clientes. Um token com escrita apenas no balde de backups reduz o estrago de uma
  fuga.
- **Cifra do lado do cliente.** Os objetos são cifrados em repouso pelo fornecedor; quem tiver a
  chave do R2 lê os dumps. Cifrar com `age` antes de enviar resolve — ao preço de uma chave privada
  que, se se perder, torna os backups inúteis. Decisão consciente de não o fazer por agora.
- **Cópia fora do R2.** Tudo num fornecedor só é um risco de conta, não de disco.
