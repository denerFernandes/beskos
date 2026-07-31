package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// Verificação de assinatura AWS SigV4.
//
// PORQUÊ VERIFICAR, se o serviço só é alcançável na rede interna: porque quem falar com ele lê o
// correio todo. "Está numa rede privada" é uma suposição sobre a infraestrutura de amanhã, não uma
// propriedade do serviço. O Stalwart já assina — só temos de conferir.
//
// O que se confere: a chave de acesso, a data (contra repetições), e o HMAC sobre o pedido
// canónico. O corpo é conferido à parte, em passagem — ver corpoConferido.

const algorithm = "AWS4-HMAC-SHA256"

// desvioMaximo é a janela de aceitação do relógio. Quinze minutos é o que a AWS usa.
const maxSkew = 15 * time.Minute

type credential struct {
	access string
	secret string
	region string
}

func (c credential) check(r *http.Request) error {
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, algorithm+" ") {
		return errf("falta a autorização %s", algorithm)
	}
	parts := map[string]string{}
	for _, p := range strings.Split(strings.TrimPrefix(auth, algorithm+" "), ",") {
		key, value, ok := strings.Cut(strings.TrimSpace(p), "=")
		if ok {
			parts[key] = value
		}
	}

	rawCredential := parts["Credential"]
	signature := parts["Signature"]
	signed := parts["SignedHeaders"]
	if rawCredential == "" || signature == "" || signed == "" {
		return errf("autorização incompleta")
	}

	fields := strings.Split(rawCredential, "/")
	if len(fields) != 5 {
		return errf("credencial malformada")
	}
	if !hmac.Equal([]byte(fields[0]), []byte(c.access)) {
		return errf("chave de acesso desconhecida")
	}
	shortDate, region, service := fields[1], fields[2], fields[3]

	when := r.Header.Get("X-Amz-Date")
	if when == "" {
		return errf("falta o X-Amz-Date")
	}
	t, err := time.Parse("20060102T150405Z", when)
	if err != nil {
		return errf("X-Amz-Date inválido: %w", err)
	}
	// Sem esta janela, um pedido assinado capturado servia para sempre.
	if d := time.Since(t); d > maxSkew || d < -maxSkew {
		return errf("pedido fora da janela de %s (desvio %s)", maxSkew, d.Round(time.Second))
	}

	bodyHash := r.Header.Get("X-Amz-Content-Sha256")
	if bodyHash == "" {
		return errf("falta o X-Amz-Content-Sha256")
	}
	if strings.HasPrefix(bodyHash, "STREAMING-") {
		return errf("corpo assinado em fluxo não é suportado")
	}

	canonical := canonicalRequest(r, strings.Split(signed, ";"), bodyHash)
	scope := strings.Join([]string{shortDate, region, service, "aws4_request"}, "/")
	toSign := strings.Join([]string{
		algorithm, when, scope, hexSHA256([]byte(canonical)),
	}, "\n")

	want := hex.EncodeToString(hmacSHA256(signingKey(c.secret, shortDate, region, service), toSign))
	if !hmac.Equal([]byte(want), []byte(signature)) {
		return errf("assinatura não confere")
	}
	return nil
}

// pedidoCanonico monta a forma canónica do pedido, tal como a especificação manda.
func canonicalRequest(r *http.Request, signed []string, bodyHash string) string {
	sort.Strings(signed)

	var headers strings.Builder
	for _, name := range signed {
		headers.WriteString(name)
		headers.WriteByte(':')
		headers.WriteString(headerValue(r, name))
		headers.WriteByte('\n')
	}

	return strings.Join([]string{
		r.Method,
		canonicalPath(r.URL),
		canonicalQuery(r.URL),
		headers.String(),
		strings.Join(signed, ";"),
		bodyHash,
	}, "\n")
}

// valorCabecalho: o Host não vem no mapa de cabeçalhos do Go, vem no campo próprio.
func headerValue(r *http.Request, name string) string {
	if name == "host" {
		return strings.TrimSpace(r.Host)
	}
	values := r.Header.Values(http.CanonicalHeaderKey(name))
	for i, v := range values {
		values[i] = strings.Join(strings.Fields(v), " ")
	}
	return strings.Join(values, ",")
}

// caminhoCanonico usa o caminho tal como veio no pedido, sem descodificar: o cliente assinou o que
// escreveu, e normalizar aqui daria assinaturas diferentes para o mesmo pedido.
func canonicalPath(u *url.URL) string {
	if p := u.EscapedPath(); p != "" {
		return p
	}
	return "/"
}

func canonicalQuery(u *url.URL) string {
	values := u.Query()
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	for _, k := range keys {
		vs := values[k]
		sort.Strings(vs)
		for _, v := range vs {
			if b.Len() > 0 {
				b.WriteByte('&')
			}
			b.WriteString(escape(k))
			b.WriteByte('=')
			b.WriteString(escape(v))
		}
	}
	return b.String()
}

// escapar segue a codificação da AWS, que difere do url.QueryEscape em dois pontos: o espaço vai
// como %20 (e não +) e o til não é escapado.
func escape(s string) string {
	e := url.QueryEscape(s)
	e = strings.ReplaceAll(e, "+", "%20")
	return strings.ReplaceAll(e, "%7E", "~")
}

func signingKey(secret, data, region, service string) []byte {
	k := hmacSHA256([]byte("AWS4"+secret), data)
	k = hmacSHA256(k, region)
	k = hmacSHA256(k, service)
	return hmacSHA256(k, "aws4_request")
}

func hmacSHA256(key []byte, data string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(data))
	return m.Sum(nil)
}

func hexSHA256(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}
