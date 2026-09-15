package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/beskosorg/beskos/backend/internal/db"
)

// Ausências (24/07/2026): day off (um dia) e férias (período) são eventos de DIA INTEIRO com
// kind próprio. Por serem dia inteiro, o booking bloqueia-os automaticamente (BookingBusy inclui
// all-day). Nas férias, o utilizador pode ligar a resposta automática da caixa — feita via JMAP
// VacationResponse com a credencial DELE (a mesma do brokering; nunca a de admin).

type timeOffReq struct {
	Kind         string `json:"kind"`      // dayoff | vacation
	Start        string `json:"start"`     // YYYY-MM-DD
	End          string `json:"end"`       // YYYY-MM-DD (vacation; dayoff ignora)
	StartMin     *int   `json:"start_min"` // dayoff parcial: minutos desde 00:00 (nil = dia inteiro)
	EndMin       *int   `json:"end_min"`
	AutoReply    bool   `json:"auto_reply"`
	ReplySubject string `json:"reply_subject"`
	ReplyBody    string `json:"reply_body"`
	// on_conflict: "" pergunta (devolve os conflitos), "cancel" cancela os eventos, "keep" mantém.
	OnConflict string `json:"on_conflict"`
}

// POST /api/timeoff — cria a ausência no calendário do próprio utilizador.
func (s *Server) handleCreateTimeOff(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	var req timeOffReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "timeoff.invalid_body")
		return
	}
	if req.Kind != "dayoff" && req.Kind != "vacation" {
		writeErr(w, http.StatusBadRequest, "timeoff.invalid_kind")
		return
	}
	start, err := time.ParseInLocation("2006-01-02", req.Start, time.Local)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "timeoff.invalid_date")
		return
	}
	end := start
	if req.Kind == "vacation" && req.End != "" {
		end, err = time.ParseInLocation("2006-01-02", req.End, time.Local)
		if err != nil || end.Before(start) {
			writeErr(w, http.StatusBadRequest, "timeoff.invalid_date")
			return
		}
	}
	// Folga parcial: intervalo de horas dentro do dia (meio dia, consulta, etc.).
	allDay := true
	evStart, evEnd := start, end.Add(24*time.Hour-time.Second)
	if req.Kind == "dayoff" && req.StartMin != nil && req.EndMin != nil {
		sm, em := *req.StartMin, *req.EndMin
		if sm < 0 || em > 1440 || sm >= em {
			writeErr(w, http.StatusBadRequest, "timeoff.invalid_date")
			return
		}
		allDay = false
		evStart = start.Add(time.Duration(sm) * time.Minute)
		evEnd = start.Add(time.Duration(em) * time.Minute)
	}

	// Conflitos com a agenda: eventos próprios e convites no intervalo. Sem decisão do utilizador,
	// devolve a lista e NÃO cria nada — a UI pergunta se quer cancelar.
	conflicts, _ := s.q.TimeOffConflicts(r.Context(), db.TimeOffConflictsParams{
		OwnerID: pgUUID(mu.ID), TenantID: mu.TenantID, InviteeID: mu.ID, FromTs: evStart, ToTs: evEnd,
	})
	if len(conflicts) > 0 && req.OnConflict == "" {
		writeJSON(w, http.StatusOK, map[string]any{"status": "conflicts", "conflicts": conflicts})
		return
	}
	if len(conflicts) > 0 && req.OnConflict == "cancel" {
		s.cancelForTimeOff(r, mu, conflicts)
	}

	// Título fallback em inglês (lei 4.6); a UI mostra a etiqueta traduzida pelo kind.
	title, color := "Day off", "#9AA0A6"
	if req.Kind == "vacation" {
		title, color = "Vacation", "#53C08B"
	}
	ev, err := s.q.CreateEvent(r.Context(), db.CreateEventParams{
		TenantID: mu.TenantID, OwnerUserID: pgUUID(mu.ID), Title: title, Description: "",
		Location: "", Color: color, StartsAt: evStart, EndsAt: evEnd,
		SourceEmailID: "", Tags: []string{}, MeetingRoom: "", RemindMinutes: 0, AllDay: allDay,
		Recur: "", RecurUntil: pgtype.Timestamptz{}, CalendarID: pgtype.UUID{},
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "timeoff.create_failed")
		return
	}
	_ = s.q.SetEventKind(r.Context(), db.SetEventKindParams{ID: ev.ID, Kind: req.Kind, TenantID: mu.TenantID})

	out := map[string]any{"status": "ok", "event_id": ev.ID}
	// Resposta automática das férias — best-effort: a ausência fica criada mesmo se o JMAP falhar,
	// e o cliente é avisado para tentar de novo nas definições.
	if req.Kind == "vacation" && req.AutoReply {
		if err := s.setVacationResponse(r, mu, start, end.Add(24*time.Hour-time.Second), req.ReplySubject, req.ReplyBody); err != nil {
			log.Printf("timeoff: vacation response for %s: %v", mu.Email, err)
			out["auto_reply"] = "failed"
		} else {
			out["auto_reply"] = "ok"
		}
	}
	writeJSON(w, http.StatusCreated, out)
}

