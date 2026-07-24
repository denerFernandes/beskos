// Regras de e-mail: modelo visual ⇄ script Sieve.
//
// O Sieve é a fonte única: o script guardado no Stalwart carrega a própria definição num
// comentário `# mailin:rules {...}`. Sem tabela nossa, sem sincronização, sem desvio — e funciona
// de qualquer dispositivo, mesmo depois de um restauro da nossa base.
//
// LIMITE DECLARADO: Sieve corre na ENTREGA. Regras de saída ("quando envio") não são Sieve; são
// aplicadas pelo webmail no momento do envio, e por isso NÃO valem se enviar por outro cliente.

export type Trigger = "receive" | "send";
export type Field = "from" | "to" | "subject" | "body" | "cc";
export type Op = "contains" | "is" | "starts" | "ends" | "domain";
export type ActionKind = "move" | "flag" | "read" | "reply" | "forward" | "delete" | "keyword" | "stop";

export interface Condition { field: Field; op: Op; value: string }
export interface Action { kind: ActionKind; value?: string; extra?: string }

export interface Rule {
  id: string;
  name: string;
  enabled: boolean;
  trigger: Trigger;
  match: "all" | "any";   // E / OU entre as condições
  conditions: Condition[];
  actions: Action[];
}

// Resposta de férias (vacation): auto-resposta com janela de datas e escopo. Vive no MESMO script
// Sieve das regras (comentário próprio), para coexistir com elas sem uma segunda fonte de verdade.
export interface VacationConfig {
  enabled: boolean;
  subject: string;
  message: string;
  start?: string;          // YYYY-MM-DD (opcional)
  end?: string;            // YYYY-MM-DD (opcional)
  scope: "all" | "internal"; // internal = só responde a remetentes do próprio domínio
  domain?: string;         // domínio do utilizador (para o escopo interno)
}

export const emptyVacation = (): VacationConfig => ({ enabled: false, subject: "", message: "", scope: "all" });

export const newRule = (): Rule => ({
  id: Math.random().toString(36).slice(2, 10),
  name: "",
  enabled: true,
  trigger: "receive",
  match: "all",
  conditions: [{ field: "from", op: "contains", value: "" }],
  actions: [{ kind: "move", value: "" }],
});

// --- Sieve: escapar strings -------------------------------------------------

// Numa string Sieve só \ e " são especiais. Sem isto, um assunto com aspas partia o script inteiro
// — e o utilizador via um erro do Stalwart em vez do seu erro de digitação.
const q = (s: string) => `"${s.replace(/\\/g, "\\\\").replace(/"/g, '\\"')}"`;

const HEADER: Record<Field, string> = { from: "from", to: "to", cc: "cc", subject: "subject", body: "" };

// testFor traduz uma condição. `address :domain` compara só o domínio do endereço — é o que faz
// "de qualquer um da acme.com" funcionar sem depender do texto do nome.
function testFor(c: Condition): string {
  const v = q(c.value);
  if (c.field === "body") return `body :text :contains ${v}`;
  const h = q(HEADER[c.field]);
  switch (c.op) {
    case "domain": return `address :domain :is ${h} ${v}`;
    case "is": return `header :is ${h} ${v}`;
    case "starts": return `header :matches ${h} ${q(escapeGlob(c.value) + "*")}`;
    case "ends": return `header :matches ${h} ${q("*" + escapeGlob(c.value))}`;
    default: return `header :contains ${h} ${v}`;
  }
}

// Em :matches, * e ? são curingas. Quem escreve "50% *promoção*" quer o literal.
const escapeGlob = (s: string) => s.replace(/([*?\\])/g, "\\$1");

// requiresFor: só as extensões usadas. Pedir tudo faria o Stalwart recusar o script se alguma
// extensão não estivesse compilada nele.
function requiresFor(rules: Rule[]): string[] {
  const need = new Set<string>();
  for (const r of rules) {
    if (r.conditions.some((c) => c.field === "body")) need.add("body");
    for (const a of r.actions) {
      if (a.kind === "move") need.add("fileinto");
      if (a.kind === "flag" || a.kind === "read" || a.kind === "keyword") need.add("imap4flags");
      if (a.kind === "reply") need.add("vacation");
    }
  }
  return [...need].sort();
}

// actionsFor: a ORDEM não é estética. As marcas (addflag/setflag) têm de vir ANTES do fileinto —
// depois de arquivar, a mensagem já foi e a marca não pega. Medido contra o Stalwart 0.16.4.
function actionsFor(actions: Action[]): string[] {
  const out: string[] = [];
  const flags = actions.filter((a) => a.kind === "flag" || a.kind === "read" || a.kind === "keyword");
  const rest = actions.filter((a) => !flags.includes(a));

  for (const a of flags) {
    if (a.kind === "flag") out.push(`addflag "\\\\Flagged";`);
    if (a.kind === "read") out.push(`addflag "\\\\Seen";`);
    if (a.kind === "keyword" && a.value) out.push(`addflag ${q(a.value)};`);
  }
  for (const a of rest) {
    switch (a.kind) {
      case "move": if (a.value) out.push(`fileinto ${q(a.value)};`); break;
      case "forward": if (a.value) out.push(`redirect ${q(a.value)};`); break;
      case "reply":
        // :days 1 evita responder à mesma pessoa em loop no mesmo dia — auto-resposta sem isto é
        // como se cria uma tempestade de e-mails entre dois autorresponders.
        out.push(`vacation :days 1 ${a.extra ? `:subject ${q(a.extra)} ` : ""}${q(a.value ?? "")};`);
        break;
      case "delete": out.push("discard;"); break;
      case "stop": out.push("stop;"); break;
    }
  }
  return out;
}

