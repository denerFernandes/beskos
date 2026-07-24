# Roadmap — Identidade, Admin no menu, Grupos e CRM de contatos

Épico grande, quebrado em fases (lei §6.1: uma fase por vez, testada antes da próxima).
Decisões abaixo foram tomadas com o cliente — estão fixas; mudar exige nova conversa.

## Decisões

| Tema | Decisão | Consequência |
|---|---|---|
| **Identidade** | **Unificar: matar `panel_users`** | Uma só identidade (`mail_users`) para painel e webmail. Migração de dados + refatorar login/MFA/sessões. Maior risco do épico. |
| **Empresa × contato** | **Tabela `companies` separada** | `contacts.company_id` → subcontatos com cargo. Busca/tags/compartilhamento precisam cobrir as duas entidades. |
| **Chamados** | **Sistema de tickets completo** | Status, prioridade, fila, responsável, thread de respostas, SLA. Subsistema próprio (fase 6). |
| **Tasks do cliente** | **Reusar o Kanban** | `cards.contact_id`. Uma só noção de "tarefa" no produto; reusa IA, filtros, responsável e prazo. |

## Revisão do modelo (2026-07-15) — "estamos misturando conceitos"

Diagnóstico: `contacts` servia três donos com regras opostas — agenda (minha, privada), cliente
(da empresa, partilhado) e atendimento (da equipa). Sintoma que apareceu no teste: chamado visível
a todos com `contact_name` a `null` → "Chamado #12 de —". O modelo contradizia-se em voz alta.

Decisões do cliente:

| Tema | Decisão | Consequência |
|---|---|---|
| **Agenda × Cliente** | **Duas entidades** | `contacts` volta a ser agenda pessoal (sempre privada, dentro do E-mail). Nasce `clients` (kind = company \| person) + `client_contacts`. |
| **Visibilidade** | **Cortar o nível "grupos"** | Agenda = privada; cliente = da empresa. Somem `contact_shares`, `company_shares`, o seletor e a subquery de visibilidade repetida em cada leitura. Zero partilhas existiam — a migração não perde nada. |
| **Atendimento** | **Dentro de Clientes** | Sem ícone próprio; o chamado é sempre de um cliente e vive na ficha dele. |

Ângulo comercial que orienta o corte: o concorrente é o **Google Workspace + 2 SaaS**, não o
Zendesk. A promessa é "o e-mail da tua empresa já sabe quem é o cliente" — ninguém migra pelo CRM,
migra pelo e-mail e fica pelo resto. CRM/Atendimento leves = upsell natural; CRM pesado = motivo
para não comprar nenhum dos dois.

Nomes (duas colisões a corrigir): `user_groups` → **Equipas**; `contact_groups` → **Etiquetas**.
Regra tarefa × chamado, escrita na UI: *veio de fora com prazo? chamado. decidimos nós? tarefa.*

Custo assumido: parte da UI construída em 15/07 (seletor de visibilidade, `CompaniesView`,
`ContactPage` com abas) é reescrita. Melhor agora, com 7 contatos e 1 empresa, do que com clientes
reais lá dentro.

## Estado (2026-07-15, fim do dia) — modelo FINAL

Houve duas voltas hoje. Registo as três para o próximo não repetir o caminho:

1. **De manhã**: `contacts` servia agenda (privada) e cliente (partilhado) ao mesmo tempo → contradição
   (sintoma: "Chamado #12 de —"). Cortámos o seletor de visibilidade de 3 níveis e separámos em
   `contacts` (agenda) + `clients`/`client_contacts` (CRM). Migração 00022.
2. **À tarde**: a separação resolveu a contradição mas criou **dois lugares para "gente"**. O cliente
   percebeu ao ver: *"não gostei, estamos misturando conceitos"*. A queixa original nunca tinha sido a
   tabela única — era a **cerimónia** da privacidade.
3. **Final** (migração 00023): **um só `contacts`**, `kind` = company|person, `company_id` aponta para
   o contato-empresa. Contatos volta ao rail; o Atendimento vive dentro dele.

Regras (decididas com o cliente, testadas):

| | regra |
|---|---|
| **Ver** | pelo **GRUPO**: sou o dono, ou estou numa **equipa** com que um grupo do contato foi partilhado. Uma pessoa herda da empresa dela. Grupo não partilhado = só o dono vê. |
| **Editar/apagar** | **só o dono** (`UpdateContact` filtra por `owner_user_id`; a UI mostra "Só leitura"). |
| **Notas** | quem **vê** pode comentar — comentar não é editar. |
| **Lista** | empresas e pessoas juntas; a pessoa mostra `cargo · empresa`. Filtro Todos/Empresa/Pessoa + busca. |

Nomes, colisão resolvida: **Grupos** = grupos de contato (dão acesso). **Equipas** = `user_groups`.
"Etiqueta" foi descartado: etiqueta não concede permissão, grupo sim.

