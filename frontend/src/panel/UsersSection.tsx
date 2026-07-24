import { useState, useMemo } from "react";
import { useTranslation } from "react-i18next";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { api, hasPerm, type Me } from "../api";
import { Modal, toast } from "./ui";
import { GroupPicker } from "./GroupPicker";
import { IcPlus, IcKey, IcTrash, IcColumns } from "./icons";

const ROLES = ["owner", "admin", "member", "billing"];

// Usuários = membros (papéis) + caixa de email. Users e Members são a mesma coisa; diferem no papel.
export function UsersSection({ me }: { me: Me | undefined }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const canManage = hasPerm(me, "members.manage");
  const members = useQuery({ queryKey: ["members"], queryFn: api.members, retry: false });
  // Chave "admin-mailboxes": NÃO usar ["mailboxes"] — essa é das pastas JMAP do webmail.
  const mailboxes = useQuery({ queryKey: ["admin-mailboxes"], queryFn: api.mailboxes, retry: false });
  const domains = useQuery({ queryKey: ["domains"], queryFn: api.domains, retry: false });
  const activeDomains = (domains.data ?? []).filter((d) => d.provision_state === "active");

  const [adding, setAdding] = useState(false);
  const [reset, setReset] = useState<string | null>(null);
  const [groupsOf, setGroupsOf] = useState<{ id: string; label: string } | null>(null);

  // Une membros (contas do painel, com papel) + caixas (mailboxes) por email: um usuário pode
  // ter conta de painel, caixa, ou ambos. Assim TODOS aparecem (bug: caixas sem conta sumiam).
  const users = useMemo(() => {
    const byEmail = new Map<string, { email: string; role: string; panelId?: string; mailboxId?: string }>();
    for (const m of members.data ?? []) byEmail.set(m.email, { email: m.email, role: m.role, panelId: m.id });
    for (const mb of mailboxes.data ?? []) {
      const e = byEmail.get(mb.email) ?? { email: mb.email, role: "member" };
      e.mailboxId = mb.id;
      byEmail.set(mb.email, e);
    }
    return [...byEmail.values()].sort((a, b) => a.email.localeCompare(b.email));
  }, [members.data, mailboxes.data]);

  const changeRole = useMutation({
    mutationFn: ({ id, role }: { id: string; role: string }) => api.changeRole(id, role),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["members"] }), onError: (e) => toast((e as Error).message),
  });
  const remove = useMutation({
    mutationFn: (id: string) => api.removeMember(id),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ["members"] }); toast(t("members.removed")); }, onError: (e) => toast((e as Error).message),
  });

  return (
    <>
      <div className="pnl-top">
        <div><h1>{t("panel.users")}</h1><div className="desc">{t("usersSection.desc")}</div></div>
        <div className="grow" />
        {canManage && <button className="pnl-btn primary" disabled={activeDomains.length === 0} onClick={() => setAdding(true)}><IcPlus size={16} />{t("usersSection.add")}</button>}
      </div>
      <div className="pnl-content">
        {activeDomains.length === 0 && <p className="desc" style={{ marginBottom: 12 }}>{t("usersSection.needDomain")}</p>}
        <div className="pnl-card"><div className="pnl-card-b">
          {users.length === 0 ? <div className="pnl-empty">{t("mailboxes.empty")}</div> : users.map((u) => (
            <div className="pnl-row" key={u.email}>
              <div className="pnl-avatar" style={{ width: 32, height: 32, fontSize: 12 }}>{u.email.slice(0, 2).toUpperCase()}</div>
              <div className="grow"><div className="mono">{u.email}</div>
                {!u.mailboxId && <div className="sub">{t("usersSection.noMailbox")}</div>}</div>
              {canManage && u.panelId
                ? <select className="pnl-select" style={{ width: 150 }} value={u.role} onChange={(e) => changeRole.mutate({ id: u.panelId!, role: e.target.value })}>
                    {ROLES.map((r) => <option key={r} value={r}>{t(`members.roles.${r}`)}</option>)}
                  </select>
                : <span className="pnl-pill muted">{t(`members.roles.${u.role}`, { defaultValue: u.role })}</span>}
              {canManage && u.panelId && <button className="pnl-icon-btn" title={t("groups.userGroups")} onClick={() => setGroupsOf({ id: u.panelId!, label: u.email })}><IcColumns size={16} /></button>}
              {canManage && u.mailboxId && <button className="pnl-icon-btn" title={t("mailboxes.changePassword")} onClick={() => setReset(u.email)}><IcKey size={16} /></button>}
              {canManage && u.panelId && <button className="pnl-icon-btn" title={t("members.remove")} onClick={() => remove.mutate(u.panelId!)}><IcTrash size={16} /></button>}
            </div>
          ))}
        </div></div>
      </div>

      {adding && <AddUserModal domains={activeDomains} actorRole={me?.role ?? ""} onClose={() => setAdding(false)}
        onDone={() => { setAdding(false); qc.invalidateQueries({ queryKey: ["members"] }); qc.invalidateQueries({ queryKey: ["admin-mailboxes"] }); toast(t("usersSection.created")); }} />}
      {reset && <ResetModal email={reset} onClose={() => setReset(null)} />}
      {groupsOf && <UserGroupsModal userId={groupsOf.id} label={groupsOf.label} onClose={() => setGroupsOf(null)} />}
    </>
  );
}

