// Package filecrypt cifra objetos grandes em blocos, de forma a permitir leitura por intervalos.
//
// PORQUÊ EM BLOCOS e não uma passagem única: o produto serve vídeo com avanço e retrocesso
// (http.ServeContent honra o cabeçalho Range) e o sync do computador lê ficheiros aos pedaços para
// comparar hashes. Uma cifra que obrigue a decifrar do princípio para chegar ao meio de um ficheiro
// de 2 GB tornaria as duas coisas inutilizáveis. Com blocos de tamanho fixo, um intervalo de bytes
// traduz-se em blocos e decifram-se só esses.
//
// A CHAVE DO OBJETO VIAJA DENTRO DO OBJETO, embrulhada. Isto não é detalhe: é o que torna a escrita
// ATÓMICA. Se a chave vivesse numa tabela à parte, gravar um ficheiro seriam duas escritas — o
// objeto e a linha — e qualquer ordem tem uma janela de falha: escrever o objeto primeiro deixa
// cifrado sem chave (dados perdidos), escrever a linha primeiro deixa linhas órfãs. Com a chave
// embrulhada no cabeçalho há UMA escrita, e o PUT do armazenamento ou acontece por inteiro ou não
// acontece. Não há ordem para acertar porque não há duas coisas para ordenar.
//
// Quem guarda o objeto fica com a chave embrulhada — e não a consegue abrir: precisaria da chave do
// tenant (na nossa base de dados) e da KEK (no nosso ambiente), e não tem nenhuma das duas.
//
// FORMATO
//
//	cabeçalho (32) | chave embrulhada (n) | bloco 0 | bloco 1 | … | bloco n
//
//	cabeçalho: "BSK1" (4) | versão (1) | opções (1) | reservado (2) | tamanho de bloco (4)
//	           | tamanho em claro (8) | prefixo do nonce (4) | versão da KEK (2)
//	           | tamanho da chave embrulhada (2) | reservado (4)
//	bloco:     AES-256-GCM do pedaço em claro, com selo de 16 bytes no fim
//
// A MAGIA NO CABEÇALHO distingue um objeto cifrado de um objeto anterior à cifra. É o que permite
// migrar o conteúdo antigo sem paragem: enquanto um objeto não for reescrito, não tem magia e é
// servido tal como está.
//
// NONCE: prefixo aleatório de 4 bytes ‖ índice do bloco em 8 bytes. Como a chave é única por
// objeto, o índice já bastaria; o prefixo é cinto e suspensórios.
//
// DADOS ASSOCIADOS: índice do bloco e marca de último bloco entram autenticados mas não cifrados.
// Sem isso, quem controlasse o armazenamento podia reordenar blocos ou cortar o fim do ficheiro sem
// que a decifra desse erro — o selo de cada bloco continuaria válido isoladamente.
package filecrypt

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	// Magic identifica um objeto cifrado por nós. Muda se o formato mudar de forma incompatível.
	Magic = "BSK1"

	// FixedHeaderSize é a parte de tamanho fixo do cabeçalho. A chave embrulhada vem logo a seguir
	// e o seu comprimento está aqui declarado — é o que permite saber onde começa o bloco 0 depois
	// de ler apenas estes 32 bytes.
	FixedHeaderSize = 32

	// TagSize é o selo do AES-GCM.
	TagSize = 16

	// DefaultChunkSize — 1 MiB. Compromisso entre sobrecarga (16 bytes por bloco, 0,0015%) e
	// granularidade do seek: um salto no vídeo decifra no máximo 1 MiB a mais do que o pedido.
	DefaultChunkSize = 1 << 20

	// KeySize é o tamanho da chave AES-256, usado para as DEK e para as chaves de tenant.
	KeySize = 32

	// FlagTenantWrapped diz que a chave do objeto foi embrulhada pela chave do TENANT (o caso
	// normal). Sem esta opção, foi embrulhada diretamente pela KEK — objetos de sistema, cuja
	// chave de armazenamento não começa por um tenant.
	FlagTenantWrapped = 1 << 0

	nonceSize  = 12 // 4 de prefixo + 8 de contador
	prefixSize = 4
)

var (
	// ErrNotEncrypted diz que o objeto não tem a nossa magia — é conteúdo legado, em claro.
	ErrNotEncrypted = errors.New("filecrypt: objeto sem cabeçalho cifrado")
	// ErrCorrupt cobre selo inválido, cabeçalho truncado e bloco fora de sítio.
	ErrCorrupt = errors.New("filecrypt: objeto corrompido ou adulterado")
)

