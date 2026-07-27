// Setup do pdf.js. O worker é empacotado pelo Vite (?url) — funciona igual na web e no app nativo
// (Tauri), sem depender de CDN. worker-src 'self' blob: na CSP cobre-o.
import * as pdfjsLib from "pdfjs-dist";
import workerUrl from "pdfjs-dist/build/pdf.worker.min.mjs?url";

pdfjsLib.GlobalWorkerOptions.workerSrc = workerUrl;

export type PDFDocument = Awaited<ReturnType<typeof pdfjsLib.getDocument>["promise"]>;

// loadPdf abre um PDF servido pela nossa API. Duas tentativas, por esta ordem:
//
//  1. Normal (streaming + Range): o endpoint da apresentação responde 206, então a primeira página
//     aparece depois de uns KB em vez de esperar pelo ficheiro inteiro — decisivo num deck de vários
//     MB, em que os outros participantes ficavam a olhar para um retângulo branco.
//  2. Ficheiro inteiro (disableRange/disableStream): se algo na cadeia (proxy, cache) estragar os
//     intervalos — devolver 200 completo a um pedido de Range, por exemplo — o pdf.js falha; então
//     baixa-se tudo de uma vez, que é o caminho que funciona em qualquer servidor.
export async function loadPdf(url: string): Promise<PDFDocument> {
  try {
    // Blocos de 512 KB (o default são 64 KB): um deck de vários MB deixa de ser ~100 pedidos, o que
    // encaixa no limite por IP do endpoint público sem sacrificar o arranque rápido.
    return await pdfjsLib.getDocument({ url, rangeChunkSize: 512 * 1024 }).promise;
  } catch (e) {
    console.warn("pdf: leitura por intervalos falhou, a descarregar o ficheiro inteiro", e);
    return await pdfjsLib.getDocument({ url, disableRange: true, disableStream: true }).promise;
  }
}
