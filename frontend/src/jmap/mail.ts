// Operações de webmail em cima do JmapClient. Payloads conforme DIVERGENCES §D7 (testados).
import { JmapClient, USING_MAIL, USING_SEND } from "./client";
import type { Mailbox, EmailSummary, EmailFull, EmailPage, SendDraft, UploadedBlob } from "./types";
import { SieveApi } from "./sieve";

export class MailService {
  constructor(private c: JmapClient, private accountId: string) {}

  // rules devolve a API de Sieve desta conta. Fica atrás do serviço em vez de expor o client
  // cru: quem precisa de regras pede regras, não uma porta aberta para o JMAP inteiro.
  rules(): SieveApi {
    return new SieveApi(this.c, this.accountId);
  }

  // URL same-origin (via proxy) para baixar um anexo/blob. Path conforme DIVERGENCES §D7.
  attachmentUrl(blobId: string, name: string, type: string): string {
    return `/jmap/download/${encodeURIComponent(this.accountId)}/${encodeURIComponent(blobId)}/` +
      `${encodeURIComponent(name)}?accept=${encodeURIComponent(type)}`;
  }

  // Baixa um anexo autenticado e devolve um object URL (blob:) usável em <img>/<iframe>/<a>.
  // Quem chama deve revogar com URL.revokeObjectURL quando terminar.
  async attachmentObjectUrl(blobId: string, name: string, type: string): Promise<string> {
    const blob = await this.c.downloadBlob(this.accountId, blobId, name, type);
    return URL.createObjectURL(blob);
  }

  async mailboxes(): Promise<Mailbox[]> {
    const [r] = await this.c.request(USING_MAIL, [
      ["Mailbox/get", { accountId: this.accountId, ids: null }, "0"],
    ]);
    return (r.list as Mailbox[]) ?? [];
  }

  // Contagem REAL de não lidas por pasta. O `unreadEmails` do Mailbox/get é mantido pelo servidor
  // à parte das mensagens e pode ficar preso num valor antigo — apanhámos uma caixa a dizer "1 por
  // ler" com tudo lido (28/07/2026). Aqui conta-se o que existe mesmo, num só pedido com um
  // Email/query por pasta (o JMAP aceita vários métodos por chamada).
  async unreadCounts(mailboxIds: string[]): Promise<Record<string, number>> {
    if (mailboxIds.length === 0) return {};
    const calls = mailboxIds.map((id, i) => [
      "Email/query",
      {
        accountId: this.accountId,
        filter: { operator: "AND", conditions: [{ inMailbox: id }, { notKeyword: "$seen" }] },
        limit: 0, calculateTotal: true,
      },
      `u${i}`,
    ] as [string, Record<string, unknown>, string]);
    const res = await this.c.request(USING_MAIL, calls);
    const out: Record<string, number> = {};
    res.forEach((r, i) => {
      const total = (r as { total?: number }).total;
      if (typeof total === "number") out[mailboxIds[i]] = total;
    });
    return out;
  }


  // unreadThreads devolve os threads com ALGUMA mensagem por ler. Uma ida só: pede as keywords dos
  // irmãos das conversas com mais de uma mensagem (as de uma só já se sabem pela própria linha).
  private async unreadThreads(threadList: { id: string; emailIds: string[] }[], shown: string[]): Promise<Set<string>> {
    const shownSet = new Set(shown);
    const siblings: string[] = [];
    for (const th of threadList) {
      for (const id of th.emailIds) if (!shownSet.has(id)) siblings.push(id);
    }
    if (siblings.length === 0) return new Set();
    // Teto de segurança: uma página de conversas longas não pode virar um pedido gigante.
    const ids = siblings.slice(0, 300);
    const [get] = await this.c.request(USING_MAIL, [
      ["Email/get", { accountId: this.accountId, ids, properties: ["id", "threadId", "keywords"] }, "s"],
    ]);
    const out = new Set<string>();
    for (const e of (get.list as EmailSummary[]) ?? []) {
      if (!e.keywords?.$seen && e.threadId) out.add(e.threadId);
    }
    return out;
  }

