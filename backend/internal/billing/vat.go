package billing

import (
	"regexp"
	"strings"
)

// IVA da venda. O vendedor está na POLÓNIA, por isso a taxa de referência é a polaca (23%).
//
// Regras aplicadas (serviço digital B2B/B2C):
//   - Fora da UE .......................... 0% (fora do âmbito do IVA da UE)
//   - Polónia (empresa ou particular) ..... 23%
//   - UE, empresa com NIF intracomunitário  0%, autoliquidação pelo cliente (art. 196 da
//     Diretiva 2006/112/CE) — cobrar IVA aqui seria cobrar a dobrar ao cliente
//   - UE, particular ou empresa sem NIF ... 23%
//
// A validação do NIF é de FORMATO (prefixo do país + máscara). A confirmação na VIES é um passo
// seguinte: enquanto não existir, uma empresa da UE com um número mal escrito paga IVA em vez de
// autoliquidar — erra-se do lado seguro (a mais), nunca a menos.

const (
	// SellerCountry é o país do vendedor: define a taxa aplicada e quem autoliquida.
	SellerCountry = "PL"
	// PolishVATRate em pontos base (2300 = 23,00%) — inteiro para não arrastar erro de vírgula
	// flutuante no valor cobrado.
	PolishVATRate = 2300
)

// euCountries são os 27 estados-membros (ISO 3166-1 alfa-2).
var euCountries = map[string]bool{
	"AT": true, "BE": true, "BG": true, "HR": true, "CY": true, "CZ": true, "DK": true,
	"EE": true, "FI": true, "FR": true, "DE": true, "GR": true, "HU": true, "IE": true,
	"IT": true, "LV": true, "LT": true, "LU": true, "MT": true, "NL": true, "PL": true,
	"PT": true, "RO": true, "SK": true, "SI": true, "ES": true, "SE": true,
}

// IsEU diz se o país (alfa-2) é da União Europeia.
func IsEU(country string) bool { return euCountries[strings.ToUpper(strings.TrimSpace(country))] }

// Máscara do NIF intracomunitário por país. A Grécia usa o prefixo EL (e não GR) no número.
var vatPatterns = map[string]*regexp.Regexp{
	"AT": regexp.MustCompile(`^ATU[0-9]{8}$`),
	"BE": regexp.MustCompile(`^BE[01][0-9]{9}$`),
	"BG": regexp.MustCompile(`^BG[0-9]{9,10}$`),
	"HR": regexp.MustCompile(`^HR[0-9]{11}$`),
	"CY": regexp.MustCompile(`^CY[0-9]{8}[A-Z]$`),
	"CZ": regexp.MustCompile(`^CZ[0-9]{8,10}$`),
	"DK": regexp.MustCompile(`^DK[0-9]{8}$`),
	"EE": regexp.MustCompile(`^EE[0-9]{9}$`),
	"FI": regexp.MustCompile(`^FI[0-9]{8}$`),
	"FR": regexp.MustCompile(`^FR[0-9A-Z]{2}[0-9]{9}$`),
	"DE": regexp.MustCompile(`^DE[0-9]{9}$`),
	"GR": regexp.MustCompile(`^EL[0-9]{9}$`),
	"HU": regexp.MustCompile(`^HU[0-9]{8}$`),
	"IE": regexp.MustCompile(`^IE([0-9]{7}[A-W]|[0-9][A-Z*+][0-9]{5}[A-W]|[0-9]{7}[A-W][AH])$`),
	"IT": regexp.MustCompile(`^IT[0-9]{11}$`),
	"LV": regexp.MustCompile(`^LV[0-9]{11}$`),
	"LT": regexp.MustCompile(`^LT([0-9]{9}|[0-9]{12})$`),
	"LU": regexp.MustCompile(`^LU[0-9]{8}$`),
	"MT": regexp.MustCompile(`^MT[0-9]{8}$`),
	"NL": regexp.MustCompile(`^NL[0-9]{9}B[0-9]{2}$`),
	"PL": regexp.MustCompile(`^PL[0-9]{10}$`),
	"PT": regexp.MustCompile(`^PT[0-9]{9}$`),
	"RO": regexp.MustCompile(`^RO[0-9]{2,10}$`),
	"SK": regexp.MustCompile(`^SK[0-9]{10}$`),
	"SI": regexp.MustCompile(`^SI[0-9]{8}$`),
	"ES": regexp.MustCompile(`^ES[0-9A-Z][0-9]{7}[0-9A-Z]$`),
	"SE": regexp.MustCompile(`^SE[0-9]{12}$`),
}

