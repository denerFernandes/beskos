package office

import (
	"fmt"
	"strings"
)

// buildPptx constrói um .pptx mínimo mas válido.
//
// É o formato mais exigente dos três: um slide não abre sozinho — precisa de um esquema (layout),
// que precisa de um modelo global (master), que precisa de um tema. Um pacote sem qualquer um deles
// é recusado pelo PowerPoint com "o ficheiro está corrompido", sem dizer o que falta. Daí estarem
// todos aqui, mesmo os que não se veem.
//
// O desenho é deliberadamente sóbrio: título e pontos, tipos de letra do tema. Gerar slides bonitos
// é outro problema, e resolvê-lo com XML à mão seria a troca errada.
func buildPptx(p Plan) ([]byte, error) {
	slides := p.Slides
	if len(slides) == 0 {
		// Uma apresentação sem slides não é um ficheiro que se abra. Se houver título, faz-se o
		// slide de capa com ele; senão, um slide vazio — melhor do que um erro por um plano magro.
		slides = []Slide{{Title: p.Title}}
	}

	parts := []part{}
	var slideRefs, presRels, overrides strings.Builder

	for i, s := range slides {
		n := i + 1
		path := fmt.Sprintf("ppt/slides/slide%d.xml", n)
		rid := fmt.Sprintf("rId%d", n+1) // rId1 fica para o master
		slideRefs.WriteString(fmt.Sprintf(`<p:sldId id="%d" r:id="%s"/>`, 255+n, rid))
		presRels.WriteString(fmt.Sprintf(
			`<Relationship Id="%s" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide" Target="slides/slide%d.xml"/>`, rid, n))
		overrides.WriteString(fmt.Sprintf(
			`<Override PartName="/%s" ContentType="application/vnd.openxmlformats-officedocument.presentationml.slide+xml"/>`, path))
		parts = append(parts,
			part{path, slideXML(s)},
			part{fmt.Sprintf("ppt/slides/_rels/slide%d.xml.rels", n), xmlHeader +
				`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
				`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideLayout" Target="../slideLayouts/slideLayout1.xml"/>` +
				`</Relationships>`})
	}

	head := []part{
		{"[Content_Types].xml", xmlHeader +
			`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
			`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
			`<Default Extension="xml" ContentType="application/xml"/>` +
			`<Override PartName="/ppt/presentation.xml" ContentType="` + MIME(KindPresentation) + `.main+xml"/>` +
			`<Override PartName="/ppt/slideMasters/slideMaster1.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.slideMaster+xml"/>` +
			`<Override PartName="/ppt/slideLayouts/slideLayout1.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.slideLayout+xml"/>` +
			`<Override PartName="/ppt/theme/theme1.xml" ContentType="application/vnd.openxmlformats-officedocument.theme+xml"/>` +
			overrides.String() +
			`</Types>`},
		{"_rels/.rels", xmlHeader +
			`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="ppt/presentation.xml"/>` +
			`</Relationships>`},
		{"ppt/presentation.xml", xmlHeader +
			`<p:presentation xmlns:a="` + nsDrawing + `" xmlns:r="` + nsRel + `" xmlns:p="` + nsPres + `">` +
			`<p:sldMasterIdLst><p:sldMasterId id="2147483648" r:id="rId1"/></p:sldMasterIdLst>` +
			`<p:sldIdLst>` + slideRefs.String() + `</p:sldIdLst>` +
			// 16:9 em EMU (English Metric Units). É o formato que toda a gente espera hoje.
			`<p:sldSz cx="12192000" cy="6858000"/><p:notesSz cx="6858000" cy="9144000"/>` +
			`</p:presentation>`},
		{"ppt/_rels/presentation.xml.rels", xmlHeader +
			`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideMaster" Target="slideMasters/slideMaster1.xml"/>` +
			presRels.String() +
			fmt.Sprintf(`<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/theme" Target="theme/theme1.xml"/>`, len(slides)+2) +
			`</Relationships>`},
		{"ppt/slideMasters/slideMaster1.xml", slideMasterXML},
		{"ppt/slideMasters/_rels/slideMaster1.xml.rels", xmlHeader +
			`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideLayout" Target="../slideLayouts/slideLayout1.xml"/>` +
			`<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/theme" Target="../theme/theme1.xml"/>` +
			`</Relationships>`},
		{"ppt/slideLayouts/slideLayout1.xml", slideLayoutXML},
		{"ppt/slideLayouts/_rels/slideLayout1.xml.rels", xmlHeader +
			`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideMaster" Target="../slideMasters/slideMaster1.xml"/>` +
			`</Relationships>`},
		{"ppt/theme/theme1.xml", themeXML},
	}
	return zipParts(append(head, parts...))
}

