package office

import "strings"

// buildDocx constrói um .docx mínimo mas VÁLIDO: o pacote de conteúdos, a relação da raiz e o
// documento. Sem styles.xml — os estilos usados são os que o Word resolve por omissão a partir dos
// nomes ("Title", "Heading1", "ListParagraph"), portanto não é preciso declará-los para o ficheiro
// abrir. Um ficheiro que abre em todo o lado vale mais do que um com estilos nossos que não abre.
func buildDocx(p Plan) ([]byte, error) {
	var body strings.Builder
	if p.Title != "" {
		body.WriteString(docxPara("Title", p.Title))
	}
	for _, b := range p.Blocks {
		switch b.Style {
		case "title":
			body.WriteString(docxPara("Title", b.Text))
		case "heading":
			body.WriteString(docxPara("Heading1", b.Text))
		case "bullet":
			body.WriteString(docxBullet(b.Text))
		default:
			body.WriteString(docxPara("", b.Text))
		}
	}

	doc := xmlHeader +
		`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">` +
		`<w:body>` + body.String() +
		// A secção final define o tamanho da página. Sem ela alguns leitores assumem valores
		// estranhos; com ela, A4 em toda a parte.
		`<w:sectPr><w:pgSz w:w="11906" w:h="16838"/>` +
		`<w:pgMar w:top="1134" w:right="1134" w:bottom="1134" w:left="1134"/></w:sectPr>` +
		`</w:body></w:document>`

	return zipParts([]part{
		{"[Content_Types].xml", xmlHeader +
			`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
			`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
			`<Default Extension="xml" ContentType="application/xml"/>` +
			`<Override PartName="/word/document.xml" ContentType="` + MIME(KindDocument) + `.main+xml"/>` +
			`</Types>`},
		{"_rels/.rels", xmlHeader +
			`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>` +
			`</Relationships>`},
		{"word/document.xml", doc},
	})
}

// docxPara escreve um parágrafo com um estilo. Texto vazio produz um parágrafo vazio, que é uma
// linha em branco — e uma linha em branco pedida é conteúdo, não um erro.
func docxPara(style, text string) string {
	var b strings.Builder
	b.WriteString(`<w:p>`)
	if style != "" {
		b.WriteString(`<w:pPr><w:pStyle w:val="` + style + `"/></w:pPr>`)
	}
	b.WriteString(docxRuns(text))
	b.WriteString(`</w:p>`)
	return b.String()
}

func docxBullet(text string) string {
	return `<w:p><w:pPr><w:pStyle w:val="ListParagraph"/>` +
		`<w:numPr><w:ilvl w:val="0"/><w:numId w:val="1"/></w:numPr></w:pPr>` +
		docxRuns(text) + `</w:p>`
}

// docxRuns parte o texto nas quebras de linha. Um \n dentro de um <w:t> é ignorado pelo Word: sem
// isto, um parágrafo de três linhas apareceria numa só, colado.
func docxRuns(text string) string {
	lines := strings.Split(sanitize(text), "\n")
	var b strings.Builder
	for i, line := range lines {
		b.WriteString(`<w:r>`)
		if i > 0 {
			b.WriteString(`<w:br/>`)
		}
		// xml:space="preserve" mantém os espaços do início e do fim, que de outra forma o leitor
		// come — e a indentação de uma citação desapareceria.
		b.WriteString(`<w:t xml:space="preserve">` + esc(line) + `</w:t>`)
		b.WriteString(`</w:r>`)
	}
	return b.String()
}
