import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { api, type DomainDetail, type DNSRecord, type DNSCheck, type DNSCheckResult } from "../api";
import { toast } from "./ui";
import { IcArrowLeft, IcChevron } from "./icons";

// Página do domínio (era um sidecar). Passou a página porque a informação não cabe numa gaveta:
// são ~18 registos com valores longos (DKIM, MTA-STS) mais o estado de cada um — na gaveta a tabela
// espremia-se ao ponto de a coluna "Copiar" sair em letras verticais.
//
// Três blocos, por esta ordem de utilidade: o que falta publicar, como publicar no teu fornecedor,
// e a zona de perigo.

// Propósitos que habilitam ENVIO vs RECEÇÃO. Agrupar por isto diz porque é que cada registo existe.
const SEND_PURPOSES = ["spf", "dkim", "dkim_ses", "dmarc", "mailfrom"];

// Fornecedores cobertos pelo guia. `import` marca os que aceitam ficheiro de zona BIND — nesses, a
// exportação resolve tudo de uma vez e é o caminho a recomendar primeiro.
const PROVIDERS = [
  { key: "cloudflare", importsZone: true },
  { key: "route53", importsZone: true },
  { key: "godaddy", importsZone: false },
  { key: "namecheap", importsZone: false },
  { key: "google", importsZone: false },
  { key: "other", importsZone: false },
];

export function DomainPage({ id, canManage, onBack }: { id: string; canManage: boolean; onBack: () => void }) {
  const { t, i18n } = useTranslation();
  const qc = useQueryClient();
  const q = useQuery<DomainDetail>({
    queryKey: ["domain", id], queryFn: () => api.getDomain(id),
    refetchInterval: (query) => (query.state.data && query.state.data.provision_state !== "active" ? 5000 : false),
  });
  const check = useQuery<DNSCheckResult>({ queryKey: ["dns-check", id], queryFn: () => api.dnsCheck(id), retry: false });
  const [confirmName, setConfirmName] = useState("");
  const [delErr, setDelErr] = useState("");
  const del = useMutation({
    mutationFn: () => api.deleteDomain(id),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ["domains"] }); toast(t("domains.deleted")); onBack(); },
    onError: (e) => setDelErr((e as Error).message),
  });

  const d = q.data;
  const records = d?.dns_records ?? [];
  const byKey = useMemo(() => {
    const m = new Map<string, DNSCheck>();
    for (const c of check.data?.records ?? []) m.set(`${c.type}|${c.host}`, c);
    return m;
  }, [check.data]);
  const sum = check.data?.summary;
  const receive = records.filter((r) => !SEND_PURPOSES.includes(r.purpose));
  const send = records.filter((r) => SEND_PURPOSES.includes(r.purpose));

  return (
    <>
      <div className="pnl-top">
        <button className="pnl-icon-btn" title={t("common.back")} onClick={onBack}><IcArrowLeft size={16} /></button>
        <div>
          <h1 className="mono">{d?.dominio ?? "…"}</h1>
          <div className="desc">{t("domains.dnsHint")}</div>
        </div>
      </div>

      <div className="pnl-content dom-page">
        {/* Resumo: quantos faltam e se o que falta é essencial. É a única linha que a maioria lê. */}
        <div className="pnl-card">
          <div className="pnl-card-b dom-summary">
            <StatePill state={d?.provision_state ?? ""} />
            <span className={"dns-sum" + (sum && sum.required_pending > 0 ? " warn" : sum ? " ok" : "")}>
              {sum ? t("domains.dnsSummary", { ok: sum.ok, total: sum.total }) : "…"}
              {sum && sum.required_pending > 0 && ` · ${t("domains.dnsRequiredPending", { n: sum.required_pending })}`}
            </span>
            {check.data?.checked_at && (
              <span className="desc">{t("domains.dnsCheckedAt", { when: new Date(check.data.checked_at).toLocaleTimeString(i18n.language) })}</span>
            )}
            <span className="grow" />
            <button className="pnl-btn" disabled={check.isFetching} onClick={() => void check.refetch()}>
              {check.isFetching ? "…" : t("domains.dnsRecheck")}
            </button>
            <button className="pnl-btn ghost" onClick={() => void api.dnsExport(id, "zone", d?.dominio ?? "").catch((e) => toast((e as Error).message))}>
              {t("domains.dnsExportZone")}
            </button>
            <button className="pnl-btn ghost" onClick={() => void api.dnsExport(id, "csv", d?.dominio ?? "").catch((e) => toast((e as Error).message))}>CSV</button>
          </div>
        </div>

        <ProviderGuide domain={d?.dominio ?? ""} onExport={() => void api.dnsExport(id, "zone", d?.dominio ?? "").catch((e) => toast((e as Error).message))} />

        <RecordsCard title={t("domains.dnsReceiveTitle")} records={receive} checks={byKey} />
        <RecordsCard title={t("domains.dnsSendTitle")} hint={t("domains.dnsSendHint")} records={send} checks={byKey} />

        {canManage && d && (
          <div className="pnl-card danger">
            <div className="pnl-card-h"><div><h3>{t("domains.dangerTitle")}</h3><div className="desc">{t("domains.deleteHint")}</div></div></div>
            <div className="pnl-card-b dom-danger">
              <input className="pnl-input" style={{ width: 260 }} placeholder={d.dominio} value={confirmName}
                onChange={(e) => { setConfirmName(e.target.value); setDelErr(""); }} />
              <button className="pnl-btn danger" disabled={confirmName !== d.dominio || del.isPending} onClick={() => del.mutate()}>
                {t("domains.deleteBtn")}
              </button>
              {delErr && <p className="pnl-err">{delErr}</p>}
            </div>
          </div>
        )}
      </div>
    </>
  );
}

