package httpapi

import (
	"bytes"
	"strings"
	"testing"
)

// rebuild é um atalho para os testes: corre a reconstrução e devolve o conteúdo final.
func rebuild(t *testing.T, old, body string, totalSize, bs int64, changed ...int64) string {
	t.Helper()
	set := map[int64]bool{}
	for _, i := range changed {
		set[i] = true
	}
	var out bytes.Buffer
	err := rebuildFromDelta(&out, strings.NewReader(old), strings.NewReader(body),
		int64(len(old)), totalSize, bs, set)
	if err != nil {
		t.Fatalf("rebuildFromDelta: %v", err)
	}
	return out.String()
}

func TestRebuildFromDelta(t *testing.T) {
	// Blocos de 4 bytes. "AAAABBBBCCCC" com o bloco do meio trocado por "XXXX".
	if got, want := rebuild(t, "AAAABBBBCCCC", "XXXX", 12, 4, 1), "AAAAXXXXCCCC"; got != want {
		t.Errorf("bloco do meio: %q, queria %q", got, want)
	}
	// Nenhum bloco alterado ⇒ conteúdo idêntico (corpo vazio).
	if got, want := rebuild(t, "AAAABBBB", "", 8, 4), "AAAABBBB"; got != want {
		t.Errorf("sem alterações: %q, queria %q", got, want)
	}
	// Vários blocos alterados, pela ordem dos índices.
	if got, want := rebuild(t, "AAAABBBBCCCC", "XXXXZZZZ", 12, 4, 0, 2), "XXXXBBBBZZZZ"; got != want {
		t.Errorf("dois blocos: %q, queria %q", got, want)
	}
}

func TestRebuildFromDeltaFileGrew(t *testing.T) {
	// Ficheiro cresceu: o bloco novo (índice 2) só pode vir do corpo.
	got := rebuild(t, "AAAABBBB", "CCCC", 12, 4, 2)
	if want := "AAAABBBBCCCC"; got != want {
		t.Errorf("cresceu: %q, queria %q", got, want)
	}
}

func TestRebuildFromDeltaFileShrank(t *testing.T) {
	// Ficheiro encolheu: para em totalSize e ignora o resto do antigo.
	if got, want := rebuild(t, "AAAABBBBCCCC", "", 8, 4), "AAAABBBB"; got != want {
		t.Errorf("encolheu: %q, queria %q", got, want)
	}
	// Último bloco parcial vindo do corpo.
	if got, want := rebuild(t, "AAAABBBB", "XX", 6, 4, 1), "AAAAXX"; got != want {
		t.Errorf("bloco parcial: %q, queria %q", got, want)
	}
}

func TestRebuildFromDeltaRejectsBadInput(t *testing.T) {
	set := map[int64]bool{}
	var out bytes.Buffer
	// Bloco NÃO marcado como alterado mas inexistente no antigo (o cliente enganou-se):
	// tem de recusar em vez de gravar um ficheiro truncado/lixo.
	err := rebuildFromDelta(&out, strings.NewReader("AAAA"), strings.NewReader(""), 4, 8, 4, set)
	if err == nil {
		t.Fatal("esperava erro quando falta um bloco no conteúdo antigo")
	}

	// Corpo mais curto do que os blocos marcados como alterados.
	out.Reset()
	err = rebuildFromDelta(&out, strings.NewReader("AAAA"), strings.NewReader("X"), 4, 4, 4,
		map[int64]bool{0: true})
	if err == nil {
		t.Fatal("esperava erro quando o corpo é curto demais")
	}
}

func TestParseBlockList(t *testing.T) {
	got, err := parseBlockList("0, 3,7")
	if err != nil {
		t.Fatalf("parseBlockList: %v", err)
	}
	for _, i := range []int64{0, 3, 7} {
		if !got[i] {
			t.Errorf("faltou o bloco %d", i)
		}
	}
	if got[1] {
		t.Error("bloco 1 não devia estar marcado")
	}
	if empty, err := parseBlockList(""); err != nil || len(empty) != 0 {
		t.Errorf("lista vazia devia dar conjunto vazio, deu %v (%v)", empty, err)
	}
}
