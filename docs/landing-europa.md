# Plano — reposicionar a landing na soberania de dados europeia

> Estado: **plano**, nada implementado. Escrito a 29/07/2026, atualizado com as decisões do Dener.
> Decisões pendentes marcadas com **[decidir]**.

## 0. Decisões já tomadas

| Tema | Decisão |
|---|---|
| URLs | **Sempre em inglês**, em todos os idiomas: `/sub-processors`, `/sla`, `/dpa`, `/security`, `/privacy`, `/cookies`, `/terms`, `/acceptable-use`. Só o conteúdo é traduzido |
| Ficheiros | R2 passa a **jurisdição UE** (em vez de trocar de fornecedor) |
| Assistente de IA | DeepSeek deixa de ser chamado diretamente: passa pela **TensorX** (`tensorx.ai`) |
| Embeddings | OpenAI sai; entra **`qwen/qwen3-embedding-8b`** pela TensorX |
| Idiomas | Os oito propostos, **`pt-BR` fora** por agora |
| Âmbito dos idiomas | Landing **e app** — não só o site |
| Páginas legais | As oito do desenho: Privacy, Cookies, Terms, Security, DPA, Acceptable Use, SLA, Sub-processors |

## 1. O que muda

Hoje a landing vende **substituição de pilha por preço**: *"Cancele quatro assinaturas. Pague
uma."* Funciona, mas é o argumento mais fácil de copiar — qualquer suite mais barata diz o mesmo, e
atrai quem compra por preço e sai por preço.

O novo eixo é **soberania**: onde estão os dados, quem manda neles, e a quem a empresa que os aloja
tem de responder. O preço passa a segundo argumento (bom para fechar, mau para abrir).

| | Antes | Depois |
|---|---|---|
| Promessa | Nove ferramentas, uma conta, mais barato | O trabalho da equipa inteiro sob jurisdição europeia |
| Prova | Calculadora de poupança | Onde correm os dados, quem os opera, que lei se aplica |
| Comparação | Google/Microsoft por preço | Google/Microsoft por **exposição legal**; Proton/Nextcloud por **âmbito** |
| Objeção a vencer | "é barato demais para ser bom" | "e se eu precisar de sair?" / "quem é esta gente?" |
| Quem compra | PME atenta ao custo | Quem tem de justificar a escolha a um DPO, a um auditor ou a um concurso |

O que nos separa dos vizinhos: a **Proton** é privacidade individual a crescer para equipas; o
**Nextcloud/openDesk** é software que alguém tem de alojar e manter; a **Infomaniak** é o par mais
próximo (suíça, não UE). Nós somos suite **completa, gerida e da UE** — e-mail, Office, drive,
reuniões, chat, calendário, wiki, quadros e assistente, numa conta e numa fatura.

---

## 2. O bloqueador: a pilha ainda não é o que a mensagem vai dizer

Isto vem primeiro porque **não se pode publicar a página antes de resolver**. Dizer "100% na UE"
com a pilha de hoje é falso, e é o tipo de afirmação que um comprador público verifica.

| Peça | Hoje | Problema para a mensagem | Saída |
|---|---|---|---|
| Servidores (app, BD, e-mail) | **Netcup**, Alemanha | Nenhum — é o melhor argumento que temos | Usar: "servidores na Alemanha" |
| Ficheiros (Drive) | **Cloudflare R2**, jurisdição a mudar para **UE** | Dados ficam na UE; a Cloudflare continua americana (CLOUD Act aplica-se à empresa, não ao sítio dos bytes) | Decidido: manter com jurisdição UE. A página tem de o dizer assim, sem prometer "fora do alcance dos EUA" |
| Envio de e-mail | **Amazon SES** (eu-west-1) | Dados na Irlanda, mas fornecedor **americano** | Relay europeu, ou enviar do próprio Stalwart |
| DNS/CDN/TLS | **Cloudflare** | Todo o tráfego passa por um edge americano | Bunny (SI), ou Cloudflare só como DNS e TLS na origem |
| Assistente de IA | DeepSeek **via TensorX** | Resolve-se **se** a TensorX servir o modelo na UE. O nome do modelo é chinês; o que conta é onde corre e quem o opera | **[decidir]** confirmar região e operador para o podermos escrever |
| Embeddings da wiki | **`qwen/qwen3-embedding-8b`** via TensorX | Idem — modelo de pesos abertos, o que ajuda: pode correr em infraestrutura nossa ou europeia | **[decidir]** mesma confirmação |

