import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { login, verifyMfa, type MfaChallenge } from "./auth";
import { backend, ApiError, type Branding } from "./backend";
import { applyBrandColor, cachedBrand, BESKOS_MARK } from "./branding";

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

  return (
    <div className="wm">
      <div className="login-wrap">
        <form className="login" onSubmit={submit}>
          <h1>
            {/* Sem logótipo do cliente, a marca Beskos (o fallback era a inicial "M", do nome antigo). */}
            <img className={"brand-logo" + (brand?.logo && brand?.logo_white_dark ? " white-dark" : "")} src={brand?.logo || BESKOS_MARK} alt={brand?.name || "Beskos"} />
            {" "}{brand?.name || t("webmail.login.title")}
          </h1>
          <p>{mfa ? t("webmail.login.mfaPrompt") : t("webmail.login.subtitle")}</p>
          {!mfa && <>
            <input type="email" placeholder={t("webmail.login.email")} value={email}
              onChange={(e) => setEmail(e.target.value)} required autoFocus />
            <input type="password" placeholder={t("webmail.login.password")} value={pwd}
              onChange={(e) => setPwd(e.target.value)} required />
          </>}
          {err && <p className="err" style={{ marginBottom: 12 }}>{err}</p>}
          {!mfa && <button className="send" disabled={busy}>{busy ? "…" : t("webmail.login.submit")}</button>}
        </form>
        {mfa && (
          <form className="login" onSubmit={submitCode} style={{ marginTop: -8 }}>
            <input inputMode="numeric" autoComplete="one-time-code" autoFocus
              placeholder={t("webmail.login.codePlaceholder")} value={code}
              onChange={(e) => setCode(e.target.value)} required />
            <button className="send" disabled={busy || !code.trim()}>{busy ? "…" : t("webmail.login.verify")}</button>
            <button type="button" className="linklike" onClick={() => { setMfa(null); setErr(""); }}>{t("webmail.login.back")}</button>
          </form>
        )}
      </div>
    </div>
  );
}
