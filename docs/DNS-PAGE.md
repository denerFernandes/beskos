# Página de DNS dos domínios — melhoria planeada

**Estado: por implementar.** Este documento descreve o que fazer; nada disto está no código ainda.
Levantado a 27/07/2026 a partir do ecrã atual (`Admin › Domínios › <domínio>`), que ficou confuso.

## O que existe hoje

- **Backend** — [`domains_handlers.go`](../backend/internal/httpapi/domains_handlers.go) devolve
  `dns_records` (o zonefile do Stalwart passado por
  [`provisioning.ParseZone`](../backend/internal/provisioning/zone.go)) e dois estados de texto:
  `dns_status` e `dkim_status`.
- **Verificação** — [`NetDNSVerifier.Verify`](../backend/internal/provisioning/dns.go) devolve **um
  único booleano** para o domínio inteiro: procura o MX e **um** registo `_domainkey` com `DKIM1`.
  Não olha para SPF, DMARC, MTA-STS, autodiscover nem para os SRV. O worker faz polling disto até
  passar (`stepVerifyDNS`), e o domínio salta de `dns_pending` para `dns_verified`.
- **Frontend** — [`DomainsSection.tsx`](../frontend/src/panel/DomainsSection.tsx) mostra uma tabela
  única (Propósito · Tipo · Host · Valor · Copiar), partida em "receber" e "enviar".

## Porque é que confunde

1. **Estado só do domínio, não do registo.** Diz "à espera" sem dizer de *quê*. Quem publicou 9 de
   11 registos não tem como saber quais faltam — nem sequer sabe que a verificação só olha para 2.
2. **A verificação mente por omissão.** Passa com MX + DKIM presentes; SPF ou DMARC em falta dão
   "verificado" e só se descobre o problema quando o correio começa a cair em spam.
3. **Copiar um a um.** São ~11 registos; copiar campo a campo para o painel do fornecedor é
   demorado e propício a erro (aspas, ponto final, prioridade do MX).
4. **A tabela transborda** em ecrãs estreitos: os valores longos (DKIM, MTA-STS) quebram o layout —
   é o que se vê no ecrã atual, com a coluna "Copy" partida em letras verticais.

## O que fazer

### 1. Verificação por registo (backend)

Substituir o booleano por um relatório. Esboço:

```go
type RecordCheck struct {
    Host     string   // como está na zona
    Type     string   // MX, TXT, CNAME, SRV
    Expected string
    Found    []string // o que o resolver devolveu (para mostrar o que está lá em vez do esperado)
    Status   string   // "ok" | "missing" | "mismatch"
    Purpose  string   // mx, spf, dkim, dmarc, mta_sts, autodiscover, srv…
}
```

- `Verify` passa a devolver `[]RecordCheck` e o booleano agregado passa a ser derivado dos que são
  **obrigatórios** (MX, SPF, DKIM) — os recomendados (DMARC, MTA-STS, SRV) entram como aviso, não
  como bloqueio. Hoje o oposto: são ignorados de todo.
- Endpoint novo `GET /api/domains/{id}/dns/check` que corre a verificação **a pedido** (o polling do
  worker continua como está, para não mudar a máquina de estados). Rate limit próprio: é I/O de rede.
- Cache curta (30–60 s) por domínio, senão um refresh repetido martela o resolver.

### 2. Página com estado granular (frontend)

- Uma linha por registo com um sinal claro: **encontrado** / **em falta** / **valor diferente**.
- Quando é "valor diferente", mostrar lado a lado o esperado e o que está publicado — é o caso mais
  difícil de diagnosticar às cegas (típico: SPF duplicado, ou o MX do fornecedor antigo).
- Agrupar por propósito (Receber correio · Enviar · Descoberta de clientes · Segurança), com o
  estado do grupo no cabeçalho e a possibilidade de fechar os grupos já resolvidos.
- Botão **"Verificar agora"** ligado ao endpoint acima, com o instante da última verificação.
- Resolver o transbordo: valor em `code` com `overflow-x:auto` próprio, nunca a empurrar a tabela.

### 3. Exportar

- **Ficheiro de zona BIND** (`.zone`) — é o formato que o Cloudflare, o Route 53 e a maioria dos
  fornecedores importam. É também o que o Stalwart já produz, portanto é quase só servir o que
  existe: `GET /api/domains/{id}/dns/export?format=zone`.
- **CSV** para quem edita à mão.
- Copiar tudo de uma vez para a área de transferência, além do copiar por registo que já existe.

> Nota: o "formato Cloudflare" **é** o ficheiro de zona BIND — o painel deles importa exatamente
> isso (`DNS › Records › Import`). Não é preciso um formato proprietário.

### 4. Documentação

Link para um guia por fornecedor (Cloudflare, Route 53, GoDaddy, Namecheap) a partir da página.
O guia ainda não existe; criar em `docs/` ou no site público e apontar daqui.

## Ordem sugerida

1. `RecordCheck` + endpoint de verificação a pedido (o resto depende disto).
2. Página com o estado por registo.
3. Exportar zona/CSV.
4. Guia por fornecedor + link.

## Cuidados

- **Multi-tenancy (lei §5.1):** o domínio tem de ser do tenant do utilizador autenticado — a
  verificação a pedido não pode virar um scanner de DNS de domínios alheios.
- **Não mexer na máquina de estados** ao adicionar a verificação a pedido: `stepVerifyDNS` continua
  a mandar na transição `dns_pending → dns_verified`. A página informa; não promove.
- **DNS mente por cache.** Mostrar sempre o instante da consulta e evitar prometer propagação
  imediata; um registo publicado agora pode não aparecer durante minutos.