  // Lista emails de uma pasta: colapsa por conversa (uma linha por thread, como o Gmail).
  async listEmails(mailboxId: string, position = 0, limit = 50): Promise<EmailPage> {
    return this.queryAndGet({ inMailbox: mailboxId }, position, limit, true);
  }

  // Busca simples. O FTS do Stalwart casa palavras INTEIRAS (com stemming), então "reser" não
  // encontra "reservas". Para dar a experiência do Gmail (parcial), tentamos o FTS primeiro (rápido,
  // toda a caixa) e, se vier vazio, fazemos um scan por SUBSTRING sobre os e-mails recentes.
  async search(text: string, position = 0, limit = 50): Promise<EmailPage> {
    const term = text.trim();
    if (!term) return { items: [], total: 0, position: 0 };
    const fts = await this.queryAndGet({ text: term }, position, limit, true);
    if (fts.total > 0) return fts;
    return this.substringSearch(term.toLowerCase(), position, limit);
  }

  // Scan por substring sobre os e-mails mais recentes de todas as pastas (casa parciais). Cobre o
  // caso comum (procurar algo recente); consultas profundas usam o FTS ou a busca avançada.
  private async substringSearch(term: string, position: number, limit: number): Promise<EmailPage> {
    const [query] = await this.c.request(USING_MAIL, [
      ["Email/query", { accountId: this.accountId, sort: [{ property: "receivedAt", isAscending: false }], limit: 400 }, "q"],
    ]);
    const ids = (query.ids as string[]) ?? [];
    if (!ids.length) return { items: [], total: 0, position };
    const [get] = await this.c.request(USING_MAIL, [
      ["Email/get", {
        accountId: this.accountId, ids,
        properties: MailService.SUMMARY_PROPS,
      }, "g"],
    ]);
    const byId = new Map((get.list as EmailSummary[] ?? []).map((e) => [e.id, e]));
    const ordered = ids.map((id) => byId.get(id)).filter(Boolean) as EmailSummary[];
    const matched = ordered.filter((e) => {
      const from = (e.from ?? []).map((a) => `${a.name ?? ""} ${a.email}`).join(" ");
      return `${e.subject} ${from} ${e.preview}`.toLowerCase().includes(term);
    });
    return { items: matched.slice(position, position + limit), total: matched.length, position };
  }

  // Busca avançada (estilo Gmail): recebe um filtro JMAP já montado (from/to/subject/text/
  // hasAttachment/after/before/minSize/maxSize/inMailbox + NOT). Colapsa por conversa.
  async searchAdvanced(filter: Record<string, unknown>, position = 0, limit = 50, collapse = true): Promise<EmailPage> {
    return this.queryAndGet(filter, position, limit, collapse);
  }

  // Importantes: uma VISTA, não uma pasta. Filtra pela marca $flagged em toda a conta — a
  // mensagem continua na pasta onde estava. NÃO colapsa: aqui cada mensagem estrelada é uma linha
  // (colapsar esconderia a estrelada se o representante da conversa fosse outra mensagem).
  async listStarred(position = 0, limit = 50): Promise<EmailPage> {
    return this.queryAndGet({ hasKeyword: "$flagged" }, position, limit, false);
  }

