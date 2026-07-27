package httpapi

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"mailin/backend/internal/db"
	"mailin/backend/internal/rbac"
)

// Exceções de permissão por membro (RBAC granular). O papel continua a ser o ponto de partida; isto
// resolve os casos que nenhum papel de fábrica cobre. As regras que importam:
//
//   - retirar ganha a conceder (rbac.Effective), para uma contradição cair sempre do lado seguro;
//   - quem edita só pode conceder/retirar o que ELE PRÓPRIO tem — senão um admin dava-se billing a
//     si mesmo por interposta pessoa (escalada de privilégio);
//   - e só age sobre alguém de rank inferior (rbac.CanManage), a mesma regra do mudar papel.

// memberOverrides lê as exceções de um utilizador, já separadas em conceder/retirar.
func (s *Server) memberOverrides(ctx context.Context, tenant, user uuid.UUID) (allow, deny []rbac.Permission) {
	rows, err := s.q.ListMemberPermissions(ctx, db.ListMemberPermissionsParams{TenantID: tenant, UserID: user})
	if err != nil {
		return nil, nil // sem exceções (ou falha de leitura): fica o papel, que é o comportamento seguro
	}
	for _, r := range rows {
		if r.Allow {
			allow = append(allow, rbac.Permission(r.Permission))
		} else {
			deny = append(deny, rbac.Permission(r.Permission))
		}
	}
	return allow, deny
}

// effectivePerms devolve as permissões efetivas de um utilizador (papel ± exceções).
func (s *Server) effectivePerms(ctx context.Context, tenant, user uuid.UUID, role string) []rbac.Permission {
	allow, deny := s.memberOverrides(ctx, tenant, user)
	return rbac.Effective(rbac.Role(role), allow, deny)
}

// handleGetMemberPermissions devolve o que vem do papel e o que foi ajustado à mão, para a UI poder
// mostrar a diferença em vez de uma lista de caixas sem contexto.
func (s *Server) handleGetMemberPermissions(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "rbac.invalid_id")
		return
	}
	target, err := s.q.GetMailUserInTenant(r.Context(), db.GetMailUserInTenantParams{ID: id, TenantID: mu.TenantID})
	if err != nil {
		writeErr(w, http.StatusNotFound, "rbac.not_found")
		return
	}
	allow, deny := s.memberOverrides(r.Context(), mu.TenantID, target.ID)
	writeJSON(w, http.StatusOK, map[string]any{
		"role":      target.Role,
		"all":       rbac.AllPermissions(),
		"from_role": rbac.Permissions(rbac.Role(target.Role)),
		"effective": rbac.Effective(rbac.Role(target.Role), allow, deny),
		"allow":     orEmpty(allow),
		"deny":      orEmpty(deny),
		// O que EU posso mexer: não se concede o que não se tem.
		"editable": s.effectivePerms(r.Context(), mu.TenantID, mu.ID, mu.Role),
	})
}

// handleSetMemberPermissions grava as exceções (substitui as que houver).
func (s *Server) handleSetMemberPermissions(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "rbac.invalid_id")
		return
	}
	target, err := s.q.GetMailUserInTenant(r.Context(), db.GetMailUserInTenantParams{ID: id, TenantID: mu.TenantID})
	if err != nil {
		writeErr(w, http.StatusNotFound, "rbac.not_found")
		return
	}
	// Mesma hierarquia do mudar papel: não se ajusta quem está no mesmo nível ou acima.
	if !rbac.CanManage(rbac.Role(mu.Role), rbac.Role(target.Role)) {
		writeErr(w, http.StatusForbidden, "rbac.forbidden")
		return
	}
	var req struct {
		Allow []string `json:"allow"`
		Deny  []string `json:"deny"`
	}
	if !decode(w, r, &req) {
		return
	}
	mine := map[rbac.Permission]bool{}
	for _, p := range s.effectivePerms(r.Context(), mu.TenantID, mu.ID, mu.Role) {
		mine[p] = true
	}
	known := map[rbac.Permission]bool{}
	for _, p := range rbac.AllPermissions() {
		known[p] = true
	}
	type entry struct {
		perm  rbac.Permission
		allow bool
	}
	var entries []entry
	for _, list := range []struct {
		vals  []string
		allow bool
	}{{req.Allow, true}, {req.Deny, false}} {
		for _, raw := range list.vals {
			p := rbac.Permission(raw)
			if !known[p] {
				writeErr(w, http.StatusBadRequest, "rbac.unknown_permission")
				return
			}
			// Vale para conceder E para retirar: quem não tem a permissão também não a governa.
			if !mine[p] {
				writeErr(w, http.StatusForbidden, "rbac.cannot_grant")
				return
			}
			entries = append(entries, entry{p, list.allow})
		}
	}
	if err := s.q.DeleteMemberPermissions(r.Context(), db.DeleteMemberPermissionsParams{TenantID: mu.TenantID, UserID: target.ID}); err != nil {
		writeErr(w, http.StatusInternalServerError, "rbac.failed")
		return
	}
	for _, e := range entries {
		if err := s.q.InsertMemberPermission(r.Context(), db.InsertMemberPermissionParams{
			TenantID: mu.TenantID, UserID: target.ID, Permission: string(e.perm), Allow: e.allow,
			CreatedBy: pgUUID(mu.ID),
		}); err != nil {
			writeErr(w, http.StatusInternalServerError, "rbac.failed")
			return
		}
	}
	allow, deny := s.memberOverrides(r.Context(), mu.TenantID, target.ID)
	writeJSON(w, http.StatusOK, map[string]any{
		"effective": rbac.Effective(rbac.Role(target.Role), allow, deny),
		"allow":     orEmpty(allow), "deny": orEmpty(deny),
	})
}
