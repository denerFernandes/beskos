package httpapi

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"mailin/backend/internal/assistant"
	"mailin/backend/internal/db"
)

// assistantMaxBody trava o tamanho do pedido. A mensagem do utilizador é uma frase, não um ficheiro.
const assistantMaxBody = 64 << 10

// assistantHistoryTurns é quanto do fio volta ao modelo. Um fio inteiro reenviado a cada turno é a
// forma mais rápida de gastar uma fatura (§9): a partir de certo ponto resume-se em vez de reenviar
// (§9.3), e esse trabalho é de outra fase. Até lá, uma janela fixa é o comportamento honesto.
const assistantHistoryTurns = 20

type assistantChatReq struct {
	Message string `json:"message"`
	// Agent, quando preenchido, FIXA o agente para o resto do fio — é a escolha manual do
	// utilizador (§3.2). Vazio deixa o encaminhamento decidir a cada turno.
	Agent string `json:"agent"`
	// Conversation vazio abre uma conversa nova.
	Conversation string `json:"conversation_id"`
	// Background manda o pedido para o worker em vez de o correr aqui (§3.6). É uma escolha
	// EXPLÍCITA de quem pede: adivinhar o que é "longo" seria adivinhar, e uma tarefa em fundo
	// muda o que a pessoa vê a seguir.
	Background bool `json:"background"`
}

// handleAssistantChat corre um turno completo e transmite-o por SSE: pedaços de texto, cartões de
// ação e o resultado final (§7.3).
//
// Os erros dividem-se em dois momentos, e é isso que explica o formato da resposta: antes de o fio
// abrir são erros HTTP normais, com error_code para o frontend traduzir (lei 4.4); depois de aberto
// já não há como mudar o código de estado, portanto viajam como um evento `error` com a mesma chave.
func (s *Server) handleAssistantChat(w http.ResponseWriter, r *http.Request) {
	if s.ai == nil || s.agent == nil {
		writeErr(w, http.StatusServiceUnavailable, "ai.disabled")
		return
	}
	mu := mailUserFrom(r.Context())
	// Interruptor-mestre: o administrador desligou o assistente para a organização (desligou o agente
	// geral). Recusa-se à entrada, antes de gastar quota ou abrir o fio.
	if s.assistantDisabled(r.Context(), mu.TenantID) {
		writeErr(w, http.StatusServiceUnavailable, "assistant.disabled")
		return
	}
	// Limitador próprio: uma execução agêntica gasta vários pedidos ao modelo, portanto não pode
	// partilhar o balde das chamadas de IA de um só passo.
	if !s.agentLimiter.allow("assistant:"+mu.ID.String(), time.Now()) {
		writeErr(w, http.StatusTooManyRequests, "assistant.rate_limited")
		return
	}
	// Quota comercial da janela de 6h (§9.2). Distinta do teto de passos (§9.1), que continua a
	// aplicar-se dentro de cada execução. Esgotada, o pedido é recusado com o quando-renova à vista.
	qid := assistant.Identity{TenantID: mu.TenantID, UserID: mu.ID}
	if s.assistantQuotaStatus(r.Context(), qid, time.Now()).Exceeded() {
		writeErr(w, http.StatusTooManyRequests, "assistant.quota_exceeded")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, assistantMaxBody)

	var req assistantChatReq
	if !decode(w, r, &req) {
		return
	}
	msg := strings.TrimSpace(req.Message)
	if msg == "" {
		writeErr(w, http.StatusBadRequest, "assistant.message_required")
		return
	}
	id := assistant.Identity{TenantID: mu.TenantID, UserID: mu.ID, Email: mu.Email, Language: mu.Language}

	turn, code := s.assistantTurn(r.Context(), id, req, msg)
	if code != "" {
		writeErr(w, http.StatusBadRequest, code)
		return
	}
	// Apps desligadas pela organização: a IA perde as ferramentas dessas apps e o agente que delas
	// depende (pedido do cliente). Vale para o fio E para as tarefas em fundo. Um agente sem app cai
	// no geral, já com as suas ferramentas filtradas.
	if disabled := s.disabledAppsEffective(r.Context(), mu.TenantID); len(disabled) > 0 {
		if !assistant.AgentAvailable(turn.agent.Key, disabled) {
			if g, ok := assistant.Builtin(assistant.AgentGeneral); ok {
				turn.agent = g
			}
		}
		turn.agent.Tools = assistant.AllowedTools(turn.agent.Tools, disabled)
	}
	if req.Background {
		// A conversa devolve logo um cartão de tarefa; o trabalho continua no servidor. Sem SSE:
		// não há nada a transmitir, o progresso vê-se no separador de Tarefas.
		s.startAssistantTask(w, r, turn, msg)
		return
	}
	conv, ag := turn.conv, turn.agent

	sse, ok := newSSE(w)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "assistant.failed")
		return
	}
	// A conversa vai primeiro: sem isto, uma conversa nova só teria id no fim, e um utilizador que
	// feche o painel a meio perderia o fio que já está gravado.
	// O agente que vai responder vai no primeiro evento: a escolha do encaminhamento é SEMPRE
	// visível (§3.2). Encaminhamento silencioso deixaria a pessoa sem perceber porque é que o
	// assistente às vezes consegue uma coisa e às vezes não.
	sse.send(map[string]any{
		"kind": "conversation", "conversation_id": conv.ID, "title": conv.Title,
		"agent": ag.Key, "pinned_agent": conv.PinnedAgent,
	})

	run, err := s.agent.RunStream(r.Context(), id, ag,
		assistant.RunOptions{
			Message: msg, History: turn.history, ConversationID: conv.ID,
			Memory: s.assistantMemoryInject(r.Context(), id), // factos duradouros, como CONTEXTO (§8.3)
		},
		func(ev assistant.Event) { sse.send(ev) })
	if err != nil {
		log.Printf("assistant: run failed for user %s: %v", mu.ID, err)
		sse.send(map[string]any{"kind": "error", "error_code": "assistant.failed"})
		return
	}

	// A resposta fica gravada mesmo que o utilizador tenha fechado o separador a meio: o trabalho
	// JÁ foi feito (e pago), e reabrir a conversa tem de mostrar o que aconteceu.
	s.saveAssistantAnswer(r.Context(), id, conv.ID, run)
	s.recordAssistantUsage(r.Context(), id, run.Usage.TotalTokens) // quota da janela de 6h (§9.2)
	sse.send(assistant.Event{Kind: assistant.EventDone, Run: &run})

	// Extração de memória DEPOIS do turno, só sobre a mensagem do utilizador (§8.2). Não é
	// silenciosa: o que guardou e o que recusou vai num evento, para o painel o mostrar (§6.5).
	facts, refusals := s.assistantExtractMemory(context.WithoutCancel(r.Context()), id, msg, turn.userMsgID)
	if len(facts) > 0 || len(refusals) > 0 {
		sse.send(map[string]any{"kind": "memory", "saved": facts, "refused": refusals})
	}
}

