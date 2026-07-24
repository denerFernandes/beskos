package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// NewEvent é um evento a criar na agenda do utilizador AUTENTICADO. Sem dono nem tenant nos campos:
// vêm da Identity, nunca dos args do modelo (§6.2).
type NewEvent struct {
	Title       string
	Description string
	Location    string
	StartsAt    time.Time
	EndsAt      time.Time
	// CreateMeet liga uma sala de vídeo (Meet) ao evento e devolve o seu caminho.
	CreateMeet bool
	// Invitees são endereços de e-mail de COLEGAS a convidar (RSVP). O handler resolve cada um para
	// um utilizador do MESMO tenant; endereços externos não são convidáveis na agenda (avisa-se por
	// e-mail). Convidar avisa outra pessoa: a ferramenta confirma quando esta lista não é vazia.
	Invitees []string
}

// EventRef é o evento criado, tal como o cartão de ação o mostra.
type EventRef struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	StartsAt string `json:"starts_at"`
	MeetURL  string `json:"meet_url,omitempty"`
}

// EventCreate cria o evento com as MESMAS consultas do handler da interface (§5: sem SQL novo).
type EventCreate func(ctx context.Context, id Identity, ev NewEvent) (EventRef, error)

// calendarEventArgs são os campos que o modelo preenche. Os tempos são RFC3339 — o prompt do sistema
// dá-lhe o presente para resolver datas relativas ("amanhã às 10h").
type calendarEventArgs struct {
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Location    string   `json:"location"`
	StartsAt    string   `json:"starts_at"`
	EndsAt      string   `json:"ends_at"`
	CreateMeet  bool     `json:"create_meet"`
	Invitees    []string `json:"invitees"`
}

// hasInvitees diz se estes args pedem para convidar alguém. Decide a confirmação da ferramenta
// (§6.3): convidar avisa outra pessoa. Args ilegíveis → true (fail-closed).
func hasInvitees(raw json.RawMessage) bool {
	var a calendarEventArgs
	if err := decodeArgs(raw, &a); err != nil {
		return true
	}
	for _, e := range a.Invitees {
		if strings.TrimSpace(e) != "" {
			return true
		}
	}
	return false
}

func calendarTools(create EventCreate) []Tool {
	return []Tool{
		{
			Name: ToolCalendarCreate,
			Description: "Create an event on the signed-in user's calendar. Times are RFC3339. Pass " +
				"invitees (email addresses of colleagues in the same organisation) to invite people — " +
				"they get an in-app invitation and RSVP. When the user asks for a meeting WITH someone, " +
				"add them to invitees, do not just put their name in the title. External addresses " +
				"cannot be invited to the calendar; tell those guests with " + ToolMailSend + " instead. " +
				"Set create_meet to attach a video-call link.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"title": {"type": "string", "description": "Title of the event."},
					"description": {"type": "string"},
					"location": {"type": "string"},
					"starts_at": {"type": "string", "description": "Start time, RFC3339 (e.g. 2026-07-22T10:00:00+02:00)."},
					"ends_at": {"type": "string", "description": "End time, RFC3339. Omit for a one-hour event."},
					"invitees": {"type": "array", "items": {"type": "string"}, "description": "Email addresses of colleagues to invite (RSVP). Come from the user or their contacts."},
					"create_meet": {"type": "boolean", "description": "Attach a video-call (Meet) link to the event."}
				},
				"required": ["title", "starts_at"]
			}`),
			Writes: true,
			// A solo, o evento é interno (sem confirmação). COM convidados, avisa outra pessoa —
			// convidar → §6.3 — por isso confirma o conteúdo à vista. ConfirmWhen decide por chamada.
			ConfirmWhen: hasInvitees,
			Handler: func(ctx context.Context, id Identity, raw json.RawMessage) (Result, error) {
				var a calendarEventArgs
				if err := decodeArgs(raw, &a); err != nil {
					return Result{}, err
				}
				a.Title = strings.TrimSpace(a.Title)
				if a.Title == "" {
					return Result{}, fmt.Errorf("title is required")
				}
				starts, err := time.Parse(time.RFC3339, strings.TrimSpace(a.StartsAt))
				if err != nil {
					return Result{}, fmt.Errorf("starts_at must be an RFC3339 time: %w", err)
				}
				// Sem fim explícito, uma hora — o default da interface para eventos sem duração.
				ends := starts.Add(time.Hour)
				if s := strings.TrimSpace(a.EndsAt); s != "" {
					ends, err = time.Parse(time.RFC3339, s)
					if err != nil {
						return Result{}, fmt.Errorf("ends_at must be an RFC3339 time: %w", err)
					}
				}
				if !ends.After(starts) {
					return Result{}, fmt.Errorf("the event must end after it starts")
				}
				ref, err := create(ctx, id, NewEvent{
					Title:       a.Title,
					Description: strings.TrimSpace(a.Description),
					Location:    strings.TrimSpace(a.Location),
					StartsAt:    starts,
					EndsAt:      ends,
					CreateMeet:  a.CreateMeet,
					Invitees:    cleanAddrs(a.Invitees),
				})
				if err != nil {
					return Result{}, fmt.Errorf("event creation failed: %w", err)
				}
				return Result{
					Summary: fmt.Sprintf("%s — %s", a.Title, starts.Format("2 Jan 15:04")),
					Data:    ref,
				}, nil
			},
		},
	}
}