// cancelForTimeOff resolve os conflitos quando o utilizador escolheu "cancelar": os eventos
// PRÓPRIOS são apagados (soft-delete) e todos os participantes avisados — convidados internos por
// notificação (+push) e externos por e-mail. Nos eventos de OUTROS, recusa o convite e avisa o dono.
func (s *Server) cancelForTimeOff(r *http.Request, mu db.MailUser, conflicts []db.TimeOffConflictsRow) {
	ctx := r.Context()
	actor := mu.Name
	if actor == "" {
		actor = mu.Email
	}
	for _, c := range conflicts {
		when := c.StartsAt.UTC().Format(time.RFC3339)
		if !c.Own {
			// Convite de outra pessoa: recusa e avisa o organizador.
			_ = s.q.SetInviteStatus(ctx, db.SetInviteStatusParams{EventID: c.ID, UserID: mu.ID, Status: "declined"})
			if ev, err := s.q.GetEventInTenant(ctx, db.GetEventInTenantParams{ID: c.ID, TenantID: mu.TenantID}); err == nil && ev.OwnerUserID.Valid {
				s.notifyUser(ctx, mu.TenantID, uuid.UUID(ev.OwnerUserID.Bytes), "event_rsvp",
					actor+" declined: "+c.Title, actor+" declined — "+when, eventLink(ev.ID, ev.StartsAt), uuid.Nil,
					map[string]string{"actor": actor, "title": c.Title, "status": "declined", "when": when})
			}
			continue
		}
		// Evento próprio: convidados primeiro (a lista some com o delete em cascata lógica).
		invites, _ := s.q.ListEventInvites(ctx, c.ID)
		externals, _ := s.q.ListEventExternals(ctx, c.ID)
		if err := s.q.DeleteEvent(ctx, db.DeleteEventParams{ID: c.ID, TenantID: mu.TenantID, OwnerUserID: pgUUID(mu.ID)}); err != nil {
			continue
		}
		for _, inv := range invites {
			s.notifyUser(ctx, mu.TenantID, inv.UserID, "event_cancelled",
				"Cancelled: "+c.Title, actor+" cancelled the event — "+when, "/mail/cal", uuid.Nil,
				map[string]string{"actor": actor, "title": c.Title, "when": when})
		}
		if s.email != nil {
			body := fmt.Sprintf("%s cancelou o evento: %s\r\nEstava marcado para: %s\r\n", actor, c.Title, c.StartsAt.Format("02/01/2006 15:04"))
			for _, ext := range externals {
				go func(to, subject, body string) {
					if err := s.email.Send(to, subject, body); err != nil {
						log.Printf("timeoff cancel mail to %s: %v", to, err)
					}
				}(ext, "Evento cancelado: "+c.Title, body)
			}
		}
	}
}

// setVacationResponse liga a resposta automática no Stalwart via JMAP, autenticado com a credencial
// da PRÓPRIA caixa (brokering H11 — nunca credencial de admin). from/until limitam o período: o
// Stalwart ativa e desativa sozinho nas datas.
func (s *Server) setVacationResponse(r *http.Request, mu db.MailUser, from, until time.Time, subject, body string) error {
	cred, ok := s.decryptCred(mu.JmapCred)
	if !ok {
		return fmt.Errorf("no mailbox credential")
	}
	ctx := r.Context()
	// 1) Sessão: descobre o accountId primário de mail.
	sreq, err := http.NewRequestWithContext(ctx, http.MethodGet, s.sw.BaseURL()+"/jmap/session", nil)
	if err != nil {
		return err
	}
	sreq.Header.Set("Authorization", "Basic "+cred)
	resp, err := http.DefaultClient.Do(sreq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("jmap session: HTTP %d", resp.StatusCode)
	}
	var sess struct {
		PrimaryAccounts map[string]string `json:"primaryAccounts"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&sess); err != nil {
		return err
	}
	accountID := sess.PrimaryAccounts["urn:ietf:params:jmap:mail"]
	if accountID == "" {
		return fmt.Errorf("jmap session: no mail account")
	}
	// 2) VacationResponse/set no singleton.
	if subject == "" {
		subject = "Out of office"
	}
	payload := map[string]any{
		"using": []string{"urn:ietf:params:jmap:core", "urn:ietf:params:jmap:vacationresponse"},
		"methodCalls": []any{[]any{"VacationResponse/set", map[string]any{
			"accountId": accountID,
			"update": map[string]any{"singleton": map[string]any{
				"isEnabled": true,
				"fromDate":  from.UTC().Format(time.RFC3339),
				"toDate":    until.UTC().Format(time.RFC3339),
				"subject":   subject,
				"textBody":  body,
			}},
		}, "0"}},
	}
	buf, _ := json.Marshal(payload)
	vreq, err := http.NewRequestWithContext(ctx, http.MethodPost, s.sw.BaseURL()+"/jmap/", bytes.NewReader(buf))
	if err != nil {
		return err
	}
	vreq.Header.Set("Authorization", "Basic "+cred)
	vreq.Header.Set("Content-Type", "application/json")
	vresp, err := http.DefaultClient.Do(vreq)
	if err != nil {
		return err
	}
	defer vresp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(vresp.Body, 64<<10))
	if vresp.StatusCode != http.StatusOK {
		return fmt.Errorf("jmap set: HTTP %d: %s", vresp.StatusCode, raw)
	}
	// Falha do método vem no corpo (notUpdated/error) com HTTP 200 — verifica.
	if bytes.Contains(raw, []byte("notUpdated")) || bytes.Contains(raw, []byte(`"error"`)) {
		return fmt.Errorf("jmap set rejected: %s", raw)
	}
	return nil
}
