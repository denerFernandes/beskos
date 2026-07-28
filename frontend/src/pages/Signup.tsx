import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { useQuery, useMutation } from "@tanstack/react-query";
import { api, type BillingCycle, type BillingProfile, type Plan } from "../api";
import { EU_COUNTRIES, COUNTRIES } from "../panel/countries";
import { BESKOS_MARK } from "../webmail/branding";
import { IcCheck, IcArrowLeft, IcGlobe, IcLock, IcMail, IcCard, IcUsers, IcShield, IcSparkle } from "../panel/icons";
import "./signup.css";

// Registo self-service (/signup): conta → plano → cliente → faturação → financeiro → resumo.
// Uma coisa por ecrã, com o resumo do valor sempre à vista: quem se regista precisa de saber
// quanto vai pagar antes de escrever o número do cartão, não depois.
//
// Tudo o que é recolhido aqui fica gravado (perfil de faturação do tenant), por isso a página de
// subscrição dentro da app não volta a pedir a mesma coisa.

type Step = 0 | 1 | 2 | 3 | 4 | 5;
const STEPS: Step[] = [0, 1, 2, 3, 4, 5];

interface Account { org: string; domain: string; login: string; name: string; password: string }

export function SignupPage() {
  const { t, i18n } = useTranslation();
  const [step, setStep] = useState<Step>(0);
  const [acc, setAcc] = useState<Account>({ org: "", domain: "", login: "", name: "", password: "" });
  const [planId, setPlanId] = useState("");
  const [cycle, setCycle] = useState<BillingCycle>("month");
  const [form, setForm] = useState<BillingProfile>({ kind: "company" });
  const [err, setErr] = useState("");
  const [done, setDone] = useState<{ email: string; domain: string } | null>(null);

  const cat = useQuery({ queryKey: ["public-plans"], queryFn: api.publicPlans, retry: false });
  const plans: Plan[] = cat.data?.plans ?? [];
  const trialDays = cat.data?.trial_days ?? 14;
  const plan = useMemo(() => plans.find((p) => p.id === planId), [plans, planId]);
  // Primeiro plano da lista por omissão: um ecrã de planos sem nada escolhido convida a hesitar.
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
      org_name: acc.org, domain: acc.domain, login: acc.login, name: acc.name, password: acc.password,
      plan: planId, cycle, billing: form,
    }),
    onSuccess: (r) => {
      // Com URL de checkout, o cartão é o passo seguinte (o teste só começa depois dele). Sem ela
      // (provedor manual ou falha na Stripe), a conta já existe e entra-se na app.
      if (r.checkout_url) { location.href = r.checkout_url; return; }
      setDone({ email: r.email, domain: acc.domain });
    },
    onError: (e) => setErr((e as Error).message),
  });

  const isEU = EU_COUNTRIES.includes((form.country ?? "").toUpperCase());
  const isCompany = form.kind === "company";
  const setB = (patch: Partial<BillingProfile>) => { setErr(""); setForm((f) => ({ ...f, ...patch })); };
  const setA = (patch: Partial<Account>) => { setErr(""); setAcc((a) => ({ ...a, ...patch })); };

  const stepError = (s: Step): string => {
    if (s === 0) {
      if (!acc.org.trim()) return t("signup.errOrg");
      if (!/^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$/.test(acc.domain.trim().toLowerCase())) return t("signup.errDomain");
      if (!/^[a-z0-9]([a-z0-9._-]*[a-z0-9])?$/.test(acc.login.trim().toLowerCase())) return t("signup.errLogin");
      if (!acc.name.trim()) return t("signup.errName");
      if (acc.password.length < 10) return t("signup.errPassword");
      return "";
    }
    if (s === 1) return planId ? "" : t("subscribe.errPlan");
    if (s === 2) return (form.country ?? "").length === 2 ? "" : t("subscribe.errCountry");
    if (s === 3) {
      if (!(form.legal_name ?? "").trim()) return isCompany ? t("subscribe.errLegalName") : t("subscribe.errName");
      if (!(form.address_line1 ?? "").trim() || !(form.city ?? "").trim()) return t("subscribe.errAddress");
      if (isCompany && (form.country ?? "").toUpperCase() === "PL" && !(form.tax_id ?? "").trim()) return t("subscribe.errTaxIdPL");
      return "";
    }
    if (s === 4) return (form.invoice_email ?? "").includes("@") ? "" : t("subscribe.errInvoiceEmail");
    return "";
  };
  const next = () => {
    const e = stepError(step);
    if (e) { setErr(e); return; }
    setErr("");
    setStep((s) => Math.min(5, s + 1) as Step);
  };

  if (done) return <SignupDone email={done.email} domain={done.domain} />;

  return (
    <div className="sg">
      {/* Painel da marca: o que é o produto, ao lado do formulário. No telemóvel fica só o topo. */}
      <aside className="sg-brand">
        <a className="sg-logo" href="https://beskos.com"><img src={BESKOS_MARK} alt="Beskos" /> Beskos</a>
        <div className="sg-pitch">
          <h2>{t("signup.pitchTitle")}</h2>
          <ul>
            <li><IcMail size={17} /> {t("signup.pitch1")}</li>
            <li><IcUsers size={17} /> {t("signup.pitch2")}</li>
            <li><IcShield size={17} /> {t("signup.pitch3")}</li>
            <li><IcSparkle size={17} /> {t("signup.pitch4")}</li>
          </ul>
        </div>
        <p className="sg-note">{t("signup.trialNote", { days: trialDays })}</p>
      </aside>

      <main className="sg-main">
        <header className="sg-head">
          <h1>{t("signup.title")}</h1>
          <p>{t("signup.subtitle")}</p>
          <a className="sg-login" href="/mail">{t("signup.haveAccount")}</a>
        </header>

        <ol className="sg-steps">
          {STEPS.map((s) => (
            <li key={s} className={s === step ? "on" : s < step ? "done" : ""}>
              <button disabled={s > step} onClick={() => setStep(s)}>
                <span className="n">{s < step ? <IcCheck size={12} /> : s + 1}</span>
                <span className="lb">{t(`signup.step${s}`)}</span>
              </button>
            </li>
          ))}
        </ol>

        <div className="sg-body">
          <section className="sg-card">
            {step === 0 && (
              <>
                <h2>{t("signup.step0")}</h2>
                <label className="sg-fld"><span>{t("signup.org")}</span>
                  <input value={acc.org} autoFocus placeholder="Acme Lda" onChange={(e) => setA({ org: e.target.value })} />
                </label>
                <label className="sg-fld"><span><IcGlobe size={14} /> {t("signup.domain")}</span>
                  <input value={acc.domain} placeholder="acme.com" autoCapitalize="off" spellCheck={false}
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
                <label className="sg-fld"><span><IcLock size={14} /> {t("signup.password")}</span>
                  <input type="password" value={acc.password} onChange={(e) => setA({ password: e.target.value })} />
                  <em className="hint">{t("signup.passwordHint")}</em>
                </label>
              </>
            )}

            {step === 1 && (
              <>
                <h2>{t("signup.step1")}</h2>
                <div className="sg-cycle">
                  <button className={cycle === "month" ? "on" : ""} onClick={() => setCycle("month")}>{t("subscribe.monthly")}</button>
                  <button className={cycle === "year" ? "on" : ""} onClick={() => setCycle("year")}>
                    {t("subscribe.yearly")}<em>{t("subscribe.yearlyOff")}</em>
                  </button>
                </div>
                <div className="sg-plans">
                  {plans.map((p) => (
                    <button key={p.id} className={"sg-plan" + (planId === p.id ? " on" : "")} onClick={() => { setPlanId(p.id); setErr(""); }}>
                      <span className="pn">{p.name}</span>
                      <span className="pp">{money(cycle === "year" ? p.price_cents_per_user * (cat.data?.months_charged ?? 10) : p.price_cents_per_user, p.currency)}</span>
                      <span className="pu">{cycle === "year" ? t("subscribe.perUserYear") : t("subscribe.perUserMonth")}</span>
                      <span className="pd">{p.mailbox_range} · {t("subscribe.storageGb", { n: Math.round(p.max_storage_bytes / (1 << 30)) })}</span>
                    </button>
                  ))}
                </div>
              </>
            )}

            {step === 2 && (
              <>
                <h2>{t("subscribe.step1")}</h2>
                <div className="sg-kind">
                  <button className={isCompany ? "on" : ""} onClick={() => setB({ kind: "company" })}>
                    <b>{t("subscribe.company")}</b><span>{t("subscribe.companyHint")}</span>
                  </button>
                  <button className={!isCompany ? "on" : ""} onClick={() => setB({ kind: "individual" })}>
                    <b>{t("subscribe.individual")}</b><span>{t("subscribe.individualHint")}</span>
                  </button>
                </div>
                <label className="sg-fld"><span><IcGlobe size={14} /> {t("subscribe.country")}</span>
                  <select value={form.country ?? ""} onChange={(e) => setB({ country: e.target.value })}>
                    <option value="">{t("subscribe.choose")}</option>
                    {COUNTRIES.map((c) => <option key={c} value={c}>{countryName(c, i18n.language)}</option>)}
                  </select>
                </label>
                {isEU && <p className="sg-note-inline">{t("subscribe.euNote")}</p>}
              </>
            )}

            {step === 3 && (
              <>
                <h2>{t("subscribe.step2")}</h2>
                <label className="sg-fld"><span>{isCompany ? t("subscribe.legalName") : t("subscribe.fullName")}</span>
                  <input value={form.legal_name ?? ""} autoFocus onChange={(e) => setB({ legal_name: e.target.value })} />
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
              </>
            )}

            {step === 4 && (
              <>
                <h2>{t("subscribe.step3")}</h2>
                <p className="desc">{t("subscribe.invoiceHint")}</p>
                <label className="sg-fld"><span><IcMail size={14} /> {t("subscribe.invoiceEmail")}</span>
                  <input type="email" autoFocus value={form.invoice_email ?? ""} onChange={(e) => setB({ invoice_email: e.target.value })} />
                </label>
                <label className="sg-fld"><span>{t("subscribe.invoiceName")}<em className="opt">{t("subscribe.optional")}</em></span>
                  <input value={form.invoice_name ?? ""} onChange={(e) => setB({ invoice_name: e.target.value })} />
                </label>
              </>
            )}

            {step === 5 && (
              <>
                <h2>{t("signup.step5")}</h2>
                <dl className="sg-review">
                  <div><dt>{t("signup.rAccount")}</dt><dd>{acc.login}@{acc.domain} · {acc.org}</dd></div>
                  <div><dt>{t("subscribe.rPlan")}</dt><dd>{plan?.name} · {cycle === "year" ? t("subscribe.yearly") : t("subscribe.monthly")}</dd></div>
                  <div><dt>{t("subscribe.rClient")}</dt><dd>{form.legal_name}{isCompany && form.tax_id ? ` · ${form.tax_id}` : ""}</dd></div>
                  <div><dt>{t("subscribe.rInvoice")}</dt><dd>{form.invoice_email}</dd></div>
                </dl>
                <p className="sg-trial"><IcCard size={16} /> {t("signup.cardNote", { days: trialDays })}</p>
              </>
            )}

            {err && <p className="sg-err">{err}</p>}

            <div className="sg-nav">
              {step > 0 && <button className="sg-btn" onClick={() => { setErr(""); setStep((s) => Math.max(0, s - 1) as Step); }}>
                <IcArrowLeft size={15} /> {t("subscribe.previous")}
              </button>}
              <span className="grow" />
              {step < 5
                ? <button className="sg-btn primary" onClick={next}>{t("subscribe.continue")}</button>
                : <button className="sg-btn primary" disabled={signup.isPending} onClick={() => signup.mutate()}>
                    {signup.isPending ? "…" : t("signup.start", { days: trialDays })}
                  </button>}
            </div>
          </section>

          <aside className="sg-sum">
            <h3>{t("subscribe.summary")}</h3>
            {!plan ? <p className="desc">{t("subscribe.pickPlanFirst")}</p> : (
              <>
                <div className="sg-line"><span>{plan.name}</span><b>{money(q?.unit_cents ?? plan.price_cents_per_user, plan.currency)}</b></div>
                <div className="sg-line sm"><span>{cycle === "year" ? t("subscribe.perUserYear") : t("subscribe.perUserMonth")}</span><span>{t("subscribe.users", { n: q?.seats ?? 1 })}</span></div>
                <div className="sg-line"><span>{t("subscribe.subtotal")}</span><b>{money(q?.subtotal_cents ?? 0, plan.currency)}</b></div>
                <div className="sg-line">
                  <span>{q?.reverse_charge ? t("subscribe.vatReverse") : t("subscribe.vat", { rate: (quote.data?.vat_rate_percent ?? 0).toLocaleString(i18n.language) })}</span>
                  <b>{money(q?.vat_cents ?? 0, plan.currency)}</b>
                </div>
                <div className="sg-line total"><span>{t("subscribe.total")}</span><b>{money(q?.total_cents ?? 0, plan.currency)}</b></div>
                <p className="sg-fine">{t("signup.trialFine", { days: trialDays })}</p>
                {q?.reverse_charge && <p className="sg-fine ok">{t("subscribe.reverseNote")}</p>}
                {!q?.reverse_charge && (q?.vat_rate_bps ?? 0) > 0 && <p className="sg-fine">{t("subscribe.plVatNote")}</p>}
              </>
            )}
          </aside>
        </div>
      </main>
    </div>
  );
}

// Ecrã final quando não há checkout (provedor manual): a conta existe, falta publicar o DNS.
function SignupDone({ email, domain }: { email: string; domain: string }) {
  const { t } = useTranslation();
  return (
    <div className="sg done">
      <main className="sg-main">
        <div className="sg-card center">
          <img className="sg-mark" src={BESKOS_MARK} alt="Beskos" />
          <h1>{t("signup.doneTitle")}</h1>
          <p>{t("signup.doneBody", { email })}</p>
          <p className="desc">{t("signup.doneDns", { domain })}</p>
          <a className="sg-btn primary" href="/mail">{t("signup.doneEnter")}</a>
        </div>
      </main>
    </div>
  );
}

function countryName(code: string, lang: string): string {
  try { return new Intl.DisplayNames([lang], { type: "region" }).of(code.toUpperCase()) ?? code; }
  catch { return code; }
}
