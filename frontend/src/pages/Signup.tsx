import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { useQuery, useMutation } from "@tanstack/react-query";
import { api, type BillingCycle, type BillingProfile, type Plan } from "../api";
import { EU_COUNTRIES, COUNTRIES } from "../panel/countries";
import { BESKOS_MARK } from "../webmail/branding";
import { IcCheck, IcArrowLeft } from "../panel/icons";
import "./signup.css";

// Self-service signup: plan → account → company and billing → payment. Four screens, one column,
// one question at a time. The rest (admin password, branding, team) is left for the first access
// inside the app — asking for everything before the card is the shortest path to a mid-flow drop-off.

type Step = 0 | 1 | 2 | 3;
const STEPS: Step[] = [0, 1, 2, 3];

export function SignupPage() {
  const { t, i18n } = useTranslation();
  const [step, setStep] = useState<Step>(0);
  const [planId, setPlanId] = useState("");
  const [cycle, setCycle] = useState<BillingCycle>("month");
  const [acc, setAcc] = useState({ domain: "", login: "", name: "" });
  const [form, setForm] = useState<BillingProfile>({ kind: "company" });
  const [err, setErr] = useState("");
  const [done, setDone] = useState<{ email: string; domain: string } | null>(null);

  const cat = useQuery({ queryKey: ["public-plans"], queryFn: api.publicPlans, retry: false });
  const plans: Plan[] = cat.data?.plans ?? [];
  const trialDays = cat.data?.trial_days ?? 14;
  const months = cat.data?.months_charged ?? 10;
  const plan = useMemo(() => plans.find((p) => p.id === planId), [plans, planId]);
  useEffect(() => { if (!planId && plans.length) setPlanId(plans[Math.min(1, plans.length - 1)].id); }, [plans, planId]);

  const money = (cents: number, cur?: string) =>
    new Intl.NumberFormat(i18n.language, { style: "currency", currency: cur || plan?.currency || cat.data?.currency || "EUR" }).format(cents / 100);

  const quote = useQuery({
    queryKey: ["public-quote", planId, cycle, form.kind, form.country, form.tax_id],
    queryFn: () => api.publicQuote({ plan: planId, cycle, kind: form.kind, country: form.country ?? "", tax_id: form.tax_id ?? "", seats: 1 }),
    enabled: !!planId,
    retry: false,
  });
  const q = quote.data?.quote;

  const signup = useMutation({
    mutationFn: () => api.publicSignup({
      // The organization is named as on the invoice: one less question in the form.
      org_name: form.legal_name || acc.domain, domain: acc.domain, login: acc.login, name: acc.name,
      password: "", plan: planId, cycle, billing: form,
    }),
    onSuccess: (r) => {
      if (r.checkout_url) { location.href = r.checkout_url; return; }
      setDone({ email: r.email, domain: acc.domain });
    },
    onError: (e) => setErr((e as Error).message),
  });

  const isEU = EU_COUNTRIES.includes((form.country ?? "").toUpperCase());
  const isCompany = form.kind === "company";
  const setB = (patch: Partial<BillingProfile>) => { setErr(""); setForm((f) => ({ ...f, ...patch })); };
  const setA = (patch: Partial<typeof acc>) => { setErr(""); setAcc((a) => ({ ...a, ...patch })); };

  const stepError = (s: Step): string => {
    if (s === 0) return planId ? "" : t("subscribe.errPlan");
    if (s === 1) {
      if (!/^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$/.test(acc.domain.trim())) return t("signup.errDomain");
      if (!/^[a-z0-9]([a-z0-9._-]*[a-z0-9])?$/.test(acc.login.trim())) return t("signup.errLogin");
      if (!acc.name.trim()) return t("signup.errName");
      return "";
    }
    if (s === 2) {
      if ((form.country ?? "").length !== 2) return t("subscribe.errCountry");
      if (!(form.legal_name ?? "").trim()) return isCompany ? t("subscribe.errLegalName") : t("subscribe.errName");
      if (!(form.address_line1 ?? "").trim() || !(form.city ?? "").trim()) return t("subscribe.errAddress");
      if (isCompany && (form.country ?? "").toUpperCase() === "PL" && !(form.tax_id ?? "").trim()) return t("subscribe.errTaxIdPL");
      if (!(form.invoice_email ?? "").includes("@")) return t("subscribe.errInvoiceEmail");
      return "";
    }
    return "";
  };
  const next = () => {
    const e = stepError(step);
    if (e) { setErr(e); return; }
    setErr("");
    setStep((s) => Math.min(3, s + 1) as Step);
  };

  if (done) return <SignupDone email={done.email} domain={done.domain} />;
  // Signup closed (SIGNUPS_OPEN=false): shows "coming soon" instead of the form. The API refuses
  // either way — this is so whoever arrives from the site doesn't hit an error.
  if (cat.data && cat.data.signups_open === false) return <ComingSoon />;

  return (
    <div className="sg">
      <header className="sg-top">
        <a className="sg-logo" href="https://beskos.com"><img src={BESKOS_MARK} alt="" /> Beskos</a>
        <a className="sg-login" href="/mail">{t("signup.haveAccount")}</a>
      </header>

      <main className="sg-main">
        <div className="sg-dots">
          {STEPS.map((s) => <i key={s} className={s === step ? "on" : s < step ? "done" : ""} />)}
        </div>
        <h1>{t(`signup.h${step}`)}</h1>
        <p className="sg-sub">{t(`signup.p${step}`, { days: trialDays })}</p>

        {step === 0 && (
          <>
            <div className="sg-cycle">
              <button className={cycle === "month" ? "on" : ""} onClick={() => setCycle("month")}>{t("subscribe.monthly")}</button>
              <button className={cycle === "year" ? "on" : ""} onClick={() => setCycle("year")}>
                {t("subscribe.yearly")} <em>−{Math.round((1 - months / 12) * 100)}%</em>
              </button>
            </div>
            <div className="sg-plans">
              {plans.map((p) => {
                const unit = cycle === "year" ? p.price_cents_per_user * months : p.price_cents_per_user;
                return (
                  <button key={p.id} className={"sg-plan" + (planId === p.id ? " on" : "")} onClick={() => { setPlanId(p.id); setErr(""); }}>
                    <span className="tick">{planId === p.id && <IcCheck size={13} />}</span>
                    <span className="pi">
                      <b>{p.name}</b>
                      <em>{p.mailbox_range} · {t("subscribe.storageGb", { n: Math.round(p.max_storage_bytes / (1 << 30)) })}</em>
                    </span>
                    <span className="pp">{money(unit, p.currency)}<em>{cycle === "year" ? t("signup.perUserYearShort") : t("signup.perUserMonthShort")}</em></span>
                  </button>
                );
              })}
            </div>
          </>
        )}

        {step === 1 && (
          <>
            <label className="sg-fld"><span>{t("signup.domain")}</span>
              <input value={acc.domain} placeholder="acme.com" autoFocus autoCapitalize="off" spellCheck={false}
                onChange={(e) => setA({ domain: e.target.value.toLowerCase().trim() })} />
              <em className="hint">{t("signup.domainHint")}</em>
            </label>
            <label className="sg-fld"><span>{t("signup.email")}</span>
              <span className="sg-email">
                <input value={acc.login} placeholder="ana" autoCapitalize="off" spellCheck={false}
                  onChange={(e) => setA({ login: e.target.value.toLowerCase().trim() })} />
                <b>@{acc.domain || "acme.com"}</b>
              </span>
            </label>
            <label className="sg-fld"><span>{t("signup.name")}</span>
              <input value={acc.name} placeholder="Ana Costa" onChange={(e) => setA({ name: e.target.value })} />
            </label>
          </>
        )}

        {step === 2 && (
          <>
            <div className="sg-kind">
              <button className={isCompany ? "on" : ""} onClick={() => setB({ kind: "company" })}>{t("subscribe.company")}</button>
              <button className={!isCompany ? "on" : ""} onClick={() => setB({ kind: "individual" })}>{t("subscribe.individual")}</button>
            </div>
            <label className="sg-fld"><span>{t("subscribe.country")}</span>
              <select value={form.country ?? ""} onChange={(e) => setB({ country: e.target.value })}>
                <option value="">{t("subscribe.choose")}</option>
                {COUNTRIES.map((c) => <option key={c} value={c}>{countryName(c, i18n.language)}</option>)}
              </select>
            </label>
            <label className="sg-fld"><span>{isCompany ? t("subscribe.legalName") : t("subscribe.fullName")}</span>
              <input value={form.legal_name ?? ""} onChange={(e) => setB({ legal_name: e.target.value })} />
            </label>
            {isCompany && (
              <label className="sg-fld"><span>{t("subscribe.taxId")}{isEU && <em className="opt">{t("subscribe.taxIdEu")}</em>}</span>
                <input value={form.tax_id ?? ""} placeholder={(form.country ?? "").toUpperCase() + "1234567890"}
                  onChange={(e) => setB({ tax_id: e.target.value.toUpperCase() })} />
                {isEU && <em className="hint">{t("subscribe.taxIdHint")}</em>}
              </label>
            )}
            <label className="sg-fld"><span>{t("subscribe.address")}</span>
              <input value={form.address_line1 ?? ""} onChange={(e) => setB({ address_line1: e.target.value })} />
            </label>
            <div className="sg-row">
              <label className="sg-fld"><span>{t("subscribe.postal")}</span>
                <input value={form.postal_code ?? ""} onChange={(e) => setB({ postal_code: e.target.value })} />
              </label>
              <label className="sg-fld"><span>{t("subscribe.city")}</span>
                <input value={form.city ?? ""} onChange={(e) => setB({ city: e.target.value })} />
              </label>
            </div>
            <label className="sg-fld"><span>{t("subscribe.invoiceEmail")}</span>
              <input type="email" value={form.invoice_email ?? ""} onChange={(e) => setB({ invoice_email: e.target.value })} />
              <em className="hint">{t("signup.invoiceHintShort")}</em>
            </label>
          </>
        )}

        {step === 3 && plan && (
          <div className="sg-recap">
            <div className="rl"><span>{plan.name} · {cycle === "year" ? t("subscribe.yearly") : t("subscribe.monthly")}</span><b>{money(q?.subtotal_cents ?? 0, plan.currency)}</b></div>
            <div className="rl"><span>{acc.login}@{acc.domain}</span><span className="dim">{form.legal_name}</span></div>
            <div className="rl"><span>{q?.reverse_charge ? t("subscribe.vatReverse") : t("subscribe.vat", { rate: (quote.data?.vat_rate_percent ?? 0).toLocaleString(i18n.language) })}</span><b>{money(q?.vat_cents ?? 0, plan.currency)}</b></div>
            <div className="rl total"><span>{t("subscribe.total")}</span><b>{money(q?.total_cents ?? 0, plan.currency)}</b></div>
            <p className="sg-fine">{t("signup.cardNote", { days: trialDays })}</p>
            {q?.reverse_charge && <p className="sg-fine ok">{t("subscribe.reverseNote")}</p>}
          </div>
        )}

        {err && <p className="sg-err">{err}</p>}

        <div className="sg-nav">
          {step > 0 && <button className="sg-back" onClick={() => { setErr(""); setStep((s) => Math.max(0, s - 1) as Step); }}>
            <IcArrowLeft size={15} /> {t("subscribe.previous")}
          </button>}
          {step < 3
            ? <button className="sg-cta" onClick={next}>{t("subscribe.continue")}</button>
            : <button className="sg-cta" disabled={signup.isPending} onClick={() => signup.mutate()}>
                {signup.isPending ? "…" : t("signup.start", { days: trialDays })}
              </button>}
        </div>

        {step === 0 && <p className="sg-fine center">{t("signup.trialFine", { days: trialDays })}</p>}
      </main>
    </div>
  );
}

