import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { IcX } from "../panel/icons";

// Barra PARTILHADA do visualizador em tela cheia (ficheiro do Drive, anexo de e-mail, anexo de
// chat…): "Fechar" + as ações (ex.: Baixar) à ESQUERDA, título sempre ao CENTRO. Um só sítio para
// o layout — as várias telas de pré-visualização passam a ter a mesma barra (24/07/2026).
export function ViewerBar({ title, onClose, children }: {
  title: string; onClose: () => void; children?: ReactNode; // children = ações extra (ex.: Baixar)
}) {
  const { t } = useTranslation();
  return (
    <header className="viewer-bar">
      <span className="vb-title" title={title}>{title}</span>
      <div className="vb-actions">
        {children}
        <button className="chipbtn" onClick={onClose}><IcX size={15} /> {t("editor.close")}</button>
      </div>
    </header>
  );
}
