import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { api, hasPerm, type Me, type DomainDetail, type DNSRecord } from "../api";
import { Modal, Drawer, toast } from "./ui";
import { IcPlus } from "./icons";

export function DomainsSection({ me }: { me: Me | undefined }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const canManage = hasPerm(me, "domains.manage");
  const list = useQuery({ queryKey: ["domains"], queryFn: api.domains, retry: false });
  const [adding, setAdding] = useState(false);
  const [name, setName] = useState("");
  const [err, setErr] = useState("");
  const [openId, setOpenId] = useState<string | null>(null);

  const add = useMutation({
    mutationFn: () => api.createDomain(name),
    onSuccess: (d) => { setAdding(false); setName(""); setErr(""); qc.invalidateQueries({ queryKey: ["domains"] }); toast(t("domains.added")); setOpenId(d.id); },
    onError: (e) => setErr((e as Error).message),
  });

  return (
    <>
      <div className="pnl-top">
        <div><h1>{t("panel.domains")}</h1><div className="desc">{t("domainsSection.desc")}</div></div>
        <div className="grow" />
        {canManage && <button className="pnl-btn primary" onClick={() => setAdding(true)}><IcPlus size={16} />{t("domains.add")}</button>}
      </div>
      <div className="pnl-content">
        <div className="pnl-card"><div className="pnl-card-b">
          {(list.data?.length ?? 0) === 0 ? <div className="pnl-empty">{t("domains.empty")}</div>
            : list.data!.map((dm) => (
              <div className="pnl-row" key={dm.id}>
                <div className="grow"><div className="mono">{dm.dominio}</div></div>
                <StatePill state={dm.provision_state} />
                <button className="pnl-btn ghost" onClick={() => setOpenId(dm.id)}>{t("domainsSection.details")}</button>
              </div>
            ))}
        </div></div>
      </div>

      {adding && (
        <Modal title={t("domains.add")} onClose={() => setAdding(false)}
          footer={<>
            <button className="pnl-btn ghost" onClick={() => setAdding(false)}>{t("common.cancel")}</button>
            <button className="pnl-btn primary" disabled={add.isPending} onClick={() => add.mutate()}>{t("domains.add")}</button>
          </>}>
          <div className="pnl-field"><label>{t("panel.domains")}</label>
            <input className="pnl-input" placeholder={t("domains.addPlaceholder")} value={name} onChange={(e) => setName(e.target.value)} autoFocus /></div>
          {err && <p className="pnl-err">{err}</p>}
        </Modal>
      )}

      {openId && <DomainDrawer id={openId} canManage={canManage} onClose={() => setOpenId(null)} />}
    </>
  );
}

function StatePill({ state }: { state: string }) {
  const { t } = useTranslation();
  const cls = state === "active" ? "ok" : state === "error" ? "warn" : state === "dns_pending" ? "warn" : "muted";
  return <span className={"pnl-pill " + cls}>{t(`domains.states.${state}`, { defaultValue: state })}</span>;
}

// Propósitos que habilitam ENVIO (SPF/DKIM/DKIM do relay/DMARC) vs RECEÇÃO (MX). A UI agrupa por
// isto e deixa claro que TODOS os registos têm de estar publicados no DNS do cliente.
const SEND_PURPOSES = ["spf", "dkim", "dkim_ses", "dmarc", "mailfrom"];

