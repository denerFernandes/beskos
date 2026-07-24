# Beskos — app desktop e mobile (Tauri 2)

Empacota o mesmo frontend React num app nativo de **desktop** (macOS/Windows/Linux) e **mobile**
(Android/iOS) via [Tauri 2](https://tauri.app). Não duplica código: é o `frontend/` a correr numa
WebView nativa.

## Pré-requisitos (uma vez)
- **Rust**: https://rustup.rs
- **Tauri CLI**: já está nos devDependencies (`npm i` na `frontend/`).
- Dependências de sistema do Tauri (WebKit no Linux, Xcode no macOS/iOS, Android Studio + NDK no Android).

## Ícones (obrigatório antes do 1º build)
A pasta `icons/` está vazia. Gere os ícones a partir de um PNG quadrado (≥512px):
```
cd frontend
npm run tauri icon caminho/para/logo.png
```

## Base da API (IMPORTANTE)
Na web, `/api` e `/jmap` funcionam por proxy (Vite/Caddy, mesma origem). No app **empacotado** não há
proxy — o frontend carrega de um protocolo interno. Por isso as chamadas usam `VITE_API_BASE`
(ver `frontend/src/apiBase.ts`): defina-a no build do app com a URL pública do servidor, ex.:
```
VITE_API_BASE=https://app.suaempresa.com npm run tauri build
```
Sem `VITE_API_BASE` as chamadas ficam relativas (só funcionam na web/dev com proxy).

Aponte `VITE_API_BASE` ao **backend Go** (control plane), não ao Stalwart. O backend serve:
- `/api/*` — a nossa API (sessão por token: no app, o login guarda o token e envia-o em
  `Authorization: Bearer`, já que o cookie cross-site não chega).
- `/jmap/*`, `/auth/*`, `/.well-known/*` — **proxy transparente para o Stalwart** (o webmail fala
  JMAP com a Basic auth do próprio utilizador, que passa direto; o backend nunca vê a password).

Este proxy JMAP existe porque o Stalwart **não envia CORS** e o app é cross-origin. Ligue-o no
backend com `APP_ORIGINS` (e `COOKIE_SECURE=true` em produção HTTPS):
```
APP_ORIGINS=tauri://localhost,https://tauri.localhost
COOKIE_SECURE=true
```
Sem `APP_ORIGINS` o proxy e o CORS ficam desligados (a web usa o Caddy/Vite same-origin).

## Correr / empacotar
```
cd frontend
npm run tauri dev            # desktop, aponta ao Vite (http://localhost:5173)
npm run tauri build          # instaladores desktop

# Mobile (uma vez para inicializar cada plataforma):
npm run tauri android init
npm run tauri ios init
npm run tauri android dev    # / ios dev
```

## Build de PRODUÇÃO — checklist

1. **Variáveis de build** (públicas, não-secretas — as únicas `VITE_` permitidas):
   ```
   VITE_API_BASE=https://app.suaempresa.com \
   VITE_PUBLIC_BASE=https://app.suaempresa.com \
   npm run tauri build
   ```
   `VITE_PUBLIC_BASE` é a origem usada nos **links partilháveis** gerados no app (convite de
   reunião `/j/<sala>`, links em e-mails, partilhas) — no desktop a origem local é `tauri://…`,
   inútil para terceiros. Sem ela, cai em `VITE_API_BASE`.

2. **CSP** (`tauri.conf.json` → `app.security.csp`). A comitada é estrita e cobre API/LiveKit em
   `https:`/`wss:`, mas o **OnlyOffice precisa de ser acrescentado explicitamente** (o editor
   carrega o `api.js` do Document Server), senão os ficheiros Office não abrem no app:
   - `script-src 'self' 'wasm-unsafe-eval' https://docs.suaempresa.com`
   - `frame-src 'self' data: blob: https://docs.suaempresa.com`
   Opcionalmente, aperte `connect-src` para os hosts exatos (API, LiveKit `wss://`, OnlyOffice)
   em vez de `https: wss:` genéricos.
   Nota: builds de teste locais injetam origens `localhost` na CSP temporariamente (os scripts desta
   sessão revertem-na); a CSP comitada deve permanecer a de produção.

3. **LiveKit em produção** tem de ser servido por **`wss://`** (TLS). Um `ws://` para host
   não-localhost é bloqueado como conteúdo misto pelo webview.

4. **Assinatura/notarização** (macOS): sem Developer ID, o Gatekeeper avisa em outras máquinas e o
   Notification Center pode registar a app de forma instável entre reinstalações.

## Segurança — nenhum segredo no pacote

O pacote desktop é distribuível e inspecionável, por isso **não pode conter segredos**. Garantias:

- **Todos os segredos vivem só no backend Go** (chaves de IA, admin do Stalwart, base de dados,
  chave VAPID privada, JWT do OnlyOffice/LiveKit…). O frontend nunca os recebe.
- **O login usa a credencial do próprio utilizador** (Basic auth JMAP contra o Stalwart), digitada em
  runtime — não há credencial embutida.
- **Vars de ambiente**: o Vite injeta em texto claro qualquer `VITE_*`. Só `VITE_API_BASE` (uma URL
  pública, não-secreta) é permitida — o `vite.config.ts` **falha o build** se aparecer outra `VITE_*`.
- **Sem sourcemaps** no build (`build.sourcemap: false`) — o código-fonte não vai no pacote.
- **CSP restritiva** (`tauri.conf.json`): sem scripts inline, sem eval (exceto WASM), e `connect-src`
  limitado a `https:`/`wss:`. Recomendado apertar `connect-src` para o host exato do backend em
  produção (ex.: `connect-src 'self' https://app.suaempresa.com wss://app.suaempresa.com`).
- **Auditoria**: `dist/` foi verificado — 0 valores de segredos, 0 sourcemaps.

Regra de ouro: se um dado é sensível, ele **não entra no frontend** — passa a ser um endpoint no
backend que o serve/usa mediante a sessão autenticada.

Nota (runtime, não pacote): o token de sessão do utilizador é guardado no `localStorage` do webview.
No desktop, considere migrar para o **keychain do SO** (plugin `tauri-plugin-stronghold` ou
`keyring`) para não deixar o token em disco em claro — melhoria de hardening, separada do "sem
segredos no pacote".

## Estrutura
- `tauri.conf.json` — config (janela, bundle, devUrl=Vite, frontendDist=../dist).
- `Cargo.toml` / `build.rs` / `src/` — o "shell" Rust (partilhado desktop+mobile via `lib.rs`).
- `capabilities/default.json` — permissões da janela (modelo de segurança do Tauri 2).
