import { useTranslation } from "react-i18next";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { backend } from "./backend";
import { Spinner } from "./Spinner";
import { toast } from "./toast";
import { IcClock, IcTrash, IcX } from "../panel/icons";

// Gestão de e-mails agendados (outbox server-side): lista os envios ainda pendentes e permite
// cancelar. Undo-send de janela curta também aparece aqui por um instante — cancelar aqui equivale
// ao "desfazer". O envio real é feito pelo worker; cancelar só afeta pendentes (409 = tarde demais).
export function ScheduledModal({ onClose }: { onClose: () => void }) {
  const { t, i18n } = useTranslation();
  const qc = useQueryClient();
  const q = useQuery({ queryKey: ["outbox"], queryFn: () => backend.outboxList(), refetchInterval: 15000 });
  const cancel = useMutation({
    mutationFn: (id: string) => backend.outboxCancel(id),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ["outbox"] }); toast(t("webmail.scheduled.canceled")); },
    onError: () => toast(t("webmail.compose.undoTooLate")),
  });
  const items = q.data ?? [];
  const fmt = (iso: string) => new Intl.DateTimeFormat(i18n.language, { dateStyle: "medium", timeStyle: "short" }).format(new Date(iso));
  return (
    <div className="tk-bg open" onMouseDown={onClose}>
      <div className="tk" onMouseDown={(e) => e.stopPropagation()} style={{ width: 480 }}>
        <header><span><IcClock size={16} /> {t("webmail.scheduled.title")}</span><button className="ib" title={t("common.close")} onClick={onClose}><IcX size={16} /></button></header>
        <div className="body">
          {q.isLoading ? <Spinner />
            : items.length === 0 ? <div className="empty" style={{ padding: 24 }}>{t("webmail.scheduled.empty")}</div>
            : (
              <div className="sched-list">
                {items.map((o) => (
                  <div key={o.id} className="sched-row">
                    <div className="sched-who">
                      <b>{o.subject || t("webmail.scheduled.noSubject")}</b>
                      <small>{t("webmail.scheduled.to", { rcpts: o.rcpts.join(", ") })}</small>
                      <small className="sched-when"><IcClock size={12} /> {fmt(o.send_at)}</small>
                    </div>
                    <button className="chipbtn danger" disabled={cancel.isPending} onClick={() => cancel.mutate(o.id)}>
                      <IcTrash size={13} /> {t("webmail.scheduled.cancel")}
                    </button>
                  </div>
                ))}
              </div>
            )}
        </div>
      </div>
    </div>
  );
}
