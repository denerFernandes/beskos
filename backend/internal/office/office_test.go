package office

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"io"
	"strings"
	"testing"
)

// unzip abre o ficheiro gerado e devolve as partes por nome.
func unzip(t *testing.T, data []byte) map[string]string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("o ficheiro não é um ZIP válido: %v", err)
	}
	out := map[string]string{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("abrir %s: %v", f.Name, err)
		}
		b, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatalf("ler %s: %v", f.Name, err)
		}
		out[f.Name] = string(b)
	}
	return out
}

// assertWellFormed prova que cada parte XML é XML válido. É a diferença entre "gerou bytes" e
// "gerou um ficheiro": um leitor de Office recusa o pacote inteiro por causa de uma parte malformada
// e diz apenas "corrompido", sem dizer qual.
func assertWellFormed(t *testing.T, parts map[string]string) {
	t.Helper()
	for name, body := range parts {
		if !strings.HasSuffix(name, ".xml") && !strings.HasSuffix(name, ".rels") {
			continue
		}
		dec := xml.NewDecoder(strings.NewReader(body))
		for {
			_, err := dec.Token()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Errorf("%s não é XML bem formado: %v", name, err)
				break
			}
		}
	}
}

// mustHave verifica que as partes obrigatórias estão lá. Uma em falta dá "ficheiro corrompido" no
// leitor, sem dizer qual — daí serem verificadas pelo nome, uma a uma.
func mustHave(t *testing.T, parts map[string]string, names ...string) {
	t.Helper()
	for _, n := range names {
		if _, ok := parts[n]; !ok {
			t.Errorf("falta a parte obrigatória %s", n)
		}
	}
}

func TestDocumentIsAValidPackage(t *testing.T) {
	data, err := Build(Plan{Kind: KindDocument, Title: "Relatório", Blocks: []Block{
		{Style: "heading", Text: "Resumo"},
		{Style: "paragraph", Text: "Primeira linha\nSegunda linha"},
		{Style: "bullet", Text: "Um ponto"},
	}})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	parts := unzip(t, data)
	assertWellFormed(t, parts)
	mustHave(t, parts, "[Content_Types].xml", "_rels/.rels", "word/document.xml")

	doc := parts["word/document.xml"]
	for _, want := range []string{"Relatório", "Resumo", "Primeira linha", "Segunda linha", "Um ponto"} {
		if !strings.Contains(doc, want) {
			t.Errorf("o documento não contém %q", want)
		}
	}
	// Uma quebra de linha dentro de um parágrafo tem de virar <w:br/>: sem isso, três linhas
	// apareceriam coladas numa só.
	if !strings.Contains(doc, "<w:br/>") {
		t.Error("a quebra de linha não virou <w:br/>")
	}
}

// INVARIANTE: todo o texto que vem de fora é escapado. Um assunto de e-mail com um "<" tornaria o
// ficheiro ilegível — e o utilizador veria "corrompido", não "o teu texto tinha um sinal estranho".
func TestUserTextIsEscapedNotInjected(t *testing.T) {
	nasty := `Ana & Bruno <script>alert("x")</script>`
	data, err := Build(Plan{Kind: KindDocument, Blocks: []Block{{Text: nasty}}})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	parts := unzip(t, data)
	assertWellFormed(t, parts) // se o escape falhasse, isto falhava aqui
	doc := parts["word/document.xml"]
	if strings.Contains(doc, "<script>") {
		t.Error("a marcação do utilizador entrou crua no XML")
	}
	if !strings.Contains(doc, "&amp;") {
		t.Error("o & não foi escapado")
	}
}

// Caracteres de controlo não são admitidos em XML 1.0. Um no meio do texto torna o ficheiro
// inválido, e o leitor não diz porquê.
func TestControlCharactersAreDropped(t *testing.T) {
	data, err := Build(Plan{Kind: KindDocument, Blocks: []Block{{Text: "antes\x00\x07depois"}}})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	parts := unzip(t, data)
	assertWellFormed(t, parts)
	if !strings.Contains(parts["word/document.xml"], "antesdepois") {
		t.Error("o texto perdeu-se ao limpar os caracteres de controlo")
	}
}