  // calculateTotal dá o "de 35.547"; position é o salto de página (0, 50, 100…). Uma request só:
  // a query calcula os ids desta página, o get busca-os e o Thread/get conta as mensagens de cada
  // conversa (para o "(3)" na linha), tudo encadeado por back-reference.
  // Nota: com collapseThreads o Stalwart devolve o total NÃO-colapsado — a paginação usa-o como
  // aproximação (pode sobrar uma linha na última página); é a limitação conhecida do servidor.
  private async queryAndGet(filter: Record<string, unknown>, position: number, limit: number, collapse: boolean): Promise<EmailPage> {
    const [query, get, threads] = await this.c.request(USING_MAIL, [
      ["Email/query", {
        accountId: this.accountId, filter, collapseThreads: collapse,
        sort: [{ property: "receivedAt", isAscending: false }],
        position, limit, calculateTotal: true,
      }, "q"],
      ["Email/get", {
        accountId: this.accountId,
        "#ids": { resultOf: "q", name: "Email/query", path: "/ids" },
        properties: MailService.SUMMARY_PROPS,
      }, "g"],
      ["Thread/get", {
        accountId: this.accountId,
        "#ids": { resultOf: "g", name: "Email/get", path: "/list/*/threadId" },
      }, "t"],
    ]);
    // nº de mensagens por conversa, para o badge "(3)". Só interessa quando colapsado e >1.
    const threadList = (threads.list as { id: string; emailIds: string[] }[]) ?? [];
    const sizeByThread = new Map(threadList.map((th) => [th.id, th.emailIds.length]));
    const byId = new Map((get.list as EmailSummary[] ?? []).map((e) => [e.id, e]));
    const ids = (query.ids as string[]) ?? [];
    // Por ler ao nível da CONVERSA: colapsada, a linha mostrava o estado da mensagem mais recente,
    // por isso uma resposta antiga por ler ficava invisível — a caixa dizia "1 por ler" com tudo a
    // parecer lido (28/07/2026). Aqui vemos os irmãos de cada conversa e marcamos a linha.
    const unreadThreads = collapse ? await this.unreadThreads(threadList, ids) : new Set<string>();
    const items = ids.map((id) => byId.get(id)).filter(Boolean).map((e) => {
      const n = collapse ? sizeByThread.get(e!.threadId ?? "") ?? 1 : 1;
      return {
        ...e!,
        threadCount: n > 1 ? n : undefined,
        threadUnread: unreadThreads.has(e!.threadId ?? "") || undefined,
      };
    });
    return {
      items,
      total: (query.total as number) ?? ids.length,
      position: (query.position as number) ?? position,
    };
  }

  private static readonly FULL_PROPS = ["id", "threadId", "messageId", "from", "to", "cc", "replyTo", "subject",
    "receivedAt", "sentAt", "preview", "hasAttachment", "keywords", "bodyValues", "textBody",
    "htmlBody", "attachments"];

  // Propriedades de uma LINHA da lista. Inclui mailboxIds para o chip "em que pasta está" nas vistas
  // que misturam pastas (Importantes, busca, marcador) — como no Gmail.
  private static readonly SUMMARY_PROPS = ["id", "threadId", "from", "to", "subject", "receivedAt",
    "preview", "hasAttachment", "keywords", "mailboxIds"];

  async getEmail(id: string): Promise<EmailFull> {
    const [r] = await this.c.request(USING_MAIL, [
      ["Email/get", {
        accountId: this.accountId, ids: [id],
        properties: MailService.FULL_PROPS,
        fetchTextBodyValues: true, fetchHTMLBodyValues: true,
        bodyProperties: ["partId", "blobId", "type", "name", "size", "cid"],
      }, "0"],
    ]);
    const list = (r.list as EmailFull[]) ?? [];
    if (!list.length) throw new Error("email não encontrado");
    return list[0];
  }

