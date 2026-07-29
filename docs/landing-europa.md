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
| Aviso de novo subprocessador | **14 dias** |
| Entidade | **Open Solutions** (nome comercial de *Dener Santos Fernandes*, empresário individual — JDG, não sp. z o.o.) · NIP **PL7773457857** · REGON 543340675 · Poznań |
| Certificações | **Nenhuma ainda** — a página de segurança descreve práticas, não invoca normas |
| Morada legal e objeções | **Wawrzyńca Engeströma 10, 60-571 Poznań, Polónia** · NIP PL7773457857 · REGON 543340675 |
| Página de estado | **Sim**, em `status.beskos.com`, na mesma infraestrutura por agora (VPS separado em breve) |
| Tom | **Corporativo**, igual em todos os idiomas — tratamento formal em cada um |
| Seguro | **Não por agora** — o que obriga a limitar a responsabilidade nos Termos (ver §2b) |
| SLA | **Fora por agora.** Em vez dele, uma página de *compromissos de serviço* sem créditos (ver §4c) |

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

1. **Morada:** publica-se `Wawrzyńca Engeströma 10, 60-571 Poznań` — escritório virtual. A VIES
   devolve a **residência** (`Skrzynki, Łagodna 7 m1`), que é o que está no registo. Publicar a
   morada virtual está certo (não se põe a casa de ninguém numa página pública), mas há um passo a
   dar: **registar Poznań no CEIDG como morada para entrega de correspondência** (*adres do
   doręczeń*) e confirmar que o contrato do escritório virtual aceita receber citações. Assim o que
   está publicado, o que está no registo e o que sai nas faturas passam a bater certo — sem isso,
   uma carta oficial vai para a sua casa e o cliente que confia na página fica a olhar para outra
   morada.
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

**Sem seguro (decidido), a proteção passa a ser contratual.** É legítimo, mas tem de estar escrito:
- **limitação de responsabilidade** nos Termos — teto igual ao que o cliente pagou nos últimos 12
  meses, que é o padrão do setor;
- **exclusão de danos indiretos** (lucros cessantes, perda de dados de terceiros);
- **créditos de SLA como único remédio** pela indisponibilidade, e pagos em crédito de conta, não em
  dinheiro.

Sem estas três cláusulas, um SLA com créditos e um DPA com indemnizações apontam ao património
pessoal. Com elas, o risco fica dimensionado ao contrato. A sp. z o.o. e o seguro ficam para quando
aparecer o primeiro contrato que os exija.

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

**Tom: corporativo, e igual em todos os idiomas** (decidido). Isso muda o texto atual, que é
informal e imperativo (*"Cancele quatro assinaturas"*). Na prática, por idioma:

| Idioma | Tratamento formal |
|---|---|
| Português (PT) | 3.ª pessoa — "a sua equipa", "poderá exportar" |
| Inglês | Neutro-formal; sem gíria nem exclamações |
| Alemão | **Sie** (nunca *du*) |
| Francês | **vous** |
| Espanhol | **usted** |
| Italiano | **Lei** |
| Neerlandês | **u** |
| Polaco | **Pan/Pani/Państwo** |

Corporativo aqui quer dizer **sóbrio e preciso**, não pomposo: frases curtas, números concretos,
zero superlativos. É também o registo que o comprador de soberania espera — quem escolhe onde ficam
os dados da empresa não quer um site a falar-lhe como a um amigo. Isto entra no prompt de tradução,
por isso é barato de aplicar e de corrigir.

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
| `/sla` | **Compromissos de serviço**, não SLA contratual (§4c): objetivo de 99,5%/mês, janelas de manutenção, objetivos de resposta do suporte, e a frase que diz que não é garantia |
| `/sub-processors` | A lista completa: **Subprocessador · Finalidade · Localização · Mecanismo de transferência**, com data da última atualização, compromisso de **aviso prévio** antes de acrescentar um novo, direito de objeção do cliente e subscrição de avisos |

