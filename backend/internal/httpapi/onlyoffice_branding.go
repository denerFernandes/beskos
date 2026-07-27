package httpapi

import (
	_ "embed"
	"encoding/base64"
)

// Marca Beskos no cabeçalho e no "Sobre" do editor de office.
//
// O Document Server Community só honra estes parâmetros com o patch de branding do arranque
// (deploy/onlyoffice-branding-patch.sh). Sem o patch, o editor descarta-os em silêncio e o
// cabeçalho fica com a marca original — a app continua a funcionar na mesma.
//
// Atribuição (AGPL v3 §5 e §13, mais a política de marcas da Ascensio System SIA): o diálogo
// "Sobre" continua a mostrar "Powered by / Ascensio System SIA / onlyoffice.com" — é o próprio
// Document Server que o revela quando enviamos `customer` — e nós acrescentamos em `customer.info`
// a nota de versão modificada, com data e o endereço do fonte modificado. Nada disto é opcional:
// servimos o editor pela rede, logo devemos o aviso e a oferta de fonte a quem o usa.

//go:embed assets/beskos-wordmark-light.svg
var wordmarkLightSVG []byte

//go:embed assets/beskos-wordmark-dark.svg
var wordmarkDarkSVG []byte

// Logos em data: URI — o editor corre noutra origem (o DS) e assim a marca não depende de a
// nossa app estar publicamente alcançável, nem em dev, nem no app desktop, nem no visualizador
// público de links.
var (
	logoForLightHeader = svgDataURI(wordmarkLightSVG)
	logoForDarkHeader  = svgDataURI(wordmarkDarkSVG)
)

func svgDataURI(b []byte) string {
	return "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString(b)
}

// officeModifiedOn é a data da modificação do Document Server, exigida pelos termos adicionais
// da AGPL ("prominent notices ... including the date of such modification"). Atualizar sempre
// que o patch de branding mudar.
const officeModifiedOn = "2026-07-27"

const officeUpstreamNotice = "Baseado em ONLYOFFICE® Docs Community Edition, da Ascensio System SIA. " +
	"Versão modificada por Beskos (interface e marca) em " + officeModifiedOn + " — não é o software " +
	"original nem é endossada pela Ascensio System SIA. Licença: GNU AGPL v3."

// applyBranding acrescenta a marca Beskos ao objeto customization do config do editor. Recebe o
// mapa já montado pelo handler para não duplicar a construção do config em dois sítios.
func (o *onlyoffice) applyBranding(customization map[string]any) {
	customization["logo"] = map[string]any{
		"image":      logoForLightHeader,
		"imageLight": logoForLightHeader,
		"imageDark":  logoForDarkHeader,
		"url":        o.brandSiteURL(),
	}
	customization["customer"] = map[string]any{
		"name":     "Beskos",
		"logo":     logoForLightHeader,
		"logoDark": logoForDarkHeader,
		"www":      o.brandSiteURL(),
		"info":     o.attributionNotice(),
	}
}

// attributionNotice é o texto que aparece no "Sobre". Junta a oferta de fonte modificada (§13)
// quando ONLYOFFICE_SOURCE_URL está configurado; sem ela, o aviso sai na mesma, mas incompleto —
// por isso a config recusa arrancar sem o endereço em produção.
func (o *onlyoffice) attributionNotice() string {
	if o.sourceURL == "" {
		return officeUpstreamNotice
	}
	return officeUpstreamNotice + " Fonte das modificações: " + o.sourceURL
}

func (o *onlyoffice) brandSiteURL() string {
	if o.brandURL == "" {
		return "https://beskos.com"
	}
	return o.brandURL
}
