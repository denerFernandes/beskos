import { useState } from "react";
import { useTranslation } from "react-i18next";
import { AssistantPanel } from "./AssistantPanel";
import { useAssistant } from "./useAssistant";
import { useAssistantTasks } from "./TasksPanel";

// Bolha do assistente: botão redondo fixo em baixo à direita, acima de tudo o resto (§3.1).
//
// Montada UMA VEZ no App, fora do <main>: o estado sobrevive à navegação, portanto mudar de área a
// meio de uma pergunta não perde a conversa. A regra de visibilidade é de quem a monta — este
// componente não sabe em que página está, e não devia saber.
export function AssistantBubble({ hidden = false }: { hidden?: boolean }) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const chat = useAssistant();
  // Trabalho em fundo conta como trabalho em curso: a bolha mostra-o em TODA a aplicação, mesmo
  // fechada e mesmo depois de recarregar a página (§3.6).
  const { active } = useAssistantTasks();

  // Escondida ≠ desmontada: quem monta decide onde não aparece, mas a conversa sobrevive à visita
  // às Definições. Os hooks correm sempre — a saída antecipada vem depois deles, de propósito.
  if (hidden) return null;

  return (
    <>
      {open && <AssistantPanel chat={chat} onClose={() => setOpen(false)} />}
      <button
        className={"as-bubble" + (open ? " on" : "")}
        title={t("assistant.title")}
        aria-label={t("assistant.title")}
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
      >
        {/* Com trabalho em curso a bolha mostra-o mesmo fechada (§3.1). */}
        {chat.busy || active > 0 ? <span className="as-bubble-work" /> : "✦"}
        {!open && active > 0 && <span className="as-bubble-count">{active}</span>}
        {/* Sessão minimizada (há conversa a decorrer, sem tarefas em fundo): um ponto discreto a
            dizer que há uma sessão ativa para retomar. */}
        {!open && active === 0 && chat.messages.length > 0 && <span className="as-bubble-session" />}
      </button>
    </>
  );
}
