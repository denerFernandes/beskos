package main

import (
	"crypto/sha256"
	"encoding/hex"
	"hash"
	"io"
)

// conferidor lê o corpo enquanto vai calculando o resumo, para o comparar no fim com o que o
// cliente assinou.
//
// PORQUÊ EM PASSAGEM e não a ler tudo primeiro: um anexo de dezenas de MiB não tem de caber em
// memória para ser verificado. O preço é a verificação só concluir DEPOIS de o objeto estar
// escrito — daí quem chama apagar o que escreveu quando não bate certo.
type conferidor struct {
	origem    io.Reader
	digestor  hash.Hash
	declarado string
}

// semCorpoAssinado é o que a AWS aceita para "não assinei o corpo". Aparece em clientes que
// transmitem em fluxo; aqui não há nada para comparar e a verificação é saltada.
const semCorpoAssinado = "UNSIGNED-PAYLOAD"

func conferente(r io.Reader, declarado string) *conferidor {
	c := &conferidor{origem: r, declarado: declarado}
	if declarado != semCorpoAssinado {
		c.digestor = sha256.New()
	}
	return c
}

func (c *conferidor) Read(p []byte) (int, error) {
	n, err := c.origem.Read(p)
	if n > 0 && c.digestor != nil {
		c.digestor.Write(p[:n])
	}
	return n, err
}

func (c *conferidor) confere() error {
	if c.digestor == nil {
		return nil
	}
	obtido := hex.EncodeToString(c.digestor.Sum(nil))
	if obtido != c.declarado {
		return errf("resumo do corpo %s, assinado %s", obtido[:12], primeiros(c.declarado, 12))
	}
	return nil
}

func primeiros(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
