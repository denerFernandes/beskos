import { useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { backend, colleaguesApi, contactsApi, type StorageFile, type Share } from "./backend";
import { useUserSettings } from "./useSettings";
import { toast } from "./toast";
import { hueFor } from "./util";
import { Avatar } from "./Avatar";
import { IcTrash, IcLink, IcSearch } from "../panel/icons";
import { publicUrl } from "../apiBase";

const EMAIL_RE = /^[^\s@]+@[^\s@]+\.[^\s@]{2,}$/;

// Busca de pessoas para partilhar: colaboradores + contactos enquanto escreve, e aceita e-mail
// livre digitado (padrão do InviteePicker). Escolher/confirmar chama onAdd com o e-mail.
function PeopleSearch({ onAdd, disabled }: { onAdd: (email: string) => void; disabled?: boolean }) {
  const { t } = useTranslation();
  const [q, setQ] = useState("");
  const [open, setOpen] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);
  const colleagues = useQuery({ queryKey: ["colleagues"], queryFn: () => colleaguesApi.list(), retry: false });
  const contacts = useQuery({ queryKey: ["contacts"], queryFn: () => contactsApi.list(), retry: false });
  const term = q.trim().toLowerCase();
  const isEmail = EMAIL_RE.test(q.trim());

  const hits = useMemo(() => {
    if (!term) return [] as { email: string; name?: string; kind: "colleague" | "contact" }[];
    const out: { email: string; name?: string; kind: "colleague" | "contact" }[] = [];
    for (const c of colleagues.data ?? []) {
      if (c.email.toLowerCase().includes(term) || (c.name ?? "").toLowerCase().includes(term)) out.push({ email: c.email, name: c.name, kind: "colleague" });
    }
    for (const c of contacts.data ?? []) {
      const e = (c.email ?? "").toLowerCase();
      if (!e || out.some((o) => o.email.toLowerCase() === e)) continue;
      if (e.includes(term) || (c.name ?? "").toLowerCase().includes(term)) out.push({ email: c.email!, name: c.name, kind: "contact" });
    }
    return out.slice(0, 6);
  }, [term, colleagues.data, contacts.data]);

  const pick = (email: string) => { onAdd(email); setQ(""); setOpen(false); inputRef.current?.focus(); };

  return (
    <div className="invp share-people">
      <div className="invp-search">
        <IcSearch size={14} />
        <input ref={inputRef} value={q} autoFocus disabled={disabled} placeholder={t("files.addPeople")}
          onChange={(e) => { setQ(e.target.value); setOpen(true); }}
          onFocus={() => setOpen(true)}
          onBlur={() => setTimeout(() => setOpen(false), 150)}
          onKeyDown={(e) => { if (e.key === "Enter") { e.preventDefault(); if (hits[0]) pick(hits[0].email); else if (isEmail) pick(q.trim()); } }} />
      </div>
      {open && (hits.length > 0 || isEmail) && (
        <div className="invp-sugg">
          {hits.map((h) => (
            <button key={h.email} type="button" onMouseDown={(e) => { e.preventDefault(); pick(h.email); }}>
              <Avatar name={h.name} email={h.email} size={26} />
              <span className="cinfo"><b>{h.name || h.email}</b><i>{h.email}</i></span>
              <span className="src">{t(h.kind === "colleague" ? "invp.colleague" : "invp.contact")}</span>
            </button>
          ))}
          {isEmail && !hits.some((h) => h.email.toLowerCase() === q.trim().toLowerCase()) && (
            <button type="button" className="invp-ext" onMouseDown={(e) => { e.preventDefault(); pick(q.trim()); }}>
              <span className="av" style={{ background: hueFor(q.trim()) }}>@</span>
              <span className="cinfo"><b>{t("invp.inviteExternal")}</b><i>{q.trim()}</i></span>
            </button>
          )}
        </div>
      )}
    </div>
  );
}

// URL pública (SEM login) de um token de link: abre o ficheiro (OnlyOffice) na página pública /p/.
const publicLinkUrl = (token: string) => publicUrl(`/p/${token}`);

