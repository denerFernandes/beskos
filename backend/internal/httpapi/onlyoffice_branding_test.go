package httpapi

import (
	"strings"
	"testing"
)

// A marca só chega ao editor se `logo` e `customer` forem para o config; e a atribuição à
// Ascensio System SIA tem de viajar sempre, porque é ela que cumpre a AGPL no "Sobre".
func TestApplyBrandingSetsLogoAndAttribution(t *testing.T) {
	o := &onlyoffice{publicURL: "http://localhost:8082"}
	c := map[string]any{"uiTheme": "theme-light"}
	o.applyBranding(c)

	logo, ok := c["logo"].(map[string]any)
	if !ok {
		t.Fatal("customization.logo em falta — o cabeçalho ficaria com a marca original")
	}
	// URL curta, servida pelo próprio Document Server. Um data: URI aqui rebenta a query string
	// do iframe do editor (nginx 414) — foi assim que partiu da primeira vez.
	for _, k := range []string{"image", "imageLight", "imageDark"} {
		v, _ := logo[k].(string)
		if !strings.HasPrefix(v, "http://localhost:8082"+brandLogoPath) {
			t.Errorf("logo.%s devia ser uma URL no Document Server, veio %q", k, v)
		}
		if len(v) > 200 {
			t.Errorf("logo.%s tem %d chars — vai para a query string do iframe, tem de ser curta", k, len(v))
		}
	}

	customer, ok := c["customer"].(map[string]any)
	if !ok {
		t.Fatal("customization.customer em falta — sem ele o Sobre não mostra a nossa identificação")
	}
	if customer["name"] != "Beskos" {
		t.Errorf("customer.name = %v, queria Beskos", customer["name"])
	}
	info, _ := customer["info"].(string)
	for _, want := range []string{"Ascensio System SIA", "ONLYOFFICE", "AGPL", officeModifiedOn} {
		if !strings.Contains(info, want) {
			t.Errorf("customer.info não menciona %q: %q", want, info)
		}
	}
	// Sem o "Sobre" visível não há aviso legal na interface — e com o patch o DS já não o
	// força sozinho. Tem de ir sempre ligado.
	if c["about"] != true {
		t.Error("customization.about tem de ir true: é onde a atribuição fica visível ao utilizador")
	}
	if c["uiTheme"] != "theme-light" {
		t.Error("applyBranding não pode mexer nos outros parâmetros de customização")
	}
}

// Com ONLYOFFICE_SOURCE_URL configurado, o aviso passa a incluir a oferta de fonte (AGPL §13).
func TestAttributionNoticeIncludesSourceURL(t *testing.T) {
	const src = "https://github.com/beskos/docs-server"
	o := &onlyoffice{sourceURL: src}
	if got := o.attributionNotice(); !strings.Contains(got, src) {
		t.Errorf("aviso sem o endereço do fonte modificado: %q", got)
	}
	if got := (&onlyoffice{}).attributionNotice(); strings.Contains(got, "Fonte das modificações") {
		t.Error("sem sourceURL o aviso não deve prometer um endereço que não temos")
	}
}