A página de subprocessadores é a mais importante das oito. É lá que se vê que a Cloudflare e a AWS
existem na pilha — e é precisamente por o dizermos que a conversa se ganha. Com as decisões atuais,
a lista fica com: Netcup (servidores, DE), Cloudflare R2 (ficheiros, jurisdição UE), Cloudflare
(DNS/CDN), Amazon SES (envio, IE), TensorX (IA), Stripe (pagamentos), e o que mais entrar.

Aviso prévio fixado em **14 dias**. As objeções vão para a morada legal (Poznań) e — porque ninguém
manda carta para objetar a um subprocessador — também para **`privacy@beskos.com`**, que é o canal
prático. A página de estado passa a existir (§4b) e é a fonte de medição do SLA.

**Mudam:** início (novo herói e nova ordem — soberania, âmbito, provas, preço), `/compare` (juntar
a coluna "jurisdição"), `/pricing` (manter a calculadora, boa, mas mais abaixo).

---

## 4c. Sem SLA (por agora)

Decidido: **não há SLA**. É a decisão certa para uma pessoa só — um SLA com créditos é uma dívida
que se paga sozinho às três da manhã, e sem seguro nem sociedade aponta ao património pessoal.

Mas não publicar nada sobre disponibilidade também custa: é a segunda pergunta de qualquer
comprador. A saída é uma página de **compromissos de serviço** — mesma URL `/sla`, outro conteúdo:

| Em vez de… | Escrevemos |
|---|---|
| "99,5% garantidos, com créditos" | "Trabalhamos para **99,5%** ao mês. Não é uma garantia contratual" |
| Créditos automáticos | "Se falharmos de forma prolongada, falamos consigo" |
| Compensação por indisponibilidade | Janelas de manutenção anunciadas, histórico público na página de estado |
| — | Tempos de resposta do suporte como **objetivo**, não promessa |

Isto dá ao comprador o que ele precisa (saber o que esperar) sem criar uma obrigação que hoje não
consegue honrar. E é coerente com o resto da mensagem: quem publica a lista de subprocessadores é
quem também admite não ter SLA.

**Quando voltar a fazer sentido:** com sociedade (sp. z o.o.), seguro e alguém de piquete. Aí o SLA
passa a argumento de venda em vez de risco.

**Nos Termos, isto tem de ficar explícito:** serviço prestado em regime de *melhores esforços*,
teto de responsabilidade nos 12 meses pagos, exclusão de danos indiretos. Sem SLA, estas cláusulas
são ainda mais importantes — são a única fronteira que existe.

---

## 4b. Página de estado

Decidido: existe, é pública, e cobre todas as APIs. É ela que sustenta o SLA — sem uma medição que
o cliente possa ver, "99,5%" é uma afirmação nossa contra a memória dele.

**O que se mede** (um componente por linha na página):

| Componente | Sonda |
|---|---|
| Aplicação (app.beskos.com) | GET da página, código 200 |
| API | endpoint público de saúde, com verificação da base de dados |
| Correio — entrega (SMTP) | ligação a `mail.beskos.com:465` + `EHLO` |
| Correio — leitura (IMAP/JMAP) | sessão JMAP com conta de teste |
| Drive / ficheiros | escrita e leitura de um objeto pequeno no armazenamento |
| Documentos (OnlyOffice) | saúde do serviço de edição |
| Reuniões (LiveKit) | saúde do servidor de sala |
| Assistente (TensorX) | pedido mínimo ao modelo |
| Consola do operador | GET da página |
| Site | GET da página |

**Onde vive:** `status.beskos.com`, por agora na mesma infraestrutura, com a troca para um VPS
separado a caminho. **Sem aviso na página** sobre essa limitação (decidido) — e o risco é pequeno:
numa falha total a página fica inacessível, o que é bem menos mau do que uma página a garantir que
está tudo bem enquanto não está. Nas falhas parciais, que são a maioria, ela funciona e é útil.

