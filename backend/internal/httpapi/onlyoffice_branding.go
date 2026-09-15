package httpapi

// Marca Beskos no cabeçalho e no "Sobre" do editor de office.
//
// O Document Server Community só honra estes parâmetros com o patch de branding do arranque
// (deploy/onlyoffice-branding-patch.sh). Sem o patch, o editor descarta-os em silêncio e o
// cabeçalho fica com a marca original — a app continua a funcionar na mesma.
//
// As imagens são URLs servidas pelo PRÓPRIO Document Server (o mesmo patch copia-as para o
// webroot dele). Não usar data: URI aqui: o api.js do DS passa logo.{image,imageDark,imageLight}
// na query string do iframe do editor, e três data URIs percent-encoded estouram o limite da
// linha de pedido do nginx — o editor abre com "414 Request-URI Too Large".
//
// Atribuição (AGPL v3 §5 e §13, mais a política de marcas da Ascensio System SIA): o diálogo
// "Sobre" continua a mostrar "Powered by / Ascensio System SIA / onlyoffice.com" — é o próprio
// Document Server que o revela quando enviamos `customer` — e nós acrescentamos em `customer.info`
// a nota de versão modificada, com data e o endereço do fonte modificado. Nada disto é opcional:
// servimos o editor pela rede, logo devemos o aviso e a oferta de fonte a quem o usa.

// brandLogoPath é onde o patch de arranque deixa os logos dentro do Document Server.
const brandLogoPath = "/web-apps/apps/common/main/resources/img/beskos/"

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
	light := o.publicURL + brandLogoPath + "wordmark-light.svg"
	dark := o.publicURL + brandLogoPath + "wordmark-dark.svg"

	customization["logo"] = map[string]any{
		"image":      light,
		"imageLight": light,
		"imageDark":  dark,
		"url":        o.brandSiteURL(),
	}
	// O "Sobre" é onde vive o aviso legal, por isso é ligado EXPLICITAMENTE. Com o patch, o
	// `canBrandingExt` fica true e o Document Server deixa de o forçar sozinho — sem esta linha,
	// bastava alguém pôr `about: false` na customização para a atribuição desaparecer da UI.
	customization["about"] = true
	customization["customer"] = map[string]any{
		"name":     "Beskos",
		"logo":     light,
		"logoDark": dark,
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

// brandSiteURL is what the editor's logo links to. Its caller (cmd/api's main) already resolves
// the fallback chain (PUBLIC_URL, then BRAND_URL); an empty result here just omits the link.
func (o *onlyoffice) brandSiteURL() string { return o.brandURL }