**Com as decisões tomadas, sobra isto:** o envio (SES) e o CDN (Cloudflare) continuam americanos, e
o R2 fica americano com dados na UE. Ou seja, a frase que a página pode sustentar é **"os seus dados
são processados e guardados na UE"** — e não "nenhum fornecedor nosso é americano", que seria falso.

A diferença não é retórica: é a primeira pergunta de um DPO alemão. A saída honesta, e que nos
distingue, é a **página de subprocessadores** dizer tudo — quem, para quê, onde, com que
salvaguarda. Quem publica a lista ganha a conversa mesmo com fornecedores americanos lá dentro;
quem a esconde perde-a assim que alguém pergunta.

**[decidir]** trocar o SES por um relay europeu e a CDN por Bunny fica para depois do lançamento?
Recomendo que sim, mas com data marcada — e a lista de subprocessadores mostra o progresso sozinha.

---

## 3. Mensagem

**Promessa (título):** o trabalho da sua equipa — e-mail, ficheiros, documentos, reuniões — sob a
lei europeia, operado por uma empresa europeia.

**As três provas** (a página tem de as mostrar, não afirmar):
1. **Onde** — servidores nomeados, país, operador. Mapa simples, sem marketing.
2. **Quem** — empresa polaca, sem holding americana, lista de subprocessadores pública.
3. **O quê** — o âmbito: correio, Office, drive, reuniões, chat, agenda, wiki, quadros, IA.

**Objeções a responder na própria página** (cada uma merece uma secção ou uma FAQ):
- "E se quiser sair?" → exportação em formatos abertos, sem contrato de fidelização.
- "Aguenta a minha auditoria?" → DPA, subprocessadores, retenção, cifra, registos de acesso.
- "E se avariar?" → réplica, backups, histórico de disponibilidade.
- "É uma empresa pequena." → sim; e o código do servidor de correio é aberto, os dados são
  exportáveis, e não há refém.
- "A IA não estraga tudo isto?" → onde corre o modelo, o que sai da UE, como se desliga.

**O que NÃO dizer:** "conformidade garantida", "certificado ISO" (sem o ser), "imune ao CLOUD Act"
(ninguém é imune — o que existe é não estar sujeito), "os seus dados nunca saem da UE" enquanto o
R2/SES/DeepSeek estiverem lá.

---

## 4. Arquitetura do site

**Regra das URLs:** sempre em **inglês**, iguais em todos os idiomas (`/de/sub-processors`,
`/fr/sub-processors`). Traduz-se o conteúdo, não o caminho — links partilhados entre colegas de
países diferentes continuam a funcionar, e os documentos legais são citados pelo mesmo endereço em
qualquer contrato.

**Novas — produto e argumento**

| URL | Porquê |
|---|---|
| `/sovereignty` | O argumento central. Onde estão os dados, que leis, que fornecedores, o que muda face aos EUA |
| `/migration` | O maior travão de compra: sair do Google/Microsoft sem perder correio nem agenda |
| `/public-sector` | Compras públicas, requisitos de alojamento, faturação, referências |
| `/product/<app>` | Uma por app (mail, drive, docs, meet, chat, calendar, wiki, boards): é o que traz tráfego de pesquisa |
| `/compare/opendesk`, `/compare/proton`, `/compare/infomaniak` | Já há Google/Microsoft; falta a dos vizinhos europeus |

**Novas — legais e confiança** (as oito do desenho). São a espinha da mensagem: o que nos distingue
não é dizer "soberania", é ter isto escrito e público.

| URL | O que leva |
|---|---|
| `/privacy` | Política de privacidade (existe, a rever para a nova posição) |
| `/cookies` | Política de cookies — separada da privacidade, como no desenho |
| `/terms` | Termos de serviço (existe) |
| `/security` | Como se protege: cifra em trânsito e em repouso, acesso, backups, réplica, resposta a incidentes, divulgação responsável |
| `/dpa` | Acordo de subcontratação, **texto completo na página** (o modelo da TensorX é bom): definições, papéis, violação de dados, subprocessadores, transferências, auditoria + Anexo I (detalhes do tratamento) e Anexo II (medidas técnicas e SCCs) |
| `/acceptable-use` | O que não se pode fazer com a plataforma — é o que permite suspender abuso sem discussão |
| `/sla` | Disponibilidade prometida, como se mede, exclusões, créditos por escalão, como se reclama, tempos de resposta do suporte |
| `/sub-processors` | A lista completa: **Subprocessador · Finalidade · Localização · Mecanismo de transferência**, com data da última atualização, compromisso de **aviso prévio** antes de acrescentar um novo, direito de objeção do cliente e subscrição de avisos |

