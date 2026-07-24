import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useQuery } from "@tanstack/react-query";
import { BlockNoteSchema, defaultInlineContentSpecs, filterSuggestionItems, PartialBlock } from "@blocknote/core";
import * as locales from "@blocknote/core/locales";
import { BlockNoteView } from "@blocknote/mantine";
import {
  useCreateBlockNote, SuggestionMenuController, getDefaultReactSlashMenuItems,
  createReactInlineContentSpec, type DefaultReactSuggestionItem,
} from "@blocknote/react";
import * as Y from "@y/y";
import "@blocknote/core/fonts/inter.css";
import "@blocknote/mantine/style.css";

import { backend, colleaguesApi } from "./backend";
import { API_BASE } from "../apiBase";
import { toast } from "./toast";
import { WikiCollabProvider } from "./WikiCollabProvider";
import { IcSparkle } from "../panel/icons";

// WikiEditor é o editor rico da wiki — BlockNote (core MPL-2.0; B2). body_json é o ARRAY de blocos
// (fonte da verdade); o backend deriva body_text/body_html. A IA entra por itens custom do slash-menu
// (nosso /api/ai/wiki-write — nunca o pacote AGPL xl-ai). Com pageId, liga a COLABORAÇÃO em tempo
// real (B10): Y.Doc + relay Go; peers===0 no hello → semeia do body_json; senão sincroniza dos pares.

function textToBlocks(text: string): PartialBlock[] {
  const paras = text.split(/\n+/).map((p) => p.trim()).filter(Boolean);
  if (paras.length === 0) return [{ type: "paragraph" }];
  return paras.map((p) => ({ type: "paragraph", content: p }));
}

// Cor estável por nome para o cursor colaborativo.
const CURSOR_COLORS = ["#e07a5f", "#3d84a8", "#81b29a", "#f2a154", "#9b5de5", "#00afb9", "#ef476f", "#118ab2"];
function colorFor(name: string): string {
  let h = 0;
  for (const c of name) h = (h * 31 + c.charCodeAt(0)) | 0;
  return CURSOR_COLORS[Math.abs(h) % CURSOR_COLORS.length];
}

// Menções @pessoa e links [[página]] (pendência do B2): inline content custom serializado como
// {type:"mention"|"pageLink", props:{id,label}} — o render_blocknote.go trata ambos com escape.
const MentionSpec = createReactInlineContentSpec(
  { type: "mention", propSchema: { id: { default: "" }, label: { default: "" } }, content: "none" },
  { render: (p) => <span className="wiki-mention">@{p.inlineContent.props.label}</span> },
);
const PageLinkSpec = createReactInlineContentSpec(
  { type: "pageLink", propSchema: { id: { default: "" }, label: { default: "" } }, content: "none" },
  { render: (p) => <span className="wiki-pagelink">{p.inlineContent.props.label}</span> },
);
const wikiSchema = BlockNoteSchema.create({
  inlineContentSpecs: { ...defaultInlineContentSpecs, mention: MentionSpec, pageLink: PageLinkSpec },
});

