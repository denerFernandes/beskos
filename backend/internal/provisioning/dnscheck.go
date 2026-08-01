package provisioning

import (
	"context"
	"strings"
)

// Verificação DNS registo a registo, para a página de domínios poder dizer O QUE falta em vez de um
// "à espera" mudo. É informativa: quem promove `dns_pending → dns_verified` continua a ser o
// stepVerifyDNS (Verify), que só olha para o mínimo. Aqui olhamos para tudo o que mandámos publicar.
//
// Não se compara texto a texto: um TXT de SPF publicado pelo cliente raramente é igual ao nosso
// carácter a carácter (aspas, ordem dos mecanismos, um include a mais). Compara-se pelo que importa
// em cada tipo — ver `matches`.

// RecordStatus é o estado de um registo no DNS real.
const (
	RecordOK       = "ok"       // publicado e a bater
	RecordMissing  = "missing"  // não existe nada naquele host/tipo
	RecordMismatch = "mismatch" // existe, mas não é o que devia
)

// RecordCheck é o resultado por registo: o que pedimos, o que está publicado e o veredito.
type RecordCheck struct {
	Host     string   `json:"host"`
	Type     string   `json:"type"`
	Expected string   `json:"expected"`
	Purpose  string   `json:"purpose"`
	Found    []string `json:"found"`
	Status   string   `json:"status"`
	// Required diz se o correio deixa de funcionar sem este registo. MX/SPF/DKIM são obrigatórios;
	// DMARC, MTA-STS, autodiscover e os SRV são recomendações — a página avisa, não bloqueia.
	Required bool `json:"required"`
}

// CheckZone resolve cada registo da zona e devolve o estado de todos. Erros de resolução não são
// fatais: um registo que não resolve é "missing", que é a informação útil para quem está a publicar.
func (v *NetDNSVerifier) CheckZone(ctx context.Context, zone string) []RecordCheck {
	recs := ParseZone(zone)
	out := make([]RecordCheck, 0, len(recs))
	for _, r := range recs {
		c := RecordCheck{
			Host: r.Host, Type: r.Type, Expected: r.Value, Purpose: r.Purpose,
			Required: requiredPurpose(r.Purpose),
			Status:   RecordMissing,
		}
		c.Found = v.lookup(ctx, r)
		switch {
		case len(c.Found) == 0:
			c.Status = RecordMissing
		case matches(r, c.Found):
			c.Status = RecordOK
		default:
			c.Status = RecordMismatch
		}
		out = append(out, c)
	}
	return out
}

// requiredPurpose separa o que é preciso para o correio funcionar do que é boa prática.
func requiredPurpose(p string) bool {
	switch p {
	case "mx", "spf", "dkim", "dkim_ses", "mailfrom":
		return true
	default:
		return false
	}
}

// lookup devolve os valores publicados para o host/tipo do registo.
func (v *NetDNSVerifier) lookup(ctx context.Context, r DNSRecord) []string {
	switch strings.ToUpper(r.Type) {
	case "MX":
		mxs, err := v.resolver.LookupMX(ctx, r.Host)
		if err != nil {
			return nil
		}
		var out []string
		for _, m := range mxs {
			out = append(out, strings.TrimSuffix(m.Host, "."))
		}
		return out
	case "TXT":
		txts, err := v.resolver.LookupTXT(ctx, r.Host)
		if err != nil {
			return nil
		}
		return txts
	case "CNAME":
		cname, err := v.resolver.LookupCNAME(ctx, r.Host)
		if err != nil || cname == "" {
			return nil
		}
		return []string{strings.TrimSuffix(cname, ".")}
	case "SRV":
		// O host do SRV já vem no formato _serviço._proto.domínio; o LookupSRV com serviço/proto
		// vazios usa o nome tal como está.
		_, srvs, err := v.resolver.LookupSRV(ctx, "", "", r.Host)
		if err != nil {
			return nil
		}
		var out []string
		for _, s := range srvs {
			out = append(out, strings.TrimSuffix(s.Target, "."))
		}
		return out
	case "A", "AAAA":
		ips, err := v.resolver.LookupHost(ctx, r.Host)
		if err != nil {
			return nil
		}
		return ips
	}
	return nil
}

