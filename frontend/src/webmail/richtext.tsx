import type { ReactNode } from "react";
import { customEmojiUrl } from "./customEmoji";

// Renderizador de markdown leve (subconjunto estilo Slack), sem dependência externa. Constrói
// nós React (o React já escapa texto → sem risco de XSS). Suporta: **negrito**, _itálico_,
// ~~tachado~~, `código`, ```bloco```, links [t](u) e URLs soltas, @menções, :emoji_custom:, > citação e listas.

const INLINE = /(`[^`]+`)|(\*\*[^*]+\*\*)|(~~[^~]+~~)|(_[^_]+_)|(\[[^\]]+\]\([^)]+\))|(https?:\/\/[^\s]+)|(@[\w.\-]+)|(:[a-z0-9_]{2,32}:)/g;

function renderInline(text: string, keyBase: string): ReactNode[] {
  const out: ReactNode[] = [];
  let last = 0;
  let i = 0;
  for (const m of text.matchAll(INLINE)) {
    const idx = m.index ?? 0;
    if (idx > last) out.push(text.slice(last, idx));
    const tok = m[0];
    const key = `${keyBase}-${i++}`;
    if (tok.startsWith("`")) out.push(<code key={key} className="md-code">{tok.slice(1, -1)}</code>);
    else if (tok.startsWith("**")) out.push(<strong key={key}>{tok.slice(2, -2)}</strong>);
    else if (tok.startsWith("~~")) out.push(<s key={key}>{tok.slice(2, -2)}</s>);
    else if (tok.startsWith("_")) out.push(<em key={key}>{tok.slice(1, -1)}</em>);
    else if (tok.startsWith("[")) {
      const mm = /^\[([^\]]+)\]\(([^)]+)\)$/.exec(tok);
      if (mm) out.push(<a key={key} href={mm[2]} target="_blank" rel="noreferrer">{mm[1]}</a>);
      else out.push(tok);
    } else if (tok.startsWith("http")) out.push(<a key={key} href={tok} target="_blank" rel="noreferrer">{tok}</a>);
    else if (tok.startsWith("@")) out.push(<span key={key} className="md-mention">{tok}</span>);
    else if (tok.startsWith(":")) {
      const url = customEmojiUrl(tok.slice(1, -1));
      out.push(url ? <img key={key} className="cemoji" src={url} alt={tok} title={tok} /> : tok);
    }
    last = idx + tok.length;
  }
  if (last < text.length) out.push(text.slice(last));
  return out;
}

// renderRich transforma o texto em blocos (código, citação, listas, parágrafos com inline).
export function renderRich(text: string): ReactNode {
  const parts = text.split(/```/);
  const blocks: ReactNode[] = [];
  parts.forEach((part, pi) => {
    if (pi % 2 === 1) { // trecho entre ``` = bloco de código
      blocks.push(<pre key={`c${pi}`} className="md-block">{part.replace(/^\n/, "")}</pre>);
      return;
    }
    const lines = part.split("\n");
    let list: ReactNode[] | null = null;
    const flush = () => { if (list) { blocks.push(<ul key={`u${pi}-${blocks.length}`} className="md-list">{list}</ul>); list = null; } };
    lines.forEach((line, li) => {
      const key = `l${pi}-${li}`;
      const bullet = /^\s*[-*]\s+(.*)$/.exec(line);
      if (bullet) { (list ??= []).push(<li key={key}>{renderInline(bullet[1], key)}</li>); return; }
      flush();
      if (/^\s*>\s?/.test(line)) { blocks.push(<blockquote key={key} className="md-quote">{renderInline(line.replace(/^\s*>\s?/, ""), key)}</blockquote>); return; }
      if (line.trim() === "") { if (li > 0 && li < lines.length - 1) blocks.push(<br key={key} />); return; }
      blocks.push(<span key={key} className="md-line">{renderInline(line, key)}</span>);
    });
    flush();
  });
  return <>{blocks}</>;
}