  // A conversa inteira (mesmo threadId), em ordem cronológica. Uma só ida ao servidor: Email/get
  // do email pedido para descobrir o thread → Thread/get pelos ids → Email/get de todos por ref.
  async getThread(id: string): Promise<EmailFull[]> {
    const resp = await this.c.request(USING_MAIL, [
      ["Email/get", { accountId: this.accountId, ids: [id], properties: ["id", "threadId"] }, "e"],
      ["Thread/get", { accountId: this.accountId, "#ids": { resultOf: "e", name: "Email/get", path: "/list/*/threadId" } }, "t"],
      ["Email/get", {
        accountId: this.accountId,
        "#ids": { resultOf: "t", name: "Thread/get", path: "/list/*/emailIds" },
        properties: MailService.FULL_PROPS,
        fetchTextBodyValues: true, fetchHTMLBodyValues: true,
        bodyProperties: ["partId", "blobId", "type", "name", "size", "cid"],
      }, "m"],
    ]);
    const list = (resp[2]?.list as EmailFull[]) ?? [];
    const sorted = list.sort((a, b) => (a.receivedAt ?? "").localeCompare(b.receivedAt ?? ""));
    // Enviar para si próprio deixa a MESMA mensagem em Enviados E na Inbox (mesmo Message-ID): são
    // dois objetos JMAP, mas uma só mensagem. No fio mostra-se uma vez. Sem Message-ID (ex.: rascunho)
    // não se deduplica — não há como afirmar que são a mesma.
    const seen = new Set<string>();
    return sorted.filter((e) => {
      const mid = (e.messageId ?? []).join(",");
      if (!mid) return true;
      if (seen.has(mid)) return false;
      seen.add(mid);
      return true;
    });
  }

  async markRead(id: string, read: boolean): Promise<void> {
    await this.c.request(USING_MAIL, [
      ["Email/set", { accountId: this.accountId, update: { [id]: { "keywords/$seen": read ? true : null } } }, "0"],
    ]);
  }

  // markThreadRead marca a CONVERSA inteira como lida. Abrir uma conversa mostra-a toda, por isso
  // deixar irmãs por ler faria a caixa continuar a contar mensagens que a pessoa já viu — era o
  // "1 por ler" que não se encontrava. Uma ida só: descobrir os ids da conversa e marcá-los.
  async markThreadRead(threadId: string): Promise<void> {
    if (!threadId) return;
    const [th] = await this.c.request(USING_MAIL, [
      ["Thread/get", { accountId: this.accountId, ids: [threadId] }, "t"],
    ]);
    const ids = ((th.list as { emailIds: string[] }[]) ?? []).flatMap((x) => x.emailIds ?? []);
    await this.markReadMany(ids, true);
  }

  // Estrela/tira a estrela. É só uma marca ($flagged, o mesmo que o IMAP e o Sieve usam), então
  // não mexe na pasta: a mensagem fica onde está e passa a aparecer também em Importantes.
  async setStarred(id: string, starred: boolean): Promise<void> {
    await this.c.request(USING_MAIL, [
      ["Email/set", { accountId: this.accountId, update: { [id]: { "keywords/$flagged": starred ? true : null } } }, "0"],
    ]);
  }

  // Marcadores (labels): aplicados como keyword JMAP (o catálogo nome/cor vive na nossa API). Como
  // estrela/spam, é só uma marca — não muda de pasta. `keyword` vem de labelKeyword(labelId).
  async setLabel(id: string, keyword: string, on: boolean): Promise<void> {
    await this.c.request(USING_MAIL, [
      ["Email/set", { accountId: this.accountId, update: { [id]: { [`keywords/${keyword}`]: on ? true : null } } }, "0"],
    ]);
  }

  // Vista por marcador: filtra a conta inteira pela keyword do label (não colapsa, como Importantes).
  async listByLabel(keyword: string, position = 0, limit = 50): Promise<EmailPage> {
    return this.queryAndGet({ hasKeyword: keyword }, position, limit, false);
  }

  // Move o email para uma pasta (substitui a membership → tira das demais). Usado por
  // "marcar como spam" / "não é spam" e arquivar. Limpa o marcador $junk conforme o destino.
  async moveTo(id: string, mailboxId: string, junk?: boolean): Promise<void> {
    const patch: Record<string, unknown> = { mailboxIds: { [mailboxId]: true } };
    if (junk !== undefined) patch["keywords/$junk"] = junk ? true : null;
    await this.c.request(USING_MAIL, [
      ["Email/set", { accountId: this.accountId, update: { [id]: patch } }, "0"],
    ]);
  }