const (
	nsDrawing = "http://schemas.openxmlformats.org/drawingml/2006/main"
	nsRel     = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"
	nsPres    = "http://schemas.openxmlformats.org/presentationml/2006/main"
)

// Paleta do desenho. srgbClr explícito (não schemeClr) para o resultado ser igual em qualquer
// leitor, sem depender do mapa de cores do tema.
const (
	accentHex  = "116466" // verde Beskos — faixas de título e capa
	accent2Hex = "0C4A4C" // tom mais escuro — realce fino sob a faixa
	lightHex   = "F4F1EC" // creme — régua decorativa e subtítulo na capa
	inkHex     = "1C1B1A" // quase-preto — texto dos pontos
)

// Slide inteiro: 16:9 em EMU. Constantes para posicionar as caixas sem números soltos no meio.
const (
	slideW   = 12192000
	slideH   = 6858000
	bandH    = 1500000 // altura da faixa de título nos slides de conteúdo
	marginX  = 838200
	contentW = slideW - 2*marginX
)

// slideXML escolhe o desenho pelo conteúdo: um slide SEM pontos é uma CAPA (fundo colorido, título
// grande e centrado); um slide COM pontos é um slide de CONTEÚDO (faixa de título colorida + pontos
// estilizados). Assim o slide de abertura, que o modelo costuma fazer só com título, ganha ar de
// capa sem ser preciso um campo novo no plano.
func slideXML(s Slide) string {
	if !hasBullets(s) {
		return coverSlide(s)
	}
	return contentSlide(s)
}

func hasBullets(s Slide) bool {
	for _, b := range s.Bullets {
		if strings.TrimSpace(b) != "" {
			return true
		}
	}
	return false
}

// coverSlide: fundo cheio na cor de destaque, régua decorativa e título grande em branco.
func coverSlide(s Slide) string {
	shapes := rectXML(2, "BG", 0, 0, slideW, slideH, accentHex) +
		rectXML(3, "Rule", marginX, 3630000, 2600000, 50000, lightHex) +
		textBoxXML(4, "Title", marginX, 2500000, contentW, 1300000, "b",
			paraXML(s.Title, 5400, "FFFFFF", true, "l"))
	return slideWrap(shapes)
}

// contentSlide: faixa de título colorida no topo (título branco) e pontos por baixo em texto escuro,
// cada um com um marcador na cor de destaque.
func contentSlide(s Slide) string {
	var bullets strings.Builder
	for _, b := range s.Bullets {
		if strings.TrimSpace(b) == "" {
			continue
		}
		bullets.WriteString(bulletXML(b))
	}
	if bullets.Len() == 0 {
		bullets.WriteString(`<a:p/>`)
	}
	shapes := rectXML(2, "TitleBand", 0, 0, slideW, bandH, accentHex) +
		rectXML(3, "Accent", 0, bandH, slideW, 60000, accent2Hex) +
		textBoxXML(4, "Title", marginX, 0, contentW, bandH, "ctr",
			paraXML(s.Title, 3200, "FFFFFF", true, "l")) +
		textBoxXML(5, "Content", marginX, bandH+400000, contentW, slideH-bandH-800000, "t", bullets.String())
	return slideWrap(shapes)
}

// slideWrap embrulha as formas no XML de um slide.
func slideWrap(shapes string) string {
	return xmlHeader +
		`<p:sld xmlns:a="` + nsDrawing + `" xmlns:r="` + nsRel + `" xmlns:p="` + nsPres + `">` +
		`<p:cSld><p:spTree>` +
		`<p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr>` +
		`<p:grpSpPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="0" cy="0"/>` +
		`<a:chOff x="0" y="0"/><a:chExt cx="0" cy="0"/></a:xfrm></p:grpSpPr>` +
		shapes +
		`</p:spTree></p:cSld><p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr></p:sld>`
}