A página de subprocessadores é a mais importante das oito. É lá que se vê que a Cloudflare e a AWS
existem na pilha — e é precisamente por o dizermos que a conversa se ganha. Com as decisões atuais,
a lista fica com: Netcup (servidores, DE), Cloudflare R2 (ficheiros, jurisdição UE), Cloudflare
(DNS/CDN), Amazon SES (envio, IE), TensorX (IA), Stripe (pagamentos), e o que mais entrar.

**[decidir]** três coisas que a lista obriga a fixar: quantos dias de aviso prévio damos (14 é o
costume), para onde vai o pedido de objeção, e se há página de estado pública para o SLA se apoiar.

**Mudam:** início (novo herói e nova ordem — soberania, âmbito, provas, preço), `/compare` (juntar
a coluna "jurisdição"), `/pricing` (manter a calculadora, boa, mas mais abaixo).

---

## 5. Idiomas

Hoje: `pt` (neutro, a puxar para o Brasil), `en`, `es`. Estrutura já feita (`/[locale]`,
dicionários tipados) — acrescentar idiomas é trabalho de tradução, não de código.

**Nível 1 — no lançamento**

| Idioma | Porquê |
|---|---|
| **Inglês** | Idioma franco da UE nos negócios; serve Irlanda, Nórdicos, Báltico e quem não tiver a sua língua |
| **Alemão** | Maior mercado, maior orçamento para soberania, e o que mais valoriza "servidores na Alemanha" |
| **Francês** | Política de soberania estabelecida; público habituado a alternativas nacionais |
| **Polaco** | É a nossa sede: credibilidade local, faturação local, concursos locais |
| **Espanhol (ES)** | Mercado grande, adoção de SaaS a crescer |
| **Italiano** | Idem, e setor público ativo em nuvem nacional |
| **Português (PT)** | Pedido seu; mercado pequeno mas nosso de casa |
| **Neerlandês** | Países Baixos + Flandres: dos maiores gastos em SaaS por pessoa da UE e debate político vivo sobre dependência americana |

**Nível 2 — depois de validar**: sueco, dinamarquês, checo, romeno.
Sueco e dinamarquês porque os debates municipais sobre a legalidade do Microsoft 365 são
exatamente a nossa conversa. Checo e romeno por dimensão e concorrência fraca.

**Deixar de fora por agora:** norueguês (fora da UE — a história muda, e leem inglês), finlandês,
grego, húngaro e os bálticos (pequenos e confortáveis em inglês). Malta e Irlanda: inglês.

**Decidido:** `pt-BR` sai por agora. O português passa a ser **de Portugal** — o que obriga a uma
passagem de vocabulário no que já existe ("arquivos"→"ficheiros", "baixar"→"transferir",
"equipe"→"equipa", "usuário"→"utilizador", "tela"→"ecrã"), tanto na landing como na app.

**A fonte de verdade passa a ser o inglês.** Hoje é o `pt.ts`. Com oito idiomas, traduzir a partir
do inglês é mais barato, mais fiável e é o que qualquer agência ou ferramenta espera.

### E a app, que é o trabalho grande

Os idiomas não são só do site. A app tem hoje **2 413 textos por idioma** (110 KB de JSON), em
`pt-BR` e `en`. Passar a oito é multiplicar isso por quatro face ao que existe:

| Superfície | Onde | Estado | Trabalho |
|---|---|---|---|
| App (webmail, painel, registo, primeiro acesso) | `frontend/src/i18n/locales/<lang>/common.json` | 2 413 textos × 2 idiomas | +6 idiomas, e converter o `pt-BR` em `pt-PT` |
| Erros do servidor | mesmos ficheiros (`server.<código>`) | já traduzidos pelo cliente | acompanham o resto |
| **E-mails transacionais** | `backend/internal/notify/messages.go` | **escritos em código**, só pt e en | tirar para ficheiros e traduzir — são o que o cliente lê fora da app |
| Landing | `landing/lib/locales/…` | pt, en, es | +5, e a passagem para pt-PT |
| Consola do operador (manager) | `manager/src/lib/strings.ts` | interno | **não traduzir** — é para nós |

