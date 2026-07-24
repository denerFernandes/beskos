package office

import (
	"fmt"
	"strconv"
	"strings"
)

// buildXlsx constrói um .xlsx mínimo mas válido.
//
// Os valores de texto vão como STRINGS EM LINHA (`t="inlineStr"`) em vez de irem para uma tabela de
// strings partilhada. A tabela partilhada poupa espaço quando há muita repetição; aqui trocamos
// essa poupança por menos uma parte no pacote e menos um sítio onde os índices podem ficar
// dessincronizados — que é o modo típico de um xlsx gerado sair corrompido.
func buildXlsx(p Plan) ([]byte, error) {
	sheets := p.Sheets
	if len(sheets) == 0 {
		sheets = []Sheet{{Name: "Sheet1"}}
	}

	parts := []part{}
	var sheetRefs, sheetRels, overrides strings.Builder

	for i, sh := range sheets {
		n := i + 1
		name := sheetName(sh.Name, n)
		path := fmt.Sprintf("xl/worksheets/sheet%d.xml", n)
		rid := fmt.Sprintf("rId%d", n)

		sheetRefs.WriteString(fmt.Sprintf(
			`<sheet name="%s" sheetId="%d" r:id="%s"/>`, esc(name), n, rid))
		sheetRels.WriteString(fmt.Sprintf(
			`<Relationship Id="%s" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet%d.xml"/>`, rid, n))
		overrides.WriteString(fmt.Sprintf(
			`<Override PartName="/%s" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>`, path))
		parts = append(parts, part{path, sheetXML(sh)})
	}

	head := []part{
		{"[Content_Types].xml", xmlHeader +
			`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
			`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
			`<Default Extension="xml" ContentType="application/xml"/>` +
			`<Override PartName="/xl/workbook.xml" ContentType="` + MIME(KindSpreadsheet) + `.main+xml"/>` +
			overrides.String() +
			`</Types>`},
		{"_rels/.rels", xmlHeader +
			`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/>` +
			`</Relationships>`},
		{"xl/workbook.xml", xmlHeader +
			`<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" ` +
			`xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">` +
			`<sheets>` + sheetRefs.String() + `</sheets></workbook>`},
		{"xl/_rels/workbook.xml.rels", xmlHeader +
			`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			sheetRels.String() + `</Relationships>`},
	}
	return zipParts(append(head, parts...))
}

func sheetXML(sh Sheet) string {
	var rows strings.Builder
	for r, row := range sh.Rows {
		rows.WriteString(fmt.Sprintf(`<row r="%d">`, r+1))
		for c, val := range row {
			rows.WriteString(cellXML(colName(c)+strconv.Itoa(r+1), val))
		}
		rows.WriteString(`</row>`)
	}
	return xmlHeader +
		`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">` +
		`<sheetData>` + rows.String() + `</sheetData></worksheet>`
}

// cellXML escreve uma célula. Um valor que seja mesmo um número entra como NÚMERO, para poder ser
// somado na folha; tudo o resto entra como texto. Escrever "1.234,00" como número daria um valor
// errado em silêncio, portanto só passa o que o Go lê como número sem ambiguidade.
func cellXML(ref, val string) string {
	if val == "" {
		return ""
	}
	if _, err := strconv.ParseFloat(strings.TrimSpace(val), 64); err == nil {
		return fmt.Sprintf(`<c r="%s"><v>%s</v></c>`, ref, esc(strings.TrimSpace(val)))
	}
	return fmt.Sprintf(`<c r="%s" t="inlineStr"><is><t xml:space="preserve">%s</t></is></c>`, ref, esc(val))
}

// colName traduz um índice de coluna para a notação do Excel (0 → A, 26 → AA).
func colName(i int) string {
	name := ""
	for i >= 0 {
		name = string(rune('A'+i%26)) + name
		i = i/26 - 1
	}
	return name
}

// sheetName limpa o nome da folha. O Excel proíbe alguns caracteres e um limite de 31 — um nome
// inválido não dá erro ao gravar, dá um ficheiro que não abre.
func sheetName(name string, n int) string {
	name = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`:\/?*[]`, r) {
			return '-'
		}
		return r
	}, sanitize(strings.TrimSpace(name)))
	if name == "" {
		name = fmt.Sprintf("Sheet%d", n)
	}
	if r := []rune(name); len(r) > 31 {
		name = string(r[:31])
	}
	return name
}
