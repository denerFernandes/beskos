# Divergências: brief (project.md) × Stalwart v0.16.4 real

O brief §11 manda: *"Se algo conflitar com a realidade da versão do Stalwart, confie na versão real e me avise da divergência."*
Este documento é a **fonte da verdade** para a integração com o Stalwart. Verificado empiricamente contra a
imagem `stalwartlabs/stalwart:v0.16.4` (bootada localmente) + código-fonte na tag `v0.16.4`.

---

## D1 — A REST Management API foi REMOVIDA. Não há OpenAPI. (impacto máximo)

O brief §6/§7/§9 assume endpoints REST (`POST /api/principal`, `POST /api/dkim`, `GET /api/queue/messages`)
e gerar um client Go a partir de um `openapi.yml`.

**Realidade 0.16:** toda a Management API REST foi removida. Confirmado: todo `/api/*` retorna **404**.
Não existe `openapi.yml` servido nem embutido. Administração agora é **100% via JMAP**, sob a capability
Stalwart-específica **`urn:stalwart:jmap`**, com métodos no namespace **`x:`**.

**Consequência no nosso projeto:**
- ❌ Cai a tarefa "gerar client via oapi-codegen".
- ✅ Escrevemos um **client Go JMAP tipado à mão** (`backend/internal/stalwart`).
- Envelope padrão de toda chamada admin:
  ```json
  { "using": ["urn:ietf:params:jmap:core","urn:stalwart:jmap"], "methodCalls": [ ["x:Object/fn", {…}, "c0"] ] }
  ```
- Endpoint: `POST /jmap/` · auth: Basic (admin global de recuperação) · `#creationId` refs funcionam entre calls.

Métodos admin (registry): `x:Domain/{get,set,query}`, `x:Account/{get,set,query}`,
`x:DkimSignature/{get,set,query}`, `x:Bootstrap/{get,set}`, e ainda `x:Tenant`, `x:Role`, `x:BeskosgList`,
`x:OAuthClient`, `x:ApiKey`, `x:AppPassword`.

> Nota: `Principal/*` (capability `urn:ietf:params:jmap:principals`) NÃO é provisionamento — são os principals
> padrão de sharing/calendário do JMAP. `Principal/set` para criar domínio falha com `notRequest`.

---

## D2 — Admin de recuperação: `STALWART_RECOVERY_ADMIN`, não `[authentication.fallback-admin]`

Brief/config inicial usava `[authentication.fallback-admin]`. **Real:** env `STALWART_RECOVERY_ADMIN=admin:<senha>`.
Esse admin (accountId `d333333`) funciona sempre, inclusive após restart. É a credencial global que só o backend Go usa.

---

## D3 — Passo de BOOTSTRAP obrigatório antes de qualquer provisionamento

Servidor novo sobe em **bootstrap mode**: qualquer método `x:` (exceto `x:Bootstrap`) retorna
`forbidden: "The server is in bootstrap mode…"`. Fluxo real (uma vez por instância):

1. `x:Bootstrap/set` no singleton `"singleton"`:
   ```json
   {"using":["urn:ietf:params:jmap:core","urn:stalwart:jmap"],
    "methodCalls":[["x:Bootstrap/set",{"update":{"singleton":{
      "serverHostname":"mail.example.org","defaultDomain":"example.org",
      "requestTlsCertificate":false,"generateDkimKeys":true}}},"0"]]}
   ```
   Resposta devolve o admin gerado `admin@<domain>` + secret (**mostrado só uma vez**).
2. **Reiniciar o processo** (`docker restart`) — o update só passa a valer após restart.

Só `serverHostname` + `defaultDomain` são obrigatórios. `requestTlsCertificate:false` evita ACME/Let's Encrypt no dev.
→ Modelamos isso como um passo de setup idempotente do control plane (não é o mesmo que provisionar tenant).

---

## D4 — Mapeamento das operações da §6 → JMAP real (payloads TESTADOS)

### Criar domínio (auto-gera DKIM) — `x:Domain/set`
```json
["x:Domain/set",{"create":{"d1":{"name":"cliente.com.br","isEnabled":true,"description":"tenant_id=42"}}},"0"]
```
→ `{"created":{"d1":{"id":"c"}}}`. `dkimManagement` default `{"@type":"Automatic"}` gera Ed25519+RSA na hora.
Guardamos o `id` retornado em `domains.stalwart_id`.

