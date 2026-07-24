import { useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate } from "react-router-dom";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { contactsApi, type Contact, type ContactKind } from "./backend";
import { ContactPage } from "./ContactPage";
import { ContactDrawer } from "./ContactDrawer";
import { GroupsSidebar } from "./GroupsSidebar";
import { initials, hueFor } from "./util";
import { toast } from "./toast";
import { triggerDownload } from "../apiBase";
import { IcPlus, IcUpload, IcDownload, IcGlobe } from "../panel/icons";

// CONTATOS: uma lista só — empresas e pessoas juntas, a pessoa mostrando a empresa ao lado.
// Ver é regido pelo GRUPO (partilhado com equipas); editar é do dono.
// URLs: /mail/contacts | /mail/contacts/<id>
export function ContactsView({ search, sub }: { search: string; sub?: string; sub2?: string }) {
  const nav = useNavigate();
  const goContacts = () => nav("/mail/contacts");
  const openContact = (id: string) => nav(`/mail/contacts/${id}`);

  if (sub) {
    // Detalhe: SEM a coluna vazia de 220px que ficava onde estava a barra de grupos — dava a
    // sensação de "menu sumiu". A ficha ocupa a largura toda da stage; o rail (menu) continua à
    // esquerda, no shell.
    return (
      <div className="cts cts-detail">
        <ContactPage id={sub} onBack={goContacts} onOpenContact={openContact} />
      </div>
    );
  }
  return <ContactList search={search} onOpen={openContact} />;
}

function ContactList({ search, onOpen }: { search: string; onOpen: (id: string) => void }) {
  const { t } = useTranslation();
  const [kind, setKind] = useState<"all" | ContactKind>("all");
  const [group, setGroup] = useState<string | null>(null);
  const [creating, setCreating] = useState(false);
  const [exportMenu, setExportMenu] = useState(false);
  const importInput = useRef<HTMLInputElement>(null);
  const qc = useQueryClient();
  const q = useQuery({ queryKey: ["contacts"], queryFn: () => contactsApi.list(), retry: false });

  const importMut = useMutation({
    mutationFn: (f: File) => contactsApi.importContacts(f),
    onSuccess: (r) => { qc.invalidateQueries({ queryKey: ["contacts"] }); toast(t("contacts.imported", { imported: r.imported, skipped: r.skipped })); },
    onError: (e) => toast((e as Error).message),
  });
  const enrichMut = useMutation({
    mutationFn: () => contactsApi.enrich(),
    onSuccess: (r) => { qc.invalidateQueries({ queryKey: ["contacts"] }); toast(t("crm.enriched", { linked: r.linked })); },
    onError: (e) => toast((e as Error).message),
  });

  const list = useMemo(() => {
    let cs = q.data ?? [];
    if (kind !== "all") cs = cs.filter((c) => c.kind === kind);
    if (group) cs = cs.filter((c) => c.group_ids.includes(group));
    const s = search.trim().toLowerCase();
    if (s) {
      cs = cs.filter((c) => c.name.toLowerCase().includes(s) || (c.email ?? "").toLowerCase().includes(s)
        || (c.company_name ?? c.company ?? "").toLowerCase().includes(s)
        || (c.domain ?? "").toLowerCase().includes(s)
        || (c.title ?? "").toLowerCase().includes(s));
    }
    return cs;
  }, [q.data, search, kind, group]);

  const countIn = (gid: string) => (q.data ?? []).filter((c) => c.group_ids.includes(gid)).length;

  return (
    // No telemóvel (max-md) empilha: a barra de grupos passa a topo e a lista ocupa a largura toda,
    // em vez de espremer o conteúdo com a coluna fixa de 220px. (Tailwind, ver tailwind.config.js.)
    <div className="cts max-md:grid-cols-1">
      <GroupsSidebar active={group} onSelect={setGroup} allCount={(q.data ?? []).length} count={countIn} />

      <section className="cts-main">
        <div className="cts-head">
          <h2>{t("contacts.title")}</h2>
          <div className="grow" />
          <button className="chipbtn" disabled={enrichMut.isPending} onClick={() => enrichMut.mutate()} title={t("crm.enrichHint")}><IcGlobe size={15} /> {t("crm.enrich")}</button>
          <button className="chipbtn" onClick={() => importInput.current?.click()}><IcUpload size={15} /> {t("contacts.import")}</button>
          <input ref={importInput} type="file" accept=".vcf,.vcard,.csv" hidden onChange={(e) => { const f = e.target.files?.[0]; if (f) importMut.mutate(f); e.target.value = ""; }} />
          <div className="new-wrap">
            <button className="chipbtn" onClick={() => setExportMenu((v) => !v)}><IcDownload size={15} /> {t("contacts.export")}</button>
            {exportMenu && (
              <>
                <div className="menu-backdrop" onClick={() => setExportMenu(false)} />
                <div className="new-menu">
                  <button onClick={() => { triggerDownload(contactsApi.exportUrl("vcard"), "contacts.vcf"); setExportMenu(false); }}>vCard (.vcf)</button>
                  <button onClick={() => { triggerDownload(contactsApi.exportUrl("csv"), "contacts.csv"); setExportMenu(false); }}>CSV (.csv)</button>
                </div>
              </>
            )}
          </div>
          <button className="chipbtn" onClick={() => setCreating(true)}><IcPlus size={15} /> {t("contacts.add")}</button>
        </div>

        {(
          <>
            <div className="crm-filters">
              {(["all", "company", "person"] as const).map((k) => (
                <button key={k} className={"fchip" + (kind === k ? " on" : "")} onClick={() => setKind(k)}>
                  {k === "all" ? t("contacts.all") : t(`contacts.kind_${k}`)}
                </button>
              ))}
            </div>

            {q.isLoading ? <div className="loading">…</div>
              : list.length === 0 ? <div className="empty">{t("contacts.empty")}</div>
              : (
                <div className="cts-grid">
                  {list.map((c) => <Card key={c.id} c={c} onOpen={() => onOpen(c.id)} />)}
                </div>
              )}
          </>
        )}

        {creating && <ContactDrawer contact={null} onClose={() => setCreating(false)} onOpen={onOpen} />}
      </section>
    </div>
  );
}

// Empresa e pessoa na mesma lista: o quadrado distingue a empresa, e a pessoa mostra onde trabalha.
// Cada card mostra a label do tipo (Empresa/Pessoa) — pedido: essa label em TODOS.
function Card({ c, onOpen }: { c: Contact; onOpen: () => void }) {
  const { t } = useTranslation();
  const sub = c.kind === "company" ? c.domain : [c.title, c.company_name ?? c.company].filter(Boolean).join(" · ");
  return (
    <div className="ctc" onClick={onOpen}>
      <div className={"av" + (c.kind === "company" ? " sq" : "")} style={{ background: hueFor(c.domain || c.email || c.name) }}>{initials(c.name)}</div>
      <div className="info">
        <div className="nm">{c.name}<span className={"ctc-kind " + c.kind}>{t(c.kind === "company" ? "contacts.kind_company" : "contacts.kind_person")}</span></div>
        {c.email && <div className="em">{c.email}</div>}
        {sub && <div className="em" style={{ fontFamily: "inherit" }}>{sub}</div>}
        {c.tags?.length > 0 && <div className="tags">{c.tags.map((tg) => <span className="tag-pill" key={tg}>{tg}</span>)}</div>}
      </div>
    </div>
  );
}
