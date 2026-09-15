import { describe, it, expect, vi, afterEach, beforeEach } from "vitest";
import { assistantApi, type ChatEvent } from "./api";
import i18n from "../../i18n";

// Devolve uma Response falsa cujo corpo entrega os pedaços tal como foram dados — incluindo
// pedaços que cortam um evento a meio, que é o que a rede faz de verdade.
function fakeStream(chunks: string[]): Response {
  const enc = new TextEncoder();
  let i = 0;
  const body = new ReadableStream<Uint8Array>({
    pull(ctrl) {
      if (i >= chunks.length) { ctrl.close(); return; }
      ctrl.enqueue(enc.encode(chunks[i++]));
    },
  });
  return new Response(body, { status: 200, headers: { "Content-Type": "text/event-stream" } });
}

// LanguageDetector picks up whatever locale the machine running the tests reports (this CI
// runner resolves to "en", a contributor's laptop might resolve to "pt-BR") -- a test asserting
// translated TEXT has to pin the language itself, or it passes and fails depending on who runs
// it (law 4.6: the frontend translates the error_code; which language it lands in is not this
// test's business).
beforeEach(async () => { await i18n.changeLanguage("pt-BR"); });
afterEach(() => { vi.unstubAllGlobals(); });

describe("assistantApi.chat", () => {
  // Um evento partido entre dois pedaços TEM de esperar pelo resto. Processar meio evento daria um
  // JSON truncado a cada leitura e a resposta apareceria com buracos.
  it("junta eventos partidos entre pedaços da rede", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => fakeStream([
      'data: {"kind":"conversa',
      'tion","conversation_id":"c1","title":"olá","agent":"assistant"}\n\n',
      'data: {"kind":"delta","text":"Oi"}\n\ndata: {"kind":"delta","text":" tudo bem"}\n\n',
    ])));

    const got: ChatEvent[] = [];
    await assistantApi.chat({ message: "olá" }, (ev) => got.push(ev));

    expect(got.map((e) => e.kind)).toEqual(["conversation", "delta", "delta"]);
    expect(got[0]).toMatchObject({ conversation_id: "c1" });
    expect(got.filter((e) => e.kind === "delta").map((e) => (e as { text: string }).text).join(""))
      .toBe("Oi tudo bem");
  });

  // Um evento estragado não pode deitar fora o fio: perder tudo por causa de uma linha seria pior.
  it("ignora um evento ilegível e continua", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => fakeStream([
      "data: {isto não é json}\n\n",
      'data: {"kind":"delta","text":"ok"}\n\n',
    ])));

    const got: ChatEvent[] = [];
    await assistantApi.chat({ message: "olá" }, (ev) => got.push(ev));
    expect(got).toEqual([{ kind: "delta", text: "ok" }]);
  });

  // Uma ação que espera aprovação chega como evento próprio, com os argumentos EXATOS que vão
  // correr — é o que o painel mostra a quem decide (§6.3).
  it("entrega o pedido de confirmação com os argumentos exatos", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => fakeStream([
      'data: {"kind":"confirm","confirm":{"id":"a1","tool":"mail.send","args":{"to":["ana@exemplo.pt"],"text":"Segue a fatura."}}}\n\n',
    ])));

    const got: ChatEvent[] = [];
    await assistantApi.chat({ message: "envia" }, (ev) => got.push(ev));

    expect(got).toHaveLength(1);
    const ev = got[0] as { kind: string; confirm: { id: string; args: Record<string, unknown> } };
    expect(ev.kind).toBe("confirm");
    expect(ev.confirm.id).toBe("a1");
    expect(ev.confirm.args).toEqual({ to: ["ana@exemplo.pt"], text: "Segue a fatura." });
  });

  // Antes de o fio abrir os erros são HTTP normais, e o error_code é o que o frontend traduz
  // (lei 4.4/4.6) — nunca se mostra texto cru do servidor.
  it("erro antes do fio abrir rejeita com a mensagem traduzida", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => new Response(
      JSON.stringify({ error_code: "assistant.rate_limited" }),
      { status: 429, headers: { "Content-Type": "application/json" } },
    )));

    await expect(assistantApi.chat({ message: "olá" }, () => {})).rejects.toThrow(/assistente/i);
  });
});

describe("escolha de agente", () => {
  // O agente fixado pelo utilizador viaja no pedido: a escolha manual manda mais do que o
  // encaminhamento automático (§3.2). Sem agente no corpo, o servidor decide.
  it("envia o agente fixado, e omite-o quando é automático", async () => {
    const bodies: unknown[] = [];
    vi.stubGlobal("fetch", vi.fn(async (_url: string, init: RequestInit) => {
      bodies.push(JSON.parse(String(init.body)));
      return fakeStream(['data: {"kind":"delta","text":"ok"}\n\n']);
    }));

    await assistantApi.chat({ message: "olá", agent: "inbox" }, () => {});
    await assistantApi.chat({ message: "olá" }, () => {});

    expect(bodies[0]).toMatchObject({ agent: "inbox" });
    expect(bodies[1]).not.toHaveProperty("agent");
  });

  // Quem respondeu chega no primeiro evento, antes de qualquer ferramenta correr — a escolha é
  // sempre visível, nunca silenciosa.
  it("anuncia o agente escolhido antes de tudo o resto", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => fakeStream([
      'data: {"kind":"conversation","conversation_id":"c1","title":"t","agent":"inbox","pinned_agent":""}\n\n',
      'data: {"kind":"step","step":{"tool":"mail.search","args":{},"summary":"3 messages"}}\n\n',
    ])));

    const got: ChatEvent[] = [];
    await assistantApi.chat({ message: "resume os e-mails" }, (ev) => got.push(ev));

    expect(got[0].kind).toBe("conversation");
    expect((got[0] as { agent: string }).agent).toBe("inbox");
  });
});

describe("assistantApi.confirm", () => {
  // Uma decisão por pedido, com o id da ação no caminho: não existe endpoint que aprove um lote
  // (§6.3). O corpo leva a decisão explícita — nunca se assume aprovação.
  it("envia uma decisão de cada vez, com a decisão explícita", async () => {
    const calls: Array<[string, RequestInit]> = [];
    vi.stubGlobal("fetch", vi.fn(async (url: string, init: RequestInit) => {
      calls.push([url, init]);
      return new Response(JSON.stringify({ status: "ok", approved: true }), {
        status: 200, headers: { "Content-Type": "application/json" },
      });
    }));

    await assistantApi.confirm("a1", true);
    await assistantApi.confirm("a2", false);

    expect(calls).toHaveLength(2);
    expect(calls[0][0]).toContain("/api/assistant/confirm/a1");
    expect(JSON.parse(String(calls[0][1].body))).toEqual({ approve: true });
    expect(JSON.parse(String(calls[1][1].body))).toEqual({ approve: false });
  });
});
