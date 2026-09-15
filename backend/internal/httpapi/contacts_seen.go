package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/mail"
	"strings"

	"github.com/google/uuid"

	"github.com/beskosorg/beskos/backend/internal/db"
)

// recordRecipientContacts cria um contacto (pessoa) para cada endereço a quem se envia e que ainda
// não esteja na agenda do utilizador — para que da próxima vez apareça no autocomplete do "Para".
// Idempotente: um endereço já conhecido é ignorado. Falhas não interrompem nada (é um efeito lateral
// do envio, não o envio em si). Escopo por tenant+dono, como todo o resto (lei 5.1).
func (s *Server) recordRecipientContacts(ctx context.Context, tenantID, userID uuid.UUID, addrs []string) {
	// Nomes e e-mails já existentes, para não duplicar. A chave é o e-mail em minúsculas.
	known := map[string]bool{}
	rows, err := s.q.ListContacts(ctx, db.ListContactsParams{OwnerUserID: userID, TenantID: tenantID})
	if err != nil {
		return
	}
	for _, c := range rows {
		if e := strings.ToLower(strings.TrimSpace(c.Email)); e != "" {
			known[e] = true
		}
	}
	for _, raw := range addrs {
		name, email := parseRecipient(raw)
		email = strings.ToLower(email)
		if email == "" || known[email] {
			continue
		}
		if name == "" {
			name = email // sem nome no endereço: usa o próprio e-mail
		}
		if _, err := s.q.CreateContact(ctx, db.CreateContactParams{
			TenantID: tenantID, OwnerUserID: userID, Kind: "person", Name: name, Email: email,
			Tags: []string{}, ExtraEmails: []string{}, ExtraPhones: []string{},
			AllowPhone: true, AllowEmail: true,
			// custom_fields is jsonb NOT NULL: without this the driver sends NULL and the INSERT
			// fails silently (the loop does `continue` and no contact gets saved).
			CustomFields: json.RawMessage("{}"),
		}); err != nil {
			continue
		}
		known[email] = true // não repetir dentro do mesmo lote (To e Cc podem coincidir)
	}
}

// parseRecipient extrai nome e e-mail de "Nome <a@b>" ou "a@b". Sem e-mail válido, devolve vazio.
func parseRecipient(raw string) (name, email string) {
	if a, err := mail.ParseAddress(strings.TrimSpace(raw)); err == nil {
		return strings.TrimSpace(a.Name), strings.TrimSpace(a.Address)
	}
	s := strings.TrimSpace(raw)
	if strings.Contains(s, "@") && !strings.ContainsAny(s, " <>") {
		return "", s
	}
	return "", ""
}

type seenRecipientsReq struct {
	Recipients []string `json:"recipients"`
}

// handleSeenRecipients grava como contactos os destinatários de um e-mail acabado de enviar pelo
// webmail (o envio manual é feito pelo cliente via JMAP, por isso é o cliente que nos avisa). O
// envio pelo assistente grava pelo lado do servidor (ver assistantMailAccess).
func (s *Server) handleSeenRecipients(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	var req seenRecipientsReq
	if !decode(w, r, &req) {
		return
	}
	s.recordRecipientContacts(r.Context(), mu.TenantID, mu.ID, req.Recipients)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