function StatePill({ state }: { state: string }) {
  const { t } = useTranslation();
  const cls = state === "active" ? "ok" : state === "error" ? "warn" : state === "dns_pending" ? "warn" : "muted";
  return <span className={"pnl-pill " + cls}>{t(`domains.states.${state}`, { defaultValue: state })}</span>;
}

// Guia por fornecedor: passos concretos de onde clicar. Quem importa a zona (Cloudflare, Route 53)
// resolve tudo de uma vez — por isso esses aparecem primeiro e com o botão de exportar à mão.
function ProviderGuide({ domain, onExport }: { domain: string; onExport: () => void }) {
  const { t } = useTranslation();
  const [open, setOpen] = useState<string | null>("cloudflare");
  return (
    <div className="pnl-card">
      <div className="pnl-card-h">
        <div><h3>{t("domains.guideTitle")}</h3><div className="desc">{t("domains.guideHint")}</div></div>
      </div>
      <div className="pnl-card-b">
        {PROVIDERS.map((p) => {
          const on = open === p.key;
          const steps = t(`domains.guide.${p.key}.steps`, { returnObjects: true, domain }) as unknown;
          const list = Array.isArray(steps) ? (steps as string[]) : [];
          return (
            <div className={"guide-item" + (on ? " on" : "")} key={p.key}>
              <button className="guide-head" onClick={() => setOpen(on ? null : p.key)}>
                <IcChevron size={14} className={on ? "chev-open" : ""} />
                <b>{t(`domains.guide.${p.key}.name`)}</b>
                {p.importsZone && <span className="pnl-pill ok">{t("domains.guideImports")}</span>}
              </button>
              {on && (
                <div className="guide-body">
                  <ol>{list.map((step, i) => <li key={i}>{step}</li>)}</ol>
                  {p.importsZone && (
                    <button className="pnl-btn" onClick={onExport}>{t("domains.dnsExportZone")}</button>
                  )}
                </div>
              )}
            </div>
          );
        })}
        <p className="desc">{t("domains.guidePropagation")}</p>
      </div>
    </div>
  );
}

// Tabela de registos com estado. Numa página há largura para o valor respirar; o que for grande
// rola dentro da própria célula em vez de empurrar a tabela.
function RecordsCard({ title, hint, records, checks }: {
  title: string; hint?: string; records: DNSRecord[]; checks: Map<string, DNSCheck>;
}) {
  const { t } = useTranslation();
  const [copied, setCopied] = useState(-1);
  if (records.length === 0) return null;
  return (
    <div className="pnl-card">
      <div className="pnl-card-h"><div><h3>{title}</h3>{hint && <div className="desc">{hint}</div>}</div></div>
      <div className="pnl-card-b">
        <div className="dns-scroll">
        <table className="dns-table">
          <thead>
            <tr>
              <th>{t("domains.colStatus")}</th><th>{t("domains.colPurpose")}</th><th>{t("domains.colType")}</th>
              <th>{t("domains.colHost")}</th><th>{t("domains.colValue")}</th><th />
            </tr>
          </thead>
          <tbody>
            {records.map((r, i) => {
              const c = checks.get(`${r.type}|${r.host}`);
              const cls = !c ? "muted" : c.status === "ok" ? "ok" : c.required ? "err" : "warn";
              return (
                <tr key={i}>
                  {/* data-label alimenta o modo empilhado (ecrã estreito), onde o cabeçalho some. */}
                  <td data-label={t("domains.colStatus")}><span className={"dns-pill " + cls}>{c ? t(`domains.dnsStatus.${c.status}`) : "…"}</span></td>
                  <td data-label={t("domains.colPurpose")}><span className="pnl-pill muted">{t(`domains.purpose.${r.purpose}`, { defaultValue: r.purpose })}</span></td>
                  <td className="mono" data-label={t("domains.colType")}>{r.type}</td>
                  <td className="mono" data-label={t("domains.colHost")}>{r.host}</td>
                  <td data-label={t("domains.colValue")}>
                    {/* Quando diverge, as DUAS linhas etiquetadas: sem dizer qual é qual, ver dois
                        valores lado a lado não ajuda ninguém a decidir o que corrigir. */}
                    {c?.status === "mismatch" ? (
                      <div className="dns-diff">
                        <div className="dns-diff-row"><span className="dns-lbl">{t("domains.dnsExpected")}</span><span className="dns-val mono">{r.value}</span></div>
                        <div className="dns-diff-row bad"><span className="dns-lbl">{t("domains.dnsFound")}</span><span className="dns-val mono">{c.found.join(" · ")}</span></div>
                      </div>
                    ) : (
                      <div className="dns-val mono">{r.value}</div>
                    )}
                  </td>
                  <td className="dns-copy">
                    <button className="pnl-btn ghost" onClick={() => { void navigator.clipboard.writeText(r.value); setCopied(i); setTimeout(() => setCopied(-1), 1500); }}>
                      {copied === i ? t("domains.copied") : t("domains.copy")}
                    </button>
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
        </div>
      </div>
    </div>
  );
}
