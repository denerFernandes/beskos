// Sons de aviso: chegada de notificação, e os três momentos de uma reunião.
//
// PORQUÊ SINTETIZADOS e não ficheiros: são cinco avisos de menos de meio segundo. Em WAV seriam
// centenas de KiB no bundle e cinco pedidos de rede no primeiro toque — o pior momento possível,
// porque é quando se quer o som JÁ. Gerados assim ocupam este ficheiro, tocam offline e afinam-se
// mudando um número em vez de reexportar áudio.
//
// PREFERÊNCIA POR DISPOSITIVO (localStorage), como as notificações em desktopNotify.ts: ligar o som
// no portátil não devia ligá-lo no telemóvel. É a mesma razão e o mesmo sítio.

export type SoundKind = "notify" | "email" | "meetJoin" | "meetKnock" | "meetPeer";

const LS = "mailin.sound.prefs";

export type SoundPrefs = {
  notify: boolean; // avisos de chat, tarefas, ficheiros, calendário, e-mail
  meet: boolean;   // entrar na sala, alguém à porta, alguém entrou
};

const DEF: SoundPrefs = { notify: true, meet: true };

export function soundPrefs(): SoundPrefs {
  try {
    const raw = localStorage.getItem(LS);
    return raw ? { ...DEF, ...JSON.parse(raw) } : DEF;
  } catch { return DEF; }
}

export function setSoundPref(key: keyof SoundPrefs, on: boolean): void {
  try { localStorage.setItem(LS, JSON.stringify({ ...soundPrefs(), [key]: on })); }
  catch { /* modo privado: vale só nesta sessão */ }
}

// Que interruptor manda em cada som.
function prefOf(kind: SoundKind): keyof SoundPrefs {
  return kind === "notify" || kind === "email" ? "notify" : "meet";
}

// --- síntese ---------------------------------------------------------------

// Cada aviso é uma sequência de notas: frequência (Hz), quando começa e quanto dura (segundos).
// As frequências são notas reais para os pares soarem afinados em vez de apenas "dois bips".
type Note = { hz: number; at: number; dur: number; gain?: number };

const VOICES: Record<SoundKind, Note[]> = {
  // Aviso genérico: dois tons a subir (lá5 -> dó#6). Curto e claro, é o que toca mais vezes.
  notify: [
    { hz: 880, at: 0, dur: 0.09 },
    { hz: 1108.73, at: 0.085, dur: 0.14 },
  ],
  // E-mail: um só tom, mais grave e mais suave. Chega muito e-mail; um aviso discreto evita que a
  // pessoa desligue o som todo por cansaço.
  email: [{ hz: 587.33, at: 0, dur: 0.18, gain: 0.5 }],
  // Entrei na sala: três notas a subir (dó6-mi6-sol6), o acorde que diz "ligado".
  meetJoin: [
    { hz: 1046.5, at: 0, dur: 0.08 },
    { hz: 1318.51, at: 0.075, dur: 0.08 },
    { hz: 1567.98, at: 0.15, dur: 0.16 },
  ],
  // Alguém pede para entrar: duas batidas graves, como bater à porta. É o único que o anfitrião TEM
  // de ouvir — se lhe escapar, fica alguém à espera sem saber porquê. Por isso é o mais insistente:
  // grave (corta melhor a voz de quem fala) e com duas batidas em vez de uma.
  meetKnock: [
    { hz: 392, at: 0, dur: 0.11, gain: 1.1 },
    { hz: 392, at: 0.17, dur: 0.13, gain: 1.1 },
  ],
  // Alguém entrou: uma nota só, discreta. Numa reunião de dez pessoas isto toca dez vezes.
  meetPeer: [{ hz: 783.99, at: 0, dur: 0.1, gain: 0.45 }],
};

let ctx: AudioContext | null = null;

function audioContext(): AudioContext | null {
  if (ctx) return ctx;
  try {
    const Ctor = window.AudioContext ?? (window as unknown as { webkitAudioContext?: typeof AudioContext }).webkitAudioContext;
    if (!Ctor) return null;
    ctx = new Ctor();
    return ctx;
  } catch { return null; }
}

// O browser cria o AudioContext SUSPENSO até haver um gesto do utilizador — política de autoplay.
// Sem isto o primeiro aviso seria engolido em silêncio e só o segundo se ouviria. Destrancamos ao
// primeiro clique/tecla/toque na app, e o listener remove-se sozinho.
export function unlockSounds(): void {
  const unlock = () => {
    const c = audioContext();
    if (c && c.state === "suspended") void c.resume().catch(() => {});
  };
  const opts = { once: true, passive: true } as const;
  window.addEventListener("pointerdown", unlock, opts);
  window.addEventListener("keydown", unlock, opts);
  window.addEventListener("touchstart", unlock, opts);
}

// Toca um aviso. Nunca lança: um som que falha não pode partir o que o disparou.
export function playSound(kind: SoundKind): void {
  if (!soundPrefs()[prefOf(kind)]) return;
  const c = audioContext();
  if (!c) return;
  // DO NOT SCHEDULE WHILE SUSPENDED. This is the whole point of the function.
  //
  // The browser suspends the AudioContext of a backgrounded page, and every context is born
  // suspended before the first gesture. While suspended, currentTime DOES NOT ADVANCE. Calling
  // resume() -- which is asynchronous -- and scheduling right after marks the envelope at
  // "now + 10ms" by a stopped clock; by the time the context wakes, seconds later, that instant is
  // in the past. The oscillator still starts, but the gain has already run its attack and decay
  // back there: nothing is heard.
  //
  // This is what made the "someone joined" alert silent whenever the Meet window was not focused --
  // and it is general, not a Meet problem: it applies to any alert while the page is in the
  // background, which is exactly when an alert is worth having.
  if (c.state !== "running") {
    void c.resume().then(() => emit(c, kind)).catch(() => {});
    return;
  }
  emit(c, kind);
}

// emit writes the notes into the context. Only ever called with the context running.
function emit(c: AudioContext, kind: SoundKind): void {
  const t0 = c.currentTime + 0.01;
  for (const n of VOICES[kind]) {
    try {
      const osc = c.createOscillator();
      const g = c.createGain();
      // Onda sinusoidal: sem harmónicos ásperos. Uma quadrada seria estridente ao fim do dia.
      osc.type = "sine";
      osc.frequency.value = n.hz;
      // Envelope com ataque e queda: um ganho ligado/desligado de repente dá um "clique" audível,
      // que é o som de um altifalante a saltar, não de um aviso.
      const startAt = t0 + n.at;
      const peak = 0.16 * (n.gain ?? 1);
      g.gain.setValueAtTime(0.0001, startAt);
      g.gain.exponentialRampToValueAtTime(peak, startAt + 0.012);
      g.gain.exponentialRampToValueAtTime(0.0001, startAt + n.dur);
      osc.connect(g).connect(c.destination);
      osc.start(startAt);
      osc.stop(startAt + n.dur + 0.02);
    } catch { /* um aviso falhado não pode partir quem o disparou */ }
  }
}
