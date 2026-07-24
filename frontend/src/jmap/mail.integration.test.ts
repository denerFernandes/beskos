import { describe, it, expect } from "vitest";
import { JmapClient } from "./client";
import { MailService } from "./mail";

// Integração contra um Stalwart de teste. Rode com:
//   STALWART_TEST_URL=http://localhost:18091 STALWART_TEST_EMAIL=bob@wmtest.com \
//   STALWART_TEST_PWD=pw-bob-123 npm test
// Sem as envs, é pulado. (Em node não há CORS, então falamos direto com o Stalwart.)
const url = process.env.STALWART_TEST_URL;
const email = process.env.STALWART_TEST_EMAIL ?? "";
const pwd = process.env.STALWART_TEST_PWD ?? "";

describe.skipIf(!url)("MailService (integração)", () => {
  const client = new JmapClient(url, { basic: JmapClient.encodeAuth(email, pwd) });

  it("lê sessão, lista pastas, envia e recebe", async () => {
    const session = await client.session();
    expect(session.accountId).toBeTruthy();

    const svc = new MailService(client, session.accountId);
    const boxes = await svc.mailboxes();
    const inbox = boxes.find((b) => b.role === "inbox");
    expect(inbox, "deve existir uma Inbox").toBeTruthy();

    const subject = "Teste webmail " + Date.now();
    await svc.send({
      from: { email, name: "Teste" },
      to: [{ email }],
      subject,
      textBody: "Corpo do teste de webmail.\n",
    });

    // O email chega de forma assíncrona; faz polling curto no Inbox.
    let found: string | undefined;
    for (let i = 0; i < 10 && !found; i++) {
      const page = await svc.listEmails(inbox!.id, 0, 20);
      found = page.items.find((e) => e.subject === subject)?.id;
      if (!found) await new Promise((r) => setTimeout(r, 1000));
    }
    expect(found, "email enviado deve aparecer no Inbox").toBeTruthy();

    const full = await svc.getEmail(found!);
    const partId = full.textBody[0]?.partId;
    expect(full.bodyValues[partId]?.value).toContain("Corpo do teste");

    // Busca full-text encontra o email.
    const hits = await svc.search(subject);
    expect(hits.items.some((e) => e.id === found)).toBe(true);
  }, 30000);
});
