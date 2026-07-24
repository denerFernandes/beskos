# Wiki / Base de Conhecimento (estilo Confluence) — Especificação

> Estado: **rascunho de desenho** (Fase 0). Documento vivo. Segue as leis do [CLAUDE.md](../CLAUDE.md)
> e as restrições do [project.md](../project.md). Prosa em PT-BR; **código, logs e `error_code`
> sempre em inglês** (lei 4.6). Uma fase de cada vez (§9): não começar a Fase N+1 antes da N testada.

## 1. O que é e porquê

Uma app de **base de conhecimento** para a organização — uma wiki interna no espírito do Confluence:
espaços, páginas hierárquicas, edição rica colaborativa, histórico de versões, e controlo de acesso
por **grupo** ou **pessoas específicas**. Além de ser útil por si, a wiki **alimenta o assistente de
IA como fonte de RAG**: o assistente passa a responder perguntas com base no conhecimento escrito da
empresa — **sempre respeitando as permissões de quem pergunta**.

### Objetivos
- **Escrever e organizar** conhecimento: espaços → páginas → subpáginas (árvore), com editor embutido
  parecido ao do Confluence (blocos, títulos, listas, tabelas, código, callouts, imagens, menções, links).
- **Colaborar**: várias pessoas editam; histórico de versões com diff e restauro; comentários.
- **Controlar acesso**: por espaço e por página, a grupos de utilizadores ([[user_groups]]) ou a
  pessoas específicas, com papéis (ver / comentar / editar / gerir).
- **Alimentar a IA (RAG)**: indexar o conteúdo e dar ao assistente uma ferramenta `wiki.search` que
  recupera trechos relevantes **filtrados pela permissão do utilizador** e responde com citação da página.
- **Ser governável**: o admin liga/desliga a app (sistema de apps existente); desligá-la tira a app do
  menu **e** corta o acesso da IA a este conhecimento (ferramenta + agente), como as outras apps.

### Não-objetivos (v1)
- Não é um CMS público nem um site externo (sem publicação anónima na v1; ver §11 futuro).
- Não substitui o editor de documentos Office (OnlyOffice) — a wiki é conteúdo **nativo** e pesquisável,
  não ficheiros `.docx`. Anexos e "inserir ficheiro do Drive" são integração, não o corpo da página.
- Sem edição concorrente em tempo real (CRDT/OT) na v1 — usamos bloqueio otimista por versão (§6).

## 2. Restrições inegociáveis (recap do project.md §2 e CLAUDE.md)
- **Multi-tenant**: toda query filtra `tenant_id` do utilizador autenticado (contexto), nunca do request
  (lei 5.1). Toda leitura/escrita de página verifica **posse/permissão** antes (lei 5.2).
- **Camadas** (lei 1.3): `httpapi` (transporte) → serviço/regra (wiki) → `db` (sqlc). Handlers não montam
  SQL nem regras de permissão inline quando há lógica; extrair para o serviço.
- **Arquivos ≤600 linhas** (mire ≤300); funções curtas (lei 1.4/1.5). Um componente por ficheiro no
  frontend; toda chamada à API passa por `src/api.ts`/`backend.ts` (lei 3.4).
- **i18n obrigatório** (lei 4): nada de string visível hard-coded; `pt-BR` default + `en` em paridade;
  erros da API voltam com `error_code` e o frontend traduz por `server.<code>`.
- **Segurança da IA** (project.md §6): o conteúdo lido é **dado não confiável**, nunca instrução; o
  utilizador nunca vê no RAG uma página a que não teria acesso na UI (a permissão é a mesma superfície).
- **Sem dependências desnecessárias** (lei 2.5): cada lib nova (ex.: editor) precisa de justificação.

## 3. Modelo de domínio (proposta de schema)

Novas tabelas (nomes finais a confirmar na Fase 1). Todas com `tenant_id UUID NOT NULL REFERENCES
tenants(id)` e índice por `tenant_id`. IDs `UUID DEFAULT gen_random_uuid()`.

### 3.1. `wiki_spaces` — espaços (topo da árvore)
| coluna | tipo | nota |
|---|---|---|
| `id` | uuid pk | |
| `tenant_id` | uuid | tenant do dono |
| `key` | text | chave curta única no tenant (ex.: `ENG`, `RH`) para URLs bonitas |
| `name` | text | nome exibível |
| `description` | text | opcional |
| `icon` | text | emoji/ícone |
| `owner_user_id` | uuid | criador/dono |
| `visibility` | text | `restricted` \| `org` \| `public` — `org` = todos do tenant veem; `public` = link anónimo só-leitura (ver §5) |
| `public_slug` | text null | slug do link público (quando `visibility=public`); único no tenant |
| `ai_indexed` | bool | se o conteúdo alimenta o RAG (default true; admin/dono controla) |
| `created_at` / `updated_at` | timestamptz | |

`UNIQUE (tenant_id, key)`. Cada linha desta tabela **é uma wiki** — podem existir **N** por tenant.

### 3.2. `wiki_pages` — páginas (árvore dentro do espaço)
| coluna | tipo | nota |
|---|---|---|
| `id` | uuid pk | |
| `tenant_id` | uuid | |
| `space_id` | uuid → wiki_spaces | |
| `parent_id` | uuid → wiki_pages null | árvore (raiz = null) |
| `title` | text | |
| `slug` | text | derivado do título, único por espaço para URL |
| `body_json` | jsonb | documento do editor (ProseMirror/TipTap) — a **fonte da verdade** |
| `body_text` | text | texto puro derivado, para pesquisa/RAG e `tsvector` |
| `body_html` | text | render cacheado para leitura rápida (regenerado ao gravar) |
| `position` | int | ordem entre irmãos |
| `status` | text | `draft` \| `published` (a wiki mostra published; draft só a quem edita) |
| `author_user_id` | uuid | último editor |
| `version` | int | inteiro incremental (bloqueio otimista, §6) |
| `search_tsv` | tsvector | GENERATED de `title`+`body_text` (índice GIN) — RAG Fase 1 |
| `created_at` / `updated_at` | timestamptz | |

`UNIQUE (space_id, slug)`. Índices: `(tenant_id, space_id, parent_id, position)`, GIN em `search_tsv`.

### 3.3. `wiki_page_versions` — histórico
Cada gravação grava a versão anterior: `id, page_id, tenant_id, version, title, body_json, body_text,
author_user_id, created_at`. Permite diff e restauro (§6). Retenção configurável (§8).

