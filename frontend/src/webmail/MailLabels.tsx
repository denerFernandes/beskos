import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { backend, labelKeyword } from "./backend";
import { IcPlus, IcCheck, IcTrash } from "../panel/icons";
import { toast } from "./toast";

// Paleta dos marcadores (mesma linguagem visual das tags/eventos).
const LABEL_COLORS = ["#116466", "#c0392b", "#8e44ad", "#2980b9", "#d68910", "#27ae60", "#2c3e50"];

// Ícone de etiqueta inline (não há IcTag no conjunto).
export function TagIcon({ size = 15 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
      <path d="M20.59 13.41 13.42 20.6a2 2 0 0 1-2.83 0L2 12V2h10l8.59 8.59a2 2 0 0 1 0 2.82Z" />
      <circle cx="7" cy="7" r="1.2" fill="currentColor" stroke="none" />
    </svg>
  );
}

// LabelChips: os marcadores aplicados a um e-mail, lidos das keywords JMAP.
export function LabelChips({ keywords }: { keywords?: Record<string, boolean> }) {
  const labelsQ = useQuery({ queryKey: ["labels"], queryFn: () => backend.labels() });
  const applied = (labelsQ.data ?? []).filter((l) => keywords?.[labelKeyword(l.id)]);
  if (!applied.length) return null;
  return (
    <span className="mlab-chips">
      {applied.map((l) => (
        <span key={l.id} className="mlab-chip" style={{ background: l.color + "22", color: l.color, borderColor: l.color + "66" }}>{l.name}</span>
      ))}
    </span>
  );
}

// LabelMenu: botão + dropdown para aplicar/remover marcadores no e-mail aberto e criar/apagar do
// catálogo. onToggle aplica no JMAP e recarrega o e-mail (fica a cargo do reader).
export function LabelMenu({ keywords, onToggle }: {
  keywords?: Record<string, boolean>;
  onToggle: (labelId: string, on: boolean) => void;
}) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [open, setOpen] = useState(false);
  const [name, setName] = useState("");
  const [color, setColor] = useState(LABEL_COLORS[0]);
  const labelsQ = useQuery({ queryKey: ["labels"], queryFn: () => backend.labels() });
  const labels = labelsQ.data ?? [];

  const create = async () => {
    const n = name.trim();
    if (!n) return;
    try {
      const l = await backend.createLabel(n, color);
      await qc.invalidateQueries({ queryKey: ["labels"] });
      setName("");
      onToggle(l.id, true); // já aplica o novo marcador ao e-mail aberto
    } catch (e) { toast((e as Error).message); }
  };
  const remove = async (id: string) => {
    // Apaga do catálogo; a keyword em e-mails antigos deixa de ter nome/cor (some da UI). Ok.
    try { await backend.deleteLabel(id); await qc.invalidateQueries({ queryKey: ["labels"] }); }
    catch (e) { toast((e as Error).message); }
  };

  return (
    <div className="mlab-wrap">
      <button className={"chipbtn" + (open ? " on" : "")} onClick={() => setOpen((v) => !v)}>
        <TagIcon /> {t("webmail.labels.label")}
      </button>
      {open && (
        <div className="mlab-menu" onMouseLeave={() => setOpen(false)}>
          <div className="mlab-list">
            {labels.length === 0 && <div className="mlab-empty">{t("webmail.labels.empty")}</div>}
            {labels.map((l) => {
              const on = !!keywords?.[labelKeyword(l.id)];
              return (
                <div className="mlab-item" key={l.id}>
                  <button className={"mlab-toggle" + (on ? " on" : "")} onClick={() => onToggle(l.id, !on)}>
                    <span className="mlab-dot" style={{ background: l.color }} />
                    <span className="mlab-name">{l.name}</span>
                    {on && <IcCheck size={14} />}
                  </button>
                  <button className="ib" title={t("webmail.labels.deleteLabel")} onClick={() => remove(l.id)}><IcTrash size={13} /></button>
                </div>
              );
            })}
          </div>
          <div className="mlab-new">
            <div className="mlab-colors">
              {LABEL_COLORS.map((c) => (
                <button key={c} className={"mlab-cbtn" + (c === color ? " on" : "")} style={{ background: c }} onClick={() => setColor(c)} aria-label={c} />
              ))}
            </div>
            <div className="mlab-newrow">
              <input value={name} placeholder={t("webmail.labels.newPlaceholder")} onChange={(e) => setName(e.target.value)}
                onKeyDown={(e) => { if (e.key === "Enter") create(); }} />
              <button className="add" title={t("common.create")} disabled={!name.trim()} onClick={create}><IcPlus size={14} /></button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
