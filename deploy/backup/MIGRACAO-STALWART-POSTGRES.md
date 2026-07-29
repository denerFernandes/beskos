# Correio em Postgres — já está feito

**Não há migração a fazer.** Verificado em produção a 29/07/2026: o Stalwart já corre sobre
PostgreSQL — base `stalwart`, no mesmo servidor da aplicação, com as mensagens, os anexos (4 928
blobs), as contas e a configuração dele todos em SQL.

Este documento existia a planear uma migração a partir do RocksDB, com janela de paragem e tudo.
Escrevi-o a partir do `deploy/stalwart/config.toml` deste repositório — que, como se veio a
descobrir, **nunca foi lido por produção**. Ver [DIVERGENCES.md](../DIVERGENCES.md) §D9.

Fica a lição, que vale mais do que o plano: **um plano escrito sobre a configuração do repositório
não é um plano sobre o sistema.** A primeira coisa a fazer era olhar para a máquina.

## O que ficou a valer

- O correio é exportado três vezes por dia, a quente, com o resto — ver [README.md](README.md).
- A configuração real está em `/opt/beskos/stalwart/config.json` no VPS-1, montada no serviço de
  swarm `stalwart`. O `config.toml` deste repositório é inerte e devia ser removido ou substituído
  pelo real, para ninguém voltar a acreditar nele.
- O armazenamento dos anexos continua dentro do Postgres. Passá-los para o R2 (`blob`) aliviaria a
  base e o backup, e é uma opção a considerar quando o volume crescer — não é urgente com 12 MB.
