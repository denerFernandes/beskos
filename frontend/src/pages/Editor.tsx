import "../webmail/webmail.css";
import { useWebmail } from "../webmail/auth";
import { WebmailLogin } from "../webmail/WebmailLogin";
import { EditorView } from "../webmail/EditorView";
import { ToastHost } from "../webmail/toast";

// Rota /mail/files/edit/:id — editor de office em tela cheia (Collabora/WOPI). Requer sessão
// de webmail; sem ela, cai no login.
export function EditorPage() {
  const { session } = useWebmail();
  if (!session) return <WebmailLogin />;
  return (
    <div className="wm">
      <EditorView />
      <ToastHost />
    </div>
  );
}
