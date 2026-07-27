// Setup do pdf.js. O worker é empacotado pelo Vite (?url) — funciona igual na web e no app nativo
// (Tauri), sem depender de CDN. worker-src 'self' blob: na CSP cobre-o.
import * as pdfjsLib from "pdfjs-dist";
import workerUrl from "pdfjs-dist/build/pdf.worker.min.mjs?url";

pdfjsLib.GlobalWorkerOptions.workerSrc = workerUrl;

export type PDFDocument = Awaited<ReturnType<typeof pdfjsLib.getDocument>["promise"]>;

export async function loadPdf(url: string): Promise<PDFDocument> {
  // Os nossos endpoints de PDF (apresentação do Meet, etc.) servem o ficheiro INTEIRO e NÃO suportam
  // Range. Para PDFs grandes o pdf.js tenta range requests (quando o proxy anuncia Accept-Ranges/
  // Content-Length) e recebe 200 completo em vez de 206 → falha a carregar. Pequenos baixam de uma
  // vez e funcionam — daí "PPT (convertido, pequeno) abre, PDF grande não". Forçar o download
  // completo (disableRange/disableStream) torna-o robusto para qualquer tamanho.
  return pdfjsLib.getDocument({ url, disableRange: true, disableStream: true }).promise;
}
