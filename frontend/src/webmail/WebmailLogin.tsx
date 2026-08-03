import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { login, verifyMfa, type MfaChallenge } from "./auth";
import { backend, ApiError, type Branding } from "./backend";
import { applyBrandColor, cachedBrand, BESKOS_MARK } from "./branding";
import { ForgotPassword } from "./PasswordRecovery";

// Login do webmail: autentica o USUÁRIO FINAL no Stalwart (Basic atrás do proxy, §D7).
// Distinto do login do painel (nossa API). Aqui a credencial é a da caixa de email.
export function WebmailLogin() {
  const { t } = useTranslation();
  const [email, setEmail] = useState("");
  const [pwd, setPwd] = useState("");
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);
  // 2º fator: quando o login devolve um desafio, mostra-se o passo do código em vez de e-mail/senha.
  const [mfa, setMfa] = useState<MfaChallenge | null>(null);
  const [code, setCode] = useState("");
  // Locked out: a separate screen rather than a modal, because somebody who cannot get in has
  // one thing to do and should not be looking at the form that just refused them.
  const [forgot, setForgot] = useState(false);
  // Marca no login: começa pela última guardada (aparece de imediato ao voltar) e resolve pela
  // organização do e-mail digitado (estilo Google), quando o e-mail parece completo.
  const [brand, setBrand] = useState<Branding | null>(() => cachedBrand());
  useEffect(() => { applyBrandColor(brand?.color ?? ""); }, [brand?.color]);
  useEffect(() => {
    if (!/^[^@\s]+@[^@\s]+\.[^@\s]+$/.test(email)) return;
    const id = setTimeout(() => { backend.brandingResolve(email).then((b) => { if (b.color || b.logo || b.name) setBrand(b); }).catch(() => {}); }, 400);
    return () => clearTimeout(id);
  }, [email]);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setErr(""); setBusy(true);
    try {
      const challenge = await login(email, pwd);
      if (challenge) { setMfa(challenge); setCode(""); } // conta com 2º fator → pede o código
    } catch (e) {
      // 401 = credencial errada; qualquer outra falha = servidor indisponível (não confundir
      // "senha errada" com "backend fora do ar", lei §4.4). Classifica-se pelo STATUS do ApiError,
      // não por regex à mensagem: a mensagem vem traduzida e um 401 traduzido não contém "401".
      const status = (e as ApiError)?.status;
      setErr(status === 401 ? t("webmail.login.failed") : t("webmail.login.unreachable"));
    } finally {
      setBusy(false);
    }
  }

  async function submitCode(e: React.FormEvent) {
    e.preventDefault();
    setErr(""); setBusy(true);
    try {
      await verifyMfa(email, mfa!.challenge, code.trim());
    } catch (e) {
      const status = (e as ApiError)?.status;
      setErr(status === 401 ? t("webmail.login.badCode") : t("webmail.login.unreachable"));
    } finally {
      setBusy(false);
    }
  }

  // The split screen. Left is the organisation — their logo, their name, their colour, at a size
  // that means something. Right is the act of signing in, and nothing else.
  //
  // WHY A PANEL AND NOT A BIGGER LOGO: this screen is white-labelled. A 26px mark above a grey card
  // told a customer's staff nothing about whose system they were entering; half the screen in their
  // own colour does. On a phone the panel collapses to a header, because there is no half to give.
  const panel = (
    <aside className="lg-panel">
      <div className="lg-panel-in">
        <img className={"lg-mark" + (brand?.logo && brand?.logo_white_dark ? " white-dark" : "")}
          src={brand?.logo || BESKOS_MARK} alt={brand?.name || "Beskos"} />
        <h2>{brand?.name || "Beskos"}</h2>
        {/* Only ours: putting words in a customer's mouth on their own sign-in screen is not our place. */}
        {!brand?.name && <p>{t("webmail.login.tagline")}</p>}
      </div>
    </aside>
  );

  if (forgot) {
    return (
      <div className="wm">
        <div className="lg-split">
          {panel}
          <main className="lg-main"><ForgotPassword email={email} onBack={() => setForgot(false)} /></main>
        </div>
      </div>
    );
  }

  return (
    <div className="wm">
      <div className="lg-split">
        {panel}
        <main className="lg-main">
          {!mfa ? (
            <form className="lg-card" onSubmit={submit}>
              <h1 className="lg-h">{t("webmail.login.signInTitle")}</h1>
              <p className="lg-sub">{t("webmail.login.subtitle")}</p>

              <label className="lg-field">
                <span>{t("webmail.login.emailLabel")}</span>
                <input type="email" value={email} autoComplete="username" required autoFocus
                  placeholder={t("webmail.login.email")}
                  onChange={(e) => setEmail(e.target.value)} />
              </label>
              <label className="lg-field">
                <span>{t("webmail.login.passwordLabel")}</span>
                <input type="password" value={pwd} autoComplete="current-password" required
                  onChange={(e) => setPwd(e.target.value)} />
              </label>

              {err && <p className="lg-err">{err}</p>}
              <button className="lg-btn" disabled={busy}>{busy ? "…" : t("webmail.login.submit")}</button>
              <button type="button" className="lg-link" onClick={() => setForgot(true)}>
                {t("login.forgotLink")}
              </button>
            </form>
          ) : (
            <form className="lg-card" onSubmit={submitCode}>
              <h1 className="lg-h">{t("webmail.login.mfaTitle")}</h1>
              <p className="lg-sub">{t("webmail.login.mfaPrompt")}</p>
              <label className="lg-field">
                <span>{t("webmail.login.codeLabel")}</span>
                <input inputMode="numeric" autoComplete="one-time-code" autoFocus value={code}
                  placeholder={t("webmail.login.codePlaceholder")}
                  onChange={(e) => setCode(e.target.value)} required />
              </label>
              {err && <p className="lg-err">{err}</p>}
              <button className="lg-btn" disabled={busy || !code.trim()}>{busy ? "…" : t("webmail.login.verify")}</button>
              <button type="button" className="lg-link" onClick={() => { setMfa(null); setErr(""); }}>
                {t("webmail.login.back")}
              </button>
            </form>
          )}
        </main>
      </div>
    </div>
  );
}
