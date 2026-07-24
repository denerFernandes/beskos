import { useRef } from "react";
import { useTranslation } from "react-i18next";
import { IcLink } from "../panel/icons";

// Editor de texto rico PARTILHADO (markdown, mesmo dialeto do chat e da descrição de card). Uma barra
// de formatação envolve a seleção do textarea com a marcação; guarda-se texto portável e mostra-se
// com renderRich. Usado em TODO o campo de texto longo: descrição, comentários, notas… — para o
// utilizador ter sempre o mesmo editor.

// MdToolbar: só a barra. Para casos onde o textarea já existe e tem lógica própria (ex.: composer de
// comentários com @menções) — chama onWrap(before, after) para envolver a seleção.
export function MdToolbar({ onWrap }: { onWrap: (before: string, after: string) => void }) {
  const { t } = useTranslation();
  const b = (title: string, before: string, after: string, node: React.ReactNode) => (
    <button type="button" title={title} onMouseDown={(e) => { e.preventDefault(); onWrap(before, after); }}>{node}</button>
  );
  return (
    <div className="md-tools">
      {b("Bold", "**", "**", <b>B</b>)}
      {b("Italic", "_", "_", <i>I</i>)}
      {b("Strikethrough", "~~", "~~", <s>S</s>)}
      {b("Code", "`", "`", <>{"<>"}</>)}
      {b("List", "\n- ", "", "•")}
      {b(t("webmail.compose.insertLink"), "[", "](https://)", <IcLink size={13} />)}
    </div>
  );
}

// wrapSelection envolve a seleção atual de um textarea com marcação e devolve o novo valor, deixando
// a seleção onde estava (útil para partilhar a lógica de "wrap" fora do MdEditor).
export function wrapSelection(ta: HTMLTextAreaElement | null, value: string, before: string, after: string, onChange: (v: string) => void) {
  if (!ta) return;
  const s = ta.selectionStart, e = ta.selectionEnd;
  onChange(value.slice(0, s) + before + value.slice(s, e) + after + value.slice(e));
  requestAnimationFrame(() => { ta.focus(); ta.selectionStart = s + before.length; ta.selectionEnd = e + before.length; });
}

export function MdEditor({ value, onChange, placeholder, autoFocus, onKeyDown, taRef, minHeight }: {
  value: string; onChange: (v: string) => void; placeholder?: string; autoFocus?: boolean;
  onKeyDown?: (e: React.KeyboardEvent<HTMLTextAreaElement>) => void;
  taRef?: React.RefObject<HTMLTextAreaElement>; minHeight?: number;
}) {
  const inner = useRef<HTMLTextAreaElement>(null);
  const ref = taRef ?? inner;
  return (
    <div className="md-editor">
      <MdToolbar onWrap={(b, a) => wrapSelection(ref.current, value, b, a, onChange)} />
      <textarea ref={ref} className="md-ta" value={value} autoFocus={autoFocus}
        style={minHeight ? { minHeight } : undefined}
        onChange={(e) => onChange(e.target.value)} onKeyDown={onKeyDown} placeholder={placeholder} />
    </div>
  );
}