  // --- Operações em LOTE ---
  // Um Email/set aceita muitos ids de uma vez: mover/marcar/apagar 50 mensagens é UMA request,
  // não 50. Menos ida-e-volta, e o servidor aplica tudo atomicamente.

  async moveMany(ids: string[], mailboxId: string, junk?: boolean): Promise<void> {
    if (!ids.length) return;
    const patch: Record<string, unknown> = { mailboxIds: { [mailboxId]: true } };
    if (junk !== undefined) patch["keywords/$junk"] = junk ? true : null;
    await this.c.request(USING_MAIL, [
      ["Email/set", { accountId: this.accountId, update: Object.fromEntries(ids.map((id) => [id, patch])) }, "0"],
    ]);
  }

  async markReadMany(ids: string[], read: boolean): Promise<void> {
    if (!ids.length) return;
    await this.c.request(USING_MAIL, [
      ["Email/set", { accountId: this.accountId,
        update: Object.fromEntries(ids.map((id) => [id, { "keywords/$seen": read ? true : null }])) }, "0"],
    ]);
  }

  async setStarredMany(ids: string[], starred: boolean): Promise<void> {
    if (!ids.length) return;
    await this.c.request(USING_MAIL, [
      ["Email/set", { accountId: this.accountId,
        update: Object.fromEntries(ids.map((id) => [id, { "keywords/$flagged": starred ? true : null }])) }, "0"],
    ]);
  }

  // deleteMany apaga DE VEZ (destroy). Usado só quando já se está no Lixo/Spam — no resto, "apagar"
  // é mover para o Lixo, que dá para desfazer.
  async deleteMany(ids: string[]): Promise<void> {
    if (!ids.length) return;
    await this.c.request(USING_MAIL, [
      ["Email/set", { accountId: this.accountId, destroy: ids }, "0"],
    ]);
  }

  // allIds devolve TODOS os ids da pasta — para "selecionar todas as N", não só a página visível.
  // Em blocos de 500: numa caixa enorme, uma query única traria ids a mais de uma vez.
  async allIds(mailboxId: string): Promise<string[]> {
    const out: string[] = [];
    for (;;) {
      const [q] = await this.c.request(USING_MAIL, [
        ["Email/query", { accountId: this.accountId, filter: { inMailbox: mailboxId }, position: out.length, limit: 500 }, "q"],
      ]);
      const ids = (q.ids as string[]) ?? [];
      out.push(...ids);
      if (ids.length < 500) break;
    }
    return out;
  }

  // emptyMailbox esvazia uma pasta inteira (Lixo/Spam): pergunta os ids todos e destrói-os. Um
  // Email/set só destrói ids conhecidos, então há mesmo de os listar primeiro — em blocos, para
  // não montar uma request gigante numa caixa com milhares.
  async emptyMailbox(mailboxId: string): Promise<number> {
    let removed = 0;
    for (;;) {
      const [q] = await this.c.request(USING_MAIL, [
        ["Email/query", { accountId: this.accountId, filter: { inMailbox: mailboxId }, limit: 500 }, "q"],
      ]);
      const ids = (q.ids as string[]) ?? [];
      if (!ids.length) break;
      await this.deleteMany(ids);
      removed += ids.length;
      if (ids.length < 500) break;
    }
    return removed;
  }

  // Abre o push JMAP (SSE): chama onState() sempre que a conta muda (novo e-mail, leitura…).
  // Delegado ao client (fetch streaming). Encerra quando a conexão cai ou o signal aborta.
  async streamPush(onState: () => void, signal: AbortSignal): Promise<void> {
    return this.c.streamEvents(onState, signal);
  }

