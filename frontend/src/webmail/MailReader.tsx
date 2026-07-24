import { useEffect, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import type { TFunction } from "i18next";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import type { MailService } from "../jmap/mail";
import type { EmailAttachment, EmailFull, EmailAddress } from "../jmap/types";
import { backend, contactsApi, settingsApi } from "./backend";
import { toast } from "./toast";
import { AiThinking } from "./Spinner";
import { MailBody } from "./MailBody";
import { LabelChips } from "./MailLabels";
import { labelKeyword } from "./backend";
import { displayName, shortTime, fullDateTime, relativeTime } from "./util";
import { Avatar } from "./Avatar";
import { IcReply, IcReplyAll, IcForward, IcSparkle, IcColumns, IcCalendar, IcShield, IcFolder, IcTrash, IcMail, IcPrinter, IcChevron, IcPlus, IcArchive, IcCheck } from "../panel/icons";
import { TagIcon } from "./MailLabels";

// Reader de e-mail com IA real: resumo (backend.summarize) e ações que abrem os fluxos de IA/tarefa/evento.
// Carrega a CONVERSA inteira (mesmo threadId): a mensagem mais recente fica aberta, as anteriores
// recolhidas numa linha — clicar expande. As ações (responder, encaminhar…) agem na mais recente.
export function MailReader({
  service, emailId, locale, me, isSpam, onReply, onDraftAI, onExtractTasks, onSchedule, spamAction,
  moveTargets, onMove, onArchive, onSnooze, onTrash, onMarkUnread, onOpenAttachment,
}: {
  service: MailService;
  emailId: string | null;
  locale: string;
  me: string;
  isSpam?: boolean;
  onReply: (draft: { to?: string; cc?: string; subject?: string; body?: string; bodyHtml?: string }) => void;
  onDraftAI: (original: string, reply: { to?: string; cc?: string; subject?: string }) => void;
  onExtractTasks: (subject: string, body: string) => void;
  onSchedule: (subject: string) => void;
  spamAction?: { label: string; junk: boolean; run: () => void };
  moveTargets: { id: string; label: string }[];
  onMove: (mailboxId: string) => void;
  onArchive?: () => void; // arquivar de 1 clique (undefined = não aplicável nesta pasta)
  onSnooze?: (wake: Date) => void; // adiar (undefined = não aplicável nesta pasta)
  onTrash: () => void;
  onMarkUnread: () => void;
  onOpenAttachment: (a: EmailAttachment) => void;
}) {
  const [snoozeOpen, setSnoozeOpen] = useState(false);
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  const [wantSummary, setWantSummary] = useState(false); // resumo é sob demanda (botão), não automático
  // Barra de ações estilo Gmail: só o essencial à vista, o resto num menu ⋯. `replyOpen` é o menu
  // do botão Responder (a todos / encaminhar / IA); `moreOpen`/`moreView` é o ⋯ com sub-níveis
  // (mover, marcadores) à maneira do iOS.
  const [replyOpen, setReplyOpen] = useState(false);
  const [moreOpen, setMoreOpen] = useState(false);
  const [moreView, setMoreView] = useState<"root" | "move" | "labels">("root");
  const labelsQ = useQuery({ queryKey: ["labels"], queryFn: () => backend.labels() });
  const { t } = useTranslation();
  const thread = useQuery({ queryKey: ["thread", emailId], queryFn: () => service.getThread(emailId!), enabled: !!emailId });

  const msgs = thread.data ?? [];
  // Por defeito, só a mais recente está aberta. Recalcula quando muda de conversa.
  useEffect(() => {
    if (msgs.length) setExpanded(new Set([msgs[msgs.length - 1].id]));
  }, [emailId, msgs.length]);
  // Trocar de e-mail volta a esconder o resumo (é sob demanda por e-mail).
  useEffect(() => { setWantSummary(false); }, [emailId]);
  // Atalho "E" arquiva a conversa aberta (estilo Gmail). Ignora quando se está a escrever num campo.
  useEffect(() => {
    if (!emailId || !onArchive) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== "e" && e.key !== "E") return;
      if (e.metaKey || e.ctrlKey || e.altKey) return;
      const el = e.target as HTMLElement | null;
      if (el && (el.tagName === "INPUT" || el.tagName === "TEXTAREA" || el.isContentEditable)) return;
      e.preventDefault();
      onArchive();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [emailId, onArchive]);
  const toggle = (id: string) => setExpanded((s) => { const n = new Set(s); n.has(id) ? n.delete(id) : n.add(id); return n; });

  // Ações agem na mensagem mais recente da conversa (como o Gmail).
  const m = msgs[msgs.length - 1];
  // Texto legível do corpo (para IA, citação de resposta e reencaminhamento). Num e-mail só-HTML o
  // textBody aponta para a parte HTML — aí extraímos o texto das tags, senão a IA veria HTML cru.
  const bodyPart = m?.textBody?.[0];
  const rawBody = bodyPart ? m.bodyValues[bodyPart.partId]?.value ?? "" : (m?.preview ?? "");
  const body = bodyPart?.type === "text/html" ? htmlToText(rawBody) : rawBody;

  // Config: o resumo por IA pode ser desligado nas Definições (settings.email.aiSummary). Nunca
  // corre em SPAM — não se gasta IA nem se envia conteúdo suspeito para o modelo.
  const settings = useQuery({ queryKey: ["settings"], queryFn: () => settingsApi.get(), retry: false });
  const aiSummaryOn = settings.data?.email?.aiSummary !== false && !isSpam; // default ligado

  // Resumo por IA (best-effort). Conteúdo é imutável por id → cache em localStorage.
  const summary = useQuery({
    queryKey: ["summary", m?.id],
    queryFn: async () => {
      const key = `mailin.sum.${m!.id}`;
      const hit = localStorage.getItem(key);
      if (hit) return { summary: hit };
      const r = await backend.summarize(m!.subject, body);
      try { localStorage.setItem(key, r.summary); } catch { /* quota: ignora */ }
      return r;
    },
    enabled: !!m && aiSummaryOn && wantSummary,
    staleTime: Infinity,
    gcTime: Infinity,
    retry: false,
  });

  if (!emailId) return <div className="empty">{t("webmail.reader.none")}</div>;
  if (thread.isLoading || !m) return <div className="loading">…</div>;

  const name = displayName(m.from);
  const addr = m.from?.[0]?.email ?? "";
  const myEmail = me.toLowerCase();

  const quoted = () => `\n\n${t("webmail.reader.quoteHeader", { who: name, date: fullDateTime(m.receivedAt, locale) })}\n` +
    body.split("\n").map((l) => "> " + l).join("\n");
  // Preserva o HTML original na resposta/encaminhamento (antes citava só texto). O corpo original vai
  // dentro de <blockquote>; o Compose sanitiza tudo. Sem parte HTML → undefined (cai no texto acima).
  const escHtml = (s: string) => s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
  const origHtml = () => { const h = m.htmlBody?.[0]; return h?.type === "text/html" ? (m.bodyValues[h.partId]?.value ?? "") : ""; };
  const wrapQuote = (headerHtml: string) => {
    const oh = origHtml(); if (!oh) return undefined;
    return `<br><br><div>${headerHtml}</div><blockquote style="margin:0 0 0 .8ex;border-left:2px solid #ccc;padding-left:1ex">${oh}</blockquote>`;
  };
  const quotedHtml = () => wrapQuote(escHtml(t("webmail.reader.quoteHeader", { who: name, date: fullDateTime(m.receivedAt, locale) })));
  const fwdHtml = () => wrapQuote([
    t("webmail.reader.fwdHeader"), `${t("webmail.reader.from")}: ${fmtAddrs(m.from)}`,
    `${t("webmail.reader.to")}: ${fmtAddrs(m.to)}`, `${t("webmail.reader.date")}: ${fullDateTime(m.receivedAt, locale)}`,
    `${t("webmail.reader.subject")}: ${m.subject}`,
  ].map(escHtml).join("<br>"));

  const replyTarget = () => (m.replyTo?.[0]?.email ?? addr);
  const others = (list?: EmailAddress[] | null) => (list ?? []).map((a) => a.email).filter((e) => {
    const l = e.toLowerCase(); return l !== myEmail && l !== replyTarget().toLowerCase();
  });
  const doReply = (all: boolean) => onReply({
    to: replyTarget(),
    cc: all ? [...others(m.to), ...others(m.cc)].join(", ") || undefined : undefined,
    subject: rePrefix("Re: ", m.subject),
    body: quoted(),
    bodyHtml: quotedHtml(),
  });
  // "Rascunhar com IA": abre o assistente já com o contexto de RESPOSTA (destinatário + "Re:"),
  // para o texto gerado virar uma resposta e não um e-mail novo.
  const draftAI = () => onDraftAI(body, { to: replyTarget(), subject: rePrefix("Re: ", m.subject) });
  const doForward = () => onReply({
    subject: rePrefix("Fwd: ", m.subject),
    body: `\n\n${t("webmail.reader.fwdHeader")}\n` +
      `${t("webmail.reader.from")}: ${fmtAddrs(m.from)}\n${t("webmail.reader.to")}: ${fmtAddrs(m.to)}\n` +
      `${t("webmail.reader.date")}: ${fullDateTime(m.receivedAt, locale)}\n${t("webmail.reader.subject")}: ${m.subject}\n\n${body}`,
    bodyHtml: fwdHtml(),
  });

  // Ações de IA por e-mail (sob demanda, NÃO automáticas): resumir, rascunhar resposta, extrair
  // tarefas. Vai como slot dentro da mensagem mais recente (entre cabeçalho e corpo). Enquanto o
  // resumo não é pedido, mostra só a barra de botões; ao pedir, expande no cartão com o resumo.
  const summaryNode = !aiSummaryOn ? undefined : (
    (wantSummary && (summary.isLoading || summary.data)) ? (
      <div className="ai-card">
        {summary.isLoading
          ? <AiThinking label={t("webmail.ai.summarizing")} />
          : <>
              <h4><IcSparkle size={14} /> {t("webmail.ai.summary")}</h4>
              <div className="txt">{summary.data!.summary}</div>
              <div className="ai-actions">
                <button className="chipbtn" onClick={draftAI}>{t("webmail.ai.draftReply")}</button>
                <button className="chipbtn" onClick={() => onExtractTasks(m.subject, body)}>{t("webmail.ai.extractTasks")}</button>
              </div>
            </>}
      </div>
    ) : (
      <div className="ai-bar">
        {/* A IA fica num único ponto de entrada — Resumir. Rascunhar (== Responder com IA) vive no
            menu do botão Responder; Extrair tarefas vive no ⋯. Sem duplicação. */}
        <button className="chipbtn" onClick={() => setWantSummary(true)}><IcSparkle size={15} /> {t("webmail.ai.summarize")}</button>
      </div>
    )
  );

  return (
    <div className="rin">
      <div className="ract">
        {/* Responder — botão dividido: clicar responde; o ▾ abre a todos / encaminhar / IA. */}
        <div className="split movewrap">
          <button className="split-main" onClick={() => doReply(false)}><IcReply size={15} /> {t("webmail.reader.reply")}</button>
          <button className={"split-caret" + (replyOpen ? " on" : "")} aria-label={t("webmail.reader.forward")} onClick={() => setReplyOpen((v) => !v)}><IcChevron size={14} className="down" /></button>
          {replyOpen && (
            <div className="move-menu" onMouseLeave={() => setReplyOpen(false)}>
              {hasOthers(m, myEmail) && <button onClick={() => { setReplyOpen(false); doReply(true); }}><IcReplyAll size={15} /> {t("webmail.reader.replyAll")}</button>}
              <button onClick={() => { setReplyOpen(false); doForward(); }}><IcForward size={15} /> {t("webmail.reader.forward")}</button>
              <button onClick={() => { setReplyOpen(false); draftAI(); }}><IcSparkle size={15} /> {t("webmail.ai.replyAI")}</button>
            </div>
          )}
        </div>

        {/* Triagem — 3 ícones (o mais usado): arquivar, adiar, apagar. */}
        {onArchive && <button className="iconbtn" title={t("webmail.archive")} onClick={onArchive}><IcArchive size={17} /></button>}
        {onSnooze && (
          <div className="movewrap">
            <button className={"iconbtn" + (snoozeOpen ? " on" : "")} title={t("webmail.snooze")} onClick={() => setSnoozeOpen((v) => !v)}><IcCalendar size={17} /></button>
            {snoozeOpen && (
              <div className="move-menu" onMouseLeave={() => setSnoozeOpen(false)}>
                {snoozeOptions().map((o) => (
                  <button key={o.key} onClick={() => { setSnoozeOpen(false); onSnooze(o.when); }}>
                    <span>{t("webmail.snoozeOpts." + o.key)}</span>
                    <span className="snz-when">{o.when.toLocaleString(locale, { weekday: "short", hour: "2-digit", minute: "2-digit" })}</span>
                  </button>
                ))}
              </div>
            )}
          </div>
        )}
        <button className="iconbtn danger" title={t("webmail.delete")} onClick={onTrash}><IcTrash size={17} /></button>

        <div className="grow" />

        {/* ⋯ Mais — tudo o resto, com sub-menus (mover, marcadores) à maneira do iOS. */}
        <div className="movewrap">
          <button className={"iconbtn more-btn" + (moreOpen ? " on" : "")} title={t("webmail.more")} aria-label={t("webmail.more")}
            onClick={() => { setMoreOpen((v) => !v); setMoreView("root"); }}>⋯</button>
          {moreOpen && (
            <div className="move-menu more-menu" onMouseLeave={() => { setMoreOpen(false); setMoreView("root"); }}>
              {moreView === "root" && <>
                <button onClick={() => { setMoreOpen(false); onExtractTasks(m.subject, body); }}><IcColumns size={15} /> {t("webmail.ai.extractTasks")}</button>
                <button onClick={() => { setMoreOpen(false); onSchedule(m.subject); }}><IcCalendar size={15} /> {t("webmail.ai.schedule")}</button>
                <div className="more-sep" />
                <button onClick={() => { setMoreOpen(false); onMarkUnread(); }}><IcMail size={15} /> {t("webmail.markUnread")}</button>
                {spamAction && <button onClick={() => { setMoreOpen(false); spamAction.run(); }}><IcShield size={15} /> {spamAction.label}</button>}
                <button onClick={() => setMoreView("move")}><IcFolder size={15} /> {t("webmail.moveTo")}<span className="more-chev">›</span></button>
                <button onClick={() => setMoreView("labels")}><TagIcon size={15} /> {t("webmail.labels.label")}<span className="more-chev">›</span></button>
              </>}
              {moreView === "move" && <>
                <button className="more-back" onClick={() => setMoreView("root")}><IcChevron size={14} className="left" /> {t("webmail.moveTo")}</button>
                {moveTargets.map((f) => (
                  <button key={f.id} onClick={() => { setMoreOpen(false); onMove(f.id); }}>{f.label}</button>
                ))}
              </>}
              {moreView === "labels" && <>
                <button className="more-back" onClick={() => setMoreView("root")}><IcChevron size={14} className="left" /> {t("webmail.labels.label")}</button>
                {(labelsQ.data ?? []).length === 0 && <div className="more-empty">{t("webmail.labels.empty")}</div>}
                {(labelsQ.data ?? []).map((l) => {
                  const on = !!m.keywords?.[labelKeyword(l.id)];
                  return (
                    <button key={l.id} onClick={async () => { await service.setLabel(m.id, labelKeyword(l.id), !on); void thread.refetch(); }}>
                      <span className="mlab-dot" style={{ background: l.color }} /> {l.name}
                      {on && <span className="lbl-check"><IcCheck size={14} /></span>}
                    </button>
                  );
                })}
              </>}
            </div>
          )}
        </div>
      </div>

      <h1 className="rsub">{m.subject || t("webmail.noSubject")}
        {msgs.length > 1 && <span className="thread-count">{msgs.length}</span>}
        <LabelChips keywords={m.keywords} />
      </h1>

      {isSpam && (
        // Banner de spam à moda do Gmail: explica por que está no Spam e deixa marcar "não é spam".
        // O conteúdo abaixo tem links e imagens bloqueados (ver MailBody) por segurança.
        <div className="spam-banner">
          <div className="spam-txt">
            <b>{t("webmail.reader.spamTitle")}</b>
            <span>{t("webmail.reader.spamWhy")}</span>
          </div>
          {spamAction && !spamAction.junk && (
            <button className="chipbtn" onClick={spamAction.run}>{spamAction.label}</button>
          )}
        </div>
      )}

      {msgs.map((msg, i) => (
        <MessageItem key={msg.id} m={msg} service={service} locale={locale} t={t} spam={!!isSpam} me={me}
          single={msgs.length === 1} open={expanded.has(msg.id)} onToggle={() => toggle(msg.id)}
          onOpenAttachment={onOpenAttachment}
          // Resumo por IA fica na mensagem mais recente, entre o cabeçalho e o corpo.
          summarySlot={i === msgs.length - 1 ? summaryNode : undefined} />
      ))}
    </div>
  );
}

// Uma mensagem da conversa. Recolhida mostra só remetente + trecho + hora; aberta mostra o cabeçalho
// Captura automática: se o remetente ainda não está na agenda, oferece adicioná-lo com um clique.
// Some quando já é contato (ou é o próprio utilizador). Compara e-mail sem caixa (case-insensitive).
function AddSenderButton({ name, email }: { name: string; email: string }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const contacts = useQuery({ queryKey: ["contacts"], queryFn: () => contactsApi.list(), retry: false });
  const me = useQuery({ queryKey: ["me"], queryFn: () => backend.me(), retry: false });
  const add = useMutation({
    mutationFn: () => contactsApi.create({
      kind: "person", name: name || email, email, phone: "", company: "", domain: "", notes: "",
      tags: [], group_ids: [], title: "", mobile: "", is_whatsapp: false, allow_phone: false,
      allow_email: false, company_id: null, extra_emails: [], extra_phones: [],
    }),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ["contacts"] }); toast(t("webmail.reader.contactAdded")); },
    onError: (e) => toast((e as Error).message),
  });
  const norm = email.trim().toLowerCase();
  if (!norm || add.isSuccess) return null;
  if ((me.data?.email ?? "").toLowerCase() === norm) return null; // não me adiciono a mim mesmo
  const exists = (contacts.data ?? []).some((c) => (c.email ?? "").toLowerCase() === norm);
  if (exists) return null;
  return (
    <button className="ib" title={t("webmail.reader.addContact")} disabled={add.isPending}
      onClick={(e) => { e.stopPropagation(); add.mutate(); }}><IcPlus size={15} /></button>
  );
}

