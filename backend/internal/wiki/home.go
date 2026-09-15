package wiki

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/beskosorg/beskos/backend/internal/db"
)

// Home da app Wiki (Bloco 2 B5): uma "página" por tenant mostrada no ecrã inicial, editada por
// admins da org com o mesmo editor. A autorização de escrita (PermOrgSettings) é aplicada na ROTA
// (requirePerm) — aqui só regras de dados. Ler é de qualquer utilizador autenticado do tenant.

// GetHome returns the tenant's wiki home; a tenant without one gets an empty home (not an error).
func (s *Service) GetHome(ctx context.Context, tenantID uuid.UUID) (db.WikiHome, error) {
	h, err := s.q.GetWikiHome(ctx, tenantID)
	if errors.Is(err, pgx.ErrNoRows) {
		return db.WikiHome{TenantID: tenantID, BodyJson: []byte("[]")}, nil
	}
	return h, err
}

// SetHome saves the tenant's wiki home from the editor document, deriving text/html like any page.
func (s *Service) SetHome(ctx context.Context, tenantID, userID uuid.UUID, bodyJSON json.RawMessage) (db.WikiHome, error) {
	body := normalizeBody(bodyJSON)
	text, html := derive(body)
	return s.q.UpsertWikiHome(ctx, db.UpsertWikiHomeParams{
		TenantID: tenantID, BodyJson: body, BodyText: text, BodyHtml: html,
		UpdatedBy: pgtype.UUID{Bytes: userID, Valid: true},
	})
}
