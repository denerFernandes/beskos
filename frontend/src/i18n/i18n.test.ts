import { describe, it, expect } from "vitest";
import ptBR from "./locales/pt-BR/common.json";
import en from "./locales/en/common.json";

// Guarda de i18n (lei §4.2): toda chave existe em TODOS os idiomas, sem sobra nem falta.
function flatten(obj: Record<string, unknown>, prefix = ""): string[] {
  return Object.entries(obj).flatMap(([k, v]) => {
    const key = prefix ? `${prefix}.${k}` : k;
    return v && typeof v === "object" ? flatten(v as Record<string, unknown>, key) : [key];
  });
}

describe("i18n locales", () => {
  const ptKeys = flatten(ptBR).sort();
  const enKeys = flatten(en).sort();

  it("pt-BR e en têm exatamente as mesmas chaves", () => {
    const missingInEn = ptKeys.filter((k) => !enKeys.includes(k));
    const missingInPt = enKeys.filter((k) => !ptKeys.includes(k));
    expect({ missingInEn, missingInPt }).toEqual({ missingInEn: [], missingInPt: [] });
  });

  it("nenhum valor de tradução está vazio", () => {
    for (const [lang, data] of [["pt-BR", ptBR], ["en", en]] as const) {
      const empties = flatten(data).filter((k) =>
        k.split(".").reduce<unknown>((o, p) => (o as Record<string, unknown>)?.[p], data) === "",
      );
      expect(empties, `${lang} tem chaves vazias`).toEqual([]);
    }
  });
});
