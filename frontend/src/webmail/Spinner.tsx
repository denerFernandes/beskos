import { useTranslation } from "react-i18next";

// Spinner de carregamento (anel girando). Usado em toda ação de IA e afins.
export function Spinner({ size = 15 }: { size?: number }) {
  return <span className="spin" style={{ width: size, height: size }} aria-label="loading" role="status" />;
}

// Indicador "IA pensando…" para áreas maiores (card de resumo, painéis).
export function AiThinking({ label }: { label?: string }) {
  const { t } = useTranslation();
  return (
    <span className="ai-think">
      <Spinner size={14} /> {label ?? t("webmail.ai.thinking")}
    </span>
  );
}
