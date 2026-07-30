package filecrypt

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
)

// EncryptTo cifra `src` (com `plainSize` bytes) para `dst`, bloco a bloco. Nunca carrega o ficheiro
// inteiro em memória: um envio de 2 GB gasta um bloco de cada vez.
//
// `wrapped` é a chave deste objeto já embrulhada, que viaja no cabeçalho — é o que torna a escrita
// atómica (ver o comentário do pacote). `key` é a mesma chave em claro, para cifrar os blocos.
//
// Devolve erro se `src` acabar antes ou depois de `plainSize` — o tamanho vai no cabeçalho e é o que
// permite servir intervalos sem ler o objeto todo, por isso tem de estar certo.
func EncryptTo(dst io.Writer, src io.Reader, plainSize int64, key []byte, h Header) error {
	chunkSize := h.ChunkSize
	if chunkSize <= 0 {
		chunkSize = DefaultChunkSize
		h.ChunkSize = chunkSize
	}
	gcm, err := newGCM(key)
	if err != nil {
		return err
	}

	var prefix [prefixSize]byte
	if _, err := rand.Read(prefix[:]); err != nil {
		return err
	}
	h.PlainSize, h.NoncePrefix = plainSize, prefix
	if _, err := dst.Write(MarshalHeader(h)); err != nil {
		return err
	}

	buf := make([]byte, chunkSize)
	out := make([]byte, 0, chunkSize+TagSize)
	var lidos int64
	for i := int64(0); ; i++ {
		n, rerr := io.ReadFull(src, buf)
		if n > 0 {
			lidos += int64(n)
			ultimo := lidos >= plainSize
			out = gcm.Seal(out[:0], nonce(prefix, i), buf[:n], aad(i, ultimo))
			if _, werr := dst.Write(out); werr != nil {
				return werr
			}
			if ultimo {
				break
			}
		}
		if rerr != nil {
			if errors.Is(rerr, io.EOF) || errors.Is(rerr, io.ErrUnexpectedEOF) {
				break
			}
			return rerr
		}
	}
	if lidos != plainSize {
		return fmt.Errorf("filecrypt: esperava %d bytes, li %d", plainSize, lidos)
	}
	return nil
}

// DecryptReader devolve um leitor sequencial do conteúdo em claro. É o caminho normal de um
// download: lê-se do princípio ao fim, um bloco de cada vez.
//
// O cabeçalho já tem de estar consumido de `src` e passado em `h` — quem chama precisa de o ler
// primeiro para saber se o objeto está sequer cifrado.
func DecryptReader(src io.Reader, h Header, key []byte) (io.Reader, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	return &decryptor{src: src, h: h, gcm: gcm, buf: make([]byte, h.ChunkSize+TagSize)}, nil
}

type decryptor struct {
	src io.Reader
	h   Header
	gcm interface {
		Open([]byte, []byte, []byte, []byte) ([]byte, error)
	}
	buf   []byte // bloco cifrado lido do armazenamento
	plain []byte // sobra em claro do bloco atual, ainda por entregar
	index int64
	lidos int64 // bytes em claro já entregues
	fim   bool
}

func (d *decryptor) Read(p []byte) (int, error) {
	for len(d.plain) == 0 {
		if d.fim || d.lidos >= d.h.PlainSize {
			return 0, io.EOF
		}
		// Quanto falta em claro decide o tamanho deste bloco: o último é mais curto.
		restam := d.h.PlainSize - d.lidos
		tamanho := int64(d.h.ChunkSize)
		ultimo := false
		if restam <= tamanho {
			tamanho, ultimo = restam, true
		}
		n, err := io.ReadFull(d.src, d.buf[:tamanho+TagSize])
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return 0, ErrCorrupt // o cabeçalho prometia mais bytes do que existem
			}
			return 0, err
		}
		aberto, err := d.gcm.Open(nil, nonce(d.h.NoncePrefix, d.index), d.buf[:n], aad(d.index, ultimo))
		if err != nil {
			return 0, ErrCorrupt
		}
		d.plain = aberto
		d.lidos += int64(len(aberto))
		d.index++
		d.fim = ultimo
	}
	n := copy(p, d.plain)
	d.plain = d.plain[n:]
	return n, nil
}
