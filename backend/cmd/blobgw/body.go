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
type checker struct {
	source   io.Reader
	digest   hash.Hash
	declared string
}

// semCorpoAssinado é o que a AWS aceita para "não assinei o corpo". Aparece em clientes que
// transmitem em fluxo; aqui não há nada para comparar e a verificação é saltada.
const unsignedPayload = "UNSIGNED-PAYLOAD"

func newChecker(r io.Reader, declared string) *checker {
	c := &checker{source: r, declared: declared}
	if declared != unsignedPayload {
		c.digest = sha256.New()
	}
	return c
}

func (c *checker) Read(p []byte) (int, error) {
	n, err := c.source.Read(p)
	if n > 0 && c.digest != nil {
		c.digest.Write(p[:n])
	}
	return n, err
}

func (c *checker) verify() error {
	if c.digest == nil {
		return nil
	}
	got := hex.EncodeToString(c.digest.Sum(nil))
	if got != c.declared {
		return errf("resumo do corpo %s, assinado %s", got[:12], first(c.declared, 12))
	}
	return nil
}

func first(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
