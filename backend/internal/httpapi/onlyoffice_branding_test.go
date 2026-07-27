package httpapi

import (
	"strings"
	"testing"
)

// A marca só chega ao editor se `logo` e `customer` forem para o config; e a atribuição à
// Ascensio System SIA tem de viajar sempre, porque é ela que cumpre a AGPL no "Sobre".
func TestApplyBrandingSetsLogoAndAttribution(t *testing.T) {
	o := &onlyoffice{}
	c := map[string]any{"uiTheme": "theme-light"}
	o.applyBranding(c)

	logo, ok := c["logo"].(map[string]any)
	if !ok {
		t.Fatal("customization.logo em falta — o cabeçalho ficaria com a marca original")
	}
	for _, k := range []string{"image", "imageLight", "imageDark"} {
		v, _ := logo[k].(string)
		if !strings.HasPrefix(v, "data:image/svg+xml;base64,") {
			t.Errorf("logo.%s devia ser um data: URI, veio %q", k, v)
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
