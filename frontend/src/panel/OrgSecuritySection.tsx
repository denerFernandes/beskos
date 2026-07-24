import { useTranslation } from "react-i18next";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { toast } from "./ui";
import { Switch } from "../webmail/Switch";
import { IcShield } from "./icons";

// admin/security = política DA ORGANIZAÇÃO: MFA obrigatório para todo o tenant, com o método
// escolhido pelo admin. Padrão visual da casa: Switch iOS + segmented control + caixa de aviso.
export function OrgSecuritySection() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const policy = useQuery({ queryKey: ["org-security"], queryFn: () => api.securityPolicyGet(), retry: false });
  const method = policy.data?.mfa_method ?? "any";
  const on = policy.data?.mfa_required ?? false;

  const save = useMutation({
    mutationFn: (p: { on: boolean; method: string }) => api.securityPolicySet(p.on, p.method),
    onSuccess: (r) => {
      qc.setQueryData(["org-security"], r);
      toast(t(r.mfa_required ? "orgSecurity.enabledToast" : "orgSecurity.disabledToast"));
    },
    onError: (e) => toast((e as Error).message),
  });
  const busy = policy.isPending || save.isPending;

  return (
    <>
      <div className="pnl-top">
        <div><h1>{t("panel.security")}</h1><div className="desc">{t("orgSecurity.subtitle")}</div></div>
      </div>
      <div className="pnl-content">
        <div className="pnl-card">
          <div className="pnl-card-h">
            <IcShield size={16} />
            <h3>{t("orgSecurity.title")}</h3>
            <div className="grow" />
            <span className={"pnl-pill " + (on ? "ok" : "muted")}>{on ? t("orgSecurity.on") : t("orgSecurity.off")}</span>
          </div>
          <div className="pnl-card-b">
            <div className="setrow">
              <div className="setrow-tx">
                <b>{t("orgSecurity.toggle")}</b>
                <span className="desc">{t("orgSecurity.desc")}</span>
              </div>
              <Switch checked={on} disabled={busy} label={t("orgSecurity.toggle")}
                onChange={(v) => save.mutate({ on: v, method })} />
            </div>

            <div className="setrow" style={{ borderTop: "1px solid var(--border)" }}>
              <div className="setrow-tx">
                <b>{t("orgSecurity.methodTitle")}</b>
                <span className="desc">{t("orgSecurity.methodDesc")}</span>
              </div>
              <div className="seg">
                {(["any", "totp", "sms"] as const).map((m) => (
                  <button key={m} type="button" disabled={busy}
                    className={"seg-opt" + (method === m ? " on" : "")}
                    onClick={() => save.mutate({ on, method: m })}>
                    {t("orgSecurity.methodShort_" + m)}
                  </button>
                ))}
              </div>
            </div>

            <div className="warnbox">{t("orgSecurity.killWarning")}</div>
          </div>
        </div>
      </div>
    </>
  );
}