export function WikiEditor({ value, bodyText, onChange, pageId, pages }: {
  value: unknown;                 // body_json atual (array BlockNote, ou legado)
  bodyText?: string;              // texto puro, para semear/migrar conteúdo legado
  onChange: (json: unknown) => void;
  pageId?: string;                // com pageId liga a colaboração em tempo real (B10)
  pages?: { id: string; label: string }[]; // páginas do espaço para o picker de [[página]]
}) {
  const { t, i18n } = useTranslation();
  const me = useQuery({ queryKey: ["me"], queryFn: () => backend.me(), retry: false, staleTime: 60_000 });
  const colleagues = useQuery({ queryKey: ["colleagues"], queryFn: () => colleaguesApi.list(), retry: false, staleTime: 60_000 });

  // Doc + provider criados UMA vez por montagem (o WikiPagePane monta o editor keyed por página).
  const [collab] = useState(() => {
    if (!pageId) return null;
    const doc = new Y.Doc();
    return { doc, provider: new WikiCollabProvider(pageId, doc) };
  });
  useEffect(() => {
    // StrictMode (dev) desmonta e remonta: o provider é partilhado pelo estado, por isso revive-se
    // no mount em vez de criar outro — e destrói-se no cleanup real.
    collab?.provider.ensureConnected();
    return () => collab?.provider.scheduleDestroy();
  }, [collab]);

  const seedBlocks = (): PartialBlock[] => {
    let v: unknown = value;
    // Defesa: respostas antigas (pré-fix) traziam body_json como string base64/JSON — tenta parse.
    if (typeof v === "string") {
      const raw: string = v;
      try { v = JSON.parse(raw); } catch { try { v = JSON.parse(atob(raw)); } catch { v = null; } }
    }
    if (Array.isArray(v) && v.length > 0) return v as PartialBlock[];
    return textToBlocks(bodyText ?? "");
  };

  const dictionary = i18n.language.toLowerCase().startsWith("pt") ? locales.pt : locales.en;
  // Imagens/ficheiros inseridos na página: upload para o DRIVE + link público do ficheiro (o leitor
  // da página — incluindo a rota pública — precisa de um URL alcançável). O aviso de que estes links
  // são públicos vive no toggle de acesso público (WikiShareDrawer).
  const uploadFile = async (file: File): Promise<string> => {
    const sf = await backend.uploadFile(file);
    const link = await backend.fileLink(sf.id, { allow_download: true });
    return `${API_BASE || ""}/api/public/files/${link.token}/download?inline=1`;
  };
  const editor = useCreateBlockNote(
    collab
      ? {
          schema: wikiSchema,
          dictionary,
          uploadFile,
          collaboration: {
            provider: collab.provider,
            fragment: collab.doc.get("document-store") as never, // v14 RC: get(nome) devolve o tipo partilhado
            user: { name: "…", color: CURSOR_COLORS[0] },
          },
        }
      : { schema: wikiSchema, dictionary, uploadFile, initialContent: seedBlocks() },
  );

  // Regra de seed (B10): só o PRIMEIRO na sala carrega o body_json; os restantes sincronizam.
  // Rede de segurança: se o hello não chegar (WS bloqueado/offline), semeia na mesma após um
  // instante — editar sozinho nunca pode abrir em branco; o 409 otimista protege o resto.
  const seeded = useRef(false);
  useEffect(() => {
    if (!collab) return;
    const docEmpty = () => editor.document.length === 0 ||
      (editor.document.length === 1 && !(editor.document[0] as { content?: unknown[] }).content?.length);
    const seed = () => {
      if (seeded.current) return;
      seeded.current = true;
      if (docEmpty()) editor.replaceBlocks(editor.document, seedBlocks());
    };
    collab.provider.onHello = (peers) => {
      if (peers === 0) seed();
      // peers>0: o conteúdo vem do sync dos pares; o fallback abaixo cobre ligações fantasma.
    };
    // 600ms: o hello local chega em ~10ms; se o WS estiver em baixo, o conteúdo não pode esperar.
    const fallback = setTimeout(() => { if (docEmpty()) seed(); }, 600);
    return () => clearTimeout(fallback);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [collab, editor]);

  // Nome/cor reais do cursor chegam depois (query me): atualiza via awareness (o BlockNote lê de lá).
  useEffect(() => {
    if (!collab || !me.data) return;
    const name = me.data.name || me.data.email || "?";
    collab.provider.awareness.setLocalStateField("user", { name, color: colorFor(name) });
  }, [collab, me.data]);

  async function runAI(instruction: string) {
    toast(t("wiki.ai.working"));
    try {
      const context = await editor.blocksToMarkdownLossy(editor.document);
      const res = await backend.aiWikiWrite(instruction, context);
      const blocks = await editor.tryParseMarkdownToBlocks(res.content);
      editor.insertBlocks(blocks, editor.getTextCursorPosition().block, "after");
    } catch (e) {
      toast((e as Error).message);
    }
  }

  const aiItems: DefaultReactSuggestionItem[] = [
    {
      title: t("wiki.ai.continue"), subtext: t("wiki.ai.continueHint"), group: t("wiki.ai.group"),
      icon: <IcSparkle size={18} />, aliases: ["ai", "ia", "continuar"],
      onItemClick: () => void runAI("Continue writing this wiki page from where it stops, keeping the same tone and structure."),
    },
    {
      title: t("wiki.ai.improve"), subtext: t("wiki.ai.improveHint"), group: t("wiki.ai.group"),
      icon: <IcSparkle size={18} />, aliases: ["ai", "ia", "melhorar"],
      onItemClick: () => void runAI("Rewrite the page content improving clarity and structure, keeping the meaning. Return the full improved content."),
    },
    {
      title: t("wiki.ai.summarize"), subtext: t("wiki.ai.summarizeHint"), group: t("wiki.ai.group"),
      icon: <IcSparkle size={18} />, aliases: ["ai", "ia", "resumo"],
      onItemClick: () => void runAI("Write a concise summary (a few bullet points) of this page."),
    },
  ];

  const mentionItems = (): DefaultReactSuggestionItem[] =>
    (colleagues.data ?? []).map((c) => ({
      title: c.name || c.email, subtext: c.email,
      onItemClick: () => editor.insertInlineContent([{ type: "mention", props: { id: c.id, label: c.name || c.email } }, " "]),
    }));
  const pageItems = (): DefaultReactSuggestionItem[] =>
    (pages ?? []).map((pg) => ({
      title: pg.label,
      onItemClick: () => editor.insertInlineContent([{ type: "pageLink", props: { id: pg.id, label: pg.label } }, " "]),
    }));

  const theme = document.documentElement.getAttribute("data-theme") === "dark" ? "dark" : "light";

  return (
    <div className="wiki-editor">
      <BlockNoteView editor={editor} theme={theme} slashMenu={false}
        onChange={() => onChange(editor.document)}>
        <SuggestionMenuController triggerCharacter="/"
          getItems={async (query) =>
            filterSuggestionItems([...getDefaultReactSlashMenuItems(editor), ...aiItems], query)} />
        <SuggestionMenuController triggerCharacter="@"
          getItems={async (query) => filterSuggestionItems(mentionItems(), query)} />
        <SuggestionMenuController triggerCharacter="["
          getItems={async (query) => filterSuggestionItems(pageItems(), query)} />
      </BlockNoteView>
    </div>
  );
}
