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
	PermDomainsManage Permission = "domains.manage" // adicionar/editar domínio
	PermDomainsDelete Permission = "domains.delete" // remover domínio (destrutivo, à parte)
	PermMailboxManage Permission = "mailboxes.manage"
	// Destroying a mailbox is separate from running one, the same way removing a domain is: the
	// person who creates accounts and resets passwords every day has no need to destroy anybody's
	// correspondence, and should not be one misclick away from it.
	PermMailboxDelete Permission = "mailboxes.delete"
	PermMembersView   Permission = "members.view"
	PermMembersInvite Permission = "members.invite" // convidar/criar conta
	PermMembersManage Permission = "members.manage" // mudar papel, remover, permissões
	PermGroupsManage  Permission = "groups.manage"  // grupos de utilizadores
	PermBillingView   Permission = "billing.view"   // ver plano e faturas
	PermBillingManage Permission = "billing.manage" // trocar plano, portal, cancelar
	PermUsageView     Permission = "usage.view"
	PermTenantDelete  Permission = "tenant.delete"
	// org.settings era o balde de tudo o que fosse "da organização" — marca, apps, política de
	// segurança, comunicados, assistente e home da wiki. Partido para dar para confiar a alguém uma
	// coisa sem lhe entregar as outras (era o pedido: permissões mais granulares, 27/07/2026).
	PermOrgBranding     Permission = "org.branding"      // marca/aparência e apps visíveis
	PermOrgSecurity     Permission = "org.security"      // política de segurança (MFA, sessões)
	PermAnnouncements   Permission = "org.announcements" // comunicados internos
	PermAssistantManage Permission = "assistant.manage"  // agentes e governança do assistente
	PermWikiManage      Permission = "wiki.manage"       // home da wiki (conteúdo da organização)
)

// Role é um papel fixo. A hierarquia é por conjunto de permissões, não por herança implícita.
type Role string

const (
	RoleOwner         Role = "owner"          // dono da organização — tudo
	RoleAdmin         Role = "admin"          // administra a organização — todas as permissões
	RoleMember        Role = "member"         // leitura
	RoleBilling       Role = "billing"        // faturamento + uso
	RoleInternalAdmin Role = "internal_admin" // nossa equipe (suporte) — tudo
)

// rolePermissions é a matriz autoritativa. Adicionar papel/permissão = editar aqui.
var rolePermissions = map[Role]map[Permission]bool{
	RoleOwner:         allPerms(),
	RoleInternalAdmin: allPerms(),
	// Admin tem TUDO, por decisão do produto (27/07/2026): quem é admin da organização não deve
	// esbarrar em permissões. A hierarquia continua a valer noutra dimensão — o rank impede um admin
	// de mexer num owner (CanManage), e uma permissão pode sempre ser retirada a UMA pessoa concreta
	// pelas exceções por membro. O que desapareceu foi o teto por papel.
	RoleAdmin: allPerms(),
	RoleMember: set(
		PermDomainsView, PermUsageView,
	),
	RoleBilling: set(
		PermBillingView, PermBillingManage, PermUsageView, PermMembersView,
	),
}

// AllPermissions lista todas as permissões conhecidas (usado por owner/internal e nos testes).
func AllPermissions() []Permission {
	return []Permission{
		PermDomainsView, PermDomainsManage, PermDomainsDelete, PermMailboxManage, PermMailboxDelete,
		PermMembersView, PermMembersInvite, PermMembersManage, PermGroupsManage,
		PermBillingView, PermBillingManage, PermUsageView, PermTenantDelete,
		PermOrgBranding, PermOrgSecurity, PermAnnouncements, PermAssistantManage, PermWikiManage,
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

// Effective devolve as permissões de alguém: as do papel, mais as concedidas à pessoa, menos as
// retiradas. É a resposta autoritativa — os handlers e o /me passam por aqui, nunca por Has direto.
// Retirar ganha sempre a conceder: numa contradição, a decisão mais restritiva é a que fica.
func Effective(role Role, allow, deny []Permission) []Permission {
	granted := map[Permission]bool{}
	for _, p := range AllPermissions() {
		granted[p] = Has(role, p)
	}
	for _, p := range allow {
		if _, known := granted[p]; known {
			granted[p] = true
		}
	}
	for _, p := range deny {
		granted[p] = false
	}
	var out []Permission
	for _, p := range AllPermissions() {
		if granted[p] {
			out = append(out, p)
		}
	}
	return out
}

// HasEffective é o Effective para uma única permissão (o caminho quente dos handlers).
func HasEffective(role Role, allow, deny []Permission, perm Permission) bool {
	for _, p := range deny {
		if p == perm {
			return false
		}
	}
	for _, p := range allow {
		if p == perm {
			return true
		}
	}
	return Has(role, perm)
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