// compile: fluxo visual → script Sieve. O JSON vai num comentário para podermos reabrir o editor.
export function compile(rules: Rule[], vacation?: VacationConfig): string {
  const inbound = rules.filter((r) => r.trigger === "receive" && r.enabled);
  const req = new Set(requiresFor(inbound));
  if (vacation?.enabled) {
    req.add("vacation");
    if (vacation.start || vacation.end) { req.add("date"); req.add("relational"); }
  }
  const lines: string[] = [
    "# Gerado pelo Beskos — não edite à mão: o editor reescreve este ficheiro.",
    `# mailin:rules ${JSON.stringify(rules)}`,
  ];
  if (vacation) lines.push(`# mailin:vacation ${JSON.stringify(vacation)}`);
  lines.push("");
  if (req.size) lines.push(`require [${[...req].sort().map(q).join(", ")}];`, "");

  // Vacation primeiro: responde independentemente do que as regras façam com a mensagem.
  if (vacation?.enabled) {
    const guards: string[] = [];
    if (vacation.start) guards.push(`currentdate :value "ge" "date" ${q(vacation.start)}`);
    if (vacation.end) guards.push(`currentdate :value "le" "date" ${q(vacation.end)}`);
    if (vacation.scope === "internal" && vacation.domain) guards.push(`address :domain :is "from" ${q(vacation.domain)}`);
    const vac = `vacation :days 1 ${vacation.subject ? `:subject ${q(vacation.subject)} ` : ""}${q(vacation.message)};`;
    lines.push("# Resposta de férias (Beskos)");
    if (guards.length) {
      const cond = guards.length === 1 ? guards[0] : `allof(${guards.join(", ")})`;
      lines.push(`if ${cond} {`, "  " + vac, "}", "");
    } else {
      lines.push(vac, "");
    }
  }

  for (const r of inbound) {
    const tests = r.conditions.filter((c) => c.value.trim()).map(testFor);
    lines.push(`# ${r.name || "(sem nome)"}`);
    if (tests.length === 0) {
      // Sem condição, a regra vale para tudo — dizê-lo no script, não escondê-lo.
      lines.push("# (sem condições: vale para todas as mensagens)");
      lines.push(...actionsFor(r.actions).map((a) => a));
    } else {
      const join = r.match === "any" ? "anyof" : "allof";
      const cond = tests.length === 1 ? tests[0] : `${join}(${tests.join(", ")})`;
      lines.push(`if ${cond} {`);
      lines.push(...actionsFor(r.actions).map((a) => "  " + a));
      lines.push("}");
    }
    lines.push("");
  }
  return lines.join("\n");
}

// parse: lê de volta o JSON do comentário. Se o script existir sem a nossa marca (alguém escreveu
// à mão), devolve null — melhor dizer "não consigo ler isto" do que apagar o trabalho da pessoa.
export function parse(script: string): Rule[] | null {
  const m = script.match(/^# mailin:rules (.+)$/m);
  if (!m) return null;
  try {
    const parsed = JSON.parse(m[1]);
    return Array.isArray(parsed) ? (parsed as Rule[]) : null;
  } catch {
    return null;
  }
}

// parseVacation: lê a config de férias do mesmo script (comentário próprio). Ausente → null.
export function parseVacation(script: string): VacationConfig | null {
  const m = script.match(/^# mailin:vacation (.+)$/m);
  if (!m) return null;
  try {
    return JSON.parse(m[1]) as VacationConfig;
  } catch {
    return null;
  }
}

// --- Regras de saída: aplicadas pelo webmail no envio -----------------------

export interface Draft { to?: string; cc?: string; subject?: string; body?: string }

// matchesOutbound testa um rascunho contra uma regra de saída, com a mesma semântica do Sieve.
export function matchesOutbound(r: Rule, d: Draft): boolean {
  const val = (f: Field): string =>
    f === "to" ? d.to ?? "" : f === "cc" ? d.cc ?? "" : f === "subject" ? d.subject ?? "" : f === "body" ? d.body ?? "" : "";
  const one = (c: Condition) => {
    const v = val(c.field).toLowerCase();
    const n = c.value.trim().toLowerCase();
    if (!n) return false;
    switch (c.op) {
      case "is": return v === n;
      case "starts": return v.startsWith(n);
      case "ends": return v.endsWith(n);
      case "domain": return v.split("@").pop()?.trim() === n.replace(/^@/, "");
      default: return v.includes(n);
    }
  };
  const tests = r.conditions.filter((c) => c.value.trim());
  if (tests.length === 0) return true;
  return r.match === "any" ? tests.some(one) : tests.every(one);
}

// outboundBcc avalia as regras de SAÍDA de um rascunho no envio e devolve os endereços a incluir em
// Bcc (ação "forward" das regras que casam). Honra "stop". É o que o webmail consegue aplicar no
// envio (as demais ações só valem na entrega, via Sieve). Antes, nenhuma regra de saída era avaliada.
export function outboundBcc(rules: Rule[], d: Draft): string[] {
  const bcc: string[] = [];
  for (const r of rules) {
    if (r.trigger !== "send" || !r.enabled) continue;
    if (!matchesOutbound(r, d)) continue;
    for (const a of r.actions) {
      if (a.kind === "forward" && a.value?.trim()) bcc.push(a.value.trim());
      if (a.kind === "stop") return bcc;
    }
  }
  return bcc;
}