// Drawer (sidecar) com estado + registros DNS agrupados + zona de perigo. Faz polling enquanto
// não está ativo.
function DomainDrawer({ id, onClose, canManage }: { id: string; onClose: () => void; canManage: boolean }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const q = useQuery<DomainDetail>({
    queryKey: ["domain", id], queryFn: () => api.getDomain(id),
    refetchInterval: (query) => (query.state.data && query.state.data.provision_state !== "active" ? 3000 : false),
  });
  const d = q.data;
  const [confirmName, setConfirmName] = useState("");
  const [delErr, setDelErr] = useState("");
  const del = useMutation({
    mutationFn: () => api.deleteDomain(id),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ["domains"] }); toast(t("domains.deleted")); onClose(); },
    onError: (e) => setDelErr((e as Error).message),
  });

  const records = d?.dns_records ?? []; // defensivo: domínio recém-criado pode vir sem registos
  const receive = records.filter((r) => !SEND_PURPOSES.includes(r.purpose));
  const send = records.filter((r) => SEND_PURPOSES.includes(r.purpose));

  return (
    <Drawer title={d?.dominio ?? "…"} onClose={onClose}>
      {!d ? <div className="pnl-empty">…</div> : (
        <>
          <p style={{ margin: "0 0 14px" }}><StatePill state={d.provision_state} /></p>
          {records.length > 0 && (
            <>
              <h4 style={{ margin: "0 0 4px", fontSize: 13 }}>{t("domains.dnsTitle")}</h4>
              <p className="desc" style={{ marginTop: 0 }}>{t("domains.dnsHint")}</p>
              <RecordsTable title={t("domains.dnsReceiveTitle")} records={receive} />
              <RecordsTable title={t("domains.dnsSendTitle")} hint={t("domains.dnsSendHint")} records={send} />
            </>
          )}

          {canManage && (
            <div style={{ marginTop: 24, borderTop: "1px solid var(--border)", paddingTop: 14 }}>
              <h4 style={{ margin: "0 0 4px", fontSize: 13, color: "var(--danger, #b42318)" }}>{t("domains.dangerTitle")}</h4>
              <p className="desc" style={{ marginTop: 0 }}>{t("domains.deleteHint")}</p>
              <div className="pnl-field">
                <input className="pnl-input" placeholder={d.dominio} value={confirmName}
                  onChange={(e) => { setConfirmName(e.target.value); setDelErr(""); }} />
              </div>
              <button className="pnl-btn" style={{ color: "var(--danger, #b42318)" }}
                disabled={confirmName !== d.dominio || del.isPending}
                onClick={() => del.mutate()}>
                {t("domains.deleteBtn")}
              </button>
              {delErr && <p className="pnl-err">{delErr}</p>}
            </div>
          )}
        </>
      )}
    </Drawer>
  );
}

// RecordsTable: um grupo de registos DNS com selo do propósito e botão de copiar o valor.
function RecordsTable({ title, hint, records }: { title: string; hint?: string; records: DNSRecord[] }) {
  const { t } = useTranslation();
  const [copied, setCopied] = useState(-1);
  if (records.length === 0) return null;
  return (
    <>
      <h5 style={{ margin: "14px 0 2px", fontSize: 12.5 }}>{title}</h5>
      {hint && <p className="desc" style={{ marginTop: 0 }}>{hint}</p>}
      <div style={{ overflowX: "auto" }}>
        <table style={{ width: "100%", fontSize: 12, borderCollapse: "collapse" }}>
          <thead><tr><Th>{t("domains.colPurpose")}</Th><Th>{t("domains.colType")}</Th><Th>{t("domains.colHost")}</Th><Th>{t("domains.colValue")}</Th><Th>{""}</Th></tr></thead>
          <tbody>{records.map((r, i) => (
            <tr key={i}>
              <Td><span className="pnl-pill muted">{t(`domains.purpose.${r.purpose}`, { defaultValue: r.purpose })}</span></Td>
              <Td mono>{r.type}</Td><Td mono>{r.host}</Td><Td mono>{r.value}</Td>
              <Td>
                <button className="pnl-btn ghost" style={{ fontSize: 11, padding: "2px 8px" }}
                  onClick={() => { void navigator.clipboard.writeText(r.value); setCopied(i); setTimeout(() => setCopied(-1), 1500); }}>
                  {copied === i ? t("domains.copied") : t("domains.copy")}
                </button>
              </Td>
            </tr>
          ))}</tbody>
        </table>
      </div>
    </>
  );
}
const Th = ({ children }: { children: React.ReactNode }) => <th style={{ textAlign: "left", padding: "6px 8px", color: "var(--faint)", borderBottom: "1px solid var(--border)" }}>{children}</th>;
const Td = ({ children, mono }: { children: React.ReactNode; mono?: boolean }) => <td style={{ padding: "6px 8px", borderBottom: "1px solid var(--border)", fontFamily: mono ? "var(--mono)" : undefined, wordBreak: "break-all" }}>{children}</td>;
