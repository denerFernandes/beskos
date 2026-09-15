import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { api, hasPerm, type Me, type Plan } from "../api";
import { Modal, toast } from "./ui";
import { SubscribeWizard } from "./SubscribeWizard";

// Per-user billing: usage vs. limits, plans with a per-user price (configurable currency),
// estimated total (price × max(users, minimum paid)). Choosing a plan opens the subscription
// wizard (billing data + VAT + payment).
export function BillingSection({ me, detail, onDetail }: {
  me: Me | undefined;
  /** "subscribe" opens the subscription wizard (lives in the URL: /mail/admin/billing/subscribe). */
  detail?: string;
  onDetail?: (id: string) => void;
}) {
  const { t, i18n } = useTranslation();
  const qc = useQueryClient();
  const canManage = hasPerm(me, "billing.manage");
  const usage = useQuery({ queryKey: ["usage"], queryFn: api.usage, retry: false });
  const billing = useQuery({ queryKey: ["billing"], queryFn: api.billing, retry: false });
  // Plan chosen in the table → the wizard opens already on that plan. The step lives in the URL so
  // a refresh (and the browser's "back" button) don't lose what was being done.
  const [wizardPlan, setWizardPlan] = useState("");
  const openSubscribe = (id: string) => { setWizardPlan(id); onDetail?.("subscribe"); };

  const refresh = () => { qc.invalidateQueries({ queryKey: ["usage"] }); qc.invalidateQueries({ queryKey: ["billing"] }); };
  // Subscription management (Stripe): cancel at the end of the period, resume, payment portal.
  const [cancelAsk, setCancelAsk] = useState(false);
  const cancelSub = useMutation({ mutationFn: () => api.billingCancel(), onSuccess: () => { refresh(); setCancelAsk(false); toast(t("billing.cancelDone")); }, onError: (e) => toast((e as Error).message) });
  const resumeSub = useMutation({ mutationFn: () => api.billingResume(), onSuccess: () => { refresh(); toast(t("billing.resumeDone")); }, onError: (e) => toast((e as Error).message) });
  const portal = useMutation({ mutationFn: () => api.billingPortal(), onSuccess: (r) => { location.href = r.url; }, onError: (e) => toast((e as Error).message) });

  const b = billing.data;
  const fmtDate = (unix: number) => new Date(unix * 1000).toLocaleDateString(i18n.language, { day: "2-digit", month: "short", year: "numeric" });
  const money = (cents: number, cur?: string) => new Intl.NumberFormat(i18n.language, { style: "currency", currency: cur || b?.currency || "BRL" }).format(cents / 100);
  const gb = (n: number) => `${(n / (1 << 30)).toFixed(1)} GB`;
  const seats = b ? Math.max(b.seats, b.min_paid_users) : 1;

  // Plan table row: one feature, value per plan (current plan's column highlighted).
  function Row({ label, val, strong }: { label: string; val: (p: Plan) => string; strong?: boolean }) {
    return (
      <tr className={strong ? "strong" : ""}>
        <td className="pt-feat">{label}</td>
        {(b?.plans ?? []).map((p) => <td key={p.id} className={p.id === b?.current_plan.id ? "cur" : ""}>{val(p)}</td>)}
      </tr>
    );
  }

  if (detail === "subscribe" && b) {
    return (
      <SubscribeWizard
        plans={b.plans} currentPlanId={b.current_plan.id} initialPlan={wizardPlan}
        seats={b.seats} minPaidUsers={b.min_paid_users}
        onDone={() => { refresh(); onDetail?.(""); toast(t("billing.planChanged")); }}
        onClose={() => onDetail?.("")} />
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
                            : canManage ? <button className="pnl-btn primary" onClick={() => openSubscribe(p.id)}>{t("billing.choose")}</button>
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