O subdomínio é a convenção (`status.stripe.com`, `status.github.com`) e é o que as pessoas escrevem
à mão quando algo falha. Além disso poupa a migração: quando a página mudar de servidor, muda-se um
registo de DNS e mais nada. `/status` no site redireciona para lá.

**Como:** [Uptime Kuma](https://github.com/louislam/uptime-kuma) (código aberto, self-hosted,
alojável na UE) resolve 90% — sondas, histórico, página pública, avisos, RSS. Alternativa: escrever
a nossa, o que só faz sentido se quisermos as sondas profundas (enviar um e-mail de teste de ponta a
ponta e medir quanto tempo demora a chegar). Recomendo começar pelo Kuma e só depois acrescentar as
sondas profundas.

**O que a página tem de mostrar:** estado de cada componente, **90 dias** de histórico, incidentes
com cronologia (detetado → causa → resolvido), e subscrição por e-mail. A mesma subscrição serve
para os avisos de subprocessadores — é uma lista só.

**[decidir]** `status.beskos.com` (subdomínio) ou `/status`? Recomendo subdomínio: se o ingress
principal cair, o `/status` cai com ele.

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

## 6b. Tradução: tudo por IA, com revisão manual depois

**Decidido:** traduzimos tudo por IA e o Dener revê. Sem agência, sem freelancers.

Volume real (contado, em inglês): **app ~8 400 palavras** (2 413 textos) + **landing ~3 200
palavras** (~500 textos) ≈ **11 600 por idioma hoje**, e cerca de **25 000** com as páginas novas.
Seis idiomas novos.

### O que isto exige para correr bem

**1. Glossário primeiro.** Antes da primeira palavra: como se diz *mailbox, drive, board, meet,
workspace, sub-processor, seat, tenant* em cada idioma. Sem glossário, a mesma coisa ganha dois
nomes em ecrãs diferentes — o defeito mais visível de tradução automática, e o mais caro de
desfazer depois.

**2. Plurais não são todos iguais.** É o erro técnico que mais aparece:

| Idioma | Formas que o i18next precisa |
|---|---|
| en, de, es, it, nl, sv | `_one`, `_other` |
| fr | `_one`, `_many`, `_other` |
| **pl** | `_one`, `_few`, `_many`, `_other` |
| pt | `_one`, `_other` (mas com "milhões" a puxar `_many` em alguns casos) |

Hoje temos chaves como `unreadElsewhere_one/_other`. O polaco precisa de **quatro** — se a tradução
não as gerar, a app mostra a chave crua a um cliente polaco. O script tem de conhecer isto por
idioma, e o teste de chaves em falta tem de o verificar.

**3. Marcadores e formatação intactos.** `{{count}}`, `{{email}}`, `<b>`, `\n` — a IA às vezes
traduz o nome do marcador ou come a pontuação. Validação automática: cada texto traduzido tem de
ter exatamente os mesmos marcadores que o original.

**4. O alemão é ~30% mais comprido que o inglês.** Botões, menus e o rail de apps vão rebentar.
Depois de traduzir: passar o ecrã principal, as definições e o registo em alemão e ver o que
transborda. É trabalho de layout, não de tradução.

**5. Legal: o inglês prevalece.** Com tradução por IA isto deixa de ser conselho e passa a ser
condição. Cada página legal traduzida leva: *"Esta tradução é fornecida por conveniência. Em caso de
divergência, prevalece a versão em inglês."* Uma revisão jurídica **em inglês** (advogado polaco com
prática de GDPR/SaaS) e as traduções seguem por IA sem risco contratual.

**6. O limite da revisão manual, dito com franqueza.** Você revê português, inglês e espanhol com
propriedade, e polaco até certo ponto. **Alemão, francês, neerlandês e italiano ficam sem revisor**
— e são três dos mercados que queremos. Duas redes de segurança baratas:
- um link discreto *"reportar erro de tradução"* no rodapé, que abre e-mail com a página e o idioma;
- tratar os primeiros clientes de cada mercado como revisores: um pedido simpático apanha mais
  erros do que uma agência.

