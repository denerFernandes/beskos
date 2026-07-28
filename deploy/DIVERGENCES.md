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

## White-label do envio (SES) — custom MAIL FROM (25/07/2026)
O Gmail (e afins) mostram **"enviado por" = domínio do envelope MAIL FROM (Return-Path)**. Sem custom
MAIL FROM na identidade SES, esse domínio é o default `<região>.amazonses.com` → **todos os
destinatários viam o Amazon**, apesar do DKIM (`assinado por`) já ser `beskos.com`.

**Decisão:** configurar custom MAIL FROM `bounce.<domínio>` por tenant (`ses.SetMailFrom`,
`BehaviorOnMxFailure=USE_DEFAULT_VALUE` para não travar o envio antes do MX propagar). O
`AugmentZoneForSES` passa a emitir, no subdomínio de bounce, `MX 10 feedback-smtp.<região>.amazonses.com`
+ `TXT "v=spf1 include:amazonses.com ~all"`.

**Trade-off aceite:** este é o **único** ponto do DNS do cliente que referencia o amazonses, e só no
subdomínio `bounce.<domínio>` (visível apenas a quem faz `dig` nesse subdomínio). Os registos do
domínio apex continuam sem qualquer Amazon (SPF via `_spf.<brand>`, DKIM da nossa chave, DMARC). Ganha-se
o white-label no que **todos os destinatários** veem, ao custo de um MX de bounce técnico. Alternativa
descartada (sem custom MAIL FROM): DNS 100% limpo mas `mailed-by: amazonses` para todos. Ver
`provisioning/zone.go`, `ses/ses.go`, testes `zone_ses_test.go`/`machine_test.go`.

## Marca própria no editor de office — patch ao Document Server (27/07/2026)

Até aqui a postura era "imagem oficial, sem modificação, só rede" (como o Stalwart). **Deixa de valer
para o OnlyOffice** — e só para ele. O Stalwart continua intocado (lei §5.4).

**Realidade medida** (imagem pinada, `onlyoffice-documentserver 9.4.0-129`, arm64):
- A doc oficial marca `customization.logo` e `customization.customer` como exclusivos da **Developer
  Edition**. Confirmado empiricamente: no Community o servidor reporta a licença com `branding: false`
  e `customization: false`, e o cliente **descarta os parâmetros em silêncio** — nem erro, nem aviso.
- O gate são **duas linhas por editor**, em `web-apps/apps/<editor>/main/app/controller/Main.js`:
  ```js
  this.appOptions.canBrandingExt = params.asc_getCanBranding() && (...)   // layout, loaderLogo, about
  this.appOptions.canBranding    = params.asc_getCustomization()          // logo do cabeçalho + "Sobre"
  ```
  (Note a inversão: `asc_getCanBranding` alimenta o `Ext`, não o básico.)
- O `web-apps` do 9.4 vem **sem minificação** — daí o patch caber num `sed`. O `DocService` do 9.4, ao
  contrário, é um ELF único de ~263 MB com o JS embutido: patchar o servidor seria muito mais caro, e é
  desnecessário, porque o veredito da licença só é *aplicado* no cliente.
- O nginx do DS serve com `gzip_static`: sem regenerar o `.gz` ao lado, o browser recebe o ficheiro
  original e o patch parece não ter efeito. É o erro fácil desta modificação.
- **O logo NÃO pode ser um `data:` URI.** O `api.js` do DS passa `customization.logo.{image,imageDark,
  imageLight}` na **query string do iframe** do editor (linhas ~1216-1219). Três data URIs de ~7 KB,
  percent-encoded, dão ~57 KB de linha de pedido → **`414 Request-URI Too Large`** ao abrir qualquer
  documento. Solução: o patch copia os SVG para dentro do webroot do DS
  (`web-apps/apps/common/main/resources/img/beskos/`) e o backend manda a URL — 362 chars, mesma
  origem do editor, sem CORS e sem depender de a app estar publicamente exposta.

