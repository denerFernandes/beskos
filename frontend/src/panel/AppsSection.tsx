import { useTranslation } from "react-i18next";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { toast } from "../webmail/toast";

// Alguns ids de app não batem com a chave i18n do rail (kanban→board, cal→calendar).
const APP_I18N: Record<string, string> = { kanban: "board", cal: "calendar" };

// AppsSection: o admin liga/desliga apps da organização. Desligar faz a app sumir do menu para
// todos, cortar a integração entre apps, e o assistente perde as ferramentas e o agente dela.
export function AppsSection() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const q = useQuery({ queryKey: ["admin-apps"], queryFn: () => api.appsGet(), retry: false });
  const set = useMutation({
    mutationFn: (disabled: string[]) => api.appsSet(disabled),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["admin-apps"] });
      qc.invalidateQueries({ queryKey: ["me"] }); // o rail relê disabled_apps daqui
      toast(t("panel.appsSaved"));
    },
    onError: (e) => toast((e as Error).message),
  });

  const d = q.data;
  const disabled = new Set(d?.disabled ?? []);
  const toggle = (id: string) => {
    const next = new Set(disabled);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    set.mutate([...next]);
  };

  return (
    <div className="pnl-card">
      <div className="pnl-card-h"><h3>{t("panel.apps")}</h3></div>
      <div className="pnl-card-b">
        <p className="desc">{t("panel.appsHint")}</p>
        {q.isError && <p className="desc">{(q.error as Error).message}</p>}
        {(d?.apps ?? []).map((id) => (
          <label key={id} className="apps-row">
            <span>{t(`webmail.apps.${APP_I18N[id] ?? id}`)}</span>
            <span className="switch">
              <input type="checkbox" checked={!disabled.has(id)} disabled={set.isPending} onChange={() => toggle(id)} />
              <span className="track" />
            </span>
          </label>
        ))}
      </div>
    </div>
  );
}