### Como o script vai funcionar (quando começarmos)

1. Lê o dicionário **inglês** (fonte de verdade) e o glossário.
2. Traduz em blocos, com o contexto da chave (`webmail.pwHintShort` diz mais do que a frase).
3. Gera as formas plurais certas para o idioma.
4. Valida: marcadores iguais, sem chaves em falta, JSON válido, plurais completos.
5. Escreve o ficheiro do idioma e mostra um `diff` para revisão.
6. Corre outra vez só o que mudou — não retraduz o que já foi revisto (marca de revisão por chave).

---

## 7. Fases

| Fase | O quê | Depende de |
|---|---|---|
| **0** ✅ | Pilha: R2 para jurisdição UE, IA e embeddings pela TensorX (`qwen3-embedding-8b`) | **Feita** a 29/07/2026 — ver §7b |
| **1** | **Página de estado** (na infraestrutura atual) + as nove páginas legais em inglês e português, com URLs em inglês. Começar pelos subprocessadores e pelo DPA | Fase 0, para a lista já refletir a realidade · `/sub-processors` ✅ |
| **2** ✅ | Nova mensagem: início, `/sovereignty`, `/security` | **Feita** a 29/07/2026 — ver §7c |
| **3** | `/migration`, páginas por app · comparações com Proton/openDesk/Infomaniak ✅ | Fase 2 |
| **4** | **Congelar o conteúdo** e traduzir: alemão, francês, polaco — site e app | Conteúdo estável |
| **5** | Espanhol, italiano, neerlandês; `pt-BR` → `pt-PT` em tudo | Fase 4 |
| **6** | `/public-sector` + nível 2 (sueco, dinamarquês, checo, romeno) | Tração |

A ordem tem uma razão: traduzir antes de o conteúdo assentar é pagar a tradução duas vezes. Por isso
os idiomas vêm depois das páginas, e não ao mesmo tempo.

---

## 7b. Fase 0 — o que ficou feito (29/07/2026)

**Ficheiros.** A jurisdição não se muda num balde do R2 já criado: passou-se para **`beskos-data`**,
criado em jurisdição UE (endpoint `<conta>.eu.r2.cloudflarestorage.com`). Os 73 objetos do balde
antigo foram copiados e **verificados um a um** por tamanho e md5 — a cópia entre jurisdições passa
obrigatoriamente pelo cliente, não há `CopyObject` de uma para a outra. Segunda passagem depois do
corte, sem diferenças. O balde antigo **fica intacto**, como rede de segurança; apagar é decisão
posterior e a quente não se apaga nada.

**IA.** `AI_PROVIDER=tensorx`, mesmo protocolo (compatível com OpenAI), destino `api.tensorx.ai/v1`.
Modelo `deepseek/deepseek-v4-flash`. O que muda para a mensagem: os modelos são servidos em
infraestrutura europeia e o conteúdo **não chega a quem os desenvolveu** — que é a frase que a
página de subprocessadores agora sustenta.

**Embeddings.** `qwen/qwen3-embedding-8b`. Duas surpresas, ambas resolvidas no nosso lado:

1. A TensorX **recusa o parâmetro `dimensions`** com 400 e devolve sempre 4096 dimensões.
2. A coluna é `vector(1536)` e o índice ivfflat do pgvector **não passa das 2000** — com 4096
   perdia-se o índice e a busca semântica passava a varrer a tabela inteira.

Cortam-se as primeiras 1536 componentes e renormaliza-se (o Qwen3 é treinado com Matryoshka, tal
como o `text-embedding-3` da OpenAI: o significado está à cabeça do vetor). A renormalização não é
detalhe — a busca compara por produto interno, e sem ela os textos longos ficariam sistematicamente
à frente. Migração `00127` limpa `wiki_chunks`, porque vetores de modelos diferentes não são
comparáveis; o worker reindexa sozinho.

