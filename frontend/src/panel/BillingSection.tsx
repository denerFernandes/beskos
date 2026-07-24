import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { api, hasPerm, type Me, type Plan } from "../api";
import { Modal, toast } from "./ui";

// Billing por usuário: uso vs. limites, planos com preço por usuário (moeda configurável),
// total estimado (preço × max(usuários, mínimo pago)), upgrade com modal de confirmação.
export function BillingSection({ me }: { me: Me | undefined }) {
  const { t, i18n } = useTranslation();
  const qc = useQueryClient();
  const canManage = hasPerm(me, "billing.manage");
  const usage = useQuery({ queryKey: ["usage"], queryFn: api.usage, retry: false });
  const billing = useQuery({ queryKey: ["billing"], queryFn: api.billing, retry: false });
  const [confirm, setConfirm] = useState<Plan | null>(null);

  const refresh = () => { qc.invalidateQueries({ queryKey: ["usage"] }); qc.invalidateQueries({ queryKey: ["billing"] }); };
  const checkout = useMutation({
    mutationFn: (plan: string) => api.checkout(plan),
    onSuccess: (r) => {
      if (r.url) { location.href = r.url; return; }
      refresh(); setConfirm(null); toast(t("billing.planChanged"));
    },
    onError: (e) => toast((e as Error).message),
  });
  // Gestão da assinatura (Stripe): cancelar no fim do período, retomar, portal de pagamento.
  const [cancelAsk, setCancelAsk] = useState(false);
  const cancelSub = useMutation({ mutationFn: () => api.billingCancel(), onSuccess: () => { refresh(); setCancelAsk(false); toast(t("billing.cancelDone")); }, onError: (e) => toast((e as Error).message) });
  const resumeSub = useMutation({ mutationFn: () => api.billingResume(), onSuccess: () => { refresh(); toast(t("billing.resumeDone")); }, onError: (e) => toast((e as Error).message) });
  const portal = useMutation({ mutationFn: () => api.billingPortal(), onSuccess: (r) => { location.href = r.url; }, onError: (e) => toast((e as Error).message) });

  const b = billing.data;
  const fmtDate = (unix: number) => new Date(unix * 1000).toLocaleDateString(i18n.language, { day: "2-digit", month: "short", year: "numeric" });
  const money = (cents: number, cur?: string) => new Intl.NumberFormat(i18n.language, { style: "currency", currency: cur || b?.currency || "BRL" }).format(cents / 100);
  const gb = (n: number) => `${(n / (1 << 30)).toFixed(1)} GB`;
  const seats = b ? Math.max(b.seats, b.min_paid_users) : 1;

  // Linha da tabela de planos: um recurso, valor por plano (coluna do plano atual destacada).
  function Row({ label, val, strong }: { label: string; val: (p: Plan) => string; strong?: boolean }) {
    return (
      <tr className={strong ? "strong" : ""}>
        <td className="pt-feat">{label}</td>
        {(b?.plans ?? []).map((p) => <td key={p.id} className={p.id === b?.current_plan.id ? "cur" : ""}>{val(p)}</td>)}
      </tr>
    );
  }

  return (
    <>
      <div className="pnl-top"><div><h1>{t("panel.billing")}</h1><div className="desc">{t("billingSection.desc", { min: b?.min_paid_users ?? 1 })}</div></div></div>
      <div className="pnl-content">
        {usage.data && (
          <div className="pnl-card"><div className="pnl-card-h"><h3>{t("billing.currentPlan")}: {usage.data.plan.name}</h3>
            {b?.subscription && <span className={"pnl-pill " + (b.subscription.status === "active" && !b.subscription.cancel_at_period_end ? "ok" : "warn")}>
              {b.subscription.cancel_at_period_end ? t("billing.cancelsAt", { date: fmtDate(b.subscription.current_period_end) }) : t("billing.status_" + b.subscription.status, { defaultValue: b.subscription.status })}
            </span>}
          </div><div className="pnl-card-b">
            <Bar label={t("billing.mailboxes")} used={usage.data.usage.mailboxes} max={usage.data.limits.max_mailboxes} fmt={String} />
            <Bar label={t("billing.storage")} used={usage.data.usage.storage_bytes} max={usage.data.limits.max_storage_bytes} fmt={gb} />
            <Bar label={t("billing.sendsToday")} used={usage.data.usage.mail_sent_today} max={usage.data.limits.max_daily_send} fmt={String} />
            {canManage && b?.subscription?.manageable && (
              <div style={{ display: "flex", gap: 10, flexWrap: "wrap", marginTop: 6 }}>
                <button className="pnl-btn" disabled={portal.isPending} onClick={() => portal.mutate()}>{t("billing.paymentMethod")}</button>
                {b.subscription.cancel_at_period_end
                  ? <button className="pnl-btn" disabled={resumeSub.isPending} onClick={() => resumeSub.mutate()}>{t("billing.resume")}</button>
                  : <button className="pnl-btn danger" onClick={() => setCancelAsk(true)}>{t("billing.cancelSub")}</button>}
              </div>
            )}
          </div></div>
        )}

        {b && (
          <div className="pnl-card" style={{ marginTop: 16 }}>
            <div className="pnl-card-h"><h3>{t("billing.plansTable")}</h3>{!canManage && <span className="desc">{t("billing.noPerm")}</span>}</div>
            <div className="plans-scroll">
              <table className="plans-table">
                <thead>
                  <tr>
                    <th />
                    {b.plans.map((p) => {
                      const current = p.id === b.current_plan.id;
                      return (
                        <th key={p.id} className={current ? "cur" : ""}>
                          <div className="pt-name">{p.name}</div>
                          <div className="pt-price">{p.price_cents_per_user === 0 ? t("billing.free") : money(p.price_cents_per_user, p.currency)}</div>
                          {p.price_cents_per_user > 0 && <div className="pt-per">{t("billing.perUserMonth")}</div>}
                        </th>
                      );
                    })}
                  </tr>
                </thead>
                <tbody>
                  <Row label={t("billing.ftUsers")} val={(p) => p.mailbox_range} />
                  <Row label={t("billing.storage")} val={(p) => gb(p.max_storage_bytes)} />
                  <Row label={t("billing.ftDailySend")} val={(p) => p.max_daily_send > 0 ? String(p.max_daily_send) : "—"} />
                  <Row label={t("billing.ftApps")} val={(p) => (p.apps_enabled?.length ?? 0) === 0 ? t("billing.ftAllApps") : p.apps_enabled.map((a) => t("webmail.apps." + (a === "cal" ? "calendar" : a === "kanban" ? "board" : a), { defaultValue: a })).join(", ")} />
                  <Row label={t("billing.ftAI")} val={(p) => p.ai_tokens_per_window > 0 ? t("billing.ftAITokens", { n: Math.round(p.ai_tokens_per_window / 1000) }) : t("billing.ftDefault")} />
                  <Row label={t("billing.ftMeetPart")} val={(p) => p.max_meet_participants > 0 ? String(p.max_meet_participants) : "—"} />
                  <Row label={t("billing.ftMeetMin")} val={(p) => p.meet_max_minutes > 0 ? `${p.meet_max_minutes} min` : t("billing.ftUnlimited")} />
                  <Row label={t("billing.ftRecording")} val={(p) => p.recording === "none" || !p.recording ? "—" : p.recording === "offline_priority" ? t("billing.ftRecPriority") : t("billing.ftRecStandard")} />
                  <Row label={t("billing.ftRecDays")} val={(p) => p.recording_retention_days > 0 ? t("billing.ftDays", { n: p.recording_retention_days }) : "—"} />
                  <Row label={t("billing.ftOffice")} val={(p) => p.onlyoffice_edit ? "✓" : "—"} />
                  <Row label={t("billing.ftWikiPublic")} val={(p) => p.wiki_public_links ? "✓" : "—"} />
                  <Row label={t("billing.ftTotal")} val={(p) => p.price_cents_per_user > 0 ? `≈ ${money(p.price_cents_per_user * Math.max(seats, p.min_users || 1), p.currency)}${t("billing.perMonth")}` : t("billing.free")} strong />
                  <tr className="pt-actions">
                    <td />
                    {b.plans.map((p) => {
                      const current = p.id === b.current_plan.id;
                      return (
                        <td key={p.id} className={current ? "cur" : ""}>
                          {current ? <span className="pnl-pill ok">{t("billing.current")}</span>
                            : canManage ? <button className="pnl-btn primary" onClick={() => setConfirm(p)}>{t("billing.choose")}</button>
                            : <span className="desc">—</span>}
                        </td>
                      );
                    })}
                  </tr>
                </tbody>
              </table>
            </div>
          </div>
        )}
      </div>

      {cancelAsk && (
        <Modal title={t("billing.cancelTitle")} onClose={() => setCancelAsk(false)}
          footer={<><button className="pnl-btn ghost" onClick={() => setCancelAsk(false)}>{t("common.cancel")}</button>
            <button className="pnl-btn danger" disabled={cancelSub.isPending} onClick={() => cancelSub.mutate()}>{t("billing.cancelConfirm")}</button></>}>
          <p>{b?.subscription ? t("billing.cancelBody", { date: fmtDate(b.subscription.current_period_end) }) : t("billing.cancelBodyManual")}</p>
        </Modal>
      )}

      {confirm && (() => {
        // Seats FATURADOS no plano escolhido: nunca menos que o mínimo do plano; acima do máximo,
        // o plano não comporta a organização (o backend recusaria — avisa antes).
        const users = b?.seats ?? 1;
        const billed = Math.max(users, b?.min_paid_users ?? 1, confirm.min_users || 1);
        const tooSmall = confirm.max_mailboxes > 0 && users > confirm.max_mailboxes;
        return (
          <Modal title={t("billing.confirmTitle")} onClose={() => setConfirm(null)}
            footer={<><button className="pnl-btn ghost" onClick={() => setConfirm(null)}>{t("common.cancel")}</button>
              <button className="pnl-btn primary" disabled={checkout.isPending || tooSmall} onClick={() => checkout.mutate(confirm.id)}>{t("billing.confirm")}</button></>}>
            {tooSmall ? (
              <p className="pnl-err">{t("billing.planTooSmall", { plan: confirm.name, max: confirm.max_mailboxes, users })}</p>
            ) : (
              <>
                <p>{billed > users
                  ? t("billing.confirmBodyMin", { plan: confirm.name, price: money(confirm.price_cents_per_user, confirm.currency), users, min: billed })
                  : t("billing.confirmBody", { plan: confirm.name, price: money(confirm.price_cents_per_user, confirm.currency), seats: billed })}</p>
                {confirm.price_cents_per_user > 0 && <p style={{ fontWeight: 600 }}>≈ {money(confirm.price_cents_per_user * billed, confirm.currency)}{t("billing.perMonth")}</p>}
              </>
            )}
          </Modal>
        );
      })()}
    </>
  );
}

function Bar({ label, used, max, fmt }: { label: string; used: number; max: number; fmt: (v: number) => string }) {
  const pct = max > 0 ? Math.min(100, Math.round((used / max) * 100)) : 0;
  return (
    <div style={{ marginBottom: 12 }}>
      <div style={{ display: "flex", justifyContent: "space-between", fontSize: 13, marginBottom: 5 }}>
        <span>{label}</span><span className="desc">{fmt(used)} / {fmt(max)}</span>
      </div>
      <div className={"pnl-bar" + (pct >= 90 ? " over" : "")}><i style={{ width: `${pct}%` }} /></div>
    </div>
  );
}
