import { useState } from "react";
import { useTranslation } from "react-i18next";
import { IcX } from "../panel/icons";

// "Adicionar link" do composer (estilo Slack): dois campos — o texto que se lê e o endereço.
// Escrever `[texto](url)` à mão no meio da mensagem é fácil de errar e não se vê o resultado; aqui
// a pessoa preenche e o composer insere o markdown já formado.
export function LinkModal({ initialText = "", onSave, onClose }: {
  /** Texto selecionado no composer, se havia — poupa reescrevê-lo. */
  initialText?: string;
  onSave: (text: string, url: string) => void;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const [text, setText] = useState(initialText);
  const [url, setUrl] = useState("");

  const save = () => {
    const href = url.trim();
    if (!href) return;
    // Sem esquema o link ficaria relativo à app (/mail/…) e levava a lado nenhum.
    const full = /^[a-z][a-z0-9+.-]*:|^\//i.test(href) ? href : `https://${href}`;
    onSave(text.trim() || full, full);
  };

  return (
    <div className="tk-bg open" onMouseDown={onClose}>
      <div className="tk modal-sm" onMouseDown={(e) => e.stopPropagation()}>
        <header>
          <span>{t("chat.addLink")}</span>
          <button className="ib" title={t("common.close")} onClick={onClose}><IcX size={15} /></button>
        </header>
        <div className="tk-body link-modal">
          <label className="fld">
            <span>{t("chat.linkText")}</span>
            <input autoFocus value={text} onChange={(e) => setText(e.target.value)}
              onKeyDown={(e) => { if (e.key === "Enter") { e.preventDefault(); save(); } if (e.key === "Escape") onClose(); }} />
          </label>
          <label className="fld">
            <span>{t("chat.linkUrl")}</span>
            <input value={url} placeholder="https://" inputMode="url" onChange={(e) => setUrl(e.target.value)}
              onKeyDown={(e) => { if (e.key === "Enter") { e.preventDefault(); save(); } if (e.key === "Escape") onClose(); }} />
          </label>
          <div className="link-acts">
            <button className="chipbtn" onClick={onClose}>{t("common.cancel")}</button>
            <button className="send" disabled={!url.trim()} onClick={save}>{t("common.save")}</button>
          </div>
        </div>
      </div>
    </div>
  );
}
