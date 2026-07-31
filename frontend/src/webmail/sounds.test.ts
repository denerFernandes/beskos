import { describe, it, expect, beforeEach, vi, afterEach } from "vitest";

// Um som que não toca é um defeito invisível: ninguém abre um bug por "não ouvi nada", assume que
// desligou sem querer. Por isso o que se testa aqui é o que decide se toca ou não.
//
// Cada teste reimporta o módulo (vi.resetModules) porque ele guarda o AudioContext em cache — um só
// contexto por página, que é o correto em produção mas faria um teste herdar o áudio falso do outro.

// O ambiente do vitest aqui é node, sem DOM. Simulamos o mínimo em vez de puxar o jsdom só para
// isto: são quatro métodos de localStorage e um addEventListener, contra uma dependência inteira
// para o mesmo efeito (lei 2.5).
function memoryStorage(): Storage {
  const m = new Map<string, string>();
  return {
    getItem: (k: string) => m.get(k) ?? null,
    setItem: (k: string, v: string) => { m.set(k, String(v)); },
    removeItem: (k: string) => { m.delete(k); },
    clear: () => m.clear(),
  } as unknown as Storage;
}

// AudioContext falso que regista quando cada oscilador arranca — é isso que prova que houve som.
function stage() {
  const started: number[] = [];
  const wire = { connect: (n: unknown) => n };
  const ctx = {
    state: "running",
    currentTime: 0,
    resume: () => Promise.resolve(),
    createOscillator: () => ({
      type: "", frequency: { value: 0 }, connect: wire.connect,
      start: (t: number) => started.push(t), stop: () => {},
    }),
    createGain: () => ({
      gain: { setValueAtTime: () => {}, exponentialRampToValueAtTime: () => {} },
      connect: wire.connect,
    }),
    destination: {},
  };
  return { started, AudioContext: function () { return ctx; } as unknown as typeof AudioContext };
}

// Monta o ambiente e devolve o módulo fresco. `withAudio: false` simula um ambiente sem Web Audio.
async function setup(withAudio = true) {
  const p = stage();
  const listeners: string[] = [];
  vi.stubGlobal("localStorage", memoryStorage());
  vi.stubGlobal("window", {
    AudioContext: withAudio ? p.AudioContext : undefined,
    addEventListener: (n: string, _f: unknown, o?: { once?: boolean }) => { listeners.push(n); if (o) expect(o.once).toBe(true); },
  });
  vi.resetModules();
  const mod = await import("./sounds");
  return { ...mod, started: p.started, listeners };
}

beforeEach(() => vi.unstubAllGlobals());
afterEach(() => vi.unstubAllGlobals());

describe("preferências", () => {
  it("vêm ligadas por omissão — um produto que chega mudo parece avariado", async () => {
    const { soundPrefs } = await setup();
    expect(soundPrefs()).toEqual({ notify: true, meet: true });
  });

  it("guarda e relê", async () => {
    const { soundPrefs, setSoundPref } = await setup();
    setSoundPref("notify", false);
    expect(soundPrefs()).toEqual({ notify: false, meet: true });
  });

  it("desligar um não desliga o outro — é a razão de serem dois interruptores", async () => {
    const { soundPrefs, setSoundPref } = await setup();
    setSoundPref("notify", false);
    expect(soundPrefs().meet).toBe(true);
  });

  it("localStorage corrompido cai no padrão em vez de rebentar", async () => {
    const { soundPrefs } = await setup();
    localStorage.setItem("mailin.sound.prefs", "{isto não é json");
    expect(soundPrefs()).toEqual({ notify: true, meet: true });
  });
});

