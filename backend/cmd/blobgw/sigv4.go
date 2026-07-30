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

const algoritmo = "AWS4-HMAC-SHA256"

// desvioMaximo é a janela de aceitação do relógio. Quinze minutos é o que a AWS usa.
const desvioMaximo = 15 * time.Minute

type credencial struct {
	acesso  string
	segredo string
	regiao  string
}

func (c credencial) verificar(r *http.Request) error {
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, algoritmo+" ") {
		return errf("falta a autorização %s", algoritmo)
	}
	partes := map[string]string{}
	for _, p := range strings.Split(strings.TrimPrefix(auth, algoritmo+" "), ",") {
		chave, valor, ok := strings.Cut(strings.TrimSpace(p), "=")
		if ok {
			partes[chave] = valor
		}
	}

	credencialBruta := partes["Credential"]
	assinatura := partes["Signature"]
	assinados := partes["SignedHeaders"]
	if credencialBruta == "" || assinatura == "" || assinados == "" {
		return errf("autorização incompleta")
	}

	campos := strings.Split(credencialBruta, "/")
	if len(campos) != 5 {
		return errf("credencial malformada")
	}
	if !hmac.Equal([]byte(campos[0]), []byte(c.acesso)) {
		return errf("chave de acesso desconhecida")
	}
	dataCurta, regiao, servico := campos[1], campos[2], campos[3]

	quando := r.Header.Get("X-Amz-Date")
	if quando == "" {
		return errf("falta o X-Amz-Date")
	}
	t, err := time.Parse("20060102T150405Z", quando)
	if err != nil {
		return errf("X-Amz-Date inválido: %w", err)
	}
	// Sem esta janela, um pedido assinado capturado servia para sempre.
	if d := time.Since(t); d > desvioMaximo || d < -desvioMaximo {
		return errf("pedido fora da janela de %s (desvio %s)", desvioMaximo, d.Round(time.Second))
	}

	hashCorpo := r.Header.Get("X-Amz-Content-Sha256")
	if hashCorpo == "" {
		return errf("falta o X-Amz-Content-Sha256")
	}
	if strings.HasPrefix(hashCorpo, "STREAMING-") {
		return errf("corpo assinado em fluxo não é suportado")
	}

	canonico := pedidoCanonico(r, strings.Split(assinados, ";"), hashCorpo)
	escopo := strings.Join([]string{dataCurta, regiao, servico, "aws4_request"}, "/")
	paraAssinar := strings.Join([]string{
		algoritmo, quando, escopo, hexSHA256([]byte(canonico)),
	}, "\n")

	esperada := hex.EncodeToString(hmacSHA256(chaveDeAssinatura(c.segredo, dataCurta, regiao, servico), paraAssinar))
	if !hmac.Equal([]byte(esperada), []byte(assinatura)) {
		return errf("assinatura não confere")
	}
	return nil
}

// pedidoCanonico monta a forma canónica do pedido, tal como a especificação manda.
func pedidoCanonico(r *http.Request, assinados []string, hashCorpo string) string {
	sort.Strings(assinados)

	var cabecalhos strings.Builder
	for _, nome := range assinados {
		cabecalhos.WriteString(nome)
		cabecalhos.WriteByte(':')
		cabecalhos.WriteString(valorCabecalho(r, nome))
		cabecalhos.WriteByte('\n')
	}

	return strings.Join([]string{
		r.Method,
		caminhoCanonico(r.URL),
		consultaCanonica(r.URL),
		cabecalhos.String(),
		strings.Join(assinados, ";"),
		hashCorpo,
	}, "\n")
}

// valorCabecalho: o Host não vem no mapa de cabeçalhos do Go, vem no campo próprio.
func valorCabecalho(r *http.Request, nome string) string {
	if nome == "host" {
		return strings.TrimSpace(r.Host)
	}
	valores := r.Header.Values(http.CanonicalHeaderKey(nome))
	for i, v := range valores {
		valores[i] = strings.Join(strings.Fields(v), " ")
	}
	return strings.Join(valores, ",")
}

// caminhoCanonico usa o caminho tal como veio no pedido, sem descodificar: o cliente assinou o que
// escreveu, e normalizar aqui daria assinaturas diferentes para o mesmo pedido.
func caminhoCanonico(u *url.URL) string {
	if p := u.EscapedPath(); p != "" {
		return p
	}
	return "/"
}

func consultaCanonica(u *url.URL) string {
	valores := u.Query()
	chaves := make([]string, 0, len(valores))
	for k := range valores {
		chaves = append(chaves, k)
	}
	sort.Strings(chaves)

	var b strings.Builder
	for _, k := range chaves {
		vs := valores[k]
		sort.Strings(vs)
		for _, v := range vs {
			if b.Len() > 0 {
				b.WriteByte('&')
			}
			b.WriteString(escapar(k))
			b.WriteByte('=')
			b.WriteString(escapar(v))
		}
	}
	return b.String()
}

// escapar segue a codificação da AWS, que difere do url.QueryEscape em dois pontos: o espaço vai
// como %20 (e não +) e o til não é escapado.
func escapar(s string) string {
	e := url.QueryEscape(s)
	e = strings.ReplaceAll(e, "+", "%20")
	return strings.ReplaceAll(e, "%7E", "~")
}

func chaveDeAssinatura(segredo, data, regiao, servico string) []byte {
	k := hmacSHA256([]byte("AWS4"+segredo), data)
	k = hmacSHA256(k, regiao)
	k = hmacSHA256(k, servico)
	return hmacSHA256(k, "aws4_request")
}

func hmacSHA256(chave []byte, dados string) []byte {
	m := hmac.New(sha256.New, chave)
	m.Write([]byte(dados))
	return m.Sum(nil)
}

func hexSHA256(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}
