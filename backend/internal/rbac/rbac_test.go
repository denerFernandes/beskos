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