describe("playSound", () => {
  it("toca quando o interruptor correspondente está ligado", async () => {
    const { playSound, started } = await setup();
    playSound("notify");
    expect(started.length).toBeGreaterThan(0);
  });

  it("não toca com o interruptor desligado", async () => {
    const { playSound, setSoundPref, started } = await setup();
    setSoundPref("notify", false);
    playSound("notify");
    expect(started).toHaveLength(0);
  });

  // O e-mail é governado pelo interruptor de avisos, não por um terceiro. Se isto se separar sem
  // querer, desligar os avisos deixaria o e-mail a tocar — o oposto do que foi pedido.
  it("o e-mail obedece ao interruptor dos avisos", async () => {
    const { playSound, setSoundPref, started } = await setup();
    setSoundPref("notify", false);
    playSound("email");
    expect(started).toHaveLength(0);
  });

  it("os sons do Meet obedecem ao interruptor do Meet, não ao dos avisos", async () => {
    const { playSound, setSoundPref, started } = await setup();
    setSoundPref("notify", false);
    playSound("meetKnock");
    expect(started.length).toBeGreaterThan(0);
    setSoundPref("meet", false);
    started.length = 0;
    playSound("meetKnock");
    expect(started).toHaveLength(0);
  });

  // O pedido de entrada é o único que o anfitrião TEM de ouvir: se lhe escapa, fica alguém à espera
  // sem saber porquê. Duas batidas distinguem-no de um bip qualquer.
  it("o pedido de entrada tem duas batidas", async () => {
    const { playSound, started } = await setup();
    playSound("meetKnock");
    expect(started).toHaveLength(2);
    expect(started[1]).toBeGreaterThan(started[0]);
  });

  it("sem Web Audio no ambiente, não lança — um aviso falhado não pode partir quem o disparou", async () => {
    const { playSound } = await setup(false);
    expect(() => playSound("notify")).not.toThrow();
  });
});

describe("unlockSounds", () => {
  it("espera pelo gesto nos três tipos de entrada, uma vez cada", async () => {
    const { unlockSounds, listeners } = await setup();
    unlockSounds();
    expect(listeners).toEqual(["pointerdown", "keydown", "touchstart"]);
  });
});

// A suspended context is the normal state of a page the browser has put in the background, and it
// is also where every context starts before the first gesture. currentTime does not advance while
// suspended, so scheduling "now + 10ms" and calling resume() in the same breath puts the whole gain
// envelope in the past: the oscillator starts, the envelope has already finished, nothing is heard.
// This is what made a Meet join sound silent while the window was not focused.
describe("a suspended context", () => {
  it("schedules only after the context is running again", async () => {
    const started: number[] = [];
    const gains: number[] = [];
    let state = "suspended";
    let now = 5;
    const wire = { connect: (n: unknown) => n };
    const ctx = {
      get state() { return state; },
      get currentTime() { return now; },
      // Real resume() is ASYNC: the state and the clock only move when the promise settles. A fake
      // that mutates synchronously hides exactly the bug this test exists for.
      resume: () => Promise.resolve().then(() => { state = "running"; now = 9; }),
      createOscillator: () => ({
        type: "", frequency: { value: 0 }, connect: wire.connect,
        start: (t: number) => started.push(t), stop: () => {},
      }),
      createGain: () => ({
        gain: { setValueAtTime: (_v: number, t: number) => gains.push(t), exponentialRampToValueAtTime: () => {} },
        connect: wire.connect,
      }),
      destination: {},
    };
    vi.stubGlobal("localStorage", memoryStorageForTest());
    vi.stubGlobal("window", { AudioContext: function () { return ctx; } as unknown as typeof AudioContext, addEventListener: () => {} });
    vi.resetModules();
    const { playSound } = await import("./sounds");

    playSound("meetPeer");
    await Promise.resolve();
    await Promise.resolve();

    expect(started.length).toBeGreaterThan(0);
    // The envelope must start at or after the time the context reports once awake -- never before.
    expect(Math.min(...gains)).toBeGreaterThanOrEqual(9);
    expect(Math.min(...started)).toBeGreaterThanOrEqual(9);
  });
});

function memoryStorageForTest(): Storage {
  const m = new Map<string, string>();
  return {
    getItem: (k: string) => m.get(k) ?? null,
    setItem: (k: string, v: string) => { m.set(k, String(v)); },
    removeItem: (k: string) => { m.delete(k); },
    clear: () => m.clear(),
  } as unknown as Storage;
}
