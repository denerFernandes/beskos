import { useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import type { TFunction } from "i18next";
import type { MailService } from "../jmap/mail";
import type { EmailFull } from "../jmap/types";
import { sanitize, unblockImages } from "./mailHtml";
import { IcMail } from "../panel/icons";

// Corpo do e-mail. HTML quando existe (sanitizado), senão texto simples.
//
// O HTML corre num IFRAME SANDBOX: mesmo depois de sanitizado, o CSS de um e-mail sobreporia o da
// app, e o sandbox garante que nada dele — nem um script que escapasse — toca no resto da página.
// O sandbox NÃO inclui allow-scripts, então NENHUM script do e-mail corre. Inclui allow-same-origin
// (apenas) para o parent poder medir a altura do conteúdo e ajustar o iframe (sem ele o
// contentDocument é null). Sem allow-scripts, same-origin não concede acesso executável — é seguro.
export function MailBody({ m, service, textBody, spam }: { m: EmailFull; service: MailService; textBody: string; spam?: boolean }) {
  const { t } = useTranslation();
  // Só é HTML de verdade se a parte for text/html. Num e-mail só-texto, o Stalwart aponta o
  // htmlBody para a parte text/plain — aí renderiza-se como texto (sem iframe, integra-se no tema).
  const htmlMeta = m.htmlBody?.[0];
  const rawHtml = htmlMeta?.type === "text/html" ? m.bodyValues[htmlMeta.partId]?.value : undefined;

  if (!rawHtml) return <div className="rbody">{textBody}</div>;
  return <HtmlBody html={rawHtml} m={m} service={service} t={t} spam={!!spam} />;
}

// Tema ativo: o data-theme no <html> (definido pelo toggle) manda; senão, a preferência do SO.
function isDarkTheme(): boolean {
  const attr = document.documentElement.getAttribute("data-theme");
  if (attr === "dark") return true;
  if (attr === "light") return false;
  return window.matchMedia("(prefers-color-scheme: dark)").matches;
}

function HtmlBody({ html, m, service, t, spam }: {
  html: string; m: EmailFull; service: MailService; t: TFunction; spam: boolean;
}) {
  const [showImages, setShowImages] = useState(false);
  const [height, setHeight] = useState(120);
  const [cidMap, setCidMap] = useState<Record<string, string>>({});
  const ref = useRef<HTMLIFrameElement>(null);
  const dark = isDarkTheme();

  const { html: clean, blockedImages } = useMemo(() => sanitize(html), [html]);

  // Imagens embutidas (cid:) são anexos do próprio e-mail — não são rastreadores, então carregam
  // sempre. Resolve cada cid: para um blob: autenticado.
  useEffect(() => {
    if (spam) return; // em spam, nem as imagens embutidas (cid) carregam
    const cids = m.attachments?.filter((a) => a.cid) ?? [];
    if (!cids.length) return;
    let alive = true;
    const urls: string[] = [];
    (async () => {
      const map: Record<string, string> = {};
      for (const a of cids) {
        try {
          const url = await service.attachmentObjectUrl(a.blobId, a.name ?? "img", a.type);
          map[`cid:${a.cid}`] = url; urls.push(url);
        } catch { /* anexo cid partido: fica sem imagem, não quebra o resto */ }
      }
      if (alive) setCidMap(map);
    })();
    return () => { alive = false; urls.forEach((u) => URL.revokeObjectURL(u)); };
  }, [m, service]);

  // Monta o documento do iframe: HTML limpo, com imagens externas restauradas só se pedido e os
  // cid: resolvidos. base target=_blank para links sem target explícito abrirem fora.
  const doc = useMemo(() => {
    // Em spam nunca desbloqueia imagens; os cid também não são resolvidos (cidMap fica vazio).
    let body = (showImages && !spam) ? unblockImages(clean) : clean;
    for (const [cid, url] of Object.entries(cidMap)) body = body.split(cid).join(url);
    // Em spam, desativa os links (pointer-events:none): não se clica em nada de um remetente suspeito.
    const linkCss = spam ? "a{color:#8a8a8a;pointer-events:none;cursor:default;text-decoration:underline}" : "a{color:#116466}";
    // Modo escuro: inverte o e-mail (fundo claro→escuro, texto escuro→claro) com hue-rotate para
    // preservar as matizes, e RE-inverte imagens/vídeos/logos para não ficarem em negativo. É a
    // técnica do "Dark Reader" — não é perfeita em e-mails muito ricos, mas integra bem no tema.
    // A inversão do dark vai num WRAPPER interno (#mroot), NÃO no body: filtrar o body quebra a
    // medição de altura (scrollHeight fica errado → iframe baixo com scroll). No wrapper, o body
    // mede a altura corretamente. As imagens são re-invertidas para não ficarem em negativo.
    // Fundo do iframe TRANSPARENTE: deixa passar o preto do painel de leitura (--paper), para o
    // e-mail se fundir com a página em vez de somar um tom próprio (o painel, o iframe e o conteúdo
    // eram 3 pretos diferentes). O conteúdo vive no #mroot (invertido); só o fundo exterior é que
    // fica transparente.
    // invert(0.93), não invert(1): a inversão total leva o branco do e-mail a #000 puro — mais
    // escuro que o preto do site (--paper #121313), o que deixa o e-mail como um cartão destacado.
    // A 0.93, o branco mapeia para ~#121313 (funde-se com a página) e o texto continua claro. As
    // imagens/logos são re-invertidos para não ficarem em negativo (contraste ligeiramente mais
    // suave, aceitável num modo escuro).
    const themeCss = dark
      ? `html{color-scheme:dark}html,body{background:transparent}` +
        `#mroot{background:#fff;filter:invert(0.93) hue-rotate(180deg)}` +
        `#mroot img,#mroot picture,#mroot video,#mroot svg,#mroot [style*="background-image"]{filter:invert(1) hue-rotate(180deg)}`
      : // Claro: fundo TRANSPARENTE também — o e-mail funde-se com o --paper da página em vez
        // de aparecer como um cartão branco destacado (feedback 23/07/2026). E-mails ricos que
        // definem o próprio fundo continuam a mostrá-lo (só o exterior é transparente).
        `html{color-scheme:light}html,body{background:transparent}`;
    // #mroot em flow-root: cria um contexto de formatação que INCLUI as margens dos filhos na sua
    // altura (senão a margem do último elemento "atravessa" e a medição fica curta → scroll).
    return `<!doctype html><html><head><meta charset="utf-8"><base target="_blank">` +
      `<style>${themeCss}html,body{margin:0;padding:0;color:#1c1b1a;` +
      `font:14px/1.5 -apple-system,system-ui,sans-serif;word-wrap:break-word;overflow-wrap:break-word}` +
      `#mroot{display:flow-root}img{max-width:100%;height:auto}${linkCss}` +
      `table{max-width:100%}</style>` +
      `</head><body><div id="mroot">${body}</div></body></html>`;
  }, [clean, showImages, cidMap, spam, dark]);

  // Ajusta a altura do iframe ao conteúdo — o e-mail não deve ter scroll interno próprio, já que o
  // leitor rola. Um ResizeObserver DENTRO do iframe acompanha qualquer mudança de altura (imagens a
  // carregar, fontes, layout tardio), que uma medição pontual no onLoad perderia.
  const onLoad = () => {
    const win = ref.current?.contentWindow as (Window & typeof globalThis) | null;
    const d = ref.current?.contentDocument;
    if (!win || !d) return;
    // Mede o WRAPPER #mroot diretamente (getBoundingClientRect): envolve todo o conteúdo e a sua
    // altura é a real, sem o ruído do filtro/scrollHeight do body.
    const measure = () => {
      const root = d.getElementById("mroot");
      const h = Math.max(
        root ? Math.ceil(root.getBoundingClientRect().height) : 0,
        root?.scrollHeight ?? 0,
        d.body?.scrollHeight ?? 0,
        d.documentElement?.scrollHeight ?? 0,
      );
      if (h) setHeight((prev) => (Math.abs(prev - (h + 8)) > 2 ? h + 8 : prev));
    };
    measure();
    const target = d.getElementById("mroot") ?? d.body;
    if (win.ResizeObserver && target) new win.ResizeObserver(measure).observe(target);
    setTimeout(measure, 300);
    setTimeout(measure, 900);
  };

  return (
    <div className="rbody-html">
      {blockedImages > 0 && !showImages && (
        // Rastreadores bloqueados: dizê-lo, e deixar a pessoa aceitar conscientemente.
        // Em spam não há botão para mostrar: fica bloqueado, ponto.
        <div className="img-blocked">
          <IcMail size={14} /> {t(spam ? "webmail.reader.imagesBlockedSpam" : "webmail.reader.imagesBlocked", { n: blockedImages })}
          {!spam && <button onClick={() => setShowImages(true)}>{t("webmail.reader.showImages")}</button>}
        </div>
      )}
      <iframe ref={ref} title="email"
        sandbox="allow-same-origin allow-popups allow-popups-to-escape-sandbox"
        srcDoc={doc} onLoad={onLoad} style={{ height }} className="mail-iframe" />
    </div>
  );
}
