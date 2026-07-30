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

	"mailin/backend/internal/db"
	"mailin/backend/internal/filecrypt"
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
		t.Skip("STORAGE_TEST_ENDPOINT não definido; a saltar o teste de integração")
	}

	s, err := New(endpoint, os.Getenv("STORAGE_TEST_KEY"), os.Getenv("STORAGE_TEST_SECRET"),
		os.Getenv("STORAGE_TEST_BUCKET"), "", false)
	if err != nil {
		t.Fatalf("ligar ao armazenamento: %v", err)
	}

	pool, err := pgxpool.New(context.Background(), os.Getenv("STORAGE_TEST_DB"))
	if err != nil {
		t.Fatalf("ligar à base de dados: %v", err)
	}
	t.Cleanup(pool.Close)
	q := db.New(pool)

	// Um tenant que existe mesmo: a chave do tenant tem uma referência para `tenants`.
	tenant := tenantDeTeste(t, pool)

	kek := make([]byte, filecrypt.KeySize)
	if _, err := rand.Read(kek); err != nil {
		t.Fatal(err)
	}
	if err := s.EnableEncryption(q, "1:"+hex.EncodeToString(kek)); err != nil {
		t.Fatalf("ligar a cifra: %v", err)
	}
	if !s.Encrypting() {
		t.Fatal("a cifra devia estar ligada")
	}
	return s, q, tenant
}

