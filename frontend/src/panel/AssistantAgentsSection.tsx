import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { assistantApi, type AgentDraft, type AgentProfile } from "../webmail/assistant/api";
import { toast } from "./ui";
import { Drawer } from "../webmail/Drawer";
import { IcPlus } from "./icons";

// Agentes do assistente (RBAC org.settings). Duas listas com regras diferentes, e a diferença é
// deliberada (§3.2 do desenho do assistente):
//
//   - os de ORIGEM são parte do produto: o administrador liga e desliga, não edita. Se pudesse
//     mudar-lhes as instruções, o produto passaria a parecer mau sem o suporte conseguir distinguir
//     um defeito nosso de uma edição local — e o conjunto de ferramentas de um agente é a sua
//     superfície de risco;
//   - os do CLIENTE são dele: cria, altera e apaga. Nunca com mais ferramentas do que o teto, e
//     isso é validado no servidor — esconder uma caixa aqui não seria defesa nenhuma.
export function AssistantAgentsSection() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [editing, setEditing] = useState<AgentProfile | "new" | null>(null);

  const q = useQuery({ queryKey: ["assistant-agents"], queryFn: () => assistantApi.agents(), retry: false });
  const refresh = () => qc.invalidateQueries({ queryKey: ["assistant-agents"] });

  const toggle = useMutation({
    mutationFn: ({ key, enabled }: { key: string; enabled: boolean }) => assistantApi.agentEnable(key, enabled),
    // Também invalida ["me"]: desligar o agente geral esconde a bolha do assistente em toda a app, e
    // isso lê-se do /api/me — sem isto só mudaria depois de recarregar.
    onSuccess: () => { refresh(); qc.invalidateQueries({ queryKey: ["me"] }); },
    onError: (e: Error) => toast(e.message),
  });
  const remove = useMutation({
    mutationFn: (key: string) => assistantApi.agentDelete(key),
    onSuccess: refresh,
    onError: (e: Error) => toast(e.message),
  });

  const builtins = q.data?.items.filter((a) => a.builtin) ?? [];
  const custom = q.data?.items.filter((a) => !a.builtin) ?? [];

  return (
    <>
      <div className="pnl-top">
        <div><h1>{t("agents.title")}</h1><div className="desc">{t("agents.intro")}</div></div>
        <div className="grow" />
        <button className="pnl-btn primary" onClick={() => setEditing("new")}>
          <IcPlus size={16} />{t("agents.create")}
        </button>
      </div>

      <div className="pnl-content">
        {q.isError && <p className="desc">{(q.error as Error).message}</p>}

        <div className="pnl-card">
          <div className="pnl-card-h">{t("agents.builtin")}<div className="desc">{t("agents.builtinHint")}</div></div>
          <div className="pnl-card-b">
            {builtins.map((a) => (
              <div className="pnl-row" key={a.key}>
                <div>
                  <b>{t(`assistant.agent.${a.key}`, { defaultValue: a.key })}</b>
                  <div className="sub">{a.tools.map((x) => t(`assistant.tools.${x}`, { defaultValue: x })).join(" · ")}</div>
                </div>
                <div className="grow" />
                {a.key === "assistant" && (
                  // O geral É o assistente: desligá-lo desliga a IA para toda a organização. O aviso
                  // torna a consequência explícita — o interruptor em si é igual aos outros.
                  <span className="desc agent-master-hint">{t("agents.masterHint")}</span>
                )}
                <label className="switch" title={a.enabled ? t("agents.on") : t("agents.off")}>
                  <input type="checkbox" checked={a.enabled} disabled={toggle.isPending}
                    onChange={() => toggle.mutate({ key: a.key, enabled: !a.enabled })} />
                  <span className="track" />
                </label>
              </div>
            ))}
          </div>
        </div>

        <div className="pnl-card">
          <div className="pnl-card-h">{t("agents.custom")}</div>
          <div className="pnl-card-b">
            {custom.length === 0 ? <div className="pnl-empty">{t("agents.noneYet")}</div> : custom.map((a) => (
              <div className="pnl-row" key={a.key}>
                <div>
                  <b>{a.name}</b>
                  <div className="sub">{a.purpose || a.tools.join(" · ")}</div>
                </div>
                <div className="grow" />
                <button className="pnl-btn" onClick={() => setEditing(a)}>{t("agents.edit")}</button>
                <button className="pnl-btn ghost" onClick={() => remove.mutate(a.key)}>{t("agents.delete")}</button>
              </div>
            ))}
          </div>
        </div>
      </div>

      {editing && (
        <AgentEditor
          agent={editing === "new" ? null : editing}
          builtins={builtins}
          maxTools={q.data?.max_tools ?? []}
          onClose={() => setEditing(null)}
          onSaved={() => { setEditing(null); refresh(); }}
        />
      )}
    </>
  );
}

