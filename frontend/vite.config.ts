import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// Reverse proxy same-origin (obrigatório — o Stalwart não envia headers CORS, ver DIVERGENCES §D7).
// TUDO passa pela NOSSA API Go (:8000): /api são os handlers do control plane; /jmap, /auth,
// /.well-known caem no proxy JMAP com BROKERING (H11) — a API autentica pela sessão e injeta a
// credencial da caixa server-to-server, então o browser nunca segura a senha (ver jmap_proxy.go).
const API = process.env.API_URL ?? "http://localhost:8000";

// Apontar o proxy à API DE PRODUÇÃO para trabalhar no frontend sem levantar o backend:
//     API_URL=https://app.beskos.com npm run dev
//
// changeOrigin é obrigatório quando o destino é remoto: sem ele o pedido chega com
// `Host: localhost:5173` e o nginx de produção, que encaminha por nome, não sabe a quem entregar.
// Contra um backend local não faz diferença nenhuma.
//
// O cookie de sessão de produção é Secure+SameSite=None. Funciona porque os browsers tratam
// `localhost` como contexto seguro mesmo em http — e é por isso que isto tem de correr em
// localhost e não num IP da rede local, onde o cookie seria recusado em silêncio.
const REMOTO = /^https:/.test(API);

// Duas variáveis parecidas, efeitos opostos — e é fácil trocá-las:
//   API_URL        destino do PROXY. O browser continua a falar com localhost (mesma origem).
//   VITE_API_BASE  base ABSOLUTA embutida no pacote. O browser passa a ir direto ao endereço,
//                  o proxy deixa de ser usado, e as chamadas ficam cross-site.
// Em desenvolvimento web quer-se a primeira. A segunda só faz sentido no pacote Tauri, que não
// tem proxy nenhum. Trocá-las dá erros de certificado ou de CORS que não apontam para a causa.
if (process.env.VITE_API_BASE && !process.env.TAURI_ENV_PLATFORM) {
  console.warn(
    `\n[aviso] VITE_API_BASE=${process.env.VITE_API_BASE} está definida.\n` +
    `        As chamadas vão DIRETAS a esse endereço e o proxy não é usado.\n` +
    `        Para trabalhar contra outro backend em desenvolvimento, use antes:\n` +
    `            API_URL=${process.env.VITE_API_BASE} npm run dev\n`,
  );
}

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
      "/api": { target: API, ws: true, changeOrigin: REMOTO },
      "/jmap": { target: API, ws: true, changeOrigin: REMOTO }, // ws para o eventsource SSE do JMAP
      "/auth": { target: API, changeOrigin: REMOTO },
      "/.well-known": { target: API, changeOrigin: REMOTO },
    },
  },
  build: {
    // Sem sourcemaps no pacote distribuído — não expor o código-fonte completo do app.
    sourcemap: false,
  },
});
