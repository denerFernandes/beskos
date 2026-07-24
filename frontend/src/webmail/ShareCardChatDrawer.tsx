import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useQuery, useMutation } from "@tanstack/react-query";

import { backend, colleaguesApi, ChatChannel } from "./backend";
import { toast } from "./toast";
import { Drawer } from "./Drawer";
import { IcSearch, IcChat, IcUsers } from "../panel/icons";

// ShareCardChatDrawer (Kanban): envia um card para um CANAL do chat ou para uma PESSOA (DM),
// como mensagem com link interno para o card. Canais mais recentes primeiro + pesquisa. Só aparece
// quando a app de Chat está ligada (gate no TaskModal).

export function ShareCardChatDrawer({ boardId, cardId, title, onClose }: {
  boardId: string; cardId: string; title: string; onClose: () => void;
}) {
  const { t } = useTranslation();
  const [q, setQ] = useState("");

  const channelsQ = useQuery({ queryKey: ["chat-channels"], queryFn: () => backend.chatChannels(), retry: false });
  const colleagues = useQuery({ queryKey: ["colleagues"], queryFn: () => colleaguesApi.list(), retry: false });

  // Mensagem: link interno em markdown (o chat já renderiza [texto](url)).
  const body = `📌 [${title}](/mail/kanban/${boardId}/${cardId})`;

  const send = useMutation({
    mutationFn: async (target: { channelId?: string; email?: string }) => {
      let channelId = target.channelId;
      if (!channelId && target.email) channelId = (await backend.chatOpenDM(target.email)).id;
      if (!channelId) throw new Error(t("webmail.kanban.shareFailed"));
      await backend.chatSend(channelId, body);
    },
    onSuccess: () => { toast(t("webmail.kanban.sharedChat")); onClose(); },
    onError: (e) => toast((e as Error).message),
  });

  const term = q.trim().toLowerCase();
  // Recentes primeiro: ordena pelo last_read_at (proxy de atividade) desc; sem data vai para o fim.
  const channels = (channelsQ.data?.channels ?? [])
    .slice()
    .sort((a: ChatChannel, b: ChatChannel) => (b.last_read_at ?? "").localeCompare(a.last_read_at ?? ""))
    .filter((c) => !term || c.name.toLowerCase().includes(term));
  const people = (colleagues.data ?? []).filter((c) => !term || c.name.toLowerCase().includes(term) || c.email.toLowerCase().includes(term));

  return (
    <Drawer title={t("webmail.kanban.shareChat")} onClose={onClose}
      footer={<><div className="grow" /><button className="chipbtn" onClick={onClose}>{t("common.cancel")}</button></>}>
      <label className="dp-search">
        <IcSearch size={14} />
        <input autoFocus value={q} placeholder={t("webmail.kanban.shareSearch")} onChange={(e) => setQ(e.target.value)} />
      </label>

      {channels.length > 0 && <div className="wiki-side-title">{t("webmail.kanban.shareChannels")}</div>}
      <div className="dp-list">
        {channels.map((c) => (
          <button key={c.id} className="dp-item" disabled={send.isPending} onClick={() => send.mutate({ channelId: c.id })}>
            <IcChat size={15} /> <span className="nm">{c.name}</span>
          </button>
        ))}
      </div>

      {people.length > 0 && <div className="wiki-side-title">{t("webmail.kanban.sharePeople")}</div>}
      <div className="dp-list">
        {people.map((p) => (
          <button key={p.id} className="dp-item" disabled={send.isPending} onClick={() => send.mutate({ email: p.email })}>
            <IcUsers size={15} /> <span className="nm">{p.name}</span> <span className="sz">{p.email}</span>
          </button>
        ))}
      </div>
    </Drawer>
  );
}