// matches decide se o publicado satisfaz o esperado. A comparação é por TIPO, porque igualdade
// literal daria falsos negativos em quase todos os casos reais.
func matches(r DNSRecord, found []string) bool {
	exp := strings.ToLower(strings.TrimSpace(r.Value))
	switch strings.ToUpper(r.Type) {
	case "MX":
		// O valor traz prioridade + host ("10 mail.exemplo.com."); só o host interessa.
		fields := strings.Fields(exp)
		want := strings.TrimSuffix(fields[len(fields)-1], ".")
		return containsFold(found, want)
	case "TXT":
		switch r.Purpose {
		case "spf":
			// Basta ser uma SPF que inclua o nosso include — o cliente pode ter outros mecanismos.
			inc := extractInclude(exp)
			for _, f := range found {
				lf := strings.ToLower(f)
				if strings.Contains(lf, "v=spf1") && (inc == "" || strings.Contains(lf, inc)) {
					return true
				}
			}
			return false
		case "dmarc":
			for _, f := range found {
				if strings.Contains(strings.ToLower(f), "v=dmarc1") {
					return true
				}
			}
			return false
		case "dkim", "dkim_ses":
			// Compara a CHAVE (p=), não o registo inteiro: o cliente pode reordenar as tags.
			key := tagValue(exp, "p=")
			for _, f := range found {
				lf := strings.ToLower(f)
				if key != "" && strings.Contains(strings.ReplaceAll(lf, " ", ""), key) {
					return true
				}
				if key == "" && strings.Contains(lf, "dkim1") {
					return true
				}
			}
			return false
		}
		return containsFold(found, exp)
	case "CNAME", "SRV":
		fields := strings.Fields(exp)
		want := strings.TrimSuffix(fields[len(fields)-1], ".")
		return containsFold(found, want)
	}
	return containsFold(found, exp)
}

func containsFold(list []string, want string) bool {
	for _, v := range list {
		if strings.EqualFold(strings.TrimSuffix(strings.TrimSpace(v), "."), want) {
			return true
		}
	}
	return false
}

// extractInclude tira o primeiro include:... de uma SPF.
func extractInclude(spf string) string {
	for _, f := range strings.Fields(spf) {
		if strings.HasPrefix(f, "include:") {
			return f
		}
	}
	return ""
}

// tagValue lê o valor de uma tag ("p=") de um registo TXT com tags separadas por ";".
func tagValue(txt, tag string) string {
	for _, part := range strings.Split(txt, ";") {
		p := strings.TrimSpace(part)
		if strings.HasPrefix(p, tag) {
			return strings.ReplaceAll(strings.TrimPrefix(p, tag), " ", "")
		}
	}
	return ""
}

// ZoneFile devolve a zona pronta a importar (formato BIND) — é o que o Cloudflare, o Route 53 e a
// maioria dos painéis aceitam. Sem inventar formato nenhum: é o que o Stalwart já produz, com um
// cabeçalho de comentário.
func ZoneFile(domain, zone string) string {
	var b strings.Builder
	// English, like everything else the backend writes (law 4.6). The alternative would be guessing
	// the reader's language from the server side, which is what §4.4 exists to prevent -- and a zone
	// file is read next to a DNS control panel that is in English anyway. If these lines ever need
	// translating, they belong in the frontend, which knows who is asking.
	b.WriteString("; DNS records for " + domain + "\n")
	b.WriteString("; Import these in your DNS provider (Cloudflare: DNS > Records > Import).\n")
	b.WriteString("; TTL is up to your provider.\n\n")
	for _, line := range strings.Split(strings.TrimSpace(zone), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		b.WriteString(strings.TrimSpace(line) + "\n")
	}
	return b.String()
}
