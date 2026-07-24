// Package totp implementa TOTP (RFC 6238, HMAC-SHA1, 6 dígitos, período 30s) em Go puro —
// compatível com Google/Microsoft Authenticator. Sem dependências externas.
//
// Segurança: comparação de código em tempo constante; janela de ±1 passo (tolerância de relógio).
// O segredo é sensível — ver nota em db/migrations sobre cifrar em repouso no futuro.
package totp

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const (
	period = 30
	digits = 6
	issuer = "Beskos"
)

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// GenerateSecret cria um segredo base32 de 160 bits (20 bytes), padrão do RFC 4226.
func GenerateSecret() (string, error) {
	buf := make([]byte, 20)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return b32.EncodeToString(buf), nil
}

// ProvisioningURI devolve o otpauth:// para o QR code do app autenticador.
func ProvisioningURI(secret, account string) string {
	label := url.PathEscape(issuer + ":" + account)
	q := url.Values{}
	q.Set("secret", secret)
	q.Set("issuer", issuer)
	q.Set("algorithm", "SHA1")
	q.Set("digits", fmt.Sprint(digits))
	q.Set("period", fmt.Sprint(period))
	return "otpauth://totp/" + label + "?" + q.Encode()
}

// code calcula o TOTP para um contador (passo de tempo).
func code(secret string, counter uint64) (string, error) {
	key, err := b32.DecodeString(strings.ToUpper(strings.TrimSpace(secret)))
	if err != nil {
		return "", err
	}
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], counter)
	h := hmac.New(sha1.New, key)
	h.Write(msg[:])
	sum := h.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	bin := (uint32(sum[offset]&0x7f) << 24) | (uint32(sum[offset+1]) << 16) |
		(uint32(sum[offset+2]) << 8) | uint32(sum[offset+3])
	return fmt.Sprintf("%0*d", digits, bin%1_000_000), nil
}

// Validate confere um código no instante t, aceitando ±1 passo (janela de 90s). Tempo constante.
func Validate(secret, input string, t time.Time) bool {
	input = strings.TrimSpace(input)
	if len(input) != digits {
		return false
	}
	counter := uint64(t.Unix()) / period
	for _, delta := range []int64{0, -1, 1} {
		want, err := code(secret, uint64(int64(counter)+delta))
		if err != nil {
			return false
		}
		if subtle.ConstantTimeCompare([]byte(want), []byte(input)) == 1 {
			return true
		}
	}
	return false
}

// Now valida usando o relógio atual (conveniência).
func Now(secret, input string) bool { return Validate(secret, input, time.Now()) }

// ValidateStep confere o código e devolve o passo de tempo aceito, para o chamador impedir replay
// (rejeitar um passo já usado antes). ok=false se nenhum passo na janela ±1 bate. Tempo constante.
func ValidateStep(secret, input string, t time.Time) (step int64, ok bool) {
	input = strings.TrimSpace(input)
	if len(input) != digits {
		return 0, false
	}
	counter := int64(uint64(t.Unix()) / period)
	for _, delta := range []int64{0, -1, 1} {
		s := counter + delta
		want, err := code(secret, uint64(s))
		if err != nil {
			return 0, false
		}
		if subtle.ConstantTimeCompare([]byte(want), []byte(input)) == 1 {
			return s, true
		}
	}
	return 0, false
}

// NowStep valida com o relógio atual e devolve o passo aceito (para anti-replay).
func NowStep(secret, input string) (int64, bool) { return ValidateStep(secret, input, time.Now()) }
