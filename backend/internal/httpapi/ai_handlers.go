package httpapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/beskosorg/beskos/backend/internal/ai"
	"github.com/beskosorg/beskos/backend/internal/assistant"
)

// Endpoints de IA sobre e-mail. O conteúdo do e-mail chega no corpo do request (o frontend
// o obtém via JMAP direto). A chave da IA fica só aqui no backend (lei §3.5).
// Idioma padrão pt-BR (mail_users ainda não guarda preferência de idioma).

// aiMaxBody limita o corpo de cada request de IA — o input vai para a API paga e é faturável,
// então sem teto um usuário dispararia custo ilimitado com corpos enormes (auditoria M — abuso de custo).
const aiMaxBody = 256 << 10 // 256 KiB

// localeFromRequest lê o idioma da PÁGINA (header X-Locale, ex.: "pt-BR", "en") para a IA gerar o
// conteúdo nesse idioma. Sanitiza (só letras e "-", 2–10 chars) — o valor entra num prompt, por isso
// nunca deve poder injetar instruções. Vazio/invalido → pt-BR.
func localeFromRequest(r *http.Request) string {
	v := strings.TrimSpace(r.Header.Get("X-Locale"))
	if v == "" || len(v) > 10 {
		return "pt-BR"
	}
	for _, c := range v {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '-') {
			return "pt-BR"
		}
	}
	return v
}

func (s *Server) assistant(w http.ResponseWriter, r *http.Request) (*ai.Assistant, bool) {
	if s.ai == nil {
		writeErr(w, http.StatusServiceUnavailable, "ai.disabled")
		return nil, false
	}
	// Rate limit por usuário (custo/abuso) + teto de corpo. O limiter conta por mail_user.
	mu := mailUserFrom(r.Context())
	if !s.aiLimiter.allow("ai:"+mu.ID.String(), time.Now()) {
		writeErr(w, http.StatusTooManyRequests, "ai.rate_limited")
		return nil, false
	}
	// Quota de tokens da janela de 6h (a MESMA do chat): estes endpoints de uma tacada gastam
	// tokens pagos e têm de contar contra o limite — senão seriam uma porta para o contornar
	// (auditoria 24/07/2026). Gate na entrada + débito por chamada via SetUsageHook.
	id := assistant.Identity{TenantID: mu.TenantID, UserID: mu.ID, Email: mu.Email}
	if s.assistantQuotaStatus(r.Context(), id, time.Now()).Exceeded() {
		writeErr(w, http.StatusTooManyRequests, "assistant.quota_exceeded")
		return nil, false
	}
	r.Body = http.MaxBytesReader(w, r.Body, aiMaxBody)
	// Idioma do CONTEÚDO gerado (briefing, resumos, tarefas…) = idioma da PÁGINA, enviado pelo
	// frontend em X-Locale. Assim o resumo segue a UI do utilizador, não um idioma fixo. (O chat é
	// à parte: espelha o idioma que a pessoa escreve — ver assistant/engine.go.)
	a := ai.NewAssistant(s.ai, localeFromRequest(r))
	ctx := r.Context()
	a.SetUsageHook(func(tokens int) { s.recordAssistantUsage(ctx, id, tokens) })
	return a, true
}

type aiEmailReq struct {
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

// handleAIPlanTask expande uma ideia curta num título + descrição de tarefa (Kanban).
func (s *Server) handleAIPlanTask(w http.ResponseWriter, r *http.Request) {
	a, ok := s.assistant(w, r)
	if !ok {
		return
	}
	var req struct {
		Idea string `json:"idea"`
	}
	if !decode(w, r, &req) {
		return
	}
	draft, err := a.PlanTask(r.Context(), req.Idea)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "ai.failed")
		return
	}
	writeJSON(w, http.StatusOK, draft)
}

func (s *Server) handleAISummarize(w http.ResponseWriter, r *http.Request) {
	a, ok := s.assistant(w, r)
	if !ok {
		return
	}
	var req aiEmailReq
	if !decode(w, r, &req) {
		return
	}
	out, err := a.SummarizeEmail(r.Context(), req.Subject, req.Body)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "ai.failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"summary": out})
}

type aiDraftReq struct {
	Original    string `json:"original"`
	Instruction string `json:"instruction"`
	Tone        string `json:"tone"` // neutro | formal | curto e direto
}

func (s *Server) handleAIDraft(w http.ResponseWriter, r *http.Request) {
	a, ok := s.assistant(w, r)
	if !ok {
		return
	}
	var req aiDraftReq
	if !decode(w, r, &req) {
		return
	}
	out, err := a.DraftReply(r.Context(), req.Original, req.Instruction, ai.Tone(req.Tone))
	if err != nil {
		writeErr(w, http.StatusBadGateway, "ai.failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"draft": out})
}

// handleAIWikiWrite gera conteúdo de página da wiki (Markdown) para o editor BlockNote: continuar a
// escrever, melhorar, resumir, ou uma instrução livre. Sessão + rate limit como as demais rotas de IA.
func (s *Server) handleAIWikiWrite(w http.ResponseWriter, r *http.Request) {
	a, ok := s.assistant(w, r)
	if !ok {
		return
	}
	var req struct {
		Instruction string `json:"instruction"`
		Context     string `json:"context"`
	}
	if !decode(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Instruction) == "" {
		writeErr(w, http.StatusBadRequest, "ai.instruction_required")
		return
	}
	out, err := a.WikiWrite(r.Context(), req.Instruction, req.Context)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "ai.failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"content": out})
}

func (s *Server) handleAIExtractTasks(w http.ResponseWriter, r *http.Request) {
	a, ok := s.assistant(w, r)
	if !ok {
		return
	}
	var req aiEmailReq
	if !decode(w, r, &req) {
		return
	}
	// The roster goes with the request so the model can answer with a real e-mail instead of echoing
	// whatever name appeared in the text. THIS tenant only: suggesting someone from another
	// organisation would reveal that they exist.
	mu := mailUserFrom(r.Context())
	people := []ai.Person{}
	if rows, err := s.q.ListColleagues(r.Context(), mu.TenantID); err == nil {
		for _, c := range rows {
			people = append(people, ai.Person{Name: c.Name, Email: c.Email})
		}
	}
	tasks, err := a.ExtractTasks(r.Context(), req.Subject, req.Body, people)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "ai.failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": tasks})
}

type aiBriefingReq struct {
	Events   []string `json:"events"`
	Tasks    []string `json:"tasks"`
	Emails   []string `json:"emails"`
	Date     string   `json:"date"`
	Period   string   `json:"period"`
	Tomorrow []string `json:"tomorrow"`
}

func (s *Server) handleAIBriefing(w http.ResponseWriter, r *http.Request) {
	a, ok := s.assistant(w, r)
	if !ok {
		return
	}
	var req aiBriefingReq
	if !decode(w, r, &req) {
		return
	}
	mu := mailUserFrom(r.Context())
	out, err := a.DailyBriefing(r.Context(), ai.BriefingInput{
		UserName: mu.Name, Date: req.Date, Period: req.Period,
		Events: req.Events, Tasks: req.Tasks, Emails: req.Emails, Tomorrow: req.Tomorrow,
	})
	if err != nil {
		writeErr(w, http.StatusBadGateway, "ai.failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"briefing": out})
}
