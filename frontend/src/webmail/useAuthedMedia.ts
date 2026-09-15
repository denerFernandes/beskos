import { useEffect, useState } from "react";
import { API_BASE, apiUrl, authHeaders } from "../apiBase";

// useAuthedMedia returns a src usable by <img>/<iframe>/<video>/<audio> from a path on our API
// that requires an authenticated session.
//
// - Na web (same-origin): usa a URL direta — o cookie autentica e o vídeo/áudio mantém streaming.
// - No app nativo (cross-origin, API_BASE definido): <img>/<iframe> não enviam header nem cookie, e a
//   origem da API não está no img-src/frame-src da CSP. Então buscamos os bytes com Authorization e
//   servimos um blob: URL (permitido pela CSP em qualquer diretiva). Revoga ao desmontar/trocar.
//
// Devolve "" enquanto o blob não está pronto (o chamador mostra um loading).
export function useAuthedMedia(path: string): string {
  const [url, setUrl] = useState<string>(() => (API_BASE ? "" : apiUrl(path)));

  useEffect(() => {
    if (!API_BASE) { setUrl(apiUrl(path)); return; }
    let cancelled = false;
    let objUrl = "";
    setUrl("");
    fetch(apiUrl(path), { headers: authHeaders(), credentials: "include" })
      .then((r) => { if (!r.ok) throw new Error(String(r.status)); return r.blob(); })
      .then((b) => { if (cancelled) return; objUrl = URL.createObjectURL(b); setUrl(objUrl); })
      .catch(() => { /* o chamador trata o vazio como erro/loading */ });
    return () => { cancelled = true; if (objUrl) URL.revokeObjectURL(objUrl); };
  }, [path]);

  return url;
}
