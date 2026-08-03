# Leis do projeto Beskos

Regras **obrigatórias** para todo código deste repositório. Valem para humanos e agentes.
Complementam — não substituem — as restrições inegociáveis do [project.md](project.md) (§2) e as
[divergências do Stalwart](deploy/DIVERGENCES.md).

## 1. Arquitetura e simplicidade

1.1. **Programe simples.** Prefira a solução óbvia à esperta. Sem abstração especulativa —
     introduza interface/generics só quando há ≥2 usos reais. YAGNI.
1.2. **Separe lógicas por responsabilidade.** Cada pacote/módulo tem um propósito. Não misture
     HTTP, regra de negócio e acesso a dados no mesmo arquivo/função.
1.3. **Camadas no backend:** `httpapi` (transporte) → serviço/regra → `db` (sqlc) / `stalwart` (JMAP).
     Handlers não montam SQL nem falam JMAP direto quando há lógica; extraia para um serviço.
1.4. **Arquivos pequenos.** Máximo **600 linhas** por arquivo (mire em ≤300). Passou disso,
     quebre por responsabilidade. Exceção única: código **gerado** (`internal/db/*`).
1.5. **Funções curtas e com nome claro.** Uma função faz uma coisa. Evite >~60 linhas.

## 2. Padrões Go

2.1. Siga **Effective Go** e os **Go Code Review Comments**. `gofmt` e `go vet` limpos são
     condição de merge. Nada entra com formatação suja.
2.2. **Erros:** retorne, não entre em pânico (exceto `main`/inicialização impossível). Enriqueça com
     `fmt.Errorf("contexto: %w", err)`. Trate ou propague — nunca engula silenciosamente.
2.3. **`context.Context`** é o primeiro parâmetro em toda função que faz I/O.
2.4. Nomes idiomáticos: sem stutter (`stalwart.Client`, não `stalwart.StalwartClient`); acrônimos
     em maiúscula (`ID`, `URL`, `HTTP`, `JMAP`, `DNS`).
2.5. **Sem dependências desnecessárias.** Prefira a stdlib. Cada lib nova precisa de justificativa.
2.6. Config e segredos **só via ambiente** (`internal/config`). Segredo no código = bug.
2.7. **Testes junto com o código** (brief §11). Todo pacote com lógica tem `_test.go`.
     Testes de integração são pulados sem as envs (não quebram CI sem container).
2.8. **Laço que engole erro tem de ESCALAR.** Um trabalhador periódico que faz
     `log.Printf(...); continue` está certo para uma falha passageira — e cego para uma permanente.
     Conte as falhas **consecutivas** e grite a partir de um limiar (`consecutive >= N`), avisando
     também quando recuperar. Ver [`watchdog.go`](backend/cmd/worker/watchdog.go) como referência.

     > Escrita a 31/07/2026 porque o vigia de reputação falhou **100% das passagens** durante dias
     > sem que ninguém soubesse: a SES devolvia `HTTP 400` de hora a hora, o `continue` seguia para o
     > domínio seguinte e a linha perdia-se no meio do registo. É a defesa que impede um cliente de
     > queimar a reputação da conta inteira — esteve desligada e parecia ligada. Um erro que se
     > repete sempre não é um erro: é uma avaria.

2.9. **"Ainda não" tem de ser distinguível de "nunca".** Um passo que espera por algo de fora
     (DNS publicado, pagamento, ficheiro carregado) devolve o mesmo "ainda não" quando a espera é
     legítima e quando a condição nunca se vai cumprir. Se espera por um alvo — um registo, um
     identificador, um caminho — **registe qual é** na primeira vez e **grite** quando a espera
     passa de um limiar. Sem isso, o ecrã diz "à espera do cliente" e o cliente já fez a parte dele.

     > Escrita a 01/08/2026 depois de um domínio ficar mais de uma hora em `dns_pending` com todos
     > os registos publicados e visíveis do mundo inteiro. O verificador procurava um DKIM cujo
     > conteúdo tinha sido apagado por um bug anterior, e uma consulta que não encontra nada é
     > exatamente igual, ali, a um cliente que ainda não publicou. Nada nos registos, nada no ecrã:
     > só o cliente a olhar para "à espera de DNS" e a concluir que o produto não funciona.

## 3. Padrões Frontend (React/TS)

3.1. TypeScript **strict**. Nada de `any` sem comentário justificando.
3.2. Componentes pequenos e de responsabilidade única (mesma lei dos 600/300 linhas).
     Um componente por arquivo; página em `pages/`, reutilizável em `components/`.
3.3. Estado de servidor via **TanStack Query**; não reinvente cache/fetch.
3.4. Toda chamada à nossa API passa por `src/api.ts`. Componentes não fazem `fetch` cru.
3.5. **Nenhuma credencial de admin do Stalwart no frontend** (project.md §2). O webmail fala JMAP
     com **token do usuário final**; o resto fala com a nossa API Go.

## 4. Internacionalização (i18n) — OBRIGATÓRIO

