import { useEffect } from "react";

// Tooltip GLOBAL, instantâneo e estilizado. Em vez de editar cada botão, intercepta QUALQUER
// elemento com `title` (ou `data-tip`) no hover/focus, mostra uma bolha bonita e suprime o tooltip
// nativo do SO (lento e inconsistente). Regra do projeto: todo botão só-de-ícone tem `title` — este
// gestor transforma-os todos num tooltip coerente. `data-tip-side` (right/left/top/bottom) força o
// lado; por omissão fica em baixo e vira para cima se não couber.

const DELAY = 180; // ms — curto, mas evita "piscar" ao varrer o ecrã com o rato

export function GlobalTooltip() {
  useEffect(() => {
    // Sem hover (toque): não faz sentido; evita bolhas presas em telemóveis/tablets.
    if (matchMedia("(hover: none)").matches) return;

    const bubble = document.createElement("div");
    bubble.className = "gtt";
    bubble.setAttribute("role", "tooltip");
    document.body.appendChild(bubble);

    let current: HTMLElement | null = null;
    let timer: number | undefined;

    const textOf = (el: HTMLElement) =>
      (el.getAttribute("data-tip") || el.getAttribute("data-tiptext") || el.getAttribute("title") || "").trim();

    // Sobe na árvore até um elemento com dica (o alvo do rato pode ser o <svg> dentro do botão).
    const findTarget = (node: EventTarget | null): HTMLElement | null => {
      let el = node as HTMLElement | null;
      for (let i = 0; el && i < 6; i++) {
        if (el.nodeType === 1 && (el.hasAttribute?.("data-tip") || el.hasAttribute?.("title")) && textOf(el)) return el;
        el = el.parentElement;
      }
      return null;
    };

    const place = (el: HTMLElement) => {
      const side = el.getAttribute("data-tip-side") || "bottom";
      const r = el.getBoundingClientRect();
      const gap = 8;
      // Mede primeiro (a bolha já tem o texto e está visível mas fora de vista).
      const bw = bubble.offsetWidth, bh = bubble.offsetHeight;
      let left: number, top: number, resolved = side;
      const centerX = r.left + r.width / 2 - bw / 2;
      const centerY = r.top + r.height / 2 - bh / 2;
      if (side === "right") { left = r.right + gap; top = centerY; }
      else if (side === "left") { left = r.left - gap - bw; top = centerY; }
      else if (side === "top") { left = centerX; top = r.top - gap - bh; }
      else { left = centerX; top = r.bottom + gap; } // bottom
      // Vira baixo↔cima se não couber verticalmente.
      if (resolved === "bottom" && top + bh > innerHeight - 4) { top = r.top - gap - bh; resolved = "top"; }
      else if (resolved === "top" && top < 4) { top = r.bottom + gap; resolved = "bottom"; }
      // Nunca sai do ecrã.
      left = Math.max(6, Math.min(left, innerWidth - bw - 6));
      top = Math.max(6, Math.min(top, innerHeight - bh - 6));
      bubble.style.left = `${Math.round(left)}px`;
      bubble.style.top = `${Math.round(top)}px`;
    };

    const show = (el: HTMLElement) => {
      const text = textOf(el);
      if (!text) return;
      // Suprime o nativo: guarda o title em data-tiptext e remove-o enquanto a bolha está visível.
      if (el.hasAttribute("title")) {
        el.setAttribute("data-tiptext", el.getAttribute("title") || "");
        el.removeAttribute("title");
      }
      bubble.textContent = text;
      bubble.classList.add("on");
      place(el);
    };

    const hide = () => {
      if (timer) { clearTimeout(timer); timer = undefined; }
      bubble.classList.remove("on");
      if (current?.hasAttribute("data-tiptext")) {
        current.setAttribute("title", current.getAttribute("data-tiptext") || "");
        current.removeAttribute("data-tiptext");
      }
      current = null;
    };

    const enter = (node: EventTarget | null) => {
      const el = findTarget(node);
      if (el === current) return;
      hide();
      if (!el) return;
      current = el;
      timer = window.setTimeout(() => { if (current === el) show(el); }, DELAY);
    };

    const onOver = (e: MouseEvent) => enter(e.target);
    const onOut = (e: MouseEvent) => {
      if (current && !current.contains(e.relatedTarget as Node)) hide();
    };
    const onFocus = (e: FocusEvent) => enter(e.target);
    const onBlur = () => hide();

    document.addEventListener("mouseover", onOver);
    document.addEventListener("mouseout", onOut);
    document.addEventListener("focusin", onFocus);
    document.addEventListener("focusout", onBlur);
    // Some ao clicar/rolar/teclar — senão fica pendurada.
    document.addEventListener("click", hide, true);
    window.addEventListener("scroll", hide, true);
    window.addEventListener("keydown", hide, true);

    return () => {
      hide();
      document.removeEventListener("mouseover", onOver);
      document.removeEventListener("mouseout", onOut);
      document.removeEventListener("focusin", onFocus);
      document.removeEventListener("focusout", onBlur);
      document.removeEventListener("click", hide, true);
      window.removeEventListener("scroll", hide, true);
      window.removeEventListener("keydown", hide, true);
      bubble.remove();
    };
  }, []);
  return null;
}
