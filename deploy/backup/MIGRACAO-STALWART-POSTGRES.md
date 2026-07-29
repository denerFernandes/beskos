# Mudar o correio de RocksDB para Postgres

**Estado: por executar.** Precisa de acesso ao VPS (não é uma operação de painel) e de uma janela
de paragem do correio. Este documento é o plano e os comandos exatos.

## Porquê — e a razão não é arrumação

Hoje o Stalwart guarda tudo num **RocksDB** embutido (`/opt/stalwart/data`): mensagens, anexos,
índice de pesquisa, contas. É rápido e é o que a documentação deles recomenda para um nó só.

O problema é o backup. O RocksDB deixa **um processo de cada vez** abrir a base. A exportação
própria do Stalwart, `--export`, é um processo separado — logo, **com o servidor a correr, não
consegue abrir a base**. Um backup consistente do correio custa parar o correio.

Três vezes por dia, isso é uma indisponibilidade programada e nenhuma promessa de serviço a
sustentar. E a alternativa preguiçosa — copiar o diretório a quente — dá um ficheiro que parece um
backup, ocupa espaço, e não restaura: o RocksDB está a escrever enquanto se copia.

Com o Postgres como armazenamento, o quadro muda por completo:

| | RocksDB (hoje) | Postgres |
|---|---|---|
| Backup do correio | Só com o servidor parado | **A quente**, no mesmo `pg_dump` que já corre 3×/dia |
| Restauro a um ponto no tempo | Não | Sim, com WAL |
| Réplica | Não | Sim |
| Latência por operação | Menor (base local) | Maior (ida e volta a SQL) |

A latência é o custo real e é honesto dizê-lo: cada operação de IMAP/JMAP passa a ser SQL. Para a
nossa dimensão é irrelevante; para dezenas de milhares de caixas voltaríamos a discutir.

**Os anexos passam para o R2** ao mesmo tempo (`blob = "s3"`), o que tira o peso maior da base e
põe o conteúdo de correio no mesmo armazenamento europeu que já usamos para o Drive.

## O plano

O formato de exportação do Stalwart é **independente do armazenamento** — é precisamente para isto
que existe. O ficheiro que se exporta do RocksDB importa-se para o Postgres.

### Antes (sem parar nada)

1. Confirmar que o backup do Postgres está a correr e que **já foi restaurado com sucesso** uma vez
   (ver [README.md](README.md)). Não se migra um armazenamento sem rede por baixo.
2. Criar a base do correio, separada da da aplicação — misturar as duas transforma qualquer
   restauro parcial num problema:
   ```sh
   psql "$DATABASE_URL" -c "CREATE DATABASE stalwart;"
   ```
3. Criar o balde `beskos-mail` em **jurisdição UE** no painel do R2, e um token com escrita nele.

### A janela (o correio pára — conte 10 a 30 minutos, conforme o volume)

```sh
cd /caminho/do/deploy

# 1. Parar SÓ o Stalwart. O resto da plataforma continua; quem enviar correio nesta janela recebe
#    um adiamento do servidor de origem e volta a tentar — não se perde mensagem.
docker compose stop stalwart

# 2. Exportar. O volume de backup é montado a partir do host para o ficheiro não morrer com o
#    container efémero.
docker compose run --rm -v /var/backups/stalwart:/backup stalwart --export /backup/$(date -u +%Y%m%d)

# 3. Guardar a exportação FORA da máquina, antes de tocar em qualquer configuração.
rclone copy /var/backups/stalwart r2:beskos-backup/mail-migracao/

# 4. Trocar a configuração (deploy/stalwart/config.toml) — ver o bloco abaixo.

# 5. Importar para o novo armazenamento.
docker compose run --rm -v /var/backups/stalwart:/backup stalwart --import /backup/$(date -u +%Y%m%d)

# 6. Subir e verificar.
docker compose up -d stalwart
```

### Verificação antes de dar por concluído

- `docker compose logs -f stalwart` — arranque sem erros de armazenamento.
- Entrar no webmail e **abrir uma mensagem antiga com anexo** (prova que o blob veio, não só o
  índice).
- Enviar e receber uma mensagem de teste.
- Contar as caixas: o número tem de bater com o de antes.
- A página de estado tem de ficar verde nas quatro linhas de correio.

**Se algo correr mal:** repor o `config.toml` anterior e subir o Stalwart. O RocksDB **não foi
tocado** — a exportação lê, não escreve. É por isso que o passo 4 vem depois do 2, e não antes.

## A configuração nova

Em `deploy/stalwart/config.toml`, substituir o bloco `[storage]` e acrescentar os armazenamentos:

```toml
[storage]
data = "postgres"
fts = "postgres"
lookup = "postgres"
directory = "internal"
blob = "r2"           # anexos e corpos grandes no R2, não na base

[store.postgres]
type = "postgresql"
host = "postgres"
port = 5432
database = "stalwart"
user = "%{env:POSTGRES_USER}%"
password = "%{env:POSTGRES_PASSWORD}%"

[store.r2]
type = "s3"
bucket = "beskos-mail"
region = "auto"
endpoint = "https://<conta>.eu.r2.cloudflarestorage.com"
access-key = "%{env:R2_ACCESS_KEY}%"
secret-key = "%{env:R2_SECRET_KEY}%"

[directory.internal]
type = "internal"
store = "postgres"
```

E no `docker-compose.yml`, passar `R2_ACCESS_KEY`/`R2_SECRET_KEY` ao serviço `stalwart`.

## Depois

- O `pg_dump` de 3×/dia passa a cobrir o correio **sem alterar nada no serviço de backup** — basta
  acrescentar a base `stalwart` à lista de bases a exportar.
- Atualizar o **Anexo II do DPA**: a frase "backups não estão em funcionamento" sai, e entra o que
  passa a ser verdade. Com aviso aos clientes, como qualquer outra alteração.
- Rever a página de segurança e a de estado.
