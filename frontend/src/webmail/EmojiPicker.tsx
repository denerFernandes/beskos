import { useEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { EMOJI_GROUPS, searchEmoji, type EmojiDef } from "./emoji";
import { customEmojiList } from "./customEmoji";
import { EmojiManager } from "./EmojiManager";
import { IcGear } from "../panel/icons";

const RECENT_KEY = "mailin.emoji.recent";

// Últimos usados: é o que faz um seletor deixar de ser uma lista e passar a servir. Fica no
// browser de propósito — é preferência de teclado, não dado da empresa.
function readRecent(): string[] {
  try {
    return JSON.parse(localStorage.getItem(RECENT_KEY) ?? "[]").slice(0, 16);
  } catch {
    return [];
  }
}
export function pushRecent(ch: string) {
  const next = [ch, ...readRecent().filter((x) => x !== ch)].slice(0, 16);
  try {
    localStorage.setItem(RECENT_KEY, JSON.stringify(next));
  } catch { /* modo privado: sem recentes, o resto continua a funcionar */ }
}

// Seletor de emoji. `anchor` é o botão que o abriu: o popup é `position:fixed` para escapar do
// overflow:hidden da caixa do compositor, então a posição vem daqui, não do CSS.
export function EmojiPicker({ anchor, onPick, onClose }: {
  anchor: DOMRect | null; onPick: (ch: string) => void; onClose: () => void;
}) {
  const { t } = useTranslation();
  const [q, setQ] = useState("");
  const [group, setGroup] = useState(0);
  const [managing, setManaging] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
  const recent = useMemo(readRecent, []);
  const custom = useMemo(() => customEmojiList(), []);
  const customHits = custom.filter((e) => !q.trim() || e.name.includes(q.trim().toLowerCase()));

  // Fecha ao clicar fora ou com Escape — sem isto o popup fica preso no ecrã.
  //
  // O listener entra só no tick seguinte: o mousedown que ABRIU o seletor ainda está a subir para
  // o document quando este efeito corre, e um listener registado agora apanharia esse mesmo evento
  // — o popup abria e fechava no mesmo clique.
  useEffect(() => {
    const onDown = (e: MouseEvent) => { if (!ref.current?.contains(e.target as Node)) onClose(); };
    const onKey = (e: KeyboardEvent) => { if (e.key === "Escape") onClose(); };
    const id = setTimeout(() => document.addEventListener("mousedown", onDown), 0);
    document.addEventListener("keydown", onKey);
    return () => {
      clearTimeout(id);
      document.removeEventListener("mousedown", onDown);
      document.removeEventListener("keydown", onKey);
    };
  }, [onClose]);

  const list: EmojiDef[] = q.trim() ? searchEmoji(q) : EMOJI_GROUPS[group].emojis;

  // Acima do botão, e nunca a sair pela esquerda nem pela direita da janela.
  const style: React.CSSProperties = anchor
    ? { left: Math.max(8, Math.min(anchor.left, window.innerWidth - 240)), top: Math.max(8, anchor.top - 268) }
    : {};

  return (
    <div className="emoji-pop up emoji-picker" ref={ref} style={style} onMouseDown={(e) => e.stopPropagation()}>
      <div className="emoji-top">
        <input className="emoji-search" autoFocus value={q} placeholder={t("chat.emojiSearch")}
          onChange={(e) => setQ(e.target.value)} />
        <button className="emoji-manage" title={t("chat.emojiManage")} onMouseDown={(e) => { e.preventDefault(); setManaging(true); }}><IcGear size={15} /></button>
      </div>
      {managing && createPortal(<EmojiManager onClose={() => setManaging(false)} />, document.body)}

      {customHits.length > 0 && (
        <div className="emoji-grid emoji-custom-sec">
          <div className="emoji-sec">{t("chat.emojiCustom")}</div>
          <div className="emoji-row">
            {customHits.map((e) => (
              <button key={e.name} title={`:${e.name}:`} onMouseDown={(ev) => { ev.preventDefault(); onPick(`:${e.name}:`); }}>
                <img className="cemoji" src={e.url} alt={e.name} />
              </button>
            ))}
          </div>
        </div>
      )}

      {!q.trim() && (
        <div className="emoji-tabs">
          {EMOJI_GROUPS.map((g, i) => (
            <button key={g.key} className={group === i ? "on" : ""} title={t(`chat.emojiGroup_${g.key}`)}
              onMouseDown={(e) => { e.preventDefault(); setGroup(i); }}>{g.icon}</button>
          ))}
        </div>
      )}

      <div className="emoji-grid">
        {!q.trim() && group === 0 && recent.length > 0 && (
          <>
            <div className="emoji-sec">{t("chat.emojiRecent")}</div>
            <div className="emoji-row">
              {recent.map((ch) => (
                <button key={"r" + ch} onMouseDown={(e) => { e.preventDefault(); onPick(ch); }}>{ch}</button>
              ))}
            </div>
            <div className="emoji-sec">{t(`chat.emojiGroup_${EMOJI_GROUPS[0].key}`)}</div>
          </>
        )}
        <div className="emoji-row">
          {list.map((e) => (
            <button key={e.ch} title={e.name} onMouseDown={(ev) => { ev.preventDefault(); onPick(e.ch); }}>{e.ch}</button>
          ))}
        </div>
        {list.length === 0 && <p className="emoji-none">{t("chat.emojiNone")}</p>}
      </div>
    </div>
  );
}
