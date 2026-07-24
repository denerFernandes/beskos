// Tipos JMAP usados pelo webmail (subconjunto de RFC 8620/8621 + o que o Stalwart 0.16 devolve).
// Ver deploy/DIVERGENCES.md §D7 para os fluxos testados.

export interface JmapSession {
  accountId: string; // primaryAccounts["urn:ietf:params:jmap:mail"]
  identityId?: string;
  capabilities: Record<string, unknown>;
  // Paths (não hosts — os hosts vêm com o serverHostname configurado, DIVERGENCES §D7).
  downloadUrlTemplate: string;
}

export interface Mailbox {
  id: string;
  name: string;
  role: string | null; // inbox | drafts | sent | trash | junk | archive | null
  parentId?: string | null; // pasta-pai (aninhamento); null = raiz
  totalEmails: number;
  unreadEmails: number;
  sortOrder?: number;
}

export interface EmailAddress {
  name?: string | null;
  email: string;
}

export interface EmailSummary {
  id: string;
  from: EmailAddress[] | null;
  to: EmailAddress[] | null;
  subject: string;
  receivedAt: string;
  preview: string;
  hasAttachment: boolean;
  keywords?: Record<string, boolean>;
  mailboxIds?: Record<string, boolean>; // pastas onde a mensagem está — p/ o chip de pasta (Importantes/busca)
  threadId?: string;
  threadCount?: number; // nº de mensagens na conversa (só quando a lista está colapsada e >1)
}

export interface EmailAttachment {
  partId?: string;
  blobId: string;
  type: string;
  name?: string | null;
  size: number;
  cid?: string | null;   // Content-ID: imagem embutida no HTML (src="cid:...")
}

// Uma página de resultados: as mensagens + o total, para "1–50 de 35.547" e as setas.
export interface EmailPage { items: EmailSummary[]; total: number; position: number }

export interface EmailFull extends EmailSummary {
  threadId?: string;            // id da conversa (mensagens relacionadas partilham-no)
  messageId?: string[];         // Message-ID RFC822; a MESMA mensagem em pastas diferentes partilha-o
  sentAt?: string;              // data que o remetente carimbou (pode diferir da entrega)
  cc?: EmailAddress[];
  replyTo?: EmailAddress[];
  textBody: { partId: string; type?: string }[];
  htmlBody: { partId: string; type?: string }[];
  bodyValues: Record<string, { value: string; isTruncated?: boolean }>;
  attachments: EmailAttachment[];
}

export interface UploadedBlob {
  blobId: string;
  type: string;
  name: string;
  size: number;
}

export interface SendDraft {
  from: EmailAddress;
  to: EmailAddress[];
  cc?: EmailAddress[];
  bcc?: EmailAddress[];
  subject: string;
  textBody: string;
  htmlBody?: string;          // quando presente, envia-se text/html (com text/plain alternativo)
  attachments?: UploadedBlob[];
}
