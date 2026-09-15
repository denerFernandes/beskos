package storage

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/minio/minio-go/v7"

	"github.com/beskosorg/beskos/backend/internal/db"
	"github.com/beskosorg/beskos/backend/internal/filecrypt"
)

// Teste de integração contra o MinIO e o Postgres locais (CLAUDE.md §2.7). Sem as envs é saltado,
// para não partir o `go test ./...` numa máquina sem containers.
//
//	STORAGE_TEST_ENDPOINT=localhost:9000 STORAGE_TEST_KEY=… STORAGE_TEST_SECRET=… \
//	STORAGE_TEST_BUCKET=mailin STORAGE_TEST_DB=postgres://… \
//	go test ./internal/storage -run TestIntegration -v
//
// O que este teste prova, e que os testes do filecrypt não podem provar: que o que fica NO BALDE é
// ilegível. Os testes de unidade verificam a ida e volta do formato — passariam na mesma se o
// Service se esquecesse de cifrar antes de enviar. Aqui olha-se para os bytes do lado de lá.

func testService(t *testing.T) (*Service, *db.Queries, uuid.UUID) {
	t.Helper()
	endpoint := os.Getenv("STORAGE_TEST_ENDPOINT")
	if endpoint == "" {
		t.Skip("STORAGE_TEST_ENDPOINT not set; skipping the integration test")
	}

	s, err := New(endpoint, os.Getenv("STORAGE_TEST_KEY"), os.Getenv("STORAGE_TEST_SECRET"),
		os.Getenv("STORAGE_TEST_BUCKET"), "", false)
	if err != nil {
		t.Fatalf("ligar ao armazenamento: %v", err)
	}

	pool, err := pgxpool.New(context.Background(), os.Getenv("STORAGE_TEST_DB"))
	if err != nil {
		t.Fatalf("connect to the database: %v", err)
	}
	t.Cleanup(pool.Close)
	q := db.New(pool)

	// Um tenant que existe mesmo: a chave do tenant tem uma referência para `tenants`.
	tenant := testTenant(t, pool)

	kek := make([]byte, filecrypt.KeySize)
	if _, err := rand.Read(kek); err != nil {
		t.Fatal(err)
	}
	if err := s.EnableEncryption(q, "1:"+hex.EncodeToString(kek)); err != nil {
		t.Fatalf("ligar a cifra: %v", err)
	}
	if !s.Encrypting() {
		t.Fatal("encryption should be on")
	}
	return s, q, tenant
}

func testTenant(t *testing.T, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	id := uuid.New()
	_, err := pool.Exec(ctx, `INSERT INTO tenants (id, nome) VALUES ($1, $2)`, id, "storage-test")
	if err != nil {
		t.Fatalf("criar tenant de teste: %v", err)
	}
	t.Cleanup(func() {
		// A chave do tenant desaparece com ele (ON DELETE CASCADE) — é essa a garantia de
		// eliminação que a migração 00128 existe para dar.
		_, _ = pool.Exec(context.Background(), `DELETE FROM tenants WHERE id = $1`, id)
	})
	return id
}

