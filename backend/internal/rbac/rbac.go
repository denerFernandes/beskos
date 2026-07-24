// Package rbac define o controle de acesso do painel: papéis FIXOS mapeados para permissões
// granulares, checadas no backend (lei §5.2). Papéis customizáveis por tenant ficam para depois.
//
// Segurança: a fonte de verdade é o papel salvo do usuário autenticado (nunca vindo do request).
// A checagem é allow-list explícita — na dúvida, nega.
package rbac

// Permission é uma capacidade granular. Handlers exigem uma permissão específica.
type Permission string

const (
	PermDomainsView   Permission = "domains.view"
	PermDomainsManage Permission = "domains.manage"
	PermMailboxManage Permission = "mailboxes.manage"
	PermMembersView   Permission = "members.view"
	PermMembersManage Permission = "members.manage" // convidar, mudar papel, remover
	PermBillingManage Permission = "billing.manage"
	PermUsageView     Permission = "usage.view"
	PermTenantDelete  Permission = "tenant.delete"
	PermOrgSettings   Permission = "org.settings" // marca/aparência da organização (cor, logo)
)

// Role é um papel fixo. A hierarquia é por conjunto de permissões, não por herança implícita.
type Role string

const (
	RoleOwner         Role = "owner"          // dono da organização — tudo
	RoleAdmin         Role = "admin"          // gestão operacional, sem billing/excluir org
	RoleMember        Role = "member"         // leitura
	RoleBilling       Role = "billing"        // faturamento + uso
	RoleInternalAdmin Role = "internal_admin" // nossa equipe (suporte) — tudo
)

// rolePermissions é a matriz autoritativa. Adicionar papel/permissão = editar aqui.
var rolePermissions = map[Role]map[Permission]bool{
	RoleOwner:         allPerms(),
	RoleInternalAdmin: allPerms(),
	RoleAdmin: set(
		PermDomainsView, PermDomainsManage, PermMailboxManage,
		PermMembersView, PermMembersManage, PermUsageView, PermOrgSettings,
	),
	RoleMember: set(
		PermDomainsView, PermUsageView,
	),
	RoleBilling: set(
		PermBillingManage, PermUsageView, PermMembersView,
	),
}

// AllPermissions lista todas as permissões conhecidas (usado por owner/internal e nos testes).
func AllPermissions() []Permission {
	return []Permission{
		PermDomainsView, PermDomainsManage, PermMailboxManage,
		PermMembersView, PermMembersManage, PermBillingManage,
		PermUsageView, PermTenantDelete, PermOrgSettings,
	}
}

// AssignableRoles são os papéis que um admin pode atribuir num convite/edição.
// internal_admin NÃO é atribuível por clientes (só provisionado internamente).
func AssignableRoles() []Role { return []Role{RoleOwner, RoleAdmin, RoleMember, RoleBilling} }

func IsAssignableRole(r Role) bool {
	for _, x := range AssignableRoles() {
		if x == r {
			return true
		}
	}
	return false
}

// roleRank define a hierarquia para operações administrativas ENTRE usuários (reset de senha,
// remoção, mudança de papel). Papel desconhecido = 0. A checagem de permissão (Has) diz se o ator
// PODE gerir membros; o rank diz sobre QUEM ele pode agir — nunca sobre alguém de rank ≥ o seu.
var roleRank = map[Role]int{
	RoleInternalAdmin: 100,
	RoleOwner:         90,
	RoleAdmin:         50,
	RoleBilling:       20,
	RoleMember:        10,
}

// Rank devolve o nível hierárquico do papel (0 se desconhecido).
func Rank(role Role) int { return roleRank[role] }

// CanManage responde se o ator pode executar uma ação administrativa sobre o alvo.
// Regra fail-closed: o rank do ator precisa ser ESTRITAMENTE maior que o do alvo. Assim um admin
// não redefine a senha de um owner nem de outro admin (lei §5.2).
func CanManage(actor, target Role) bool {
	return roleRank[actor] > roleRank[target]
}

// Has responde se o papel possui a permissão. Papel desconhecido → nega (fail-closed).
func Has(role Role, perm Permission) bool {
	perms, ok := rolePermissions[role]
	if !ok {
		return false
	}
	return perms[perm]
}

// Permissions devolve as permissões do papel (para o /me, a UI gatear botões).
func Permissions(role Role) []Permission {
	var out []Permission
	for _, p := range AllPermissions() {
		if Has(role, p) {
			out = append(out, p)
		}
	}
	return out
}

func set(perms ...Permission) map[Permission]bool {
	m := make(map[Permission]bool, len(perms))
	for _, p := range perms {
		m[p] = true
	}
	return m
}

func allPerms() map[Permission]bool {
	m := map[Permission]bool{}
	for _, p := range AllPermissions() {
		m[p] = true
	}
	return m
}