### 3.4. Permissões — `wiki_space_grants` e `wiki_page_grants`
> **Grounding**: o padrão de partilha já existente é **por equipa** — `contact_group_shares`
> (`00023_contacts_unified.sql:28`) partilha um *grupo de recursos* com um `user_group` (equipa),
> chave `(group_id, user_group_id)`; o dono mantém editar/apagar, a equipa recebe ver. A tabela
> mínima estilo casa seria `wiki_space_shares(space_id, user_group_id)`. O pedido do utilizador inclui
> "**ou determinadas pessoas**", que o precedente não cobre (só equipas) — por isso generalizamos com
> `subject_type` (`group`|`user`) e `role`, uma extensão pequena e explícita do padrão. Uma linha por
> concessão:
| coluna | tipo | nota |
|---|---|---|
| `id` | uuid pk | |
| `tenant_id` | uuid | |
| `space_id` (ou `page_id`) | uuid | alvo |
| `subject_type` | text | `user` \| `group` \| `org` (toda a organização) |
| `subject_id` | uuid null | → mail_users.id ou user_groups.id; **null** quando `subject_type=org` |
| `role` | text | `viewer` \| `commenter` \| `editor` \| `manager` (ver §5) |

`subject_type=org` é o "geral dentro da organização" (todo o tenant, sem escolher grupo). O acesso
**público** (anónimo, só-leitura) **não** é um grant — vive em `visibility=public` + `public_slug` (§5.4).

Página **herda** do espaço; um grant na página **acrescenta/eleva** (nunca reduz abaixo do espaço na v1;
regra explícita em §5). `UNIQUE (space_id, subject_type, subject_id)` idem por página.

### 3.5. `wiki_comments` (v1.1)
`id, tenant_id, page_id, author_user_id, body_text, anchor (json opcional p/ comentário em linha),
created_at`. Comentar exige papel ≥ `commenter`.

### 3.6. `wiki_attachments`
Liga ficheiros (MinIO/Drive existente) a uma página: `id, tenant_id, page_id, file_id, ...`. Reusa o
storage atual — não reinventa upload.

### 3.7. `wiki_chunks` — índice semântico (RAG, §7.2)
`id, tenant_id, page_id, space_id, chunk_index, chunk_text, embedding vector(D)`. Um trecho por linha;
recomputado por um worker ao gravar a página (idempotente). Índice **pgvector** (`vector_cosine_ops`,
ivfflat/hnsw). Exige `CREATE EXTENSION IF NOT EXISTS vector` (migração da Fase 3). `D` = dimensão do
provedor de embeddings escolhido (§7.2).

## 4. Editor embutido (estilo Confluence)

### 4.1. Escolha da lib
Proposta: **TipTap** (sobre ProseMirror) — React-first, extensível, produz **JSON estruturado**
(ideal para versionar, renderizar e extrair texto para RAG). Alternativas ponderadas: Lexical (Meta,
mais novo/menos maduro em extensões), Slate (mais baixo-nível, mais trabalho), Quill (HTML-cêntrico,
pior para blocos). **Decisão estrutural → confirmar com o utilizador antes de instalar** (lei 6.2);
justificação da dependência: um editor de blocos rico é inviável de reescrever à mão (lei 2.5 aceita).
> **Grounding (confirmado)**: **não existe nenhuma lib de editor rico** no `package.json`
> (sem tiptap/prosemirror/slate/quill/lexical). A edição de documentos atual é o **OnlyOffice
> Document Server** ([EditorView.tsx:28](../frontend/src/webmail/EditorView.tsx)) — editor full-screen
> pesado, keyed por `file_id` + sessão JWT assinada; **não serve** para edição inline de blocos estilo
> Confluence. Dele só se reaproveita o padrão de *sessão assinada + tema/idioma*, não a UX. Logo, o
> editor da wiki é uma dependência **nova** (a confirmar em §12).

