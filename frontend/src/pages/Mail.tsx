import "../webmail/webmail.css";
import { useWebmail } from "../webmail/auth";
import { WebmailLogin } from "../webmail/WebmailLogin";
import { App } from "../webmail/App";

// Route /mail: the end user's email app. Login validates against Stalwart (JMAP) and also opens
// a session with our own API (AI/Kanban/calendar).
export function MailPage() {
  const { session } = useWebmail();
  return session ? <App session={session} /> : <WebmailLogin />;
}
