import { useEffect, useRef } from "react";
import type { MailService } from "../jmap/mail";

// Mantém uma conexão de push JMAP (SSE) viva enquanto o componente está montado, reconectando
// com backoff se cair. Chama onState() a cada mudança de estado da conta (novo e-mail, leitura…).
// O polling do TanStack Query continua como rede de segurança caso o proxy não segure o SSE.
export function useJmapPush(service: MailService, onState: () => void): void {
  const cb = useRef(onState);
  cb.current = onState;

  useEffect(() => {
    const ctrl = new AbortController();
    let stopped = false;
    let delay = 2000;

    (async () => {
      while (!stopped) {
        try {
          await service.streamPush(() => cb.current(), ctrl.signal);
          delay = 2000; // encerrou limpo → reconecta rápido
        } catch {
          if (ctrl.signal.aborted) return;
          delay = Math.min(delay * 2, 30000); // erro → backoff até 30s
        }
        if (stopped) return;
        await new Promise((r) => setTimeout(r, delay));
      }
    })();

    return () => { stopped = true; ctrl.abort(); };
  }, [service]);
}
