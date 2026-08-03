import React from "react";
import ReactDOM from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { BrowserRouter, Routes, Route, Navigate } from "react-router-dom";
import "./index.css";
import "./i18n";
import { ResetPage } from "./pages/Reset";
import { MailPage } from "./pages/Mail";
import { EditorPage } from "./pages/Editor";
import { PublicFilePage } from "./pages/PublicFile";
import { PublicWikiPage } from "./pages/PublicWiki";
import { GuestPage } from "./pages/Guest";
import { RoomPage } from "./pages/Room";
import { AcceptInvitePage } from "./pages/AcceptInvite";
import { BookingPage } from "./pages/Booking";
import { SignupPage } from "./pages/Signup";
import { isTauri, initTauriNotify } from "./webmail/tauriNotify";
import { unlockSounds } from "./webmail/sounds";
import { GlobalTooltip } from "./webmail/GlobalTooltip";

const qc = new QueryClient();

// No app nativo (Tauri), lê a permissão de notificações do SO para a cache (a UI lê-a de forma
// síncrona). Na web é no-op.
if (isTauri()) void initTauriNotify();

// Destranca o áudio ao primeiro gesto na app. O browser mantém o AudioContext suspenso até haver
// interação — sem isto, o primeiro aviso da sessão seria engolido em silêncio.
unlockSounds();

// Janela dedicada da sala (Tauri): abre-se em index.html?wpath=/room/:room (o asset server não faz
// fallback SPA). Reescreve a URL para a rota real ANTES do BrowserRouter montar, para renderizar a
// sala em vez do fallback /mail.
const wpath = new URLSearchParams(window.location.search).get("wpath");
if (wpath && wpath.startsWith("/")) {
  // Marca esta como janela dedicada (sala/editor): o botão fechar deve fechar a janela nativa em vez
  // de navegar para o Drive. Ver isDedicatedWindow()/closeSelfWindow() em nativeWindow.ts.
  (window as unknown as { __mailinDedicated?: boolean }).__mailinDedicated = true;
  window.history.replaceState(null, "", wpath);
}

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <QueryClientProvider client={qc}>
      <GlobalTooltip />
      <BrowserRouter>
        <Routes>
          <Route path="/mail/files/edit/:id" element={<EditorPage />} />
          <Route path="/reset" element={<ResetPage />} />
          <Route path="/mail" element={<MailPage />} />
          <Route path="/mail/:view" element={<MailPage />} />
          <Route path="/mail/:view/:sub" element={<MailPage />} />
          <Route path="/mail/:view/:sub/:sub2" element={<MailPage />} />
          {/* Sala em janela dedicada: só a chamada, sem os menus do webmail. */}
          <Route path="/room/:room" element={<RoomPage />} />
          <Route path="/j/:room" element={<GuestPage />} />
          {/* Ficheiro partilhado por link público (sem login): abre no OnlyOffice anónimo ou descarrega. */}
          <Route path="/p/:token" element={<PublicFilePage />} />
          {/* Wiki pública só-leitura (sem login): árvore de páginas published + leitor. */}
          <Route path="/w/:slug" element={<PublicWikiPage />} />
          <Route path="/w/:slug/:pageSlug" element={<PublicWikiPage />} />
          {/* Registo self-service (é para aqui que aponta o "Get started" do site). */}
          <Route path="/signup" element={<SignupPage />} />
          <Route path="/invite/:token" element={<AcceptInvitePage />} />
          {/* Agendamento público (estilo Calendly): externos marcam reunião com um colaborador. */}
          <Route path="/b/:org/:user" element={<BookingPage />} />
          {/* Tela única: o webmail. A administração é uma view dele (o /app foi eliminado). */}
          <Route path="*" element={<Navigate to="/mail" replace />} />
        </Routes>
      </BrowserRouter>
    </QueryClientProvider>
  </React.StrictMode>
);
