# Plano — cifra de envelope dos ficheiros, com chave nossa

> Estado: **no ar** desde 30/07/2026. Escrito como plano a 29/07.
> Cifra de envelope no nosso backend (não SSE-C, não E2EE). Gravações e cópias de segurança
> incluídas. Ler o §2f antes de mexer na selagem — custou 43 mensagens de correio.

## 1. O problema, dito com precisão

Os ficheiros **estão** cifrados em repouso — pelo Cloudflare R2, com AES-256, de forma transparente.
A página de segurança diz exatamente isso ("cifrados em repouso **pelo fornecedor**"), pelo que não
há afirmação falsa publicada. O que falta é cifra com **chave nossa**.

Em [`storage.go`](../backend/internal/storage/storage.go) o `PutObject` vai sem qualquer opção de
cifra: escrevemos bytes em claro e o fornecedor cifra-os com a chave dele.

Três consequências, por ordem de gravidade para o posicionamento:

1. **A Cloudflare detém a chave.** É a única empresa americana da pilha que guarda *conteúdo* de
   clientes — as outras duas tratam metadados de trânsito e de pagamento. É a linha mais pesada da
   tabela de `/sovereignty`, e a primeira que um encarregado de proteção de dados aponta.
2. **Uma credencial R2 fugida expõe tudo.** Hoje dá acesso ao conteúdo; depois daria a cifrado.
3. **Nós próprios lemos os ficheiros.** Legítimo, e igual ao Google e à Microsoft — mas não é o que
   dizemos vender.

**Porque não é E2EE.** Cifra ponta a ponta partiria o que o produto promete: link público sem conta,
anexo grande por e-mail para fora da organização, edição de Office no navegador, miniaturas,
`Range` no vídeo e pesquisa. A Proton consegue fazê-lo porque não tem nada disso. Fica como opção
futura, opt-in por pasta, com as limitações declaradas.

## 2. O desenho

**Cifra de envelope, dentro do serviço de storage.** O R2 passa a guardar só cifrado; as chaves
ficam na nossa infraestrutura, na UE. As partilhas continuam a funcionar porque o servidor decifra
ao servir.

### 2a. Onde entra o código

Todo o tráfego de ficheiros já passa pelo nosso backend — **não há URLs pré-assinadas em lado
nenhum** (verificado: zero ocorrências de `Presign` no repositório). Isso é o que torna isto
possível sem mexer no protocolo do cliente nem na app de computador.

São **33 pontos de chamada** (15 `Get`, 8 `Put`, 5 `Exists`, 3 `Remove`, 2 `GetSeek`), espalhados por
ficheiros, versões, chat, wiki, quadros, apresentações, gravações e OnlyOffice. **Nenhum deles muda:**
a cifra entra dentro do `storage.Service`, que é o único que fala com o balde.

### 2b. Hierarquia de chaves

```
KEK (ambiente, versionada)  →  chave do tenant  →  DEK (uma por blob)  →  objeto
```

- **DEK por blob**, aleatória de 256 bits. O dedup mantém-se: a chave do objeto é
  `tenant/sha256(conteúdo)` e a DEK é *por objeto*, logo o mesmo conteúdo no mesmo tenant continua a
  ser um só blob. (Cifra convergente — chave derivada do conteúdo — permitiria dedup entre tenants,
  mas revela se um ficheiro já existe na plataforma. Não compensa.)
- **A DEK viaja DENTRO do objeto**, embrulhada, no cabeçalho — não numa tabela. Ver §2d: é o que
  torna cada escrita atómica. Quem guarda o objeto fica com a chave embrulhada e não a consegue
  abrir: precisaria da chave do tenant (na nossa base de dados) e da KEK (no nosso ambiente).
  É o desenho da cifra do lado do cliente da AWS e do Tink, pela mesma razão.
- Só a **chave do tenant** vive em tabela (`tenant_keys`), embrulhada pela KEK.
- **Chave por tenant** pela mesma razão que toda a consulta filtra `tenant_id`: apagar a chave de um
  tenant torna os dados dele ilegíveis, o que é uma garantia de eliminação muito mais forte do que
  "apagámos as linhas" — e é escrutinável no acordo de subcontratação.
- **KEK versionada** no ambiente, como o `MFA_ENC_KEY` já faz para o TOTP. Rodar a KEK re-embrulha
  chaves (rápido, tabela pequena) e **não reescreve um único objeto** — é a razão de existir do
  envelope.

### 2c. Formato do objeto

Blocos de tamanho fixo, para o `Range` continuar a funcionar. Sem isto, perde-se o avanço no vídeo
e a leitura por blocos do sync.

