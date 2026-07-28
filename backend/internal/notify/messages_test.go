package notify

import (
	"strings"
	"testing"
	"time"
)

func TestBuildHasBothParts(t *testing.T) {
	m := AccountCreated("pt-BR", "ana@acme.pt", "acme.pt", "https://app.beskos.com/mail")
	if !strings.Contains(m.HTML, "<!doctype html>") {
		t.Error("falta a parte HTML")
	}
	if !strings.Contains(m.HTML, "ana@acme.pt") || !strings.Contains(m.Text, "ana@acme.pt") {
		t.Error("o endereço tem de aparecer nas duas partes")
	}
	if !strings.Contains(m.Text, "https://app.beskos.com/mail") {
		t.Error("a versão em texto tem de levar o link (não há botão para clicar)")
	}
}

func TestLanguage(t *testing.T) {
	if s := AccountCreated("en", "a@b.pt", "b.pt", "").Subject; !strings.Contains(s, "ready") {
		t.Errorf("assunto em inglês = %q", s)
	}
	if s := AccountCreated("pt-BR", "a@b.pt", "b.pt", "").Subject; !strings.Contains(s, "pronta") {
		t.Errorf("assunto em português = %q", s)
	}
	// Idioma vazio ou desconhecido cai em português (idioma padrão do produto).
	if s := AccountCreated("", "a@b.pt", "b.pt", "").Subject; !strings.Contains(s, "pronta") {
		t.Errorf("assunto por omissão = %q", s)
	}
}

func TestEscaping(t *testing.T) {
	// Um valor com HTML não pode injetar marcação no aviso.
	m := NewLogin("pt-BR", "a@b.pt", "1.2.3.4", "<script>x</script>", "Chrome", time.Now(), "")
	if strings.Contains(m.HTML, "<script>") {
		t.Error("valor não escapado no HTML")
	}
}

func TestNewLoginUnknownLocation(t *testing.T) {
	m := NewLogin("pt-BR", "a@b.pt", "1.2.3.4", "", "Chrome", time.Now(), "")
	if !strings.Contains(m.Text, "desconhecida") {
		t.Error("sem localização, tem de o dizer em vez de deixar em branco")
	}
}
