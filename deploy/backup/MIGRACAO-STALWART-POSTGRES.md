# Mudar o correio de RocksDB para Postgres

**Estado: por executar.** Precisa de acesso ao VPS — não é operação de painel.

**Sem correio real em produção** (29/07/2026), a janela de paragem deixa de ser um problema: não há
mensagens de clientes para preservar nem utilizadores a incomodar. Continua a valer a pena usar a
exportação/importação em vez de começar do zero, porque ela traz também o **registo** — domínios,
DKIM, TLS, contas — e isso poupa reconfigurar tudo à mão.

**Antes de executar, é preciso ver o VPS.** Ao preparar isto descobri que o `config.toml` deste
repositório não é sequer lido por esta versão (ver [DIVERGENCES.md](../DIVERGENCES.md) §D9), e que
produção tem de estar a ler outra coisa qualquer. Migrar às cegas seria trabalhar sobre uma planta
que não corresponde ao edifício.

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

## A configuração nova — provada, não suposta

Corri a imagem oficial `v0.16.4` contra um Postgres e um S3 locais até arrancar: sobe, fica
saudável e cria as 27 tabelas. O ficheiro de arranque é **JSON**, e define **apenas o armazenamento
de dados** — o resto (listeners, TLS, domínios, contas) vive num registo dentro desse
armazenamento. Ver [DIVERGENCES.md](../DIVERGENCES.md) §D9.

`deploy/stalwart/config.json`:

```json
{
  "@type": "PostgreSql",
  "host": "postgres",
  "port": 5432,
  "database": "stalwart",
  "authUsername": "beskos",
  "authSecret": { "@type": "EnvironmentVariable", "value": "POSTGRES_PASSWORD" }
}
```

E no `docker-compose.yml`, montá-lo onde a imagem o procura:

```yaml
    volumes:
      - ./stalwart/config.json:/etc/stalwart/config.json:ro
```

Duas armadilhas que custaram tempo e que vale a pena não repetir:

- `@type` é **`PostgreSql`** — `postgresql` em minúsculas é recusado. As variantes aceites são
  `RocksDb`, `Sqlite`, `FoundationDb`, `PostgreSql`, `MySql`.
- O segredo é um enum etiquetado. Escrever `{"@type":"Value","value":"..."}` em vez de `"secret"`
  **não dá erro nenhum** — dá uma senha vazia e depois uma falha de autenticação obscura do lado do
  Postgres. Confirmar sempre com um arranque real.

**O armazenamento dos anexos (S3/R2) já não se define aqui** — passou para o registo, ou seja,
configura-se na interface de administração depois de o servidor subir. É um passo a fazer à mão
uma vez, e a exportação/importação leva-o consigo daí em diante.

## Depois

- O `pg_dump` de 3×/dia passa a cobrir o correio **sem alterar nada no serviço de backup** — basta
  acrescentar a base `stalwart` à lista de bases a exportar.
- Atualizar o **Anexo II do DPA**: a frase "backups não estão em funcionamento" sai, e entra o que
  passa a ser verdade. Com aviso aos clientes, como qualquer outra alteração.
- Rever a página de segurança e a de estado.
