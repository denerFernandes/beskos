package managerapi

import (
	"fmt"
	"net/http"
)

// Overuse alerts. Rules computed on the fly from TODAY's counters plus a 7-day baseline — no new
// state; an alert disappears once the condition clears.
//
//	users_cap      warn ≥90% do teto de utilizadores do plano · crit = no teto
//	storage_cap    warn ≥80% do pool de storage do plano · crit ≥95%
//	mail_cap       warn ≥80% do teto diário de envio · crit ≥100%
//	ai_spike       tenant gastou hoje ≥3× a média dos 7 dias anteriores (e ≥50k tokens)
//	ai_user_heavy  UM utilizador gastou ≥100k tokens em 24h (top users vêm juntos)
const (
	aiSpikeFactor  = 3
	aiSpikeFloor   = 50_000  // spikes só contam a partir daqui (3×0 seria ruído)
	aiUserHeavy    = 100_000 // tokens/24h de UM utilizador
	warnUsersPct   = 90
	warnStoragePct = 80
	critStoragePct = 95
	warnMailPct    = 80
)

type alert struct {
	Severity string `json:"severity"` // warn | critical
	Kind     string `json:"kind"`
	TenantID string `json:"tenant_id"`
	Tenant   string `json:"tenant"`
	Detail   string `json:"detail"` // texto legível pt-BR (ferramenta interna, como o resto do manager)
}

// handleAlerts calcula os alertas e devolve também o top de utilizadores de IA (24h).
func (s *Server) handleAlerts(w http.ResponseWriter, r *http.Request) {
	rows, err := s.q.ManagerAlertRows(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "manager.failed")
		return
	}
	alerts := []alert{}
	add := func(sev, kind, tid, tenant, detail string) {
		alerts = append(alerts, alert{Severity: sev, Kind: kind, TenantID: tid, Tenant: tenant, Detail: detail})
	}
	for _, t := range rows {
		id := t.ID.String()
		// Teto de utilizadores.
		if t.MaxUsers > 0 {
			pct := int(t.Users) * 100 / int(t.MaxUsers)
			if int(t.Users) >= int(t.MaxUsers) {
				add("critical", "users_cap", id, t.Name, fmt.Sprintf("%d/%d utilizadores (teto do plano atingido)", t.Users, t.MaxUsers))
			} else if pct >= warnUsersPct {
				add("warn", "users_cap", id, t.Name, fmt.Sprintf("%d/%d utilizadores (%d%% do teto)", t.Users, t.MaxUsers, pct))
			}
		}
		// Pool de storage.
		if t.MaxStorageGb > 0 {
			capBytes := int64(t.MaxStorageGb) << 30
			pct := int(t.StorageBytes * 100 / capBytes)
			if pct >= critStoragePct {
				add("critical", "storage_cap", id, t.Name, fmt.Sprintf("storage a %d%% do plano (%d GB)", pct, t.MaxStorageGb))
			} else if pct >= warnStoragePct {
				add("warn", "storage_cap", id, t.Name, fmt.Sprintf("storage a %d%% do plano (%d GB)", pct, t.MaxStorageGb))
			}
		}
		// Envio diário.
		if t.MaxDailySend > 0 {
			pct := int(t.MailToday) * 100 / int(t.MaxDailySend)
			if pct >= 100 {
				add("critical", "mail_cap", id, t.Name, fmt.Sprintf("%d envios hoje — teto diário (%d) atingido", t.MailToday, t.MaxDailySend))
			} else if pct >= warnMailPct {
				add("warn", "mail_cap", id, t.Name, fmt.Sprintf("%d envios hoje (%d%% do teto diário)", t.MailToday, pct))
			}
		}
		// Spike de IA vs baseline.
		if t.AiToday >= aiSpikeFloor && t.AiPrevAvg > 0 && t.AiToday >= t.AiPrevAvg*aiSpikeFactor {
			add("warn", "ai_spike", id, t.Name,
				fmt.Sprintf("%d tokens de IA hoje (%d× a média dos 7 dias: %d)", t.AiToday, t.AiToday/max64(t.AiPrevAvg, 1), t.AiPrevAvg))
		}
	}

	// Deliverability (limiares da AWS: bounce >5% em revisão, >10% pausa; complaint >0.1% / >0.5%).
	if del, derr := s.q.ManagerDeliverability(r.Context()); derr == nil {
		for _, t := range del {
			if t.Sent30d < 50 {
				continue // amostra pequena demais para taxa fazer sentido
			}
			bouncePct := float64(t.Bounces30d) * 100 / float64(t.Sent30d)
			complaintPct := float64(t.Complaints30d) * 100 / float64(t.Sent30d)
			id := t.ID.String()
			if bouncePct >= 10 {
				add("critical", "bounce_rate", id, t.Name, fmt.Sprintf("taxa de bounce %.1f%% em 30d (%d/%d) — risco de pausa no SES", bouncePct, t.Bounces30d, t.Sent30d))
			} else if bouncePct >= 5 {
				add("warn", "bounce_rate", id, t.Name, fmt.Sprintf("taxa de bounce %.1f%% em 30d (%d/%d)", bouncePct, t.Bounces30d, t.Sent30d))
			}
			if complaintPct >= 0.5 {
				add("critical", "complaint_rate", id, t.Name, fmt.Sprintf("taxa de reclamações %.2f%% em 30d (%d/%d) — risco de pausa no SES", complaintPct, t.Complaints30d, t.Sent30d))
			} else if complaintPct >= 0.1 {
				add("warn", "complaint_rate", id, t.Name, fmt.Sprintf("taxa de reclamações %.2f%% em 30d (%d/%d)", complaintPct, t.Complaints30d, t.Sent30d))
			}
		}
	}

	top, err := s.q.ManagerTopAIUsers(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "manager.failed")
		return
	}
	// Utilizador individual acima do teto de atenção → alerta próprio.
	for _, u := range top {
		if u.Tokens >= aiUserHeavy {
			alerts = append(alerts, alert{
				Severity: "warn", Kind: "ai_user_heavy", Tenant: u.Tenant,
				Detail: fmt.Sprintf("%s gastou %d tokens de IA em 24h", u.Email, u.Tokens),
			})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"alerts": alerts, "top_ai_users": orEmpty(top)})
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