```
cabeçalho (32) | DEK embrulhada | bloco 0 | bloco 1 | ... | bloco n
```

- **Cabeçalho:** magia `BSK1`, versão, opções, tamanho de bloco, tamanho em claro, prefixo de nonce,
  versão da KEK, comprimento da DEK embrulhada. Fixo em 32 bytes: 32 bytes chegam para saber onde
  começa o bloco 0 e se vale a pena ler mais.
- **Bloco:** AES-256-GCM, nonce = prefixo ‖ índice do bloco, dados associados = índice + marca de
  último bloco (impede reordenar e truncar).
- **Tamanho de bloco: 1 MiB.** Sobrecarga de 16 bytes por bloco (0,0015%) e granularidade suficiente
  para o seek. Um pedido de bytes traduz-se em blocos; decifram-se só esses.

### 2d. Uma escrita, e porquê

O plano original guardava a DEK numa tabela e escrevia o objeto a seguir, escolhendo a ordem cuja
janela de falha era benigna. **Isso foi abandonado**: com duas escritas, *toda* a ordem tem uma
janela — o objeto primeiro deixa cifrado sem chave (perda de dados), a linha primeiro deixa linhas
órfãs. "Benigna" é uma promessa que só se cumpre enquanto ninguém mexer no código.

A DEK passou a viajar **dentro do próprio objeto**, embrulhada pela chave do tenant. Fica **uma
escrita só**, e o `PUT` do armazenamento ou acontece por inteiro ou não acontece. Não há ordem para
acertar porque não há duas coisas para ordenar.

Ganhos que vieram atrás: menos uma tabela; o `Get` deixa de fazer uma consulta por cada leitura de
ficheiro; e o `Remove` volta a ser uma linha, porque a chave morre com o objeto.

### 2e. Migração dos objetos que já existem

A deteção é pela **magia no cabeçalho** — pelo próprio objeto, não por uma coluna que possa
dessincronizar. Isso torna a migração idempotente e imune a reinícios: uma marca na base de dados
que falhasse a escrever levaria a cifrar o cifrado.

Um trabalho no worker ([`sealing.go`](../backend/cmd/worker/sealing.go)) percorre o balde e sela em
fundo, com pausa entre objetos — isto corre no mesmo servidor que serve o correio dos clientes, e
migrar depressa não vale interromper quem está a trabalhar. Uma passagem que não termine não é
problema: a seguinte salta o que já está feito.

## 2f. O balde não é só nosso

Descoberto à primeira passagem em produção, da pior maneira: a selagem cifrou **43 mensagens de
correio** e o Stalwart deixou de as conseguir ler. Ele guarda-as em `mail/…`, no mesmo balde,
escritas e lidas por ele, sem passar por este código.

A causa não foi um caso não previsto — foi a **pergunta errada**. A função perguntava "isto parece
seguro de cifrar?" quando tinha de perguntar "**isto foi escrito por nós?**". Passou a lista branca:
`<tenant>/…` e `recordings/…`, as únicas famílias de chaves que o `storage.Service` escreve. Uma
lista negra teria de adivinhar todos os vizinhos futuros, e um balde partilhado só ganha vizinhos.

A reversão está em [`cmd/unseal`](../backend/cmd/unseal/main.go), e fica no repositório: se voltar a
acontecer, quer-se a reversão pronta e não um programa escrito à pressa durante a avaria.

**Duas consequências que ficam por resolver**, ambas registadas nas páginas públicas:

1. **O conteúdo do correio não é cifrado por nós.** Está fora do alcance deste desenho enquanto o
   Stalwart escrever direto no balde — e nós não modificamos o Stalwart (CLAUDE.md §5.4). A página
   de segurança diz isto por palavras nossas em vez de deixar descobrir.
2. **A cópia de segurança da base `stalwart` pode não conter as mensagens.** Se o corpo das
   mensagens vive no R2 e não no Postgres, o `pg_dump` leva a configuração e os metadados e deixa o
   conteúdo para trás — e o R2 não tem segunda cópia nossa. Falta confirmar contra a configuração do
   Stalwart em produção, a que não se chega deste lado.

## 3. As duas vias que não passam pelo backend

Foi o achado mais relevante do levantamento, e é por isso que entram nesta fase.

### 3a. Gravações de reuniões

O **LiveKit Egress escreve direto no balde** com as credenciais S3
([`meet_recording_tracks.go`](../backend/internal/httpapi/meet_recording_tracks.go)). Qualquer cifra
feita no backend não as apanharia.

- As gravações **por faixa** já são baixadas pelo worker, compostas com ffmpeg e **reenviadas** pelo
  `storage.Put` ([`assembly.go`](../backend/cmd/worker/assembly.go)) — essas ficam cifradas de
  borla, e as faixas cruas são apagadas no fim.
