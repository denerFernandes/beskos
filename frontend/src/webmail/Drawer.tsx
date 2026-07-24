import { useEffect, type ReactNode } from "react";
import { IcX } from "../panel/icons";
import { overlayOpened } from "./overlay";

// Drawer é o container PADRÃO de detalhe/edição de um item (evento, tarefa, contacto): um painel
// encostado à direita, com o contexto (calendário/lista) à vista ao lado. Um só sítio para o
// comportamento comum — fundo escurecido, fechar ao clicar fora, ESC, scroll do corpo, rodapé fixo,
// largura opcional para conteúdo com painel lateral — para não haver dois padrões na app.
export function Drawer({ title, actions, footer, wide, onClose, children }: {
  title: ReactNode;
  // actions são botões do cabeçalho à direita do título (ex.: o lápis de "editar"). O ✕ é sempre do
  // Drawer, para o gesto de fechar ser igual em todo o lado.
  actions?: ReactNode;
  footer?: ReactNode;
  // wide alarga o painel para conteúdo com coluna lateral (ex.: a tarefa com atividade ao lado).
  wide?: boolean;
  onClose: () => void;
  children: ReactNode;
}) {
  // ESC fecha — o mesmo atalho em toda a app. Só o Drawer o trata: cada écran deixa de reinventar.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => { if (e.key === "Escape") onClose(); };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  // Sidecar aberto → a bolha do assistente esconde-se (regista/desregista no ciclo de vida).
  useEffect(() => overlayOpened(), []);

  return (
    <div className="drawer-bg open" onMouseDown={onClose}>
      <div className={"drawer" + (wide ? " drawer-wide" : "")} onMouseDown={(e) => e.stopPropagation()}>
        <header>
          <span className="dw-title">{title}</span>
          <div className="grow" />
          {actions}
          <button className="ib" onClick={onClose} aria-label="Close"><IcX size={15} /></button>
        </header>
        <div className="body">{children}</div>
        {footer && <footer>{footer}</footer>}
      </div>
    </div>
  );
}
