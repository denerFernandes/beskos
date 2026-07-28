import { useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { backend, settingsApi } from "./backend";
import { BESKOS_MARK } from "./branding";
import { PasswordField } from "./PasswordField";
import { BRAND_COLORS } from "./branding";
import { IcCheck, IcUsers, IcStar, IcPlus, IcX } from "../panel/icons";
import "./onboarding.css";

// Primeiro acesso do dono: definir a senha, vestir a casa (logótipo e cor) e chamar a equipa.
// Fica FORA do registo de propósito — antes do cartão, cada campo a mais é gente que desiste.
//
// Só corre uma vez: a marca de concluído vive nas definições do utilizador (general.onboarded),
// por isso não precisa de coluna nova nem de migração.

type Step = 0 | 1 | 2;

export function Onboarding({ email, onDone }: { email: string; onDone: () => void }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [step, setStep] = useState<Step>(0);
  const [err, setErr] = useState("");

  // Passo 1 — senha
  const [pw, setPw] = useState("");
  const [pw2, setPw2] = useState("");
  const strength = useRef(false); // senha aceitável? (o medidor decide)
  // Passo 2 — marca
  const [color, setColor] = useState(BRAND_COLORS[0].hex);
  const [logo, setLogo] = useState("");
  // Passo 3 — equipa
  const [invites, setInvites] = useState<string[]>([]);
  const [invite, setInvite] = useState("");

  const domains = useQuery({ queryKey: ["domains"], queryFn: api.domains, retry: false });
  const domain = domains.data?.[0];

  const finish = useMutation({
    mutationFn: async () => {
      await settingsApi.patch({ general: { onboarded: true } });
    },
    onSuccess: () => { qc.invalidateQueries({ queryKey: ["settings"] }); onDone(); },
  });

  const savePassword = useMutation({
    mutationFn: () => api.resetUserPassword(email, pw),
    onSuccess: () => { setErr(""); setStep(1); },
    onError: (e) => setErr((e as Error).message),
  });
  const saveBrand = useMutation({
    mutationFn: () => backend.brandingUpdate({ color, logo }),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ["branding"] }); setErr(""); setStep(2); },
    onError: (e) => setErr((e as Error).message),
  });
  // Convites: cada endereço vira um convite por e-mail (a pessoa escolhe a senha ao aceitar).
  const sendInvites = useMutation({
    mutationFn: async () => { for (const e of invites) await api.createInvite(e, "member"); },
    onSuccess: () => finish.mutate(),
    onError: (e) => setErr((e as Error).message),
  });

  const onLogo = (f: File | undefined) => {
    if (!f) return;
    // Data URL: é assim que a marca é guardada (o backend recebe a string). Ficheiro pequeno.
    if (f.size > 512 * 1024) { setErr(t("onboarding.logoTooBig")); return; }
    const rd = new FileReader();
    rd.onload = () => { setErr(""); setLogo(String(rd.result || "")); };
    rd.readAsDataURL(f);
  };

  const addInvite = () => {
    const e = invite.trim().toLowerCase();
    if (!e.includes("@") || invites.includes(e)) return;
    setInvites((v) => [...v, e]);
    setInvite("");
  };

  return (
    <div className="ob">
      <div className="ob-card">
        <header className="ob-head">
          <img src={BESKOS_MARK} alt="" />
          <div className="ob-dots">{[0, 1, 2].map((s) => <i key={s} className={s === step ? "on" : s < step ? "done" : ""} />)}</div>
        </header>

        {step === 0 && (
          <>
            <h1>{t("onboarding.h0")}</h1>
            <p className="ob-sub">{t("onboarding.p0", { email })}</p>
            <PasswordField label={t("onboarding.password")} value={pw} autoFocus userInputs={[email]}
              onChange={(v) => { setErr(""); setPw(v); }} onStrength={(s) => { strength.current = s.ok; }} />
            <label className="ob-fld"><span>{t("onboarding.password2")}</span>
              <input type="password" value={pw2} onChange={(e) => { setErr(""); setPw2(e.target.value); }} />
            </label>
            {err && <p className="ob-err">{err}</p>}
            {/* Bloqueado enquanto a senha não passa: o servidor de correio recusa-a de qualquer
                forma, e é melhor dizê-lo aqui do que depois de submeter. */}
            <button className="ob-cta" disabled={savePassword.isPending || !strength.current || pw !== pw2}
              onClick={() => {
                if (!strength.current) { setErr(t("webmail.pwHintWeak")); return; }
                if (pw !== pw2) { setErr(t("onboarding.errPwMatch")); return; }
                savePassword.mutate();
              }}>{t("onboarding.continue")}</button>
          </>
        )}

        {step === 1 && (
          <>
            <h1>{t("onboarding.h1")}</h1>
            <p className="ob-sub">{t("onboarding.p1")}</p>
            <div className="ob-logo">
              <div className="ob-prev" style={{ background: color }}>
                {logo ? <img src={logo} alt="" /> : <IcStar size={22} />}
              </div>
              <label className="ob-upload">
                {t("onboarding.pickLogo")}
                <input type="file" accept="image/png,image/jpeg,image/svg+xml,image/webp" hidden
                  onChange={(e) => onLogo(e.target.files?.[0])} />
              </label>
              {logo && <button className="ob-link" onClick={() => setLogo("")}>{t("onboarding.removeLogo")}</button>}
            </div>
            <div className="ob-colors">
              {BRAND_COLORS.map((c) => (
                <button key={c.id} className={"ob-color" + (color === c.hex ? " on" : "")} style={{ background: c.hex }}
                  title={c.name} onClick={() => setColor(c.hex)}>{color === c.hex && <IcCheck size={14} />}</button>
              ))}
            </div>
            {err && <p className="ob-err">{err}</p>}
            <button className="ob-cta" disabled={saveBrand.isPending} onClick={() => saveBrand.mutate()}>{t("onboarding.continue")}</button>
            <button className="ob-skip" onClick={() => setStep(2)}>{t("onboarding.skip")}</button>
          </>
        )}

        {step === 2 && (
          <>
            <h1>{t("onboarding.h2")}</h1>
            <p className="ob-sub">{t("onboarding.p2", { domain: domain?.dominio ?? "" })}</p>
            <div className="ob-invite">
              <input type="email" placeholder={`colega@${domain?.dominio ?? "acme.com"}`} value={invite}
                onChange={(e) => setInvite(e.target.value)}
                onKeyDown={(e) => { if (e.key === "Enter") { e.preventDefault(); addInvite(); } }} />
              <button onClick={addInvite}><IcPlus size={16} /></button>
            </div>
            {invites.length > 0 && (
              <ul className="ob-list">
                {invites.map((e) => (
                  <li key={e}><IcUsers size={14} /> {e}
                    <button onClick={() => setInvites((v) => v.filter((x) => x !== e))}><IcX size={13} /></button>
                  </li>
                ))}
              </ul>
            )}
            {err && <p className="ob-err">{err}</p>}
            <button className="ob-cta" disabled={sendInvites.isPending || finish.isPending}
              onClick={() => (invites.length ? sendInvites.mutate() : finish.mutate())}>
              {invites.length ? t("onboarding.invite", { count: invites.length }) : t("onboarding.enter")}
            </button>
            {invites.length > 0 && <button className="ob-skip" onClick={() => finish.mutate()}>{t("onboarding.skip")}</button>}
          </>
        )}
      </div>
    </div>
  );
}