Duas coisas a acertar já, porque mudam o desenho:
- **Idioma por utilizador** já existe (`mail_users.language`) e o seletor também. Falta decidir se
  a organização impõe um idioma por omissão aos novos utilizadores. **[decidir]**
- **Os e-mails saem no idioma de quem recebe** — já é assim; mas com oito idiomas convém que o
  texto viva em ficheiros de tradução como o resto, e não em Go.

**Custo realista por idioma:** ~700 linhas de texto hoje, ~1 200–1 500 com as páginas novas. Por
idioma: tradução + revisão nativa + manutenção de cada página futura. É a manutenção que morde —
cada secção nova passa a ser oito. Sugiro fixar o conteúdo em português/inglês, congelar, e só
depois traduzir tudo de uma vez.

---

## 6. Mecânica (SEO e i18n)

- **URLs em inglês em todos os idiomas.** Perde-se um pouco de SEO local (uma página `/de/preise`
  posicionaria melhor em alemão do que `/de/pricing`), mas ganha-se em partilha, suporte e citação
  legal. Compensa-se com `<title>`, `<h1>` e conteúdo no idioma — que é o que mais pesa.
- `hreflang` recíproco entre os oito, mais `x-default` para o inglês.
- Deteção por `Accept-Language` já existe; manter o seletor visível — quem chega de fora não quer
  ser adivinhado.
- Moeda por país já existe (5 moedas) — acrescentar zloty se faltar, e mostrar preços **com IVA**
  para consumidor e **sem** para empresa, com a nota da autoliquidação.
- **Páginas legais têm de ser datadas e versionadas.** "Última atualização" visível, e o histórico
  guardado: um cliente que assinou em março tem direito a ver o texto de março.
- Termos, privacidade e DPA são texto **legal**: revisão jurídica por idioma, não tradução
  automática.
- Páginas por país (`/de/…` com "servidores na Alemanha" em destaque) valem mais que páginas por
  idioma genéricas.
- Na app, o mesmo dicionário serve oito idiomas: convém uma verificação no CI de que **nenhuma
  chave falta** em nenhum idioma (hoje o TypeScript garante-o na landing, mas não no JSON da app).

---

## 7. Fases

| Fase | O quê | Depende de |
|---|---|---|
| **0** | Pilha: R2 para jurisdição UE, IA e embeddings pela TensorX (`qwen3-embedding-8b`) | Decisões tomadas — falta confirmar a região da TensorX |
| **1** | As oito páginas legais em **inglês e português**, com as URLs em inglês. Começar pelos subprocessadores, DPA e SLA | Fase 0, para a lista já refletir a realidade |
| **2** | Nova mensagem: início, `/sovereignty`, `/security` | Fase 1 (as provas têm de existir antes de se apontar para elas) |
| **3** | `/migration`, páginas por app, comparações com Proton/openDesk/Infomaniak | Fase 2 |
| **4** | **Congelar o conteúdo** e traduzir: alemão, francês, polaco — site e app | Conteúdo estável |
| **5** | Espanhol, italiano, neerlandês; `pt-BR` → `pt-PT` em tudo | Fase 4 |
| **6** | `/public-sector` + nível 2 (sueco, dinamarquês, checo, romeno) | Tração |

A ordem tem uma razão: traduzir antes de o conteúdo assentar é pagar a tradução duas vezes. Por isso
os idiomas vêm depois das páginas, e não ao mesmo tempo.

---

## 8. O que preciso de si

1. **TensorX**: em que região correm os modelos, e quem é a entidade legal? É o que decide se a IA
   entra na frase "processado na UE" ou se fica com uma nota à parte.
2. **Factos legais**: entidade e morada fiscal, DPO nomeado (ou não), certificações em curso, e se
   podemos nomear os data centers da Netcup.
3. **SLA**: que disponibilidade queremos prometer (99,5%? 99,9%?), com que créditos e que tempos de
   resposta do suporte. Prometer a mais é dívida.
4. **Subprocessadores**: dias de aviso prévio (sugiro 14) e endereço para objeções.
5. **Tom por idioma**: um só tom, ou mais formal em alemão e no setor público?
6. **Tradução**: agência, nativos, ou IA com revisão nativa? (o legal — DPA, termos, privacidade —
   deve levar revisão jurídica, não tradução automática)
7. **Quando começo.** Nada disto está implementado; fico à espera da sua palavra.
