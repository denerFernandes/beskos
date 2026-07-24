// Setup do pdf.js. O worker é empacotado pelo Vite (?url) — funciona igual na web e no app nativo
// (Tauri), sem depender de CDN. worker-src 'self' blob: na CSP cobre-o.
import * as pdfjsLib from "pdfjs-dist";
import workerUrl from "pdfjs-dist/build/pdf.worker.min.mjs?url";

pdfjsLib.GlobalWorkerOptions.workerSrc = workerUrl;

export type PDFDocument = Awaited<ReturnType<typeof pdfjsLib.getDocument>["promise"]>;

export async function loadPdf(url: string): Promise<PDFDocument> {
  return pdfjsLib.getDocument({ url }).promise;
}
