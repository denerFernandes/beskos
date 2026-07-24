package httpapi

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// A pesquisa do Drive é partilhada entre a interface e a ferramenta drive.search do assistente.
// Se o escape dos curingas se perder, um utilizador que procure "50%" passa a receber tudo — e o
// assistente passaria a "ver" ficheiros que o ecrã não mostra.
func TestLikePatternEscapesWildcards(t *testing.T) {
	cases := map[string]string{
		"relatorio": `%relatorio%`,
		"50%":       `%50\%%`,
		"a_b":       `%a\_b%`,
		`c\d`:       `%c\\d%`,
		"":          `%%`,
	}
	for in, want := range cases {
		if got := likePattern(in); got != want {
			t.Errorf("likePattern(%q) = %q, quero %q", in, got, want)
		}
	}
}

func TestUUIDText(t *testing.T) {
	id := uuid.MustParse("6ba7b810-9dad-11d1-80b4-00c04fd430c8")
	got := uuidText(pgtype.UUID{Bytes: id, Valid: true})
	if got != id.String() {
		t.Errorf("uuidText = %q, quero %q", got, id)
	}
	// Uma coluna nula devolve string vazia em vez de um UUID de zeros, que pareceria um id real.
	if got := uuidText(pgtype.UUID{}); got != "" {
		t.Errorf("uuidText de UUID nulo = %q, quero vazio", got)
	}
}

// O título vem da frase do utilizador, não do modelo — e tem de caber na lista sem a rebentar.
func TestConversationTitle(t *testing.T) {
	if got := conversationTitle("  procura a fatura\ndo mês  "); got != "procura a fatura do mês" {
		t.Errorf("conversationTitle = %q", got)
	}
	// Corta por RUNAS, não por bytes: cortar a meio de um caractere acentuado daria lixo.
	long := strings.Repeat("á", 200)
	got := conversationTitle(long)
	if r := []rune(got); len(r) != 81 || r[80] != '…' {
		t.Errorf("título longo = %d runas (%q…), quero 80 + reticências", len(r), string(r[:5]))
	}
}
