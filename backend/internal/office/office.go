// Package office constrói ficheiros Office (docx, xlsx, pptx) a partir de um plano, em Go puro.
//
// Porquê à mão e não com uma biblioteca: um ficheiro OOXML é um ZIP com uns quantos XML dentro, e
// a stdlib já traz `archive/zip` e `encoding/xml`. Uma dependência nova precisa de justificação
// (lei 2.5), e o que se gera aqui é deliberadamente simples — texto, listas, tabelas e slides. O
// dia em que for preciso gráficos ou estilos ricos é o dia de reavaliar isto, não antes.
//
// Também não depende do OnlyOffice: gerar tem de funcionar mesmo onde a edição está desligada
// (decisão §12.1, ponto 2 do desenho do assistente).
package office

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"strings"
)

// Kind é o tipo de ficheiro a gerar.
const (
	KindDocument     = "document"
	KindSpreadsheet  = "spreadsheet"
	KindPresentation = "presentation"
)

// Block é um pedaço de um documento de texto.
type Block struct {
	// Style: "title" | "heading" | "paragraph" | "bullet"
	Style string `json:"style"`
	Text  string `json:"text"`
}

// Sheet é uma folha de cálculo. A primeira linha é tratada como cabeçalho na apresentação, mas
// continua a ser uma linha normal — não se inventa uma estrutura que o utilizador não pediu.
type Sheet struct {
	Name string     `json:"name"`
	Rows [][]string `json:"rows"`
}

// Slide é um slide de apresentação: um título e uns pontos.
type Slide struct {
	Title   string   `json:"title"`
	Bullets []string `json:"bullets"`
	Notes   string   `json:"notes,omitempty"`
}

// Plan é o que o modelo produz e o que se converte em ficheiro. Um plano, não um ficheiro: o modelo
// descreve o conteúdo e é o servidor que constrói o formato — que é o que torna o resultado um
// ficheiro Office válido em vez de texto com extensão trocada.
type Plan struct {
	Kind   string  `json:"kind"`
	Title  string  `json:"title"`
	Blocks []Block `json:"blocks,omitempty"`
	Sheets []Sheet `json:"sheets,omitempty"`
	Slides []Slide `json:"slides,omitempty"`
}

// Ext devolve a extensão do ficheiro para este tipo.
func Ext(kind string) (string, bool) {
	switch kind {
	case KindDocument:
		return "docx", true
	case KindSpreadsheet:
		return "xlsx", true
	case KindPresentation:
		return "pptx", true
	}
	return "", false
}

// MIME devolve o content-type do ficheiro.
func MIME(kind string) string {
	switch kind {
	case KindDocument:
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case KindSpreadsheet:
		return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	case KindPresentation:
		return "application/vnd.openxmlformats-officedocument.presentationml.presentation"
	}
	return "application/octet-stream"
}

// Build constrói o ficheiro. Devolve erro para um tipo desconhecido em vez de escolher um por
// omissão: um plano de apresentação gravado como documento seria pior do que uma recusa clara.
func Build(p Plan) ([]byte, error) {
	switch p.Kind {
	case KindDocument:
		return buildDocx(p)
	case KindSpreadsheet:
		return buildXlsx(p)
	case KindPresentation:
		return buildPptx(p)
	}
	return nil, fmt.Errorf("office: unknown kind %q", p.Kind)
}

// part é um ficheiro dentro do ZIP.
type part struct {
	name string
	body string
}

// zipParts escreve as partes num ZIP. A ordem importa em alguns leitores antigos, por isso é a de
// escrita — não a de um mapa, que seria aleatória.
func zipParts(parts []part) ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, p := range parts {
		w, err := zw.Create(p.name)
		if err != nil {
			return nil, fmt.Errorf("office: create %s: %w", p.name, err)
		}
		if _, err := w.Write([]byte(p.body)); err != nil {
			return nil, fmt.Errorf("office: write %s: %w", p.name, err)
		}
	}
	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("office: close zip: %w", err)
	}
	return buf.Bytes(), nil
}

// esc escapa texto para dentro de XML.
//
// É a única defesa que separa "conteúdo do utilizador" de "marcação": um assunto de e-mail com um
// `<` transformaria o ficheiro em lixo ilegível se passasse cru. Passa por aqui TODO o texto que
// vem de fora, sem exceção.
func esc(s string) string {
	var b bytes.Buffer
	if err := xml.EscapeText(&b, []byte(sanitize(s))); err != nil {
		return ""
	}
	return b.String()
}

// sanitize deita fora os caracteres de controlo que o XML 1.0 não admite. Um deles no meio do texto
// torna o ficheiro inválido, e o leitor não diz porquê — diz só que está corrompido.
func sanitize(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\t' || r == '\n' || r == '\r':
			return r
		case r < 0x20, r >= 0x7F && r <= 0x9F:
			return -1
		}
		return r
	}, s)
}

const xmlHeader = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n"
