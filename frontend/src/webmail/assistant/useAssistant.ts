import { useCallback, useRef, useState } from "react";
import { useQueryClient, type QueryClient } from "@tanstack/react-query";
import { assistantApi, type ChatEvent, type ConfirmRequest, type ToolStep } from "./api";

// Quando o assistente escreve no correio (envia, rascunha), as listas do webmail já em cache ficam
// desatualizadas — o e-mail enviado continuaria a aparecer em Rascunhos até um refetch. Invalidamos
// as queries afetadas assim que o passo corre, para a UI refletir a ação na hora.
// Cada ferramenta de escrita mapeia para as chaves de query que ficam obsoletas quando ela corre.
const INVALIDATE_ON: Record<string, string[]> = {
  "mail.send": ["emails", "mailboxes", "thread", "today-emails", "contacts"],
  "mail.draft": ["emails", "mailboxes", "thread", "today-emails"],
  "calendar.create": ["events", "meetings"],
  "tasks.create": ["boards", "board"],
};

function invalidateForStep(qc: QueryClient, step: ToolStep) {
  if (step.error_code) return; // ação falhou: nada mudou, nada a refrescar
  for (const key of INVALIDATE_ON[step.tool] ?? []) {
    void qc.invalidateQueries({ queryKey: [key] });
  }
}

// Uma mensagem tal como o painel a mostra. É o mesmo formato para o que veio do servidor e para o
// que está a chegar ao vivo — assim reabrir uma conversa mostra exatamente o que se viu ao vivo.
export interface ChatMessage {
  role: "user" | "assistant";
  // agent é quem respondeu — a escolha do encaminhamento é sempre visível (§3.2).
  agent?: string;
  content: string;
  // Factos guardados/recusados neste turno (§6.5). Mostrados na conversa: memória que cresce sem
  // se ver é memória que ninguém corrige (§8.2).
  savedFacts?: { fact: string }[];
  refusedFacts?: { fact: string; reason: string }[];
  steps: ToolStep[];
  stopped?: string;
  // errorCode é uma falha do pedido, não do modelo. Chave de i18n (lei 4.6).
  errorCode?: string;
}

// useAssistant guarda o estado da conversa aberta e trata do fio SSE.
//
// Vive no componente montado uma vez no App (fora do <main>), portanto sobrevive à navegação: mudar
// de área não perde o que se estava a perguntar.
export function useAssistant() {
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [conversationId, setConversationId] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  // A ação à espera de decisão. Uma de cada vez, por construção: a execução PÁRA no servidor até
  // haver resposta, portanto nunca chega uma segunda antes de esta ser decidida (§6.3).
  const [pending, setPending] = useState<ConfirmRequest | null>(null);
  // pinned vazio = o encaminhamento decide a cada turno. Fixar vale para o resto do fio, e é
  // reversível: o utilizador manda sempre mais do que o modelo.
  const [pinned, setPinned] = useState("");
  const abortRef = useRef<AbortController | null>(null);
  const qc = useQueryClient();

  // patchLast altera a última mensagem (a do assistente, que está a ser escrita ao vivo).
  const patchLast = useCallback((fn: (m: ChatMessage) => ChatMessage) => {
    setMessages((prev) => (prev.length === 0 ? prev : [...prev.slice(0, -1), fn(prev[prev.length - 1])]));
  }, []);

  const send = useCallback(async (text: string) => {
    const message = text.trim();
    if (!message || busy) return;
    setBusy(true);
    setMessages((prev) => [
      ...prev,
      { role: "user", content: message, steps: [] },
      { role: "assistant", content: "", steps: [] },
    ]);

    const ctrl = new AbortController();
    abortRef.current = ctrl;
    try {
      await assistantApi.chat(
        { message, agent: pinned || undefined, conversation_id: conversationId ?? undefined },
        (ev: ChatEvent) => {
          switch (ev.kind) {
            case "conversation":
              setConversationId(ev.conversation_id);
              // Quem vai responder, à vista, antes de qualquer ferramenta correr.
              patchLast((m) => ({ ...m, agent: ev.agent }));
              break;
            case "delta":
              patchLast((m) => ({ ...m, content: m.content + ev.text }));
              break;
            case "step":
              // O cartão de ação a chegar significa que a ação já foi decidida e corrida.
              setPending(null);
              patchLast((m) => ({ ...m, steps: [...m.steps, ev.step] }));
              // A ação já mudou os dados no servidor: refresca as listas do webmail afetadas.
              invalidateForStep(qc, ev.step);
              break;
            case "confirm":
              setPending(ev.confirm);
              break;
            case "memory":
              patchLast((m) => ({ ...m, savedFacts: ev.saved, refusedFacts: ev.refused }));
              break;
            case "done":
              setPending(null);
              // O run final manda: os deltas podem ter-se perdido a meio, o servidor tem a verdade.
              patchLast((m) => ({ ...m, content: ev.run.answer || m.content, steps: ev.run.steps ?? m.steps, stopped: ev.run.stopped }));
              break;
            case "error":
              patchLast((m) => ({ ...m, errorCode: ev.error_code }));
              break;
          }
        },
        ctrl.signal,
      );
    } catch {
      // Cancelar não é falhar: quem fechou o painel não precisa de ver um erro.
      if (!ctrl.signal.aborted) patchLast((m) => ({ ...m, errorCode: "assistant.failed" }));
    } finally {
      abortRef.current = null;
      setPending(null);
      setBusy(false);
    }
  }, [busy, conversationId, patchLast, pinned, qc]);

  // sendBackground enfileira o pedido no servidor. Não devolve texto: devolve uma tarefa, e o
  // progresso passa a ver-se no separador de Tarefas (§3.6).
  const sendBackground = useCallback(async (text: string) => {
    const message = text.trim();
    if (!message) return;
    const res = await assistantApi.background({
      message, agent: pinned || undefined, conversation_id: conversationId ?? undefined,
    });
    setConversationId(res.conversation_id);
    setMessages((prev) => [...prev, { role: "user", content: message, steps: [] }]);
  }, [conversationId, pinned]);

  // stop only cuts the client-side stream. Work already done stays recorded on the server — this
  // does not cancel execution on the backend, it only stops listening to it. True cancellation is
  // not implemented yet.
  const stop = useCallback(() => {
    abortRef.current?.abort();
    abortRef.current = null;
    setPending(null);
    setBusy(false);
  }, []);

  const reset = useCallback(() => {
    stop();
    setMessages([]);
    setConversationId(null);
    setPinned("");
  }, [stop]);

  // open carrega uma conversa do histórico com os seus cartões de ação.
  const open = useCallback(async (id: string) => {
    stop();
    const conv = await assistantApi.conversation(id);
    setConversationId(conv.id);
    setPinned(conv.pinned_agent ?? "");
    setMessages(conv.messages.map((m) => ({
      role: m.role, agent: m.agent, content: m.content, steps: m.steps ?? [], stopped: m.stopped,
    })));
  }, [stop]);

  return { messages, conversationId, busy, pending, pinned, setPinned, send, sendBackground, stop, reset, open };
}
