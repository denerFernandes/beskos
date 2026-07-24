package provisioning

import (
	"fmt"
	"strings"
)

// AugmentZoneForSES acrescenta à zona do Stalwart os registros exigidos pelo relay SES
// (docs/manager.md §4): os 3 CNAMEs de Easy DKIM, o include:amazonses.com no SPF existente e
// um DMARC básico se ainda não houver. Idempotente: registros já presentes não se repetem.
func AugmentZoneForSES(zone, domain string, tokens []string) string {
	lines := strings.Split(strings.TrimRight(zone, "\n"), "\n")
	// SPF: injeta o include do SES na TXT v=spf1 (antes do mecanismo terminal ...all).
	for i, line := range lines {
		if strings.Contains(line, "v=spf1") && !strings.Contains(line, "amazonses.com") {
			lines[i] = spfWithSES(line)
		}
	}
	out := strings.Join(lines, "\n")
	for _, tok := range tokens {
		rec := fmt.Sprintf("%s._domainkey.%s. IN CNAME %s.dkim.amazonses.com.", tok, domain, tok)
		if !strings.Contains(out, rec) {
			out += "\n" + rec
		}
	}
	if !strings.Contains(out, "_dmarc."+domain) {
		out += fmt.Sprintf("\n_dmarc.%s. IN TXT \"v=DMARC1; p=quarantine; rua=mailto:postmaster@%s\"", domain, domain)
	}
	return out + "\n"
}

// AugmentZoneMailFrom acrescenta os 2 registos do custom MAIL FROM (mailer.<domínio>): o MX para
// o feedback-smtp da região (bounces do SES) e o SPF do próprio subdomínio. Idempotente.
func AugmentZoneMailFrom(zone, mailFrom, mxTarget string) string {
	out := strings.TrimRight(zone, "\n")
	mx := fmt.Sprintf("%s. IN MX 10 %s.", mailFrom, mxTarget)
	if !strings.Contains(out, mx) {
		out += "\n" + mx
	}
	spf := fmt.Sprintf("%s. IN TXT \"v=spf1 include:amazonses.com ~all\"", mailFrom)
	if !strings.Contains(out, spf) {
		out += "\n" + spf
	}
	return out + "\n"
}

// spfWithSES insere "include:amazonses.com" antes do mecanismo terminal (-all/~all/?all);
// sem terminal, anexa ao fim do valor.
func spfWithSES(line string) string {
	for _, term := range []string{"-all", "~all", "?all", "+all"} {
		if i := strings.Index(line, term); i >= 0 {
			return line[:i] + "include:amazonses.com " + line[i:]
		}
	}
	return line + " include:amazonses.com"
}

// DNSRecord é um registro pronto para o cliente publicar, estruturado para a UI (tela dns_pending).
// Purpose diz PARA QUE o registro serve (mx | spf | dkim | dkim_ses | dmarc | other) — a UI agrupa
// por ele e explica que TODOS são obrigatórios para receber/enviar e-mail.
type DNSRecord struct {
	Host    string `json:"host"`
	Type    string `json:"type"`
	Value   string `json:"value"`
	Purpose string `json:"purpose"`
}

// classify deduz o propósito do registro pelo tipo/host/valor.
func classify(r DNSRecord) string {
	switch {
	case r.Type == "MX" && strings.Contains(r.Value, "amazonses.com"):
		return "mailfrom" // MX do custom MAIL FROM (bounces do SES) — faz parte do ENVIO
	case r.Type == "MX":
		return "mx"
	case strings.Contains(r.Value, "v=spf1"):
		return "spf"
	case strings.HasPrefix(r.Host, "_dmarc."):
		return "dmarc"
	case strings.Contains(r.Host, "_domainkey") && strings.Contains(r.Value, "amazonses.com"):
		return "dkim_ses"
	case strings.Contains(r.Host, "_domainkey"):
		return "dkim"
	default:
		return "other"
	}
}

// ParseZone converte o trecho de zona BIND devolvido pelo Stalwart (dnsZoneFile) em registros
// estruturados. Formato de cada linha: "<host>. IN <TYPE> <value...>". Linhas inválidas são ignoradas.
func ParseZone(zone string) []DNSRecord {
	var out []DNSRecord
	for _, raw := range strings.Split(zone, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, ";") {
			continue
		}
		fields := strings.Fields(line)
		// Menor forma válida: host IN TYPE value
		in := indexOf(fields, "IN")
		if in < 1 || in+2 >= len(fields) {
			continue
		}
		host := strings.TrimSuffix(fields[in-1], ".")
		rtype := fields[in+1]
		value := strings.Join(fields[in+2:], " ")
		value = strings.Trim(value, "\"")
		rec := DNSRecord{Host: host, Type: rtype, Value: value}
		rec.Purpose = classify(rec)
		out = append(out, rec)
	}
	return out
}

func indexOf(fields []string, target string) int {
	for i, f := range fields {
		if f == target {
			return i
		}
	}
	return -1
}