// assistantDisabled diz se a organização desligou o assistente por completo. O agente geral É o
// assistente: desligá-lo desliga a funcionalidade para toda a gente na organização. Um erro
// transitório a ler a base não desliga nada — degrada-se para "ligado".
func (s *Server) assistantDisabled(ctx context.Context, tenant uuid.UUID) bool {
	rows, err := s.q.ListDisabledBuiltinAgents(ctx, tenant)
	if err != nil {
		return false
	}
	for _, k := range rows {
		if k == assistant.AgentGeneral {
			return true
		}
	}
	return false
}

// assistantTurnState é o que um turno precisa de saber antes de o ciclo arrancar.
type assistantTurnState struct {
	conv      db.AssistantConversation
	history   []assistant.Turn
	agent     assistant.Agent
	userMsgID pgtype.UUID // a mensagem do utilizador deste turno — origem de um facto de memória (§6.5)
}

// assistantTurn resolve a conversa, ESCOLHE O AGENTE e grava a mensagem do utilizador.
//
// A ordem aqui é uma regra de segurança, não arrumação: o agente é decidido ANTES de qualquer
// ferramenta correr, uma vez por turno, e só a partir da mensagem do utilizador (§3.2). Trocar de
// agente é trocar de privilégios; se a decisão pudesse acontecer depois de o ciclo ler um e-mail,
// esse e-mail poderia desencadeá-la.
func (s *Server) assistantTurn(ctx context.Context, id assistant.Identity, req assistantChatReq, msg string) (assistantTurnState, string) {
	var st assistantTurnState

	// 1. A conversa e o seu fio.
	if req.Conversation != "" {
		parsed, err := uuid.Parse(req.Conversation)
		if err != nil {
			return st, "assistant.unknown_conversation"
		}
		st.conv, err = s.q.GetAssistantConversation(ctx, db.GetAssistantConversationParams{
			ID: parsed, TenantID: id.TenantID, MailUserID: id.UserID,
		})
		if err != nil {
			// Não se distingue "não existe" de "não é tua": a diferença diria a quem adivinhasse um
			// id que ele existe noutra conta.
			return st, "assistant.unknown_conversation"
		}
		msgs, err := s.q.ListAssistantMessages(ctx, db.ListAssistantMessagesParams{
			ConversationID: st.conv.ID, TenantID: id.TenantID, MailUserID: id.UserID, Lim: assistantHistoryTurns,
		})
		if err != nil {
			log.Printf("assistant: load history: %v", err)
			return st, "assistant.failed"
		}
		for _, m := range msgs {
			st.history = append(st.history, assistant.Turn{Role: m.Role, Content: m.Content})
		}
	}

	// 2. O agente.
	key, code := s.pickAssistantAgent(ctx, id.TenantID, req, st.conv, msg)
	if code != "" {
		return st, code
	}
	ag, ok := s.assistantAgentByKey(ctx, id.TenantID, key)
	if !ok {
		return st, "assistant.unknown_agent"
	}
	st.agent = ag

	// 3. A conversa nova, já com o agente escolhido; ou a fixação, quando o utilizador escolheu.
	if st.conv.ID == (uuid.UUID{}) {
		created, err := s.q.CreateAssistantConversation(ctx, db.CreateAssistantConversationParams{
			TenantID: id.TenantID, MailUserID: id.UserID, Agent: ag.Key, Title: conversationTitle(msg),
		})
		if err != nil {
			log.Printf("assistant: create conversation: %v", err)
			return st, "assistant.failed"
		}
		st.conv = created
	}
	if req.Agent != "" && st.conv.PinnedAgent != req.Agent {
		if err := s.q.PinAssistantConversationAgent(ctx, db.PinAssistantConversationAgentParams{
			ID: st.conv.ID, TenantID: id.TenantID, MailUserID: id.UserID, PinnedAgent: req.Agent,
		}); err != nil {
			log.Printf("assistant: pin agent: %v", err)
		} else {
			st.conv.PinnedAgent = req.Agent
		}
	}

	// 4. A mensagem do utilizador.
	userMsg, err := s.q.AppendAssistantMessage(ctx, db.AppendAssistantMessageParams{
		TenantID: id.TenantID, ConversationID: st.conv.ID, Role: "user", Content: msg,
		Steps: json.RawMessage("[]"),
	})
	if err != nil {
		log.Printf("assistant: append user message: %v", err)
		return st, "assistant.failed"
	}
	st.userMsgID = pgtype.UUID{Bytes: userMsg.ID, Valid: true}
	return st, ""
}