### 4.2. Blocos suportados (v1)
Títulos H1–H3, parágrafo, **negrito/itálico/código/link**, listas (ordenada/não/checkbox), citação,
bloco de código (com linguagem), **callout/painel** (info/aviso/sucesso), tabela, imagem/anexo,
divisória, **menção `@pessoa`** e **link `[[página]]`** interno. Slash-menu (`/`) para inserir blocos,
como no Confluence/Notion. Atalhos de markdown ao digitar (`## `, `- `, ``` ``` ```).

### 4.3. Persistência e render
- **Fonte da verdade = `body_json`** (documento ProseMirror). Ao gravar, o backend (ou um util
  partilhado) deriva `body_text` (para pesquisa/RAG) e `body_html` (para leitura rápida sem carregar o
  editor). O render tem de ser **determinístico e seguro** (sanitizar HTML; nada de script no conteúdo).
- Autosave com debounce + gravação explícita; conflito de versão tratado em §6.

## 5. Permissões e acesso

### 5.1. Papéis (por espaço e por página)
- `viewer` — lê.
- `commenter` — lê + comenta.
- `editor` — lê + comenta + edita conteúdo e cria subpáginas.
- `manager` — tudo + gere permissões do espaço/página, apaga, move.

### 5.2. Sujeitos e tipos de acesso (decisão do utilizador)
Quatro alcances, dois tipos de acesso:
- **Pessoa** (`mail_users`) — `subject_type=user`.
- **Grupo/equipa** ([[user_groups]]) — `subject_type=group`.
- **Geral na organização** (todo o tenant) — `subject_type=org` (ou o atalho `visibility=org`).
- **Público só-leitura** — `visibility=public` + `public_slug` (link anónimo, §5.4). Não é grant.

Tipo de acesso = o **papel**: **ver** (`viewer`/`commenter`) ou **editar** (`editor`); `manager` gere.
O dono e os `manager` gerem a lista numa UI de partilha próxima da dos ficheiros (Ver / Editar / Gerir,
+ interruptor "Acesso público só-leitura"). Cada **wiki** (espaço) tem a sua política — há **N** wikis.

### 5.3. Resolução de acesso — padrão SQL `vis` CTE (como já se faz na casa)
> **Grounding**: o acesso a recursos **não é um helper Go** — é feito **em SQL**, sempre com
> `AND tenant_id = $tenant`, via uma CTE de visibilidade. Ex.: `ListContacts`/`GetContact`
> ([queries.sql:507](../backend/db/queries/queries.sql)) usa
> `WITH vis AS (SELECT ... WHERE owner_user_id = $user OR EXISTS (member-of-team-partilhada)) ...`.
> A wiki replica isto: uma página é visível se `owner` **ou** o espaço/página foi partilhado com uma
> equipa (`user_group`) de que o utilizador é membro **ou** um grant direto ao utilizador.

Regra efetiva (composta na query e conferida no serviço para o `needed_role`):
1. `tenant_id` bate (senão 404 — não revelar existência).
2. `space.visibility = org` → todos do tenant têm pelo menos `viewer` (grants elevam).
3. Senão, papel efetivo = **máximo** entre: grants do espaço (user/grupos do user) e grants da página
   (user/grupos). Dono do espaço = `manager`.
4. `needed_role` satisfeito? Sim → segue; não → 403 `wiki.forbidden` (ou 404 se nem `viewer`).

O serviço expõe uma função fina `canAccess(ctx, user, page, needed_role)` que envolve as **mesmas
queries `vis`** usadas para listar — a UI e o RAG partilham a superfície. O assistente nunca recupera
uma página que o utilizador não veria (§7.4). Ponto de segurança crítico.

### 5.4. Acesso público (só-leitura, anónimo)
Uma wiki com `visibility=public` fica acessível por um **link anónimo** (`/w/<public_slug>`), **só
leitura** e **só páginas `published`** (drafts nunca saem). Mesma família do `file_link_access`
([00087](../backend/internal/db/migrations/00087_file_link_access.sql)): a rota pública é separada,
não passa pela auth de sessão, serve `body_html` sanitizado, **não** expõe editar/histórico/comentários
nem entra no RAG do assistente (que é sempre por utilizador autenticado). Ligar/desligar público é do
`manager`/dono. Regenerar o `public_slug` invalida o link antigo.

### 5.5. Multi-tenancy
Toda query recebe `tenant_id` do contexto autenticado (lei 5.1). Nunca confiar em `space_id`/`page_id`
do request sem revalidar posse (lei 5.2). Idempotência em criações onde fizer sentido (lei 5.3). A rota
pública resolve o tenant pelo `public_slug`, nunca por parâmetro do request.

## 6. Versões, edição concorrente e histórico
- **Bloqueio otimista**: o editor guarda a `version` que carregou; no `PUT`, se `version` no servidor
  divergir → 409 `wiki.version_conflict`, e a UI oferece "ver diferenças / recarregar". Simples e
  honesto na v1 (sem OT/CRDT; §1 não-objetivo).
- Cada gravação: `version++`, grava a anterior em `wiki_page_versions`. Histórico com **diff** (texto)
  e **restaurar** (cria nova versão a partir da antiga — não apaga histórico).
- Mostrar "editado por X há Y" e presença leve ("N a ver esta página") reusando o realtime do Chat, se
  barato; senão, adiar.

## 7. Integração com o Assistente de IA (RAG)

O assistente já injeta contexto duradouro (memória, §8.3 do desenho do assistente) via
`RunOptions.Memory` e tem catálogo de ferramentas (Registry). A wiki entra por **duas vias**:

### 7.1. Ferramenta `wiki.search` (recuperação sob demanda — preferida)
Uma ferramenta de leitura no catálogo do assistente: dado um termo/pergunta, devolve os **N trechos**
mais relevantes de páginas **que o utilizador pode ver**, com `page_id`, título, espaço e citação.
- Handler recebe a identidade do utilizador (tenant+user do **contexto**, nunca de parâmetro do modelo
  — project.md §6.2) e chama o serviço de busca com o **mesmo `canAccess`** da UI (§5.3).
- O texto recuperado é **dado não confiável** (§6.1 do assistente): entra como contexto/contéudo, nunca
  como instrução. O prompt do sistema deixa isto explícito.
- A resposta do assistente **cita a página** (link) para o utilizador confirmar a fonte.

### 7.2. Recuperação semântica — **pgvector** (decisão do utilizador)
Abordagem principal: **embeddings + pgvector**. Tabela `wiki_chunks(id, tenant_id, page_id, space_id,
chunk_index, chunk_text, embedding vector(D))` — a página é partida em trechos (por secção/título,
~500–1000 tokens com sobreposição) e cada trecho embeddado. Índice **ivfflat/hnsw** por `vector_cosine_ops`.
- **Worker de (re)embedding idempotente** ao gravar/alterar/apagar página (padrão fila já existente):
  recomputa os chunks dessa página; nunca deixa embeddings órfãos (lei 5.3).
- **Busca** = kNN por cosseno sobre `wiki_chunks`, **filtrado pela permissão de quem pergunta e por
  `tenant_id`** (join com a CTE `vis`, §5.3) e por `ai_indexed=true`.
> **Grounding / infra nova**: **não existe pgvector/embeddings hoje** no projeto — a extensão
> `pgvector` no Postgres é infra nova (aprovada, §12). Docker do Postgres tem de a incluir/ativar
> (`CREATE EXTENSION IF NOT EXISTS vector` numa migração). **Provedor de embeddings** ainda por
> escolher (decisão operacional, não bloqueia o schema — `vector(D)` fixa a dimensão ao provedor): o
> DeepSeek ([deepseek.go](../backend/internal/ai/deepseek.go)) não é forte em embeddings; opções =
> endpoint de embeddings dedicado (chave/URL via `internal/config`, lei 2.6) ou modelo local. Definir
> `D` e o cliente de embeddings no arranque da Fase 3.

### 7.3. FTS como pré-filtro híbrido (opcional, barato)
`tsvector`/GIN sobre `title`+`body_text` (precedente do Chat, [00044_chat_fts.sql](../backend/internal/db/migrations/00044_chat_fts.sql))
pode servir de **pré-filtro** barato antes do kNN, ou de fallback quando os embeddings ainda não
correram. Não é fase própria — acompanha o schema das páginas.

### 7.4. Segurança do RAG (não-negociável)
A recuperação **filtra pela permissão do utilizador que pergunta**, sempre. Nunca indexar num índice
"global" sem o campo de permissão; a query de busca aplica `canAccess`. Espaços/páginas com
`ai_indexed = false` ficam de fora do RAG mesmo sendo legíveis (controlo do dono/admin, §8). Desligar a
app Wiki (§9) remove **todas** as ferramentas `wiki.*` e o agente `AgentKnowledge`. A **escrita** pela
IA revalida `needed_role=editor` no handler e pede **confirmação** com o conteúdo exato (§6.3) — o
modelo nunca escreve sem aprovação nem além da permissão do utilizador.

### 7.5. Agente de conhecimento (lê **e escreve**) + wiring (grounded)
Decisão do utilizador: um **agente dedicado `AgentKnowledge`** que **lê e escreve** páginas. Ferramentas:
- **Leitura** (`Writes:false, Confirm:false`): `wiki.search` (kNN semântico, §7.2) e `wiki.read`
  (devolve o conteúdo de uma página por id/slug). Ambas filtradas por `canAccess` (§5.3).
- **Escrita** (`Writes:true, Confirm:true` — project.md §6.3, toda escrita pede aprovação com o
  conteúdo exato à vista): `wiki.create_page` (novo rascunho/página) e `wiki.edit_page` (altera uma
  página existente — só se o utilizador tiver papel ≥ `editor` nela; o handler revalida `canAccess`
  com `needed_role=editor`, **nunca** confia no modelo). A escrita respeita versionamento (§6).

Passos concretos (padrão do catálogo):
1. `internal/assistant/tools.go`: consts `ToolWikiSearch="wiki.search"`, `ToolWikiRead="wiki.read"`,
   `ToolWikiCreatePage="wiki.create_page"`, `ToolWikiEditPage="wiki.edit_page"`. `Handler` =
   `func(ctx, id Identity, args json.RawMessage) (Result, error)` — Identity da **sessão**, nunca dos
   `args`; `Parameters` (JSON Schema) **não** declaram tenant/user. Moldes: leitura `tools_contacts.go:35`;
   escrita-com-confirmação — copiar o padrão de `mail.send` (`Writes:true, Confirm:true`, cartão com o
   conteúdo exato).
2. `internal/assistant/tools_wiki.go`: `wikiTools(w WikiService)` devolve as 4 ferramentas; usam o
   serviço da wiki com a mesma superfície `vis`/`canAccess`.
3. `internal/assistant/tools_drive.go`: `Deps` ganha `Wiki WikiService`; `BuildRegistry` ganha
   `if d.Wiki != nil { tools = append(tools, wikiTools(d.Wiki)...) }`.
4. `internal/httpapi/assistant_handlers.go` (`buildAssistant`): liga o serviço em `assistant.Deps{...}`.
5. `internal/assistant/agent.go`: novo agente builtin `AgentKnowledge` com `Tools:
   {ToolWikiSearch, ToolWikiRead, ToolWikiCreatePage, ToolWikiEditPage}` (+ o que fizer sentido).
6. `internal/assistant/apps.go`: `AppTools["wiki"] = {ToolWikiSearch, ToolWikiRead, ToolWikiCreatePage,
   ToolWikiEditPage}` e `AppAgent["wiki"] = AgentKnowledge`. Desligar a app → `AllowedTools`/
   `AgentAvailable` cortam as ferramentas **e** o agente (mecanismo já existente).
7. `internal/httpapi/apps_handlers.go`: `toggleableApps += "wiki"`.

**Como o conteúdo recuperado entra no prompt** (precedente): `MemoryContext(facts)`
([memory.go:184](../backend/internal/assistant/memory.go)) já embrulha factos como **CONTEXT ONLY,
nunca instrução**, injetado via `RunOptions.Memory` no `RunStream` ([engine.go:144](../backend/internal/assistant/engine.go)).
Os trechos da wiki devolvidos pela ferramenta seguem a mesma cerca "isto é contexto, não ordem".

## 8. Configurações e definições
> **Grounding RBAC** ([rbac.go:12](../backend/internal/rbac/rbac.go)): permissões existentes —
> `PermDomainsView/Manage`, `PermMailboxManage`, `PermMembersView/Manage`, `PermBillingManage`,
> `PermUsageView`, `PermTenantDelete`, `PermOrgSettings`. `requirePerm(perm)` ([server.go:768])
> gateia rotas de admin; `mailUserFrom(ctx)` dá o utilizador autenticado. **RBAC é para admin da org**;
> o acesso do dia-a-dia à página usa o padrão `vis` em SQL (§5.3), não RBAC.
- **Nível organização (admin, `PermOrgSettings`)**: ligar/desligar a app Wiki (sistema de apps → some
  do menu e corta a IA); política default de visibilidade de novos espaços (`restricted` vs `org`);
  ligar/desligar globalmente a indexação de IA; retenção de versões. (Opcional: uma `PermWikiManage`
  nova em `rbac.go` se quisermos gating de admin dedicado, distinto de `PermOrgSettings`.)
- **Nível espaço (dono/`manager`)**: nome/chave/ícone, visibilidade, membros e papéis, `ai_indexed`
  on/off, arquivar espaço.
- **Nível página**: grants adicionais, mover, publicar/despublicar, restaurar versão.
- Segredos/config **só por ambiente** (`internal/config`, lei 2.6) — ex.: chave/endpoint de embeddings
  na Fase 2. Nada de segredo no código nem no frontend.

## 9. Gating pelo sistema de apps (consistência com o resto)
Reusar o mecanismo já entregue nesta sessão. **Grounding — pontos exatos a tocar** (o comentário em
`apps_handlers.go:10` pede que se mantenham alinhados):
- Backend: `toggleableApps += "wiki"` ([apps_handlers.go:12]); `disabled_apps` já vai no `/api/me`.
- Frontend [App.tsx](../frontend/src/webmail/App.tsx): união `View` (`:29`), `VIEWS` (`:32`),
  `SEARCHABLE` (`:31`, se pesquisável), array `allApps` (`:172`, filtrado por `disabled_apps` em `:182`),
  lista de reordenação móvel (`:118`) e o `switch` de render (`:228`). Cada vista é um ficheiro próprio.
- i18n: `webmail.apps.wiki` em `en/common.json` (`:192`) **e** `pt-BR/common.json` (paridade).
- IA: `AppTools["wiki"]` + (opcional) `AppAgent["wiki"]` → desligar a app remove a ferramenta e o
  agente do assistente e do picker, exatamente como Mail/Files/etc.

## 10. Arquitetura de implementação

### Backend (Go)
- `internal/db/migrations/00090_wiki.sql` (**próximo número livre confirmado**; o último é
  `00089_drop_deals.sql`) — as tabelas de §3, formato goose (`-- +goose Up/Down`). `sqlc generate`
  (config `backend/sqlc.yaml`, pgx/v5) para os modelos/queries; queries em
  `backend/db/queries/queries.sql` com anotações `-- name: ListWikiPages :many` etc., usando a CTE
  `vis` (§5.3) e `@tenant_id`. Gerado cai em `internal/db/queries.sql.go` + `models.go`.
- `internal/wiki/` — serviço com a regra: árvore de páginas, render/derivação de `body_text`/`body_html`,
  `canAccess`, versionamento, busca (FTS Fase 1). Handlers HTTP finos em `internal/httpapi/wiki_handlers.go`
  chamando o serviço (lei 1.3).
- `internal/assistant/tools_wiki.go` — a ferramenta `wiki.search` (Registry), usando o serviço de busca
  com a identidade do contexto.
- Rotas em `server.go` sob auth; mutações que partilham/apagam continuam a exigir verificação de posse.

### Frontend (React/TS)
- `src/webmail/WikiView.tsx` (+ subcomponentes ≤300 linhas): árvore lateral de espaços/páginas, leitor,
  e o editor TipTap num componente próprio (`WikiEditor.tsx`). Estado de servidor via TanStack Query;
  chamadas por `backend.ts` (lei 3.4).
- Rotas `/mail/wiki`, `/mail/wiki/:space`, `/mail/wiki/:space/:slug`. Registo no `allApps` (§9).
- i18n `wiki.*` em `pt-BR` e `en` (paridade total).

## 11. Fases (uma de cada vez — project.md §9)
- **Fase 1 — Fundação e leitura/escrita**: schema (**N** wikis/espaços, pages, versions, grants), CRUD,
  árvore, editor TipTap básico (blocos essenciais), **permissões por pessoa/grupo/org (acesso|edição)**,
  i18n, gating de app. Sem IA ainda.
- **Fase 2 — Histórico, colaboração e público**: versões (diff/restaurar), bloqueio otimista 409,
  comentários, anexos (reuso Drive), menções/links internos, **partilha pública só-leitura** (link
  anónimo, padrão `file_link_access`; rota pública serve só páginas `published`).
- **Fase 3 — RAG (pgvector) + agente de conhecimento**: extensão `pgvector`, tabela `wiki_chunks`
  (embedding), worker de (re)embedding idempotente ao gravar, busca kNN por cosseno **filtrada pela
  permissão de quem pergunta**, citações, `ai_indexed` on/off. Agente dedicado `AgentKnowledge` com
  ferramentas de **leitura** (`wiki.search`/`wiki.read`) **e escrita** (`wiki.create_page`/
  `wiki.edit_page`, com confirmação §6.3). **Entrega o "alimenta o assistente" e o "ler e escrever".**
- **Futuro**: edição em tempo real (CRDT), templates de página, exportar PDF (reuso da stack de
  apresentação/OnlyOffice), FTS híbrido como pré-filtro barato do kNN.

## 12. Decisões estruturais — **RESOLVIDAS** (2026-07-22, pelo utilizador)
1. **Editor**: ✅ **TipTap/ProseMirror**. Dependência nova autorizada.
2. **RAG**: ✅ **pgvector** (semântico) como abordagem principal — a extensão Postgres `pgvector` é
   infra nova aprovada. FTS (`tsvector`) fica como filtro barato/híbrido opcional, não como fase
   separada. Falta só escolher o **provedor de embeddings** (§7.3) — decisão operacional, não bloqueia
   o schema.
3. **Permissões**: ✅ por **pessoa**, **grupo** e **geral na organização** (org-wide), com tipo
   **acesso (ver)** ou **edição**; mais **acesso público só-leitura** (link anónimo). Deve dar para
   criar **N wikis** independentes (cada "wiki" = um espaço com a sua árvore e permissões). Ver §5.
4. **Assistente**: ✅ **agente dedicado de conhecimento** (`AgentKnowledge`) que **lê e escreve**
   páginas — ferramentas `wiki.search`/`wiki.read` (leitura) **e** `wiki.create_page`/`wiki.edit_page`
   (escrita, com confirmação §6.3). Ver §7.5.

## 13. Riscos e mitigação
- **Vazamento no RAG**: mitigado por §7.4 (mesma `canAccess` da UI; `ai_indexed`; gating por app).
- **Injeção via conteúdo** (uma página maliciosa a "instruir" a IA): conteúdo é dado, nunca instrução
  (§2, §7.1); o prompt do sistema reforça.
- **HTML inseguro** no render: sanitizar sempre; `body_json` é a fonte, `body_html` é derivado controlado.
- **Ficheiros grandes/tabelas**: `body_json` em `jsonb` com limite de tamanho; paginação da árvore.
- **Conflitos de edição**: 409 otimista honesto (§6); tempo real fica para o futuro.

---
_§12 resolvido (2026-07-22). Próximo passo: arrancar a **Fase 1** (schema das N wikis + CRUD + editor
TipTap + permissões pessoa/grupo/org), testada antes de avançar. O acesso público entra na Fase 2 e o
RAG pgvector + agente de conhecimento na Fase 3._

## Progresso de implementação

> Checklist derivada do §11. Marca `[x]` **só** o que estiver implementado **e** com os portões de
> qualidade a passar (sqlc/go build/vet/test + tsc/build + i18n). Um passo pequeno por iteração.

### Fase 1 — Fundação e leitura/escrita
- [x] Migração `00090_wiki.sql` (spaces, pages, versions, space_grants, page_grants) + `sqlc generate` + `go build` OK
- [x] Queries sqlc (`vis` CTE via `WikiSpaceRole`/`WikiPageRole`, CRUD spaces/pages, grants, versões) em `db/queries/queries_wiki.sql` — 26 métodos gerados, `go build` OK
- [x] Serviço `internal/wiki` — acesso (`spaceRole`/`pageRole`/`authorize`), árvore (`Tree`/`buildTree`), derivação `body_text`/`body_html` segura por construção (`render.go`), versionamento otimista (`UpdatePage` → 409 `ErrVersionConflict` + snapshot); `go build`/`vet` OK
- [x] Testes do serviço (`internal/wiki/wiki_test.go`): `derive` (escape/anti-script/URL insegura), `slugify`, `safeURL`, `validateGrant`, `authorize`, `buildTree` — `go test ./internal/wiki/` OK
- [x] Handlers finos `internal/httpapi/wiki_handlers.go` + rotas em `server.go` (CRUD de N wikis, páginas, grants, árvore, versões, move); `writeWikiErr` mapeia sentinelas→`error_code`; `go build`/`vet`/`test` OK
- [x] Gating de app backend: `toggleableApps += "wiki"` (`apps_handlers.go`); `go build`/`vet` OK
- [x] Gating de app frontend: `App.tsx` (View union + VIEWS + mobile-order + `allApps` + `IcBook` + switch `WikiView`) + i18n `webmail.apps.wiki` (pt-BR + en); `tsc`/`build` OK
- [x] Instalar TipTap (`frontend`) — `@tiptap/react`, `@tiptap/pm`, `@tiptap/starter-kit`, `@tiptap/extension-link`, `@tiptap/extension-placeholder` (v2.27); `tsc`/`build` OK
- [x] `WikiView.tsx` — lista de N wikis + criar/apagar espaço, árvore de páginas (`WikiTree`), leitor (`body_html` sanitizado), criar página, publicar/despublicar, editor de texto temporário com 409 otimista; `backend.ts` `wikiApi` + CSS `.wiki-*`
- [x] `WikiEditor.tsx` — editor TipTap (StarterKit H1–H3/negrito/itálico/código/listas/citação/bloco de código/hr + Link + Placeholder), toolbar i18n, emite `body_json` (`getJSON`); integrado no `WikiView` (substituiu o textarea). **UI sem diálogos nativos** — criar wiki via `Drawer` (sidecar), nova página inline, apagar com `ConfirmButton` de 2 passos (feedback do utilizador; ver memória). `tsc`/`build` OK
- [x] UI de permissões por pessoa/grupo/org — `WikiShareDrawer` (sidecar, só managers): lista grants, adiciona/remove acesso a pessoa (colegas), equipa (`/api/my-groups`) ou org, papel ver/editar/gerir; controlos gated por `spaceRole` (share/apagar=manager, nova página=editor). `tsc`/`build` OK
- [x] i18n `wiki.*` + `server.wiki.*` (pt-BR + en, 1718 chaves em paridade, validado)
- [x] `tsc --noEmit` + `npm run build` OK ; **deploy feito**: `docker compose up -d --build api worker` → migração `00090_wiki.sql` aplicada (goose v90), `wiki_spaces`/`wiki_page_grants` existem, api em `:8000`, `GET /api/wiki/spaces` → 401 (rota viva e protegida), worker OK

> **Fase 1 CONCLUÍDA e testada (deploy ao vivo).** Backend (serviço+handlers+rotas+gating), frontend (WikiView+WikiEditor TipTap+WikiShareDrawer, sem diálogos nativos), i18n pt-BR/en em paridade (1754 chaves). Próximo: **Fase 2**.

### Fase 2 — Histórico, colaboração e público
- [x] Versões: diff + restaurar — backend `GetVersion`/`RestoreVersion` (reusa `UpdatePage`: versiona+snapshot+otimista) + rotas `GET/POST .../versions/{version}[/restore]`; frontend `WikiHistoryDrawer` (sidecar) com diff LCS por linhas e restaurar (só editor). `go build/vet/test` + `tsc`/`build` OK
- [x] Bloqueio otimista 409 (`wiki.version_conflict`) na UI — `WikiView` save e `WikiHistoryDrawer` restore detetam `ApiError.code` e mostram `wiki.conflict` + recarregam
- [x] Comentários — migração `00091_wiki_comments.sql` (aplicada, tabela existe), sqlc, serviço `internal/wiki/comments.go` (ler=viewer, comentar=commenter, apagar=autor|manager), handlers+rotas (`GET/POST .../comments`, `DELETE /wiki/comments/{id}`), frontend `WikiComments` (lista+form, apagar próprio/manager, sem diálogos nativos), i18n paridade. Deploy OK, rota 401 guarded
- [x] Anexos (reuso Drive/MinIO) — migração `00092_wiki_attachments.sql` (aplicada), sqlc, serviço `internal/wiki/attachments.go` (anexar/remover=editor, listar/descarregar=viewer; só anexa ficheiro que o utilizador possui; download confere acesso à página), handlers+rotas (`GET/POST .../attachments`, `DELETE .../attachments/{attId}`, `GET .../download`), frontend `WikiAttachments` (busca no Drive por nome + anexar/remover/descarregar). Deploy OK, rota 401 guarded
- [x] Menções `@pessoa` e links `[[página]]` — TipTap `@tiptap/extension-mention` (2 instâncias: `@`=pessoa via `/api/colleagues`, `[`=`pageLink` via árvore do espaço) com popup vanilla (`WikiSuggest.ts`, sem tippy); `render.go` trata nós `mention`/`pageLink` (HTML escapado `wiki-mention`/`wiki-pagelink` + `data-page-id`, e `body_text` inclui `@label`/título) com testes; `go build/vet/test` + `tsc`/`build` OK; deploy OK
- [x] Acesso público só-leitura — queries `SetWikiSpacePublic`/`GetWikiSpaceByPublicSlug`; serviço `internal/wiki/public.go` (`SetPublicAccess` manager: gera `public_slug` aleatório/regenerável, liga/desliga; `PublicSpace`/`PublicPage` anónimos, só `published`, tenant resolvido pelo slug); handlers autenticado (toggle) + **anónimos** `GET /api/public/wiki/{slug}[/{pageSlug}]` (grupo `publicRateLimit`, fora da auth); frontend: toggle+link+copiar+regenerar no `WikiShareDrawer`, página pública `src/pages/PublicWiki.tsx` nas rotas `/w/:slug[/:pageSlug]` (fora do shell), serve só `body_html`. Testado: rota anónima → 404 (não 401), toggle → 401 guarded. Deploy OK

> **Fase 2 CONCLUÍDA e testada (deploy ao vivo).** Histórico (diff/restaurar) + 409, comentários, anexos (Drive), menções `@`/links `[[página]]`, e acesso público só-leitura `/w/<slug>`. i18n pt-BR/en em paridade (1773 chaves). Próximo: **Fase 3 (RAG + agente)** — 1º passo PARA e pergunta o provedor de embeddings (dimensão D) antes de criar `wiki_chunks`.

### Bloco 2 — Melhorias (pedido do utilizador, 2026-07-22)
> Loop contínuo. Ordem por dependência; um passo por iteração, portões sempre.
- [x] **B1 Design + deep-links** — todos os `btn*` (classe inexistente) trocados pelos padrões da casa: primário `add` (com CSS scoped p/ `.wiki`/corpo de drawer), neutro `chipbtn`, ícone `ib`; footers de drawer com `<div class="grow">` como os demais. Rotas `/mail/wiki/:spaceId/:pageId` — a seleção vive no URL (deep-links; `WikiView` recebe `sub/sub2/onNav` do `App`). `tsc`/`build` OK
- [x] **B2 Editor BlockNote** — `@blocknote/core|react|mantine` 0.52 (MPL; instalado com `--legacy-peer-deps` + peers `@mantine/core|hooks` e `@tiptap/pm@3`; TipTap direto removido). `WikiEditor` reescrito: BlockNote não-controlado, locale pt/en, tema via `data-theme`, slash-menu default + **3 itens de IA** (continuar/melhorar/resumir) a chamar o novo `POST /api/ai/wiki-write` (sessão+rate-limit; `Assistant.WikiWrite` responde Markdown → `tryParseMarkdownToBlocks`; conteúdo da página vai como DADO). `body_json` = array de blocos BlockNote; `render_blocknote.go` (HTML/texto seguros: listas agrupadas, checklist, tabela, código, imagem, estilos, links whitelisted) com testes; formato ProseMirror legado continua a renderizar e abre no editor via `body_text`. **Adiado com nota:** menções `@`/links `[[página]]` no BlockNote (custom inline content; re-implementar num passo próprio). Portões go+tsc+build OK; deploy OK (rota 401 guarded)
- [x] **B3 Permissões por página** — serviço `page_grants.go` (listar/definir/remover, gerir exige manager NA página; grants somam ao espaço, nunca reduzem), rotas `GET/PUT/DELETE /wiki/pages/{id}/grants[/{grantId}]`; UI: `WikiGrantsEditor` extraído (partilhado espaço↔página, DRY), `WikiPageShareDrawer` (sidecar) com hint, botão partilhar no cabeçalho da página (só manager). Portões go+tsc+build+i18n OK; deploy OK (401 guarded)
- [x] **B4 Configuração da wiki + apagar protegido** — `WikiSettingsDrawer` (sidecar, engrenagem na sidebar p/ managers): nome/ícone/descrição/visibilidade/`ai_indexed`; **danger zone** só p/ dono do espaço OU admin da org (owner/admin/internal_admin), apagar exige ESCREVER O NOME da wiki (input compara, sem diálogo nativo); botão de apagar removido da sidebar. Backend: `DeleteSpace` agora exige dono∥admin (manager concedido já não chega; 404/403 corretos). Portões go+tsc+build+i18n OK; deploy OK. Nota: cache `.vite` limpa após o BlockNote (o dev server precisa de restart)
- [x] **B5 Home personalizável** — migração `00094_wiki_home.sql` (aplicada; 1 linha por tenant), serviço `home.go` (derive igual às páginas), rotas `GET /api/wiki/home` (autenticado) e `PUT` com `requirePerm(PermOrgSettings)`; frontend `WikiHome` no ecrã inicial (renderiza `body_html`; admins têm lápis→editor BlockNote inline; sem conteúdo mostra o texto padrão). **Correção Mantine**: v9 exige React 19 → downgrade para `@mantine/core@8.3.18` (peer React 18, aceite pelo BlockNote); cache `.vite` limpa (dev server precisa restart). Portões go+tsc+build+i18n OK; deploy OK (401 guarded, tabela existe)
- [x] **B6 Assistente → redirecionar + tempo real** — as ferramentas `wiki.create_page`/`wiki.edit_page` devolvem `link` (`/mail/wiki/<space>/<page>`) no `Result.Data` (o modelo cita) e publicam evento **`wiki_page`** no hub Redis para o utilizador da sessão; o `WikiView` subscreve (`useChatSocket`): **criou → navega para a página; editou → refresca a aberta** (invalidate tree+page). Bónus: a IA agora grava `body_json` no formato **BlockNote** (abre nativo no editor). Portões go+tsc+build OK; deploy OK
- [x] **B7 Templates de página** — migração `00095` (`is_template` + índice parcial, aplicada), queries `SetWikiPageTemplate`/`ListWikiTemplates`, serviço (marcar=editor, listar=viewer), rotas `POST /wiki/pages/{id}/template` e `GET /wiki/spaces/{id}/templates`; frontend: chip "Tornar template/Template ✓" nas ações da página, e o form de nova página ganha select "Página em branco / Template: X" que **copia o body_json** do template escolhido. Portões go+tsc+build+i18n OK; deploy OK
- [x] **B8 Exportar PDF** — botão impressora nas ações da página (`window.print`, que é o diálogo de impressão do SO, não um prompt de dados) + `@media print` que esconde shell/rail/sidebar/ações/comentários/anexos e imprime só o conteúdo limpo → "Guardar como PDF" do browser. `tsc`/`build`/i18n OK (sem backend)
- [x] **B9 FTS híbrido** — `SearchWikiChunks` ganha `AND (@use_fts=false OR search_tsv @@ websearch_to_tsquery('simple', @query_text))` (pré-filtro GIN barato antes do kNN); `SearchChunks` faz 2 passagens: híbrida primeiro, e se devolver 0 repete só-semântica (recall garantido). sqlc+build+test OK; deploy OK
- [x] **B10 Tempo real (CRDT)** — **decisão do utilizador: relay Yjs em GO, sem container novo.** Backend: `wiki_collab.go` — rota WS `/api/wiki/pages/{id}/collab/ws` (sessão + `canAccess`≥editor; viewer nem entra), salas in-process por página com writer-goroutine por peer, retransmissão binária opaca (o servidor não entende Yjs; os clientes respondem aos sync steps entre si), 1º frame `{"type":"hello","peers":N}` para a regra de seed; escala multi-instância fica para quando houver (comentado). Frontend: `WikiCollabProvider.ts` (protocolo y-websocket clássico sobre `@y/y`+`@y/protocols` v14 RC — os que o BlockNote 0.52 usa; sync+awareness+reconnect) e `WikiEditor` com `collaboration:{provider, fragment: doc.get("document-store"), user}` — **peers===0 semeia do body_json; peers>0 começa vazio e sincroniza** (anti-duplicação); cursores com nome/cor estável via awareness (query `me`). O autosave continua a gravar `body_json` via `UpdatePage` (RAG/versões/render intactos). Portões go+tsc+build OK; deploy OK (WS 401 guarded)
- [x] **Menções `@` e links `[[página]]` no BlockNote** (pendência do B2 fechada) — `createReactInlineContentSpec` ×2 (`mention`/`pageLink` com props `{id,label}`), schema custom, 2 `SuggestionMenuController` extra (`@`=colegas via `/api/colleagues`, `[`=páginas do espaço, exclui a própria) inserindo via `insertInlineContent`; `render_blocknote.go` trata ambos (HTML escapado `wiki-mention`/`wiki-pagelink`+`data-page-id`, e `body_text` com `@label`/título p/ RAG) com testes incl. anti-`<script>`. Portões go+tsc+build OK; deploy OK

- [x] **Template básico em página nova** (pedido pós-bloco) — página criada sem template escolhido nasce com esqueleto i18n "Visão geral / Detalhes / Notas" (blocos BlockNote via `basicTemplate(t)` no `WikiView`); escolher um template do espaço continua a copiá-lo. `tsc`/`build`/i18n OK (só frontend)

- [x] **IA escreve com componentes do editor** (feedback pós-teste) — conversor Go `wiki.MarkdownToBlocks` (títulos, listas, numeradas, checklists, citações, código com fence, negrito/itálico/código inline, links) com testes; as ferramentas `wiki.create_page`/`wiki.edit_page` documentam Markdown e o conteúdo entra como blocos BlockNote nativos. **Notificação global**: quando a IA cria/edita página e o utilizador NÃO está na wiki, aparece toast com ação "Abrir" (dentro da wiki continua a navegar/refrescar). Nota de diagnóstico: "ferramenta não disponível" acontecia porque conversas EXISTENTES mantêm o agente com que começaram (routing é por conversa, §3.2) — em conversa nova ou com o agente "Base de conhecimento" no picker funciona

- [x] **Fix "esta ação não está disponível" no wiki.edit_page** — trace mostrou o modelo a chamar `wiki.edit.page` (pontos) em vez de `wiki.edit_page`; o motor ganhou `canonicalToolName` (compara ignorando [._-], SÓ contra a lista do próprio agente — corrige grafia, nunca alarga permissões) com teste. Routing afinado: propósito do agente Documentos exclui explicitamente páginas da wiki; o de Conhecimento menciona "páginas". Deploy OK

- [x] **Fix RAIZ do "editar sem formatação"** — o Go serializa `[]byte` como BASE64: `body_json` chegava ao frontend como string, o `Array.isArray` falhava e o editor caía no fallback de texto plano (era esta a causa primária desde o início; StrictMode/hello eram agravantes). Override sqlc `wiki_pages/versions/home/comments.body_json → encoding/json.RawMessage` (padrão da casa, como `contacts.custom_fields`) → JSON cru no response; defesa no editor para respostas antigas em cache (parse string/base64). Deploy OK

### Bloco 4 — Kanban (pedido do utilizador, 2026-07-22)
- [x] **Cards flat** — sem box-shadow, sem borda (incl. a barra esquerda accent), hover subtil; sombras removidas na página (kb-views)
- [x] **Cor do card** — migração `00097` (`cards.color`, aplicada), `UpdateCard` + handler; picker de 8 tintas suaves no TaskModal (+ "sem cor"); card colorido força tinta de texto escura (contraste garantido em claro/escuro)
- [x] **Partilhar card no chat** — `ShareCardChatDrawer`: canais (recentes primeiro por `last_read_at`, pesquisáveis) e pessoas (abre DM via `chatOpenDM`); envia mensagem com link interno markdown ao card; botão só aparece com a app de Chat ligada (`useDisabledApps`). i18n pt-BR/en (1857 em paridade)

- [x] **Kanban extra (pós-feedback)** — hover visível em todos os cards (filter de brilho, funciona sobre tintas e nos dois temas); footer do TaskModal corrigido (estava aninhado fora do alcance de .drawer>footer: Salvar accent, Excluir/Arquivar inline); chave i18n `crm.linkContact` adicionada; **reordenação de quadros por utilizador** — tabs arrastáveis, ordem em `settings.kanban.boardOrder` (persistida na conta; quadros novos entram no fim)

- [x] **Permissões nos quadros do Kanban** — migração `00098` (`boards.owner_user_id` + `board_grants` pessoa/equipa/org com viewer|editor|manager, aplicada). Regras em SQL (padrão wiki): dono=manager; **quadro legado (sem dono nem grants) = org toda edita** (compatibilidade); **card atribuído = acesso editor automático** (pedido). `ListBoards` filtrado por utilizador; gates nos handlers (ver=viewer; cards/colunas=editor via `requireCardEditor`; renomear/apagar/arquivar/prefixo/grants=manager). Quadro novo: dono=criador + grant org editor por defeito (removível). Frontend: `BoardShareDrawer` (reusa `WikiGrantsEditor`) com nota do auto-acesso, botão partilhar + gestão só para managers, rota `/access` para o papel. Deploy OK (401 guarded)

> **BLOCO 2 COMPLETO (2026-07-22).** B1–B10 + menções: design alinhado, BlockNote+IA, permissões por página, apagar protegido, home personalizável, redirecionamento+tempo real do assistente, templates, PDF, FTS híbrido e colaboração CRDT via relay em Go (sem infra nova).

### Bloco 3 — Anexos/imagens/comentários (pedido do utilizador, 2026-07-22)
- [x] **Drive picker integrado** — `DrivePickerDrawer` (navegar pastas com breadcrumb, pesquisa, upload para a pasta atual, selecionar) estilo Google; usado nos anexos da wiki (substitui a busca por nome) e reutilizável
- [x] **Mesmo picker no chat** — botão "Anexar do Drive" na toolbar do composer dos canais; anexa por `file_id` (o envio já suportava `attachments[]` — zero backend)
- [x] **Comentários com editor** — migração `00096` (body_json/body_html em `wiki_comments`, aplicada); `AddComment` deriva texto+HTML pelo mesmo caminho seguro das páginas; `CommentEditor` (BlockNote mini, sem side/slash menu) no lugar do textarea; render por `body_html` (antigos caem no texto)
- [x] **Nova criação de página com galeria de templates** (revisão pós-feedback) — o form inline saiu; criar página abre um Drawer com título + galeria: Vazia, Base de conhecimento, Ata de reunião, Brief de projeto, Guia passo-a-passo (miniaturas CSS que representam a estrutura) + os templates do próprio espaço; o conteúdo escolhido grava-se no body_json (não é só visual). **Fix da causa raiz do "template some ao editar"**: o hello do relay chegava antes do editor registar o callback → seed nunca corria; o provider agora bufferiza e replay-a o hello. Editor de comentários com slash-menu ativo (o placeholder promete '/')
- [x] **Fix definitivo "editar abre em branco"** — raiz: o initializer do useState corre 2× no StrictMode e o provider ÓRFÃO conectava no construtor, ocupando a sala (o real recebia peers=1 e não semeava). O provider já não conecta no construtor (só no useEffect via ensureConnected(), idempotente) — o órfão nunca abre WS; fallback de seed a 2s cobre ligações fantasma. Editor integrado na página (sem borda, fundo transparente). — (fix anterior StrictMode) — em dev o React monta→desmonta→monta e o cleanup destruía o provider partilhado (o WS nunca reabria → seed nunca corria); `ensureConnected()` revive o provider no mount + **fallback de seed por timeout** (1,5s sem hello → semeia na mesma; editar sozinho nunca abre vazio). Editor de página com altura da janela (`min-height: calc(100vh-240px)`). Comentários com **toolbar visível estilo chat** (B/I/S/código/lista/citação via `toggleStyles`/`updateBlock`)
- [x] **Imagens da página → Drive + aviso** — `uploadFile` do BlockNote: upload para o Drive + link público do ficheiro (URL `inline` alcançável por qualquer leitor, incl. rota pública); colar URL continua no separador Embed nativo; **aviso** no toggle de acesso público (`wiki.public.imagesWarn`) de que os links embutidos são públicos

### Fase 3 — RAG (pgvector) + agente de conhecimento
- [x] Provedor de embeddings **decidido pelo utilizador (2026-07-22): OpenAI `text-embedding-3-small`, D=1536** (endpoint OpenAI-compat por env; ver memória `wiki-embeddings-provider`)
- [x] Migração `00093_wiki_chunks.sql` (`CREATE EXTENSION vector` + `wiki_chunks(embedding vector(1536))` + ivfflat cosseno) **aplicada e verificada** (extensão `vector` ativa, tabela existe); imagem Postgres trocada para `pgvector/pgvector:pg16` (volume 16.x preservado); sqlc com override `vector→string` (literal `[..]` + cast `::vector`)
- [x] Worker de (re)embedding idempotente — `internal/wiki/rag.go` (`EmbedPending`: limpa stale, chunking ~2000 chars c/ sobreposição+teto, batch 32, upsert por `(page_id,chunk_index)` + trim) + cliente `internal/ai/embeddings.go` (OpenAI-compat) + ticker 30s no `cmd/worker`; testes de chunking/literal OK. **Ativa quando `EMBEDDINGS_API_KEY` for definida no ambiente** (compose já passa `EMBEDDINGS_*`; sem chave = RAG desligado, fail-safe)
- [x] Busca kNN cosseno filtrada — query `SearchWikiChunks` (tenant + `vis` + `ai_indexed` + `published`, distância cosseno) + `Service.SearchChunks` (embedda a pergunta); devolve título/slug/trecho (dados de citação). A superfície de citações ao utilizador entra com a ferramenta `wiki.search` (passo seguinte)
- [x] Agente `AgentKnowledge` + 4 ferramentas `wiki.*` — `tools_wiki.go` (leitura `wiki.search`/`wiki.read` sem confirmação; escrita `wiki.create_page`/`wiki.edit_page` com `Writes+Confirm` §6.3); adapters `httpapi/assistant_wiki.go` revalidam `canAccess` no serviço (criar/editar = `editor`, otimista, rascunho por defeito); conteúdo lido é DADO (instruções do agente reforçam anti-injeção); invariante "geral ⊇ builtin" mantido (4 ferramentas também no `AgentGeneral`); sem `EMBEDDINGS_API_KEY` o `wiki.search` devolve erro claro e o resto funciona. `go test ./...` 11/11 OK
- [x] Wiring completo — `Deps.Wiki*` + `BuildRegistry` + `buildAssistant` + `Server.wikiEmb` + `AppTools["wiki"]` + `AppAgent["wiki"]` (desligar a app corta ferramentas+agente) + i18n `assistant.agent.knowledge` (pt-BR+en, 1774 chaves em paridade)

> **Fase 3 CONCLUÍDA e testada ao vivo (2026-07-22).** Com `EMBEDDINGS_API_KEY` definida: worker logou `wiki embed: 1 page(s) indexed` e gravou chunk real `vector(1536)` no pgvector (verificado no Postgres). **Fases 1–3 da Wiki completas.**
