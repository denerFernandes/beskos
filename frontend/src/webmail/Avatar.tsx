import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";

import { colleaguesApi } from "./backend";
import { hueFor, initials } from "./util";

// Avatar partilhado (24/07/2026): mostra a FOTO do utilizador onde quer que o nome apareça (topo,
// chat, kanban, meet, correio…). A foto vem explicitamente por prop `src`, ou é resolvida por
// e-mail/id no DIRETÓRIO de colegas (que já traz a foto) — assim qualquer sítio que mostre um
// colega ganha a foto sem alterar cada payload. Sem foto, cai no círculo com iniciais (hueFor).

// useAvatarDir: mapa e-mail→foto e id→foto, a partir dos colegas do tenant (query cacheada).
function useAvatarDir() {
  const q = useQuery({ queryKey: ["colleagues"], queryFn: () => colleaguesApi.list(), retry: false, staleTime: 300000 });
  return useMemo(() => {
    const byEmail = new Map<string, string>();
    const byId = new Map<string, string>();
    for (const c of q.data ?? []) {
      if (c.avatar) { byEmail.set(c.email.toLowerCase(), c.avatar); byId.set(c.id, c.avatar); }
    }
    return { byEmail, byId };
  }, [q.data]);
}

export function Avatar({ name, email, userId, src, size = 28, radius = "50%", title, className, onClick }: {
  name?: string; email?: string; userId?: string; src?: string; size?: number; radius?: string; title?: string; className?: string; onClick?: () => void;
}) {
  const dir = useAvatarDir();
  const photo = src || (email ? dir.byEmail.get(email.toLowerCase()) : undefined) || (userId ? dir.byId.get(userId) : undefined);
  const key = email || userId || name || "?";
  const tip = title ?? (name && email ? `${name} (${email})` : name || email);
  const cls = "av" + (className ? " " + className : "");
  const box = { width: size, height: size, borderRadius: radius, flex: "none" as const, cursor: onClick ? "pointer" : undefined };
  if (photo) {
    return <img className={cls} src={photo} alt="" title={tip} onClick={onClick} style={{ ...box, objectFit: "cover" }} />;
  }
  return (
    <span className={cls} title={tip} onClick={onClick}
      style={{ ...box, background: hueFor(key), color: "#fff", display: "inline-grid", placeItems: "center", fontSize: Math.round(size * 0.4), fontWeight: 650 }}>
      {initials(name || email || "?")}
    </span>
  );
}
