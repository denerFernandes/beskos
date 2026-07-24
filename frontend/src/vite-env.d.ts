// Import de asset com sufixo ?url (Vite): devolve a URL final do ficheiro empacotado. Usado pelo
// worker do pdf.js (pdfjs.ts). Sem depender de vite/client completo.
declare module "*?url" {
  const src: string;
  export default src;
}
