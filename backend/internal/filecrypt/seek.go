package filecrypt

import (
	"errors"
	"fmt"
	"io"
)

// SeekReader dá acesso aleatório ao conteúdo em claro de um objeto cifrado.
//
// É isto que mantém o vídeo com avanço e retrocesso a funcionar depois da cifra: o http.ServeContent
// pede um intervalo de bytes, este leitor traduz o intervalo em blocos, vai buscar só esses ao
// armazenamento e decifra-os. Um salto para o meio de um ficheiro de 2 GB lê 1 MiB, não 2 GB.
//
// `src` é o objeto CIFRADO com acesso aleatório — no nosso caso o *minio.Object, que resolve cada
// Seek com um pedido Range ao armazenamento.
type SeekReader struct {
	src io.ReadSeeker
	h   Header
	gcm interface {
		Open([]byte, []byte, []byte, []byte) ([]byte, error)
	}

	pos   int64 // posição atual, em bytes de conteúdo em claro
	chunk []byte
	// Índice do bloco que está em `chunk`. -1 quando não há nada carregado.
	chunkIndex int64
}

// NewSeekReader monta o leitor. O cabeçalho já tem de ter sido lido de `src`.
func NewSeekReader(src io.ReadSeeker, h Header, key []byte) (*SeekReader, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	return &SeekReader{src: src, h: h, gcm: gcm, chunkIndex: -1}, nil
}

// Size devolve o tamanho em claro — o que o cliente tem de ver nos cabeçalhos HTTP.
func (r *SeekReader) Size() int64 { return r.h.PlainSize }

func (r *SeekReader) Read(p []byte) (int, error) {
	if r.pos >= r.h.PlainSize {
		return 0, io.EOF
	}
	idx := r.pos / int64(r.h.ChunkSize)
	if idx != r.chunkIndex {
		if err := r.carregar(idx); err != nil {
			return 0, err
		}
	}
	dentro := r.pos - idx*int64(r.h.ChunkSize)
	n := copy(p, r.chunk[dentro:])
	r.pos += int64(n)
	return n, nil
}

func (r *SeekReader) Seek(offset int64, whence int) (int64, error) {
	var abs int64
	switch whence {
	case io.SeekStart:
		abs = offset
	case io.SeekCurrent:
		abs = r.pos + offset
	case io.SeekEnd:
		abs = r.h.PlainSize + offset
	default:
		return 0, fmt.Errorf("filecrypt: whence inválido: %d", whence)
	}
	if abs < 0 {
		return 0, errors.New("filecrypt: posição negativa")
	}
	// Posicionar depois do fim é legítimo (o io.Seeker permite-o); a leitura seguinte dá EOF.
	r.pos = abs
	return abs, nil
}

// carregar traz um bloco do armazenamento e decifra-o.
func (r *SeekReader) carregar(idx int64) error {
	// O último bloco é mais curto: o tamanho vem do que falta até ao fim em claro.
	restam := r.h.PlainSize - idx*int64(r.h.ChunkSize)
	if restam <= 0 {
		return io.EOF
	}
	tamanho := int64(r.h.ChunkSize)
	ultimo := false
	if restam <= tamanho {
		tamanho, ultimo = restam, true
	}

	if _, err := r.src.Seek(chunkOffset(r.h, idx), io.SeekStart); err != nil {
		return err
	}
	buf := make([]byte, tamanho+TagSize)
	if _, err := io.ReadFull(r.src, buf); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return ErrCorrupt
		}
		return err
	}
	aberto, err := r.gcm.Open(nil, nonce(r.h.NoncePrefix, idx), buf, aad(idx, ultimo))
	if err != nil {
		return ErrCorrupt
	}
	r.chunk, r.chunkIndex = aberto, idx
	return nil
}