// Principal sentinela da partilha ao NÍVEL DA ORGANIZAÇÃO (tem de casar o backend). Um âmbito de
// acesso geral, não uma pessoa.
const ORG = "@org";
type Scope = "restricted" | "org" | "public";

// Compartilhamento estilo Google Drive: adicionar pessoas com papel + acesso por link.
export function ShareModal({ file, onClose }: { file: StorageFile; onClose: () => void }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const meQ = useQuery({ queryKey: ["me"], queryFn: () => backend.me(), retry: false });
  const sharesQ = useQuery({ queryKey: ["shares", file.id], queryFn: () => backend.shares(file.id), retry: false });
  const [role, setRole] = useState("leitor");
  const [groupId, setGroupId] = useState("");
  const refresh = () => qc.invalidateQueries({ queryKey: ["shares", file.id] });
  // Trocar a permissão de quem já tem acesso: é o mesmo pedido de partilhar (o servidor faz upsert
  // por ficheiro+pessoa), por isso não duplica nem dispara e-mail a quem já foi avisado.
  const setPerm = useMutation({
    mutationFn: (v: { principal: string; nivel: string }) => backend.shareFile(file.id, v.principal, v.nivel),
    onSuccess: () => { refresh(); toast(t("files.permChanged")); },
    onError: (e) => toast((e as Error).message),
  });

  // Equipas do utilizador: partilhar com o grupo inteiro (expande em e-mails no backend).
  const myGroupsQ = useQuery({ queryKey: ["my-groups"], queryFn: () => backend.myGroups(), retry: false });
  const myGroups = myGroupsQ.data ?? [];
  const shareGroup = useMutation({
    mutationFn: () => backend.shareWithGroup(file.id, groupId, role),
    onSuccess: (r) => { setGroupId(""); refresh(); toast(t("files.sharedWithTeam", { count: r.shared })); },
    onError: (e) => toast((e as Error).message),
  });

  const shares = sharesQ.data ?? [];
  // Partilha de organização = principal sentinela "@org"; não é uma pessoa, não entra na lista.
  const orgShare = shares.find((s) => s.principal === ORG);
  const people = shares.filter((s) => s.principal !== ORG && s.nivel !== "link");

  // Partilha com alguém de FORA: não há conta para notificar, por isso vai um link por e-mail. Se o
  // envio estiver desligado no servidor, dizemo-lo — dar por concluído o que não saiu é mentir.
  const add = useMutation({
    mutationFn: (email: string) => backend.shareFile(file.id, email, role),
    onSuccess: (r) => {
      refresh();
      if (r.external && !r.emailed) toast(t("files.sharedExternalNoEmail"));
      else if (r.external) toast(t("files.sharedExternal"));
      else toast(t("files.shared"));
    },
    onError: (e) => toast((e as Error).message),
  });
  const remove = useMutation({ mutationFn: (s: Share) => backend.removeShare(file.id, s.id), onSuccess: refresh });

  // Estado do link PÚBLICO (sem login): abre no OnlyOffice, com permissão + senha + expiração + descarga.
  const pubLinkQ = useQuery({ queryKey: ["file-link", file.id], queryFn: () => backend.getFileLink(file.id), retry: false });
  const pubToken = pubLinkQ.data?.token ?? "";
  const refreshPub = () => qc.invalidateQueries({ queryKey: ["file-link", file.id] });
  const refreshAll = () => { refresh(); refreshPub(); };
  const settings = useUserSettings(); // expiração por defeito do link (settings.files.linkExpiryDays)
  const [expDays, setExpDays] = useState(settings.files?.linkExpiryDays ?? 30);
  const [pw, setPw] = useState("");
  const [linkPerm, setLinkPerm] = useState<"view" | "edit">("view");
  const [allowDl, setAllowDl] = useState(true);
  // dirty = há alterações aos controlos do link ainda por guardar (só valem ao clicar em Aplicar).
  const [dirty, setDirty] = useState(false);
  useEffect(() => {
    const d = pubLinkQ.data;
    if (d?.token) { setLinkPerm(d.permission ?? "view"); setAllowDl(d.allow_download ?? true); setDirty(false); }
  }, [pubLinkQ.data]);
  const applyPub = (opts?: { expires_days?: number; password?: string; permission?: "view" | "edit"; allow_download?: boolean }) =>
    backend.fileLink(file.id, { expires_days: expDays, password: pw, permission: linkPerm, allow_download: allowDl, ...opts });
  const createPub = useMutation({ mutationFn: () => applyPub(), onSuccess: () => { refreshPub(); setPw(""); setDirty(false); toast(t("files.linkSaved")); }, onError: (e) => toast((e as Error).message) });

  // Âmbito do "acesso geral": um de cada vez (estilo Google). Escolher um limpa os outros.
  const scope: Scope = pubToken ? "public" : orgShare ? "org" : "restricted";
  const orgPerm: "view" | "edit" = orgShare?.nivel === "editor" ? "edit" : "view";
  const setScope = useMutation({
    mutationFn: async (next: Scope) => {
      if (next !== "public" && pubToken) await backend.revokeFileLink(file.id);
      if (next !== "org" && orgShare) await backend.removeShare(file.id, orgShare.id);
      if (next === "org" && !orgShare) await backend.shareFile(file.id, ORG, "leitor");
      if (next === "public" && !pubToken) await applyPub();
    },
    onSuccess: refreshAll,
    onError: (e) => toast((e as Error).message),
  });
  // Permissão da organização (ver/editar): recria o share com o novo nível.
  const setOrgPerm = useMutation({
    mutationFn: async (perm: "view" | "edit") => {
      if (orgShare) await backend.removeShare(file.id, orgShare.id);
      await backend.shareFile(file.id, ORG, perm === "edit" ? "editor" : "leitor");
    },
    onSuccess: refresh,
    onError: (e) => toast((e as Error).message),
  });

  return (
    <div className="tk-bg open" onMouseDown={onClose}>
      <div className="tk" onMouseDown={(e) => e.stopPropagation()} style={{ width: 500 }}>
        <header><span>{t("files.shareTitle", { name: file.nome })}</span><button className="ib" title={t("common.close")} onClick={onClose}>✕</button></header>
        <div className="body">
          <div className="share-add">
            <PeopleSearch disabled={add.isPending} onAdd={(email) => add.mutate(email)} />
            <select value={role} onChange={(e) => setRole(e.target.value)}>
              <option value="leitor">{t("files.roleViewer")}</option>
              <option value="editor">{t("files.roleEditor")}</option>
            </select>
          </div>
          <p className="share-hint">{t("files.addPeopleHint")}</p>

          {myGroups.length > 0 && (
            <div className="share-add">
              <select className="grow" value={groupId} onChange={(e) => setGroupId(e.target.value)}>
                <option value="">{t("files.shareWithTeamPlaceholder")}</option>
                {myGroups.map((g) => <option key={g.id} value={g.id}>{g.name}</option>)}
              </select>
              <button className="add" disabled={!groupId || shareGroup.isPending} onClick={() => shareGroup.mutate()}>{t("files.shareTeamBtn")}</button>
            </div>
          )}

          <p className="dv-sec" style={{ margin: "6px 0" }}>{t("files.peopleAccess")}</p>
          <div className="share-row">
            <Avatar name={t("files.you")} email={meQ.data?.email} size={30} />
            <div className="grow"><div className="who">{t("files.you")}</div></div>
            <span className="tx" style={{ color: "var(--faint)", fontSize: 12 }}>{t("files.owner")}</span>
          </div>
          {people.map((s) => (
            <div className="share-row" key={s.id}>
              <Avatar email={s.principal} name={s.principal} size={30} />
              <div className="grow"><div className="who">{s.principal}</div></div>
              {/* A permissão é EDITÁVEL: mudar de leitor para editor obrigava a remover e voltar a
                  partilhar, e quem o fizesse com alguém de fora mandava-lhe um segundo e-mail. */}
              <select className="share-perm" value={s.nivel === "editor" ? "editor" : "leitor"}
                disabled={setPerm.isPending}
                onChange={(e) => setPerm.mutate({ principal: s.principal, nivel: e.target.value })}>
                <option value="leitor">{t("files.roleViewer")}</option>
                <option value="editor">{t("files.roleEditor")}</option>
              </select>
              <button className="ib" title={t("files.removeShare")} onClick={() => remove.mutate(s)}><IcTrash size={15} /></button>
            </div>
          ))}

          {/* ACESSO GERAL — um só seletor (estilo Google): Restrito · Organização · Link público. */}
          <p className="dv-sec" style={{ margin: "16px 0 6px" }}>{t("files.generalAccess")}</p>
          <div className="share-general">
            <span className={"ic" + (scope === "restricted" ? "" : " on")}><IcLink size={20} /></span>
            <div className="grow">
              <select className="scope-sel" value={scope} disabled={setScope.isPending}
                onChange={(e) => setScope.mutate(e.target.value as Scope)}>
                <option value="restricted">{t("files.scopeRestricted")}</option>
                <option value="org">{t("files.scopeOrg")}</option>
                <option value="public">{t("files.scopePublic")}</option>
              </select>
              <div className="tx">{t(`files.scopeHint_${scope}`)}</div>
            </div>
            {scope === "org" && (
              <label className="field-sm">
                <span>{t("files.linkPermission")}</span>
                <select value={orgPerm} disabled={setOrgPerm.isPending} onChange={(e) => setOrgPerm.mutate(e.target.value as "view" | "edit")}>
                  <option value="view">{t("files.roleViewer")}</option>
                  <option value="edit">{t("files.roleEditor")}</option>
                </select>
              </label>
            )}
          </div>

          {scope === "org" && (
            <div className="link-box">
              <input readOnly value={publicUrl(`/mail/files/edit/${file.id}`)} onFocus={(e) => e.target.select()} />
              <button className="chipbtn" onClick={() => { navigator.clipboard?.writeText(publicUrl(`/mail/files/edit/${file.id}`)); toast(t("files.linkCopied")); }}>{t("files.copyLink")}</button>
            </div>
          )}

          {scope === "public" && (
            <>
              <div className="link-cfg">
                <label className="field-sm">
                  <span>{t("files.linkPermission")}</span>
                  <select value={linkPerm} onChange={(e) => { setLinkPerm(e.target.value as "view" | "edit"); setDirty(true); }}>
                    <option value="view">{t("files.roleViewer")}</option>
                    <option value="edit">{t("files.roleEditor")}</option>
                  </select>
                </label>
                <label className="field-sm">
                  <span>{t("files.linkExpiry")}</span>
                  <select value={expDays} onChange={(e) => { setExpDays(Number(e.target.value)); setDirty(true); }}>
                    <option value={0}>{t("files.linkNoExpiry")}</option>
                    <option value={1}>{t("files.link1d")}</option>
                    <option value={7}>{t("files.link7d")}</option>
                    <option value={30}>{t("files.link30d")}</option>
                    <option value={90}>{t("files.link90d")}</option>
                  </select>
                </label>
                <label className="field-sm">
                  <span>{t("files.linkPasswordLabel")}</span>
                  <input type="password" placeholder={pubLinkQ.data?.has_password ? t("files.linkPasswordSet") : t("files.linkPassword")} value={pw} onChange={(e) => { setPw(e.target.value); setDirty(true); }} />
                </label>
              </div>
              <label className="share-dl">
                <span className="switch"><input type="checkbox" checked={allowDl} onChange={(e) => { setAllowDl(e.target.checked); setDirty(true); }} /><span className="track" /></span>
                <span>{t("files.linkAllowDownload")}</span>
              </label>
              <div className="link-apply">
                <button className={"add grow" + (dirty ? "" : " ghost")} disabled={createPub.isPending || !dirty} onClick={() => createPub.mutate()}>{t("files.linkApply")}</button>
                <span className={"save-hint" + (dirty ? " dirty" : "")}>{dirty ? t("files.linkUnsaved") : t("files.linkSavedShort")}</span>
              </div>
              <div className="link-box">
                <input readOnly value={publicLinkUrl(pubToken)} onFocus={(e) => e.target.select()} />
                <button className="chipbtn" onClick={() => { navigator.clipboard?.writeText(publicLinkUrl(pubToken)); toast(t("files.linkCopied")); }}>{t("files.copyLink")}</button>
              </div>
            </>
          )}
        </div>
        <footer><button className="add" onClick={onClose}>{t("common.close")}</button></footer>
      </div>
    </div>
  );
}