  // Cria uma pasta (Mailbox) no nível raiz (ou sob parentId). Retorna o id novo.
  // Garante a pasta Arquivo (role archive): devolve a existente ou cria uma. Se o servidor recusar
  // o role no create, cria só pelo nome — o importante é ter para onde arquivar.
  async ensureArchiveMailbox(): Promise<string> {
    const boxes = await this.mailboxes();
    const found = boxes.find((b) => b.role === "archive");
    if (found) return found.id;
    const withRole = await this.c.request(USING_MAIL, [
      ["Mailbox/set", { accountId: this.accountId, create: { arch: { name: "Archive", role: "archive", parentId: null } } }, "0"],
    ]).then(([r]) => (r.created as Record<string, { id: string }> | undefined)?.["arch"]?.id).catch(() => undefined);
    if (withRole) return withRole;
    const [r2] = await this.c.request(USING_MAIL, [
      ["Mailbox/set", { accountId: this.accountId, create: { arch: { name: "Archive", parentId: null } } }, "0"],
    ]);
    const created = (r2.created as Record<string, { id: string }>) ?? {};
    if (created["arch"]) return created["arch"].id;
    throw new Error(`archive mailbox create failed: ${JSON.stringify((r2 as Record<string, unknown>).notCreated)}`);
  }

  async renameMailbox(id: string, name: string): Promise<void> {
    const [r] = await this.c.request(USING_MAIL, [
      ["Mailbox/set", { accountId: this.accountId, update: { [id]: { name } } }, "0"],
    ]);
    if ((r.notUpdated as Record<string, unknown> | undefined)?.[id]) throw new Error("rename failed");
  }

  // Apaga a pasta. SEM remover e-mails (onDestroyRemoveEmails ausente): se não estiver vazia, o
  // servidor recusa e sinalizamos — não apagamos mensagens por baixo do utilizador.
  async deleteMailbox(id: string): Promise<void> {
    const [r] = await this.c.request(USING_MAIL, [
      ["Mailbox/set", { accountId: this.accountId, destroy: [id] }, "0"],
    ]);
    const nd = (r.notDestroyed as Record<string, { type?: string }> | undefined) ?? {};
    if (nd[id]) throw new Error(nd[id].type === "mailboxHasChild" ? "hasChild" : "notEmpty");
  }

  // Pasta "Snoozed" (sem role JMAP padrão): guarda as mensagens adiadas até o worker as devolver.
  async ensureSnoozedMailbox(): Promise<string> {
    const boxes = await this.mailboxes();
    const found = boxes.find((b) => b.name.toLowerCase() === "snoozed");
    if (found) return found.id;
    return this.createMailbox("Snoozed");
  }

  async createMailbox(name: string, parentId?: string): Promise<string> {
    const [r] = await this.c.request(USING_MAIL, [
      ["Mailbox/set", {
        accountId: this.accountId,
        create: { nm: { name, parentId: parentId ?? null } },
      }, "0"],
    ]);
    const created = (r.created as Record<string, { id: string }>) ?? {};
    const box = created["nm"];
    if (!box) throw new Error(`mailbox create failed: ${JSON.stringify((r as Record<string, unknown>).notCreated)}`);
    return box.id;
  }

  // Sobe um anexo para o blobstore JMAP (same-origin via proxy). Path padrão /jmap/upload/{accountId}/.
  async uploadBlob(file: File): Promise<UploadedBlob> {
    const meta = await this.c.upload(this.accountId, file);
    return { blobId: meta.blobId, type: file.type || meta.type || "application/octet-stream", name: file.name, size: file.size };
  }

