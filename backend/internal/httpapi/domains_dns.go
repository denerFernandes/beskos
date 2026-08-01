package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"mailin/backend/internal/provisioning"
)

// Verificação de DNS a pedido + exportação da zona (docs/DNS-PAGE.md).
//
// Isto é INFORMATIVO: quem promove o domínio de `dns_pending` para `dns_verified` continua a ser o
// worker (stepVerifyDNS). A página diz o que falta; não altera o estado do domínio. Assim uma
// consulta com cache envenenada não pode "ativar" um domínio por engano.

// dnsCheckTTL: o DNS não muda de segundo a segundo e cada verificação são N consultas de rede.
// Uma janela curta chega para o botão "verificar agora" não virar uma alavanca de carga.
const dnsCheckTTL = 30 * time.Second

type dnsCheckEntry struct {
	at      time.Time
	records []provisioning.RecordCheck
}

type dnsCheckCache struct {
	mu sync.Mutex
	m  map[uuid.UUID]dnsCheckEntry
}

func (c *dnsCheckCache) get(id uuid.UUID) ([]provisioning.RecordCheck, time.Time, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[id]
	if !ok || time.Since(e.at) > dnsCheckTTL {
		return nil, time.Time{}, false
	}
	return e.records, e.at, true
}

func (c *dnsCheckCache) put(id uuid.UUID, recs []provisioning.RecordCheck) time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = map[uuid.UUID]dnsCheckEntry{}
	}
	now := time.Now()
	c.m[id] = dnsCheckEntry{at: now, records: recs}
	return now
}

var dnsChecks dnsCheckCache

// handleCheckDomainDNS verifica registo a registo e devolve o estado de cada um.
func (s *Server) handleCheckDomainDNS(w http.ResponseWriter, r *http.Request) {
	d, ok := s.ownedDomain(w, r) // posse do tenant primeiro: isto resolve DNS, não pode virar scanner alheio
	if !ok {
		return
	}
	zone := s.zoneOf(d)
	if strings.TrimSpace(zone) == "" {
		writeJSON(w, http.StatusOK, map[string]any{"records": []any{}, "checked_at": nil, "summary": dnsSummary(nil)})
		return
	}
	recs, at, cached := dnsChecks.get(d.ID)
	if !cached {
		// Teto de tempo: um domínio mal configurado pode ter vários registos a bater no timeout do
		// resolver, e a página não pode ficar pendurada por causa disso.
		ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
		defer cancel()
		recs = provisioning.NewNetDNSVerifier().CheckZone(ctx, zone)
		at = dnsChecks.put(d.ID, recs)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"records":    orEmpty(recs),
		"checked_at": at.UTC().Format(time.RFC3339),
		"cached":     cached,
		"summary":    dnsSummary(recs),
	})
}

// dnsSummary conta o que interessa ao cabeçalho da página: o que falta e se o que falta é essencial.
func dnsSummary(recs []provisioning.RecordCheck) map[string]any {
	var ok, missing, mismatch, requiredPending int
	for _, r := range recs {
		switch r.Status {
		case provisioning.RecordOK:
			ok++
		case provisioning.RecordMissing:
			missing++
		case provisioning.RecordMismatch:
			mismatch++
		}
		if r.Required && r.Status != provisioning.RecordOK {
			requiredPending++
		}
	}
	return map[string]any{
		"total": len(recs), "ok": ok, "missing": missing, "mismatch": mismatch,
		"required_pending": requiredPending,
	}
}

// handleExportDomainDNS devolve a zona para importar no fornecedor. `zone` (BIND) é o que o
// Cloudflare e o Route 53 importam; `csv` é para quem prefere editar à mão.
func (s *Server) handleExportDomainDNS(w http.ResponseWriter, r *http.Request) {
	d, ok := s.ownedDomain(w, r)
	if !ok {
		return
	}
	zone := s.zoneOf(d)
	switch r.URL.Query().Get("format") {
	case "csv":
		var b strings.Builder
		b.WriteString("type,host,value\n")
		for _, rec := range provisioning.ParseZone(zone) {
			b.WriteString(fmt.Sprintf("%s,%s,%q\n", rec.Type, rec.Host, rec.Value))
		}
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", d.Domain+"-dns.csv"))
		_, _ = w.Write([]byte(b.String()))
	default:
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", d.Domain+".zone"))
		_, _ = w.Write([]byte(provisioning.ZoneFile(d.Domain, zone)))
	}
}
