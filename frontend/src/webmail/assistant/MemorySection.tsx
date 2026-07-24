import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { assistantApi, type MemoryFact, type AuditEntry } from "./api";
import { ToolCard } from "./ToolCard";

// Ecrã de gestão da memória (§3.5, §8.4). O utilizador vê tudo o que a app sabe sobre si, de onde
// veio (a categoria) e apaga item a item ou por inteiro. Sem isto, a memória seria um arquivo de
// dados pessoais que cresce sem ninguém ver — e o RGPD dá o direito de o ver e apagar.
//
// Aparece nas Definições, que é o único sítio onde a bolha do assistente NÃO aparece: tê-lo a pairar
// sobre a sua própria configuração seria confuso (§3.1).
export function MemorySection() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [draft, setDraft] = useState("");

  const q = useQuery({ queryKey: ["assistant-memory"], queryFn: () => assistantApi.memory(), retry: false });
  const refresh = () => qc.invalidateQueries({ queryKey: ["assistant-memory"] });

  const add = useMutation({
    mutationFn: () => assistantApi.memoryAdd(draft.trim(), "preferência"),
    onSuccess: () => { setDraft(""); refresh(); },
  });
  const remove = useMutation({
    mutationFn: (id: string) => assistantApi.memoryDelete(id),
    onSuccess: refresh,
  });
  const clear = useMutation({
    mutationFn: () => assistantApi.memoryClear(),
    onSuccess: refresh,
  });

  const items = q.data?.items ?? [];

  return (
    <div className="mem">
      <div className="pnl-top">
        <div>
          <h1>{t("memory.title")}</h1>
          <div className="desc">{t("memory.intro")}</div>
        </div>
      </div>

      <div className="pnl-content">
        {/* Acrescentar à mão: a via mais pura de §6.5 — só o utilizador escreve na memória. */}
        <div className="pnl-card"><div className="pnl-card-b">
          <div className="mem-add">
            <input className="pnl-input" value={draft} maxLength={300}
              placeholder={t("memory.addPlaceholder")}
              onChange={(e) => setDraft(e.target.value)}
              onKeyDown={(e) => { if (e.key === "Enter" && draft.trim()) add.mutate(); }} />
            <button className="pnl-btn primary" disabled={!draft.trim() || add.isPending} onClick={() => add.mutate()}>
              {t("memory.add")}
            </button>
          </div>
          {add.isError && <p className="pnl-err">{(add.error as Error).message}</p>}
        </div></div>

        {q.isError && <p className="desc">{(q.error as Error).message}</p>}

        <div className="pnl-card"><div className="pnl-card-b">
          {items.length === 0 ? (
            <div className="pnl-empty">{t("memory.empty")}</div>
          ) : items.map((m: MemoryFact) => (
            <div className="pnl-row" key={m.id}>
              <div>
                <b>{m.fact}</b>
                <div className="sub">{t(`memory.category.${m.category}`, { defaultValue: m.category })}</div>
              </div>
              <div className="grow" />
              <button className="pnl-btn ghost" onClick={() => remove.mutate(m.id)}>{t("memory.delete")}</button>
            </div>
          ))}
        </div></div>

        {items.length > 0 && (
          <button className="pnl-btn ghost" disabled={clear.isPending}
            onClick={() => { if (confirm(t("memory.confirmClear"))) clear.mutate(); }}>
            {t("memory.clearAll")}
          </button>
        )}

        <AuditLog />
      </div>
    </div>
  );
}

// Registo de auditoria (§6.4): as ações que o assistente executou, legíveis pelo utilizador. É o
// que responde a "porque é que este e-mail foi enviado?" — e tem de ser lido por quem usa, não só
// pelo administrador.
function AuditLog() {
  const { t } = useTranslation();
  const q = useQuery({ queryKey: ["assistant-audit"], queryFn: () => assistantApi.audit(), retry: false });
  const items = q.data?.items ?? [];
  if (items.length === 0) return null;
  return (
    <div className="pnl-card">
      <div className="pnl-card-h">{t("memory.auditTitle")}<div className="desc">{t("memory.auditHint")}</div></div>
      <div className="pnl-card-b">
        {items.map((e: AuditEntry) => (
          <div className="mem-audit" key={e.id}>
            <ToolCard step={{ tool: e.tool, args: e.args, summary: e.result, error_code: e.error ? "assistant.tool_failed" : undefined }} />
            <span className="sub">{new Date(e.created_at).toLocaleString()}</span>
          </div>
        ))}
      </div>
    </div>
  );
}
