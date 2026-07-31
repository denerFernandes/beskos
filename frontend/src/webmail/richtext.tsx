import type { ReactNode } from "react";
import { customEmojiUrl } from "./customEmoji";
import { URL_TOKEN, splitTrailing, href as linkHref } from "./links";

// Renderizador de markdown leve (subconjunto estilo Slack), sem dependência externa. Constrói
// nós React (o React já escapa texto → sem risco de XSS). Suporta: **negrito**, _itálico_,
// __sublinhado__, ~~tachado~~, `código`, ```bloco```, links [t](u) e URLs soltas, @menções,
// :emoji_custom:, > citação e listas (com marca e numeradas).

// `__sublinhado__` vem ANTES de `_itálico_`: em alternância a primeira que casa ganha, e com a
// ordem trocada o sublinhado seria lido como itálico com underscores a mais.
// The URL alternative comes from links.ts so the renderer and the unfurl preview agree on what a
// link is. They used to carry a copy each, both demanding "https://", and a link pasted the way
// people paste them ("meet.google.com/abc") was neither linked nor previewed.
const INLINE = new RegExp(
  "(`[^`]+`)|(\\*\\*[^*]+\\*\\*)|(~~[^~]+~~)|(__[^_]+__)|(_[^_]+_)|(\\[[^\\]]+\\]\\([^)]+\\))" +
  `|(${URL_TOKEN})|(@[\\w.\\-]+)|(:[a-z0-9_]{2,32}:)`,
  "gi",
);

// Which alternative fired. Testing the token against the URL pattern is the only reliable way now
// that a link need not start with "http" -- `tok.startsWith("http")` would miss every bare host.
const IS_URL = new RegExp(`^(?:${URL_TOKEN})$`, "i");


// Link para uma tarefa (/mail/kanban/<quadro>/<tarefa>): em vez de um link azul no meio da frase,
// vira um cartão com a chave em destaque — é o que se procura numa conversa cheia de texto.
const TASK_HREF = /^\/mail\/kanban\/[0-9a-fA-F-]{36}\/[0-9a-fA-F-]{36}$/;

// Navegação INTERNA sem recarregar: um href normal faria reload, e no app nativo (Tauri) isso sai da
// aplicação — o servidor de assets não tem fallback de SPA. pushState + popstate: o router reage.
// Ctrl/Cmd/Shift continuam a valer (abrir noutro separador/janela) porque não interceptamos.
function goInternal(e: React.MouseEvent, href: string): void {
  if (e.metaKey || e.ctrlKey || e.shiftKey || e.button !== 0) return;
  e.preventDefault();
  window.history.pushState({}, "", href);
  window.dispatchEvent(new PopStateEvent("popstate"));
}

function TaskRef({ href, label }: { href: string; label: string }): ReactNode {
  // O texto do link vem como "QUA-12 — Título" (é assim que o partilhar e o /task o escrevem);
  // quando não vier, mostramos o rótulo inteiro sem inventar uma chave.
  const m = /^([A-Za-z0-9]{1,8}-\d+)\s*[—–-]\s*([\s\S]+)$/.exec(label);
  return (
    <a className="task-ref" href={href} onClick={(e) => goInternal(e, href)}>
      <svg className="tr-ic" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
        <rect x="3" y="4" width="18" height="16" rx="2" /><path d="M8 10h8M8 14h5" />
      </svg>
      {m ? <><b className="tr-key">{m[1]}</b><span className="tr-title">{m[2]}</span></> : <span className="tr-title">{label}</span>}
    </a>
  );
}

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
    else if (tok.startsWith("__")) out.push(<u key={key}>{tok.slice(2, -2)}</u>);
    else if (tok.startsWith("_")) out.push(<em key={key}>{tok.slice(1, -1)}</em>);
    else if (tok.startsWith("[")) {
      const mm = /^\[([^\]]+)\]\(([^)]+)\)$/.exec(tok);
      if (mm && TASK_HREF.test(mm[2])) out.push(<TaskRef key={key} href={mm[2]} label={mm[1]} />);
      else if (mm && mm[2].startsWith("/")) out.push(<a key={key} href={mm[2]} onClick={(e) => goInternal(e, mm[2])}>{mm[1]}</a>);
      else if (mm) out.push(<a key={key} href={mm[2]} target="_blank" rel="noreferrer">{mm[1]}</a>);
      else out.push(tok);
    } else if (IS_URL.test(tok)) {
      // The sentence's punctuation is not part of the address: "see beskos.com." must not link to a
      // host ending in a dot. Whatever gets trimmed goes back as plain text so the line still reads.
      const [url, tail] = splitTrailing(tok);
      out.push(<a key={key} href={linkHref(url)} target="_blank" rel="noreferrer noopener">{url}</a>);
      if (tail) out.push(tail);
    } else if (tok.startsWith("@")) out.push(<span key={key} className="md-mention">{tok}</span>);
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
    // Uma lista de cada vez: `ordered` diz se o que está a ser acumulado é numerado (<ol>) ou de
    // marcas (<ul>). Trocar de tipo fecha a anterior — senão "1." e "-" caíam na mesma lista.
    let list: ReactNode[] | null = null;
    let ordered = false;
    const flush = () => {
      if (!list) return;
      const key = `u${pi}-${blocks.length}`;
      blocks.push(ordered
        ? <ol key={key} className="md-list md-olist">{list}</ol>
        : <ul key={key} className="md-list">{list}</ul>);
      list = null;
    };
    lines.forEach((line, li) => {
      const key = `l${pi}-${li}`;
      const bullet = /^\s*[-*]\s+(.*)$/.exec(line);
      const numbered = /^\s*\d+[.)]\s+(.*)$/.exec(line);
      if (bullet || numbered) {
        const isOrdered = !bullet;
        if (list && ordered !== isOrdered) flush();
        ordered = isOrdered;
        (list ??= []).push(<li key={key}>{renderInline((bullet ?? numbered)![1], key)}</li>);
        return;
      }
      flush();
      if (/^\s*>\s?/.test(line)) { blocks.push(<blockquote key={key} className="md-quote">{renderInline(line.replace(/^\s*>\s?/, ""), key)}</blockquote>); return; }
      if (line.trim() === "") { if (li > 0 && li < lines.length - 1) blocks.push(<br key={key} />); return; }
      blocks.push(<span key={key} className="md-line">{renderInline(line, key)}</span>);
    });
    flush();
  });
  return <>{blocks}</>;
}
