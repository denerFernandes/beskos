package rbac

import "testing"

func TestOwnerAndInternalHaveEverything(t *testing.T) {
	for _, role := range []Role{RoleOwner, RoleInternalAdmin} {
		for _, p := range AllPermissions() {
			if !Has(role, p) {
				t.Errorf("%s deveria ter %s", role, p)
			}
		}
	}
}

func TestPermissionMatrix(t *testing.T) {
	cases := []struct {
		role Role
		perm Permission
		want bool
	}{
		{RoleAdmin, PermDomainsManage, true},
		{RoleAdmin, PermMembersManage, true},
		{RoleAdmin, PermBillingManage, false}, // admin NÃO faz billing
		{RoleAdmin, PermTenantDelete, false},  // admin NÃO exclui a org
		{RoleMember, PermDomainsView, true},
		{RoleMember, PermDomainsManage, false}, // member é leitura
		{RoleMember, PermMembersManage, false},
		{RoleBilling, PermBillingManage, true},
		{RoleBilling, PermDomainsManage, false},
		{RoleBilling, PermMembersManage, false},
	}
	for _, c := range cases {
		if got := Has(c.role, c.perm); got != c.want {
			t.Errorf("Has(%s,%s)=%v, quer %v", c.role, c.perm, got, c.want)
		}
	}
}

func TestUnknownRoleFailsClosed(t *testing.T) {
	if Has(Role("hacker"), PermDomainsView) {
		t.Error("papel desconhecido não pode ter permissão")
	}
	if len(Permissions(Role(""))) != 0 {
		t.Error("papel vazio não tem permissões")
	}
}

func TestOnlyOwnerAndInternalCanDeleteTenant(t *testing.T) {
	for _, r := range []Role{RoleAdmin, RoleMember, RoleBilling} {
		if Has(r, PermTenantDelete) {
			t.Errorf("%s não pode excluir tenant", r)
		}
	}
}

func TestInternalAdminNotClientAssignable(t *testing.T) {
	if IsAssignableRole(RoleInternalAdmin) {
		t.Error("internal_admin não pode ser atribuído por clientes (escalonamento)")
	}
	for _, r := range []Role{RoleOwner, RoleAdmin, RoleMember, RoleBilling} {
		if !IsAssignableRole(r) {
			t.Errorf("%s deveria ser atribuível", r)
		}
	}
}

// As exceções por membro são decisão de segurança: o que interessa testar é a ordem de precedência
// (retirar ganha), o que fica intocado e que uma permissão inventada não entra por engano.
func TestEffectiveOverrides(t *testing.T) {
	has := func(list []Permission, p Permission) bool {
		for _, x := range list {
			if x == p {
				return true
			}
		}
		return false
	}

	// Conceder por cima do papel: um admin não apaga domínios, mas pode receber a permissão.
	got := Effective(RoleAdmin, []Permission{PermDomainsDelete}, nil)
	if !has(got, PermDomainsDelete) {
		t.Error("conceder devia acrescentar domains.delete ao admin")
	}
	if !has(got, PermMembersManage) {
		t.Error("conceder não pode tirar o que o papel já dava")
	}

	// Retirar do papel: admin sem gestão de membros.
	got = Effective(RoleAdmin, nil, []Permission{PermMembersManage})
	if has(got, PermMembersManage) {
		t.Error("retirar devia tirar members.manage ao admin")
	}

	// Contradição: retirar ganha, sempre (fail-closed).
	got = Effective(RoleAdmin, []Permission{PermBillingManage}, []Permission{PermBillingManage})
	if has(got, PermBillingManage) {
		t.Error("com conceder e retirar ao mesmo tempo, deve ficar retirado")
	}

	// Permissão desconhecida não entra na lista efetiva.
	got = Effective(RoleMember, []Permission{"inventada.qualquer"}, nil)
	for _, p := range got {
		if p == "inventada.qualquer" {
			t.Error("permissão desconhecida não devia ser concedida")
		}
	}

	// Owner com uma exceção a retirar continua a perder a permissão (ninguém está acima da regra).
	if HasEffective(RoleOwner, nil, []Permission{PermTenantDelete}, PermTenantDelete) {
		t.Error("retirar tem de valer também para o owner")
	}
	// E o caminho rápido concorda com o completo.
	if HasEffective(RoleMember, []Permission{PermUsageView}, nil, PermUsageView) != has(Effective(RoleMember, []Permission{PermUsageView}, nil), PermUsageView) {
		t.Error("HasEffective e Effective têm de dar a mesma resposta")
	}
}
