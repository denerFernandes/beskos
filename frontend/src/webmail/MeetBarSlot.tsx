import { useEffect, useState } from "react";
import { createPortal } from "react-dom";

// Encaixe para pôr os NOSSOS botões dentro da barra de controlos do LiveKit, em vez de flutuarem
// por cima dela.
//
// Porquê um portal e não simplesmente posicionar ao lado: a barra é renderizada pelo
// <VideoConference/> e a sua largura muda com o idioma, os dispositivos e o botão de chat. Qualquer
// posicionamento absoluto ao lado dela é um palpite que falha assim que ela cresce — foi assim que
// os ícones acabaram sobrepostos. Sendo filhos do mesmo flex, partilham a fila e quebram linha
// juntos, sem número mágico nenhum.
//
// Reconstruir a barra de raiz com os hooks do LiveKit era a outra hipótese, mas o ChatToggle depende
// do contexto de layout do <VideoConference/> e o chat deixaria de funcionar.

/** Devolve um nó dentro da barra do LiveKit, ou null enquanto ela não existir. */
export function useControlBarSlot(): HTMLElement | null {
  const [slot, setSlot] = useState<HTMLElement | null>(null);

  useEffect(() => {
    let mine: HTMLDivElement | null = null;

    const attach = () => {
      if (mine?.isConnected) return; // já lá está
      const bar = document.querySelector<HTMLElement>(".lk-control-bar");
      if (!bar) return;
      mine = document.createElement("div");
      // display:contents — o invólucro desaparece do layout e os botões viram filhos diretos da fila.
      mine.className = "room-actions";
      bar.appendChild(mine);
      setSlot(mine);
    };

    attach();
    // A barra só aparece depois de a sala ligar, e o LiveKit volta a montá-la ao reconectar.
    const obs = new MutationObserver(attach);
    obs.observe(document.body, { childList: true, subtree: true });
    return () => {
      obs.disconnect();
      mine?.remove();
    };
  }, []);

  return slot;
}

/**
 * Destino para sobreposições de ecrã inteiro (palco da apresentação, mãos levantadas, modais).
 *
 * `.wm` e NÃO `document.body`: todo o design system está escopado em `.wm` — as variáveis
 * (`--surface`, `--ink`) e as classes utilitárias (`.chipbtn`, `.ib`). Fora dela `var(--surface)`
 * não resolve e os botões perdem o estilo. `.wm` não tem transform/filter/contain, por isso os
 * `position:fixed` continuam a medir-se pelo ecrã — que é a razão de sair da barra de controlos.
 */
export function overlayRoot(): HTMLElement {
  return document.querySelector<HTMLElement>(".wm") ?? document.body;
}

/** Rende os filhos dentro da barra do LiveKit. Enquanto ela não existir, não rende nada. */
export function ControlBarSlot({ children }: { children: React.ReactNode }) {
  const slot = useControlBarSlot();
  return slot ? createPortal(children, slot) : null;
}