  // Monta o objeto Email a partir do rascunho. O corpo é o "content" (text/plain, ou
  // multipart/alternative text+html quando há HTML); com anexos, embrulha-se em multipart/mixed.
  private buildEmail(draft: SendDraft, draftsId: string): Record<string, unknown> {
    const atts = draft.attachments ?? [];
    const email: Record<string, unknown> = {
      mailboxIds: { [draftsId]: true },
      keywords: { $draft: true },
      from: [draft.from],
      to: draft.to,
      subject: draft.subject,
    };
    if (draft.cc?.length) email.cc = draft.cc;
    if (draft.bcc?.length) email.bcc = draft.bcc;

    const hasHtml = !!draft.htmlBody;
    const bodyValues: Record<string, { value: string }> = { p1: { value: draft.textBody } };
    if (hasHtml) bodyValues.p2 = { value: draft.htmlBody! };
    // O "content" do e-mail: só texto, ou alternativa texto+HTML (o cliente escolhe o que renderiza).
    const content = hasHtml
      ? { type: "multipart/alternative", subParts: [{ type: "text/plain", partId: "p1" }, { type: "text/html", partId: "p2" }] }
      : { type: "text/plain", partId: "p1" };

    if (!atts.length) {
      email.bodyStructure = content;
    } else {
      email.bodyStructure = {
        type: "multipart/mixed",
        subParts: [content, ...atts.map((a) => ({ blobId: a.blobId, type: a.type, name: a.name, disposition: "attachment" }))],
      };
    }
    email.bodyValues = bodyValues;
    return email;
  }

  // Salva/atualiza um rascunho na pasta Drafts. Retorna o id do rascunho.
  async saveDraft(draft: SendDraft, existingId?: string): Promise<string> {
    const draftsId = await this.draftsMailboxId();
    const email = this.buildEmail(draft, draftsId);
    if (existingId) {
      // Recria (Email é imutável em conteúdo): destrói o antigo e cria o novo, atômico no mesmo /set.
      const [r] = await this.c.request(USING_MAIL, [
        ["Email/set", { accountId: this.accountId, create: { d: email }, destroy: [existingId] }, "0"],
      ]);
      const created = (r.created as Record<string, { id: string }>) ?? {};
      if (!created["d"]) throw new Error(`draft save failed: ${JSON.stringify((r as Record<string, unknown>).notCreated)}`);
      return created["d"].id;
    }
    const [r] = await this.c.request(USING_MAIL, [
      ["Email/set", { accountId: this.accountId, create: { d: email } }, "0"],
    ]);
    const created = (r.created as Record<string, { id: string }>) ?? {};
    if (!created["d"]) throw new Error(`draft save failed: ${JSON.stringify((r as Record<string, unknown>).notCreated)}`);
    return created["d"].id;
  }

  // Identidades de envio (endereços "De:" que a conta pode usar — o principal + aliases). O servidor
  // popula a partir das moradas da conta; a UI oferece a escolha quando há mais de uma.
  async identities(): Promise<{ id: string; email: string; name: string }[]> {
    const [r] = await this.c.request(USING_SEND, [
      ["Identity/get", { accountId: this.accountId, ids: null }, "0"],
    ]);
    return ((r.list as { id: string; email?: string; name?: string }[]) ?? [])
      .map((i) => ({ id: i.id, email: i.email ?? "", name: i.name ?? "" }));
  }

  // identityId escolhe a identidade cujo e-mail casa o "De:" pedido (para o Identity e o envelope
  // baterem); sem correspondência, cai na primeira.
  private async identityId(fromEmail?: string): Promise<string> {
    const list = await this.identities();
    if (!list.length) throw new Error("nenhuma identidade de envio disponível");
    if (fromEmail) {
      const m = list.find((i) => i.email.toLowerCase() === fromEmail.toLowerCase());
      if (m) return m.id;
    }
    return list[0].id;
  }

  private async draftsMailboxId(): Promise<string> {
    const boxes = await this.mailboxes();
    const drafts = boxes.find((b) => b.role === "drafts") ?? boxes[0];
    return drafts.id;
  }

  // Id da pasta Enviados, ou null se não existir. Usado para guardar lá a cópia do e-mail enviado.
  private async sentMailboxId(): Promise<string | null> {
    const boxes = await this.mailboxes();
    return boxes.find((b) => b.role === "sent")?.id ?? null;
  }