// completo (com detalhes e imprimir) e o corpo. Numa conversa de uma só mensagem, sem cromo de colapso.
function MessageItem({ m, service, locale, t, single, spam, me, open, onToggle, onOpenAttachment, summarySlot }: {
  m: EmailFull; service: MailService; locale: string; t: TFunction; me: string;
  single: boolean; spam: boolean; open: boolean; onToggle: () => void; onOpenAttachment: (a: EmailAttachment) => void;
  summarySlot?: ReactNode;
}) {
  const [details, setDetails] = useState(false);
  const name = displayName(m.from);
  const addr = m.from?.[0]?.email ?? "";
  const textPart = m.textBody?.[0]?.partId;
  const body = textPart ? m.bodyValues[textPart]?.value ?? "" : m.preview;

  const printEmail = () => {
    const rows: [string, string][] = [
      [t("webmail.reader.from"), fmtAddrs(m.from)],
      [t("webmail.reader.to"), fmtAddrs(m.to)],
      ...(m.cc?.length ? [[t("webmail.reader.cc"), fmtAddrs(m.cc)] as [string, string]] : []),
      [t("webmail.reader.date"), fullDateTime(m.receivedAt, locale)],
    ];
    const head = rows.map(([k, v]) => `<tr><td class="k">${esc(k)}</td><td>${esc(v)}</td></tr>`).join("");
    const w = window.open("", "_blank", "width=800,height=900");
    if (!w) { toast(t("webmail.reader.printBlocked")); return; }
    w.document.write(`<!doctype html><meta charset="utf-8"><title>${esc(m.subject)}</title>` +
      `<style>body{font:14px -apple-system,system-ui,sans-serif;color:#111;padding:32px;max-width:720px}` +
      `h1{font-size:20px}table{margin:12px 0 20px;font-size:13px;color:#444}td.k{padding-right:14px;color:#888;white-space:nowrap;vertical-align:top}` +
      `pre{white-space:pre-wrap;word-wrap:break-word;font:inherit}</style>` +
      `<h1>${esc(m.subject || "(sem assunto)")}</h1><table>${head}</table><pre>${esc(body)}</pre>`);
    w.document.close();
    w.focus();
    setTimeout(() => w.print(), 250);
  };

  // Linha recolhida: um resumo clicável. Não aparece numa conversa de mensagem única.
  if (!open && !single) {
    return (
      <button className="emsg-collapsed" onClick={onToggle}>
        <Avatar name={name} email={addr} size={26} className="sm" />
        <span className="w">{name}</span>
        <span className="snip">{m.preview}</span>
        <span className="tm">{shortTime(m.receivedAt, locale)}</span>
      </button>
    );
  }

  return (
    <div className={"emsg" + (single ? " single" : "")}>
      <div className="rfrom" onClick={single ? undefined : onToggle} style={single ? undefined : { cursor: "pointer" }}>
        <Avatar name={name} email={addr} size={38} />
        <div className="rfrom-id">
          <div className="n">{name} <span className="a">&lt;{addr}&gt;</span></div>
          <button className="rdetails-toggle" onClick={(e) => { e.stopPropagation(); setDetails((v) => !v); }}>
            {recipientSummary(m, t, me)}
            <IcChevron size={12} className={details ? "up" : ""} />
          </button>
        </div>
        <div className="rfrom-r">
          <AddSenderButton name={name} email={addr} />
          <button className="ib" title={t("webmail.reader.print")} onClick={(e) => { e.stopPropagation(); printEmail(); }}><IcPrinter size={15} /></button>
          <span className="tm" title={fullDateTime(m.receivedAt, locale)}>
            {shortTime(m.receivedAt, locale)} <i>({relativeTime(m.receivedAt, locale)})</i>
          </span>
        </div>
      </div>

      {details && <DetailsPanel m={m} locale={locale} />}

      {summarySlot}

      <MailBody m={m} service={service} textBody={body} spam={spam} />

      {m.attachments?.length > 0 && (
        <div className="att-grid">
          {m.attachments.map((a, i) => (
            <button key={i} className="att" onClick={() => onOpenAttachment(a)}>
              <div className="ic">{(a.type.split("/")[1] ?? "bin").slice(0, 4).toUpperCase()}</div>
              <div><div className="nm">{a.name ?? t("webmail.reader.attachment")}</div><div className="z">{Math.max(1, Math.round(a.size / 1024))} KB</div></div>
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

// Presets de snooze estilo Gmail: mais tarde (+3h), esta noite (18h), amanhã (8h), próxima semana
// (segunda 8h). Calculados a partir de agora — sempre no futuro.
function snoozeOptions(): { key: string; when: Date }[] {
  const now = new Date();
  const at = (d: Date, h: number) => { const x = new Date(d); x.setHours(h, 0, 0, 0); return x; };
  const later = new Date(now.getTime() + 3 * 3600_000);
  let evening = at(now, 18);
  if (evening <= now) evening = at(new Date(now.getTime() + 86400_000), 18);
  const tomorrow = at(new Date(now.getTime() + 86400_000), 8);
  const nextMon = at(now, 8);
  do { nextMon.setDate(nextMon.getDate() + 1); } while (nextMon.getDay() !== 1); // próxima segunda
  return [
    { key: "later", when: later },
    { key: "evening", when: evening },
    { key: "tomorrow", when: tomorrow },
    { key: "nextWeek", when: nextMon },
  ];
}

const esc = (s: string) => s.replace(/[&<>"]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c]!));

// Extrai o texto legível de um corpo HTML (para a IA e citações de resposta não verem tags cruas).
function htmlToText(html: string): string {
  const doc = new DOMParser().parseFromString(html, "text/html");
  return (doc.body.textContent ?? "").replace(/\n{3,}/g, "\n\n").trim();
}

function fmtAddrs(list?: EmailAddress[] | null): string {
  if (!list?.length) return "—";
  return list.map((a) => (a.name ? `${a.name} <${a.email}>` : a.email)).join(", ");
}

// "para mim", "para Ana", "para dener@x.com e +2" — o resumo curto ao lado do remetente, à Gmail.
// Antes assumia sempre "para mim"; em Enviados (destinatário ≠ eu) isso estava errado. Agora usa o
// destinatário real e só diz "mim" quando o endereço é o do próprio utilizador.
function recipientSummary(m: EmailFull, t: TFunction, me: string): string {
  const all = [...(m.to ?? []), ...(m.cc ?? [])];
  if (!all.length) return t("webmail.reader.toNobody");
  const meL = me.toLowerCase();
  const label = (a: EmailAddress) => (a.email.toLowerCase() === meL ? t("webmail.reader.meWord") : a.name || a.email);
  if (all.length === 1) return t("webmail.reader.toOne", { who: label(all[0]) });
  return t("webmail.reader.toMany", { who: label(all[0]), n: all.length - 1 });
}

// Painel de detalhes: os cabeçalhos que o Gmail esconde atrás da setinha. Só mostra o que existe.
function DetailsPanel({ m, locale }: { m: EmailFull; locale: string }) {
  const { t } = useTranslation();
  const rows: [string, string][] = [
    [t("webmail.reader.from"), fmtAddrs(m.from)],
    ...(m.replyTo?.length && m.replyTo[0]?.email !== m.from?.[0]?.email
      ? [[t("webmail.reader.replyTo"), fmtAddrs(m.replyTo)] as [string, string]] : []),
    [t("webmail.reader.to"), fmtAddrs(m.to)],
    ...(m.cc?.length ? [[t("webmail.reader.cc"), fmtAddrs(m.cc)] as [string, string]] : []),
    [t("webmail.reader.date"), fullDateTime(m.receivedAt, locale)],
    ...(m.sentAt && m.sentAt !== m.receivedAt
      ? [[t("webmail.reader.sentAt"), fullDateTime(m.sentAt, locale)] as [string, string]] : []),
    [t("webmail.reader.subject"), m.subject || "—"],
  ];
  return (
    <div className="rdetails">
      <table><tbody>
        {rows.map(([k, v]) => <tr key={k}><td className="k">{k}</td><td>{v}</td></tr>)}
      </tbody></table>
    </div>
  );
}

// "Re: " / "Fwd: " sem empilhar prefixos: "Re: Re: x" é ruído.
function rePrefix(prefix: string, subject: string): string {
  const s = subject ?? "";
  return s.toLowerCase().startsWith(prefix.toLowerCase()) ? s : prefix + s;
}

// Há mais alguém além de mim e do remetente? Decide se o botão "responder a todos" faz sentido.
function hasOthers(m: EmailFull, myEmail: string): boolean {
  const from = m.from?.[0]?.email.toLowerCase();
  return [...(m.to ?? []), ...(m.cc ?? [])].some((a) => {
    const l = a.email.toLowerCase(); return l !== myEmail.toLowerCase() && l !== from;
  });
}
