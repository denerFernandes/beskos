package office

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

// KindFromName deduz o tipo de ficheiro pela extensão. É a única pista que temos sem abrir o
// ficheiro, e chega: as três extensões são inequívocas.
func KindFromName(name string) (string, bool) {
	lower := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lower, ".docx"):
		return KindDocument, true
	case strings.HasSuffix(lower, ".xlsx"):
		return KindSpreadsheet, true
	case strings.HasSuffix(lower, ".pptx"):
		return KindPresentation, true
	}
	return "", false
}

// ExtractText devolve o texto legível de um ficheiro OOXML. É o que o modelo lê antes de editar
// (§5.1): não reconstrói a estrutura — para isso o modelo reescreve o plano — mas mostra o que lá
// está, para a edição não ser às cegas.
//
// Um ficheiro que não seja OOXML (imagem, PDF, binário) devolve erro em vez de lixo: esses não se
// editam, só se substituem.
func ExtractText(kind string, data []byte) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("office: not a valid package: %w", err)
	}
	switch kind {
	case KindDocument:
		return textFromParts(zr, []string{"word/document.xml"})
	case KindSpreadsheet:
		return textFromPrefix(zr, "xl/worksheets/")
	case KindPresentation:
		return textFromPrefix(zr, "ppt/slides/slide")
	}
	return "", fmt.Errorf("office: cannot read text from kind %q", kind)
}

// textFromParts junta o texto dos elementos <a:t>/<w:t>/<t> de partes específicas.
func textFromParts(zr *zip.Reader, names []string) (string, error) {
	var b strings.Builder
	for _, name := range names {
		for _, f := range zr.File {
			if f.Name != name {
				continue
			}
			s, err := xmlText(f)
			if err != nil {
				return "", err
			}
			b.WriteString(s)
		}
	}
	return b.String(), nil
}

// textFromPrefix faz o mesmo para todas as partes cujo nome começa por prefix, por ordem de nome —
// os slides e as folhas numeram-se, portanto a ordem alfabética é a ordem de apresentação até ao
// slide 9; a partir do 10 desalinha, mas para uma pré-visualização de leitura chega.
func textFromPrefix(zr *zip.Reader, prefix string) (string, error) {
	var b strings.Builder
	for _, f := range zr.File {
		if !strings.HasPrefix(f.Name, prefix) || !strings.HasSuffix(f.Name, ".xml") {
			continue
		}
		s, err := xmlText(f)
		if err != nil {
			return "", err
		}
		if s != "" {
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			b.WriteString(s)
		}
	}
	return b.String(), nil
}

// xmlText extrai o texto dos nós de caracteres de um XML, sem se preocupar com os nomes das tags:
// os elementos de texto do OOXML (<w:t>, <a:t>, <t>) são folhas com o texto lá dentro, e apanhar
// todos os CharData dá o corpo legível sem ter de conhecer o esquema todo.
func xmlText(f *zip.File) (string, error) {
	rc, err := f.Open()
	if err != nil {
		return "", err
	}
	defer rc.Close()
	dec := xml.NewDecoder(rc)
	var b strings.Builder
	var inText bool
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("office: read text: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			// Só o conteúdo dos elementos de texto conta. Sem este filtro, apanharíamos também os
			// valores de atributos e o ruído dos metadados.
			if t.Name.Local == "t" {
				inText = true
			}
		case xml.EndElement:
			if t.Name.Local == "t" {
				inText = false
				b.WriteString(" ")
			}
		case xml.CharData:
			if inText {
				b.Write(t)
			}
		}
	}
	return strings.TrimSpace(b.String()), nil
}