func TestSpreadsheetIsAValidPackage(t *testing.T) {
	data, err := Build(Plan{Kind: KindSpreadsheet, Sheets: []Sheet{
		{Name: "Vendas", Rows: [][]string{{"Mês", "Total"}, {"Janeiro", "1500"}, {"Fevereiro", "2300.5"}}},
		{Name: "Notas", Rows: [][]string{{"a"}}},
	}})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	parts := unzip(t, data)
	assertWellFormed(t, parts)
	mustHave(t, parts, "[Content_Types].xml", "_rels/.rels", "xl/workbook.xml",
		"xl/_rels/workbook.xml.rels", "xl/worksheets/sheet1.xml", "xl/worksheets/sheet2.xml")

	sheet := parts["xl/worksheets/sheet1.xml"]
	// Um número entra como número, para poder ser somado na folha.
	if !strings.Contains(sheet, `<c r="B2"><v>1500</v></c>`) {
		t.Errorf("1500 não entrou como número: %s", sheet)
	}
	// Texto entra como string em linha.
	if !strings.Contains(sheet, `t="inlineStr"`) {
		t.Error("o texto não entrou como string em linha")
	}
}

// Nomes de folha inválidos não dão erro ao gravar — dão um ficheiro que não abre. Por isso são
// limpos, não recusados.
func TestSheetNamesAreSanitised(t *testing.T) {
	if got := sheetName(`Vendas/2024*`, 1); got != "Vendas-2024-" {
		t.Errorf("sheetName = %q", got)
	}
	if got := sheetName("  ", 3); got != "Sheet3" {
		t.Errorf("nome vazio = %q, quero Sheet3", got)
	}
	if got := sheetName(strings.Repeat("a", 40), 1); len([]rune(got)) != 31 {
		t.Errorf("nome longo tem %d runas, quero 31", len([]rune(got)))
	}
}

func TestColumnNames(t *testing.T) {
	cases := map[int]string{0: "A", 25: "Z", 26: "AA", 27: "AB", 51: "AZ", 52: "BA", 701: "ZZ", 702: "AAA"}
	for i, want := range cases {
		if got := colName(i); got != want {
			t.Errorf("colName(%d) = %q, quero %q", i, got, want)
		}
	}
}

func TestPresentationIsAValidPackage(t *testing.T) {
	data, err := Build(Plan{Kind: KindPresentation, Title: "Resultados", Slides: []Slide{
		{Title: "Primeiro trimestre", Bullets: []string{"Cresceu 12%", "Dois clientes novos"}},
		{Title: "Segundo trimestre", Bullets: []string{"Estável"}},
	}})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	parts := unzip(t, data)
	assertWellFormed(t, parts)
	// Um slide não abre sozinho: precisa do esquema, do modelo global e do tema. Faltar um deles dá
	// "ficheiro corrompido" sem dizer qual.
	mustHave(t, parts,
		"[Content_Types].xml", "_rels/.rels",
		"ppt/presentation.xml", "ppt/_rels/presentation.xml.rels",
		"ppt/slideMasters/slideMaster1.xml", "ppt/slideMasters/_rels/slideMaster1.xml.rels",
		"ppt/slideLayouts/slideLayout1.xml", "ppt/slideLayouts/_rels/slideLayout1.xml.rels",
		"ppt/theme/theme1.xml",
		"ppt/slides/slide1.xml", "ppt/slides/_rels/slide1.xml.rels",
		"ppt/slides/slide2.xml", "ppt/slides/_rels/slide2.xml.rels")

	if !strings.Contains(parts["ppt/slides/slide1.xml"], "Cresceu 12%") {
		t.Error("o primeiro slide não tem o seu conteúdo")
	}
	// Cada slide tem de estar declarado no pacote e na lista da apresentação, senão não aparece.
	for _, want := range []string{`Target="slides/slide1.xml"`, `Target="slides/slide2.xml"`} {
		if !strings.Contains(parts["ppt/_rels/presentation.xml.rels"], want) {
			t.Errorf("a relação %s não está declarada", want)
		}
	}
	if strings.Count(parts["ppt/presentation.xml"], "<p:sldId ") != 2 {
		t.Error("a lista de slides não tem os dois slides")
	}
}