func tenantDeTeste(t *testing.T, pool *pgxpool.Pool) uuid.UUID {
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

func TestIntegrationCifraNoBalde(t *testing.T) {
	s, _, tenant := testService(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	// Maior do que um bloco, para exercitar mais de um bloco de verdade.
	claro := bytes.Repeat([]byte("conteudo confidencial do cliente. "), 40_000)
	key := fmt.Sprintf("%s/teste-%s", tenant, uuid.NewString())
	t.Cleanup(func() { _ = s.Remove(context.Background(), key) })

	if err := s.Put(ctx, key, bytes.NewReader(claro), int64(len(claro)), "application/octet-stream"); err != nil {
		t.Fatalf("Put: %v", err)
	}

	// 1. O que está NO BALDE não é o que foi enviado.
	bruto := lerCru(t, s, key)
	if bytes.Contains(bruto, []byte("conteudo confidencial")) {
		t.Fatal("o objeto no balde contém o texto em claro")
	}
	if string(bruto[:4]) != filecrypt.Magic {
		t.Fatalf("o objeto no balde não tem a magia %q, começa por %q", filecrypt.Magic, bruto[:4])
	}

	// 2. A chave do objeto viaja lá dentro — é o que torna a escrita uma operação só.
	h, err := filecrypt.ParseHeader(bruto[:filecrypt.FixedHeaderSize])
	if err != nil {
		t.Fatalf("cabeçalho: %v", err)
	}
	if !h.TenantWrapped {
		t.Fatal("a chave devia estar embrulhada pela chave do tenant")
	}
	if h.WrappedKeyLen() == 0 {
		t.Fatal("o cabeçalho não traz chave embrulhada")
	}
	if h.PlainSize != int64(len(claro)) {
		t.Fatalf("tamanho em claro %d, esperava %d", h.PlainSize, len(claro))
	}

	// 3. E volta inteiro pelo Get.
	r, err := s.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer r.Close()
	volta, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("ler: %v", err)
	}
	if !bytes.Equal(volta, claro) {
		t.Fatalf("voltou diferente: %d bytes vs %d", len(volta), len(claro))
	}
}

func TestIntegrationSeekNoMeio(t *testing.T) {
	s, _, tenant := testService(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	// 5 MiB: o salto cai no meio de um bloco que não é o primeiro nem o último, que é o caso em que
	// um erro de aritmética de blocos aparece.
	claro := make([]byte, 5<<20)
	if _, err := rand.Read(claro); err != nil {
		t.Fatal(err)
	}
	key := fmt.Sprintf("%s/seek-%s", tenant, uuid.NewString())
	t.Cleanup(func() { _ = s.Remove(context.Background(), key) })

	if err := s.Put(ctx, key, bytes.NewReader(claro), int64(len(claro)), "video/mp4"); err != nil {
		t.Fatalf("Put: %v", err)
	}

	sr, err := s.GetSeek(ctx, key)
	if err != nil {
		t.Fatalf("GetSeek: %v", err)
	}
	defer sr.Close()

	const salto = 3<<20 + 12345 // no meio do quarto bloco
	if _, err := sr.Seek(salto, io.SeekStart); err != nil {
		t.Fatalf("Seek: %v", err)
	}
	pedaco := make([]byte, 4096)
	if _, err := io.ReadFull(sr, pedaco); err != nil {
		t.Fatalf("ler depois do salto: %v", err)
	}
	if !bytes.Equal(pedaco, claro[salto:salto+4096]) {
		t.Fatal("o conteúdo depois do salto não é o do ficheiro original")
	}
}

// TestIntegracaoSelagem prova o caminho por onde entram as gravações compostas e o conteúdo
// anterior à cifra: um objeto que chegou ao balde em claro é selado sem mudar de sítio nem de nome.
func TestIntegrationSelagem(t *testing.T) {
	s, _, tenant := testService(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	claro := []byte("gravação escrita pelo egress, direto no balde, sem passar por nós")
	key := fmt.Sprintf("%s/recordings/selar-%s.mp4", tenant, uuid.NewString())
	t.Cleanup(func() { _ = s.Remove(context.Background(), key) })

	// Escreve à socapa da cifra, como o LiveKit faz com as credenciais S3.
	semCifra := &Service{client: s.client, bucket: s.bucket}
	if err := semCifra.Put(ctx, key, bytes.NewReader(claro), int64(len(claro)), "video/mp4"); err != nil {
		t.Fatalf("Put em claro: %v", err)
	}

	cifrado, err := s.IsEncrypted(ctx, key)
	if err != nil {
		t.Fatalf("IsEncrypted: %v", err)
	}
	if cifrado {
		t.Fatal("acabou de ser escrito em claro e diz estar cifrado")
	}

	// Antes de selar, o objeto em claro TEM de continuar a ler-se — é o que permite migrar sem
	// paragem.
	if lido := lerTudo(t, s, ctx, key); !bytes.Equal(lido, claro) {
		t.Fatal("um objeto anterior à cifra deixou de se ler")
	}

	if err := s.Seal(ctx, key); err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if bruto := lerCru(t, s, key); string(bruto[:4]) != filecrypt.Magic {
		t.Fatal("depois da selagem o objeto continua em claro no balde")
	}
	if lido := lerTudo(t, s, ctx, key); !bytes.Equal(lido, claro) {
		t.Fatal("o conteúdo mudou com a selagem")
	}

	// Selar outra vez não pode cifrar o cifrado: a passagem em fundo repete-se de dez em dez
	// minutos e passa por todos os objetos, incluindo os que já selou.
	if err := s.Seal(ctx, key); err != nil {
		t.Fatalf("segunda selagem: %v", err)
	}
	if lido := lerTudo(t, s, ctx, key); !bytes.Equal(lido, claro) {
		t.Fatal("selar duas vezes estragou o objeto")
	}
}

// lerCru busca os bytes diretamente do balde, sem passar pelo Service — é literalmente o que o
// fornecedor de armazenamento tem. Não dá para reutilizar um Service sem chaves: esse recusa-se a
// servir um objeto cifrado, e ainda bem (devolver bytes cifrados como se fossem o ficheiro seria
// pior do que falhar).
func lerCru(t *testing.T, s *Service, key string) []byte {
	t.Helper()
	r, err := s.client.GetObject(context.Background(), s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		t.Fatalf("ler cru: %v", err)
	}
	defer r.Close()
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("ler cru: %v", err)
	}
	return b
}

func lerTudo(t *testing.T, s *Service, ctx context.Context, key string) []byte {
	t.Helper()
	r, err := s.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer r.Close()
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("ler: %v", err)
	}
	return b
}
