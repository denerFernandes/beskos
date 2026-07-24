import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useQuery, useMutation } from "@tanstack/react-query";
import { backend, colleaguesApi, type Task, type Colleague } from "./backend";
import { Spinner, AiThinking } from "./Spinner";
import { BoardPicker } from "./BoardPicker";
import { toast } from "./toast";

// Modal "Extrair tarefas": a IA lê o e-mail (backend.extractTasks) e permite mandar ao quadro real.
export function ExtractTasksModal({
  subject, body, sourceEmailId, onClose,
}: {
  subject: string;
  body: string;
  sourceEmailId: string;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const [checked, setChecked] = useState<Record<number, boolean>>({});
  const [boardId, setBoardId] = useState("");

  // Colegas do tenant para casar o responsável SUGERIDO pela IA (texto) com um utilizador real →
  // assignee_id. Sem isto o responsável era descartado (o backend só aceita id, não nome).
  const colleaguesQ = useQuery({ queryKey: ["colleagues"], queryFn: () => colleaguesApi.list() });
  const resolveAssignee = (name: string): string => {
    const n = name.trim().toLowerCase();
    if (!n) return "";
    const list: Colleague[] = colleaguesQ.data ?? [];
    const hit = list.find((c) => c.name.toLowerCase() === n || c.email.toLowerCase() === n)
      ?? list.find((c) => c.name.toLowerCase().startsWith(n) || c.email.toLowerCase().startsWith(n));
    return hit?.id ?? "";
  };

  const tasks = useQuery({
    queryKey: ["extract", subject, body],
    queryFn: async () => {
      const r = await backend.extractTasks(subject, body);
      setChecked(Object.fromEntries(r.tasks.map((_, i) => [i, true])));
      return r.tasks;
    },
    retry: false,
  });

  const add = useMutation({
    mutationFn: async (list: Task[]) => {
      // Quadro escolhido no seletor; com um único quadro o seletor já o pré-seleciona.
      const bid = boardId || (await backend.boards())[0].id;
      const board = await backend.board(bid);
      const col = board.columns[0].column.id; // 1ª coluna ("A fazer")
      for (const tk of list) {
        await backend.createCard(bid, {
          column_id: col, title: tk.title,
          description: tk.due ? `${t("webmail.ai.due")}: ${tk.due}` : "",
          assignee_id: resolveAssignee(tk.assignee), due: tk.due, source_email_id: sourceEmailId,
        });
      }
    },
    onSuccess: (_r, list) => { toast(t("webmail.ai.addedTasks", { n: list.length })); onClose(); },
  });

  const selected = (tasks.data ?? []).filter((_, i) => checked[i]);

  return (
    <div className="tk-bg open" onMouseDown={onClose}>
      <div className="tk" onMouseDown={(e) => e.stopPropagation()}>
        <header><span>✦ {t("webmail.ai.extractedTasks")}</span><button className="ib" title={t("common.close")} onClick={onClose}>✕</button></header>
        <div className="body">
          <BoardPicker value={boardId} onChange={setBoardId} />
          {tasks.isLoading && <div className="ai-think-row"><AiThinking /></div>}
          {tasks.isError && <p className="err">{(tasks.error as Error).message}</p>}
          {(tasks.data ?? []).map((tk, i) => (
            <div className="task" key={i}>
              <div className={"cb" + (checked[i] ? " on" : "")} onClick={() => setChecked((c) => ({ ...c, [i]: !c[i] }))}>
                {checked[i] ? "✓" : ""}
              </div>
              <div>
                <div className="tt">{tk.title}</div>
                <div className="tm2">📅 {tk.due || "—"} · 👤 {tk.assignee ? (resolveAssignee(tk.assignee) ? tk.assignee : `${tk.assignee} ${t("webmail.ai.assigneeUnmatched")}`) : "—"}</div>
              </div>
            </div>
          ))}
        </div>
        <footer>
          <button className="chipbtn" onClick={onClose}>{t("webmail.cancel")}</button>
          <button className="add" disabled={add.isPending || selected.length === 0} onClick={() => add.mutate(selected)}>
            {add.isPending ? <Spinner size={14} /> : t("webmail.ai.addToBoard")}
          </button>
        </footer>
      </div>
    </div>
  );
}
