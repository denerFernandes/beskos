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
| IA | **TensorX corre 100% na UE** — a IA entra na frase "processado na UE", sem nota de rodapé |
| SLA | **99,5%/mês** |
| Aviso de novo subprocessador | **14 dias** |
| Entidade | **Open Solutions** (nome comercial de *Dener Santos Fernandes*, empresário individual — JDG, não sp. z o.o.) · NIP **PL7773457857** · REGON 543340675 · Poznań |
| Certificações | **Nenhuma ainda** — a página de segurança descreve práticas, não invoca normas |

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
| Assistente de IA | DeepSeek **via TensorX (100% UE)** | Resolvido: o conteúdo deixa de sair da UE | Escrever na página: modelo servido na UE, não pelo criador do modelo |
| Embeddings da wiki | **`qwen/qwen3-embedding-8b`** via TensorX (UE) | Resolvido | Idem |

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

## 2b. A entidade legal (e o que ela obriga)

Verifiquei o NIP na VIES: **PL7773457857 está válido para operações intracomunitárias**, o que
confirma que a autoliquidação que já implementámos funciona connosco como vendedor. Duas coisas
saltaram à vista:

1. **A morada na VIES não é a que me deu.** Lá consta `SKRZYNKI, ŁAGODNA 7 M1, 62-060`; você
   indicou `Wawrzyńca Engeströma 10, 60-571 Poznań`. As faturas e as páginas legais devem bater
   certo com o registo — **[decidir]** qual é a morada de registo e qual é a de correspondência.
2. **O sujeito legal é a pessoa, não a marca.** Em JDG, quem contrata é *Dener Santos Fernandes*;
   "Open Solutions" é nome comercial. As páginas legais devem identificar assim:
   *"Open Solutions — Dener Santos Fernandes, Poznań, NIP PL7773457857, REGON 543340675"*. Esconder
   o nome não é opção: a identificação do prestador é obrigatória (art. 5 da Diretiva 2000/31/CE e
   a lei polaca dos serviços eletrónicos), e num mercado que compra confiança, ser claro joga a
   favor.

**O que isto custa, dito sem rodeios:** como empresário individual, responde com o património
pessoal. Um SLA com créditos e um DPA com indemnizações são obrigações reais. E há concursos
públicos e empresas grandes que exigem sociedade — a JDG chega para PME, trava em contratos
grandes. Não é impeditivo hoje; é um marcador para quando aparecer o primeiro cliente sério.

**Recomendo, por esta ordem:** (a) identificação legal correta nas páginas, (b) **seguro de
responsabilidade civil profissional** antes de prometer créditos de SLA, (c) abrir a sp. z o.o.
quando o primeiro contrato o exigir — e não antes.

**Falta uma página** nas oito: `/imprint` (identificação legal). É esperada na Alemanha e na
Áustria, e resolve a obrigação nos restantes. Barata de fazer: nome, morada, NIP, contacto,
responsável.

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
| `/sla` | **99,5%/mês** (≈3h39 de indisponibilidade máxima), como se mede, exclusões, créditos por escalão, como se reclama, tempos de resposta do suporte |
| `/sub-processors` | A lista completa: **Subprocessador · Finalidade · Localização · Mecanismo de transferência**, com data da última atualização, compromisso de **aviso prévio** antes de acrescentar um novo, direito de objeção do cliente e subscrição de avisos |

A página de subprocessadores é a mais importante das oito. É lá que se vê que a Cloudflare e a AWS
existem na pilha — e é precisamente por o dizermos que a conversa se ganha. Com as decisões atuais,
a lista fica com: Netcup (servidores, DE), Cloudflare R2 (ficheiros, jurisdição UE), Cloudflare
(DNS/CDN), Amazon SES (envio, IE), TensorX (IA), Stripe (pagamentos), e o que mais entrar.

Aviso prévio fixado em **14 dias**. **[decidir]** para onde vai o pedido de objeção
(`privacy@beskos.com`?) e se criamos página de estado pública — sem ela, o SLA mede-se pelos nossos
registos, e o cliente tem de acreditar.

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

## 6b. Tradução: como fazer, e quanto custa

Contei o que existe hoje (em inglês): **app ~8 400 palavras** (2 413 textos) e **landing ~3 200
palavras** (~500 textos) — cerca de **11 600 palavras por idioma**. Com as páginas novas (soberania,
migração, produto, e as nove legais) conto **2 a 2,5×**: à volta de **25 000 palavras por idioma**.
São seis idiomas novos, mais a passagem do português para pt-PT.

Há três caminhos, e o certo é **misturá-los conforme o texto**:

| Caminho | Como | Custo típico | Onde usar |
|---|---|---|---|
| **Agência (LSP)** | Empresa de tradução, com revisor e glossário | €0,10–0,18/palavra → ~€2 500–4 500 por idioma | Não recomendo para tudo: são ~€20 000 nos seis |
| **Nativo freelance** | Um tradutor por idioma (ProZ, Upwork), com glossário nosso | €0,06–0,12/palavra → ~€1 500–3 000 por idioma | Textos de marketing do site, onde o tom vende |
| **IA + revisão nativa** | Traduzimos com IA e um nativo revê o resultado | 3–6 h de revisão por idioma → ~€150–400 | **Interface da app** (frases curtas, repetitivas, contexto fechado) |

**A minha recomendação:**
- **App (8 400 palavras, 2 413 textos):** IA + revisão nativa. É onde a IA acerta mais — "Guardar",
  "Anexar ficheiro", "3 mensagens por ler" — e onde a agência custaria mais por menos valor.
- **Landing (marketing):** nativo freelance por idioma, pelo menos nos três que compram soberania
  (alemão, francês, polaco). Um título mal traduzido custa conversão; um botão, não.
- **Legal (DPA, Termos, Privacidade, SLA, Uso aceitável):** aqui muda o método. Não se traduz para
  ter oito contratos — **fixa-se um idioma que prevalece**.

**A regra que poupa dinheiro e risco no legal:** o **inglês é a versão que governa**, e as outras
são cortesia. Todas as páginas traduzidas levam uma linha do género *"Esta tradução é fornecida por
conveniência. Em caso de divergência, prevalece a versão em inglês."* É o que fazem Stripe, Proton
e companhia — evita pagar revisão jurídica em oito jurisdições e evita o pesadelo de dois contratos
com sentidos diferentes. **Uma** revisão jurídica em inglês (advogado polaco com prática de GDPR e
SaaS), e as traduções seguem o método da app.

**Glossário primeiro.** Antes de traduzir uma palavra: fixar como se diz *mailbox, drive, board,
meet, workspace, sub-processor, tenant, seat* em cada idioma. Sem isso, dois tradutores dão dois
nomes à mesma coisa e a app fica incoerente.

**Ordem prática:** congelar o inglês → glossário → app por IA + revisão → landing por nativos →
legal só em inglês, com a nota de prevalência.

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

## 8. O que falta decidir

1. **Morada legal**: a da VIES (`Skrzynki, Łagodna 7 m1`) ou a de Poznań? As faturas e as páginas
   legais têm de bater certo com o registo.
2. **Endereço para objeções a subprocessadores** e para assuntos de privacidade (`privacy@`?).
3. **Página de estado pública** para o SLA se apoiar — fazemos, ou o SLA mede-se pelos nossos
   registos?
4. **Seguro de responsabilidade civil** antes de prometer créditos de SLA: tratamos disso?
5. **Tom por idioma**: um só, ou mais formal em alemão e no setor público?
6. **Quando começo.** Continua tudo por implementar, à espera da sua palavra.