// Signup closed: a single page, with the brand and where to go next.
function ComingSoon() {
  const { t } = useTranslation();
  return (
    <div className="sg">
      <header className="sg-top">
        <a className="sg-logo" href="https://beskos.com"><img src={BESKOS_MARK} alt="" /> Beskos</a>
        <a className="sg-login" href="/mail">{t("signup.haveAccount")}</a>
      </header>
      <main className="sg-main center">
        <span className="sg-badge">{t("signup.soonBadge")}</span>
        <h1>{t("signup.soonTitle")}</h1>
        <p className="sg-sub">{t("signup.soonBody")}</p>
        {/* The form, not an address. Somebody who navigated to /signup has already decided to try
            the product -- telling them to go and compose an e-mail instead is asking them to do our
            work, and most of them simply will not. */}
        <SoonWaitlist />
        <a className="sg-cta ghost" href="https://beskos.com">{t("signup.soonCta")}</a>
      </main>
    </div>
  );
}

// SoonWaitlist puts somebody on the waiting list from the closed-signup screen.
function SoonWaitlist() {
  const { t, i18n } = useTranslation();
  const [name, setName] = useState("");
  const [email, setEmail] = useState("");
  const [busy, setBusy] = useState(false);
  const [done, setDone] = useState(false);
  const [err, setErr] = useState("");

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    if (busy || !name.trim() || !email.trim()) return;
    setBusy(true);
    setErr("");
    try {
      await api.joinWaitlist({
        email: email.trim(), name: name.trim(), locale: i18n.language, source: "signup",
      });
      setDone(true);
    } catch (e) {
      setErr((e as Error).message);
    } finally {
      setBusy(false);
    }
  }

  // The form is replaced by the confirmation: leaving it there invites a second submission that
  // changes nothing, which reads as a form that did not work.
  if (done) {
    return (
      <p className="sg-fine ok center sg-soon-done">{t("signup.soonDone")}</p>
    );
  }

  return (
    <form className="sg-soon-form" onSubmit={submit}>
      <div className="sg-fld">
        <input value={name} onChange={(ev) => setName(ev.target.value)} required
          placeholder={t("signup.soonName")} aria-label={t("signup.soonName")} autoComplete="name" />
      </div>
      <div className="sg-fld">
        <input value={email} onChange={(ev) => setEmail(ev.target.value)} required type="email"
          placeholder={t("signup.soonEmail")} aria-label={t("signup.soonEmail")} autoComplete="email" />
      </div>
      <button className="sg-cta" type="submit" disabled={busy}>
        {busy ? t("signup.soonSending") : t("signup.soonSubmit")}
      </button>
      {err && <p className="sg-fine center sg-soon-err">{err}</p>}
      <p className="sg-fine center">{t("signup.soonPrivacy")}</p>
    </form>
  );
}

// No checkout (manual provider): the account already exists, just log in.
function SignupDone({ email, domain }: { email: string; domain: string }) {
  const { t } = useTranslation();
  return (
    <div className="sg">
      <header className="sg-top"><a className="sg-logo" href="https://beskos.com"><img src={BESKOS_MARK} alt="" /> Beskos</a></header>
      <main className="sg-main center">
        <div className="sg-ok"><IcCheck size={26} /></div>
        <h1>{t("signup.doneTitle")}</h1>
        <p className="sg-sub">{t("signup.doneBody", { email })}</p>
        <p className="sg-fine">{t("signup.doneDns", { domain })}</p>
        <a className="sg-cta" href="/mail">{t("signup.doneEnter")}</a>
      </main>
    </div>
  );
}

function countryName(code: string, lang: string): string {
  try { return new Intl.DisplayNames([lang], { type: "region" }).of(code.toUpperCase()) ?? code; }
  catch { return code; }
}
