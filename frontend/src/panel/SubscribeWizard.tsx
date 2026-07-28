import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { useQuery, useMutation } from "@tanstack/react-query";
import { api, type BillingCycle, type BillingProfile, type Plan } from "../api";
import { EU_COUNTRIES, COUNTRIES } from "./countries";
import { toast } from "./ui";
import { IcCheck, IcArrowLeft, IcCard, IcUsers, IcGlobe, IcMail } from "./icons";

// Assinatura passo a passo: plano e periodicidade → quem é o cliente → morada e NIF → responsável
// financeiro → resumo com IVA. Os dados são NOSSOS (a fatura é obrigação nossa) e seguem para a
// Stripe como cliente, de modo que o checkout só peça o cartão.
//
// O IVA mostrado no resumo vem do backend (mesma conta que vai para a fatura). Repetir a regra em
// JavaScript era garantir que um dia o total mostrado e o cobrado deixavam de bater certo.

type Step = 0 | 1 | 2 | 3 | 4;
const STEPS: Step[] = [0, 1, 2, 3, 4];

export function SubscribeWizard({ plans, currentPlanId, initialPlan, seats, minPaidUsers, onDone, onClose }: {
  plans: Plan[];
  currentPlanId?: string;
  initialPlan?: string;
  seats: number;
  minPaidUsers: number;
  onDone: () => void;
  onClose: () => void;
}) {
  const { t, i18n } = useTranslation();
  const [step, setStep] = useState<Step>(0);
  const [planId, setPlanId] = useState(initialPlan || "");
  const [cycle, setCycle] = useState<BillingCycle>("month");
  const [form, setForm] = useState<BillingProfile>({ kind: "company" });
  const [err, setErr] = useState("");

  // Perfil já guardado: quem volta para trocar de plano não reescreve a morada toda.
  const saved = useQuery({ queryKey: ["billing-profile"], queryFn: api.billingProfile, retry: false });
  useEffect(() => {
    if (saved.data) setForm((f) => ({ ...f, ...saved.data }));
  }, [saved.data]);

  const plan = useMemo(() => plans.find((p) => p.id === planId), [plans, planId]);
  const money = (cents: number, cur?: string) =>
    new Intl.NumberFormat(i18n.language, { style: "currency", currency: cur || plan?.currency || "EUR" }).format(cents / 100);

  // Simulação (preço × utilizadores + IVA): recalcula ao mudar plano, ciclo, país ou NIF.
  const quote = useQuery({
    queryKey: ["billing-quote", planId, cycle, form.kind, form.country, form.tax_id],
    queryFn: () => api.billingQuote({ plan: planId, cycle, kind: form.kind, country: form.country ?? "", tax_id: form.tax_id ?? "" }),
    enabled: !!planId && step >= 1,
    retry: false,
  });

  const save = useMutation({ mutationFn: () => api.saveBillingProfile(form) });
  const checkout = useMutation({
    mutationFn: () => api.checkout(planId, cycle),
    onSuccess: (r) => { if (r.url) { location.href = r.url; return; } onDone(); },
    onError: (e) => setErr((e as Error).message),
  });

  const isEU = EU_COUNTRIES.includes((form.country ?? "").toUpperCase());
  const isCompany = form.kind === "company";
  const set = (patch: Partial<BillingProfile>) => { setErr(""); setForm((f) => ({ ...f, ...patch })); };

  // Cada passo diz o que lhe falta; sem isto o "Continuar" avança e o erro só aparece no fim.
  const stepError = (s: Step): string => {
    if (s === 0) return planId ? "" : t("subscribe.errPlan");
    if (s === 1) return (form.country ?? "").length === 2 ? "" : t("subscribe.errCountry");
    if (s === 2) {
      if (!(form.legal_name ?? "").trim()) return isCompany ? t("subscribe.errLegalName") : t("subscribe.errName");
      if (!(form.address_line1 ?? "").trim() || !(form.city ?? "").trim()) return t("subscribe.errAddress");
      if (isCompany && isEU && (form.country ?? "").toUpperCase() === "PL" && !(form.tax_id ?? "").trim()) return t("subscribe.errTaxIdPL");
      return "";
    }
    if (s === 3) return (form.invoice_email ?? "").includes("@") ? "" : t("subscribe.errInvoiceEmail");
    return "";
  };

  const next = async () => {
    const e = stepError(step);
    if (e) { setErr(e); return; }
    setErr("");
    // Antes do resumo, grava o perfil: é o backend que valida o NIF a sério (e o resumo já lê daqui).
    if (step === 3) {
      try { await save.mutateAsync(); }
      catch (ex) { setErr((ex as Error).message); return; }
    }
    setStep((s) => Math.min(4, s + 1) as Step);
  };

  const q = quote.data?.quote;
  const billed = q?.seats ?? Math.max(seats, minPaidUsers, plan?.min_users || 1);

  return (
    <div className="sub-wrap">
      <div className="sub-head">
        <button className="pnl-btn ghost" onClick={onClose}><IcArrowLeft size={16} /> {t("subscribe.back")}</button>
        <div className="sub-title">
          <h1>{t("subscribe.title")}</h1>
          <p className="desc">{t("subscribe.subtitle")}</p>
        </div>
      </div>

      {/* Passos: o percorrido fica marcado e é clicável para trás (rever sem perder o preenchido). */}
      <ol className="sub-steps">
        {STEPS.map((s) => (
          <li key={s} className={s === step ? "on" : s < step ? "done" : ""}>
            <button disabled={s > step} onClick={() => setStep(s)}>
              <span className="n">{s < step ? <IcCheck size={13} /> : s + 1}</span>
              <span className="lb">{t(`subscribe.step${s}`)}</span>
            </button>
          </li>
        ))}
      </ol>

      <div className="sub-body">
        <div className="sub-main">
          {step === 0 && (
            <section className="sub-card">
              <h2><IcCard size={17} /> {t("subscribe.step0")}</h2>
              <div className="sub-cycle">
                <button className={cycle === "month" ? "on" : ""} onClick={() => setCycle("month")}>{t("subscribe.monthly")}</button>
                <button className={cycle === "year" ? "on" : ""} onClick={() => setCycle("year")}>
                  {t("subscribe.yearly")}<em>{t("subscribe.yearlyOff")}</em>
                </button>
              </div>
              <div className="sub-plans">
                {plans.filter((p) => p.price_cents_per_user > 0).map((p) => {
                  const unit = cycle === "year" ? p.price_cents_per_user * 10 : p.price_cents_per_user;
                  const tooSmall = p.max_mailboxes > 0 && seats > p.max_mailboxes;
                  return (
                    <button key={p.id} className={"sub-plan" + (planId === p.id ? " on" : "") + (tooSmall ? " off" : "")}
                      disabled={tooSmall} onClick={() => { setPlanId(p.id); setErr(""); }}>
                      <span className="pn">{p.name}{p.id === currentPlanId && <em className="cur">{t("billing.current")}</em>}</span>
                      <span className="pp">{money(unit, p.currency)}</span>
                      <span className="pu">{cycle === "year" ? t("subscribe.perUserYear") : t("subscribe.perUserMonth")}</span>
                      <span className="pd">{p.mailbox_range} · {t("subscribe.storageGb", { n: Math.round(p.max_storage_bytes / (1 << 30)) })}</span>
                      {tooSmall && <span className="pd err">{t("billing.planTooSmall", { plan: p.name, max: p.max_mailboxes, users: seats })}</span>}
                    </button>
                  );
                })}
              </div>
            </section>
          )}

          {step === 1 && (
            <section className="sub-card">
              <h2><IcUsers size={17} /> {t("subscribe.step1")}</h2>
              <div className="sub-kind">
                <button className={isCompany ? "on" : ""} onClick={() => set({ kind: "company" })}>
                  <b>{t("subscribe.company")}</b><span>{t("subscribe.companyHint")}</span>
                </button>
                <button className={!isCompany ? "on" : ""} onClick={() => set({ kind: "individual" })}>
                  <b>{t("subscribe.individual")}</b><span>{t("subscribe.individualHint")}</span>
                </button>
              </div>
              <label className="sub-fld">
                <span><IcGlobe size={14} /> {t("subscribe.country")}</span>
                <select value={form.country ?? ""} onChange={(e) => set({ country: e.target.value })}>
                  <option value="">{t("subscribe.choose")}</option>
                  {COUNTRIES.map((c) => <option key={c} value={c}>{countryName(c, i18n.language)}</option>)}
                </select>
              </label>
              {isEU && <p className="sub-note">{t("subscribe.euNote")}</p>}
            </section>
          )}

          {step === 2 && (
            <section className="sub-card">
              <h2><IcCard size={17} /> {t("subscribe.step2")}</h2>
              <label className="sub-fld">
                <span>{isCompany ? t("subscribe.legalName") : t("subscribe.fullName")}</span>
                <input value={form.legal_name ?? ""} onChange={(e) => set({ legal_name: e.target.value })} autoFocus />
              </label>
              {isCompany && (
                <label className="sub-fld">
                  <span>{t("subscribe.taxId")}{isEU && <em className="opt">{t("subscribe.taxIdEu")}</em>}</span>
                  <input value={form.tax_id ?? ""} placeholder={(form.country ?? "").toUpperCase() + "1234567890"}
                    onChange={(e) => set({ tax_id: e.target.value.toUpperCase() })} />
                  {isEU && <em className="hint">{t("subscribe.taxIdHint")}</em>}
                </label>
              )}
              <label className="sub-fld">
                <span>{t("subscribe.address")}</span>
                <input value={form.address_line1 ?? ""} onChange={(e) => set({ address_line1: e.target.value })} />
              </label>
              <label className="sub-fld">
                <span>{t("subscribe.address2")}<em className="opt">{t("subscribe.optional")}</em></span>
                <input value={form.address_line2 ?? ""} onChange={(e) => set({ address_line2: e.target.value })} />
              </label>
              <div className="sub-row">
                <label className="sub-fld">
                  <span>{t("subscribe.postal")}</span>
                  <input value={form.postal_code ?? ""} onChange={(e) => set({ postal_code: e.target.value })} />
                </label>
                <label className="sub-fld">
                  <span>{t("subscribe.city")}</span>
                  <input value={form.city ?? ""} onChange={(e) => set({ city: e.target.value })} />
                </label>
              </div>
            </section>
          )}

          {step === 3 && (
            <section className="sub-card">
              <h2><IcMail size={17} /> {t("subscribe.step3")}</h2>
              <p className="desc">{t("subscribe.invoiceHint")}</p>
              <label className="sub-fld">
                <span>{t("subscribe.invoiceEmail")}</span>
                <input type="email" inputMode="email" value={form.invoice_email ?? ""} autoFocus
                  onChange={(e) => set({ invoice_email: e.target.value })} />
              </label>
              <label className="sub-fld">
                <span>{t("subscribe.invoiceName")}<em className="opt">{t("subscribe.optional")}</em></span>
                <input value={form.invoice_name ?? ""} onChange={(e) => set({ invoice_name: e.target.value })} />
              </label>
              <label className="sub-fld">
                <span>{t("subscribe.phone")}<em className="opt">{t("subscribe.optional")}</em></span>
                <input value={form.phone ?? ""} inputMode="tel" onChange={(e) => set({ phone: e.target.value })} />
              </label>
            </section>
          )}

          {step === 4 && (
            <section className="sub-card">
              <h2><IcCheck size={17} /> {t("subscribe.step4")}</h2>
              <dl className="sub-review">
                <div><dt>{t("subscribe.rPlan")}</dt><dd>{plan?.name} · {cycle === "year" ? t("subscribe.yearly") : t("subscribe.monthly")}</dd></div>
                <div><dt>{t("subscribe.rClient")}</dt><dd>{form.legal_name}{isCompany && form.tax_id ? ` · ${form.tax_id}` : ""}</dd></div>
                <div><dt>{t("subscribe.rAddress")}</dt><dd>{[form.address_line1, form.address_line2, `${form.postal_code ?? ""} ${form.city ?? ""}`.trim(), countryName(form.country ?? "", i18n.language)].filter(Boolean).join(", ")}</dd></div>
                <div><dt>{t("subscribe.rInvoice")}</dt><dd>{form.invoice_email}{form.invoice_name ? ` (${form.invoice_name})` : ""}</dd></div>
              </dl>
              <button className="pnl-btn ghost sm" onClick={() => setStep(2)}>{t("subscribe.edit")}</button>
            </section>
          )}

          {err && <p className="pnl-err sub-err">{err}</p>}

          <div className="sub-nav">
            {step > 0 && <button className="pnl-btn" onClick={() => { setErr(""); setStep((s) => Math.max(0, s - 1) as Step); }}>{t("subscribe.previous")}</button>}
            <span className="grow" />
            {step < 4
              ? <button className="pnl-btn primary" disabled={save.isPending} onClick={() => void next()}>{t("subscribe.continue")}</button>
              : <button className="pnl-btn primary" disabled={checkout.isPending} onClick={() => checkout.mutate()}>{t("subscribe.pay")}</button>}
          </div>
        </div>

        {/* Resumo sempre à vista: o valor muda com o plano, o ciclo e o país — esconder isso num
            passo final é como pedir para assinar sem ver o preço. */}
        <aside className="sub-sum">
          <h3>{t("subscribe.summary")}</h3>
          {!plan ? <p className="desc">{t("subscribe.pickPlanFirst")}</p> : (
            <>
              <div className="sub-line"><span>{plan.name}</span><b>{money(q?.unit_cents ?? (cycle === "year" ? plan.price_cents_per_user * 10 : plan.price_cents_per_user), plan.currency)}</b></div>
              <div className="sub-line sm"><span>{cycle === "year" ? t("subscribe.perUserYear") : t("subscribe.perUserMonth")} × {billed}</span><span>{t("subscribe.users", { n: billed })}</span></div>
              <div className="sub-line"><span>{t("subscribe.subtotal")}</span><b>{money(q?.subtotal_cents ?? 0, plan.currency)}</b></div>
              <div className="sub-line">
                <span>{q?.reverse_charge ? t("subscribe.vatReverse") : t("subscribe.vat", { rate: (quote.data?.vat_rate_percent ?? 0).toLocaleString(i18n.language) })}</span>
                <b>{money(q?.vat_cents ?? 0, plan.currency)}</b>
              </div>
              <div className="sub-line total"><span>{t("subscribe.total")}</span><b>{money(q?.total_cents ?? 0, plan.currency)}</b></div>
              <p className="sub-fine">{cycle === "year" ? t("subscribe.fineYear") : t("subscribe.fineMonth")}</p>
              {q?.reverse_charge && <p className="sub-fine ok">{t("subscribe.reverseNote")}</p>}
              {!q?.reverse_charge && q?.vat_rate_bps ? <p className="sub-fine">{t("subscribe.plVatNote")}</p> : null}
              {isCompany && isEU && (form.tax_id ?? "") !== "" && q && !q.tax_id_valid && (
                <p className="sub-fine warn">{t("subscribe.taxIdInvalid")}</p>
              )}
            </>
          )}
        </aside>
      </div>
    </div>
  );
}

// Nome do país no idioma do utilizador (Intl faz o trabalho; sem tabela de traduções à mão).
function countryName(code: string, lang: string): string {
  if (!code) return "";
  try { return new Intl.DisplayNames([lang], { type: "region" }).of(code.toUpperCase()) ?? code; }
  catch { return code; }
}

export { toast };
