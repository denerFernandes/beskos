# Roadmap — Meet (salas, controles, notas, transcrição)

Estado e próximos passos da videochamada. Decisões de custo em [§Transcrição](#v2--transcrição-e-notas-por-ia).

## v1 — agora (em implementação)

| Item | Descrição |
|---|---|
| **Sala em janela própria** | A sala abre sempre numa janela separada (`/room/:room`), só com a chamada — sem rail de apps nem topbar. O webmail continua usável atrás. |
| **Acesso da sala (dentro da chamada)** | O organizador troca entre **Aberta** (qualquer um entra) e **Aprovação** (lobby) sem sair da sala. |
| **Controles do organizador** | Ligar/desligar por participante: **microfone**, **câmera**, **compartilhar tela**, **mensagens/reações**. Aplicado de verdade (grants do token + `UpdateParticipant` no LiveKit), não só escondendo botão. |
| **Host = dono da sala** | Salas ad-hoc (ex.: chamada do chat) passam a ter registro: quem inicia vira organizador. |

## v1.5 — ✅ FEITO (2026-07-16)

| Item | Estado |
|---|---|
| Notas da reunião | ✅ painel colaborativo na sala |
| Notas → tasks | ✅ seleção vira card (com IA) |
| Notas → e-mail | ✅ botão do host; envia aos participantes e **conta** (`{sent, failed}`) — um "enviado!" que esconde metade dos envios perdidos é pior que o silêncio |
| Organizador entra primeiro | ✅ `meetings.lock_until_host` (migração 00025). A recusa é ANTES de assinar o token: o convidado recebe `locked:true` e token nenhum. A sala de espera repete sozinha a cada 5s |
| Expulsar participante | ✅ `RemoveParticipant` do LiveKit, só o dono; o host não se expulsa a si mesmo (com a tranca ligada, deixaria toda a gente à porta) |

**Decisão registada:** se o LiveKit não responder à checagem de presença do host, **não trancamos** ninguém
de fora — a sala existir importa mais que a tranca, e o erro é nosso, não do convidado.

**Pré-requisito que faltava e foi resolvido:** o envio de e-mail nunca teria saído — `NOTIFY_SMTP_HOST`
não estava configurado e o `notify.Email` vira `nil` em silêncio. Além disso a **587 do Stalwart não
serve** (o listener fecha a ligação); só a **465 com TLS implícito** funciona, e o sender fazia
`Dial`+`STARTTLS`. Corrigido e medido: e-mails chegam à caixa real.

## v1.5 — original (referência)

| Item | Descrição | Depende de |
|---|---|---|
| **Notas da reunião** | Painel de notas colaborativo dentro da sala (texto livre, ao vivo entre participantes). | — |
| **Notas → tasks** | Selecionar trechos das notas → vira card no Kanban (reusa `POST /boards/{id}/cards` + a IA de `/ai/task`). | Notas |
| **Notas → e-mail** | Enviar o resumo da reunião para os participantes ao encerrar. | Notas |
| **"Organizador entra primeiro"** | Trava a sala até o host chegar (o Meet tem). | — |
| **Expulsar participante** | `RemoveParticipant` do LiveKit, host-only. | — |

## v2 — Transcrição e notas por IA

**Decisão de custo (jul/2026):** transcrever é barato — o gargalo é privacidade, não preço.

| Opção | Custo por hora de reunião | Observação |
|---|---|---|
| **Groq** (whisper-large-v3-turbo) | **US$ 0,04** | 216× realtime. Recomendado. |
| Groq, faixa por participante | US$ 0,20 (5 pessoas) | Atribuição de falante perfeita, sem diarization. |
| AssemblyAI / Deepgram / OpenAI | US$ 0,15 – 0,36 | 4–9× mais caro pelo mesmo resultado. |
| Streaming (legendas ao vivo) | US$ 0,44 – 1,02 | 2–10× o batch + serviço novo (LiveKit Agent). **Não é necessário** para notas. |
| **Whisper self-hosted** | R$ 0 marginal | Dados não saem do VPS (LGPD). Consome CPU (1h ≈ 20–60 min) e pt-BR pior que `large`. |

**Notas por IA:** ~US$ 0,005 por reunião no DeepSeek (já configurado). Irrelevante.

**Conclusão:** ~**US$ 0,045 por hora de reunião** (Groq + DeepSeek). Um cliente com 100 h/mês custa
**US$ 4,50/mês**. A decisão real é **LGPD**: se áudio de cliente não puder sair do Brasil, troca-se o
motor por Whisper local mantendo o resto igual (a interface de STT é a mesma).

**Arquitetura planejada:** pós-reunião (batch), reusando a gravação que o Egress já sobe no MinIO →
extrai áudio → STT → transcript → DeepSeek gera notas/decisões/tasks. Sem infra de tempo real.

Escopo v2:
- Transcript da gravação (motor plugável: Groq | Whisper local).
- Notas automáticas: resumo, decisões, pendências.
- Tasks sugeridas → Kanban (com aprovação do usuário).
- Busca dentro de transcripts.

## v3 — depois

- Legendas ao vivo (LiveKit Agent + STT streaming).
- Tradução ao vivo.
- Salas de apoio (breakout rooms).
- Enquetes e Q&A.
- Fundo virtual por sala (hoje é por usuário).