### Criar mailbox individual — `x:Account/set`
Endereço = `name` + `domainId` (NÃO existe campo `email`). `credentials`/`aliases`/`quotas` são **mapas** (`{"0":…}`), não arrays.
```json
["x:Account/set",{"create":{"u1":{
  "@type":"User","name":"joao","domainId":"c","description":"João",
  "credentials":{"0":{"@type":"Password","secret":"<senha-forte>"}},
  "quotas":{"maxDiskQuota":5368709120}}}},"0"]
```
→ `{"created":{"u1":{"id":"c"}}}`; `emailAddress` resolve para `joao@cliente.com.br`. Guardamos em `mail_users.stalwart_account_id`.
`domainId` aceita id vivo (`"c"`) ou ref de mesma request (`"#d1"`) → dá pra criar domínio + mailbox num só round-trip.
⚠️ Senha passa por checador de força (zxcvbn); senhas fracas → `invalidProperties`.

### DKIM — `x:DkimSignature`
Recomendado: deixar `dkimManagement` Automatic (default). Ler chaves p/ montar DNS: `x:DkimSignature/query` → `/get`
(retorna `selector`, `publicKey`, `@type`, `stage`). Criação manual exige fornecer `privateKey.secret` (PEM).

### Ler/editar/remover — `x:Account/set` / `x:Domain/set`
- Editar (JSON-pointer nas subpropriedades): `{"update":{"c":{"quotas/maxDiskQuota":2147483648,"credentials/0/secret":"<nova>"}}}`
- Remover: `{"destroy":["c"]}`. **Integridade referencial:** destruir domínio com DKIM/contas ligadas falha
  com `objectIsLinked`. Ordem correta: contas → DkimSignatures do domínio → domínio.

### Registros DNS p/ o cliente publicar — propriedade `dnsZoneFile`
```json
["x:Domain/get",{"ids":["c"],"properties":["dnsZoneFile"]},"0"]
```
⚠️ **Timing do DKIM (verificado no teste de integração):** num domínio **recém-criado**, o registro
DKIM TXT (`_domainkey`) **não** aparece imediatamente no `dnsZoneFile` — só SPF/MX/DMARC/MTA-STS/SRV estão lá na hora.
A geração das assinaturas não é síncrona com o create. → A tela de DNS (`dns_pending`) deve fazer **polling**
do zonefile até o DKIM surgir antes de considerar a lista de registros completa.

Retorna um trecho de zona BIND pronto com **todos** os registros calculados pelo servidor:
DKIM TXT (por selector), SPF (`v=spf1 mx -all`), `MX 10 mail.<domain>.`, `_dmarc` (`p=reject`), MTA-STS, TLS-RPT,
autoconfig/autodiscover, SRV (imaps/submissions/jmap/…). `dnsManagement` default `Manual` (servidor reporta, cliente publica).
→ É a fonte para a tela de DNS do estado `dns_pending` (§7). Parsear esse zonefile em vez de montar registros à mão.

---

## D5 — Sem `stalwart` CLI de provisionamento nesta imagem

O binário `stalwart` é só o servidor (`--config/--export/--import/--console`). Não há `stalwart-cli`/`apply`.
Confirma a escolha de integrar por JMAP/HTTP (mais testável, sem acoplar ao binário).

---

## D6 — Impacto na máquina de estados de provisionamento (§7)

O mapeamento continua válido, com as chamadas trocadas:
```
pending → domain_created (x:Domain/set) → dkim_created (automático no create; só ler p/ DNS)
        → dns_pending (ler dnsZoneFile) → dns_verified (polling DNS próprio) → active (x:Account/set)
```
`dkim_created` deixa de ser uma chamada separada quando `dkimManagement=Automatic` — vira "ler as assinaturas geradas".

---

## D7 — Webmail (Fase 2): CORS ausente, sem password-grant, `.test` quebra envio

Verificado contra Stalwart 0.16.4 real (usuário final `alice@wm.test`):

- **CORS não existe e não é configurável.** Preflight `OPTIONS /jmap/` responde 204 **sem** nenhum
  header `Access-Control-Allow-*`; POSTs com `Origin` cross-site retornam 200 mas o browser bloqueia a
  leitura. Doc oficial só cobre restrição por IP/listener. → **O React NÃO fala direto com o Stalwart.**
  **Arquitetura obrigatória: reverse proxy same-origin** (nginx/Caddy/Vite proxy) servindo o SPA e
  repassando `/jmap`, `/jmap/download`, `/jmap/upload`, `/auth`, `/.well-known` para o Stalwart.
