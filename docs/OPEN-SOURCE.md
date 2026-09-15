# Abertura do código — Community e Pro

> Estado: **proposta para decisão** (2026-09-15). Documento vivo. Depois das decisões da §8 passa a
> plano de execução, uma fase de cada vez (lei §6.1).

## 0. Resumo

O objetivo tem duas metades que a definição de open source não deixa juntar:

1. código público, que qualquer empresa pode instalar e usar internamente;
2. proibição de o oferecer a terceiros como serviço.

A [Open Source Definition](https://opensource.org/osd) (critério 6) proíbe qualquer restrição ao
campo de utilização. Uma licença que impede a revenda como serviço **não é open source** por
definição, e chamar-lhe open source é a forma mais rápida de perder a confiança da comunidade que se
quer atrair. Há duas saídas honestas:

| Caminho | Licença do núcleo | O que se ganha | O que se perde |
|---|---|---|---|
| **A. Source-available com conversão** (decidido) | BUSL-1.1 | Proíbe legalmente o uso concorrente (SaaS de terceiros); cada versão converte-se em Apache 2.0 quatro anos depois de publicada | Não pode usar a expressão "open source"; diz-se "source-available" ou "fair source" |
| **B. Open source** | AGPL-3.0 | Selo OSI, listagens (awesome-selfhosted, distribuições), máxima credibilidade | Qualquer um pode revender como serviço, desde que publique as alterações; o travão é apenas prático |

**Decisão (2026-09-15): caminho A com BUSL-1.1**, com cláusula de uso adicional que permite produção
interna e proíbe o serviço concorrente. A conversão automática dá a garantia que a comunidade pede: o
código nunca fica preso, a versão de 2026 é Apache 2.0 em 2030. É o modelo da MariaDB, HashiCorp e
CockroachDB.

Em cima do núcleo, uma edição **Pro** a 1 €/utilizador/mês, com código no mesmo repositório em
`ee/` sob licença comercial própria e ativação por chave (modelo Cal.com/PostHog/GitLab). O
**Manager** (consola do operador do SaaS) sai do repositório público para um repositório privado.

## 1. Licença

### 1.1 Comparação

| Licença | OSI | Proíbe SaaS concorrente | Conversão | Notas |
|---|---|---|---|---|
| **FSL-1.1-ALv2** (Functional Source License) | não | sim ("Competing Use") | Apache 2.0 após 2 anos, por versão | Texto curto (uma página), permite uso interno, não comercial e serviços profissionais. Desenhada para empresas SaaS. |
| BUSL-1.1 (Business Source License) | não | sim, via *Additional Use Grant* redigido por nós | licença à escolha após ≤4 anos | Mais conhecida (HashiCorp, MariaDB, CockroachDB), mais parametrizável, texto mais longo e mais ambíguo no que é "produção". |
| ELv2 (Elastic License) | não | sim (serviço gerido) | nunca | Sem conversão, pior receção. |
| SSPL | não | indiretamente | nunca | Rejeitada pela OSI e pela Debian/Fedora. Evitar. |
| AGPL-3.0 | sim | não | — | Copyleft de rede: quem opera o serviço tem de publicar alterações. Travão prático, não legal. |
| Commons Clause / CC BY-NC | não | sim | nunca | Redação problemática (Commons Clause) ou inadequada a software (CC). Evitar. |

### 1.2 Decisão e parâmetros

**Núcleo (Community): BUSL-1.1.** O texto da licença é fixo (MariaDB, versão 1.1) e não se altera;
o que é nosso são os cinco parâmetros do cabeçalho do `LICENSE.md`:

```
Licensor:             Beskos, Lda.
Licensed Work:        Beskos <versão>
                      The Licensed Work is (c) 2026 Beskos, Lda.
Additional Use Grant: You may make production use of the Licensed Work, including
                      for internal business purposes and for providing professional
                      services around it, provided that such use does not include a
                      Competing Offering. A "Competing Offering" is making the
                      Licensed Work, or a substantial part of it, available to third
                      parties as a hosted, managed or embedded service, whether paid or
                      free, in a way that gives those third parties the functionality of
                      the Licensed Work. Hosting the Licensed Work for a single
                      organisation's own users, including its affiliates, is not a
                      Competing Offering.
Change Date:          <data de publicação desta versão + 4 anos>
Change License:       Apache License, Version 2.0
```

Notas de redação:

- A BUSL exige que o *Additional Use Grant* seja uma **permissão**, não uma proibição; a proibição
  entra como condição da permissão. A redação acima segue esse formato.
- Definir "Competing Offering" pela **forma de disponibilização** (a terceiros, como serviço) e não
  por "concorre com os produtos da Beskos". A redação da HashiCorp usou o segundo critério e a
  ambiguidade custou-lhe a comunidade e o fork OpenTofu. A nossa não depende de saber o que a Beskos
  vende.
- "Single organisation's own users, including its affiliates" cobre o caso que se quer permitir: uma
  empresa, ou um grupo de empresas, a operar o Beskos para si. Um integrador que instala e mantém o
  Beskos na infraestrutura do cliente também está coberto (serviços profissionais).
- **Change Date por versão.** Cada release leva a sua data: a versão publicada em 2026-10 converte em
  2030-10, a de 2028-10 em 2032-10. Quatro anos é o máximo que a BUSL permite. O `LICENSE.md` é
  atualizado pelo pipeline de release (GoReleaser corre um script que preenche `Licensed Work` e
  `Change Date` a partir da tag), nunca à mão; uma versão sem data herda a anterior, o que é um
  bug a evitar.
- **Change License Apache 2.0**: permissiva, com cláusula de patentes, compatível com tudo o que
  usamos. MPL-2.0 seria a alternativa se se quisesse copyleft ao nível do ficheiro após a conversão.
- Antes de publicar, uma revisão jurídica do parágrafo (1 a 2 horas de um advogado de TI; a MariaDB
  publica FAQ e exemplos aprovados em <https://mariadb.com/bsl-faq-adopting/>).

**Pro:** `ee/LICENSE` com a "Beskos Enterprise License": uso permitido apenas com chave válida,
proibida a alteração ou contorno do mecanismo de licença, código visível para auditoria. Base: a
*Cal.com Commercial License* ou a *PostHog Enterprise License*, ambas curtas e testadas.

**Marca:** `TRADEMARK.md`. A licença cobre o código, não o nome. Registar **Beskos** no EUIPO
antes do lançamento (≈ 850 € por classe, 4 a 6 meses). É o que impede alguém de operar o
"Beskos hospedado por X" mesmo dentro do que a licença permite.

**Contribuições:** CLA (Contributor License Agreement) via CLA Assistant no GitHub. Sem CLA, uma
contribuição de terceiros não pode entrar em `ee/` nem ser relicenciada. DCO não basta aqui.

**Cabeçalho SPDX** em todos os ficheiros: `// SPDX-License-Identifier: BUSL-1.1` no núcleo e
`// SPDX-License-Identifier: LicenseRef-Beskos-Enterprise` em `ee/`. Verificado no CI.

**Comunicação:** "source-available, converts to Apache 2.0 after four years". Nunca "open source".

### 1.2.1 Licenciamento dual: o serviço concorrente compra a licença comercial

A BUSL foi desenhada para isto. O texto padrão termina com "For information about alternative
licensing arrangements for the Licensed Work, please visit: <URL>"; preenche-se com
`https://beskos.com/licensing`. Quem quer fazer o que a BUSL proíbe não fica bloqueado: compra o
direito. Ficam por isso **três licenças** sobre o mesmo código, e convém não as confundir:

| Licença | Quem | Dá o direito a | Preço |
|---|---|---|---|
| BUSL-1.1 (`LICENSE.md`) | qualquer pessoa ou empresa | usar, alterar, correr em produção para a própria organização | grátis |
| Beskos Enterprise License (`ee/LICENSE`) | clientes self-hosted | ativar as funcionalidades Pro em `ee/` com chave | 1 €/utilizador/mês |
| **Beskos Commercial License** (contrato) | quem quer oferecer o Beskos a terceiros como serviço, revender, ou embutir num produto | o que a BUSL proíbe: *Competing Offering*, marca branca, OEM | negociado; ver abaixo |

**Modelo de preço da Commercial License** (proposta, decidir em F0):

- **Hosting partner:** percentagem da receita (15 a 20 %) com mínimo anual, ou preço por utilizador
  final ativo (2 a 3 €/mês) que já inclui o Pro. Auditoria anual por declaração.
- **OEM / embedded:** valor fixo anual por produto, escalonado por número de instalações.
- Inclui sempre: direito de usar `ee/` sem limite de lugares, acesso antecipado a releases, canal de
  suporte, e o direito de **não** publicar as alterações (que a BUSL também não obriga). Não inclui a
  marca: o parceiro opera com o nome dele ou com "powered by Beskos" mediante `TRADEMARK.md`.
- O contrato é por versão maior e renovável; termina, e o parceiro volta a ficar sujeito à BUSL.

**Consequências para o código e o processo:**

- O CLA é agora indispensável: sem ele não podemos licenciar comercialmente código de terceiros. O
  CLA cede à Beskos o direito de relicenciar (modelo Apache ICLA, ou o da HashiCorp).
- O copyright tem de estar todo em nome da **Beskos, Lda.**, incluindo o do `sync-daemon` e o de
  contribuições antigas (hoje só há dois autores no histórico; obter a cessão do segundo por escrito).
- `beskos.com/licensing`: página com as três licenças, uma tabela "posso fazer X?" com dez casos
  concretos (usar na minha empresa, instalar para um cliente, oferecer como SaaS, fork interno,
  embutir, etc.) e formulário de contacto para a comercial. Escrever em F6.
- Um parceiro comercial recebe a mesma imagem e o mesmo binário; o que muda é o contrato e uma chave
  Pro do tipo `partner` sem limite de lugares. Não há build separado.

### 1.3 Compatibilidade com dependências

| Componente | Licença | Situação |
|---|---|---|
| Stalwart (motor de email) | AGPL-3.0 | Usado apenas via rede (JMAP), sem modificação (lei §5.4). Sem contaminação. |
| OnlyOffice Document Server | AGPL-3.0 | Via rede, container separado. O `onlyoffice-branding-patch.sh` altera a UI deles: publicá-lo cumpre a AGPL. |
| LiveKit + Egress | Apache 2.0 | Sem restrições. |
| PostgreSQL | PostgreSQL License | Sem restrições. |
| Redis ≥ 7.4 | RSALv2/SSPL (8.x: AGPL) | Trocar por **Valkey** no `docker-compose.yml` público. |
| MinIO | AGPL-3.0, edição community descontinuada em 2025 | Só em dev. No compose público usar Garage ou RustFS, ou documentar S3/R2. |
| Go (10 dependências diretas) | MIT/BSD/Apache | Todas permissivas. |
| Frontend (177 pacotes) | MIT/Apache/BSD; BlockNote MPL-2.0; libvips LGPL (só build) | Sem problema. O `package.json` diz `UNLICENSED`: corrigir. |

Automatizar: `go-licenses report` e `license-checker --failOn 'GPL;SSPL'` no CI.

## 2. Estado do repositório (auditoria de 2026-09-15)

O que está bem: CI com gofmt/vet/testes/sqlc/vitest/cargo; verificador de leis em hook; `.gitignore`
cobre `*.env`; **nenhum segredo, chave ou IP** nos ficheiros versionados; histórico sem ficheiros
`.env`.

Esta tabela é a fotografia do dia da auditoria; o progresso real fica nas fases F1–F7 abaixo, que
já resolveram vários dos itens aqui listados (marcados nas respetivas secções).

O que impede a publicação tal como está:

| Problema | Medida | Onde |
|---|---|---|
| Manager (consola do operador) misturado com o produto | 1 616 linhas em `internal/managerapi`, `cmd/manager-api`, app Next `manager/`, metering em `cmd/worker`, migrações `00101`, `00106`… | §3.3 |
| Comentários em português | 241 de 262 ficheiros Go; 145 ficheiros TS | §5 |
| Comentários-diário ("bug 24/07/2026", "decisão desta sessão", "M2 do manager, ver docs/manager.md §3.5") | 194 no backend, 40 no frontend | §5 |
| Densidade de comentário | 12,1 % das linhas Go, 9,2 % TS; ficheiros com 25–36 % | §5 |
| Ficheiros acima das 600 linhas (lei §1.4) | `chat_handlers.go` 1 508; `wiki_handlers.go` 729; `files_handlers.go` 723; `MailView.tsx` 832; `backend.ts` 818; `MeetPresentation.tsx` 671; `FilesView.tsx` 607 | §4 F4 |
| `internal/httpapi` é um monólito | 26 239 linhas num só pacote | §4 F4 |
| Pacotes sem testes | `auth`, `chat`, `config`, `mailbox` | §4 F4 |
| Nome do módulo | `mailin/backend` (190 imports), crate `mailin-sync` | §4 F4 |
| Marca embutida no código | `beskos.com` em 8 sítios do backend (`config.go`, `mail_alerts.go`, `onlyoffice_branding.go`, `notify/email.go`, `cmd/api/main.go`…), "Powered by Beskos" em `Guest.tsx`/`Booking.tsx`, `dns-beskos.zone` | §4 F3 |
| Dados pessoais | e-mail pessoal do fundador semeado na migração `00106_manager_admin.sql` | §4 F1 |
| Lixo versionado | `deploy/__pycache__/*.pyc`, `frontend/tsconfig.tsbuildinfo` | §4 F1 |
| Gitlink para repositório privado | `landing` (commit `f2b33ff`) | §4 F1 |
| Documentação interna | `project.md` (brief), `docs/manager.md`, `AVALIACAO-PLATAFORMA-PRODUCAO.md`, `ROADMAP*.md`, `GAPS-FUNCIONALIDADES.md`, `landing-europa.md`, `deploy/provision/*` (CapRover nosso) | §4 F1 |
| Documentação toda em português | 13 documentos em `docs/`, README, `CLAUDE.md` | §4 F6 |
| Só 2 idiomas na app | `pt-BR`, `en` (a landing tem 8) | opcional |

## 3. Arquitetura Community / Pro / Cloud

### 3.1 Três camadas, dois repositórios

```
github.com/beskosorg/beskos         (público, BUSL-1.1 + ee/ comercial)
├── backend/                         núcleo Go
│   └── ee/                          Pro: só compila com -tags pro
├── frontend/                        webmail + painel
│   └── src/ee/                      Pro: componentes ativados por capability
├── sync-daemon/                     motor Rust (MIT hoje; alinhar com o núcleo)
├── deploy/                          docker-compose de referência (self-hosted)
└── docs/                            em inglês

github.com/beskosorg/beskos-cloud   (privado)
├── manager/ + cmd/manager-api       consola do operador
├── billing Stripe, signup, waitlist, metering, SES por tenant
├── licensing/                       emissão e renovação de chaves Pro
└── deploy prod (CapRover, secrets, zona DNS, provision/)
```

Regra: **o repositório privado importa o público, nunca ao contrário.** `beskos-cloud` é um módulo
Go que depende de `github.com/beskosorg/beskos/backend` e acrescenta os seus binários. Assim o
produto público continua a ser exatamente o que corremos em produção.

### 3.2 Distribuição das funcionalidades (proposta a validar)

| Community (gratuito, sem limite de utilizadores) | Pro (1 €/utilizador/mês) | Cloud (só nós) |
|---|---|---|
| Email (Stalwart), calendário, contactos, Drive, chat, Wiki, Kanban, "Hoje" | Gravação e transcrição de reuniões (Egress + montagem) | Manager |
| Meet sem gravação | Marca própria: logótipo, cores, domínio, sem "Powered by", tema OnlyOffice | Billing Stripe, planos, IVA |
| Edição office (OnlyOffice CE) | Registo de auditoria e políticas de retenção | Signup self-service, lista de espera |
| Assistente de IA com a chave do cliente (DeepSeek/OpenAI) | Agentes de IA de organização (`AssistantAgentsSection`) | Metering, relatórios, alertas |
| RBAC, grupos, convites, MFA (TOTP/SMS) | Políticas de segurança da organização (MFA obrigatório, sessões, IP) | SES por tenant, reputação |
| Migração IMAP/ficheiros | Migração assistida em massa (vários domínios) | |
| App desktop e sincronização de pastas | Suporte prioritário, SLA | |
| i18n pt-BR/en | | |

Critério: fica em Pro o que uma empresa **paga sem hesitar** e um particular ou uma equipa pequena
não sente falta. Nada que seja segurança básica vai para Pro.

### 3.3 Mecanismo técnico

**Backend.** Novo pacote `internal/edition` com:

```go
type Capabilities struct {
    Recording, Branding, AuditLog, OrgPolicies, Agents bool
    Seats  int        // 0 = ilimitado (Community)
    Expiry time.Time
}
func Load(ctx context.Context, licenseKey string) (Capabilities, error)
```

Community devolve tudo `false`, `Seats: 0`. A chave Pro é um JSON assinado com **Ed25519**
(`org`, `seats`, `expires_at`, `features`), verificado **offline** com a chave pública embutida;
período de tolerância de 14 dias após a expiração; renovação por download no portal. Os handlers
Pro vivem em `backend/ee/...` com `//go:build pro` e registam-se em `httpapi` através de um
`func RegisterEE(r chi.Router, deps)`; sem a tag, o ficheiro `ee_stub.go` regista nada. Um só
binário na imagem publicada, compilado com `-tags pro`: a Community é o mesmo binário sem chave.

**Frontend.** `GET /api/edition` devolve as capabilities. Os componentes Pro ficam em
`src/ee/` e só se montam com a capability ligada; sem chave aparece o cartão "disponível na edição
Pro" com ligação ao portal. Um só bundle.

**Planos.** A tabela `plans` fica, mas na Community é semeada com uma única linha `self_hosted`
sem limites. `billing_handlers.go`, `signup.go`, `plans_catalog.go` (Stripe) migram para
`beskos-cloud`; a Community tem as rotas de billing desligadas por ausência de `STRIPE_*`.

**Remoção do Manager (checklist).**

1. Mover `manager/`, `cmd/manager-api`, `internal/managerapi` para `beskos-cloud`.
2. Extrair `cmd/worker/metering.go` e a retenção de gravações por plano para um worker do Cloud.
3. Migrações `00101` (seed de planos), `00106` (admin do manager) e as tabelas `operators`,
   `usage_daily`, `waitlist`, `alerts`: squash numa migração inicial limpa do público, e as tabelas
   do operador passam para as migrações do Cloud.
4. `docs/manager.md`, `deploy/provision/`, `dns-beskos.zone`, `secrets.env` e `deploy.py` seguem
   para o privado.
5. `docker-compose.yml` público fica com: stalwart, postgres, valkey, garage (ou S3 externo),
   api, worker, onlyoffice, livekit, caddy. Um comando: `docker compose up`.

## 4. Fases

Estimativas para uma pessoa a tempo inteiro; F3–F6 são paralelizáveis.

### F0 — Decisões (1 semana)

Responder ao que resta da §8, registar as respostas neste documento, abrir o repositório `beskos-cloud`
privado. **Pronto quando:** licença, nome da organização GitHub e tabela §3.2 aprovados.

### F1 — Higiene e segurança do histórico (1 semana) — em curso desde 2026-09-15

Feito: gitleaks sobre os 219 commits (8 alertas: 7 fixtures de testes, 1 chave `sk_live_` da
Stripe no primeiro commit, já fora do HEAD; **rotar na Stripe**); gitleaks no CI com allowlist;
CI corrigido para o ramo `master` (apontava para `main` e nunca correu em push); CodeQL e
Dependabot; `deploy/__pycache__` e `tsconfig.tsbuildinfo` deixam de ser versionados;
`scripts/export_public.py` com a lista de exclusão e o relatório de resíduo; repositórios
`beskosorg/beskos` e `beskosorg/beskos-cloud` criados (privados).

~~Cessão de copyright do segundo autor~~ **resolvido:** `katisuhara@gmail.com` é a mesma pessoa
(`dener.php@gmail.com`) noutra configuração local de `git`; sem terceiros no histórico, sem CLA
retroativo necessário. **Chave Stripe `sk_live_...` do primeiro commit:** o utilizador confirmou
que vai rodar na Stripe, mas só no final do processo (antes do lançamento, F9) — decisão dele,
registada aqui para não se perder. Por fazer: rotar a senha do operador cujo hash bcrypt está na
migração 00106; secret scanning e push protection nos repositórios da organização (gratuitos
quando o `beskos` passar a público).


- Correr `gitleaks detect --source . --log-opts=--all` e `trufflehog git` sobre **todo o histórico**.
  Se aparecer um único segredo real, publicar com **histórico novo** (squash num commit inicial) e
  manter o histórico atual só no privado. Sem achados, o histórico pode ir, mas os 217 commits estão
  em português e contam a história interna: recomenda-se de qualquer forma começar limpo.
- Remover: gitlink `landing`, `deploy/__pycache__`, `frontend/tsconfig.tsbuildinfo` (juntar ao
  `.gitignore`), e-mail pessoal da migração `00106`.
- Mover a documentação interna (§2) para o privado; `project.md` transforma-se em `docs/architecture.md`
  público e sem brief comercial.
- Ativar no GitHub: secret scanning, push protection, Dependabot, CodeQL.
- **Pronto quando:** `gitleaks` limpo, `git ls-files` sem nada da lista da §2, CodeQL a correr.

### F2 — Separação Community / Pro / Cloud (3 a 4 semanas) — iniciada em 2026-09-15

Feito: pacote `backend/internal/edition` (verificação Ed25519 offline de chave Pro, período de
tolerância de 14 dias, `Community()`/`Load()`, 11 testes); endpoint público `GET /api/edition`
ligado ao `Server` de `cmd/api` e `cmd/worker`; tipo `Edition` e `api.edition()` no frontend
(`src/api.ts`). Build, `go vet`, testes do backend (35 pacotes) e `tsc --noEmit` do frontend
verdes; `check_laws.py` limpo.

**Achado ao corrigir o ramo do CI (F1):** o `ci.yml` nunca tinha realmente corrido — apontava
para `main`, o repositório usa `master`. A primeira execução real revelou três avarias
pré-existentes, nenhuma relacionada com este trabalho, escondidas há quem sabe quanto tempo por
trás de um CI que parecia ligado e nunca correu (é o mesmo padrão da lei §2.8/§2.9, agora no
próprio CI): `cargo fmt` por aplicar em `sync-daemon` e em `frontend/src-tauri` (corrigido);
`npm ci` sem `--legacy-peer-deps` no job Frontend, que o `Makefile` já usa mas o workflow não
(corrigido); e um teste (`assistant/api.test.ts`) que assumia o idioma detectado pt-BR e falhava
sempre que o executor resolve `en` (o caso desta máquina e provavelmente o dos runners do
GitHub) — corrigido fixando o idioma no `beforeEach`. Pendência nova, fora do âmbito desta
correção: `sync-daemon` e `frontend/src-tauri` têm ~27 strings de UI em português (mensagens da
bandeja do sistema) que não passam pelo i18next do frontend por serem Rust nativo — candidatas a
`lang:ok` explícito ou a uma solução de i18n própria na F4/F6.

Falta o grosso da F2: emitir chaves reais (repositório privado, ainda sem esse serviço — F8);
mover `manager/`, `cmd/manager-api`, `internal/managerapi`, `cmd/ses-tenants` e a metering do
worker para `beskos-cloud`; extrair as rotas de billing Stripe; ficheiros `ee_stub.go` e
`//go:build pro` para as funcionalidades Pro da tabela §3.2; ajustar `docker-compose.yml` público
(Valkey em vez de Redis, alternativa ao MinIO). Continua a executar a §3.3 abaixo. Ordem: `internal/edition` e stub `ee/` → mover Manager e billing para
`beskos-cloud` → compose público a arrancar do zero numa máquina limpa → mover funcionalidades Pro
para `ee/` uma a uma, com teste que prova que sem chave a rota devolve `403 edition_required`.
**Pronto quando:** `go build ./...` e `go build -tags pro ./...` verdes; `docker compose up` em
VM limpa dá um Beskos funcional em menos de 10 minutos; produção continua a correr a partir do
`beskos-cloud`.

### F3 — Desacoplar a marca (1 semana)

`BRAND_NAME`, `BRAND_URL`, `PUBLIC_URL`, `NOTIFY_FROM`, `SUPPORT_EMAIL` em `internal/config`, sem
defaults `beskos.com`. "Powered by" passa a capability (`Branding` desliga-o). Tema OnlyOffice e
`onlyoffice-branding-patch.sh` passam a opcionais. O `mailprobe` deixa de assumir `mail.beskos.com`.
**Pronto quando:** `git grep beskos.com -- backend frontend/src` só devolve testes e o ficheiro de
defaults do Cloud.

### F4 — Qualidade do código (3 semanas) — ferramentas prontas, limpeza por fazer

- **Comentários:** `scripts/comment_audit.py` (feito, `make check` corre-o em modo relatório)
  conta comentários datados, com referência a documento interno, ou a número de fase. Contagem
  em 2026-09-15: **301** (77 datados, 33 com referência a `docs/manager.md`/`project.md`, 70 com
  `M2`/`A3b`/`§x`, 121 em tom de diário). Aplicar a política da §5 a todo o repositório até o
  script sair limpo — trabalho real de leitura, não mecanizável.
- **`internal/httpapi/server.go` já foi dividido** (F2, `2687f76`): `Server`/`NewServer` saíram
  para `server_types.go`, `server.go` ficou só com `Router()`. Os outros 6 ficheiros acima de 600
  linhas continuam por dividir. Dividir `internal/httpapi` por domínio (`httpapi/mail`,
  `httpapi/files`, `httpapi/chat`, `httpapi/wiki`, `httpapi/meet`, `httpapi/admin`), mantendo
  `httpapi` só com servidor, middleware e router.
- **Testes:** `auth`, `chat`, `config`, `mailbox` ganham `_test.go`; cobertura mínima no CI (70 % nos
  pacotes de regra de negócio, medida com `go test -cover`).
- **Nomes:** módulo `github.com/beskosorg/beskos/backend`; crate `beskos-sync`; `package.json`
  `beskos-web`.
- **Ferramentas:** `golangci-lint` (errcheck, gosec, staticcheck, revive, misspell), `eslint` +
  `typescript-eslint` no frontend, `cargo clippy -D warnings`. Tudo no CI.
- **CLAUDE.md/AGENTS.md:** as leis passam para `CONTRIBUTING.md` em inglês, sem os parágrafos
  de crónica ("Escrita a 31/07/2026 porque…"). Ficam as regras, não as histórias. `CLAUDE.md`
  aponta para `CONTRIBUTING.md`.
- **Pronto quando:** `comment_audit.py` a zero, `check_laws.py` sem exceções, lint verde.

### F5 — Segurança (2 semanas, antes de publicar) — em curso

Feito (2026-09-15): `govulncheck` no CI (Go) — encontrou logo à primeira 21 CVEs da stdlib
corrigidos pelo pin em `1.25.7`, com caminho de chamada real (TLS, `net/url`, `net/http`,
`encoding/xml`, `encoding/asn1`); resolvido subindo para `1.25.13` (`6ded514`). `cargo audit` nos
dois workspaces Rust — apanhou um `rustls` desatualizado (RUSTSEC-2026-0285), corrigido só no
`Cargo.lock`. `npm audit --audit-level=critical` bloqueia (limpo); `--audit-level=high` fica
report-only (12 achados pré-existentes, sem correção publicada a montante). `SECURITY.md` escrito
(§ pendente de decisão: orçamento e data do pentest).

Por fazer: modelo de ameaças (`docs/security/threat-model.md`); verificação automática da lei
§5.1 (toda query sqlc sobre tabela com `tenant_id` filtra por ele); `gosec`; fuzzing Go
(`filecrypt/format.go`, `blobgw/sigv4.go`, cliente JMAP); revisão de rate limit/CSRF/timeouts;
pentest externo (orçamento e data por decidir, §8).
- **Pronto quando:** CI de segurança verde (✅), modelo de ameaças revisto, pentest sem críticos abertos.

### F6 — Documentação (2 a 3 semanas) — núcleo escrito

Feito (2026-09-15), tudo em inglês: `README.md` (fica em `docs/README-oss.md`, ligado ao export
por `OVERRIDES`), `docs/getting-started.md`, `docs/self-hosting.md`, `docs/architecture.md`,
`docs/configuration.md` (**gerado** de `internal/config` por `scripts/gen_env_docs.py`, não
desatualiza), `docs/editions.md` (a tabela Community/Pro pública, sem depender deste ficheiro
interno), `docs/FUNCTIONALITY.md` (mapa por área, sem tabela de status — a interna diverge do
código e publicar essa divergência seria pior que não publicar nada), `CONTRIBUTING.md`,
`CODE_OF_CONDUCT.md`, `SECURITY.md`, `TRADEMARK.md`, `LICENSE.md`, `ee/LICENSE`.

Por fazer: site publicado (MkDocs Material/Docusaurus em `docs.beskos.com`); `docs/api/` (OpenAPI
das rotas chi); `docs/pro/`; `CHANGELOG.md`; `docs/adr/` com as decisões estruturais existentes.

**Pronto quando:** uma pessoa que nunca viu o projeto instala e envia um email só com a
documentação, sem perguntar nada (testar com alguém de fora).

### F7 — Infraestrutura do projeto aberto (2 semanas) — parcial

Feito: `.goreleaser.yaml` (binários `api`/`worker`/`bootstrap`, linux+darwin, amd64+arm64;
validado com `goreleaser release --snapshot --clean`, release como `draft`);
`.github/workflows/release.yml` (dispara em tag `v*`); templates de issue/PR;
`.github/CODEOWNERS` (aponta para uma equipa `@beskosorg/maintainers` a criar).

Por fazer: imagens multi-arch em `ghcr.io/beskosorg/{api,worker,web}`, assinadas com **cosign**,
SBOM (syft) anexado — precisam de credenciais de registo que este repositório ainda não tem
configuradas; imagem `edge` a partir de `master`; GitHub Discussions; roadmap público (GitHub
Projects); CLA Assistant; branch protection; política de suporte (Community sem SLA, Pro com).
- **Pronto quando:** `git tag v0.1.0` produz release completa sem intervenção manual.

### F8 — Licenciamento e venda do Pro (2 semanas, no `beskos-cloud`)

- Serviço de chaves: par Ed25519 (privada no Cloud, pública embutida no binário), emissão por
  organização com `seats` e validade de 12 meses, renovação automática por webhook Stripe.
- Portal em `beskos.com/pro`: comprar (Stripe Checkout, 1 €/utilizador/mês, IVA por país com o
  código já existente em `billing/vat.go`), descarregar chave, gerir lugares.
- Contagem de lugares: utilizadores ativos no tenant; acima do limite, aviso no painel 30 dias, depois
  as funcionalidades Pro desligam-se, nunca o produto.
- Telemetria **opt-in** apenas, documentada (versão, número de utilizadores, funcionalidades ativas).
- **Pronto quando:** compra → chave → ativação num self-hosted de teste funciona ponta a ponta.

### F9 — Lançamento (1 semana)

Anúncio no blog (o que é, porquê source-available, o que é Pro, como licenciar um serviço), Hacker News ("Show HN"), r/selfhosted,
Product Hunt, PR em `awesome-selfhosted` (aceita source-available com etiqueta). Landing ganha a
secção "Self-hosted" e a página de preços ganha a coluna Community/Pro.

**Total:** 16 a 20 semanas para uma pessoa; 10 a 12 com duas (F3–F6 em paralelo com F2).

## 5. Política de comentários

Quem lê o repositório vai concluir, pelos comentários, se o código foi pensado ou gerado. Os
comentários atuais têm três marcas de escrita por agente: contam a história ("bug 24/07/2026:
Growth sem BRL mostrava R$"), apontam para documentos internos ("docs/manager.md §3.5, M3") e
explicam o óbvio. Regras, verificadas por `scripts/comment_audit.py` e aplicadas em F4:

1. **Inglês** (lei §4.6), sem exceção fora de `lang:ok`.
2. **Godoc em todo o símbolo exportado**, uma frase, começa pelo nome. Nada mais é obrigatório.
3. Um comentário no corpo diz **porquê**, nunca **o quê**. Se o código precisa de um comentário
   para dizer o que faz, muda-se o nome ou extrai-se a função.
4. **Sem datas, sem "sessão", sem "decisão", sem números de fase (M2, A3b), sem referências a
   documentos internos.** Essa informação vive no `git log`, nos ADR e no CHANGELOG.
5. **Sem cabeçalhos de ficheiro** narrativos. `package billing` leva um godoc de duas linhas no
   `doc.go`; os outros ficheiros do pacote não repetem.
6. Um `TODO` tem issue: `// TODO(#123): ...`.
7. Alvo de densidade: **≤ 6 %** de linhas de comentário por pacote (hoje 12,1 %).

Exemplo real, `internal/billing/plans.go`:

```go
// antes
// Plan é uma faixa de assinatura. O preço é POR USUÁRIO/mês (por seat).
// Desde a M2 do manager a fonte de verdade é a tabela `plans` (editável no painel do operador);
// o catálogo em código abaixo é o FALLBACK (tabela vazia/erro) e o seed da migração 00101.

// depois
// Plan is a subscription tier. Prices are per seat per month; the plans table
// is the source of truth and this catalog is the fallback when it is empty.
```

## 6. Riscos

| Risco | Mitigação |
|---|---|
| Reação negativa por não ser OSI ("open source de mentira") | Nunca usar a expressão. Dizer "source-available, Apache 2.0 após 4 anos" em todo o lado. |
| Fork comercial depois da conversão a Apache (4 anos) | É o preço da honestidade da cláusula; a marca e o ritmo de desenvolvimento são a defesa. |
| Alguém usa o nome sem operar o serviço (agregadores, "Beskos hosting") | Registo EUIPO e `TRADEMARK.md` antes do anúncio. |
| Contribuidor externo recusa CLA | Aceitar só sob CLA; sem CLA a contribuição fica de fora. Explicar no `CONTRIBUTING.md`. |
| Carga de suporte da Community | Discussions e FAQ; sem promessas de resposta; canal pago é o Pro. |
| Fuga de segredo no histórico não detetada | Histórico novo na publicação (F1). |
| Distribuir imagens de terceiros AGPL (OnlyOffice, Stalwart) | Não redistribuir: o compose aponta para as imagens oficiais deles. |
| Pro a 1 € não paga suporte com SLA | Mínimo de 10 lugares por chave, ou SLA só a partir de N lugares. Decidir em F0. |
| Produção diverge do público depois da separação | Regra §3.1: o Cloud importa o público; deploy de produção constrói a partir dos dois. |

## 7. Práticas a manter depois do lançamento

- Uma fase por vez continua (lei §6.1); cada release tem changelog e notas de migração.
- Todo o PR passa CI, lint, `check_laws`, `comment_audit`, licenças e uma revisão.
- Vulnerabilidades: advisory, correção em todas as versões suportadas (última minor e a anterior),
  CVE pedido pelo GitHub.
- Roadmap público trimestral; decisões estruturais como ADR no repositório antes de código.
- Reunião de comunidade mensal em Meet (o nosso), gravada e publicada.

## 8. Decisões pendentes

1. ~~Licença do núcleo~~ **Decidido 2026-09-15: BUSL-1.1 com licenciamento dual** (§1.2 e §1.2.1). Falta a revisão jurídica do *Additional Use Grant* e do contrato comercial.
1b. **Preço da Commercial License:** percentagem de receita ou por utilizador final; mínimo anual.
2. ~~Nome da organização~~ **Decidido 2026-09-15: `beskosorg`**, repositórios `beskosorg/beskos`
   (público na F9, privado até lá) e `beskosorg/beskos-cloud` (privado). Ambos criados. O nome
   interno `mailin` desaparece na F4.
3. ~~Tabela Community/Pro~~ **Aprovada 2026-09-15 tal como proposta na §3.2.** Destrava a F2.
4. ~~Histórico~~ **Decidido 2026-09-15: histórico novo.** O repositório atual continua a ser o de
   trabalho até ao fim da F2 e passa então a `beskos-cloud`; o público nasce por exportação
   (`scripts/export_public.py`) num único commit inicial.
5. **Pro:** mínimo de lugares por chave; validade 12 meses; tolerância 14 dias.
6. **Telemetria** opt-in: sim ou não.
7. **Pentest externo:** orçamento e data.
8. **sync-daemon:** manter MIT ou passar a BUSL-1.1 como o núcleo.
