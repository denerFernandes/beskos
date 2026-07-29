# Plano — reposicionar a landing na soberania de dados europeia

> Estado: **plano**, nada implementado. Escrito a 29/07/2026.
> Decisões pendentes marcadas com **[decidir]**.

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
| Ficheiros (Drive) | **Cloudflare R2** | Empresa **americana**: sujeita ao CLOUD Act mesmo com dados na UE | Trocar por Scaleway / OVHcloud / Hetzner / IONOS |
| Envio de e-mail | **Amazon SES** (eu-west-1) | Dados na Irlanda, mas fornecedor **americano** | Relay europeu, ou enviar do próprio Stalwart |
| DNS/CDN/TLS | **Cloudflare** | Todo o tráfego passa por um edge americano | Bunny (SI), ou Cloudflare só como DNS e TLS na origem |
| Assistente de IA | **DeepSeek** (China) | Conteúdo do cliente sai da UE — o pior dos casos | **Mistral** (FR) ou modelo próprio |
| Embeddings da wiki | **OpenAI** (EUA) | Idem | Modelo de embeddings europeu ou local |

**Recomendação:** tratar isto como pré-requisito do lançamento, por esta ordem — IA (é conteúdo do
cliente a sair), ficheiros (é o maior volume), envio, CDN. Enquanto não estiver feito, a página só
pode dizer o que é verdade: *"servidores na Alemanha, empresa europeia"*, e não *"nenhum fornecedor
fora da UE"*.

**[decidir]** avançamos com as trocas antes de publicar, ou publicamos uma versão mais modesta já e
reforçamos depois? A minha recomendação é a primeira: a mensagem de soberania só vale se aguentar
uma pergunta de seguimento.

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

Páginas atuais: início, funcionalidades, preços, comparar, docs, download, privacidade, termos.

**Novas:**
| Página | Porquê |
|---|---|
| `/soberania` | O argumento central. Onde estão os dados, que leis, que fornecedores, o que muda face aos EUA |
| `/seguranca` (centro de confiança) | DPA, subprocessadores, cifra, retenção, resposta a incidentes, contacto do DPO |
| `/migracao` | O maior travão de compra: como se sai do Google/Microsoft sem perder correio nem agenda |
| `/setor-publico` | Compras públicas, requisitos de alojamento, faturação, referências |
| `/produto/<app>` | Uma por app (correio, drive, documentos, reuniões, chat, agenda, wiki, quadros): é o que traz tráfego de pesquisa |
| `/comparar/openDesk`, `/comparar/proton`, `/comparar/infomaniak` | Já existe comparação com Google/Microsoft; falta a dos vizinhos europeus |

**Mudam:** início (novo herói e nova ordem — soberania, âmbito, provas, preço), comparar (juntar a
coluna "jurisdição"), preços (manter a calculadora, que é boa, mas abaixo).

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

**Duas decisões que isto levanta:**

1. **Português divide-se.** O dicionário atual é neutro a puxar para o Brasil ("arquivos", "baixar",
   "equipe"). Português de Portugal quer "ficheiros", "transferir", "equipa" — e um leitor
   português nota a diferença de imediato. **[decidir]** mantemos os dois (`pt-PT` e `pt-BR`) ou o
   Brasil deixa de ser alvo desta landing?
2. **A fonte de verdade passa a ser o inglês.** Hoje é o `pt.ts`. Com oito idiomas, traduzir a
   partir do inglês é mais barato, mais fiável e é o que qualquer agência ou ferramenta espera.

**Custo realista por idioma:** ~700 linhas de texto hoje, ~1 200–1 500 com as páginas novas. Por
idioma: tradução + revisão nativa + manutenção de cada página futura. É a manutenção que morde —
cada secção nova passa a ser oito. Sugiro fixar o conteúdo em português/inglês, congelar, e só
depois traduzir tudo de uma vez.

---

## 6. Mecânica (SEO e i18n)

- `hreflang` recíproco entre os oito, mais `x-default` para o inglês.
- Deteção por `Accept-Language` já existe; manter o seletor visível — quem chega de fora não quer
  ser adivinhado.
- Moeda por país já existe (5 moedas) — acrescentar zloty se ainda não estiver, e mostrar preços
  **com IVA** para consumidor e **sem** para empresa, com a nota da autoliquidação.
- Termos e privacidade por idioma são texto **legal**: traduzir com revisão jurídica, não com IA.
- Páginas por país (`/de/…` com "servidores na Alemanha" em destaque) valem mais que páginas por
  idioma genéricas.

---

## 7. Fases

| Fase | O quê | Depende de |
|---|---|---|
| **0** | Trocar os fornecedores não-europeus (IA, ficheiros, envio, CDN) | Decisão sua |
| **1** | Nova mensagem em pt + en: início, `/soberania`, `/seguranca` | Fase 0 para poder afirmar |
| **2** | `/migracao`, páginas por app, comparações com Proton/openDesk/Infomaniak | Fase 1 |
| **3** | Alemão, francês, polaco (os três que compram soberania) | Conteúdo congelado |
| **4** | Espanhol, italiano, português de Portugal, neerlandês | Fase 3 |
| **5** | `/setor-publico` + nível 2 de idiomas (sueco, dinamarquês, checo, romeno) | Tração |

---

## 8. O que preciso de si

1. **Fase 0**: avançamos com as trocas de fornecedor antes de publicar? (recomendo que sim)
2. **Português**: `pt-PT` e `pt-BR` em paralelo, ou só `pt-PT`?
3. **Factos que só você sabe**: entidade legal e país, morada fiscal, se há DPO nomeado,
   certificações em curso (ISO 27001?), e se pode nomear os data centers.
4. **Tom**: o atual é direto e informal. Alemanha e setor público esperam mais formalidade —
   mantemos um tom só, ou ajustamos por idioma?
5. **Tradução**: agência, tradutores nativos, ou IA com revisão nativa?
