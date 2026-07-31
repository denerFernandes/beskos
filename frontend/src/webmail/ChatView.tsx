import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import type { WebmailSession } from "./auth";
import { backend, type ChatChannel, type ChatMessage } from "./backend";
import { useChatSocket } from "./useChatSocket";
import { setCustomEmoji } from "./customEmoji";
import { toast } from "./toast";
import { shortTime } from "./util";
import { Avatar } from "./Avatar";
import { IcPlus, IcUsers, IcChevron } from "../panel/icons";
import { applyReaction } from "./chatUtils";
import { BookmarkIcon } from "./ChatBits";
import { ChannelPane, ThreadPanel } from "./ChannelPane";
import { NewConvoModal } from "./ChatModals";

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
  // Arquivados: a lista só traz os que ESTA pessoa pode restaurar (dono do canal ou admin), por isso
  // tudo o que aparece aqui é acionável. Para os restantes, o canal simplesmente desaparece.
  const archQ = useQuery({ queryKey: ["chat-archived"], queryFn: () => backend.chatArchivedChannels(), retry: false, staleTime: 60000 });
  const archived = archQ.data?.channels ?? [];
  const [showArch, setShowArch] = useState(false);
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
            {archived.length > 0 && (
              <>
                <button className="chat-section as-toggle" onClick={() => setShowArch((v) => !v)}>
                  <IcChevron size={13} className={showArch ? "chev-open" : ""} /> {t("chat.archivedSection")} ({archived.length})
                </button>
                {showArch && archived.map((c) => (
                  <div key={c.id} className="chan archived">
                    <span className="cn"># {c.is_general ? t("chat.generalName") : c.name}</span>
                    <button className="join" onClick={async () => {
                      try {
                        await backend.chatArchiveChannel(c.id, false);
                        qc.invalidateQueries({ queryKey: ["chat-channels"] });
                        qc.invalidateQueries({ queryKey: ["chat-archived"] });
                        toast(t("chat.restored"));
                      } catch (e) { toast((e as Error).message); }
                    }}>{t("chat.restore")}</button>
                  </div>
                ))}
              </>
            )}
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
