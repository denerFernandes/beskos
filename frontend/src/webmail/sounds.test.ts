import { describe, it, expect, beforeEach, vi, afterEach } from "vitest";

// Um som que não toca é um defeito invisível: ninguém abre um bug por "não ouvi nada", assume que
// desligou sem querer. Por isso o que se testa aqui é o que decide se toca ou não.
//
// Cada teste reimporta o módulo (vi.resetModules) porque ele guarda o AudioContext em cache — um só
// contexto por página, que é o correto em produção mas faria um teste herdar o áudio falso do outro.

// O ambiente do vitest aqui é node, sem DOM. Simulamos o mínimo em vez de puxar o jsdom só para
// isto: são quatro métodos de localStorage e um addEventListener, contra uma dependência inteira
// para o mesmo efeito (lei 2.5).
function memoriaLocal(): Storage {
  const m = new Map<string, string>();
  return {
    getItem: (k: string) => m.get(k) ?? null,
    setItem: (k: string, v: string) => { m.set(k, String(v)); },
    removeItem: (k: string) => { m.delete(k); },
    clear: () => m.clear(),
  } as unknown as Storage;
}

// AudioContext falso que regista quando cada oscilador arranca — é isso que prova que houve som.
function palco() {
  const arrancados: number[] = [];
  const ligar = { connect: (n: unknown) => n };
  const ctx = {
    state: "running",
    currentTime: 0,
    resume: () => Promise.resolve(),
    createOscillator: () => ({
      type: "", frequency: { value: 0 }, connect: ligar.connect,
      start: (t: number) => arrancados.push(t), stop: () => {},
    }),
    createGain: () => ({
      gain: { setValueAtTime: () => {}, exponentialRampToValueAtTime: () => {} },
      connect: ligar.connect,
    }),
    destination: {},
  };
  return { arrancados, AudioContext: function () { return ctx; } as unknown as typeof AudioContext };
}

// Monta o ambiente e devolve o módulo fresco. `comAudio: false` simula um ambiente sem Web Audio.
async function montar(comAudio = true) {
  const p = palco();
  const escutas: string[] = [];
  vi.stubGlobal("localStorage", memoriaLocal());
  vi.stubGlobal("window", {
    AudioContext: comAudio ? p.AudioContext : undefined,
    addEventListener: (n: string, _f: unknown, o?: { once?: boolean }) => { escutas.push(n); if (o) expect(o.once).toBe(true); },
  });
  vi.resetModules();
  const mod = await import("./sounds");
  return { ...mod, arrancados: p.arrancados, escutas };
}

beforeEach(() => vi.unstubAllGlobals());
afterEach(() => vi.unstubAllGlobals());

describe("preferências", () => {
  it("vêm ligadas por omissão — um produto que chega mudo parece avariado", async () => {
    const { soundPrefs } = await montar();
    expect(soundPrefs()).toEqual({ notify: true, meet: true });
  });

  it("guarda e relê", async () => {
    const { soundPrefs, setSoundPref } = await montar();
    setSoundPref("notify", false);
    expect(soundPrefs()).toEqual({ notify: false, meet: true });
  });

  it("desligar um não desliga o outro — é a razão de serem dois interruptores", async () => {
    const { soundPrefs, setSoundPref } = await montar();
    setSoundPref("notify", false);
    expect(soundPrefs().meet).toBe(true);
  });

  it("localStorage corrompido cai no padrão em vez de rebentar", async () => {
    const { soundPrefs } = await montar();
    localStorage.setItem("mailin.sound.prefs", "{isto não é json");
    expect(soundPrefs()).toEqual({ notify: true, meet: true });
  });
});

describe("playSound", () => {
  it("toca quando o interruptor correspondente está ligado", async () => {
    const { playSound, arrancados } = await montar();
    playSound("notify");
    expect(arrancados.length).toBeGreaterThan(0);
  });

  it("não toca com o interruptor desligado", async () => {
    const { playSound, setSoundPref, arrancados } = await montar();
    setSoundPref("notify", false);
    playSound("notify");
    expect(arrancados).toHaveLength(0);
  });

  // O e-mail é governado pelo interruptor de avisos, não por um terceiro. Se isto se separar sem
  // querer, desligar os avisos deixaria o e-mail a tocar — o oposto do que foi pedido.
  it("o e-mail obedece ao interruptor dos avisos", async () => {
    const { playSound, setSoundPref, arrancados } = await montar();
    setSoundPref("notify", false);
    playSound("email");
    expect(arrancados).toHaveLength(0);
  });

  it("os sons do Meet obedecem ao interruptor do Meet, não ao dos avisos", async () => {
    const { playSound, setSoundPref, arrancados } = await montar();
    setSoundPref("notify", false);
    playSound("meetKnock");
    expect(arrancados.length).toBeGreaterThan(0);
    setSoundPref("meet", false);
    arrancados.length = 0;
    playSound("meetKnock");
    expect(arrancados).toHaveLength(0);
  });

  // O pedido de entrada é o único que o anfitrião TEM de ouvir: se lhe escapa, fica alguém à espera
  // sem saber porquê. Duas batidas distinguem-no de um bip qualquer.
  it("o pedido de entrada tem duas batidas", async () => {
    const { playSound, arrancados } = await montar();
    playSound("meetKnock");
    expect(arrancados).toHaveLength(2);
    expect(arrancados[1]).toBeGreaterThan(arrancados[0]);
  });

  it("sem Web Audio no ambiente, não lança — um aviso falhado não pode partir quem o disparou", async () => {
    const { playSound } = await montar(false);
    expect(() => playSound("notify")).not.toThrow();
  });
});

describe("unlockSounds", () => {
  it("espera pelo gesto nos três tipos de entrada, uma vez cada", async () => {
    const { unlockSounds, escutas } = await montar();
    unlockSounds();
    expect(escutas).toEqual(["pointerdown", "keydown", "touchstart"]);
  });
});
