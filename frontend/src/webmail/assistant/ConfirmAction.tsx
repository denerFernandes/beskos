import { useState } from "react";
import { useTranslation } from "react-i18next";
import { assistantApi, type ConfirmRequest, type PendingAction } from "./api";

// Diálogo de confirmação de uma ação que sai para fora ou destrói (§6.3 do desenho).
//
// Mostra o CONTEÚDO EXATO que vai acontecer, não um resumo — é o único ponto onde um humano vê o
// que vai realmente ser feito, e um resumo que divergisse do que corre daria confiança sem a
// merecer. É deliberadamente incómodo: uma ação, uma decisão.
export function ConfirmAction({ req, onDecided }: { req: ConfirmRequest | PendingAction; onDecided: () => void }) {
  const { t } = useTranslation();
  const [sending, setSending] = useState(false);
  const [failed, setFailed] = useState(false);

  const decide = async (approve: boolean) => {
    setSending(true);
    setFailed(false);
    try {
      await assistantApi.confirm(req.id, approve);
      onDecided();
    } catch {
      // Falhar a entregar a decisão não é aprovar: o painel volta a mostrar os botões.
      setFailed(true);
      setSending(false);
    }
  };

  return (
    <div className="as-confirm">
      <div className="as-confirm-head">
        ⚠ {t("assistant.confirm.title", { action: t(`assistant.tools.${req.tool}`, { defaultValue: req.tool }) })}
      </div>
      <dl className="as-confirm-body">
        {Object.entries(req.args).map(([k, v]) => (
          <div key={k}>
            <dt>{t(`assistant.fields.${k}`, { defaultValue: k })}</dt>
            <dd>{Array.isArray(v) ? v.join(", ") : String(v ?? "")}</dd>
          </div>
        ))}
      </dl>
      {failed && <p className="as-err">{t("assistant.confirm.failed")}</p>}
      <div className="as-confirm-actions">
        <button className="as-reject" disabled={sending} onClick={() => void decide(false)}>
          {t("assistant.confirm.reject")}
        </button>
        <button className="as-approve" disabled={sending} onClick={() => void decide(true)}>
          {t("assistant.confirm.approve")}
        </button>
      </div>
    </div>
  );
}