- **Auth do usuário final:** Basic auth (`email:senha`) no `/jmap/session` **funciona** (200, devolve o
  accountId do usuário). OAuth 2.0 existe (`authorization_code`+PKCE S256, `device_code`) mas **não há
  `password`/`client_credentials` grant** — não dá para trocar user+senha por Bearer sem consentimento
  interativo. O `POST /api/auth` (código→token) do brief §8 **sumiu** (404). → Fase 2: **Basic auth /
  app-password atrás do proxy**; OAuth interativo fica para login unificado depois.
- **URLs da sessão** (`apiUrl`, `downloadUrl`, `uploadUrl`) usam o `serverHostname` configurado, não a
  origin. Confie nos **paths** (`/jmap/`, `/jmap/download/{accountId}/{blobId}/{name}`), não nos hosts.
- **TLD `.test`/`.local` quebra ENVIO:** `Identity/get` vem vazia (falha na validação de public-suffix),
  impossibilitando `EmailSubmission`. Leitura/auth funcionam. → Provisionar sempre com **TLD público real**.
- **Fluxos JMAP testados:** `Mailbox/get` (usar `role`, ids são opacos); `Email/query`+`Email/get` com
  back-reference `#ids`; leitura com `fetchTextBodyValues`/`fetchHTMLBodyValues`; envio =
  `Email/set` (draft, `mailboxIds` com id real de Drafts, `identityId` obrigatório de `Identity/get`) +
  `EmailSubmission/set` com `onSuccessDestroyEmail`; busca via `filter.text`.

## D8 — Webmail: brokering da credencial JMAP (hardening H11, 2026-07)

Decorrência direta de D7 (sem password-grant; React não fala direto com o Stalwart). Antes, o webmail
guardava a credencial Basic (`base64(email:senha)`) no `localStorage` e a enviava a cada request JMAP —
uma senha reversível exposta a qualquer XSS. Como o Stalwart 0.16.4 **não** oferece bearer/app-password
não-interativo trocável por senha (D7), adotou-se o padrão **BFF (backend-for-frontend)**:

- No login (`/api/webmail/login`), o backend verifica a senha no Stalwart e **guarda a credencial Basic
  CIFRADA** (AES-256-GCM, `internal/secretbox`, chave `MFA_ENC_KEY`) em `mail_users.jmap_cred`.
- **Todo** o tráfego JMAP passa pelo nosso proxy (`/jmap`, `/auth`, `/.well-known` → API Go, **não**
  direto ao Stalwart). O proxy autentica pela **sessão** (cookie HttpOnly na web, Bearer do token opaco
  no app nativo) e **injeta** a Basic da caixa server-to-server (`jmap_proxy.go` → `jmapBrokerAuth`).
- O browser **nunca** segura a senha da caixa. Só persiste dados não-secretos (email/accountId) + o token
  de sessão opaco (revogável), e só no app nativo; na web a sessão é o cookie HttpOnly.

Implicações operacionais:
- **Deploy:** o reverse proxy (Caddy/nginx/Vite) roteia `/jmap`, `/auth`, `/.well-known` para a **API Go**
  (`:8000`), não mais para o Stalwart. A API alcança o Stalwart pela rede interna (`STALWART_URL`).
- **`MFA_ENC_KEY` é obrigatória em produção** (a config falha fechado sem ela) — cifra a cred JMAP e o
  segredo TOTP. Trocar a chave invalida as creds guardadas (usuários re-logam).
- **Login expira** (token de sessão, 7 dias): sem a senha guardada, ao expirar o usuário re-loga — some o
  "login eterno" antigo, que era exatamente o vetor de risco.
- Sessões/anteriores à migração (`jmap_cred` vazio) recebem 401 no JMAP → re-login (que popula a cred).
- **Verificação:** exige teste de integração end-to-end com a stack completa (Stalwart + API + proxy). O
  brokering usa só Basic auth (comprovado em D7); o risco está na fiação, não no Stalwart.

## Como reproduzir / re-verificar
Container de teste: `docker run -d --name mailin-sw-probe -e STALWART_RECOVERY_ADMIN='admin:probe-secret' -p 18080:8080 stalwartlabs/stalwart:v0.16.4`
Auth: `admin:probe-secret`. Ver `backend/internal/stalwart/*_test.go` (testes de integração que exercem estes payloads).
