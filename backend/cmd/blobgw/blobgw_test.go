package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/minio/minio-go/v7/pkg/signer"

	"github.com/beskosorg/beskos/backend/internal/filecrypt"
	"github.com/beskosorg/beskos/backend/internal/storage"
)

// Os testes falam com o gateway usando um cliente S3 DE VERDADE (minio-go), que assina em SigV4
// como o Stalwart assina. Escrever um cliente de teste que assinasse "à nossa maneira" provaria
// apenas que o nosso verificador concorda consigo próprio.
//
// Precisam do MinIO local, como os do internal/storage:
//
//	STORAGE_TEST_ENDPOINT=minio:9000 STORAGE_TEST_KEY=… STORAGE_TEST_SECRET=… \
//	STORAGE_TEST_BUCKET=mailin go test ./cmd/blobgw -v

const (
	testAccess = "stalwart-teste"
	testSecret = "segredo-do-stalwart-para-o-teste"
)

func build(t *testing.T) (*gateway, *httptest.Server, *minio.Client) {
	t.Helper()
	endpoint := os.Getenv("STORAGE_TEST_ENDPOINT")
	if endpoint == "" {
		t.Skip("STORAGE_TEST_ENDPOINT not set; skipping")
	}
	bucket := os.Getenv("STORAGE_TEST_BUCKET")

	st, err := storage.New(endpoint, os.Getenv("STORAGE_TEST_KEY"), os.Getenv("STORAGE_TEST_SECRET"),
		bucket, "", false)
	if err != nil {
		t.Fatalf("armazenamento: %v", err)
	}
	kek := make([]byte, filecrypt.KeySize)
	if _, err := rand.Read(kek); err != nil {
		t.Fatal(err)
	}
	if err := st.EnableEncryption(nil, "1:"+hex.EncodeToString(kek)); err != nil {
		t.Fatal(err)
	}

	g := &gateway{
		st: st, bucket: bucket, prefixes: []string{"mail/"},
		cred: credential{access: testAccess, secret: testSecret, region: "auto"},
	}
	// Servidor TLS de propósito: sobre HTTP simples o minio-go assina o corpo EM FLUXO
	// (`aws-chunked`), que o Stalwart não usa e este gateway recusa. Sobre TLS ele assina como o
	// rust-s3 — que é o caminho que interessa exercitar.
	srv := httptest.NewTLSServer(g)
	t.Cleanup(srv.Close)

	u, _ := url.Parse(srv.URL)
	cli, err := minio.New(u.Host, &minio.Options{
		Creds:        credentials.NewStaticV4(testAccess, testSecret, ""),
		Secure:       true,
		Transport:    srv.Client().Transport,
		Region:       "auto",
		BucketLookup: minio.BucketLookupPath, // como o Stalwart: with_path_style()
	})
	if err != nil {
		t.Fatal(err)
	}
	return g, srv, cli
}

// TestIntegrationGatewayComoORustS3 exercita exatamente o que o Stalwart envia: o resumo REAL do
// corpo no X-Amz-Content-Sha256, em vez do UNSIGNED-PAYLOAD que o minio-go usa. A assinatura é
// feita pelo assinador do minio — independente do nosso verificador, senão o teste provaria apenas
// que o nosso código concorda consigo próprio.
func TestIntegrationGatewayLikeRustS3(t *testing.T) {
	g, srv, _ := build(t)
	plain := []byte("mensagem crua, assinada com o resumo do corpo")
	key := "mail/" + randomHex(t)
	t.Cleanup(func() { _ = g.st.Remove(context.Background(), key) })

	resp := byRustS3(t, srv, key, plain, hexSHA256(plain))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("PUT signed the way Stalwart does returned %d: %s", resp.StatusCode, body)
	}
	if raw := readRaw(t, g, key); string(raw[:4]) != filecrypt.Magic {
		t.Fatal("the object was not encrypted")
	}
}

