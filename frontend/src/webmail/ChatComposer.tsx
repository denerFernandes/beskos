import { forwardRef, useImperativeHandle, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { backend, type ChatMember, type ChatAttachment, type CardHit } from "./backend";
import { LinkModal } from "./LinkModal";
import { useUserSettings } from "./useSettings";
import { toast } from "./toast";
import { EmojiPicker, pushRecent } from "./EmojiPicker";
import { GifPicker } from "./GifPicker";
import { Avatar } from "./Avatar";
import { IcPlus, IcX, IcPaperclip, IcLink, IcList, IcClock, IcFolder, IcColumns, IcBold, IcItalic, IcUnderline, IcStrike, IcListOrdered, IcQuote, IcCode, IcCodeBlock, IcAt, IcSlash, IcTextSize, IcEmoji, IcSend, IcUpload } from "../panel/icons";
import { TaskPickerModal } from "./TaskPickerModal";
import { DrivePickerDrawer } from "./DrivePickerDrawer";
import { applyShortcodes, applySlash, SLASH_COMMANDS, LS_FMT } from "./chatUtils";
import { TimeMenu } from "./ChatBits";

export interface ComposerHandle { addFiles: (files: FileList | File[]) => void }

export const Composer = forwardRef<ComposerHandle, {
  channelId: string; members: ChatMember[]; onTyping: () => void; onSent: () => void; parentId?: string; dm?: boolean;
}>(function Composer({ channelId, members, onTyping, onSent, parentId = "", dm = false }, ref) {
  const { t } = useTranslation();
  const settings = useUserSettings(); // settings.chat.enterSends
  const [text, setText] = useState("");
  const [mentionQ, setMentionQ] = useState<string | null>(null);
  const [slashQ, setSlashQ] = useState<string | null>(null); // texto depois do "/" (menu de comandos)
  const [slashIdx, setSlashIdx] = useState(0);               // item destacado no menu (setas ↑/↓)
  const [taskPick, setTaskPick] = useState(false);           // seletor de tarefa (/task)
  const [emoji, setEmoji] = useState<DOMRect | null>(null);
  const [gif, setGif] = useState<DOMRect | null>(null);
  const [drivePick, setDrivePick] = useState(false);
  const [atts, setAtts] = useState<ChatAttachment[]>([]);
  const [uploading, setUploading] = useState(0);
  const [sched, setSched] = useState(false);
  // Menu do "+": guarda o retângulo do botão (null = fechado). A posição vem do JS porque o menu
  // é `fixed` — dentro da .rich-box (overflow:hidden) ficaria recortado, como o seletor de emojis.
  const [addMenu, setAddMenu] = useState<DOMRect | null>(null);
  // Seleção guardada enquanto o modal do link está aberto (null = modal fechado).
  const [linkSel, setLinkSel] = useState<{ start: number; end: number; text: string } | null>(null);
  // Barra de formatação visível? É preferência de quem escreve (o "Aa" do Slack) e vive no
  // aparelho: quem escreve markdown de cor deixa-a escondida e ganha altura para a mensagem.
  const [showFmt, setShowFmt] = useState(() => { try { return localStorage.getItem(LS_FMT) !== "0"; } catch { return true; } });
  const toggleFmt = () => setShowFmt((v) => { try { localStorage.setItem(LS_FMT, v ? "0" : "1"); } catch { /* modo privado */ } return !v; });
  const mentioned = useRef<Set<string>>(new Set());
  const lastTyping = useRef(0);
  const ta = useRef<HTMLTextAreaElement>(null);
  const fileIn = useRef<HTMLInputElement>(null);

  async function onFiles(files: FileList | File[] | null) {
    if (!files) return;
    for (const f of Array.from(files)) {
      setUploading((n) => n + 1);
      try { const sf = await backend.uploadFile(f); setAtts((a) => [...a, { file_id: sf.id, name: sf.name, size: sf.size }]); }
      catch (e) { toast((e as Error).message); }
      finally { setUploading((n) => n - 1); }
    }
  }

  // Permite que o painel (zona de drop) injete arquivos arrastados neste composer.
  useImperativeHandle(ref, () => ({ addFiles: (files) => { void onFiles(files); } }));

  // Envolve a seleção com marcadores markdown (negrito, itálico, etc.), estilo Slack.
  const wrap = (before: string, after = before) => {
    const el = ta.current; if (!el) return;
    const s = el.selectionStart, e = el.selectionEnd, val = el.value;
    const sel = val.slice(s, e) || t("chat.text");
    const next = val.slice(0, s) + before + sel + after + val.slice(e);
    setText(next);
    requestAnimationFrame(() => { el.focus(); el.setSelectionRange(s + before.length, s + before.length + sel.length); });
  };
  // "Adicionar link": guarda a seleção ANTES de abrir o modal — ao focar o campo de texto a caixa
  // perde a seleção, e sem ela não saberíamos onde inserir nem o que a pessoa tinha marcado.
  const openLink = () => {
    const el = ta.current;
    const start = el?.selectionStart ?? text.length;
    const end = el?.selectionEnd ?? text.length;
    setLinkSel({ start, end, text: text.slice(start, end) });
  };
  const insertLink = (label: string, url: string) => {
    const sel = linkSel;
    setLinkSel(null);
    if (!sel) return;
    const md = `[${label}](${url})`;
    setText(text.slice(0, sel.start) + md + text.slice(sel.end));
    const caret = sel.start + md.length;
    requestAnimationFrame(() => { const el = ta.current; el?.focus(); el?.setSelectionRange(caret, caret); });
  };
  // Insere "@" ou "/" na posição do cursor e devolve o foco: escrever o carácter é o que ABRE o
  // menu (menções/comandos), então o botão faz exatamente o que a pessoa faria pelo teclado.
  const insertAt = (ch: string) => {
    const el = ta.current;
    const pos = el ? el.selectionStart : text.length;
    // O menu de comandos só abre com "/" no início da mensagem — é a regra do onChange.
    const at = ch === "/" ? 0 : pos;
    const next = text.slice(0, at) + ch + text.slice(at);
    onChange(next);
    requestAnimationFrame(() => { el?.focus(); el?.setSelectionRange(at + 1, at + 1); });
  };
  const insertEmoji = (em: string) => {
    const el = ta.current; const pos = el ? el.selectionStart : text.length;
    setText((v) => v.slice(0, pos) + em + v.slice(pos)); setEmoji(null); pushRecent(em);
    requestAnimationFrame(() => { el?.focus(); el?.setSelectionRange(pos + em.length, pos + em.length); });
  };

  const suggestions = useMemo(() => {
    if (mentionQ === null) return [];
    const q = mentionQ.toLowerCase();
    return members.filter((m) => m.name.toLowerCase().includes(q) || m.email.toLowerCase().includes(q)).slice(0, 5);
  }, [mentionQ, members]);
  // @channel / @here (não em DM): mencionam o canal inteiro / só os online. São texto no corpo — o
  // backend os interpreta; aqui é só descoberta pelo autocomplete.
  const specials = useMemo(() => {
    if (mentionQ === null || dm) return [] as { key: string; desc: string }[];
    const q = mentionQ.toLowerCase();
    return ([{ key: "channel", desc: t("chat.atChannelDesc") }, { key: "here", desc: t("chat.atHereDesc") }]).filter((o) => o.key.startsWith(q));
  }, [mentionQ, dm, t]);
  const pickSpecial = (key: string) => { setText((v) => v.replace(/@([\w.\-]*)$/, `@${key} `)); setMentionQ(null); };

  const onChange = (raw: string) => {
    // Atalhos (:) → 🙂) só quando a pessoa acaba de os fechar com espaço: substituir enquanto
    // escreve trocaria ":" no meio de um "http://" ou de um horário.
    const v = applyShortcodes(raw);
    setText(v);
    const mt = v.match(/@([\w.\-]*)$/);
    setMentionQ(mt ? mt[1] : null);
    // Menu de comandos: só quando o "/" abre a mensagem (um "e/ou" no meio do texto não é comando).
    const st = v.match(/^\/(\w*)$/);
    setSlashQ(st ? st[1] : null);
    setSlashIdx(0); // filtrou, volta ao topo — o destaque nunca fica num item que saiu da lista
    const now = Date.now();
    if (now - lastTyping.current > 2000) { lastTyping.current = now; onTyping(); }
  };
  const slashCmds = useMemo(() => {
    if (slashQ === null) return [];
    const q = slashQ.toLowerCase();
    return SLASH_COMMANDS.filter((c) => c.key.startsWith(q));
  }, [slashQ]);
  const pickCommand = (key: string) => {
    setSlashQ(null);
    // O menu só aparece quando o "/" é a mensagem inteira, por isso limpar a caixa não perde nada.
    if (key === "task") { setText(""); setTaskPick(true); return; }
    setText(`/${key} `);
    requestAnimationFrame(() => ta.current?.focus());
  };
  // Insere a referência no composer (em vez de enviar já): assim dá para juntar contexto antes de
  // enviar, e uma escolha errada corrige-se sem apagar mensagem nenhuma. O formato é o MESMO do
  // partilhar do quadro, portanto os dois caminhos aparecem iguais na conversa.
  const insertTask = (c: CardHit) => {
    setTaskPick(false);
    const ref = `📌 [${String(c.card_key)} — ${c.title}](/mail/kanban/${c.board_id}/${c.id})`;
    setText((v) => (v.trim() ? v.replace(/\s*$/, " ") : "") + ref + " ");
    requestAnimationFrame(() => ta.current?.focus());
  };
  const pickMention = (m: ChatMember) => {
    const name = m.name || m.email;
    mentioned.current.add(m.user_id);
    setText((v) => v.replace(/@([\w.\-]*)$/, `@${name} `));
    setMentionQ(null);
  };
  const send = async () => {
    const body = applySlash(text.trim());
    if (!body && atts.length === 0) return;
    const mentions = members.filter((m) => mentioned.current.has(m.user_id) && body.includes("@" + (m.name || m.email))).map((m) => m.user_id);
    const sending = atts;
    setText(""); setAtts([]); mentioned.current.clear(); setMentionQ(null);
    try { await backend.chatSend(channelId, body, parentId, mentions, sending); onSent(); }
    catch (e) { toast((e as Error).message); setText(body); setAtts(sending); }
  };

  return (
    <div className="composer-wrap">
      {slashCmds.length > 0 && (
        <div className="mention-pop slash-pop">
          {slashCmds.map((c, i) => (
            <button key={c.key} className={i === slashIdx ? "on" : ""} onMouseEnter={() => setSlashIdx(i)}
              onMouseDown={(e) => { e.preventDefault(); pickCommand(c.key); }}>
              <span className="av special">/</span>
              <span className="nm">/{c.key}{c.args ? ` ${t(c.args)}` : ""}</span>
              <span className="em">{t(c.desc)}</span>
            </button>
          ))}
        </div>
      )}
      {taskPick && <TaskPickerModal onPick={insertTask} onClose={() => setTaskPick(false)} />}
      {linkSel && <LinkModal initialText={linkSel.text} onSave={insertLink} onClose={() => setLinkSel(null)} />}
      {(suggestions.length > 0 || specials.length > 0) && (
        <div className="mention-pop">
          {specials.map((o) => (
            <button key={o.key} onMouseDown={(e) => { e.preventDefault(); pickSpecial(o.key); }}>
              <span className="av special">@</span>
              <span className="nm">@{o.key}</span><span className="em">{o.desc}</span>
            </button>
          ))}
          {suggestions.map((m) => (
            <button key={m.user_id} onMouseDown={(e) => { e.preventDefault(); pickMention(m); }}>
              <Avatar name={m.name} email={m.email} userId={m.user_id} size={28} />
              <span className="nm">{m.name || m.email}</span><span className="em">{m.email}</span>
            </button>
          ))}
        </div>
      )}
      <div className="rich-box">
        {/* Barra de FORMATAÇÃO em cima (só ícones de traço, como o Slack); as ações de conteúdo
            vivem em baixo. O "Aa" esconde/mostra esta barra e a escolha fica no aparelho. */}
        {showFmt && (
          <div className="rich-toolbar">
            <button title={t("chat.bold")} onMouseDown={(e) => { e.preventDefault(); wrap("**"); }}><IcBold size={16} /></button>
            <button title={t("chat.italic")} onMouseDown={(e) => { e.preventDefault(); wrap("_"); }}><IcItalic size={16} /></button>
            <button title={t("chat.underline")} onMouseDown={(e) => { e.preventDefault(); wrap("__"); }}><IcUnderline size={16} /></button>
            <button title={t("chat.strike")} onMouseDown={(e) => { e.preventDefault(); wrap("~~"); }}><IcStrike size={16} /></button>
            <span className="sep" />
            <button title={t("chat.link")} onMouseDown={(e) => { e.preventDefault(); openLink(); }}><IcLink size={16} /></button>
            <span className="sep" />
            <button title={t("chat.listOrdered")} onMouseDown={(e) => { e.preventDefault(); wrap("\n1. ", ""); }}><IcListOrdered size={16} /></button>
            <button title={t("chat.list")} onMouseDown={(e) => { e.preventDefault(); wrap("\n- ", ""); }}><IcList size={16} /></button>
            <span className="sep" />
            <button title={t("chat.quote")} onMouseDown={(e) => { e.preventDefault(); wrap("\n> ", ""); }}><IcQuote size={16} /></button>
            <button title={t("chat.code")} onMouseDown={(e) => { e.preventDefault(); wrap("`"); }}><IcCode size={16} /></button>
            <button title={t("chat.codeBlock")} onMouseDown={(e) => { e.preventDefault(); wrap("\n```\n", "\n```\n"); }}><IcCodeBlock size={16} /></button>
          </div>
        )}
        <textarea ref={ta} rows={1} value={text} placeholder={t("chat.messagePh")} onChange={(e) => onChange(e.target.value)}
          onInput={(e) => { const el = e.currentTarget; el.style.height = "auto"; el.style.height = Math.min(el.scrollHeight, 180) + "px"; }}
          onPaste={(e) => {
            // Colar imagem (print) ou ficheiro do clipboard → vira anexo, como arrastar. Só intercepta
            // quando há ficheiros; texto normal continua a colar-se como sempre.
            const files = e.clipboardData?.files;
            if (files && files.length) { e.preventDefault(); void onFiles(files); }
          }}
          onKeyDown={(e) => {
            if (e.key === "Escape" && slashQ !== null) { setSlashQ(null); return; }
            // Menu de comandos aberto: ↑/↓ andam na lista (com volta ao início/fim) e Enter/Tab
            // escolhem o destacado — em vez de enviar "/task" como texto.
            if (slashCmds.length > 0) {
              if (e.key === "ArrowDown") { e.preventDefault(); setSlashIdx((i) => (i + 1) % slashCmds.length); return; }
              if (e.key === "ArrowUp") { e.preventDefault(); setSlashIdx((i) => (i - 1 + slashCmds.length) % slashCmds.length); return; }
              if (e.key === "Enter" || e.key === "Tab") {
                e.preventDefault();
                pickCommand(slashCmds[Math.min(slashIdx, slashCmds.length - 1)].key);
                return;
              }
            }
            if (e.key !== "Enter" || suggestions.length) return;
            // settings.chat.enterSends: com Enter-envia (default), Enter envia e Shift+Enter quebra
            // linha; desligado, inverte-se — só Ctrl/Cmd+Enter envia e Enter quebra linha.
            const enterSends = settings.chat?.enterSends !== false;
            const shouldSend = enterSends ? !e.shiftKey : (e.metaKey || e.ctrlKey);
            if (shouldSend) { e.preventDefault(); send(); }
          }} />
        <input ref={fileIn} type="file" multiple hidden onChange={(e) => { onFiles(e.target.files); e.target.value = ""; }} />
        {(atts.length > 0 || uploading > 0) && (
          <div className="composer-atts">
            {atts.map((a) => (
              <span key={a.file_id} className="cmp-att"><IcPaperclip size={12} /> {a.name}
                <button title={t("common.removeAttachment")} onClick={() => setAtts((x) => x.filter((y) => y !== a))}><IcX size={11} /></button>
              </span>
            ))}
            {uploading > 0 && <span className="cmp-att up">{t("webmail.compose.uploading")}…</span>}
          </div>
        )}
        <div className="rich-foot">
          {/* "+" abre um menu (como o Slack) em vez de saltar direto para o seletor de ficheiros:
              anexar do computador e anexar do Drive são caminhos diferentes e ambos cabem aqui. */}
          <span className="react-wrap">
            <button className={addMenu ? "on" : ""} title={t("chat.attach")}
              onMouseDown={(e) => { e.preventDefault(); const r = e.currentTarget.getBoundingClientRect(); setAddMenu((v) => (v ? null : r)); }}>
              {addMenu ? <IcX size={17} /> : <IcPlus size={17} />}
            </button>
            {addMenu && (
              <>
                {/* Fundo invisível: clicar fora fecha, como em qualquer menu da app. */}
                <div className="menu-backdrop" onMouseDown={() => setAddMenu(null)} />
                <div className="add-menu" style={{ left: addMenu.left, bottom: window.innerHeight - addMenu.top + 8 }}>
                  <button onMouseDown={(e) => { e.preventDefault(); setAddMenu(null); fileIn.current?.click(); }}>
                    <IcUpload size={16} /><span className="nm">{t("chat.addUpload")}</span>
                  </button>
                  <button onMouseDown={(e) => { e.preventDefault(); setAddMenu(null); setDrivePick(true); }}>
                    <IcFolder size={16} /><span className="nm">{t("chat.addDrive")}</span>
                  </button>
                  <button onMouseDown={(e) => { e.preventDefault(); setAddMenu(null); wrap("\n```\n", "\n```\n"); }}>
                    <IcCodeBlock size={16} /><span className="nm">{t("chat.addSnippet")}</span>
                  </button>
                  <button onMouseDown={(e) => { e.preventDefault(); setAddMenu(null); setTaskPick(true); }}>
                    <IcColumns size={16} /><span className="nm">{t("chat.addTask")}</span>
                  </button>
                </div>
              </>
            )}
          </span>
          <button className={showFmt ? "on" : ""} title={t("chat.formatting")}
            onMouseDown={(e) => { e.preventDefault(); toggleFmt(); }}><IcTextSize size={17} /></button>
          <span className="react-wrap">
            <button title={t("chat.emoji")}
              onMouseDown={(e) => { e.preventDefault(); const r = e.currentTarget.getBoundingClientRect(); setEmoji((v) => (v ? null : r)); }}><IcEmoji size={17} /></button>
            {emoji && <EmojiPicker anchor={emoji} onPick={insertEmoji} onClose={() => setEmoji(null)} />}
          </span>
          <button title={t("chat.mention")} onMouseDown={(e) => { e.preventDefault(); insertAt("@"); }}><IcAt size={17} /></button>
          <span className="sep" />
          <span className="react-wrap">
            <button className="gif-btn" title={t("chat.gif")}
              onMouseDown={(e) => { e.preventDefault(); const r = e.currentTarget.getBoundingClientRect(); setGif((v) => (v ? null : r)); }}>GIF</button>
            {/* O GIF entra como anexo — é o mesmo caminho de qualquer imagem, já com visualizador. */}
            {gif && <GifPicker anchor={gif} onClose={() => setGif(null)}
              onPick={(g) => { setAtts((a) => [...a, { file_id: g.file_id, name: g.name, size: g.size }]); setGif(null); }} />}
            {drivePick && <DrivePickerDrawer onClose={() => setDrivePick(false)}
              onPick={(f) => setAtts((a) => [...a, { file_id: f.id, name: f.name, size: f.size }])} />}
          </span>
          <button title={t("chat.commands")} onMouseDown={(e) => { e.preventDefault(); insertAt("/"); }}><IcSlash size={17} /></button>
          <span className="grow" />
          <span className="hint">{t("chat.enterHint")}</span>
          <span className="sched-wrap">
            <button className="ib sm" title={t("chat.scheduleSend")} disabled={!text.trim()} onClick={() => setSched((v) => !v)}><IcClock size={16} /></button>
            {sched && <TimeMenu onClose={() => setSched(false)}
              onPick={(d) => { setSched(false); const b = text.trim(); if (!b) return; void backend.chatSchedule(channelId, b, d.toISOString()).then(() => { setText(""); toast(t("chat.scheduled")); }).catch((e) => toast((e as Error).message)); }} />}
          </span>
          <button className="send" title={t("chat.send")} disabled={(!text.trim() && atts.length === 0) || uploading > 0} onClick={send}><IcSend size={16} /></button>
        </div>
      </div>
    </div>
  );
});