// Header descreve um objeto cifrado.
type Header struct {
	ChunkSize     int
	PlainSize     int64
	NoncePrefix   [prefixSize]byte
	TenantWrapped bool
	// KEKVersion só tem significado quando TenantWrapped é falso.
	KEKVersion int
	// WrappedKey é a chave do objeto, embrulhada. Preenchida por ParseWrappedKey.
	WrappedKey []byte
}

// DataOffset é onde começa o bloco 0.
func (h Header) DataOffset() int64 { return int64(FixedHeaderSize + len(h.WrappedKey)) }

// MarshalHeader serializa o cabeçalho fixo seguido da chave embrulhada.
func MarshalHeader(h Header) []byte {
	buf := make([]byte, FixedHeaderSize, FixedHeaderSize+len(h.WrappedKey))
	copy(buf[0:4], Magic)
	buf[4] = 1 // versão do formato
	if h.TenantWrapped {
		buf[5] |= FlagTenantWrapped
	}
	binary.BigEndian.PutUint32(buf[8:12], uint32(h.ChunkSize))
	binary.BigEndian.PutUint64(buf[12:20], uint64(h.PlainSize))
	copy(buf[20:24], h.NoncePrefix[:])
	binary.BigEndian.PutUint16(buf[24:26], uint16(h.KEKVersion))
	binary.BigEndian.PutUint16(buf[26:28], uint16(len(h.WrappedKey)))
	return append(buf, h.WrappedKey...)
}

// ParseHeader lê a parte fixa. Devolve ErrNotEncrypted quando a magia não bate — que é o caso
// normal para os objetos que já estavam no balde antes desta cifra existir.
//
// A chave embrulhada NÃO vem aqui: quem chama lê os `WrappedKeyLen` bytes seguintes e passa-os a
// ParseWrappedKey. É essa separação que permite decidir com 32 bytes se vale a pena ler mais.
func ParseHeader(buf []byte) (Header, error) {
	if len(buf) < FixedHeaderSize || string(buf[0:4]) != Magic {
		return Header{}, ErrNotEncrypted
	}
	if buf[4] != 1 {
		return Header{}, fmt.Errorf("filecrypt: versão de formato %d desconhecida", buf[4])
	}
	h := Header{
		ChunkSize:     int(binary.BigEndian.Uint32(buf[8:12])),
		PlainSize:     int64(binary.BigEndian.Uint64(buf[12:20])),
		TenantWrapped: buf[5]&FlagTenantWrapped != 0,
		KEKVersion:    int(binary.BigEndian.Uint16(buf[24:26])),
	}
	if h.ChunkSize <= 0 || h.PlainSize < 0 {
		return Header{}, ErrCorrupt
	}
	copy(h.NoncePrefix[:], buf[20:24])
	h.WrappedKey = make([]byte, binary.BigEndian.Uint16(buf[26:28]))
	return h, nil
}

// WrappedKeyLen diz quantos bytes de chave embrulhada vêm a seguir ao cabeçalho fixo.
func (h Header) WrappedKeyLen() int { return len(h.WrappedKey) }

// EncryptedSize devolve o tamanho do objeto cifrado. Serve para o upload declarar o comprimento sem
// ter de construir tudo em memória.
func EncryptedSize(plainSize int64, chunkSize, wrappedLen int) int64 {
	base := int64(FixedHeaderSize + wrappedLen)
	if plainSize == 0 {
		return base
	}
	blocos := (plainSize + int64(chunkSize) - 1) / int64(chunkSize)
	return base + plainSize + blocos*TagSize
}

// chunkOffset devolve onde começa, no objeto cifrado, o bloco de índice i.
func chunkOffset(h Header, i int64) int64 {
	return h.DataOffset() + i*int64(h.ChunkSize+TagSize)
}

// nonce compõe o nonce do bloco: prefixo ‖ índice.
func nonce(prefix [prefixSize]byte, index int64) []byte {
	n := make([]byte, nonceSize)
	copy(n[:prefixSize], prefix[:])
	binary.BigEndian.PutUint64(n[prefixSize:], uint64(index))
	return n
}

// aad são os dados associados do bloco: índice e marca de último. Autenticados, não cifrados —
// é isto que impede reordenar blocos e cortar o fim do ficheiro.
func aad(index int64, last bool) []byte {
	b := make([]byte, 9)
	binary.BigEndian.PutUint64(b[:8], uint64(index))
	if last {
		b[8] = 1
	}
	return b
}

func newGCM(key []byte) (cipher.AEAD, error) {
	if len(key) != KeySize {
		return nil, fmt.Errorf("filecrypt: chave tem de ter %d bytes, tem %d", KeySize, len(key))
	}
	blk, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(blk)
}