function AddUserModal({ domains, actorRole, onClose, onDone }: { domains: { id: string; dominio: string }[]; actorRole: string; onClose: () => void; onDone: () => void }) {
  const { t } = useTranslation();
  const [domainId, setDomainId] = useState(domains[0]?.id ?? "");
  const [login, setLogin] = useState(""), [name, setName] = useState(""), [pwd, setPwd] = useState(""), [role, setRole] = useState("member"), [err, setErr] = useState("");
  const [groups, setGroups] = useState<string[]>([]); // pode entrar em vários grupos de uma vez
  const roles = actorRole === "owner" ? ROLES : ROLES.filter((r) => r !== "owner");
  const m = useMutation({
    mutationFn: () => api.createUser(domainId, login, name, pwd, role, groups),
    onSuccess: onDone, onError: (e) => setErr((e as Error).message),
  });
  const dom = domains.find((d) => d.id === domainId)?.dominio ?? "";
  // Aviso de cobrança (24/07/2026): com assinatura Stripe ativa e plano pago, um utilizador novo
  // entra na assinatura com valor PROPORCIONAL aos dias restantes do período.
  const billing = useQuery({ queryKey: ["billing"], queryFn: api.billing, retry: false });
  const b = billing.data;
  const paid = !!b?.subscription?.manageable && (b?.current_plan.price_cents_per_user ?? 0) > 0;
  const unit = b ? new Intl.NumberFormat(undefined, { style: "currency", currency: b.currency }).format(b.current_plan.price_cents_per_user / 100) : "";
  return (
    <Modal title={t("usersSection.add")} onClose={onClose}
      footer={<><button className="pnl-btn ghost" onClick={onClose}>{t("common.cancel")}</button>
        <button className="pnl-btn primary" disabled={m.isPending} onClick={() => m.mutate()}>{t("usersSection.create")}</button></>}>
      <div className="pnl-field"><label>{t("panel.domains")}</label>
        <select className="pnl-select" value={domainId} onChange={(e) => setDomainId(e.target.value)}>{domains.map((d) => <option key={d.id} value={d.id}>{d.dominio}</option>)}</select></div>
      <div className="pnl-field"><label>{t("usersSection.login")}</label>
        <div style={{ display: "flex", alignItems: "center", gap: 6 }}>
          <input className="pnl-input" value={login} onChange={(e) => setLogin(e.target.value)} placeholder="ana" autoFocus /><span className="mono desc">@{dom}</span></div></div>
      <div className="pnl-field"><label>{t("usersSection.name")}</label><input className="pnl-input" value={name} onChange={(e) => setName(e.target.value)} /></div>
      <div className="pnl-field"><label>{t("groups.title")}</label>
        <GroupPicker value={groups} onChange={setGroups} /></div>
      <div className="pnl-field"><label>{t("membersSection.role")}</label>
        <select className="pnl-select" value={role} onChange={(e) => setRole(e.target.value)}>{roles.map((r) => <option key={r} value={r}>{t(`members.roles.${r}`)}</option>)}</select></div>
      <div className="pnl-field"><label>{t("login.password")}</label><input className="pnl-input" type="password" value={pwd} onChange={(e) => setPwd(e.target.value)} /></div>
      {paid && <div className="pnl-note">{t("usersSection.billingNote", { price: unit })}</div>}
      {err && <p className="pnl-err">{err}</p>}
    </Modal>
  );
}

function ResetModal({ email, onClose }: { email: string; onClose: () => void }) {
  const { t } = useTranslation();
  const [pwd, setPwd] = useState(""), [err, setErr] = useState("");
  const m = useMutation({
    mutationFn: () => api.resetUserPassword(email, pwd),
    onSuccess: () => { onClose(); toast(t("mailboxes.passwordChanged")); }, onError: (e) => setErr((e as Error).message),
  });
  return (
    <Modal title={t("mailboxes.changePassword")} onClose={onClose}
      footer={<><button className="pnl-btn ghost" onClick={onClose}>{t("common.cancel")}</button>
        <button className="pnl-btn primary" disabled={m.isPending || !pwd} onClick={() => m.mutate()}>{t("mailboxes.changePassword")}</button></>}>
      <p className="desc" style={{ marginTop: 0 }}>{email}</p>
      <div className="pnl-field"><label>{t("usersSection.newPassword")}</label><input className="pnl-input" type="password" value={pwd} onChange={(e) => setPwd(e.target.value)} autoFocus /></div>
      {err && <p className="pnl-err">{err}</p>}
    </Modal>
  );
}

// Grupos DO usuário: marca/desmarca vários. Salvar sincroniza os canais do chat correspondentes.
function UserGroupsModal({ userId, label, onClose }: { userId: string; label: string; onClose: () => void }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const cur = useQuery({ queryKey: ["user-groups", userId], queryFn: () => api.userGroups(userId) });
  const [sel, setSel] = useState<string[] | null>(null);
  const value = sel ?? (cur.data ?? []).map((g) => g.id);
  const save = useMutation({
    mutationFn: () => api.setUserGroups(userId, value),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["groups"] });
      qc.invalidateQueries({ queryKey: ["user-groups", userId] });
      toast(t("groups.saved"));
      onClose();
    },
    onError: (e) => toast((e as Error).message),
  });
  return (
    <Modal title={t("groups.userGroupsOf", { who: label })} onClose={onClose}
      footer={<><button className="pnl-btn ghost" onClick={onClose}>{t("common.cancel")}</button>
        <button className="pnl-btn primary" disabled={save.isPending} onClick={() => save.mutate()}>{t("groups.save")}</button></>}>
      <p className="desc" style={{ marginTop: 0 }}>{t("groups.multiHint")}</p>
      <GroupPicker value={value} onChange={setSel} />
    </Modal>
  );
}
