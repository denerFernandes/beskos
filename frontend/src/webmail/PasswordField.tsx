import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { measure, type Strength } from "./passwordStrength";
import "./password.css";

// Campo de palavra-passe com medidor. Um só componente para todos os sítios onde se DEFINE uma
// senha (primeiro acesso, perfil, admin a criar/repor, convite): a regra é a mesma em todos, e
// tê-la escrita quatro vezes era garantir que um dia deixavam de coincidir.
//
// Quem usa recebe a avaliação por `onStrength` e desativa o botão enquanto `ok` for falso — o
// servidor recusa na mesma, mas a pessoa fica a saber antes de submeter, e porquê.
export function PasswordField({ value, onChange, label, userInputs = [], autoFocus, autoComplete = "new-password", onStrength }: {
  value: string;
  onChange: (v: string) => void;
  label: string;
  /** E-mail, nome, domínio: penalizam a senha (é o que o zxcvbn do servidor também faz). */
  userInputs?: string[];
  autoFocus?: boolean;
  autoComplete?: string;
  onStrength?: (s: Strength) => void;
}) {
  const { t } = useTranslation();
  const [show, setShow] = useState(false);
  const st = useMemo(() => {
    const s = measure(value, userInputs);
    onStrength?.(s);
    return s;
    // onStrength fora das deps de propósito: é uma função nova a cada render de quem usa.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [value, userInputs.join("|")]);

  const levels = ["pwWeak", "pwWeak", "pwFair", "pwGood", "pwStrong"] as const;
  return (
    <div className="pwf">
      <label className="pwf-lb">{label}</label>
      <div className="pwf-box">
        <input type={show ? "text" : "password"} value={value} autoFocus={autoFocus} autoComplete={autoComplete}
          onChange={(e) => onChange(e.target.value)} />
        <button type="button" className="pwf-eye" onClick={() => setShow((v) => !v)}
          title={t(show ? "webmail.pwHide" : "webmail.pwShow")}>
          {show ? t("webmail.pwHide") : t("webmail.pwShow")}
        </button>
      </div>
      {/* Quatro traços: enchem-se com a força e ficam vermelhos enquanto não passa. */}
      <div className={"pwf-bar s" + st.score}>
        {[1, 2, 3, 4].map((i) => <i key={i} className={st.score >= i ? "on" : ""} />)}
      </div>
      <div className="pwf-msg">
        {value.length > 0 && <b>{t("webmail." + levels[st.score])}</b>}
        {st.hint && <span>{t("webmail." + st.hint)}</span>}
        {!st.hint && value.length === 0 && <span>{t("webmail.pwHintEmpty")}</span>}
      </div>
    </div>
  );
}
