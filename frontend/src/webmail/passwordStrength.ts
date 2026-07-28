// Força da palavra-passe, do lado do cliente.
//
// O servidor de correio (Stalwart) mede com o zxcvbn e RECUSA abaixo do mínimo configurado — foi
// isso que devolvia "Bad gateway" ao criar caixas. Esta função existe para a pessoa saber ANTES de
// submeter, e é de propósito mais exigente do que o servidor: melhor recusar aqui, com explicação,
// do que deixar passar e apanhar a recusa dele.
//
// Não trazemos o zxcvbn (≈800 KB com dicionários) para uma caixa de texto: replicamos os sinais que
// pesam de facto — comprimento, variedade, repetições, sequências, palavras comuns e o próprio
// endereço de quem escolhe a senha.

export type Score = 0 | 1 | 2 | 3 | 4;

export interface Strength {
  score: Score;
  /** Aceitável para submeter (o servidor recusa abaixo disto). */
  ok: boolean;
  /** Chave i18n do que falta melhorar ("" quando está bem). */
  hint: string;
}

/** Mínimo aceite: abaixo de 12 caracteres nem vale a pena medir o resto. */
export const MIN_LENGTH = 12;

// As mais usadas do mundo + as óbvias em português. Lista curta de propósito: é para apanhar o
// descuido ("password123"), não para fazer de dicionário.
const COMMON = [
  "password", "passwd", "senha", "palavra-passe", "qwerty", "azerty", "letmein", "welcome",
  "admin", "administrator", "iloveyou", "sunshine", "princess", "dragon", "monkey", "football",
  "abcabc", "123456", "12345678", "111111", "000000", "beskos", "correio", "email",
];

const SEQUENCES = ["abcdefghijklmnopqrstuvwxyz", "0123456789", "qwertyuiop", "asdfghjkl", "zxcvbnm"];

// Repetição de bloco: "abcabcabc", "ha-ha-ha". É o caso que o Stalwart citou na recusa.
function hasRepeatedBlock(s: string): boolean {
  // Metade e não um terço: "noreply2026noreply2026" é o bloco repetido DUAS vezes, e uma senha
  // assim é tão fraca como o bloco sozinho — foi o caso que passou no primeiro teste.
  for (let len = 1; len <= Math.floor(s.length / 2); len++) {
    const block = s.slice(0, len);
    if (block.repeat(Math.ceil(s.length / len)).slice(0, s.length) === s) return true;
  }
  // Também apanha repetições no meio ("xyzabcabcabc").
  return /(.{2,8})\1{1,}/.test(s);
}

// Corrida sequencial de 4+ (crescente ou decrescente) no teclado ou no alfabeto.
function hasSequence(s: string): boolean {
  const low = s.toLowerCase();
  for (const seq of SEQUENCES) {
    const rev = [...seq].reverse().join("");
    for (let i = 0; i + 4 <= seq.length; i++) {
      if (low.includes(seq.slice(i, i + 4)) || low.includes(rev.slice(i, i + 4))) return true;
    }
  }
  return false;
}

/**
 * measure avalia a senha. `userInputs` são dados de quem escolhe (e-mail, nome, domínio): o zxcvbn
 * penaliza-os e o Stalwart passa-lhos, por isso penalizamos também.
 */
export function measure(password: string, userInputs: string[] = []): Strength {
  const pw = password ?? "";
  if (pw.length === 0) return { score: 0, ok: false, hint: "" };
  if (pw.length < MIN_LENGTH) return { score: pw.length < 8 ? 0 : 1, ok: false, hint: "pwHintShort" };

  const low = pw.toLowerCase();
  // Pedaços do endereço/nome: "dener", "beskos", "com"… Palavras de 3+ letras contam.
  const parts = userInputs
    .flatMap((s) => (s ?? "").toLowerCase().split(/[^a-z0-9]+/i))
    .filter((s) => s.length >= 3);
  if (parts.some((p) => low.includes(p))) return { score: 1, ok: false, hint: "pwHintPersonal" };
  if (COMMON.some((c) => low.includes(c))) return { score: 1, ok: false, hint: "pwHintCommon" };
  if (hasRepeatedBlock(pw)) return { score: 1, ok: false, hint: "pwHintRepeat" };
  if (hasSequence(pw)) return { score: 1, ok: false, hint: "pwHintSequence" };

  // Variedade + comprimento. Uma frase longa vale tanto como um esparguete curto com símbolos —
  // é o que o zxcvbn também conclui.
  const classes = [/[a-z]/, /[A-Z]/, /[0-9]/, /[^a-zA-Z0-9]/].filter((re) => re.test(pw)).length;
  const words = pw.trim().split(/[\s._-]+/).filter((w) => w.length >= 3).length;
  let score: Score = 2;
  if (pw.length >= 16 || (pw.length >= 14 && classes >= 3) || words >= 4) score = 4;
  else if (pw.length >= 14 || classes >= 3 || words >= 3) score = 3;

  const hint = score >= 3 ? "" : "pwHintWeak";
  return { score, ok: score >= 3, hint };
}