// pickAssistantAgent decide quem responde, por esta ordem:
//
//  1. a escolha MANUAL do utilizador neste pedido — fixa o agente para o resto do fio;
//  2. o agente já fixado na conversa;
//  3. o encaminhamento automático, a partir da mensagem do utilizador.
//
// A escolha manual vem primeiro de propósito: o utilizador manda sempre mais do que o modelo.
func (s *Server) pickAssistantAgent(ctx context.Context, tenant uuid.UUID, req assistantChatReq, conv db.AssistantConversation, msg string) (string, string) {
	available := s.assistantAgentsFor(ctx, tenant)
	has := func(key string) bool {
		for _, a := range available {
			if a.Key == key {
				return true
			}
		}
		return false
	}
	if req.Agent != "" {
		if !has(req.Agent) {
			return "", "assistant.unknown_agent"
		}
		return req.Agent, ""
	}
	if conv.PinnedAgent != "" && has(conv.PinnedAgent) {
		return conv.PinnedAgent, ""
	}
	// O agente fixado deixou de existir (apagado, ou desligado pelo administrador). Cai no
	// encaminhamento em vez de falhar o pedido: a conversa da pessoa não pode ficar refém de um
	// perfil que já não há.
	choices := make([]assistant.AgentChoice, 0, len(available))
	for _, a := range available {
		choices = append(choices, assistant.AgentChoice{Key: a.Key, Purpose: a.Purpose})
	}
	return s.agentRouter.Pick(ctx, msg, choices), ""
}

// saveAssistantAnswer persiste a resposta e sobe a conversa na lista. Falhar aqui não estraga o
// turno que o utilizador acabou de ver — mas fica no log, porque o fio ficou incompleto.
func (s *Server) saveAssistantAnswer(ctx context.Context, id assistant.Identity, conv uuid.UUID, run assistant.Run) {
	// Sem cancelamento: se o separador fechou, a escrita ainda tem de acontecer.
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()

	steps, err := json.Marshal(run.Steps)
	if err != nil {
		steps = json.RawMessage("[]")
	}
	if _, err := s.q.AppendAssistantMessage(saveCtx, db.AppendAssistantMessageParams{
		TenantID: id.TenantID, ConversationID: conv, Role: "assistant", Content: run.Answer,
		Steps: steps, Stopped: run.Stopped, RunID: pgtype.UUID{Bytes: run.ID, Valid: true},
		// Quem respondeu fica gravado: reabrir a conversa mostra a mesma escolha que se viu ao vivo.
		Agent: run.Agent,
	}); err != nil {
		log.Printf("assistant: append answer: %v", err)
	}
	if err := s.q.TouchAssistantConversation(saveCtx, db.TouchAssistantConversationParams{
		ID: conv, TenantID: id.TenantID, MailUserID: id.UserID,
	}); err != nil {
		log.Printf("assistant: touch conversation: %v", err)
	}
}

// conversationTitle deriva o título da primeira frase do utilizador. Não se pede um título ao
// modelo: custaria uma chamada por conversa para dizer menos do que a própria frase.
func conversationTitle(msg string) string {
	title := strings.TrimSpace(strings.ReplaceAll(msg, "\n", " "))
	const max = 80
	if len([]rune(title)) > max {
		title = strings.TrimSpace(string([]rune(title)[:max])) + "…"
	}
	return title
}
