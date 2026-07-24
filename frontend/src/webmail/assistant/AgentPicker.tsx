import { useTranslation } from "react-i18next";
import type { TFunction } from "i18next";
import { useQuery } from "@tanstack/react-query";
import { assistantApi } from "./api";

// Seletor de agente. Fixar um vale para o resto do fio; "automático" devolve a decisão ao
// encaminhamento (§3.2).
//
// A escolha é visível E reversível de propósito: encaminhamento silencioso deixaria a pessoa sem
// perceber porque é que o assistente às vezes consegue uma coisa e às vezes não.
export function AgentPicker({ pinned, onPin }: { pinned: string; onPin: (key: string) => void }) {
  const { t } = useTranslation();
  const agents = useQuery({
    queryKey: ["assistant-agents"],
    queryFn: () => assistantApi.agents(),
    staleTime: 5 * 60 * 1000, // os agentes de origem são do produto: não mudam durante uma sessão
    retry: false,
  });

  return (
    <select
      className="as-agent"
      value={pinned}
      title={t("assistant.agent.pick")}
      onChange={(e) => onPin(e.target.value)}
    >
      <option value="">{t("assistant.agent.auto")}</option>
      {agents.data?.items.filter((a) => a.enabled).map((a) => (
        <option key={a.key} value={a.key}>{agentLabel(a.key, a.name, t)}</option>
      ))}
    </select>
  );
}

// AgentBadge diz quem respondeu a esta mensagem. Só aparece quando o agente não é o geral: uma
// etiqueta em todas as respostas viraria ruído e deixaria de se notar quando muda.
export function AgentBadge({ agent }: { agent?: string }) {
  const { t } = useTranslation();
  const agents = useQuery({
    queryKey: ["assistant-agents"],
    queryFn: () => assistantApi.agents(),
    staleTime: 5 * 60 * 1000,
    retry: false,
  });
  if (!agent || agent === "assistant") return null;
  const name = agents.data?.items.find((a) => a.key === agent)?.name;
  return <span className="as-agent-badge">{agentLabel(agent, name, t)}</span>;
}

// agentLabel: o nome do cliente quando existe, senão a tradução da chave. Um agente de origem
// nunca traz nome do servidor — o texto visível é do frontend (lei 4.1).
function agentLabel(key: string, name: string | undefined, t: TFunction): string {
  return name || t(`assistant.agent.${key}`, { defaultValue: key });
}