// rectXML é um retângulo preenchido a cor sólida (faixas, fundo, réguas). Sem texto.
func rectXML(id int, name string, x, y, cx, cy int, fill string) string {
	return fmt.Sprintf(
		`<p:sp><p:nvSpPr><p:cNvPr id="%d" name="%s"/><p:cNvSpPr/><p:nvPr/></p:nvSpPr>`+
			`<p:spPr><a:xfrm><a:off x="%d" y="%d"/><a:ext cx="%d" cy="%d"/></a:xfrm>`+
			`<a:prstGeom prst="rect"><a:avLst/></a:prstGeom>`+
			`<a:solidFill><a:srgbClr val="%s"/></a:solidFill><a:ln><a:noFill/></a:ln></p:spPr>`+
			`<p:txBody><a:bodyPr/><a:lstStyle/><a:p/></p:txBody></p:sp>`,
		id, name, x, y, cx, cy, fill)
}

// textBoxXML é uma caixa de texto posicionada; anchor é o alinhamento vertical (t|ctr|b).
func textBoxXML(id int, name string, x, y, cx, cy int, anchor, body string) string {
	return fmt.Sprintf(
		`<p:sp><p:nvSpPr><p:cNvPr id="%d" name="%s"/><p:cNvSpPr txBox="1"/><p:nvPr/></p:nvSpPr>`+
			`<p:spPr><a:xfrm><a:off x="%d" y="%d"/><a:ext cx="%d" cy="%d"/></a:xfrm>`+
			`<a:prstGeom prst="rect"><a:avLst/></a:prstGeom></p:spPr>`+
			`<p:txBody><a:bodyPr wrap="square" anchor="%s"><a:normAutofit/></a:bodyPr><a:lstStyle/>%s</p:txBody></p:sp>`,
		id, name, x, y, cx, cy, anchor, body)
}

// paraXML é um parágrafo com uma corrida de texto formatada (tamanho, cor, negrito, alinhamento).
func paraXML(text string, sz int, color string, bold bool, align string) string {
	b := "0"
	if bold {
		b = "1"
	}
	return fmt.Sprintf(
		`<a:p><a:pPr algn="%s"/><a:r><a:rPr lang="en-US" sz="%d" b="%s"><a:solidFill><a:srgbClr val="%s"/></a:solidFill></a:rPr><a:t>%s</a:t></a:r></a:p>`,
		align, sz, b, color, esc(text))
}

// bulletXML é um ponto com marcador na cor de destaque e texto escuro, com recuo pendente.
func bulletXML(text string) string {
	return `<a:p><a:pPr marL="285750" indent="-285750"><a:spcBef><a:spcPts val="600"/></a:spcBef>` +
		`<a:buClr><a:srgbClr val="` + accentHex + `"/></a:buClr><a:buFont typeface="Arial"/><a:buChar char="&#8226;"/></a:pPr>` +
		`<a:r><a:rPr lang="en-US" sz="2000"><a:solidFill><a:srgbClr val="` + inkHex + `"/></a:solidFill></a:rPr>` +
		`<a:t>` + esc(text) + `</a:t></a:r></a:p>`
}

// slideMasterXML é o modelo global. Não se vê, mas sem ele o pacote é recusado.
const slideMasterXML = xmlHeader +
	`<p:sldMaster xmlns:a="` + nsDrawing + `" xmlns:r="` + nsRel + `" xmlns:p="` + nsPres + `">` +
	`<p:cSld><p:spTree>` +
	`<p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr>` +
	`<p:grpSpPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="0" cy="0"/>` +
	`<a:chOff x="0" y="0"/><a:chExt cx="0" cy="0"/></a:xfrm></p:grpSpPr>` +
	`</p:spTree></p:cSld>` +
	// O mapa de cores liga os papéis (fundo, texto, destaque) às cores do tema. É obrigatório.
	`<p:clrMap bg1="lt1" tx1="dk1" bg2="lt2" tx2="dk2" accent1="accent1" accent2="accent2" ` +
	`accent3="accent3" accent4="accent4" accent5="accent5" accent6="accent6" hlink="hlink" folHlink="folHlink"/>` +
	`<p:sldLayoutIdLst><p:sldLayoutId id="2147483649" r:id="rId1"/></p:sldLayoutIdLst>` +
	`</p:sldMaster>`

