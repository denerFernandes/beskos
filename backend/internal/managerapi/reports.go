package managerapi

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

// Relatórios e exports CSV (M5, docs/manager.md §5.2). Coortes de churn ficam para o billing real
// (sem eventos de cancelamento ainda) — registado no doc.

func (s *Server) handleGrowth(w http.ResponseWriter, r *http.Request) {
	rows, err := s.q.ManagerGrowth(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "manager.failed")
		return
	}
	writeJSON(w, http.StatusOK, orEmpty(rows))
}

// csvOut prepara a resposta CSV (download). O writer devolvido tem de levar Flush no fim.
func csvOut(w http.ResponseWriter, filename string) *csv.Writer {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename="+filename)
	return csv.NewWriter(w)
}

func (s *Server) handleExportTenants(w http.ResponseWriter, r *http.Request) {
	rows, err := s.q.ManagerListTenants(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "manager.failed")
		return
	}
	cw := csvOut(w, "tenants.csv")
	_ = cw.Write([]string{"id", "name", "plan", "status", "users", "domains", "storage_bytes", "created_at"})
	for _, t := range rows {
		_ = cw.Write([]string{t.ID.String(), t.Nome, t.Plano, t.Status,
			strconv.FormatInt(t.Users, 10), strconv.FormatInt(t.Domains, 10),
			strconv.FormatInt(t.StorageBytes, 10), t.CreatedAt.Format(time.RFC3339)})
	}
	cw.Flush()
}

func (s *Server) handleExportUsage(w http.ResponseWriter, r *http.Request) {
	days := 30
	if v, err := strconv.Atoi(r.URL.Query().Get("days")); err == nil && v > 0 && v <= 365 {
		days = v
	}
	since := pgtype.Date{Time: time.Now().AddDate(0, 0, -days), Valid: true}
	rows, err := s.q.ManagerUsageAllRows(r.Context(), since)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "manager.failed")
		return
	}
	cw := csvOut(w, "usage.csv")
	_ = cw.Write([]string{"day", "tenant", "users", "users_active", "storage_bytes", "files",
		"ai_tokens", "meet_minutes", "recordings", "recordings_bytes", "wiki_pages", "cards", "mail_sent"})
	for _, u := range rows {
		_ = cw.Write([]string{u.Day.Time.Format("2006-01-02"), u.Nome,
			strconv.Itoa(int(u.Users)), strconv.Itoa(int(u.UsersActive)),
			strconv.FormatInt(u.StorageBytes, 10), strconv.Itoa(int(u.Files)),
			strconv.FormatInt(u.AiTokens, 10), strconv.Itoa(int(u.MeetMinutes)),
			strconv.Itoa(int(u.Recordings)), strconv.FormatInt(u.RecordingsBytes, 10),
			strconv.Itoa(int(u.WikiPages)), strconv.Itoa(int(u.Cards)), strconv.Itoa(int(u.MailSent))})
	}
	cw.Flush()
}

func (s *Server) handleExportFinance(w http.ResponseWriter, r *http.Request) {
	_, tenants, err := s.computeFinance(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "manager.failed")
		return
	}
	cw := csvOut(w, "finance.csv")
	_ = cw.Write([]string{"tenant", "plan", "status", "users", "mrr_cents", "cost_cents", "margin_cents"})
	for _, t := range tenants {
		_ = cw.Write([]string{t.Nome, t.Plano, t.Status, strconv.Itoa(int(t.Users)),
			fmt.Sprint(t.MrrCents), fmt.Sprint(t.CostCents), fmt.Sprint(t.MarginCents)})
	}
	cw.Flush()
}