// AgentEditor cria ou altera um perfil do cliente.
function AgentEditor({
  agent, builtins, maxTools, onClose, onSaved,
}: {
  agent: AgentProfile | null;
  builtins: AgentProfile[];
  maxTools: string[];
  onClose: () => void;
  onSaved: () => void;
}) {
  const { t } = useTranslation();
  const [name, setName] = useState(agent?.name ?? "");
  const [purpose, setPurpose] = useState(agent?.purpose ?? "");
  const [instructions, setInstructions] = useState(agent?.instructions ?? "");
  const [tools, setTools] = useState<string[]>(agent?.tools ?? []);
  const [duplicateOf, setDuplicateOf] = useState("");

  const save = useMutation({
    mutationFn: () => {
      const draft: AgentDraft = { name, purpose, instructions, tools };
      if (agent) return assistantApi.agentUpdate(agent.key, draft);
      return assistantApi.agentCreate(duplicateOf ? { ...draft, duplicate_of: duplicateOf } : draft);
    },
    onSuccess: onSaved,
    onError: (e: Error) => toast(e.message),
  });

  // Duplicar traz as ferramentas do agente de origem como PONTO DE PARTIDA. A cópia é independente
  // a partir daí — não recebe as nossas melhorias — e é isso que o aviso diz.
  const applyDuplicate = (key: string) => {
    setDuplicateOf(key);
    const src = builtins.find((b) => b.key === key);
    setTools(src ? src.tools : []);
  };

  const toggleTool = (tool: string) =>
    setTools((prev) => (prev.includes(tool) ? prev.filter((x) => x !== tool) : [...prev, tool]));

  return (
    <Drawer
      title={agent ? t("agents.edit") : t("agents.create")}
      onClose={onClose}
      footer={
        <>
          <button className="pnl-btn ghost" onClick={onClose}>{t("common.cancel")}</button>
          <div className="grow" />
          <button className="pnl-btn primary" disabled={save.isPending || !name.trim()} onClick={() => save.mutate()}>
            {t("agents.save")}
          </button>
        </>
      }
    >
      {!agent && (
        <div className="pnl-field">
          <label>{t("agents.duplicateOf")}</label>
          <select className="pnl-select" value={duplicateOf} onChange={(e) => applyDuplicate(e.target.value)}>
            <option value="">{t("agents.fromScratch")}</option>
            {builtins.map((b) => (
              <option key={b.key} value={b.key}>{t(`assistant.agent.${b.key}`, { defaultValue: b.key })}</option>
            ))}
          </select>
          {duplicateOf && <p className="desc">{t("agents.duplicateWarning")}</p>}
        </div>
      )}

      <div className="pnl-field">
        <label>{t("agents.name")}</label>
        <input className="pnl-input" autoFocus value={name} maxLength={60} onChange={(e) => setName(e.target.value)} />
      </div>

      <div className="pnl-field">
        <label>{t("agents.purpose")}</label>
        <input className="pnl-input" value={purpose} maxLength={200}
          placeholder={t("agents.purposePlaceholder")} onChange={(e) => setPurpose(e.target.value)} />
        <p className="desc">{t("agents.purposeHint")}</p>
      </div>

      <div className="pnl-field">
        <label>{t("agents.instructions")}</label>
        <textarea className="pnl-input" rows={5} value={instructions} maxLength={4000}
          onChange={(e) => setInstructions(e.target.value)} />
      </div>

      <div className="pnl-field">
        <label>{t("agents.tools")}</label>
        <div className="agent-tools">
          {maxTools.map((tool) => (
            <label key={tool} className="agent-tool">
              <span className="switch"><input type="checkbox" checked={tools.includes(tool)} onChange={() => toggleTool(tool)} /><span className="track" /></span>
              <span>{t(`assistant.tools.${tool}`, { defaultValue: tool })}</span>
            </label>
          ))}
        </div>
        <p className="desc">{t("agents.toolsHint")}</p>
      </div>
    </Drawer>
  );
}
