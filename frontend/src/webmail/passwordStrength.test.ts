import { describe, it, expect } from "vitest";
import { measure } from "./passwordStrength";

// O servidor (zxcvbn do Stalwart) é a autoridade; este medidor existe para avisar ANTES e é de
// propósito mais exigente. Os casos abaixo são os que apanhámos em produção ou que o utilizador
// tenta naturalmente.
describe("medidor de palavra-passe", () => {
  it("recusa o que o Stalwart recusou (repetições)", () => {
    expect(measure("abcabcabcabc").ok).toBe(false);
    expect(measure("noreply2026noreply2026").ok).toBe(false);
  });

  it("recusa curtas, comuns e sequências", () => {
    expect(measure("Ab1!xY").ok).toBe(false);            // curta
    expect(measure("password12345").ok).toBe(false);     // comum
    expect(measure("qwertyuiop123").ok).toBe(false);     // sequência do teclado
    expect(measure("abcdefghijkl").ok).toBe(false);      // sequência do alfabeto
  });

  it("recusa senhas feitas do próprio endereço", () => {
    expect(measure("dfernandes2026!", ["dfernandes@beskos.com"]).ok).toBe(false);
    expect(measure("beskos-forte-2026", ["ana@beskos.com"]).ok).toBe(false);
  });

  it("aceita frases longas e senhas variadas", () => {
    expect(measure("cavalo bateria grampo correto").ok).toBe(true);
    expect(measure("Tr0vo4da-Lilas!Verde").ok).toBe(true);
    expect(measure("xJ9#pQ2vLm74Kd", ["ana@acme.pt"]).ok).toBe(true);
  });

  it("vazia não acusa nada (ainda não escreveu)", () => {
    const s = measure("");
    expect(s.score).toBe(0);
    expect(s.hint).toBe("");
  });
});
