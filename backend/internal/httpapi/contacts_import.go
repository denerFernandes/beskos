package httpapi

import (
	"bufio"
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"mailin/backend/internal/db"
)

// Import/export de contatos em vCard 3.0 e CSV — os formatos universais de agenda (Google, Apple,
// Outlook exportam/importam ambos). Trabalha sobre o modelo atual (um e-mail/telefone por contato).

// handleExportContacts devolve todos os contatos do dono como vCard (default) ou CSV.
func (s *Server) handleExportContacts(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	rows, err := s.q.ListContacts(r.Context(), db.ListContactsParams{OwnerUserID: mu.ID, TenantID: mu.TenantID})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "contacts.failed")
		return
	}
	// Só os do próprio dono (não exportar contatos vistos por partilha de outrem).
	own := rows[:0]
	for _, c := range rows {
		if c.OwnerUserID == mu.ID {
			own = append(own, c)
		}
	}
	if r.URL.Query().Get("format") == "csv" {
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="contacts.csv"`)
		cw := csv.NewWriter(w)
		_ = cw.Write([]string{"name", "email", "phone", "company", "title", "notes"})
		for _, c := range own {
			_ = cw.Write([]string{c.Name, c.Email, c.Phone, c.Company, c.Title, c.Notes})
		}
		cw.Flush()
		return
	}
	w.Header().Set("Content-Type", "text/vcard; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="contacts.vcf"`)
	var b bytes.Buffer
	for _, c := range own {
		writeVCard(&b, vcardOf(c.Name, c.Email, c.Phone, c.Mobile, c.Company, c.Title, c.Notes))
	}
	_, _ = w.Write(b.Bytes())
}

// vcardFields são os campos que sabemos serializar em vCard, independentes do tipo de linha (lista,
// get…). vcardOf monta um a partir dos campos soltos.
type vcardFields struct{ Name, Email, Phone, Mobile, Company, Title, Notes string }

func vcardOf(name, email, phone, mobile, company, title, notes string) vcardFields {
	return vcardFields{Name: name, Email: email, Phone: phone, Mobile: mobile, Company: company, Title: title, Notes: notes}
}

// writeVCard serializa um contato em vCard 3.0. Escapa vírgula/ponto-e-vírgula/quebra conforme a RFC.
func writeVCard(b *bytes.Buffer, c vcardFields) {
	esc := func(s string) string {
		s = strings.ReplaceAll(s, "\\", "\\\\")
		s = strings.ReplaceAll(s, "\n", "\\n")
		s = strings.ReplaceAll(s, ",", "\\,")
		s = strings.ReplaceAll(s, ";", "\\;")
		return s
	}
	b.WriteString("BEGIN:VCARD\r\nVERSION:3.0\r\n")
	fmt.Fprintf(b, "FN:%s\r\n", esc(c.Name))
	if c.Email != "" {
		fmt.Fprintf(b, "EMAIL;TYPE=INTERNET:%s\r\n", esc(c.Email))
	}
	if c.Phone != "" {
		fmt.Fprintf(b, "TEL:%s\r\n", esc(c.Phone))
	}
	if c.Mobile != "" {
		fmt.Fprintf(b, "TEL;TYPE=CELL:%s\r\n", esc(c.Mobile))
	}
	if c.Company != "" {
		fmt.Fprintf(b, "ORG:%s\r\n", esc(c.Company))
	}
	if c.Title != "" {
		fmt.Fprintf(b, "TITLE:%s\r\n", esc(c.Title))
	}
	if c.Notes != "" {
		fmt.Fprintf(b, "NOTE:%s\r\n", esc(c.Notes))
	}
	b.WriteString("END:VCARD\r\n")
}

type importedContact struct {
	Name, Email, Phone, Company, Title, Notes string
}

// handleImportContacts recebe um ficheiro (multipart "file") vCard ou CSV e cria os contatos. Dedup
// por e-mail (não recria um contato cujo e-mail já existe na agenda do dono). Devolve os totais.
func (s *Server) handleImportContacts(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	file, header, err := r.FormFile("file")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "contacts.import_no_file")
		return
	}
	defer file.Close()
	data := make([]byte, 0, header.Size)
	buf := bytes.NewBuffer(data)
	if _, err := buf.ReadFrom(file); err != nil {
		writeErr(w, http.StatusBadRequest, "contacts.import_failed")
		return
	}
	var list []importedContact
	if strings.HasSuffix(strings.ToLower(header.Filename), ".csv") {
		list, err = parseCSVContacts(buf.Bytes())
	} else {
		list = parseVCards(buf.Bytes())
	}
	if err != nil {
		writeErr(w, http.StatusBadRequest, "contacts.import_failed")
		return
	}
	// Dedup: por e-mail quando há, senão pelo nome (contatos sem e-mail, como empresas, senão
	// re-importariam sempre). A chave é minúscula para casar diferenças só de caixa.
	dedupKey := func(name, email string) string {
		if email != "" {
			return "e:" + strings.ToLower(email)
		}
		return "n:" + strings.ToLower(strings.TrimSpace(name))
	}
	existing := map[string]bool{}
	if rows, err := s.q.ListContacts(r.Context(), db.ListContactsParams{OwnerUserID: mu.ID, TenantID: mu.TenantID}); err == nil {
		for _, c := range rows {
			if c.OwnerUserID == mu.ID {
				existing[dedupKey(c.Name, c.Email)] = true
			}
		}
	}
	imported, skipped := 0, 0
	for _, ic := range list {
		if strings.TrimSpace(ic.Name) == "" && ic.Email == "" {
			continue
		}
		if ic.Name == "" {
			ic.Name = ic.Email // sem FN: usa o e-mail como nome
		}
		key := dedupKey(ic.Name, ic.Email)
		if existing[key] {
			skipped++
			continue
		}
		if _, err := s.q.CreateContact(r.Context(), db.CreateContactParams{
			TenantID: mu.TenantID, OwnerUserID: mu.ID, Kind: "person", Name: strings.TrimSpace(ic.Name),
			Email: ic.Email, Phone: ic.Phone, Company: ic.Company, Notes: ic.Notes, Title: ic.Title,
			Tags: []string{}, ExtraEmails: []string{}, ExtraPhones: []string{},
			AllowPhone: true, AllowEmail: true, CustomFields: json.RawMessage("{}"), // jsonb NOT NULL
		}); err != nil {
			continue
		}
		existing[key] = true
		imported++
	}
	writeJSON(w, http.StatusOK, map[string]int{"imported": imported, "skipped": skipped})
}