// TestIntegrationGatewayCorpoTrocado: a assinatura cobre o resumo DECLARADO, não o corpo. Se os
// dois não baterem certo, o objeto não pode ficar no armazenamento.
func TestIntegrationGatewaySwappedBody(t *testing.T) {
	g, srv, _ := build(t)
	key := "mail/" + randomHex(t)
	t.Cleanup(func() { _ = g.st.Remove(context.Background(), key) })

	// Assina o resumo de um conteúdo e envia outro.
	resp := byRustS3(t, srv, key, []byte("o corpo que vai"), hexSHA256([]byte("o corpo assinado")))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("a body different from the signed one should give 400, gave %d", resp.StatusCode)
	}
	if _, err := g.st.Get(context.Background(), key); err == nil {
		t.Fatal("the rejected object stayed in storage")
	}
}

// porRustS3 monta e assina um PUT como o cliente do Stalwart o faz.
func byRustS3(t *testing.T, srv *httptest.Server, key string, body []byte, summary string) *http.Response {
	t.Helper()
	u, _ := url.Parse(srv.URL)
	req, err := http.NewRequest(http.MethodPut,
		fmt.Sprintf("%s/%s/%s", srv.URL, os.Getenv("STORAGE_TEST_BUCKET"), key), bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Host = u.Host
	req.ContentLength = int64(len(body))
	req.Header.Set("X-Amz-Content-Sha256", summary)
	signed := signer.SignV4(*req, testAccess, testSecret, "", "auto")
	signed.Body = io.NopCloser(bytes.NewReader(body))

	resp, err := srv.Client().Do(signed)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestIntegrationGatewayRoundTrip(t *testing.T) {
	g, _, cli := build(t)
	ctx := context.Background()

	// Maior do que um bloco: obriga a cifra a partir em pedaços de verdade.
	plain := bytes.Repeat([]byte("mensagem de correio com anexo. "), 50_000)
	key := "mail/" + randomHex(t)
	t.Cleanup(func() { _ = g.st.Remove(context.Background(), key) })

	_, err := cli.PutObject(ctx, g.bucket, key, bytes.NewReader(plain), int64(len(plain)),
		minio.PutObjectOptions{DisableMultipart: true})
	if err != nil {
		t.Fatalf("PutObject: %v", err)
	}

	// 1. O que ficou no balde não é o que foi enviado.
	raw := readRaw(t, g, key)
	if bytes.Contains(raw, []byte("mensagem de correio")) {
		t.Fatal("the object in the bucket holds the mail text in the clear")
	}
	if string(raw[:4]) != filecrypt.Magic {
		t.Fatalf("the object in the bucket is not encrypted, it starts with %q", raw[:4])
	}

	// 2. E o cliente recebe-o de volta inteiro, sem saber de nada disto.
	obj, err := cli.GetObject(ctx, g.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer obj.Close()
	back, err := io.ReadAll(obj)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(back, plain) {
		t.Fatalf("came back different: %d bytes vs %d", len(back), len(plain))
	}
}

// TestIntegrationGatewayRange é a razão de o formato ser em blocos: o Stalwart pede pedaços de uma
// mensagem para servir uma parte MIME, e não pode ter de decifrar o anexo todo para isso.
func TestIntegrationGatewayRange(t *testing.T) {
	g, _, cli := build(t)
	ctx := context.Background()

	plain := make([]byte, 3<<20)
	if _, err := rand.Read(plain); err != nil {
		t.Fatal(err)
	}
	key := "mail/" + randomHex(t)
	t.Cleanup(func() { _ = g.st.Remove(context.Background(), key) })

	if _, err := cli.PutObject(ctx, g.bucket, key, bytes.NewReader(plain), int64(len(plain)),
		minio.PutObjectOptions{DisableMultipart: true}); err != nil {
		t.Fatal(err)
	}

	for _, caso := range []struct{ from, until int64 }{
		{0, 99},                     // início
		{1<<20 + 500, 1<<20 + 1499}, // a cavalo entre dois blocos
		{int64(len(plain)) - 10, int64(len(plain)) - 1}, // o fim
	} {
		opts := minio.GetObjectOptions{}
		if err := opts.SetRange(caso.from, caso.until); err != nil {
			t.Fatal(err)
		}
		obj, err := cli.GetObject(ctx, g.bucket, key, opts)
		if err != nil {
			t.Fatal(err)
		}
		read, err := io.ReadAll(obj)
		obj.Close()
		if err != nil {
			t.Fatalf("[%d-%d] read: %v", caso.from, caso.until, err)
		}
		if !bytes.Equal(read, plain[caso.from:caso.until+1]) {
			t.Fatalf("[%d-%d] returned %d wrong bytes", caso.from, caso.until, len(read))
		}
	}
}

// TestIntegrationGatewayHeadDepoisDoPut cobre o verify_after_write do Stalwart: ele faz HEAD logo a
// seguir a cada escrita para se defender de armazenamentos que confirmam antes de tornar visível.
func TestIntegrationGatewayHeadAfterPut(t *testing.T) {
	g, _, cli := build(t)
	ctx := context.Background()

	plain := []byte("uma mensagem curta")
	key := "mail/" + randomHex(t)
	t.Cleanup(func() { _ = g.st.Remove(context.Background(), key) })

	if _, err := cli.PutObject(ctx, g.bucket, key, bytes.NewReader(plain), int64(len(plain)),
		minio.PutObjectOptions{DisableMultipart: true}); err != nil {
		t.Fatal(err)
	}

	info, err := cli.StatObject(ctx, g.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		t.Fatalf("StatObject: %v", err)
	}
	// O tamanho tem de ser o do texto em claro. Devolver o do cifrado faria o cliente pedir bytes
	// que não existem.
	if info.Size != int64(len(plain)) {
		t.Fatalf("HEAD diz %d bytes, o claro tem %d", info.Size, len(plain))
	}

	if err := cli.RemoveObject(ctx, g.bucket, key, minio.RemoveObjectOptions{}); err != nil {
		t.Fatalf("RemoveObject: %v", err)
	}
	if _, err := cli.StatObject(ctx, g.bucket, key, minio.StatObjectOptions{}); err == nil {
		t.Fatal("the object is still there after DELETE")
	}
}

// TestIntegrationGatewayNaoExiste: o 404 tem de passar como 404. O Stalwart distingue "não existe"
// (resposta legítima, devolve None) de "avariou" (que ele repete com recuo) — trocar os dois faria
// uma caixa vazia parecer uma avaria, ou pior.
func TestIntegrationGatewayNotFound(t *testing.T) {
	g, _, cli := build(t)
	_, err := cli.GetObject(context.Background(), g.bucket, "mail/"+randomHex(t), minio.GetObjectOptions{})
	if err == nil {
		// o minio-go só falha na primeira leitura
		obj, _ := cli.GetObject(context.Background(), g.bucket, "mail/nao-existe", minio.GetObjectOptions{})
		if _, err = io.ReadAll(obj); err == nil {
			t.Fatal("reading an object that does not exist should fail")
		}
	}
	if resp := minio.ToErrorResponse(err); resp.StatusCode != http.StatusNotFound && resp.Code != "NoSuchKey" {
		t.Fatalf("want 404/NoSuchKey, got %+v", resp)
	}
}

// TestGatewayRecusaSemAssinatura e os seguintes não precisam de armazenamento: param antes.
func TestGatewayRejectsUnsignedRequest(t *testing.T) {
	g := &gateway{bucket: "b", prefixes: []string{"mail/"},
		cred: credential{access: testAccess, secret: testSecret, region: "auto"}}
	w := httptest.NewRecorder()
	g.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/b/mail/x", nil))
	if w.Code != http.StatusForbidden {
		t.Fatalf("without a signature it should give 403, gave %d", w.Code)
	}
}

func TestGatewayRejectsOutsidePrefix(t *testing.T) {
	g := &gateway{bucket: "b", prefixes: []string{"mail/"},
		cred: credential{access: testAccess, secret: testSecret, region: "auto"}}
	for _, path := range []string{"/b/recordings/x.mp4", "/b/outro/x", "/outro-balde/mail/x"} {
		w := httptest.NewRecorder()
		g.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusForbidden {
			t.Fatalf("%s should give 403, gave %d", path, w.Code)
		}
	}
}

func TestGatewayRejectsUnsupportedOperation(t *testing.T) {
	g, _, cli := build(t)
	// ListObjects é das operações que este gateway NÃO implementa, de propósito.
	for obj := range cli.ListObjects(context.Background(), g.bucket, minio.ListObjectsOptions{Prefix: "mail/"}) {
		if obj.Err == nil {
			t.Fatal("listing should be rejected")
		}
		return
	}
}

func TestReadRange(t *testing.T) {
	cases := []struct {
		header      string
		total       int64
		from, until int64
		failure     bool
	}{
		{"bytes=0-99", 1000, 0, 99, false},
		{"bytes=100-", 1000, 100, 999, false},
		{"bytes=-100", 1000, 900, 999, false},
		{"bytes=-5000", 1000, 0, 999, false},   // sufixo maior do que o objeto
		{"bytes=0-5000", 1000, 0, 999, false},  // fim além do objeto: corta-se
		{"bytes=1000-", 1000, 0, 0, true},      // início além do fim
		{"bytes=50-10", 1000, 0, 0, true},      // invertido
		{"bytes=0-10,20-30", 1000, 0, 0, true}, // múltiplos
		{"itens=0-10", 1000, 0, 0, true},
	}
	for _, c := range cases {
		from, until, err := readRange(c.header, c.total)
		if c.failure {
			if err == nil {
				t.Errorf("%q should fail", c.header)
			}
			continue
		}
		if err != nil {
			t.Errorf("%q: %v", c.header, err)
			continue
		}
		if from != c.from || until != c.until {
			t.Errorf("%q gave [%d-%d], want [%d-%d]", c.header, from, until, c.from, c.until)
		}
	}
}

func TestCheckerCatchesSwappedBody(t *testing.T) {
	c := newChecker(strings.NewReader("o corpo verdadeiro"), hexSHA256([]byte("outro corpo")))
	if _, err := io.ReadAll(c); err != nil {
		t.Fatal(err)
	}
	if err := c.verify(); err == nil {
		t.Fatal("a body different from the signed one had to be rejected")
	}

	ok := newChecker(strings.NewReader("o corpo verdadeiro"), hexSHA256([]byte("o corpo verdadeiro")))
	if _, err := io.ReadAll(ok); err != nil {
		t.Fatal(err)
	}
	if err := ok.verify(); err != nil {
		t.Fatalf("the correct body was rejected: %v", err)
	}
}

// lerCru vai buscar os bytes DIRETAMENTE ao armazenamento, sem passar pelo gateway — é o que a
// Cloudflare tem. Não serve um Service sem cifra: esse recusa-se a servir um objeto cifrado, e ainda
// bem.
func readRaw(t *testing.T, g *gateway, key string) []byte {
	t.Helper()
	cli, err := minio.New(os.Getenv("STORAGE_TEST_ENDPOINT"), &minio.Options{
		Creds:  credentials.NewStaticV4(os.Getenv("STORAGE_TEST_KEY"), os.Getenv("STORAGE_TEST_SECRET"), ""),
		Secure: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	r, err := cli.GetObject(context.Background(), g.bucket, key, minio.GetObjectOptions{})
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

func randomHex(t *testing.T) string {
	t.Helper()
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("teste-%s", hex.EncodeToString(b))
}
