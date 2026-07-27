import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { useQuery } from "@tanstack/react-query";
import { backend, type CardHit } from "./backend";
import { Spinner } from "./Spinner";
import { IcSearch, IcX } from "../panel/icons";

// Seletor de tarefa do chat (comando /task): procura por chave (QUA-12), número solto ou título e
// devolve a tarefa escolhida a quem chamou. A busca é do servidor porque só ele sabe que quadros a
// pessoa pode ver — filtrar no cliente mostraria títulos de quadros fechados.
export function TaskPickerModal({ onPick, onClose }: { onPick: (c: CardHit) => void; onClose: () => void }) {
  const { t } = useTranslation();
  const [q, setQ] = useState("");
  const [term, setTerm] = useState("");
  // Espera a pessoa parar de escrever: sem isto era um pedido por tecla.
  useEffect(() => { const id = setTimeout(() => setTerm(q.trim()), 220); return () => clearTimeout(id); }, [q]);

  const hits = useQuery({
    queryKey: ["card-search", term],
    queryFn: () => backend.searchCards(term),
    enabled: term.length >= 1,
    retry: false,
  });
  const cards = hits.data?.cards ?? [];
  // Navegação por teclado (mesma lógica do menu de comandos): escrever, descer com as setas e
  // escolher com Enter, sem tirar as mãos do teclado.
  const [idx, setIdx] = useState(0);
  useEffect(() => { setIdx(0); }, [term]);
  const onKeys = (e: React.KeyboardEvent) => {
    if (e.key === "Escape") { onClose(); return; }
    if (cards.length === 0) return;
    if (e.key === "ArrowDown") { e.preventDefault(); setIdx((i) => (i + 1) % cards.length); }
    else if (e.key === "ArrowUp") { e.preventDefault(); setIdx((i) => (i - 1 + cards.length) % cards.length); }
    else if (e.key === "Enter") { e.preventDefault(); onPick(cards[Math.min(idx, cards.length - 1)]); }
  };

  return (
    <div className="tk-bg open" onMouseDown={onClose}>
      <div className="tk modal-sm" onMouseDown={(e) => e.stopPropagation()}>
        <header>
          <span>{t("chat.taskPickTitle")}</span>
          <button className="ib" title={t("common.close")} onClick={onClose}><IcX size={15} /></button>
        </header>
        <div className="tk-body">
          <label className="dp-search">
            <IcSearch size={14} />
            <input autoFocus value={q} placeholder={t("chat.taskPickPh")} onChange={(e) => setQ(e.target.value)} onKeyDown={onKeys} />
          </label>

          <div className="task-pick-list">
            {term.length === 0 && <p className="desc">{t("chat.taskPickHint")}</p>}
            {hits.isFetching && <div className="loading"><Spinner size={16} /></div>}
            {term.length > 0 && !hits.isFetching && cards.length === 0 && <p className="desc">{t("chat.taskPickEmpty")}</p>}
            {cards.map((c, i) => (
              <button key={c.id} className={"task-hit" + (i === idx ? " on" : "")}
                onMouseEnter={() => setIdx(i)} onClick={() => onPick(c)}>
                <b className="th-key">{String(c.card_key)}</b>
                <span className="th-main">
                  <span className="th-title">{c.title}</span>
                  <span className="th-meta">
                    {c.board_name} · {c.column_name}
                    {c.assignee_name ? ` · ${c.assignee_name}` : ""}
                    {c.due ? ` · ${c.due}` : ""}
                  </span>
                </span>
              </button>
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}