// parseCSVContacts lê um CSV com cabeçalho e mapeia colunas conhecidas (name/email/phone/company/
// title/notes) por nome — a ordem das colunas não importa.
func parseCSVContacts(data []byte) ([]importedContact, error) {
	cr := csv.NewReader(bytes.NewReader(data))
	cr.FieldsPerRecord = -1 // linhas com nº de campos variável não quebram
	records, err := cr.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, nil
	}
	col := map[string]int{}
	for i, h := range records[0] {
		col[strings.ToLower(strings.TrimSpace(h))] = i
	}
	at := func(rec []string, key string) string {
		if i, ok := col[key]; ok && i < len(rec) {
			return strings.TrimSpace(rec[i])
		}
		return ""
	}
	out := make([]importedContact, 0, len(records)-1)
	for _, rec := range records[1:] {
		out = append(out, importedContact{
			Name: at(rec, "name"), Email: at(rec, "email"), Phone: at(rec, "phone"),
			Company: at(rec, "company"), Title: at(rec, "title"), Notes: at(rec, "notes"),
		})
	}
	return out, nil
}

// parseVCards lê um ou vários VCARD e extrai os campos que o nosso modelo suporta. Desdobra linhas
// (continuação começa com espaço/tab) e trata o unescape básico do vCard.
func parseVCards(data []byte) []importedContact {
	var out []importedContact
	var cur *importedContact
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var logical string
	flush := func(line string) {
		if line == "" {
			return
		}
		up := strings.ToUpper(line)
		switch {
		case up == "BEGIN:VCARD":
			cur = &importedContact{}
		case up == "END:VCARD":
			if cur != nil {
				out = append(out, *cur)
				cur = nil
			}
		case cur != nil:
			applyVCardLine(cur, line)
		}
	}
	for sc.Scan() {
		raw := strings.TrimRight(sc.Text(), "\r")
		if len(raw) > 0 && (raw[0] == ' ' || raw[0] == '\t') { // continuação da linha anterior
			logical += raw[1:]
			continue
		}
		flush(logical)
		logical = raw
	}
	flush(logical)
	return out
}

func applyVCardLine(c *importedContact, line string) {
	colon := strings.IndexByte(line, ':')
	if colon < 0 {
		return
	}
	name, value := line[:colon], unescapeVCard(line[colon+1:])
	prop := strings.ToUpper(name)
	if i := strings.IndexByte(prop, ';'); i >= 0 {
		prop = prop[:i] // descarta os parâmetros (TYPE=...)
	}
	switch prop {
	case "FN":
		c.Name = value
	case "EMAIL":
		if c.Email == "" {
			c.Email = value
		}
	case "TEL":
		c.Phone = firstNonEmpty(c.Phone, value)
	case "ORG":
		c.Company = strings.TrimRight(value, ";") // ORG pode vir "Empresa;Departamento"
	case "TITLE":
		c.Title = value
	case "NOTE":
		c.Notes = value
	}
}

func unescapeVCard(s string) string {
	s = strings.ReplaceAll(s, "\\n", "\n")
	s = strings.ReplaceAll(s, "\\N", "\n")
	s = strings.ReplaceAll(s, "\\,", ",")
	s = strings.ReplaceAll(s, "\\;", ";")
	s = strings.ReplaceAll(s, "\\\\", "\\")
	return s
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// handleContactVCard devolve UM contato como vCard 3.0 — o webmail usa isto para anexar o cartão a
// um e-mail ("enviar contato"). Scoped pelo contactOr404 (tenant + dono/partilha), como o resto.
func (s *Server) handleContactVCard(w http.ResponseWriter, r *http.Request) {
	c, ok := s.contactOr404(w, r)
	if !ok {
		return
	}
	var b bytes.Buffer
	writeVCard(&b, vcardOf(c.Name, c.Email, c.Phone, c.Mobile, c.Company, c.Title, c.Notes))
	w.Header().Set("Content-Type", "text/vcard; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+vcardFilename(c.Name)+`"`)
	_, _ = w.Write(b.Bytes())
}

// vcardFilename gera um nome de ficheiro seguro a partir do nome do contato ("João Silva" →
// "Joao-Silva.vcf" não; mantemos simples: troca o que não serve por "-").
func vcardFilename(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "contact.vcf"
	}
	safe := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		case r == ' ':
			return '-'
		default:
			return -1
		}
	}, name)
	if safe == "" {
		safe = "contact"
	}
	return safe + ".vcf"
}