// NormalizeVATID tira espaços/pontos e põe em maiúsculas. Sem prefixo de país, usa o do endereço —
// é o erro mais comum de quem preenche ("1234567890" em vez de "PL1234567890").
func NormalizeVATID(taxID, country string) string {
	s := strings.ToUpper(taxID)
	s = strings.Map(func(r rune) rune {
		if r == ' ' || r == '.' || r == '-' || r == '/' {
			return -1
		}
		return r
	}, s)
	if s == "" {
		return ""
	}
	cc := strings.ToUpper(strings.TrimSpace(country))
	if cc == "GR" {
		cc = "EL" // a Grécia declara-se EL no número
	}
	if cc != "" && len(s) > 1 && !(s[0] >= 'A' && s[0] <= 'Z' && s[1] >= 'A' && s[1] <= 'Z') {
		s = cc + s
	}
	return s
}

// ValidVATID valida o FORMATO do NIF intracomunitário para o país dado.
func ValidVATID(taxID, country string) bool {
	cc := strings.ToUpper(strings.TrimSpace(country))
	re, ok := vatPatterns[cc]
	if !ok {
		return false
	}
	return re.MatchString(NormalizeVATID(taxID, cc))
}

// Quote é o resumo de valores de uma subscrição, já com IVA.
type Quote struct {
	Currency      string `json:"currency"`
	Seats         int    `json:"seats"`
	UnitCents     int    `json:"unit_cents"`     // preço por utilizador/mês
	SubtotalCents int    `json:"subtotal_cents"` // unit × seats
	VATRateBps    int    `json:"vat_rate_bps"`   // 2300 = 23%
	VATCents      int    `json:"vat_cents"`
	TotalCents    int    `json:"total_cents"`
	ReverseCharge bool   `json:"reverse_charge"` // IVA autoliquidado pelo cliente (UE, B2B)
	TaxIDValid    bool   `json:"tax_id_valid"`   // formato do NIF aceite
	Country       string `json:"country"`
}

// QuoteFor calcula o resumo com IVA. `kind` é "company" ou "individual".
func QuoteFor(currency string, unitCents, seats int, kind, country, taxID string) Quote {
	if seats < 1 {
		seats = 1
	}
	cc := strings.ToUpper(strings.TrimSpace(country))
	q := Quote{
		Currency:      currency,
		Seats:         seats,
		UnitCents:     unitCents,
		SubtotalCents: unitCents * seats,
		Country:       cc,
	}
	isCompany := kind == "company"
	q.TaxIDValid = isCompany && ValidVATID(taxID, cc)

	switch {
	case !IsEU(cc):
		// Fora da UE não se liquida IVA polaco.
	case cc == SellerCountry:
		q.VATRateBps = PolishVATRate
	case isCompany && q.TaxIDValid:
		q.ReverseCharge = true // 0% — quem liquida é o cliente no país dele
	default:
		q.VATRateBps = PolishVATRate
	}
	// Arredonda ao cêntimo (meio para cima), como sai na fatura.
	q.VATCents = (q.SubtotalCents*q.VATRateBps + 5000) / 10000
	q.TotalCents = q.SubtotalCents + q.VATCents
	return q
}
