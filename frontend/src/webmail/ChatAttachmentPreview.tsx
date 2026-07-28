import { useTranslation } from "react-i18next";
import { useQuery } from "@tanstack/react-query";
import { backend, type ChatAttachment } from "./backend";
import { IcTable, IcCode, IcMaximize } from "../panel/icons";

// Pré-visualização de anexos de TEXTO na própria conversa: um CSV vira tabela e um txt/json/log
// mostra as primeiras linhas. Antes eram todos uma linha cinzenta com o nome do ficheiro — para
// ver três colunas de um CSV era preciso descarregá-lo e abrir noutro programa.
//
// Só lemos o INÍCIO do ficheiro (MAX_BYTES): um log de 40 MB não pode travar a conversa. Quem
// quiser o resto abre o visualizador (o botão de expandir) ou descarrega.

const MAX_BYTES = 64 * 1024;
const MAX_ROWS = 12;   // linhas de dados mostradas na tabela (fora o cabeçalho)
const MAX_COLS = 8;
const MAX_LINES = 10;  // linhas mostradas no texto/JSON

export type PreviewKind = "csv" | "text";

// previewKind diz se este anexo tem pré-visualização na conversa (e qual).
export function previewKind(name: string): PreviewKind | null {
  const ext = name.split(".").pop()?.toLowerCase() ?? "";
  if (ext === "csv" || ext === "tsv") return "csv";
  if (["txt", "log", "json", "md", "markdown", "yml", "yaml", "xml", "ini", "toml", "env"].includes(ext)) return "text";
  return null;
}

export function ChatAttachmentPreview({ att, kind, onOpen }: {
  att: ChatAttachment; kind: PreviewKind; onOpen: () => void;
}) {
  const { t } = useTranslation();
  const q = useQuery({
    queryKey: ["chat-att-preview", att.file_id],
    queryFn: () => fetchHead(att.file_id),
    retry: false,
    staleTime: 5 * 60 * 1000,
  });

  const head = (
    <header className="ap-head">
      <span className="ap-ic">{kind === "csv" ? <IcTable size={14} /> : <IcCode size={14} />}</span>
      <span className="ap-name">{att.name}</span>
      <button className="ib sm" title={t("chat.previewOpen")} onClick={onOpen}><IcMaximize size={14} /></button>
    </header>
  );

  if (q.isLoading) return <div className="att-preview">{head}<div className="ap-msg">{t("editor.loading")}</div></div>;
  // Falhar a leitura não é motivo para esconder o anexo: a linha normal (descarregar) continua lá.
  if (q.isError || !q.data) return null;

  return (
    <div className="att-preview">
      {head}
      {kind === "csv"
        ? <CsvTable text={q.data.text} truncated={q.data.truncated} tab={att.name.toLowerCase().endsWith(".tsv")} />
        : <TextHead text={q.data.text} json={att.name.toLowerCase().endsWith(".json")} truncated={q.data.truncated} />}
    </div>
  );
}

async function fetchHead(fileId: string): Promise<{ text: string; truncated: boolean }> {
  // Range: o servidor pode ignorá-lo (devolve 200 com tudo) — por isso cortamos na mesma no cliente.
  const res = await fetch(backend.chatAttachmentUrl(fileId, true), {
    credentials: "same-origin",
    headers: { Range: `bytes=0-${MAX_BYTES - 1}` },
  });
  if (!res.ok && res.status !== 206) throw new Error(`HTTP ${res.status}`);
  const full = await res.text();
  return { text: full.slice(0, MAX_BYTES), truncated: full.length > MAX_BYTES || res.status === 206 };
}

// --- CSV ---

// Divide uma linha de CSV respeitando aspas ("a,b" é UM campo) e aspas duplicadas ("" = ").
function splitRow(line: string, sep: string): string[] {
  const out: string[] = [];
  let cur = "";
  let quoted = false;
  for (let i = 0; i < line.length; i++) {
    const c = line[i];
    if (quoted) {
      if (c === '"') {
        if (line[i + 1] === '"') { cur += '"'; i++; } else quoted = false;
      } else cur += c;
    } else if (c === '"') quoted = true;
    else if (c === sep) { out.push(cur); cur = ""; }
    else cur += c;
  }
  out.push(cur);
  return out;
}

// O separador varia com a região (Excel português grava com ";"). Decide-se pela primeira linha:
// ganha o que aparecer mais vezes fora de aspas.
function guessSep(line: string): string {
  const count = (sep: string) => splitRow(line, sep).length;
  return count(";") > count(",") ? ";" : ",";
}

function CsvTable({ text, truncated, tab }: { text: string; truncated: boolean; tab: boolean }) {
  const { t } = useTranslation();
  const lines = text.split(/\r?\n/).filter((l) => l.trim() !== "");
  if (lines.length === 0) return null;
  // A última linha de um pedaço cortado costuma vir a meio — não vale a pena mostrá-la.
  const usable = truncated && lines.length > 1 ? lines.slice(0, -1) : lines;
  const sep = tab ? "\t" : guessSep(usable[0]);
  const rows = usable.slice(0, MAX_ROWS + 1).map((l) => splitRow(l, sep).slice(0, MAX_COLS));
  const cols = Math.max(...rows.map((r) => r.length));
  const [header, ...body] = rows;
  const hiddenRows = usable.length - 1 - body.length;
  const hiddenCols = splitRow(usable[0], sep).length - cols;

  return (
    <>
      <div className="ap-table-wrap">
        <table className="ap-table">
          <thead>
            <tr>{Array.from({ length: cols }, (_, i) => <th key={i}>{header[i] ?? ""}</th>)}</tr>
          </thead>
          <tbody>
            {body.map((r, ri) => (
              <tr key={ri}>{Array.from({ length: cols }, (_, i) => <td key={i}>{r[i] ?? ""}</td>)}</tr>
            ))}
          </tbody>
        </table>
      </div>
      {(hiddenRows > 0 || hiddenCols > 0 || truncated) && (
        <div className="ap-more">
          {hiddenRows > 0 && t("chat.csvMoreRows", { count: hiddenRows })}
          {hiddenRows > 0 && hiddenCols > 0 ? " · " : ""}
          {hiddenCols > 0 && t("chat.csvMoreCols", { count: hiddenCols })}
          {hiddenRows <= 0 && hiddenCols <= 0 && truncated ? t("chat.previewPartial") : ""}
        </div>
      )}
    </>
  );
}

// --- Texto / JSON ---

function TextHead({ text, json, truncated }: { text: string; json: boolean; truncated: boolean }) {
  const { t } = useTranslation();
  let body = text;
  // JSON válido fica legível (indentado); inválido ou cortado a meio mostra-se como veio.
  if (json) { try { body = JSON.stringify(JSON.parse(text), null, 2); } catch { /* mantém cru */ } }
  const lines = body.split("\n");
  const shown = lines.slice(0, MAX_LINES).join("\n");
  const rest = lines.length - MAX_LINES;
  return (
    <>
      <pre className="ap-text">{shown}</pre>
      {(rest > 0 || truncated) && (
        <div className="ap-more">{rest > 0 ? t("chat.textMoreLines", { count: rest }) : t("chat.previewPartial")}</div>
      )}
    </>
  );
}
