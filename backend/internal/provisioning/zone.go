package provisioning

import (
	"fmt"
	"strings"
)

// AugmentZoneForSES prepara a zona do cliente em modo WHITE-LABEL: o SES assina via BYODKIM (chave
// nossa), por isso o cliente publica só um TXT DKIM genérico (a nossa chave pública num seletor da
// marca) — SEM qualquer referência ao amazonses. O SPF passa a incluir `_spf.<brand>` (que na NOSSA
// zona embrulha o include:amazonses.com), e há um DMARC com rua na nossa marca. Não há custom MAIL
// FROM (evita o MX feedback-smtp que revelaria o SES); o DMARC alinha por DKIM. Idempotente.
func AugmentZoneForSES(zone, domain, selector, publicKey, brand string) string {
	spfInclude := "include:_spf." + brand
	lines := strings.Split(strings.TrimRight(zone, "\n"), "\n")
	for i, line := range lines {
		if strings.Contains(line, "v=spf1") && !strings.Contains(line, spfInclude) {
			lines[i] = spfWithInclude(line, spfInclude)
		}
	}
	out := strings.Join(lines, "\n")
	dkim := fmt.Sprintf("%s._domainkey.%s. IN TXT \"v=DKIM1; k=rsa; p=%s\"", selector, domain, publicKey)
	if !strings.Contains(out, selector+"._domainkey."+domain) {
		out += "\n" + dkim
	}
	if !strings.Contains(out, "_dmarc."+domain) {
		out += fmt.Sprintf("\n_dmarc.%s. IN TXT \"v=DMARC1; p=quarantine; rua=mailto:dmarc@%s\"", domain, brand)
	}
	return out + "\n"
}

// StripStalwartDKIM remove da zona os registos DKIM gerados pelo Stalwart (TXT `..._domainkey...`
// com `DKIM1`). No modo relay o Stalwart não assina (DKIM Manual), então esses registos não devem
// chegar ao cliente — quem assina é o SES com a nossa chave (via AugmentZoneForSES).
func StripStalwartDKIM(zone string) string {
	var kept []string
	for _, line := range strings.Split(zone, "\n") {
		if strings.Contains(line, "_domainkey") && strings.Contains(line, "DKIM1") {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

// spfWithInclude insere um mecanismo `include:...` antes do mecanismo terminal (-all/~all/?all);
// sem terminal, anexa ao fim do valor.
func spfWithInclude(line, include string) string {
	for _, term := range []string{"-all", "~all", "?all", "+all"} {
		if i := strings.Index(line, term); i >= 0 {
			return line[:i] + include + " " + line[i:]
		}
	}
	return line + " " + include
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