  // prepareOutbox cria só o rascunho (Email/set) e devolve o que o servidor precisa para submeter
  // depois (outbox server-side: undo-send e agendados). O envio real é feito pelo worker via JMAP —
  // por isso NÃO submete aqui. Devolve o id do e-mail, a identidade e o envelope.
  async prepareOutbox(draft: SendDraft, draftId?: string): Promise<{ emailId: string; identityId: string; from: string; rcpts: string[] }> {
    const [identityId, draftsId] = await Promise.all([this.identityId(draft.from.email), this.draftsMailboxId()]);
    const [r] = await this.c.request(USING_SEND, [
      ["Email/set", {
        accountId: this.accountId,
        create: { draft: this.buildEmail(draft, draftsId) },
        ...(draftId ? { destroy: [draftId] } : {}),
      }, "c1"],
    ]);
    const created = (r.created as Record<string, { id: string }>) ?? {};
    if (!created["draft"]) throw new Error(`draft failed: ${JSON.stringify((r as Record<string, unknown>).notCreated)}`);
    const rcpt = [...draft.to, ...(draft.cc ?? []), ...(draft.bcc ?? [])];
    return { emailId: created["draft"].id, identityId, from: draft.from.email, rcpts: rcpt.map((t) => t.email) };
  }

  // Envia um email: cria o rascunho (Email/set) e submete (EmailSubmission/set). No sucesso, MOVE o
  // e-mail para Enviados (onSuccessUpdateEmail) em vez de o destruir — senão o enviado desaparecia,
  // pois o Stalwart não guarda cópia em Enviados sozinho. Sem pasta Enviados, cai no destruir.
  // mailboxIds usa o id REAL de Drafts; identityId é obrigatório (§D7).
  // draftId opcional: rascunho existente a destruir junto (evita duplicata em Rascunhos).
  async send(draft: SendDraft, draftId?: string, sendAt?: Date): Promise<void> {
    const [identityId, draftsId, sentId] = await Promise.all([
      this.identityId(draft.from.email),
      this.draftsMailboxId(),
      this.sentMailboxId(),
    ]);
    const rcpt = [...draft.to, ...(draft.cc ?? []), ...(draft.bcc ?? [])];
    const onSuccess = sentId
      ? { onSuccessUpdateEmail: { "#sub": { mailboxIds: { [sentId]: true }, "keywords/$draft": null, "keywords/$seen": true } } }
      : { onSuccessDestroyEmail: ["#sub"] };
    const responses = await this.c.request(USING_SEND, [
      ["Email/set", {
        accountId: this.accountId,
        create: { draft: this.buildEmail(draft, draftsId) },
        ...(draftId ? { destroy: [draftId] } : {}),
      }, "c1"],
      ["EmailSubmission/set", {
        accountId: this.accountId,
        ...onSuccess,
        create: {
          sub: {
            emailId: "#draft",
            identityId,
            envelope: {
              // HOLDUNTIL: o servidor segura a mensagem até este instante (RFC 8621 futureRelease).
              // Testado contra o Stalwart 0.16.4 — aceita mesmo sem anunciar a capacidade.
              mailFrom: { email: draft.from.email, ...(sendAt ? { parameters: { HOLDFOR: String(holdForSecs(sendAt)) } } : {}) },
              rcptTo: rcpt.map((t) => ({ email: t.email })),
            },
          },
        },
      }, "c2"],
    ]);
    const submitResp = responses[1] as { notCreated?: Record<string, unknown> };
    if (submitResp.notCreated && Object.keys(submitResp.notCreated).length) {
      throw new Error(`envio falhou: ${JSON.stringify(submitResp.notCreated)}`);
    }
  }
}

// Segundos de espera para o HOLDFOR (RFC 4865). O Stalwart 0.16.4 aceita HOLDFOR mas rejeita
// HOLDUNTIL (testado), por isso convertemos o instante-alvo para um atraso em segundos.
function holdForSecs(sendAt: Date): number {
  return Math.max(1, Math.round((sendAt.getTime() - Date.now()) / 1000));
}