4.1. **Nenhuma string visível ao usuário fica hard-coded.** Todo texto de UI vem do sistema de i18n.
4.2. Frontend usa **`i18next`/`react-i18next`**. Chaves em `src/i18n/locales/<lang>/*.json`.
     Idioma default **`pt-BR`**; **`en`** obrigatório. Toda chave existe em todos os idiomas.
4.3. Use chaves semânticas (`login.submit`, não `"Entrar"`). Plurais/interpolação via i18next.
4.4. **Mensagens de erro da API** voltam com uma **chave** (`error_code`) além do texto, para o
     frontend traduzir. Backend não assume idioma do usuário no texto exibível.
4.5. Datas/números/moeda via `Intl` conforme o locale — não formate à mão.
4.6. **Idioma do código: TUDO em inglês.** Nomes (variáveis, funções, tipos, campos, ficheiros),
     **comentários**, colunas da base de dados, chaves JSON, flags de CLI, logs e mensagens de erro.
     Vale para backend e frontend. A **única** exceção é o texto que o utilizador final lê, que vem
     do i18n — e onde tiver de ficar em código (ex.: SMS), marque a linha com `lang:ok`.
     O `error_code` é o contrato estável; o **frontend traduz** o código via i18n (`server.<code>`)
     e mostra ao usuário no idioma dele. Nunca mostre texto de erro do backend cru na UI.

     > Os comentários eram em português até 31/07/2026. Os que já existem vão sendo migrados; o que
     > se escreve de novo é em inglês. O verificador só olha para linhas NOVAS, por isso o histórico
     > não atrapalha.

4.7. **A lei é verificada por máquina, não por boa vontade.** `scripts/check_laws.py` corre
     automaticamente a cada escrita de ficheiro (hook em `.claude/settings.json`) e valida as leis
     4.6 e 1.4 nas linhas novas. Corra-o à mão antes de commitar: `make check` ou
     `python3 scripts/check_laws.py`. Ele existe porque estas leis já estavam escritas e foram
     quebradas na mesma — um documento não obriga a nada; um comando que falha, sim.

4.8. **Todo o texto que o utilizador lê é formal e corporativo.** Trate o cliente pela forma de
     cortesia do idioma dele — `usted`/`su` (es), `Sie`/`Ihr` (de), `vous`/`votre` (fr), `lei`/`suo`
     (it), `u`/`uw` (nl), `Państwa` (pl), terceira pessoa (pt). **Nunca** o tratamento informal:
     `tu`, `tú`, `du`, `je`/`jij`, `tuo`, `Twój`. Sem exclamações, emoji, gíria nem piadas; frases
     declarativas. Vale para a landing, a documentação, a UI e as mensagens transacionais (email,
     SMS) — **todas as superfícies**, porque o cliente não sabe que são projetos diferentes.
     Verificado por `scripts/check_register.py`, incluído no `make check`.

     **O tratamento formal não chega — o tom também é a lei.** Escreva declarativo: diga o que o
     sistema faz. Não justifique a decisão de desenho ao leitor, não comente, não opine, não faça
     perguntas retóricas. "O envio é suspenso até haver subscrição" — não "o envio para, porque um
     teste que esconde o que se estava a avaliar responde à pergunta errada". Isto **não** é
     verificado por máquina: os pronomes são, o tom é item de revisão.

     > Escrita a 03/08/2026, depois de o registo já divergir entre superfícies sem ninguém dar por
     > isso: a documentação polaca tratava o cliente por `Państwa` e a landing polaca por `Twój`; o
     > mailcheck italiano estava inteiramente em `tu` enquanto o resto do italiano usava `lei`. Quem
     > abre as duas páginas na mesma sessão não vê duas escolhas de estilo — vê duas empresas. E o
     > mailcheck é a porta de entrada do site: era exatamente a página onde a informalidade custava
     > mais.
     >
     > O parágrafo do tom foi acrescentado no mesmo dia, algumas horas depois. O verificador tinha
     > acabado de dar tudo por limpo e a documentação inteira — dezassete artigos em oito idiomas —
     > continuava escrita em registo de ensaio, a justificar cada decisão ao leitor. Os pronomes
     > estavam certos. Uma máquina que só sabe medir pronomes assina por baixo de um texto que
     > nenhum cliente empresarial leria até ao fim, e o "limpo" que ela imprime passa a valer menos
     > do que parece.

## 5. Segurança e multi-tenancy (reforço do project.md)

5.1. **Toda query de dados de cliente filtra `tenant_id`.** Use sempre o tenant do usuário
     autenticado (contexto), nunca um vindo do request.
5.2. Cheque **posse do recurso** antes de qualquer operação admin no Stalwart.
5.3. **Idempotência** em provisionamento e webhooks — assuma que toda chamada de rede pode repetir.
5.4. Nunca modifique o Stalwart; só via rede (JMAP). Ver [DIVERGENCES.md](deploy/DIVERGENCES.md).

## 6. Processo

6.1. **Uma fase por vez** (project.md §9). Não comece a Fase N+1 antes da N testada.
6.2. Decisões estruturais (libs, schema, auth): proponha trade-offs e **pergunte** antes.
6.3. Documente decisões e setup no README conforme avança.
6.4. Divergências entre brief e realidade do Stalwart → confie na realidade e **registre em DIVERGENCES.md**.
