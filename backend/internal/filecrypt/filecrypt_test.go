package filecrypt

import (
	"bytes"
	"crypto/rand"
	"errors"
	"io"
	"strings"
	"testing"
)

// wrapExemplo simula uma chave embrulhada: os testes deste pacote não precisam de a saber abrir,
// só de a ver viajar no cabeçalho com o tamanho certo.
var wrapExemplo = bytes.Repeat([]byte{0xAB}, 60)

func hdr(bloco int) Header {
	return Header{ChunkSize: bloco, TenantWrapped: true, WrappedKey: wrapExemplo}
}

func hdrLen() int64 { return int64(FixedHeaderSize + len(wrapExemplo)) }

// leHeader lê a parte fixa e a chave embrulhada de um objeto cifrado em memória.
func leHeader(t *testing.T, b []byte) (Header, error) {
	t.Helper()
	h, err := ParseHeader(b[:FixedHeaderSize])
	if err != nil {
		return h, err
	}
	copy(h.WrappedKey, b[FixedHeaderSize:FixedHeaderSize+h.WrappedKeyLen()])
	return h, nil
}

func chave(t *testing.T) []byte {
	t.Helper()
	k, err := NewKey()
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// idaEVolta é o teste que mais importa: o que sai tem de ser byte a byte o que entrou, em vários
// tamanhos à volta da fronteira do bloco — é aí que os erros de índice se escondem.
func TestIdaEVolta(t *testing.T) {
	k := chave(t)
	const bloco = 1024
	tamanhos := []int{0, 1, 100, bloco - 1, bloco, bloco + 1, 3*bloco - 1, 3 * bloco, 3*bloco + 7}

	for _, n := range tamanhos {
		claro := make([]byte, n)
		if _, err := rand.Read(claro); err != nil {
			t.Fatal(err)
		}
		var cifrado bytes.Buffer
		if err := EncryptTo(&cifrado, bytes.NewReader(claro), int64(n), k, hdr(bloco)); err != nil {
			t.Fatalf("tamanho %d: cifrar: %v", n, err)
		}
		if got, want := int64(cifrado.Len()), EncryptedSize(int64(n), bloco, len(wrapExemplo)); got != want {
			t.Errorf("tamanho %d: cifrado tem %d bytes, EncryptedSize dizia %d", n, got, want)
		}

		h, err := leHeader(t, cifrado.Bytes())
		if err != nil {
			t.Fatalf("tamanho %d: cabeçalho: %v", n, err)
		}
		if h.PlainSize != int64(n) {
			t.Errorf("tamanho %d: cabeçalho diz %d", n, h.PlainSize)
		}
		r, err := DecryptReader(bytes.NewReader(cifrado.Bytes()[h.DataOffset():]), h, k)
		if err != nil {
			t.Fatal(err)
		}
		volta, err := io.ReadAll(r)
		if err != nil {
			t.Fatalf("tamanho %d: decifrar: %v", n, err)
		}
		if !bytes.Equal(volta, claro) {
			t.Errorf("tamanho %d: conteúdo diferente na volta", n)
		}
	}
}

// Um objeto antigo, em claro, tem de ser reconhecido como tal — é o que mantém a migração segura
// quando falha a meio (docs/cifra-ficheiros.md §2d).
func TestObjetoEmClaroNaoEConfundido(t *testing.T) {
	_, err := ParseHeader([]byte("PK\x03\x04 isto é um zip, não nosso......."))
	if !errors.Is(err, ErrNotEncrypted) {
		t.Fatalf("esperava ErrNotEncrypted, veio %v", err)
	}
}

// Seek: o ponto de todo o formato em blocos. Cada leitura a partir de uma posição arbitrária tem de
// devolver exatamente a cauda do conteúdo.
func TestSeekLeDeQualquerPosicao(t *testing.T) {
	k := chave(t)
	const bloco = 256
	claro := make([]byte, 5*bloco+33)
	if _, err := rand.Read(claro); err != nil {
		t.Fatal(err)
	}
	var cifrado bytes.Buffer
	if err := EncryptTo(&cifrado, bytes.NewReader(claro), int64(len(claro)), k, hdr(bloco)); err != nil {
		t.Fatal(err)
	}
	h, err := leHeader(t, cifrado.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	sr, err := NewSeekReader(bytes.NewReader(cifrado.Bytes()), h, k)
	if err != nil {
		t.Fatal(err)
	}
	if sr.Size() != int64(len(claro)) {
		t.Fatalf("Size() = %d, esperava %d", sr.Size(), len(claro))
	}

	// Posições escolhidas nas fronteiras e no meio dos blocos, que é onde se erra.
	for _, off := range []int64{0, 1, bloco - 1, bloco, bloco + 1, 3 * bloco, int64(len(claro)) - 1} {
		if _, err := sr.Seek(off, io.SeekStart); err != nil {
			t.Fatalf("off %d: seek: %v", off, err)
		}
		got, err := io.ReadAll(sr)
		if err != nil {
			t.Fatalf("off %d: ler: %v", off, err)
		}
		if !bytes.Equal(got, claro[off:]) {
			t.Errorf("off %d: leu %d bytes, esperava %d", off, len(got), len(claro)-int(off))
		}
	}

	// SeekEnd é o que o http.ServeContent usa para descobrir o tamanho.
	if p, err := sr.Seek(0, io.SeekEnd); err != nil || p != int64(len(claro)) {
		t.Fatalf("SeekEnd = %d, %v", p, err)
	}
}

// Adulterar um byte tem de dar erro, não conteúdo silenciosamente errado.
func TestAdulteracaoEDetetada(t *testing.T) {
	k := chave(t)
	claro := bytes.Repeat([]byte("a"), 3000)
	var cifrado bytes.Buffer
	if err := EncryptTo(&cifrado, bytes.NewReader(claro), int64(len(claro)), k, hdr(1024)); err != nil {
		t.Fatal(err)
	}
	b := cifrado.Bytes()
	b[int(hdrLen())+10] ^= 0xff

	h, _ := leHeader(t, b)
	r, err := DecryptReader(bytes.NewReader(b[h.DataOffset():]), h, k)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadAll(r); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("esperava ErrCorrupt, veio %v", err)
	}
}

// Truncar o fim tem de dar erro. Sem a marca de último bloco nos dados associados, um ficheiro
// cortado passaria como válido — cada bloco restante continua individualmente bem selado.
func TestTruncarEDetetado(t *testing.T) {
	k := chave(t)
	claro := bytes.Repeat([]byte("b"), 3000)
	var cifrado bytes.Buffer
	if err := EncryptTo(&cifrado, bytes.NewReader(claro), int64(len(claro)), k, hdr(1024)); err != nil {
		t.Fatal(err)
	}
	b := cifrado.Bytes()
	cortado := b[:len(b)-(1024+TagSize)] // menos um bloco

	h, _ := leHeader(t, cortado)
	r, err := DecryptReader(bytes.NewReader(cortado[h.DataOffset():]), h, k)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadAll(r); err == nil {
		t.Fatal("um ficheiro truncado foi aceite como completo")
	}
}

// Chave errada não pode devolver conteúdo.
func TestChaveErrada(t *testing.T) {
	k1, k2 := chave(t), chave(t)
	var cifrado bytes.Buffer
	if err := EncryptTo(&cifrado, strings.NewReader("segredo"), 7, k1, hdr(1024)); err != nil {
		t.Fatal(err)
	}
	h, _ := leHeader(t, cifrado.Bytes())
	r, err := DecryptReader(bytes.NewReader(cifrado.Bytes()[h.DataOffset():]), h, k2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadAll(r); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("chave errada devia dar ErrCorrupt, deu %v", err)
	}
}

func TestKEKEmbrulhaEAbre(t *testing.T) {
	k1 := strings.Repeat("11", KeySize)
	k2 := strings.Repeat("22", KeySize)
	kek, err := ParseKEK("1:" + k1 + ",2:" + k2)
	if err != nil {
		t.Fatal(err)
	}
	if kek.CurrentVersion() != 2 {
		t.Fatalf("a corrente devia ser a mais alta, veio %d", kek.CurrentVersion())
	}

	material := chave(t)
	wrapped, versao, err := kek.Wrap(material)
	if err != nil {
		t.Fatal(err)
	}
	if versao != 2 {
		t.Fatalf("embrulhou com v%d", versao)
	}
	aberto, err := kek.Unwrap(wrapped, versao)
	if err != nil || !bytes.Equal(aberto, material) {
		t.Fatalf("não voltou igual: %v", err)
	}

	// Material antigo continua a abrir-se enquanto a KEK antiga estiver configurada — é o que
	// permite rodar em fundo sem parar o serviço.
	antigo, err := kek.WrapWithVersion(material, 1)
	if err != nil {
		t.Fatal(err)
	}
	if aberto, err := kek.Unwrap(antigo, 1); err != nil || !bytes.Equal(aberto, material) {
		t.Fatalf("v1 não abriu: %v", err)
	}
}

func TestKEKRecusaConfiguracaoInvalida(t *testing.T) {
	casos := map[string]string{
		"hex curto":       "1:abcd",
		"não hex":         "1:" + strings.Repeat("zz", KeySize),
		"versão inválida": "x:" + strings.Repeat("11", KeySize),
	}
	for nome, valor := range casos {
		if _, err := ParseKEK(valor); err == nil {
			t.Errorf("%s: devia recusar", nome)
		}
	}
	// Vazio é legítimo: significa cifra desligada.
	if k, err := ParseKEK(""); err != nil || k != nil {
		t.Errorf("vazio devia dar (nil, nil), deu (%v, %v)", k, err)
	}
}

func TestDEKPassaPelaChaveDoTenant(t *testing.T) {
	tenant, dek := chave(t), chave(t)
	wrapped, err := WrapDEK(tenant, dek)
	if err != nil {
		t.Fatal(err)
	}
	aberto, err := UnwrapDEK(tenant, wrapped)
	if err != nil || !bytes.Equal(aberto, dek) {
		t.Fatalf("DEK não voltou: %v", err)
	}
	// A chave de outro tenant não abre — é isto que sustenta a promessa de eliminação por tenant.
	if _, err := UnwrapDEK(chave(t), wrapped); !errors.Is(err, ErrCorrupt) {
		t.Fatal("a chave de outro tenant abriu a DEK")
	}
}

func TestHeaderRoundTrip(t *testing.T) {
	var prefixo [prefixSize]byte
	copy(prefixo[:], []byte{1, 2, 3, 4})
	h := Header{
		ChunkSize: 4096, PlainSize: 123456789, NoncePrefix: prefixo,
		TenantWrapped: true, WrappedKey: wrapExemplo,
	}
	bruto := MarshalHeader(h)
	volta, err := ParseHeader(bruto[:FixedHeaderSize])
	if err != nil {
		t.Fatal(err)
	}
	copy(volta.WrappedKey, bruto[FixedHeaderSize:])

	if volta.ChunkSize != h.ChunkSize || volta.PlainSize != h.PlainSize ||
		volta.NoncePrefix != h.NoncePrefix || !volta.TenantWrapped {
		t.Fatalf("cabeçalho mudou: %+v vs %+v", volta, h)
	}
	// A chave embrulhada tem de voltar byte a byte: é ela que abre o objeto.
	if !bytes.Equal(volta.WrappedKey, h.WrappedKey) {
		t.Fatal("a chave embrulhada não sobreviveu à ida e volta")
	}
	// Objeto de sistema: sem tenant, a versão da KEK é que conta.
	sis := MarshalHeader(Header{ChunkSize: 4096, KEKVersion: 7, WrappedKey: wrapExemplo})
	v2, err := ParseHeader(sis[:FixedHeaderSize])
	if err != nil {
		t.Fatal(err)
	}
	if v2.TenantWrapped || v2.KEKVersion != 7 {
		t.Fatalf("objeto de sistema: %+v", v2)
	}
}