func TestIntegrationEncryptsInBucket(t *testing.T) {
	s, _, tenant := testService(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	// Maior do que um bloco, para exercitar mais de um bloco de verdade.
	plain := bytes.Repeat([]byte("conteudo confidencial do cliente. "), 40_000)
	key := fmt.Sprintf("%s/teste-%s", tenant, uuid.NewString())
	t.Cleanup(func() { _ = s.Remove(context.Background(), key) })

	if err := s.Put(ctx, key, bytes.NewReader(plain), int64(len(plain)), "application/octet-stream"); err != nil {
		t.Fatalf("Put: %v", err)
	}

	// 1. O que está NO BALDE não é o que foi enviado.
	raw := readRaw(t, s, key)
	if bytes.Contains(raw, []byte("conteudo confidencial")) {
		t.Fatal("the object in the bucket contains the plaintext")
	}
	if string(raw[:4]) != filecrypt.Magic {
		t.Fatalf("the object in the bucket lacks the %q magic, it starts with %q", filecrypt.Magic, raw[:4])
	}

	// 2. A chave do objeto viaja lá dentro — é o que torna a escrita uma operação só.
	h, err := filecrypt.ParseHeader(raw[:filecrypt.FixedHeaderSize])
	if err != nil {
		t.Fatalf("header: %v", err)
	}
	if !h.TenantWrapped {
		t.Fatal("the key should be wrapped by the tenant key")
	}
	if h.WrappedKeyLen() == 0 {
		t.Fatal("the header carries no wrapped key")
	}
	if h.PlainSize != int64(len(plain)) {
		t.Fatalf("plaintext size %d, want %d", h.PlainSize, len(plain))
	}

	// 3. E volta inteiro pelo Get.
	r, err := s.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer r.Close()
	back, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(back, plain) {
		t.Fatalf("came back different: %d bytes vs %d", len(back), len(plain))
	}
}

func TestIntegrationSeekInTheMiddle(t *testing.T) {
	s, _, tenant := testService(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	// 5 MiB: o salto cai no meio de um bloco que não é o primeiro nem o último, que é o caso em que
	// um erro de aritmética de blocos aparece.
	plain := make([]byte, 5<<20)
	if _, err := rand.Read(plain); err != nil {
		t.Fatal(err)
	}
	key := fmt.Sprintf("%s/seek-%s", tenant, uuid.NewString())
	t.Cleanup(func() { _ = s.Remove(context.Background(), key) })

	if err := s.Put(ctx, key, bytes.NewReader(plain), int64(len(plain)), "video/mp4"); err != nil {
		t.Fatalf("Put: %v", err)
	}

	sr, err := s.GetSeek(ctx, key)
	if err != nil {
		t.Fatalf("GetSeek: %v", err)
	}
	defer sr.Close()

	const skip = 3<<20 + 12345 // no meio do quarto bloco
	if _, err := sr.Seek(skip, io.SeekStart); err != nil {
		t.Fatalf("Seek: %v", err)
	}
	chunk := make([]byte, 4096)
	if _, err := io.ReadFull(sr, chunk); err != nil {
		t.Fatalf("read after the seek: %v", err)
	}
	if !bytes.Equal(chunk, plain[skip:skip+4096]) {
		t.Fatal("the content after the seek is not the original file's")
	}
}

// TestIntegracaoSelagem prova o caminho por onde entram as gravações compostas e o conteúdo
// anterior à cifra: um objeto que chegou ao balde em claro é selado sem mudar de sítio nem de nome.
func TestIntegrationSealing(t *testing.T) {
	s, _, tenant := testService(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	plain := []byte("gravação escrita pelo egress, direto no balde, sem passar por nós")
	key := fmt.Sprintf("%s/recordings/selar-%s.mp4", tenant, uuid.NewString())
	t.Cleanup(func() { _ = s.Remove(context.Background(), key) })

	// Escreve à socapa da cifra, como o LiveKit faz com as credenciais S3.
	unencrypted := &Service{client: s.client, bucket: s.bucket}
	if err := unencrypted.Put(ctx, key, bytes.NewReader(plain), int64(len(plain)), "video/mp4"); err != nil {
		t.Fatalf("Put em claro: %v", err)
	}

	cifrado, err := s.IsEncrypted(ctx, key)
	if err != nil {
		t.Fatalf("IsEncrypted: %v", err)
	}
	if cifrado {
		t.Fatal("it was just written in the clear and claims to be encrypted")
	}

	// Antes de selar, o objeto em claro TEM de continuar a ler-se — é o que permite migrar sem
	// paragem.
	if read := readAll(t, s, ctx, key); !bytes.Equal(read, plain) {
		t.Fatal("an object predating encryption stopped being readable")
	}

	if err := s.Seal(ctx, key); err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if raw := readRaw(t, s, key); string(raw[:4]) != filecrypt.Magic {
		t.Fatal("after sealing the object is still in the clear in the bucket")
	}
	if read := readAll(t, s, ctx, key); !bytes.Equal(read, plain) {
		t.Fatal("the content changed with sealing")
	}

	// Selar outra vez não pode cifrar o cifrado: a passagem em fundo repete-se de dez em dez
	// minutos e passa por todos os objetos, incluindo os que já selou.
	if err := s.Seal(ctx, key); err != nil {
		t.Fatalf("segunda selagem: %v", err)
	}
	if read := readAll(t, s, ctx, key); !bytes.Equal(read, plain) {
		t.Fatal("sealing twice broke the object")
	}
}

// TestIntegrationObjetoVazio cobre o caso que apareceu no primeiro dia em produção: os marcadores
// de pasta do Drive têm zero bytes, o pedido do cabeçalho responde 416, e a passagem de selagem
// enchia o registo de erros que não eram erros.
func TestIntegrationEmptyObject(t *testing.T) {
	s, _, tenant := testService(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	key := fmt.Sprintf("%s/pasta-%s/", tenant, uuid.NewString())
	t.Cleanup(func() { _ = s.Remove(context.Background(), key) })

	unencrypted := &Service{client: s.client, bucket: s.bucket}
	if err := unencrypted.Put(ctx, key, bytes.NewReader(nil), 0, "application/x-directory"); err != nil {
		t.Fatalf("create marker: %v", err)
	}

	cifrado, err := s.IsEncrypted(ctx, key)
	if err != nil {
		t.Fatalf("IsEncrypted on an empty object should be silent, gave: %v", err)
	}
	if cifrado {
		t.Fatal("a zero-byte object cannot be encrypted")
	}
	if (Object{Key: key, Size: 0}).Sealable() {
		t.Fatal("a folder marker should not be sealed")
	}
}

// TestIntegrationSelagemNaoRevelaTipo: depois de selar, o fornecedor deixa de saber que aquilo é um
// vídeo. O tipo é metadado sobre o conteúdo, e o conteúdo passou a ser opaco — dizer-lhe que é um
// `video/mp4` devolvia-lhe parte do que a cifra escondeu. Quem serve o ficheiro tira o tipo do nome
// na nossa base de dados (`mime.TypeByExtension`), nunca do armazenamento.
func TestIntegrationSealingHidesContentType(t *testing.T) {
	s, _, tenant := testService(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	key := fmt.Sprintf("%s/recordings/tipo-%s.mp4", tenant, uuid.NewString())
	t.Cleanup(func() { _ = s.Remove(context.Background(), key) })

	unencrypted := &Service{client: s.client, bucket: s.bucket}
	if err := unencrypted.Put(ctx, key, bytes.NewReader([]byte("filme")), 5, "video/mp4"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := s.Seal(ctx, key); err != nil {
		t.Fatalf("Seal: %v", err)
	}

	info, err := s.client.StatObject(ctx, s.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if info.ContentType == "video/mp4" {
		t.Fatal("storage still knows the encrypted object is a video")
	}
}

// lerCru busca os bytes diretamente do balde, sem passar pelo Service — é literalmente o que o
// fornecedor de armazenamento tem. Não dá para reutilizar um Service sem chaves: esse recusa-se a
// servir um objeto cifrado, e ainda bem (devolver bytes cifrados como se fossem o ficheiro seria
// pior do que falhar).
func readRaw(t *testing.T, s *Service, key string) []byte {
	t.Helper()
	r, err := s.client.GetObject(context.Background(), s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		t.Fatalf("raw read: %v", err)
	}
	defer r.Close()
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("raw read: %v", err)
	}
	return b
}

func readAll(t *testing.T, s *Service, ctx context.Context, key string) []byte {
	t.Helper()
	r, err := s.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer r.Close()
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return b
}
