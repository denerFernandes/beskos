import "../webmail/webmail.css";
import { useWebmail } from "../webmail/auth";
import { WebmailLogin } from "../webmail/WebmailLogin";
import { App } from "../webmail/App";

// Rota /mail: app de e-mail do usuário final. Login valida no Stalwart (JMAP) + abre sessão
// na nossa API (IA/Kanban/calendário). Ver DIVERGENCES §D7.
export function MailPage() {
  const { session } = useWebmail();
  return session ? <App session={session} /> : <WebmailLogin />;
}
