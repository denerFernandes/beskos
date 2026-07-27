import { Fragment, forwardRef, useEffect, useImperativeHandle, useLayoutEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import type { WebmailSession } from "./auth";
import { backend, colleaguesApi, viewerKind, type ChatChannel, type ChatMessage, type ChatMember, type ChatAttachment } from "./backend";
import { useChatSocket } from "./useChatSocket";
import { renderRich } from "./richtext";
import { setCustomEmoji } from "./customEmoji";
import { sanitizeRich } from "./mailHtml";
import { ChatAttachmentViewer } from "./ChatAttachmentViewer";
import { openRoom } from "./MeetRoom";
import { useUserSettings } from "./useSettings";
import { toast } from "./toast";
import { EmojiPicker, pushRecent } from "./EmojiPicker";
import { Switch } from "./Switch";
import { GifPicker } from "./GifPicker";
import { SHORTCODES } from "./emoji";
import { initials, shortTime } from "./util";
import { Avatar } from "./Avatar";
import { IcPlus, IcUsers, IcTrash, IcPencil, IcReply, IcForward, IcX, IcVideo, IcPhone, IcPaperclip, IcChevron, IcLink, IcPin, IcBell, IcSearch, IcMail, IcList, IcClock, IcFolder, IcColumns } from "../panel/icons";
import { ExtractTasksModal } from "./ExtractTasksModal";
import { DrivePickerDrawer } from "./DrivePickerDrawer";

const KB = (n: number) => `${Math.max(1, Math.round(n / 1024))} KB`;

// Zona de arrastar-e-soltar arquivos. Só reage a arrasto de arquivos (ignora drag de outros
// elementos); usa contador de enter/leave para não piscar ao passar sobre filhos.
function useFileDrop(onDrop: (files: FileList) => void) {
  const [over, setOver] = useState(false);
  const depth = useRef(0);
  const hasFiles = (e: React.DragEvent) => Array.from(e.dataTransfer?.types ?? []).includes("Files");
  const handlers = {
    onDragEnter: (e: React.DragEvent) => { if (!hasFiles(e)) return; e.preventDefault(); depth.current++; setOver(true); },
    onDragOver: (e: React.DragEvent) => { if (!hasFiles(e)) return; e.preventDefault(); e.dataTransfer.dropEffect = "copy"; },
    onDragLeave: (e: React.DragEvent) => { if (!hasFiles(e)) return; depth.current--; if (depth.current <= 0) { depth.current = 0; setOver(false); } },
    onDrop: (e: React.DragEvent) => {
      if (!hasFiles(e)) return;
      e.preventDefault(); depth.current = 0; setOver(false);
      if (e.dataTransfer.files?.length) onDrop(e.dataTransfer.files);
    },
  };
  return { over, handlers };
}

// Reações rápidas: as poucas que se usam em 90% dos casos. O resto vem do seletor completo.
const QUICK = ["👍", "❤️", "😄", "🎉", "👀", "🙏", "🔥", "✅"];

// Convenção de "chamada": mensagem-sistema que o frontend renderiza como cartão de entrada.
const CALL_RE = /^📞 __call__ (video|audio) (\S+)$/;
const callBody = (mode: "video" | "audio", room: string) => `📞 __call__ ${mode} ${room}`;
function parseCall(body: string): { mode: "video" | "audio"; room: string } | null {
  const m = CALL_RE.exec(body);
  return m ? { mode: m[1] as "video" | "audio", room: m[2] } : null;
}

// Slash-commands client-side (estilo Slack): transformam o texto antes do envio. Comandos
// desconhecidos seguem como texto normal. `/me` e `/shrug` são os úteis; os flips são brincadeira.
function applySlash(body: string): string {
  const m = /^\/(\w+)(?:\s+([\s\S]*))?$/.exec(body);
  if (!m) return body;
  const cmd = m[1].toLowerCase();
  const rest = (m[2] || "").trim();
  switch (cmd) {
    case "shrug": return (rest ? rest + " " : "") + "¯\\_(ツ)_/¯";
    case "me": return rest ? "_" + rest + "_" : body;
    case "tableflip": return (rest ? rest + " " : "") + "(╯°□°)╯︵ ┻━┻";
    case "unflip": return (rest ? rest + " " : "") + "┬─┬ノ( º _ ºノ)";
    default: return body;
  }
}

// Aplica um evento de reação (WS) a uma mensagem, incrementalmente — sem refazer o fetch do canal.
// Idempotente por membership em `users`: o eco da própria ação (ou reentrega) não conta em dobro.
// `mine` é recalculado com o id do usuário atual, então serve para qualquer membro que receba o evento.
function applyReaction(m: ChatMessage, emoji: string, userId: string, remove: boolean, myId: string): ChatMessage {
  const reactions = [...(m.reactions ?? [])];
  const idx = reactions.findIndex((r) => r.emoji === emoji);
  if (remove) {
    if (idx < 0 || !reactions[idx].users.includes(userId)) return m; // já removido → no-op
    const users = reactions[idx].users.filter((u) => u !== userId);
    if (users.length === 0) reactions.splice(idx, 1);
    else reactions[idx] = { ...reactions[idx], users, count: users.length, mine: users.includes(myId) };
  } else {
    if (idx < 0) {
      reactions.push({ emoji, users: [userId], count: 1, mine: userId === myId });
    } else {
      if (reactions[idx].users.includes(userId)) return m; // dedup do próprio eco
      const users = [...reactions[idx].users, userId];
      reactions[idx] = { ...reactions[idx], users, count: users.length, mine: users.includes(myId) };
    }
  }
  return { ...m, reactions };
}

// Chat estilo Slack: canais + DMs, mensagens em tempo real (WebSocket), reações e menções.
export function ChatView({ search, onSearch, session, channelParam, onChannel }: {
  search: string; onSearch?: (v: string) => void; session: WebmailSession; channelParam?: string; onChannel: (id: string) => void;
}) {
  const { t, i18n } = useTranslation();
  const qc = useQueryClient();
  const [typing, setTyping] = useState<Record<string, string>>({}); // channelId -> author
  const [showNew, setShowNew] = useState<null | "channel" | "dm">(null);
  const [thread, setThread] = useState<ChatMessage | null>(null);
  const [railOpen, setRailOpen] = useState(false); // gaveta de conversas no telemóvel
  const [focus, setFocus] = useState<{ channel: string; msg: string } | null>(null); // saltar para a mensagem
  const [showSaved, setShowSaved] = useState(false);
  const savedQ = useQuery({ queryKey: ["chat-saved"], queryFn: () => backend.chatSaved(), enabled: showSaved });
  // Id do usuário atual: usado para recalcular `mine` ao aplicar reações vindas do WS.
  const meQ = useQuery({ queryKey: ["me"], queryFn: () => backend.me(), retry: false, staleTime: Infinity });
  const myId = meQ.data?.id ?? "";
  // Emojis customizados do workspace: alimenta o registo global (renderRich troca :nome: por imagem).
  const emojisQ = useQuery({ queryKey: ["chat-emojis"], queryFn: () => backend.chatEmojis(), retry: false, staleTime: 300000 });
  useEffect(() => { if (emojisQ.data) setCustomEmoji(emojisQ.data); }, [emojisQ.data]);

  const chansQ = useQuery({ queryKey: ["chat-channels"], queryFn: () => backend.chatChannels(), refetchInterval: 30000 });
  // O canal por defeito chega marcado (is_general) com o nome guardado em inglês; mostramos o nome
  // no idioma da interface. Trocamos aqui, à entrada, para toda a UI (barra lateral, cabeçalho,
  // troca rápida, `in:` da busca) falar do mesmo nome — a busca casa por id, não por texto.
  const channels = useMemo(
    () => (chansQ.data?.channels ?? []).map((c) => (c.is_general ? { ...c, name: t("chat.generalName") } : c)),
    [chansQ.data, t],
  );
  const discover = chansQ.data?.discover ?? [];
  // O canal ativo vem da URL (link compartilhável); sem ele, cai no primeiro.
  const active = channelParam && channels.some((c) => c.id === channelParam) ? channelParam : channels[0]?.id ?? null;
  const activeChan = channels.find((c) => c.id === active) ?? null;
  const setActive = onChannel;

  // Presença (online/ausente): poll a cada 20s.
  const presenceQ = useQuery({ queryKey: ["chat-presence"], queryFn: () => backend.chatPresence(), refetchInterval: 20000, retry: false });
  const online = useMemo(() => new Set(presenceQ.data?.online ?? []), [presenceQ.data]);
  // Presença rica: offline se não estiver online; senão o estado escolhido (active/away/dnd).
  const presenceOf = (id?: string): string => (id && online.has(id) ? (presenceQ.data?.presence?.[id] ?? "active") : "offline");

  // Busca de mensagens. Suporta `in:<canal>` para restringir a um canal (estilo Slack) — o filtro
  // vai na PRÓPRIA busca, sem um segundo campo.
  const inMatch = search.match(/(?:^|\s)in:(\S+)/i);
  const inChannel = inMatch ? channels.find((c) => c.name.toLowerCase() === inMatch[1].toLowerCase()) : undefined;
  const rawTerm = search.replace(/(?:^|\s)in:\S+/i, " ").trim();
  const term = rawTerm.toLowerCase();
  const searchActive = term.length >= 2 || (!!inChannel && rawTerm.length >= 1);
  const searchQ = useQuery({
    queryKey: ["chat-search", term, inChannel?.id ?? ""],
    queryFn: async () => {
      if (inChannel) {
        const r = await backend.chatSearchInChannel(inChannel.id, rawTerm);
        return { results: r.results.map((m) => ({ id: m.id, channel_id: inChannel.id, channel_name: inChannel.name, channel_kind: inChannel.kind, author: m.author, body: m.body, created_at: m.created_at })) };
      }
      return backend.chatSearch(term);
    },
    enabled: searchActive,
  });

  // Tempo real: cada evento revalida a conversa ativa e a lista (não-lido). "typing" é efêmero.
  const { sendTyping } = useChatSocket((ev) => {
    if (ev.type === "typing") {
      const author = String(ev.payload.author ?? "");
      setTyping((m) => ({ ...m, [ev.channel_id]: author }));
      window.setTimeout(() => setTyping((m) => { const n = { ...m }; if (n[ev.channel_id] === author) delete n[ev.channel_id]; return n; }), 3000);
      return;
    }
    void qc.invalidateQueries({ queryKey: ["chat-channels"] }); // não-lido/ordem sempre

    // Patch incremental na lista do canal em vez de refetch — o evento é entregue a TODOS os membros,
    // então refazer o fetch inteiro por evento não escala em canais movimentados (débito técnico §chat).
    // Cada patch é idempotente/dedup, então o eco da própria ação não duplica.
    const p = ev.payload as Record<string, unknown>;
    const patchMsgs = (fn: (ms: ChatMessage[]) => ChatMessage[]) =>
      qc.setQueryData<{ messages: ChatMessage[]; has_more: boolean }>(["chat-messages", ev.channel_id], (old) =>
        old ? { ...old, messages: fn(old.messages) } : old);

    if (ev.type === "message") {
      const msg = ev.payload as unknown as ChatMessage;
      if (msg?.id && !msg.parent_id) {
        patchMsgs((ms) => (ms.some((m) => m.id === msg.id) ? ms : [...ms, msg]));
      } else if (msg?.parent_id) {
        // Resposta de thread: sobe o contador do pai na lista principal (sem refazer o canal).
        patchMsgs((ms) => ms.map((m) => (m.id === msg.parent_id ? { ...m, reply_count: m.reply_count + 1 } : m)));
      }
    } else if (ev.type === "message_edit") {
      const id = String(p.id ?? "");
      patchMsgs((ms) => ms.map((m) => (m.id === id ? { ...m, body: String(p.body ?? ""), edited_at: String(p.edited_at ?? "") } : m)));
    } else if (ev.type === "message_delete") {
      const id = String(p.id ?? "");
      patchMsgs((ms) => ms.map((m) => (m.id === id ? { ...m, deleted: true, body: "" } : m)));
    } else if (ev.type === "reaction") {
      const mid = String(p.message_id ?? "");
      patchMsgs((ms) => ms.map((m) => (m.id === mid ? applyReaction(m, String(p.emoji ?? ""), String(p.user_id ?? ""), Boolean(p.remove), myId) : m)));
    }

    // Thread aberta: nova resposta, edição, remoção ou reação → revalida a thread (query pequena, não
    // é o canal inteiro). Cobre o caso de outro membro mexer numa mensagem da thread que tenho aberta.
    if (thread && ev.channel_id === active) {
      const touchesThread =
        (ev.type === "message" && String(p.parent_id ?? "") === thread.id) ||
        ev.type === "message_edit" || ev.type === "message_delete" || ev.type === "reaction";
      if (touchesThread) void qc.invalidateQueries({ queryKey: ["chat-thread", thread.id] });
    }
  });

  // Fecha a thread ao trocar de canal.
  useEffect(() => { setThread(null); setRailOpen(false); }, [active]);

  return (
    <div className={"chat" + (thread ? " with-thread" : "")}>
      {railOpen && <div className="mgrid-backdrop" onClick={() => setRailOpen(false)} />}
      <aside className={"chat-rail" + (railOpen ? " open" : "")} onClick={() => setRailOpen(false)}>
        <div className="chat-rail-h"><b>{t("chat.title")}</b>
          <div className="grow" />
          <button className={"ib" + (showSaved ? " on" : "")} title={t("chat.saved")} onClick={() => setShowSaved((v) => !v)}><BookmarkIcon size={16} /></button>
          <button className="ib" title={t("chat.newDM")} onClick={() => setShowNew("dm")}><IcUsers size={16} /></button>
          <button className="ib" title={t("chat.newChannel")} onClick={() => setShowNew("channel")}><IcPlus size={16} /></button>
        </div>
        {showSaved ? (
          <div className="chat-results">
            <div className="chat-section">{t("chat.saved")}</div>
            {savedQ.isLoading ? <div className="loading">…</div>
              : (savedQ.data?.saved.length ?? 0) === 0 ? <div className="chat-empty">{t("chat.noSaved")}</div>
              : savedQ.data!.saved.map((sv) => (
                <button key={sv.id} className="sr" onClick={() => { setFocus({ channel: sv.channel_id, msg: sv.id }); setActive(sv.channel_id); setShowSaved(false); }}>
                  <div className="sr-h"><span className="sr-ch">{sv.channel_kind === "dm" ? "@" : "#"}{sv.channel_name}</span><span className="tm">{shortTime(sv.created_at, i18n.language)}</span></div>
                  <div className="sr-b"><b>{sv.author}:</b> {sv.body}</div>
                </button>
              ))}
          </div>
        ) : searchActive ? (
          <div className="chat-results">
            {searchQ.isLoading ? <div className="loading">…</div>
              : (searchQ.data?.results.length ?? 0) === 0 ? <div className="chat-empty">{t("chat.noResults")}</div>
              : searchQ.data!.results.map((h) => (
                <button key={h.id} className="sr" onClick={() => { setFocus({ channel: h.channel_id, msg: h.id }); setActive(h.channel_id); }}>
                  <div className="sr-h"><span className="sr-ch">{h.channel_kind === "dm" ? "@" : "#"}{h.channel_name}</span><span className="tm">{shortTime(h.created_at, i18n.language)}</span></div>
                  <div className="sr-b"><b>{h.author}:</b> {h.body}</div>
                </button>
              ))}
          </div>
        ) : (
          <>
            <div className="chat-section">{t("chat.channels")}</div>
            {channels.filter((c) => c.kind !== "dm").map((c) => (
              <ChannelRow key={c.id} c={c} active={c.id === active} onClick={() => setActive(c.id)} />
            ))}
            <div className="chat-section">{t("chat.dms")}</div>
            {channels.filter((c) => c.kind === "dm").map((c) => (
              <ChannelRow key={c.id} c={c} dm presence={presenceOf(c.other_id)} active={c.id === active} onClick={() => setActive(c.id)} />
            ))}
            {discover.length > 0 && <div className="chat-section">{t("chat.discover")}</div>}
            {discover.map((c) => (
              <button key={c.id} className="chan discover" onClick={async () => { await backend.chatJoin(c.id); qc.invalidateQueries({ queryKey: ["chat-channels"] }); setActive(c.id); }}>
                # {c.name} <span className="join">{t("chat.join")}</span>
              </button>
            ))}
          </>
        )}
      </aside>

      <section className="chat-main">
        {activeChan ? (
          <ChannelPane key={activeChan.id} channel={activeChan} channels={channels} session={session} locale={i18n.language}
            typingAuthor={typing[activeChan.id]} onTyping={() => sendTyping(activeChan.id)} onOpenThread={setThread}
            onSearch={onSearch} onToggleRail={() => setRailOpen(true)}
            focusMsg={focus && focus.channel === activeChan.id ? focus.msg : null} onFocusConsumed={() => setFocus(null)}
            onOpenDM={async (email) => { try { const c = await backend.chatOpenDM(email); onChannel(c.id); } catch (e) { toast((e as Error).message); } }} />
        ) : (
          <div className="empty">{t("chat.pickChannel")}</div>
        )}
      </section>

      {thread && activeChan && (
        <ThreadPanel key={thread.id} parent={thread} channelId={activeChan.id} session={session} locale={i18n.language}
          onClose={() => setThread(null)} />
      )}

      {showNew && <NewConvoModal mode={showNew} me={session.email} onClose={() => setShowNew(null)}
        onCreated={(id) => { setShowNew(null); qc.invalidateQueries({ queryKey: ["chat-channels"] }); setActive(id); }} />}
    </div>
  );
}

function ChannelRow({ c, active, dm, presence, onClick }: { c: ChatChannel; active: boolean; dm?: boolean; presence?: string; onClick: () => void }) {
  const { t } = useTranslation();
  const unread = c.unread ?? 0;
  const pres = presence ?? "offline";
  return (
    <button className={"chan" + (active ? " on" : "") + (unread > 0 ? " unread" : "")} onClick={onClick}>
      {dm ? (
        <span className="dot-wrap" title={t("presence." + (pres === "offline" ? "offline" : pres))}>
          <Avatar name={c.name} userId={c.other_id} size={36} radius="9px" className="dot-av" />
          <span className={"presence " + pres} />
        </span>
      ) : <span className="hash">#</span>}
      <span className="cn">{c.name}</span>
      {unread > 0 && <span className="c">{unread}</span>}
    </button>
  );
}

// ---- Painel de um canal: mensagens + composer ----
function ChannelPane({ channel, channels, session, locale, typingAuthor, onTyping, onOpenThread, onOpenDM, onSearch, onToggleRail, focusMsg, onFocusConsumed }: {
  channel: ChatChannel; channels: ChatChannel[]; session: WebmailSession; locale: string; typingAuthor?: string; onTyping: () => void;
  onOpenThread: (m: ChatMessage) => void; onOpenDM: (email: string) => void; onSearch?: (v: string) => void; onToggleRail?: () => void;
  focusMsg?: string | null; onFocusConsumed?: () => void;
}) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [panelOpen, setPanelOpen] = useState(false);
  const [tab, setTab] = useState<"messages" | "canvas" | "files" | "pins">("messages");
  const [notifOpen, setNotifOpen] = useState(false);
  const [showSched, setShowSched] = useState(false);
  const [notifPref, setNotifPref] = useState(channel.notif_pref ?? "all");
  const [profile, setProfile] = useState<ChatMember | null>(null);
  // Volta a Mensagens ao trocar de canal.
  useEffect(() => { setTab("messages"); setNotifPref(channel.notif_pref ?? "all"); }, [channel.id]);
  const setPref = async (p: "all" | "mentions" | "none") => { setNotifPref(p); setNotifOpen(false); try { await backend.chatSetNotifPref(channel.id, p); qc.invalidateQueries({ queryKey: ["chat-channels"] }); } catch (e) { toast((e as Error).message); } };
  const msgsQ = useQuery({ queryKey: ["chat-messages", channel.id], queryFn: () => backend.chatMessages(channel.id) });
  const membersQ = useQuery({ queryKey: ["chat-members", channel.id], queryFn: () => backend.chatMembers(channel.id), refetchInterval: 25000 });
  const scroller = useRef<HTMLDivElement>(null);

  // Paginação do histórico: a base é a página mais recente (msgsQ); `older` acumula as páginas
  // anteriores carregadas sob demanda. Combinadas e deduplicadas por id.
  const [older, setOlder] = useState<ChatMessage[]>([]);
  const [moreOlder, setMoreOlder] = useState(false);
  const [loadingOlder, setLoadingOlder] = useState(false);
  useEffect(() => { setOlder([]); setMoreOlder(false); }, [channel.id]); // reset ao trocar de canal
  const base = useMemo(() => msgsQ.data?.messages ?? [], [msgsQ.data]);
  const messages = useMemo(() => {
    const seen = new Set(base.map((m) => m.id));
    const combined = [...older.filter((m) => !seen.has(m.id)), ...base];
    // Ordena por data: robusto quando a janela de "saltar para a mensagem" é fundida ao histórico.
    combined.sort((a, b) => new Date(a.created_at).getTime() - new Date(b.created_at).getTime());
    return combined;
  }, [older, base]);
  // Há mais antigas? Antes de carregar qualquer página, é o has_more da base; depois, o da última página.
  const hasMore = older.length > 0 ? moreOlder : (msgsQ.data?.has_more ?? false);

  const canManage = channel.my_role === "owner" || channel.my_role === "manager";
  // Fronteira de "não-lidas": captura o last_read_at no momento em que o canal foi aberto (antes do
  // mark-read automático) — assim a divisória "novas mensagens" mostra onde a pessoa parou.
  const [unreadBoundary, setUnreadBoundary] = useState<string | null>(null);
  useEffect(() => { setUnreadBoundary(channel.last_read_at ?? null); }, [channel.id]);
  const firstUnreadIdx = useMemo(() => (unreadBoundary ? messages.findIndex((m) => m.created_at > unreadBoundary) : -1), [messages, unreadBoundary]);
  const markUnread = async (msgId: string) => {
    try { await backend.chatMarkUnread(msgId); qc.invalidateQueries({ queryKey: ["chat-channels"] }); toast(t("chat.markedUnread")); }
    catch (e) { toast((e as Error).message); }
  };

  // Saltar para a mensagem (busca externa OU um pin do próprio canal): garante que o alvo está
  // carregado (busca a janela em torno dele se preciso), rola até ele e o destaca por um instante.
  const [highlight, setHighlight] = useState<string | null>(null);
  const [localFocus, setLocalFocus] = useState<string | null>(null); // ex.: clique num pin
  const target = focusMsg ?? localFocus;
  useEffect(() => {
    if (!target) return;
    let cancelled = false;
    (async () => {
      if (!messages.some((m) => m.id === target)) {
        try {
          const win = await backend.chatMessagesAround(channel.id, target);
          if (cancelled) return;
          const b = msgsQ.data?.messages ?? [];
          setOlder((prev) => { const have = new Set([...prev, ...b].map((m) => m.id)); return [...win.messages.filter((m) => !have.has(m.id)), ...prev]; });
        } catch { /* se falhar, ao menos abre o canal */ }
      }
      setTimeout(() => {
        if (cancelled) return;
        scroller.current?.querySelector(`[data-mid="${target}"]`)?.scrollIntoView({ block: "center" });
        setHighlight(target);
        setTimeout(() => setHighlight(null), 2500);
        onFocusConsumed?.();
        setLocalFocus(null);
      }, 140);
    })();
    return () => { cancelled = true; };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [target, channel.id]);
  const jumpToPin = (msgId: string) => { setTab("messages"); setLocalFocus(msgId); };
  const [forwarding, setForwarding] = useState<string | null>(null); // id da mensagem a encaminhar
  const [extracting, setExtracting] = useState<ChatMessage | null>(null); // mensagem a virar tarefas

  // Preserva a posição do scroll ao prepender antigas (o conteúdo cresce por cima).
  const prevH = useRef<number | null>(null);
  useLayoutEffect(() => {
    if (prevH.current != null && scroller.current) {
      scroller.current.scrollTop += scroller.current.scrollHeight - prevH.current;
      prevH.current = null;
    }
  }, [older]);

  const loadOlder = async () => {
    if (loadingOlder || !messages.length) return;
    setLoadingOlder(true);
    prevH.current = scroller.current?.scrollHeight ?? 0;
    try {
      const page = await backend.chatMessages(channel.id, messages[0].created_at);
      setOlder((prev) => {
        const seen = new Set([...prev, ...base].map((m) => m.id));
        return [...page.messages.filter((m) => !seen.has(m.id)), ...prev];
      });
      setMoreOlder(page.has_more);
    } catch (e) { prevH.current = null; toast((e as Error).message); }
    finally { setLoadingOlder(false); }
  };

  // Inicia uma chamada (áudio/vídeo) reusando o Meet/LiveKit: posta o convite no canal e entra na sala.
  const startCall = async (mode: "video" | "audio") => {
    const room = `chat-${channel.id}`;
    try { await backend.chatSend(channel.id, callBody(mode, room)); } catch { /* segue mesmo assim */ }
    openRoom(room, mode === "audio");
  };

  // Marca lido ao abrir/receber e rola para o fim.
  useEffect(() => { void backend.chatMarkRead(channel.id).then(() => qc.invalidateQueries({ queryKey: ["chat-channels"] })); }, [channel.id, base.length, qc]);
  // Rola ao fim quando chega uma mensagem NOVA (última muda) ou ao abrir o canal — nunca ao carregar
  // antigas (aí o id da última não muda e a posição é preservada pelo useLayoutEffect acima).
  const lastId = messages[messages.length - 1]?.id;
  useEffect(() => { scroller.current?.scrollTo({ top: scroller.current.scrollHeight }); }, [lastId, channel.id]);

  const refresh = () => qc.invalidateQueries({ queryKey: ["chat-messages", channel.id] });
  // react/edit/delete não refazem o canal: o eco do WS aplica o patch incremental (p/ todos os membros).
  const react = async (mid: string, emoji: string, mine: boolean) => { await backend.chatReact(mid, emoji, mine); };

  // Arrastar arquivos para qualquer ponto do canal → vira anexo no composer.
  const composerRef = useRef<ComposerHandle>(null);
  const { over, handlers } = useFileDrop((files) => composerRef.current?.addFiles(files));

  return (
    <div className="chat-pane" {...handlers}>
      <header className="chat-head">
        {onToggleRail && <button className="ib chat-burger" onClick={onToggleRail} aria-label="menu"><IcList size={17} /></button>}
        {channel.kind === "dm" ? <Avatar name={channel.name} userId={channel.other_id} size={28} radius="8px" className="dot-av sm" /> : <span className="hash">#</span>}
        <b>{channel.name}</b>
        {channel.topic && <span className="topic">{channel.topic}</span>}
        <div className="grow" />
        {channel.kind !== "dm" && (membersQ.data?.length ?? 0) > 0 && (
          <button className="member-stack" title={t("chat.members")} onClick={() => setPanelOpen(true)}>
            {(membersQ.data ?? []).slice(0, 4).map((m) => (
              <Avatar key={m.user_id} name={m.name} email={m.email} userId={m.user_id} size={24} className="ms-av" />
            ))}
            <span className="ms-count">{membersQ.data?.length}</span>
          </button>
        )}
        {onSearch && <button className="ib" title={t("chat.searchChannel")} onClick={() => onSearch(`in:${channel.name} `)}><IcSearch size={17} /></button>}
        <div className="notif-wrap">
          <button className={"ib" + (notifPref !== "all" ? " muted-on" : "")} title={t("chat.notifPref")} onClick={() => setNotifOpen((v) => !v)}><IcBell size={17} /></button>
          {notifOpen && (
            <div className="notif-menu" onMouseLeave={() => setNotifOpen(false)}>
              {(["all", "mentions", "none"] as const).map((p) => (
                <button key={p} className={notifPref === p ? "on" : ""} onClick={() => void setPref(p)}>{t("chat.notif_" + p)}</button>
              ))}
            </div>
          )}
        </div>
        <button className="ib" title={t("chat.scheduledTitle")} onClick={() => setShowSched(true)}><IcClock size={17} /></button>
        <button className="ib" title={t("chat.audioCall")} onClick={() => startCall("audio")}><IcPhone size={17} /></button>
        <button className="ib" title={t("chat.videoCall")} onClick={() => startCall("video")}><IcVideo size={18} /></button>
        {channel.kind !== "dm" && <button className="ib" title={t("chat.members")} onClick={() => setPanelOpen(true)}><IcUsers size={18} /></button>}
      </header>
      {showSched && <ScheduledModal onClose={() => setShowSched(false)} />}
      {profile && <MemberProfile member={profile} me={session.email} onClose={() => setProfile(null)} onOpenDM={(e) => { setProfile(null); onOpenDM(e); }} />}
      {/* Extrair tarefas da mensagem: reaproveita o mesmo fluxo do e-mail (IA → escolher → quadro). */}
      {extracting && (
        <ExtractTasksModal subject={`#${channel.name} · ${extracting.author}`} body={extracting.body}
          sourceEmailId="" onClose={() => setExtracting(null)} />
      )}

      {forwarding && <ForwardModal channels={channels} onClose={() => setForwarding(null)}
        onPick={async (target) => { try { await backend.chatForward(forwarding, target); setForwarding(null); toast(t("chat.forwarded")); } catch (e) { toast((e as Error).message); } }} />}
      {channel.kind !== "dm" && (
        <div className="chat-tabs">
          <button className={tab === "messages" ? "on" : ""} onClick={() => setTab("messages")}>{t("chat.messages")}</button>
          <button className={tab === "canvas" ? "on" : ""} onClick={() => setTab("canvas")}>{t("chat.canvas")}</button>
          <button className={tab === "files" ? "on" : ""} onClick={() => setTab("files")}>{t("chat.filesLinks")}</button>
          <button className={tab === "pins" ? "on" : ""} onClick={() => setTab("pins")}>{t("chat.pins")}</button>
        </div>
      )}
      {panelOpen && <ChannelPanel channel={channel} me={session.email} onClose={() => setPanelOpen(false)} />}

      {tab === "canvas" && <div className="chat-tabpane"><CanvasTab channelId={channel.id} /></div>}
      {tab === "pins" && <div className="chat-tabpane"><PinsTab channelId={channel.id} onJump={jumpToPin} /></div>}
      {tab === "files" && <div className="chat-tabpane"><FilesLinksTab channelId={channel.id} /></div>}

      {tab === "messages" && <>
      <div className="chat-msgs" ref={scroller}>
        {hasMore && messages.length > 0 && (
          <button className="chat-loadmore" onClick={loadOlder} disabled={loadingOlder}>
            {loadingOlder ? "…" : t("chat.loadOlder")}
          </button>
        )}
        {msgsQ.isLoading ? <div className="loading">…</div>
          : messages.length === 0 ? <div className="empty">{t("chat.firstMessage")}</div>
          : messages.map((m, i) => (
            <Fragment key={m.id}>
              {i === firstUnreadIdx && firstUnreadIdx >= 1 && <div className="chat-unread-divider"><span>{t("chat.newMessages")}</span></div>}
              <MessageRow m={m} prev={messages[i - 1]} locale={locale} mine={m.author_email === session.email} canManage={canManage} highlighted={highlight === m.id}
                onReact={react} onEdit={async (body) => { await backend.chatEdit(m.id, body); }}
                onDelete={async () => { await backend.chatDelete(m.id); }} onMarkUnread={() => markUnread(m.id)}
                onExtractTasks={() => setExtracting(m)}
                onForward={() => setForwarding(m.id)} onSave={async () => { try { if (m.saved) await backend.chatUnsave(m.id); else await backend.chatSave(m.id); refresh(); } catch (e) { toast((e as Error).message); } }} onOpenThread={onOpenThread}
                onOpenProfile={() => setProfile({ user_id: m.user_id, email: m.author_email, name: m.author, role: "" })} />
            </Fragment>
          ))}
        {/* Recibo de leitura: quem já leu até a última mensagem (last_read_at ≥ created_at dela). */}
        {(() => {
          const last = messages[messages.length - 1];
          if (!last || last.deleted) return null;
          const lastTs = new Date(last.created_at).getTime();
          const readers = (membersQ.data ?? []).filter((mem) => mem.email !== last.author_email && mem.email !== session.email && mem.last_read_at && new Date(mem.last_read_at).getTime() >= lastTs);
          if (readers.length === 0) return null;
          const label = channel.kind === "dm"
            ? t("chat.seen")
            : t("chat.seenBy", { names: readers.slice(0, 3).map((r) => r.name || r.email).join(", ") }) + (readers.length > 3 ? ` +${readers.length - 3}` : "");
          return <div className="read-receipt">✓ {label}</div>;
        })()}
        {typingAuthor && <div className="typing">{t("chat.typing", { name: typingAuthor })}</div>}
      </div>

      <Composer ref={composerRef} channelId={channel.id} members={membersQ.data ?? []} onTyping={onTyping} dm={channel.kind === "dm"}
        onSent={() => { refresh(); qc.invalidateQueries({ queryKey: ["chat-channels"] }); }} />
      </>}

      {over && <div className="drop-overlay"><div className="drop-card"><IcPaperclip size={22} /> {t("chat.dropHere")}</div></div>}
    </div>
  );
}

const URL_RE = /(https?:\/\/[^\s<>"']+)/;

// LinkUnfurl busca (uma vez, cacheado) os metadados de uma URL e mostra um cartão de preview.
function LinkUnfurl({ url }: { url: string }) {
  const q = useQuery({ queryKey: ["unfurl", url], queryFn: () => backend.chatUnfurl(url), retry: false, staleTime: 3600_000 });
  const d = q.data?.unfurl;
  if (!d) return null;
  return (
    <a className="unfurl" href={d.url} target="_blank" rel="noopener noreferrer nofollow">
      {d.image && <img className="unfurl-img" src={d.image} alt="" loading="lazy" onError={(e) => { (e.currentTarget as HTMLImageElement).style.display = "none"; }} />}
      <div className="unfurl-txt">
        {d.site && <div className="unfurl-site">{d.site}</div>}
        <div className="unfurl-title">{d.title}</div>
        {d.description && <div className="unfurl-desc">{d.description}</div>}
      </div>
    </a>
  );
}

// Opções rápidas de horário (agendar envio / lembrar-me), estilo Slack.
function quickTimes(t: (k: string) => string): { label: string; date: Date }[] {
  const now = Date.now();
  const tomorrow9 = new Date(); tomorrow9.setDate(tomorrow9.getDate() + 1); tomorrow9.setHours(9, 0, 0, 0);
  return [
    { label: t("chat.in30m"), date: new Date(now + 30 * 60000) },
    { label: t("chat.in1h"), date: new Date(now + 60 * 60000) },
    { label: t("chat.in3h"), date: new Date(now + 180 * 60000) },
    { label: t("chat.tomorrow9"), date: tomorrow9 },
  ];
}
function TimeMenu({ onPick, onClose }: { onPick: (d: Date) => void; onClose: () => void }) {
  const { t } = useTranslation();
  return (
    <div className="time-menu" onMouseLeave={onClose}>
      {quickTimes(t).map((o) => (
        <button key={o.label} onMouseDown={(e) => { e.preventDefault(); onPick(o.date); }}>{o.label}</button>
      ))}
    </div>
  );
}

function BookmarkIcon({ filled, size = 13 }: { filled?: boolean; size?: number }) {
  return <svg width={size} height={size} viewBox="0 0 24 24" fill={filled ? "currentColor" : "none"} stroke="currentColor" strokeWidth="1.9" strokeLinejoin="round" aria-hidden><path d="M6 3h12a1 1 0 0 1 1 1v17l-7-4-7 4V4a1 1 0 0 1 1-1Z" /></svg>;
}

function MessageRow({ m, prev, locale, mine, canManage, highlighted, onReact, onEdit, onDelete, onMarkUnread, onForward, onSave, onExtractTasks, onOpenThread, onOpenProfile }: {
  m: ChatMessage; prev?: ChatMessage; locale: string; mine: boolean; canManage?: boolean; highlighted?: boolean;
  onReact: (mid: string, emoji: string, mine: boolean) => void; onEdit: (body: string) => void; onDelete: () => void;
  onMarkUnread?: () => void; onForward?: () => void; onSave?: () => void; onExtractTasks?: () => void;
  onOpenThread?: (m: ChatMessage) => void; onOpenProfile?: () => void;
}) {
  const { t } = useTranslation();
  const [pick, setPick] = useState(false);
  const [full, setFull] = useState<DOMRect | null>(null);
  const [editing, setEditing] = useState(false);
  const [remind, setRemind] = useState(false);
  const [draft, setDraft] = useState(m.body);
  const [viewAtt, setViewAtt] = useState<ChatAttachment | null>(null);
  const call = m.deleted ? null : parseCall(m.body);
  const firstUrl = !m.deleted && !call ? (m.body.match(URL_RE)?.[1] ?? null) : null;
  const grouped = !call && prev && prev.user_id === m.user_id && (new Date(m.created_at).getTime() - new Date(prev.created_at).getTime() < 5 * 60000);

  return (
    <div className={"msg" + (grouped ? " grouped" : "") + (highlighted ? " flash" : "")} data-mid={m.id}>
      {grouped ? <span className="msg-gutter" /> : m.bot_name ? <span className="av bot-av">{initials(m.bot_name)}</span> : <Avatar name={m.author} email={m.author_email} userId={m.user_id} size={34} className="clickable" onClick={onOpenProfile} />}
      <div className="msg-body">
        {!grouped && <div className="msg-h">{m.bot_name ? <b className="author-name">{m.bot_name} <span className="bot-badge">{t("chat.appBadge")}</span></b> : <b className="author-name" onClick={onOpenProfile}>{m.author}</b>}<span className="tm">{shortTime(m.created_at, locale)}</span></div>}
        {m.deleted ? <div className="msg-txt deleted">{t("chat.deleted")}</div>
          : call ? <CallCard call={call} author={m.author} />
          : editing ? (
            <div className="msg-edit">
              {/* textarea (não input): mensagens têm markdown/multilinha. Enter salva, Shift+Enter quebra linha. */}
              <textarea autoFocus rows={1} value={draft}
                onChange={(e) => { setDraft(e.target.value); e.target.style.height = "auto"; e.target.style.height = Math.min(e.target.scrollHeight, 200) + "px"; }}
                onKeyDown={(e) => { if (e.key === "Enter" && !e.shiftKey) { e.preventDefault(); if (draft.trim()) { onEdit(draft.trim()); setEditing(false); } } if (e.key === "Escape") setEditing(false); }} />
              <button className="chipbtn" onClick={() => { if (draft.trim()) { onEdit(draft.trim()); setEditing(false); } }}>{t("chat.save")}</button>
            </div>
          ) : m.body ? <div className="msg-txt">{renderRich(m.body)}{m.edited_at && <span className="edited"> ({t("chat.edited")})</span>}</div> : null}

        {firstUrl && !editing && <LinkUnfurl url={firstUrl} />}

        {!m.deleted && m.attachments?.length > 0 && (
          <div className="msg-atts">
            {m.attachments.map((a) => {
              const kind = viewerKind(a.name);
              return kind === "image" ? (
                <button key={a.file_id} className="att-thumb" onClick={() => setViewAtt(a)}>
                  <img src={backend.chatAttachmentUrl(a.file_id, true)} alt={a.name} loading="lazy" />
                </button>
              ) : (
                <button key={a.file_id} className="att-file" onClick={() => (kind ? setViewAtt(a) : window.open(backend.chatAttachmentUrl(a.file_id), "_blank"))}>
                  <span className="att-ic"><IcPaperclip size={15} /></span>
                  <span className="att-meta"><span className="nm">{a.name}</span><span className="z">{KB(a.size)}</span></span>
                </button>
              );
            })}
          </div>
        )}
        {viewAtt && <ChatAttachmentViewer attachment={viewAtt} onClose={() => setViewAtt(null)} />}

        {!m.deleted && m.reactions?.length > 0 && (
          <div className="reacts">
            {m.reactions.map((r) => (
              <button key={r.emoji} className={"react" + (r.mine ? " mine" : "")} onClick={() => onReact(m.id, r.emoji, r.mine)}>
                {r.emoji} {r.count}
              </button>
            ))}
          </div>
        )}
        {onOpenThread && m.reply_count > 0 && (
          <button className="thread-chip" onClick={() => onOpenThread(m)}>
            <IcReply size={13} /> {t("chat.replies", { count: m.reply_count })}
          </button>
        )}
      </div>
      {!m.deleted && !call && (
        <div className="msg-actions">
          <div className="react-wrap">
            <button className="ib sm" title={t("chat.react")} onClick={() => setPick((v) => !v)}>☺</button>
            {pick && (
              <div className="emoji-pop" onMouseLeave={() => setPick(false)}>
                {QUICK.map((e) => <button key={e} onClick={() => { onReact(m.id, e, false); pushRecent(e); setPick(false); }}>{e}</button>)}
                <button className="more" title={t("chat.emojiMore")} onClick={(e) => { setFull(e.currentTarget.getBoundingClientRect()); setPick(false); }}>+</button>
              </div>
            )}
            {full && <EmojiPicker anchor={full} onClose={() => setFull(null)}
              onPick={(ch) => { onReact(m.id, ch, false); pushRecent(ch); setFull(null); }} />}
          </div>
          {onOpenThread && <button className="ib sm" title={t("chat.reply")} onClick={() => onOpenThread(m)}><IcReply size={13} /></button>}
          {onForward && !m.deleted && <button className="ib sm" title={t("chat.forward")} onClick={onForward}><IcForward size={13} /></button>}
          {onSave && !m.deleted && <button className={"ib sm" + (m.saved ? " saved-on" : "")} title={m.saved ? t("chat.unsaveMsg") : t("chat.saveMsg")} onClick={onSave}><BookmarkIcon filled={m.saved} /></button>}
          <button className="ib sm" title={t("chat.pin")} onClick={() => void backend.chatPin(m.id, true).then(() => toast(t("chat.pinned"))).catch(() => {})}><IcPin size={13} /></button>
          {!m.deleted && (
            <span className="remind-wrap">
              <button className="ib sm" title={t("chat.remindMe")} onClick={() => setRemind((v) => !v)}><IcBell size={13} /></button>
              {remind && <TimeMenu onClose={() => setRemind(false)}
                onPick={(d) => { setRemind(false); void backend.chatRemind(m.id, d.toISOString()).then(() => toast(t("chat.reminderSet"))).catch((e) => toast((e as Error).message)); }} />}
            </span>
          )}
          {onMarkUnread && <button className="ib sm" title={t("chat.markUnread")} onClick={onMarkUnread}><IcMail size={13} /></button>}
          {onExtractTasks && m.body.trim() !== "" && (
            <button className="ib sm" title={t("webmail.ai.extractTasks")} onClick={onExtractTasks}><IcColumns size={13} /></button>
          )}
          {mine && <button className="ib sm" title={t("chat.edit")} onClick={() => { setDraft(m.body); setEditing(true); }}><IcPencil size={13} /></button>}
          {/* Apaga: o autor a própria; um gestor/owner modera as de terceiros. */}
          {(mine || canManage) && <button className="ib sm" title={t("chat.delete")} onClick={onDelete}><IcTrash size={13} /></button>}
        </div>
      )}
    </div>
  );
}

// Mensagens agendadas do usuário: lista com hora + canal, com opção de cancelar antes do envio.
function ScheduledModal({ onClose }: { onClose: () => void }) {
  const { t, i18n } = useTranslation();
  const qc = useQueryClient();
  const q = useQuery({ queryKey: ["chat-scheduled"], queryFn: () => backend.chatScheduled(), retry: false });
  const cancel = useMutation({ mutationFn: (id: string) => backend.chatCancelScheduled(id), onSuccess: () => qc.invalidateQueries({ queryKey: ["chat-scheduled"] }), onError: (e) => toast((e as Error).message) });
  const items = q.data ?? [];
  return (
    <div className="tk-bg open" onMouseDown={onClose}>
      <div className="tk modal-sm" onMouseDown={(e) => e.stopPropagation()}>
        <header><span>{t("chat.scheduledTitle")}</span><button className="ib" title={t("common.close")} onClick={onClose}><IcX size={15} /></button></header>
        <div className="tk-body">
          {items.length === 0 ? <p className="desc">{t("chat.scheduledEmpty")}</p> : items.map((s) => (
            <div className="sched-row" key={s.id}>
              <div className="grow">
                <div className="sr-when">#{s.channel_name} · {new Date(s.send_at).toLocaleString(i18n.language, { dateStyle: "short", timeStyle: "short" })}</div>
                <div className="sr-body">{s.body}</div>
              </div>
              <button className="ib" title={t("common.cancel")} onClick={() => cancel.mutate(s.id)}><IcTrash size={14} /></button>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}

// Cartão de chamada: mostra quem iniciou e um botão para entrar na sala do Meet (LiveKit).
function CallCard({ call, author }: { call: { mode: "video" | "audio"; room: string }; author: string }) {
  const { t } = useTranslation();
  return (
    <div className="call-card">
      <span className="call-ic">{call.mode === "video" ? <IcVideo size={18} /> : <IcPhone size={17} />}</span>
      <div className="call-info">
        <b>{t(call.mode === "video" ? "chat.videoCallStarted" : "chat.audioCallStarted")}</b>
        <span>{t("chat.callBy", { name: author })}</span>
      </div>
      <button className="call-join" onClick={() => openRoom(call.room, call.mode === "audio")}>{t("chat.joinCall")}</button>
    </div>
  );
}

// Painel de thread (3ª coluna): mensagem-pai + respostas + composer que responde no parent_id.
function ThreadPanel({ parent, channelId, session, locale, onClose }: {
  parent: ChatMessage; channelId: string; session: WebmailSession; locale: string; onClose: () => void;
}) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const repliesQ = useQuery({ queryKey: ["chat-thread", parent.id], queryFn: () => backend.chatReplies(parent.id) });
  const membersQ = useQuery({ queryKey: ["chat-members", channelId], queryFn: () => backend.chatMembers(channelId) });
  const replies = repliesQ.data?.replies ?? [];
  // Só revalida a thread (query pequena). A lista principal é atualizada incrementalmente pelo eco do
  // WS (contador de respostas do pai, edições, reações) — não refaz o canal inteiro.
  const refresh = () => { qc.invalidateQueries({ queryKey: ["chat-thread", parent.id] }); };
  const react = async (mid: string, emoji: string, mine: boolean) => { await backend.chatReact(mid, emoji, mine); refresh(); };
  const composerRef = useRef<ComposerHandle>(null);
  const { over, handlers } = useFileDrop((files) => composerRef.current?.addFiles(files));

  return (
    <aside className="thread-panel" {...handlers}>
      <header className="chat-head"><b>{t("chat.thread")}</b><div className="grow" /><button className="ib" title={t("common.close")} onClick={onClose}><IcX size={15} /></button></header>
      <div className="chat-msgs">
        <MessageRow m={parent} locale={locale} mine={parent.author_email === session.email} onReact={react}
          onEdit={async (body) => { await backend.chatEdit(parent.id, body); refresh(); }} onDelete={async () => { await backend.chatDelete(parent.id); refresh(); }} />
        <div className="thread-sep">{t("chat.replies", { count: replies.length })}</div>
        {replies.map((m, i) => (
          <MessageRow key={m.id} m={m} prev={replies[i - 1]} locale={locale} mine={m.author_email === session.email}
            onReact={react} onEdit={async (body) => { await backend.chatEdit(m.id, body); refresh(); }}
            onDelete={async () => { await backend.chatDelete(m.id); refresh(); }} />
        ))}
      </div>
      <Composer ref={composerRef} channelId={channelId} members={membersQ.data ?? []} parentId={parent.id} onTyping={() => {}} onSent={refresh} />
      {over && <div className="drop-overlay"><div className="drop-card"><IcPaperclip size={22} /> {t("chat.dropHere")}</div></div>}
    </aside>
  );
}

export interface ComposerHandle { addFiles: (files: FileList | File[]) => void }

const Composer = forwardRef<ComposerHandle, {
  channelId: string; members: ChatMember[]; onTyping: () => void; onSent: () => void; parentId?: string; dm?: boolean;
}>(function Composer({ channelId, members, onTyping, onSent, parentId = "", dm = false }, ref) {
  const { t } = useTranslation();
  const settings = useUserSettings(); // settings.chat.enterSends
  const [text, setText] = useState("");
  const [mentionQ, setMentionQ] = useState<string | null>(null);
  const [emoji, setEmoji] = useState<DOMRect | null>(null);
  const [gif, setGif] = useState<DOMRect | null>(null);
  const [drivePick, setDrivePick] = useState(false);
  const [atts, setAtts] = useState<ChatAttachment[]>([]);
  const [uploading, setUploading] = useState(0);
  const [sched, setSched] = useState(false);
  const mentioned = useRef<Set<string>>(new Set());
  const lastTyping = useRef(0);
  const ta = useRef<HTMLTextAreaElement>(null);
  const fileIn = useRef<HTMLInputElement>(null);

  async function onFiles(files: FileList | File[] | null) {
    if (!files) return;
    for (const f of Array.from(files)) {
      setUploading((n) => n + 1);
      try { const sf = await backend.uploadFile(f); setAtts((a) => [...a, { file_id: sf.id, name: sf.nome, size: sf.tamanho }]); }
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
    const now = Date.now();
    if (now - lastTyping.current > 2000) { lastTyping.current = now; onTyping(); }
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
        <div className="rich-toolbar">
          <button title={t("chat.bold")} onMouseDown={(e) => { e.preventDefault(); wrap("**"); }}><b>B</b></button>
          <button title={t("chat.italic")} onMouseDown={(e) => { e.preventDefault(); wrap("_"); }}><i>I</i></button>
          <button title={t("chat.strike")} onMouseDown={(e) => { e.preventDefault(); wrap("~~"); }}><s>S</s></button>
          <span className="sep" />
          <button title={t("chat.code")} onMouseDown={(e) => { e.preventDefault(); wrap("`"); }}>{"</>"}</button>
          <button title={t("chat.codeBlock")} onMouseDown={(e) => { e.preventDefault(); wrap("\n```\n", "\n```\n"); }}>▤</button>
          <button title={t("chat.link")} onMouseDown={(e) => { e.preventDefault(); wrap("[", "](url)"); }}>🔗</button>
          <span className="sep" />
          <button title={t("chat.list")} onMouseDown={(e) => { e.preventDefault(); wrap("\n- ", ""); }}>•</button>
          <button title={t("chat.quote")} onMouseDown={(e) => { e.preventDefault(); wrap("\n> ", ""); }}>❝</button>
          <button title={t("chat.attach")} onMouseDown={(e) => { e.preventDefault(); fileIn.current?.click(); }}><IcPaperclip size={15} /></button>
          <button title={t("chat.attachDrive")} onMouseDown={(e) => { e.preventDefault(); setDrivePick(true); }}><IcFolder size={15} /></button>
          <div className="react-wrap">
            <button title={t("chat.emoji")} className="emoji-btn"
              onMouseDown={(e) => { e.preventDefault(); const r = e.currentTarget.getBoundingClientRect(); setEmoji((v) => (v ? null : r)); }}>😀</button>
            {emoji && <EmojiPicker anchor={emoji} onPick={insertEmoji} onClose={() => setEmoji(null)} />}
          </div>
          <div className="react-wrap">
            <button title={t("chat.gif")} className="gif-btn"
              onMouseDown={(e) => { e.preventDefault(); const r = e.currentTarget.getBoundingClientRect(); setGif((v) => (v ? null : r)); }}>GIF</button>
            {/* O GIF entra como anexo — é o mesmo caminho de qualquer imagem, já com visualizador. */}
            {gif && <GifPicker anchor={gif} onClose={() => setGif(null)}
              onPick={(g) => { setAtts((a) => [...a, { file_id: g.file_id, name: g.name, size: g.size }]); setGif(null); }} />}
            {drivePick && <DrivePickerDrawer onClose={() => setDrivePick(false)}
              onPick={(f) => setAtts((a) => [...a, { file_id: f.id, name: f.nome, size: f.tamanho }])} />}
          </div>
        </div>
        <textarea ref={ta} rows={1} value={text} placeholder={t("chat.messagePh")} onChange={(e) => onChange(e.target.value)}
          onInput={(e) => { const el = e.currentTarget; el.style.height = "auto"; el.style.height = Math.min(el.scrollHeight, 180) + "px"; }}
          onPaste={(e) => {
            // Colar imagem (print) ou ficheiro do clipboard → vira anexo, como arrastar. Só intercepta
            // quando há ficheiros; texto normal continua a colar-se como sempre.
            const files = e.clipboardData?.files;
            if (files && files.length) { e.preventDefault(); void onFiles(files); }
          }}
          onKeyDown={(e) => {
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
          <span className="hint">{t("chat.enterHint")}</span>
          <span className="sched-wrap">
            <button className="ib sm" title={t("chat.scheduleSend")} disabled={!text.trim()} onClick={() => setSched((v) => !v)}><IcClock size={15} /></button>
            {sched && <TimeMenu onClose={() => setSched(false)}
              onPick={(d) => { setSched(false); const b = text.trim(); if (!b) return; void backend.chatSchedule(channelId, b, d.toISOString()).then(() => { setText(""); toast(t("chat.scheduled")); }).catch((e) => toast((e as Error).message)); }} />}
          </span>
          <button className="send" disabled={(!text.trim() && atts.length === 0) || uploading > 0} onClick={send}>{t("chat.send")}</button>
        </div>
      </div>
    </div>
  );
});

// Integrações (webhooks de entrada): cada uma é um token de URL pública que vira mensagem-bot no
// canal. Útil para CI/alertas/serviços externos postarem sem login. Só gestores criam/removem.
function WebhooksSection({ channelId }: { channelId: string }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [name, setName] = useState("");
  const hooksQ = useQuery({ queryKey: ["chat-webhooks", channelId], queryFn: () => backend.chatWebhooks(channelId) });
  const hooks = hooksQ.data ?? [];
  const refresh = () => qc.invalidateQueries({ queryKey: ["chat-webhooks", channelId] });
  const hookUrl = (token: string) => `${window.location.origin}/api/public/chat/hook/${token}`;
  const create = async () => {
    try { await backend.chatCreateWebhook(channelId, name.trim() || t("chat.webhookDefault")); setName(""); refresh(); }
    catch (e) { toast((e as Error).message); }
  };
  const del = async (id: string) => {
    if (!window.confirm(t("chat.webhookDeleteConfirm"))) return;
    try { await backend.chatDeleteWebhook(id); refresh(); } catch (e) { toast((e as Error).message); }
  };
  const copy = (token: string) => { navigator.clipboard?.writeText(hookUrl(token)); toast(t("chat.webhookCopied")); };
  return (
    <div className="chan-webhooks">
      <h4>{t("chat.webhooks")}</h4>
      <p className="wh-hint">{t("chat.webhooksHint")}</p>
      <div className="wh-add">
        <input value={name} onChange={(e) => setName(e.target.value)} placeholder={t("chat.webhookNamePh")} onKeyDown={(e) => { if (e.key === "Enter") create(); }} />
        <button className="chipbtn" onClick={create}>{t("chat.webhookCreate")}</button>
      </div>
      {hooks.map((h) => (
        <div key={h.id} className="wh-row">
          <div className="wh-who"><b>{h.name}</b><code className="wh-url">{hookUrl(h.token)}</code></div>
          <button className="chipbtn" onClick={() => copy(h.token)}>{t("chat.webhookCopy")}</button>
          <button className="chipbtn danger" onClick={() => del(h.id)}>{t("chat.removeMember")}</button>
        </div>
      ))}
    </div>
  );
}

// Painel de membros e definições do canal. Quem pode adicionar depende da política (aberta = qualquer
// membro; restrita = só gestor). Gestores (owner/manager) removem, promovem e editam as definições.
function ChannelPanel({ channel, me, onClose }: { channel: ChatChannel; me: string; onClose: () => void }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [pick, setPick] = useState("");
  const [filter, setFilter] = useState("");
  const [page, setPage] = useState(1);
  const [listed, setListed] = useState(channel.listed !== false);
  const [restricted, setRestricted] = useState(channel.add_policy === "restricted");

  // Membros com busca + paginação no SERVIDOR (aguenta canais grandes).
  const membersQ = useQuery({ queryKey: ["chat-members", channel.id, filter, page], queryFn: () => backend.chatMembersSearch(channel.id, filter, page) });
  const colleagues = useQuery({ queryKey: ["colleagues"], queryFn: () => colleaguesApi.list(), retry: false });

  const canManage = channel.my_role === "owner" || channel.my_role === "manager";
  const canAdd = canManage || channel.add_policy !== "restricted";
  const members = membersQ.data?.members ?? [];
  const total = membersQ.data?.total ?? 0;
  const q = pick.trim().toLowerCase();
  // Adicionar é idempotente no servidor (ON CONFLICT), então não é preciso excluir os já-membros.
  const candidates = q ? (colleagues.data ?? []).filter((c) => c.name.toLowerCase().includes(q) || c.email.toLowerCase().includes(q)).slice(0, 6) : [];
  const refresh = () => { membersQ.refetch(); qc.invalidateQueries({ queryKey: ["chat-channels"] }); };

  const add = async (email: string) => { try { await backend.chatAddMember(channel.id, email); setPick(""); refresh(); } catch (e) { toast((e as Error).message); } };
  const remove = async (userId: string) => { try { await backend.chatRemoveMember(channel.id, userId); refresh(); } catch (e) { toast((e as Error).message); } };
  const setRole = async (userId: string, role: "manager" | "member") => { try { await backend.chatSetMemberRole(channel.id, userId, role); refresh(); } catch (e) { toast((e as Error).message); } };
  const saveSettings = async () => { try { await backend.chatUpdateChannel(channel.id, { listed, add_policy: restricted ? "restricted" : "open" }); refresh(); toast(t("chat.settingsSaved")); } catch (e) { toast((e as Error).message); } };

  return (
    <div className="tk-bg open" onMouseDown={onClose}>
      <div className="tk" onMouseDown={(e) => e.stopPropagation()} style={{ width: 460 }}>
        <header><span>#{channel.name} — {t("chat.members")}</span><button className="ib" title={t("common.close")} onClick={onClose}>✕</button></header>
        <div className="body">
          {canAdd && (
            <div className="rcpt-in" style={{ position: "relative", marginBottom: 12 }}>
              <input autoFocus value={pick} onChange={(e) => setPick(e.target.value)} placeholder={t("chat.addMemberPh")} />
              {candidates.length > 0 && (
                <div className="rcpt-sugg">
                  {candidates.map((c) => (
                    <button key={c.id} type="button" onMouseDown={(e) => { e.preventDefault(); add(c.email); }}>
                      <Avatar name={c.name} email={c.email} size={28} />
                      <span className="nm">{c.name || c.email}{c.email === me ? " (você)" : ""}</span><span className="em">{c.email}</span>
                    </button>
                  ))}
                </div>
              )}
            </div>
          )}
          <div className="chan-members-head">
            <span>{t("chat.membersCount", { n: total })}</span>
            <input className="chan-filter" value={filter} onChange={(e) => { setPage(1); setFilter(e.target.value); }} placeholder={t("chat.searchMembers")} />
          </div>
          <div className="chan-members">
            {members.length === 0 && <div className="empty" style={{ padding: 18 }}>{t("chat.noMembersFound")}</div>}
            {members.map((m) => (
              <div key={m.user_id} className="chan-member">
                <Avatar name={m.name} email={m.email} userId={m.user_id} size={28} />
                <div className="who"><b>{m.name || m.email}</b><small>{m.email}</small></div>
                {m.role !== "member" && <span className="role-badge">{t(m.role === "owner" ? "chat.roleOwner" : "chat.roleManager")}</span>}
                {canManage && m.role !== "owner" && m.email !== me && (
                  <div className="member-actions">
                    <button className="chipbtn" onClick={() => setRole(m.user_id, m.role === "manager" ? "member" : "manager")}>
                      {t(m.role === "manager" ? "chat.demote" : "chat.promote")}
                    </button>
                    <button className="chipbtn danger" onClick={() => remove(m.user_id)}>{t("chat.removeMember")}</button>
                  </div>
                )}
              </div>
            ))}
          </div>
          {total > 20 && (
            <div className="ann-pager">
              <button className="ib" title={t("common.prevPage")} disabled={page <= 1} onClick={() => setPage((p) => p - 1)}><IcChevron size={16} className="left" /></button>
              <span>{t("announce.pageOf", { page, pages: Math.ceil(total / 20) })}</span>
              <button className="ib" title={t("common.nextPage")} disabled={page * 20 >= total} onClick={() => setPage((p) => p + 1)}><IcChevron size={16} className="right" /></button>
            </div>
          )}
          {canManage && (
            <div className="chan-settings">
              <h4>{t("chat.channelSettings")}</h4>
              {channel.kind === "public" && <div className="switch-row full"><span>{t("chat.unlisted")}</span><Switch checked={!listed} onChange={(on) => setListed(!on)} label={t("chat.unlisted")} /></div>}
              <div className="switch-row full"><span>{t("chat.restrictedAdd")}</span><Switch checked={restricted} onChange={setRestricted} label={t("chat.restrictedAdd")} /></div>
              <button className="add" onClick={saveSettings}>{t("chat.saveSettings")}</button>
              {channel.kind !== "dm" && <WebhooksSection channelId={channel.id} />}
              {channel.my_role === "owner" && channel.kind !== "dm" && (
                <button className="chan-delete" onClick={async () => {
                  if (!window.confirm(t("chat.deleteChannelConfirm", { name: channel.name }))) return;
                  try { await backend.chatDeleteChannel(channel.id); qc.invalidateQueries({ queryKey: ["chat-channels"] }); onClose(); }
                  catch (e) { toast((e as Error).message); }
                }}><IcTrash size={14} /> {t("chat.deleteChannel")}</button>
              )}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

// --- Abas do painel do canal ---

// Canvas: documento rico partilhado do canal. Qualquer membro edita e guarda.
function CanvasTab({ channelId }: { channelId: string }) {
  const { t } = useTranslation();
  const ed = useRef<HTMLDivElement>(null);
  const [saving, setSaving] = useState(false);
  const canvasQ = useQuery({ queryKey: ["chat-canvas", channelId], queryFn: () => backend.chatCanvas(channelId) });
  // Canvas é HTML gravado por qualquer membro do canal. Sanitizar SEMPRE antes de injetar via
  // innerHTML (na leitura) e antes de persistir (defesa em profundidade) — senão é stored XSS.
  useEffect(() => { if (ed.current && canvasQ.data) ed.current.innerHTML = sanitizeRich(canvasQ.data.canvas); }, [canvasQ.data]);
  const exec = (e: React.MouseEvent, cmd: string) => { e.preventDefault(); document.execCommand(cmd); ed.current?.focus(); };
  const save = async () => { setSaving(true); try { await backend.chatSaveCanvas(channelId, sanitizeRich(ed.current?.innerHTML ?? "")); toast(t("chat.canvasSaved")); } catch (e) { toast((e as Error).message); } finally { setSaving(false); } };
  const clear = async () => {
    if (!window.confirm(t("chat.canvasClearConfirm"))) return;
    if (ed.current) ed.current.innerHTML = "";
    try { await backend.chatSaveCanvas(channelId, ""); toast(t("chat.canvasCleared")); } catch (e) { toast((e as Error).message); }
  };
  if (canvasQ.isLoading) return <div className="loading">…</div>;
  return (
    <div>
      <div className="tools sig-tools">
        <button title="Bold" onMouseDown={(e) => exec(e, "bold")}><b>B</b></button>
        <button title="Italic" onMouseDown={(e) => exec(e, "italic")}><i>I</i></button>
        <button title="List" onMouseDown={(e) => exec(e, "insertUnorderedList")}>☰</button>
        <button title="Heading" onMouseDown={(e) => { e.preventDefault(); document.execCommand("formatBlock", false, "h3"); }}>H</button>
        <span className="grow" />
        <button className="chipbtn danger" onClick={clear}>{t("chat.canvasClear")}</button>
        <button className="add" disabled={saving} onClick={save}>{saving ? "…" : t("chat.saveCanvas")}</button>
      </div>
      <div className="sig-surface" style={{ minHeight: 300 }} ref={ed} contentEditable suppressContentEditableWarning data-ph={t("chat.canvasPh")} />
    </div>
  );
}

// Pinos: mensagens fixadas do canal.
function PinsTab({ channelId, onJump }: { channelId: string; onJump?: (msgId: string) => void }) {
  const { t, i18n } = useTranslation();
  const pinsQ = useQuery({ queryKey: ["chat-pins", channelId], queryFn: () => backend.chatPins(channelId) });
  const unpin = async (id: string) => { try { await backend.chatPin(id, false); pinsQ.refetch(); } catch (e) { toast((e as Error).message); } };
  const pins = pinsQ.data?.pins ?? [];
  return (
    <div>
      {pins.length === 0 ? <div className="ann-empty">{t("chat.noPins")}</div> : (
        <div className="chan-list">
          {pins.map((p) => (
            <div key={p.id} className="chan-listitem">
              <button className="cl-jump" style={{ flex: 1, minWidth: 0, textAlign: "left", background: "none", border: 0, cursor: "pointer" }} onClick={() => onJump?.(p.id)} title={t("chat.jumpToMessage")}>
                <div className="cl-body">{p.body}</div>
                <div className="cl-meta">{p.author} · {new Date(p.created_at).toLocaleDateString(i18n.language, { day: "2-digit", month: "short" })}</div>
              </button>
              <button className="chipbtn" onClick={() => void unpin(p.id)}>{t("chat.unpin")}</button>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

// Ficheiros & links do canal (coleção curada). Qualquer membro adiciona e remove.
function FilesLinksTab({ channelId }: { channelId: string }) {
  const { t } = useTranslation();
  const [uploading, setUploading] = useState(false);
  const fileIn = useRef<HTMLInputElement>(null);
  const bmQ = useQuery({ queryKey: ["chat-bookmarks", channelId], queryFn: () => backend.chatBookmarks(channelId) });
  const items = bmQ.data?.bookmarks ?? [];
  const files = items.filter((b) => b.kind === "file");
  const links = items.filter((b) => b.kind === "link");
  const fmtSize = (n: number) => n >= 1 << 20 ? `${(n / (1 << 20)).toFixed(1)} MB` : `${Math.max(1, Math.round(n / 1024))} KB`;

  const addLink = async () => {
    const url = window.prompt(t("chat.addLinkPrompt"), "https://");
    if (!url) return;
    const title = window.prompt(t("chat.addLinkTitle"), "") ?? "";
    try { await backend.chatAddBookmark(channelId, { kind: "link", url, title }); bmQ.refetch(); } catch (e) { toast((e as Error).message); }
  };
  const onFile = async (f?: File) => {
    if (!f) return;
    setUploading(true);
    try {
      const rec = await backend.uploadFile(f);
      await backend.chatAddBookmark(channelId, { kind: "file", file_id: rec.id, file_name: f.name, file_size: f.size });
      bmQ.refetch();
    } catch (e) { toast((e as Error).message); }
    finally { setUploading(false); }
  };
  const remove = async (bid: string) => { try { await backend.chatDeleteBookmark(channelId, bid); bmQ.refetch(); } catch (e) { toast((e as Error).message); } };

  return (
    <div>
      <div className="tabpane-actions">
        <button className="chipbtn" onClick={addLink}><IcLink size={14} /> {t("chat.addLink")}</button>
        <button className="chipbtn" disabled={uploading} onClick={() => fileIn.current?.click()}><IcPaperclip size={14} /> {uploading ? "…" : t("chat.addFile")}</button>
        <input ref={fileIn} type="file" hidden onChange={(e) => { void onFile(e.target.files?.[0]); e.target.value = ""; }} />
      </div>
      <h4 style={{ margin: "4px 0 8px" }}>{t("chat.files")} <span className="n">{files.length}</span></h4>
      {files.length === 0 ? <div className="ann-empty" style={{ marginBottom: 14 }}>{t("chat.noFiles")}</div> : (
        <div className="chan-list" style={{ marginBottom: 16 }}>
          {files.map((f) => (
            <div key={f.id} className="chan-listitem">
              <IcPaperclip size={15} />
              <a href={backend.bookmarkDownloadUrl(channelId, f.id)} style={{ flex: 1, minWidth: 0, color: "inherit", textDecoration: "none" }}>
                <div className="cl-body">{f.file_name}</div><div className="cl-meta">{fmtSize(f.file_size)}</div>
              </a>
              <button className="ib" title={t("chat.removeBookmark")} onClick={() => void remove(f.id)}><IcTrash size={14} /></button>
            </div>
          ))}
        </div>
      )}
      <h4 style={{ margin: "0 0 8px" }}>{t("chat.links")} <span className="n">{links.length}</span></h4>
      {links.length === 0 ? <div className="ann-empty">{t("chat.noLinks")}</div> : (
        <div className="chan-list">
          {links.map((l) => (
            <div key={l.id} className="chan-listitem">
              <IcLink size={15} />
              <a href={l.url} target="_blank" rel="noopener noreferrer nofollow" style={{ flex: 1, minWidth: 0, color: "inherit", textDecoration: "none" }}>
                <div className="cl-body">{l.title || l.url}</div>{l.title && <div className="cl-meta">{l.url}</div>}
              </a>
              <button className="ib" title={t("chat.removeBookmark")} onClick={() => void remove(l.id)}><IcTrash size={14} /></button>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

// Cartão de perfil do membro (ao clicar no nome/avatar): detalhes + botões.
function MemberProfile({ member, me, onClose, onOpenDM }: { member: ChatMember; me: string; onClose: () => void; onOpenDM: (email: string) => void }) {
  const { t } = useTranslation();
  const isMe = member.email === me;
  const statusQ = useQuery({ queryKey: ["user-status", member.email], queryFn: () => backend.userStatus(member.email), retry: false, enabled: !member.status_emoji && !member.status_text });
  const stEmoji = member.status_emoji || statusQ.data?.emoji || "";
  const stText = member.status_text || statusQ.data?.text || "";
  return (
    <div className="tk-bg open" onMouseDown={onClose}>
      <div className="tk" onMouseDown={(e) => e.stopPropagation()} style={{ width: 340 }}>
        <div className="member-profile">
          <button className="ib prof-x" title={t("common.close")} onClick={onClose}><IcX size={15} /></button>
          <Avatar name={member.name} email={member.email} userId={member.user_id} size={48} className="prof-av" />
          <div className="prof-name">{member.name || member.email}</div>
          {(stEmoji || stText) && <div className="prof-status">{stEmoji} {stText}</div>}
          <div className="prof-email">{member.email}</div>
          {member.role && member.role !== "member" && <span className="role-badge" style={{ marginTop: 6 }}>{t(member.role === "owner" ? "chat.roleOwner" : "chat.roleManager")}</span>}
          {!isMe && (
            <div className="prof-actions">
              <button className="add" onClick={() => onOpenDM(member.email)}><IcMail size={15} /> {t("chat.message")}</button>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

function NewConvoModal({ mode, me, onClose, onCreated }: { mode: "channel" | "dm"; me: string; onClose: () => void; onCreated: (id: string) => void }) {
  const { t } = useTranslation();
  const [name, setName] = useState("");
  const [topic, setTopic] = useState("");
  const [priv, setPriv] = useState(false);     // privado (só por convite, nunca listado)
  const [unlisted, setUnlisted] = useState(false); // público mas não listado na descoberta
  const [restricted, setRestricted] = useState(false); // só gestores adicionam membros
  const [picked, setPicked] = useState<{ email: string; name: string }[]>([]); // DM: 1 pessoa = DM, 2+ = grupo
  const orgMembers = useQuery({ queryKey: ["colleagues"], queryFn: () => colleaguesApi.list(), retry: false, enabled: mode === "dm" });
  const q = name.trim().toLowerCase();
  // Conversa é com MEMBROS DA ORGANIZAÇÃO (colegas) — não contactos externos (não dá para lhes
  // enviar mensagens internas). Exclui a própria pessoa (24/07/2026).
  // Conversa é com MEMBROS DA ORGANIZAÇÃO (colegas) — não contactos externos. Mostra a lista LOGO
  // (sem adivinhar nomes) e filtra ao escrever. INCLUI a própria pessoa: mandar mensagem a si mesmo
  // é um espaço de notas pessoais (como no Slack/WhatsApp) — pedido 24/07/2026.
  const people = mode === "dm"
    ? (orgMembers.data ?? []).filter((c) => c.email && !picked.some((p) => p.email === c.email) && (!q || (c.name ?? "").toLowerCase().includes(q) || c.email.toLowerCase().includes(q))).slice(0, 8)
    : [];
  const addPerson = (c: { email: string; name: string }) => { setPicked((p) => [...p, { email: c.email, name: c.name || c.email }]); setName(""); };

  const create = async () => {
    try {
      if (mode === "channel") {
        const c = await backend.chatCreateChannel({
          name: name.trim(), topic: topic.trim(),
          kind: priv ? "private" : "public",
          listed: !priv && !unlisted,
          add_policy: restricted ? "restricted" : "open",
        });
        onCreated(c.id);
      }
    } catch (e) { toast((e as Error).message); }
  };
  // 1 pessoa → DM normal; 2+ → DM de grupo. Reutiliza o canal se já existir (dedup por dm_key).
  const startDM = async () => {
    try {
      const emails = picked.map((p) => p.email);
      const c = emails.length <= 1 ? await backend.chatOpenDM(emails[0]) : await backend.chatOpenGroupDM(emails);
      onCreated(c.id);
    } catch (e) { toast((e as Error).message); }
  };

  return (
    <div className="tk-bg open" onMouseDown={onClose}>
      <div className="tk" onMouseDown={(e) => e.stopPropagation()} style={{ width: 440 }}>
        <header><span>{mode === "channel" ? t("chat.newChannel") : t("chat.newDM")}</span><button className="ib" title={t("common.close")} onClick={onClose}>✕</button></header>
        <div className="body meet-form">
          {mode === "channel" ? (
            <>
              <label>{t("chat.channelName")}<input autoFocus value={name} onChange={(e) => setName(e.target.value)} placeholder={t("chat.channelNamePh")} /></label>
              <label>{t("chat.topic")}<input value={topic} onChange={(e) => setTopic(e.target.value)} placeholder={t("chat.topicPh")} /></label>
              <div className="switch-row"><span>{t("chat.private")}</span><Switch checked={priv} onChange={setPriv} label={t("chat.private")} /></div>
              {!priv && <div className="switch-row"><span>{t("chat.unlisted")}</span><Switch checked={unlisted} onChange={setUnlisted} label={t("chat.unlisted")} /></div>}
              <div className="switch-row"><span>{t("chat.restrictedAdd")}</span><Switch checked={restricted} onChange={setRestricted} label={t("chat.restrictedAdd")} /></div>
              <p className="desc">{priv ? t("chat.privateHint") : unlisted ? t("chat.unlistedHint") : t("chat.publicHint")}</p>
            </>
          ) : (
            <label>{t("chat.people")}
              {picked.length > 0 && (
                <div className="dm-chips">
                  {picked.map((p) => (
                    <span key={p.email} className="dm-chip"><Avatar name={p.name} email={p.email} size={18} className="xs" />{p.name}
                      <button className="x" title={t("common.remove")} onClick={() => setPicked((list) => list.filter((x) => x.email !== p.email))}>✕</button></span>
                  ))}
                </div>
              )}
              <div className="rcpt-in" style={{ position: "relative" }}>
                <input autoFocus value={name} onChange={(e) => setName(e.target.value)} placeholder={t("chat.peoplePh")} />
                {people.length > 0 && (
                  <div className="rcpt-sugg">
                    {people.map((c) => (
                      <button key={c.id} type="button" onMouseDown={(e) => { e.preventDefault(); addPerson({ email: c.email, name: c.name }); }}>
                        <Avatar name={c.name} email={c.email} size={28} />
                        <span className="nm">{c.name || c.email}{c.email === me ? " (você)" : ""}</span><span className="em">{c.email}</span>
                      </button>
                    ))}
                  </div>
                )}
              </div>
              <p className="desc">{picked.length >= 2 ? t("chat.groupDMHint") : t("chat.dmHint")}</p>
            </label>
          )}
        </div>
        <footer>
          <button className="chipbtn" onClick={onClose}>{t("common.cancel")}</button>
          {mode === "channel"
            ? <button className="add" disabled={!name.trim()} onClick={create}>{t("chat.create")}</button>
            : <button className="add" disabled={picked.length === 0} onClick={startDM}>{picked.length >= 2 ? t("chat.startGroupDM") : t("chat.startDM")}</button>}
        </footer>
      </div>
    </div>
  );
}

// ForwardModal escolhe o canal de destino para encaminhar uma mensagem.
function ForwardModal({ channels, onClose, onPick }: { channels: ChatChannel[]; onClose: () => void; onPick: (channelId: string) => void }) {
  const { t } = useTranslation();
  const [q, setQ] = useState("");
  const term = q.trim().toLowerCase();
  const list = channels.filter((c) => c.name.toLowerCase().includes(term));
  return (
    <div className="tk-bg open" onMouseDown={onClose}>
      <div className="tk" onMouseDown={(e) => e.stopPropagation()} style={{ width: 380 }}>
        <header><span>{t("chat.forwardTo")}</span><button className="ib" title={t("common.close")} onClick={onClose}>✕</button></header>
        <div className="body">
          <input autoFocus className="chan-filter" value={q} onChange={(e) => setQ(e.target.value)} placeholder={t("chat.searchChannel")} />
          <div className="chan-list" style={{ maxHeight: 320, overflowY: "auto", marginTop: 8 }}>
            {list.length === 0 && <div className="ann-empty" style={{ padding: 16 }}>{t("chat.noResults")}</div>}
            {list.map((c) => (
              <button key={c.id} className="fwd-target" onClick={() => onPick(c.id)}>
                <span className="hash">{c.kind === "dm" ? "@" : "#"}</span> {c.name}
              </button>
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}

// applyShortcodes troca ":atalho: " ou ":) " pelo emoji, só depois do espaço final.
function applyShortcodes(v: string): string {
  if (!v.endsWith(" ")) return v;
  const body = v.slice(0, -1);
  for (const [code, ch] of Object.entries(SHORTCODES)) {
    if (body.endsWith(code)) return body.slice(0, -code.length) + ch + " ";
  }
  return v;
}
