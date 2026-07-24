import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// Reverse proxy same-origin (obrigatório — o Stalwart não envia headers CORS, ver DIVERGENCES §D7).
// TUDO passa pela NOSSA API Go (:8000): /api são os handlers do control plane; /jmap, /auth,
// /.well-known caem no proxy JMAP com BROKERING (H11) — a API autentica pela sessão e injeta a
// credencial da caixa server-to-server, então o browser nunca segura a senha (ver jmap_proxy.go).
const API = process.env.API_URL ?? "http://localhost:8000";

// SEGURANÇA DO PACOTE (desktop/mobile Tauri): o Vite injeta em TEXTO CLARO no bundle qualquer
// variável de ambiente com prefixo VITE_. Como o app é distribuído (e um utilizador pode inspecionar
// o pacote), NENHUM segredo pode ser VITE_. Só a allow-list abaixo é permitida — o build FALHA se
// aparecer outra VITE_*. Segredos (chaves de IA, admin do Stalwart, DB, VAPID privado) vivem só no
// backend Go e nunca chegam ao frontend.
// VITE_API_BASE: URL do backend (app nativo). VITE_PUBLIC_BASE: origem pública da web para links
// partilháveis gerados no app nativo (convites de reunião etc.). Ambas são URLs públicas, não-secretas.
const ALLOWED_VITE_VARS = ["VITE_API_BASE", "VITE_PUBLIC_BASE"];
const leaked = Object.keys(process.env).filter((k) => k.startsWith("VITE_") && !ALLOWED_VITE_VARS.includes(k));
if (leaked.length) {
  throw new Error(
    `[segurança] Variáveis VITE_ não permitidas seriam embutidas no pacote: ${leaked.join(", ")}. ` +
    `Só ${ALLOWED_VITE_VARS.join(", ")} é permitida (não-secreta). Segredos ficam no backend.`,
  );
}

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      "/api": { target: API, ws: true },
      "/jmap": { target: API, ws: true }, // ws para o eventsource SSE do JMAP
      "/auth": API,
      "/.well-known": API,
    },
  },
  build: {
    // Sem sourcemaps no pacote distribuído — não expor o código-fonte completo do app.
    sourcemap: false,
  },
});