**Decisão:** aplicar o patch no arranque do container (`deploy/onlyoffice-branding-patch.sh`, chamado
pelo entrypoint), mantendo a imagem oficial pinada por digest. Idempotente e fail-safe: se o padrão
mudar numa versão futura do DS, o script avisa e o editor arranca na mesma, só sem marca.

**Conformidade (AGPL v3 + política de marcas da Ascensio System SIA):**
- Os termos adicionais do 9.4 já **não** exigem manter o logo ONLYOFFICE na UI de versões modificadas —
  a antiga cláusula *badgeware* caiu; o cabeçalho do ficheiro remete para a **secção 5** da AGPL
  ("Appropriate Legal Notices") e diz "No trademark rights are granted under this License".
- O que continua obrigatório e nós cumprimos: aviso de versão modificada **com data**, identificação do
  software original, licença ligada, e a oferta do fonte modificado (§13, porque servimos pela rede).
  Tudo isso vai em `customization.customer.info` (ver `backend/internal/httpapi/onlyoffice_branding.go`),
  e o próprio `setLicInfo` do DS mantém o bloco *"Powered by / Ascensio System SIA / onlyoffice.com"*
  no "Sobre" quando enviamos `customer`.
- `ONLYOFFICE_SOURCE_URL` é **obrigatório em produção** (a config recusa arrancar sem ele) — é o
  endereço do fonte modificado. Enquanto não existir esse repositório público, não subir em produção.
- Nota: o *FAQ comercial* deles continua a dizer que remover a marca exige licença paga, o que
  contradiz os termos do LICENSE. O que vincula é a licença; o risco residual é de disputa, não de
  incumprimento claro. Decisão tomada com esse risco à vista.

**Paleta (27/07/2026):** o layout de abas é genérico, mas a cor é identidade — a política de marcas
proíbe imitar a identidade visual deles, e um editor idêntico ao das imagens oficiais é exatamente
o que não queremos. Trocamos o azul (`#446995`) pelo petróleo Beskos. Duas armadilhas medidas:

- **Tema personalizado via `themes.json` NÃO funciona no arranque.** Quem serve os temas é o
  DocService (não o nginx): lê os ficheiros de `services.CoAuthoring.themes.uri` e publica-os em
  `GET /themes.json`, **embrulhando cada ficheiro num array `themes`** — logo cada ficheiro tem de
  conter UM tema, não uma lista, senão fica aninhado a mais e é ignorado em silêncio. Mesmo depois
  disso: o editor aplica a classe (`body class="theme-beskos"`) mas **nunca escreve as variáveis
  CSS**, porque `write_theme_css()` está comentado em `parse_themes_object` e só corre em
  `apply_theme()` — ou seja, apenas quando o utilizador TROCA de tema no menu. Medido:
  `--toolbar-header-document` continuava `#446995` com o tema Beskos ativo.
- **Solução:** o patch acrescenta `beskos-theme.css` ao fim do `app.css` de cada editor, sobre
  `:root`/`.theme-type-light` e `.theme-dark`/`.theme-type-dark` — ganha por ordem em igualdade de
  especificidade, vale para qualquer tema que o utilizador escolha, e não depende do mecanismo de
  temas deles. `uiTheme` volta a ser o `theme-light`/`theme-dark` padrão.

**Verificação:** `docker compose logs onlyoffice | grep branding` → `5 editor(es) modificado(s)`; e
`curl -sL -H 'Accept-Encoding: gzip' --compressed http://127.0.0.1:8082/web-apps/apps/documenteditor/main/app/controller/Main.js | grep beskos-branding-patch`
tem de encontrar o marcador (é o teste que apanha o `.gz` desatualizado).

## Como reproduzir / re-verificar
Container de teste: `docker run -d --name mailin-sw-probe -e STALWART_RECOVERY_ADMIN='admin:probe-secret' -p 18080:8080 stalwartlabs/stalwart:v0.16.4`
Auth: `admin:probe-secret`. Ver `backend/internal/stalwart/*_test.go` (testes de integração que exercem estes payloads).
