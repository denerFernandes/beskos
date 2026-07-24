// Regras de e-mail = script Sieve no Stalwart, gerido pela sessão do PRÓPRIO utilizador (JMAP).
//
// Por que aqui e não no backend Go: o Sieve de uma conta só se escreve com a credencial dessa
// conta, e o backend nunca a tem (§D7: não guardamos senha; o webmail usa Basic auth em memória).
// É o mesmo caminho do resto do webmail — JMAP direto.
//
// As regras correm NA ENTREGA, no servidor: valem com o browser fechado, no telemóvel, em tudo.
import type { JmapClient } from "./client";

export const SIEVE_CAP = "urn:ietf:params:jmap:sieve";
export const SCRIPT_NAME = "mailin-regras";

export interface SieveScript { id: string; name: string; isActive: boolean; blobId?: string }

export class SieveApi {
  constructor(private c: JmapClient, private accountId: string) {}

  // list devolve os scripts da conta. Só mexemos no nosso (SCRIPT_NAME): se alguém tiver criado
  // outro por fora, não é da nossa conta mexer.
  async list(): Promise<SieveScript[]> {
    const r = await this.c.request([SIEVE_CAP], [["SieveScript/get", { accountId: this.accountId }, "g"]]);
    return ((r[0] as { list?: SieveScript[] }).list ?? []);
  }

  // download traz o conteúdo do script pelo blobId (é onde vive o JSON das regras, no comentário).
  async download(blobId: string): Promise<string> {
    const blob = await this.c.downloadBlob(this.accountId, blobId, "regras.sieve", "application/sieve");
    return blob.text();
  }

  // save sobe o script e ativa-o numa só request: se a ativação falhasse depois de um upload
  // separado, ficaria um script guardado e inerte — pior que falhar.
  async save(content: string): Promise<void> {
    const existing = (await this.list()).find((s) => s.name === SCRIPT_NAME);
    const b64 = b64encode(content);
    const create: Record<string, unknown> = {};
    const update: Record<string, unknown> = {};
    if (existing) update[existing.id] = { blobId: "#s1" };
    else create["r1"] = { name: SCRIPT_NAME, blobId: "#s1" };

    const r = await this.c.request([SIEVE_CAP, "urn:ietf:params:jmap:blob"], [
      ["Blob/upload", {
        accountId: this.accountId,
        create: { s1: { data: [{ "data:asBase64": b64 }], type: "application/sieve" } },
      }, "u"],
      ["SieveScript/set", {
        accountId: this.accountId,
        ...(existing ? { update } : { create }),
        onSuccessActivateScript: existing ? existing.id : "#r1",
      }, "s"],
    ]);

    const res = r[1] as { notCreated?: Record<string, { description?: string }>; notUpdated?: Record<string, { description?: string }> };
    const err = Object.values(res.notCreated ?? {})[0] ?? Object.values(res.notUpdated ?? {})[0];
    if (err) throw new Error(err.description ?? "sieve");
  }

  // deactivate desliga sem apagar: quem desliga as regras costuma querer voltar a ligá-las.
  async deactivate(): Promise<void> {
    await this.c.request([SIEVE_CAP],
      [["SieveScript/set", { accountId: this.accountId, onSuccessActivateScript: null }, "s"]]);
  }
}

function b64encode(s: string): string {
  // btoa não aceita acentos; TextEncoder resolve ("férias" ia rebentar aqui).
  const bytes = new TextEncoder().encode(s);
  let bin = "";
  bytes.forEach((b) => { bin += String.fromCharCode(b); });
  return btoa(bin);
}