- As gravações **compostas** (RoomComposite) são escritas pelo egress e ficam assim. São seladas
  pela **mesma passagem** que migra o conteúdo antigo (§2e) — é o mesmo problema (um objeto que não
  passou pela nossa cifra) e não merecia dois mecanismos. A selagem reescreve a mesma chave: uma
  escrita, atómica; quem estiver a ler nesse instante recebe a versão antiga ou a nova, e ambas se
  leem.
- As chaves das gravações **novas** levam **prefixo de tenant** (`<tenant>/recordings/…`). Sem ele,
  a gravação seria embrulhada pela KEK e ficaria de fora da eliminação por tenant — apagar a chave
  de um tenant tem de tornar ilegível tudo o que é dele, gravações incluídas. As antigas, sem
  prefixo, são seladas com a KEK.
- **Fica uma janela de texto claro** entre o fim do egress e a selagem. É honesto dizê-lo.
  Eliminá-la exigia que o egress escrevesse para um destino nosso, o que o LiveKit não faz sem um
  servidor S3 intermédio.

### 3b. Cópias de segurança

O [`backup.sh`](../deploy/backup/backup.sh) envia os dumps por `rclone` **sem cifrar**, e o próprio
[README](../deploy/backup/README.md) já assinala a lacuna: *"quem tiver a chave do R2 lê os dumps"*.
Isto inclui a base de dados do correio — mensagens, anexos e configuração das caixas.

- Cifrar com **`age`**, com chave **assimétrica**: o servidor leva só a chave pública. Um servidor
  comprometido pode escrever cópias novas, mas **não consegue ler as antigas** — o que a cifra
  simétrica não daria.
- A chave privada fica fora da infraestrutura. Sem ela não há restauro: **o procedimento tem de ser
  testado antes de se considerar isto feito**, porque a página de segurança afirma que o restauro é
  testado.
- Feito com o [`test-restore.sh`](../deploy/backup/test-restore.sh): levanta um Postgres
  descartável, corre o `backup.sh` e o `restore.sh` **verdadeiros** e compara o conteúdo. Verifica
  também o que interessa mais do que o restauro correr — que o ficheiro enviado **não se lê**: tem
  cabeçalho `age`, o `pg_restore` não o abre, não tem texto do conteúdo lá dentro, e uma chave
  privada diferente é rejeitada.

## 4. Fases

| # | O quê | Verificação | Estado |
|---|---|---|---|
| 1 | Formato cifrado + `Put`/`Get`/`Remove` no `storage.Service`, com deteção por magia | testes de ida e volta, e leitura de objeto legado | ✅ 10 testes |
| 2 | `GetSeek` por blocos | vídeo com avanço/retrocesso; `Range` no meio de um bloco | ✅ |
| 3 | Chave por tenant, KEK versionada | rodar a KEK sem tocar em objetos | ✅ |
| 4 | Selagem em fundo (objetos antigos **e** gravações compostas) | contagem: objetos selados = objetos no balde | ✅ código; falta correr em produção |
| 5 | Prefixo de tenant nas gravações novas | gravação nova cai na chave do tenant | ✅ |
| 6 | `age` nas cópias de segurança | **restauro completo a partir de uma cópia cifrada** | ✅ ensaio automático |
| 7 | Gerar chaves e verificar em produção | objeto novo no balde ilegível sem a nossa chave | ⏳ |
| 8 | Atualizar `/security`, `/sovereignty` e a comparação com a Proton | — | ⏳ |

## 5. O que muda na mensagem

A linha do R2 passa de *"ficheiros, jurisdição UE, fornecedor americano"* para *"o fornecedor guarda
apenas cifrado; a chave é nossa e está na UE"*. É a forma mais barata de fortalecer a linha mais
fraca de `/sovereignty` — mais barata do que trocar de fornecedor, que é a outra saída registada no
plano da landing (§2).

**O que continua a não se poder afirmar:** que nós não conseguimos ler os ficheiros. Conseguimos —
temos as chaves, porque o produto precisa delas para gerar miniaturas, editar Office no navegador e
servir links públicos. Dizer o contrário seria o mesmo erro que a página de soberania existe para
não cometer.

## 6. Custo

- **Rede:** nenhum custo novo. O tráfego já passa todo pelo backend.
- **CPU:** AES-GCM com aceleração de hardware é praticamente gratuito face ao I/O.
- **Armazenamento:** +0,0015% (16 bytes por MiB).
- **Latência:** nenhuma consulta nova por leitura — a chave do objeto vem dentro do objeto (§2d) e a
  do tenant fica em cache em memória, carregada uma vez.
