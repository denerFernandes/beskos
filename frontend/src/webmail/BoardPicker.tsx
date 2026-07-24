import { useEffect } from "react";
import { useTranslation } from "react-i18next";
import { useQuery } from "@tanstack/react-query";
import { backend, type Board } from "./backend";

// Lista de quadros do tenant (cache compartilhado com o Kanban).
export function useBoards(): Board[] {
  const q = useQuery({ queryKey: ["boards"], queryFn: () => backend.boards(), retry: false });
  return q.data ?? [];
}

// Seletor de quadro para as ações de IA (extrair tarefas, criar tarefa). Com um único quadro não
// pergunta nada — só seleciona sozinho; com dois ou mais, obriga a escolher onde criar.
export function BoardPicker({ value, onChange }: { value: string; onChange: (id: string) => void }) {
  const { t } = useTranslation();
  const boards = useBoards();

  useEffect(() => {
    if (!value && boards.length) onChange(boards[0].id);
  }, [boards, value, onChange]);

  if (boards.length <= 1) return null;
  return (
    <label className="board-pick">{t("webmail.kanban.board")}
      <select value={value} onChange={(e) => onChange(e.target.value)}>
        {boards.map((b) => <option key={b.id} value={b.id}>{b.name}</option>)}
      </select>
    </label>
  );
}
