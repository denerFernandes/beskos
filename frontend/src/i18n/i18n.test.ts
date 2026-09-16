import { describe, it, expect } from "vitest";
import ptBR from "./locales/pt-BR/common.json";
import en from "./locales/en/common.json";

// i18n guard (law §4.2): every key exists in ALL languages, no surplus or missing.
function flatten(obj: Record<string, unknown>, prefix = ""): string[] {
  return Object.entries(obj).flatMap(([k, v]) => {
    const key = prefix ? `${prefix}.${k}` : k;
    return v && typeof v === "object" ? flatten(v as Record<string, unknown>, key) : [key];
  });
}

describe("i18n locales", () => {
  const ptKeys = flatten(ptBR).sort();
  const enKeys = flatten(en).sort();

  it("pt-BR and en have exactly the same keys", () => {
    const missingInEn = ptKeys.filter((k) => !enKeys.includes(k));
    const missingInPt = enKeys.filter((k) => !ptKeys.includes(k));
    expect({ missingInEn, missingInPt }).toEqual({ missingInEn: [], missingInPt: [] });
  });

  it("no translation value is empty", () => {
    for (const [lang, data] of [["pt-BR", ptBR], ["en", en]] as const) {
      const empties = flatten(data).filter((k) =>
        k.split(".").reduce<unknown>((o, p) => (o as Record<string, unknown>)?.[p], data) === "",
      );
      expect(empties, `${lang} has empty keys`).toEqual([]);
    }
  });
});
