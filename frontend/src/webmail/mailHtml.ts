import DOMPurify from "dompurify";

// Sanitização do HTML de e-mails. NÃO se faz à mão: o corpo vem de um remetente hostil por
// definição, e um filtro caseiro deixa sempre passar um vetor (svg onload, javascript: em href,
// css expression, …). DOMPurify é a escolha responsável — esta é uma dependência de segurança,
// não de conveniência (lei §2.5).

export interface SanitizedMail {
  html: string;
  blockedImages: number; // quantas imagens externas foram removidas (para oferecer "mostrar")
}

// Hook único: neutraliza links e imagens ANTES de o DOMPurify devolver o resultado.
let hookInstalled = false;
function installHook() {
  if (hookInstalled) return;
  hookInstalled = true;
  DOMPurify.addHook("afterSanitizeAttributes", (node) => {
    // Todo link abre em nova aba e sem passar o referer/opener — um e-mail não controla a nossa aba.
    if (node.tagName === "A") {
      node.setAttribute("target", "_blank");
      node.setAttribute("rel", "noopener noreferrer nofollow");
    }
    // Imagens externas são rastreadores até prova em contrário (o "pixel" que confirma a abertura).
    // Guardo o src original em data-src e removo o real; a UI decide se as carrega.
    if (node.tagName === "IMG") {
      const src = node.getAttribute("src") ?? "";
      if (/^https?:/i.test(src)) {
        node.setAttribute("data-blocked-src", src);
        node.removeAttribute("src");
      } else if (!src.startsWith("cid:") && !src.startsWith("data:")) {
        node.removeAttribute("src"); // esquemas estranhos: fora
      }
    }
  });
}

const CONFIG = {
  // Allow-list: só o que faz sentido num e-mail. Sem <script>, <iframe>, <object>, <form>, <style>
  // com seletores globais, etc. FORBID_TAGS é redundante com a allow-list, mas deixa a intenção clara.
  ALLOWED_TAGS: [
    "a", "b", "strong", "i", "em", "u", "s", "strike", "sub", "sup", "br", "hr", "p", "div", "span",
    "blockquote", "pre", "code", "ul", "ol", "li", "dl", "dt", "dd",
    "table", "thead", "tbody", "tfoot", "tr", "td", "th", "caption", "colgroup", "col",
    "h1", "h2", "h3", "h4", "h5", "h6", "img", "figure", "figcaption", "font", "center", "small",
  ],
  ALLOWED_ATTR: [
    "href", "title", "alt", "src", "width", "height", "align", "valign", "border", "cellpadding",
    "cellspacing", "colspan", "rowspan", "bgcolor", "color", "face", "size", "dir", "style", "target", "rel",
  ],
  FORBID_TAGS: ["script", "iframe", "object", "embed", "form", "input", "button", "style", "link", "meta", "base"],
  FORBID_ATTR: ["srcset", "onerror", "onload", "onclick", "formaction"],
  ALLOW_DATA_ATTR: false,
  ADD_ATTR: ["target", "data-blocked-src"],
  // Sem javascript:/vbscript:/data:text-html em href — DOMPurify já bloqueia, mas explícito é melhor.
  ALLOWED_URI_REGEXP: /^(?:(?:https?|mailto|tel|cid):|[^a-z]|[a-z+.-]+(?:[^a-z+.\-:]|$))/i,
};

// sanitize devolve o HTML seguro e quantas imagens externas foram bloqueadas.
export function sanitize(html: string): SanitizedMail {
  installHook();
  const clean = DOMPurify.sanitize(html, CONFIG as Parameters<typeof DOMPurify.sanitize>[1]) as unknown as string;
  const blockedImages = (clean.match(/data-blocked-src=/g) ?? []).length;
  return { html: clean, blockedImages };
}

// sanitizeRich limpa HTML de conteúdo colaborativo interno antes de o injetar via innerHTML (canvas
// do chat, assinaturas). O autor é um colega, não um remetente anónimo, então mantemos formatação e
// imagens/links inline — mas NUNCA confiamos em HTML de terceiros: DOMPurify remove <script>, SVG
// hostil e handlers de evento (onerror/onload/…), que é o vetor real de stored XSS aqui.
export function sanitizeRich(html: string): string {
  return DOMPurify.sanitize(html, {
    FORBID_TAGS: ["script", "iframe", "object", "embed", "form", "input", "button", "style", "link", "meta", "base"],
    FORBID_ATTR: ["onerror", "onload", "onclick", "onmouseover", "onfocus", "formaction", "srcset"],
    ADD_ATTR: ["target"],
  }) as unknown as string;
}

// unblockImages devolve o mesmo HTML com as imagens externas restauradas — chamado só quando a
// pessoa clica em "mostrar imagens", uma decisão consciente de aceitar o rastreio daquele e-mail.
export function unblockImages(html: string): string {
  return html.replace(/data-blocked-src="([^"]*)"/g, (_m, src) => `src="${src}"`);
}