**Uma lição para registo:** o primeiro commit da Fase 0 descrevia na mensagem um `case "tensorx"`
que nunca chegou ao `config.go`. Em produção deu silêncio — IA desligada, `/api/ai/briefing` a 503 —
e só apareceu ao ler os logs de arranque. Mensagem de commit não é prova; log de produção é.

---

## 7c. Fase 2 — o que ficou feito (29/07/2026)

**A página inicial deixou de vender preço.** O herói passou a *"O trabalho da sua equipa, sob lei
europeia"* e a calculadora desceu para junto do preço, com o enquadramento invertido — *alojar na
Europa não é a opção cara* em vez de *poupe dinheiro*. Nova ordem: herói → onde estão os dados →
quem opera → âmbito → integração → IA → saída → custo → preços → fecho.

**As provas passaram a estar na primeira dobra.** Duas secções novas: `where` (tabela de oito
componentes com operador, país e mecanismo de transferência, gerada de `lib/infrastructure.ts`) e
`who` (entidade, morada e NIP/REGON, lidos de `lib/subprocessors.ts` para não haver duas verdades).
Os três fornecedores americanos aparecem lá, com as CCT declaradas. As páginas legais já existiam
desde a Fase 1 e a home não ligava a nenhuma; agora cada cartão liga à que o prova.

**`/sovereignty`** leva o argumento completo, com a secção *o que não afirmamos*: sem certificações,
sem SLA contratual, três fornecedores sujeitos a lei estrangeira e migração acompanhada em vez de
automática.

**Comparações nos dois eixos.** A tabela ganhou `euJurisdiction` (a que lei responde a empresa) e
`managed` (serviço gerido ou software para alojar) — sem elas, a comparação decorria toda no
terreno de quem já lá está. Entraram Proton, Infomaniak kSuite e openDesk: contra estes não se ganha
por jurisdição, ganha-se por âmbito, e as páginas dizem-no. O openDesk não tem preço de tabela e a
página omite o custo em vez de inventar um.

**Idiomas.** O inglês passou a fonte de verdade (`Messages = typeof en`) e a idioma por omissão; o
português passou a europeu e a registo corporativo em toda a landing, não só no legal.

**Preços verificados** (julho de 2026): Proton Business Suite 12,99 USD/utilizador/mês no anual;
Infomaniak kSuite Business CHF 7,33 (~8,03 €), convertido pelo câmbio fixo já declarado.

---

## 8. Está tudo decidido

| # | Decisão |
|---|---|
| Morada | Poznań publicada; CEIDG a atualizar (o Dener trata) |
| Estado | `status.beskos.com`, mesma infraestrutura, sem aviso de limitação |
| SLA | Fora; fica a página de compromissos |
| Tom | Corporativo, formal em todos os idiomas |
| Tradução | Toda por IA, revisão manual depois |
| Idiomas | en, de, fr, pl, es, it, pt-PT, nl (nível 1) · sv, da, cs, ro (nível 2) |
| URLs | Sempre em inglês |
| Pilha | R2 em jurisdição UE; IA e embeddings pela TensorX (100% UE) |

**Fase 0 feita e no ar** (§7b), e a página de subprocessadores publicada nos três idiomas atuais.
A seguir, o resto das páginas legais e a página de estado.

---

## 9. Uma nota sobre o tamanho

Você disse "um developer com um sonho". Vale a pena decidir o que fazer com isso, porque a mensagem
muda conforme:

- **Escondido**, é uma bomba-relógio: quem descobre tarde sente-se enganado, e descobre-se sempre.
- **Dito**, é um argumento — em soberania, "uma pessoa em Poznań que responde ao telefone" bate uma
  megaempresa americana para muita PME, agência e profissional liberal. É a mesma razão por que se
  escolhe o contabilista da esquina.

O que ele **não** ganha é o setor público e a empresa grande: aí pedem sociedade, seguro, SLA e
certificação — e é por isso que a `/public-sector` deve ficar para o fim do plano, não para o
princípio. O alvo realista destes primeiros meses são PME europeias com 5 a 50 pessoas que já hoje
desconfiam do Google e do Microsoft.