// slideLayoutXML é o esquema que os slides dizem seguir.
const slideLayoutXML = xmlHeader +
	`<p:sldLayout xmlns:a="` + nsDrawing + `" xmlns:r="` + nsRel + `" xmlns:p="` + nsPres + `" type="blank">` +
	`<p:cSld name="Blank"><p:spTree>` +
	`<p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr>` +
	`<p:grpSpPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="0" cy="0"/>` +
	`<a:chOff x="0" y="0"/><a:chExt cx="0" cy="0"/></a:xfrm></p:grpSpPr>` +
	`</p:spTree></p:cSld><p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr></p:sldLayout>`

// themeXML é o tema mínimo. O esquema exige as seis cores de destaque, as duas de ligação, as duas
// claras e as duas escuras, mais os esquemas de tipo de letra e de formato — nenhum é opcional.
const themeXML = xmlHeader +
	`<a:theme xmlns:a="` + nsDrawing + `" name="Beskos">` +
	`<a:themeElements>` +
	`<a:clrScheme name="Beskos">` +
	`<a:dk1><a:sysClr val="windowText" lastClr="000000"/></a:dk1>` +
	`<a:lt1><a:sysClr val="window" lastClr="FFFFFF"/></a:lt1>` +
	`<a:dk2><a:srgbClr val="1C1B1A"/></a:dk2>` +
	`<a:lt2><a:srgbClr val="F4F1EC"/></a:lt2>` +
	`<a:accent1><a:srgbClr val="116466"/></a:accent1>` +
	`<a:accent2><a:srgbClr val="3B6FB0"/></a:accent2>` +
	`<a:accent3><a:srgbClr val="2E7D5B"/></a:accent3>` +
	`<a:accent4><a:srgbClr val="C0692F"/></a:accent4>` +
	`<a:accent5><a:srgbClr val="8257C7"/></a:accent5>` +
	`<a:accent6><a:srgbClr val="B44D3A"/></a:accent6>` +
	`<a:hlink><a:srgbClr val="3B6FB0"/></a:hlink>` +
	`<a:folHlink><a:srgbClr val="8257C7"/></a:folHlink>` +
	`</a:clrScheme>` +
	`<a:fontScheme name="Beskos">` +
	`<a:majorFont><a:latin typeface="Calibri Light"/><a:ea typeface=""/><a:cs typeface=""/></a:majorFont>` +
	`<a:minorFont><a:latin typeface="Calibri"/><a:ea typeface=""/><a:cs typeface=""/></a:minorFont>` +
	`</a:fontScheme>` +
	`<a:fmtScheme name="Beskos">` +
	`<a:fillStyleLst><a:solidFill><a:schemeClr val="phClr"/></a:solidFill>` +
	`<a:solidFill><a:schemeClr val="phClr"/></a:solidFill>` +
	`<a:solidFill><a:schemeClr val="phClr"/></a:solidFill></a:fillStyleLst>` +
	`<a:lnStyleLst><a:ln><a:solidFill><a:schemeClr val="phClr"/></a:solidFill></a:ln>` +
	`<a:ln><a:solidFill><a:schemeClr val="phClr"/></a:solidFill></a:ln>` +
	`<a:ln><a:solidFill><a:schemeClr val="phClr"/></a:solidFill></a:ln></a:lnStyleLst>` +
	`<a:effectStyleLst><a:effectStyle><a:effectLst/></a:effectStyle>` +
	`<a:effectStyle><a:effectLst/></a:effectStyle>` +
	`<a:effectStyle><a:effectLst/></a:effectStyle></a:effectStyleLst>` +
	`<a:bgFillStyleLst><a:solidFill><a:schemeClr val="phClr"/></a:solidFill>` +
	`<a:solidFill><a:schemeClr val="phClr"/></a:solidFill>` +
	`<a:solidFill><a:schemeClr val="phClr"/></a:solidFill></a:bgFillStyleLst>` +
	`</a:fmtScheme>` +
	`</a:themeElements></a:theme>`
