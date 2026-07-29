# Configuração do Stalwart

Este ficheiro **substituiu um `config.toml`** que nunca foi lido por nada — nem em produção, nem em
desenvolvimento. Ver [DIVERGENCES.md](../DIVERGENCES.md) §D9. Enquanto existiu, levou-me a planear
uma migração de armazenamento que já estava feita há meses.

## O que este ficheiro é — e o que não é

Na 0.16, o `--config` define **apenas onde vivem os dados**. Tudo o resto — listeners, TLS, ACME,
domínios, DKIM, contas, filas, relay de saída — passou para um **registo** guardado dentro desse
armazenamento, e edita-se pela interface de administração ou pela API.

Ou seja: **não se afina o Stalwart mexendo aqui.** Se procura onde mudar o relay de saída ou um
listener, é no registo, não neste ficheiro.

## Formato

Foi reconstruído a martelar contra a imagem oficial, porque o esquema não está documentado.
Duas armadilhas, ambas confirmadas com arranques reais:

- `@type` é **`PostgreSql`** (maiúsculas assim). As variantes aceites são `RocksDb`, `Sqlite`,
  `FoundationDb`, `PostgreSql`, `MySql`.
- O segredo é um enum etiquetado. Aqui usamos `EnvironmentVariable` + **`variableName`** — os nomes
  `name`, `value`, `key` e `secret` dão todos *"Variable name cannot be empty"*. Com a variante
  `Value`, o campo chama-se **`secret`**, e escrever `value` **não dá erro**: dá senha vazia e uma
  falha de autenticação obscura do lado do Postgres.

A variante `EnvironmentVariable` é a que usamos de propósito — assim o segredo não vive no
ficheiro (lei 2.6).

## Produção

Em produção este ficheiro está em `/opt/beskos/stalwart/config.json` no VPS-1, montado no serviço
de swarm `stalwart` como `/etc/stalwart/config.json`. **Produção não corre por `docker compose`.**