Verificado a correr (Chrome, sessões reais): Ana vê 8 contatos e os grupos dela; **Bob vê 3** (só os
do grupo partilhado com a equipa dele), sem os grupos da Ana, e a ficha diz **"Só leitura"** sem botão
de editar. Backend: partilhar/despartilhar o grupo faz o contato aparecer/desaparecer para o Bob;
Bob não consegue partilhar o grupo da Ana (404); PATCH no contato da Ana → 404; nota → 201.

Armadilha apanhada na migração: eu ia criar um grupo "Clientes" para os ex-clientes, mas a Ana já
tinha um com esse nome — sem restrição única teriam ficado dois homónimos e o `JOIN ... ON name`
casaria com ambos, partilhando o grupo **pessoal** dela com a equipa comercial. Criei "Contatos da
empresa"; o "Clientes" dela continua só dela.

Falta: fase **6b** (envio MIME com `Message-ID` + hook de entrada do Stalwart).

## Fases

### 1. Unificar identidade (base de tudo)
Hoje existem dois mundos: `panel_users` (painel `/app`, RBAC, MFA, senha) e `mail_users` (webmail,
caixa no Stalwart). Criar usuário cria **os dois** com o mesmo e-mail e a mesma role — e trocar o
papel em `/members/{id}/role` atualiza **só o painel**, deixando `mail_users.role` desatualizado.
Esse bug é o motivo de a unificação vir primeiro.

- `mail_users` absorve: `password_hash`, `language`, `totp_secret`, `mfa_totp_enabled`,
  `mfa_sms_enabled`, `phone`.
- Repontar FKs: `sessions.user_id`, `recovery_codes.user_id`, `invites.invited_by`,
  `mfa_challenges` → `mail_users`.
- Migração: casar por (tenant, e-mail); `panel_user` sem `mail_user` (conta de signup, ainda sem
  caixa) vira `mail_user` com `stalwart_account_id` nulo.
- **Nota:** `mail_users` passa a significar "usuário", com ou sem caixa de correio.

### 2. Admin no menu do webmail
- **Um ícone** no rail → menu interno (padrão do E-mail): Usuários, Domínios, Pagamentos, Grupos.
- **RBAC aplicado nos dois lados**: item some da UI **e** a API recusa (defesa real, não só UI).
- Permissões existentes: `domains.view/manage`, `mailboxes.manage`, `members.view/manage`,
  `billing.manage`, `usage.view`, `tenant.delete`.

### 3. Grupos de utilizadores
- `user_groups` + `user_group_members` (≠ `contact_groups`, que é catálogo de contatos).
- **Criar grupo cria automaticamente um canal privado no chat** com os membros.
- Admin adiciona usuários a grupos ao criar/editar usuário; gerir grupos na área admin.

### 4. Contatos ricos
- `companies` (empresa) + `contacts.company_id` + `contacts.title` (cargo).
- Telefones com flags: **é WhatsApp**, **pode ligar**, **pode e-mail** (consentimento — LGPD).
- **Privacidade**: `visibility` = `private` | `groups` | `tenant`; `contact_shares(contact_id, group_id)`.
  Toda query de contato passa a filtrar dono + visibilidade + grupos do usuário.

### 5. CRM
- Página de detalhe moderna por cliente (empresa e pessoa).
- **Notas** (timeline), **tasks** (card do Kanban ligado ao contato), **histórico** agregado.

### 6. Chamados — REMOVIDO (00024)

Construído (00021) e removido no mesmo dia, a pedido do cliente. O motivo é coerente com o
posicionamento: o produto é "mais simples e simplificado que o Workspace", e helpdesk com
SLA/fila/thread/primeira-resposta é **outro produto**. O contato ficou com **notas** e **tarefas**,
que é o que resolve o dia a dia de uma empresa pequena.

O desenho inteiro fica em `00021_tickets.sql` e no histórico do git, se um dia voltar. Junto com
ele saíram: política de SLA (Admin → SLA), "abrir chamado a partir do e-mail" no leitor, e a
integração de e-mail da fase 6b (nunca construída).

**Achado técnico que sobrevive à remoção** (vale para qualquer coisa que precise de e-mail de
entrada): o Stalwart 0.16.4 **aceita MTA hooks por JMAP** — `x:MtaHook/get` responde (lista vazia) na
sessão admin com `urn:stalwart:jmap`. Ou seja, dá para o Stalwart **chamar a nossa API** quando chega
mensagem, sem caixa de serviço com senha guardada na nossa base (que violaria a lei §2.6) nem
polling. `x:Setting/get` não existe — a configuração é por objeto (`x:MtaHook`, `x:Domain`, …).

O que o backend NÃO tem, e precisaria para e-mail de saída a sério: MIME com `Message-ID` /
`In-Reply-To` / `References`. O único sender é `notify/email.go` (`net/smtp`, texto puro, 1
destinatário, sem anexo/HTML).