// Uma apresentação sem slides continua a ter de abrir: faz-se a capa com o título.
func TestEmptyPresentationStillOpens(t *testing.T) {
	data, err := Build(Plan{Kind: KindPresentation, Title: "Só a capa"})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	parts := unzip(t, data)
	assertWellFormed(t, parts)
	if !strings.Contains(parts["ppt/slides/slide1.xml"], "Só a capa") {
		t.Error("a capa não levou o título")
	}
}

// Um tipo desconhecido é recusado, não convertido no primeiro que houver: um plano de apresentação
// gravado como documento seria pior do que uma recusa clara.
func TestUnknownKindIsRefused(t *testing.T) {
	if _, err := Build(Plan{Kind: "pdf"}); err == nil {
		t.Error("Build aceitou um tipo desconhecido")
	}
	if _, ok := Ext("pdf"); ok {
		t.Error("Ext aceitou um tipo desconhecido")
	}
}

func TestExtAndMIMEAgree(t *testing.T) {
	for _, kind := range []string{KindDocument, KindSpreadsheet, KindPresentation} {
		ext, ok := Ext(kind)
		if !ok || ext == "" {
			t.Errorf("%s não tem extensão", kind)
		}
		if MIME(kind) == "application/octet-stream" {
			t.Errorf("%s não tem content-type próprio", kind)
		}
	}
}

// O texto que se põe num documento tem de sair na leitura: é o que o modelo lê antes de editar
// (§5.1). Um round-trip que perdesse o conteúdo editaria às cegas.
func TestExtractTextRoundTrip(t *testing.T) {
	data, err := Build(Plan{Kind: KindDocument, Title: "Plano", Blocks: []Block{
		{Style: "heading", Text: "Objetivos"},
		{Style: "paragraph", Text: "Crescer a base de clientes"},
	}})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	text, err := ExtractText(KindDocument, data)
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}
	for _, want := range []string{"Plano", "Objetivos", "Crescer a base de clientes"} {
		if !strings.Contains(text, want) {
			t.Errorf("o texto extraído não contém %q: %q", want, text)
		}
	}
}

// A apresentação também: os pontos dos slides são conteúdo, e a leitura tem de os ver.
func TestExtractTextFromPresentation(t *testing.T) {
	data, _ := Build(Plan{Kind: KindPresentation, Slides: []Slide{
		{Title: "Trimestre", Bullets: []string{"Cresceu 12%"}},
	}})
	text, err := ExtractText(KindPresentation, data)
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}
	if !strings.Contains(text, "Cresceu 12%") || !strings.Contains(text, "Trimestre") {
		t.Errorf("faltou conteúdo do slide: %q", text)
	}
}

// Um tipo não-Office não se lê como texto: devolve erro, para a edição saber que não pode editar.
func TestExtractTextRefusesNonOffice(t *testing.T) {
	if _, err := ExtractText("other", []byte("uma imagem qualquer")); err == nil {
		t.Error("ExtractText aceitou um tipo não-Office")
	}
}

func TestKindFromName(t *testing.T) {
	cases := map[string]string{"a.docx": KindDocument, "b.XLSX": KindSpreadsheet, "c.pptx": KindPresentation}
	for name, want := range cases {
		if got, ok := KindFromName(name); !ok || got != want {
			t.Errorf("KindFromName(%q) = %q,%v", name, got, ok)
		}
	}
	if _, ok := KindFromName("foto.png"); ok {
		t.Error("KindFromName aceitou um .png")
	}
}
