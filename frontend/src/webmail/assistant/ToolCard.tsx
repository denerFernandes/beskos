import { useTranslation } from "react-i18next";
import type { ToolStep } from "./api";

// Cartão de uma chamada de ferramenta: o que fez, sobre o quê, e o resultado (§3.3 do desenho).
// Nunca uma frase do modelo a dizer que fez — o registo da chamada real. É o que impede a confiança
// a mais: quem lê vê a ação, não a afirmação.
export function ToolCard({ step }: { step: ToolStep }) {
  const { t } = useTranslation();
  // O nome da ferramenta é uma chave de i18n. Sem tradução, mostra-se o nome técnico em vez de
  // inventar texto — é honesto e diz a quem reporta um problema qual a ferramenta.
  const label = t(`assistant.tools.${step.tool}`, { defaultValue: step.tool });
  const failed = !!step.error_code;

  return (
    <div className={"as-tool" + (failed ? " failed" : "")}>
      <div className="as-tool-head">
        <span className="as-tool-ico" aria-hidden="true">{failed ? "⚠" : "🔍"}</span>
        <b>{label}</b>
      </div>
      <div className="as-tool-args">{summarizeArgs(step.args)}</div>
      <div className="as-tool-out">
        {failed ? t(`server.${step.error_code}`, { defaultValue: t("assistant.genericError") }) : step.summary}
      </div>
    </div>
  );
}

// summarizeArgs mostra os argumentos como o modelo os produziu, em texto curto. Não se esconde nada:
// o que a ferramenta recebeu é parte do que se está a mostrar.
function summarizeArgs(args: unknown): string {
  if (!args || typeof args !== "object") return "";
  return Object.entries(args as Record<string, unknown>)
    .filter(([, v]) => v !== "" && v !== null && v !== undefined)
    .map(([k, v]) => `${k}: ${String(v)}`)
    .join(" · ");
}
