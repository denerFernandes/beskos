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

	"mailin/backend/internal/filecrypt"
	"mailin/backend/internal/storage"
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
	acessoTeste  = "stalwart-teste"
	segredoTeste = "segredo-do-stalwart-para-o-teste"
)

func montar(t *testing.T) (*gateway, *httptest.Server, *minio.Client) {
	t.Helper()
	endpoint := os.Getenv("STORAGE_TEST_ENDPOINT")
	if endpoint == "" {
		t.Skip("STORAGE_TEST_ENDPOINT não definido; a saltar")
	}
	balde := os.Getenv("STORAGE_TEST_BUCKET")

	st, err := storage.New(endpoint, os.Getenv("STORAGE_TEST_KEY"), os.Getenv("STORAGE_TEST_SECRET"),
		balde, "", false)
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
		st: st, bucket: balde, prefixos: []string{"mail/"},
		cred: credencial{acesso: acessoTeste, segredo: segredoTeste, regiao: "auto"},
	}
	// Servidor TLS de propósito: sobre HTTP simples o minio-go assina o corpo EM FLUXO
	// (`aws-chunked`), que o Stalwart não usa e este gateway recusa. Sobre TLS ele assina como o
	// rust-s3 — que é o caminho que interessa exercitar.
	srv := httptest.NewTLSServer(g)
	t.Cleanup(srv.Close)

	u, _ := url.Parse(srv.URL)
	cli, err := minio.New(u.Host, &minio.Options{
		Creds:        credentials.NewStaticV4(acessoTeste, segredoTeste, ""),
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
func TestIntegrationGatewayComoORustS3(t *testing.T) {
	g, srv, _ := montar(t)
	claro := []byte("mensagem crua, assinada com o resumo do corpo")
	key := "mail/" + hexAleatorio(t)
	t.Cleanup(func() { _ = g.st.Remove(context.Background(), key) })

	resp := porRustS3(t, srv, key, claro, hexSHA256(claro))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		corpo, _ := io.ReadAll(resp.Body)
		t.Fatalf("PUT assinado como o Stalwart deu %d: %s", resp.StatusCode, corpo)
	}
	if bruto := lerCru(t, g, key); string(bruto[:4]) != filecrypt.Magic {
		t.Fatal("o objeto não foi cifrado")
	}
}

// TestIntegrationGatewayCorpoTrocado: a assinatura cobre o resumo DECLARADO, não o corpo. Se os
// dois não baterem certo, o objeto não pode ficar no armazenamento.
func TestIntegrationGatewayCorpoTrocado(t *testing.T) {
	g, srv, _ := montar(t)
	key := "mail/" + hexAleatorio(t)
	t.Cleanup(func() { _ = g.st.Remove(context.Background(), key) })

	// Assina o resumo de um conteúdo e envia outro.
	resp := porRustS3(t, srv, key, []byte("o corpo que vai"), hexSHA256([]byte("o corpo assinado")))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("um corpo diferente do assinado devia dar 400, deu %d", resp.StatusCode)
	}
	if _, err := g.st.Get(context.Background(), key); err == nil {
		t.Fatal("o objeto recusado ficou no armazenamento")
	}
}

// porRustS3 monta e assina um PUT como o cliente do Stalwart o faz.
func porRustS3(t *testing.T, srv *httptest.Server, key string, corpo []byte, resumo string) *http.Response {
	t.Helper()
	u, _ := url.Parse(srv.URL)
	req, err := http.NewRequest(http.MethodPut,
		fmt.Sprintf("%s/%s/%s", srv.URL, os.Getenv("STORAGE_TEST_BUCKET"), key), bytes.NewReader(corpo))
	if err != nil {
		t.Fatal(err)
	}
	req.Host = u.Host
	req.ContentLength = int64(len(corpo))
	req.Header.Set("X-Amz-Content-Sha256", resumo)
	assinado := signer.SignV4(*req, acessoTeste, segredoTeste, "", "auto")
	assinado.Body = io.NopCloser(bytes.NewReader(corpo))

	resp, err := srv.Client().Do(assinado)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestIntegrationGatewayIdaEVolta(t *testing.T) {
	g, _, cli := montar(t)
	ctx := context.Background()

	// Maior do que um bloco: obriga a cifra a partir em pedaços de verdade.
	claro := bytes.Repeat([]byte("mensagem de correio com anexo. "), 50_000)
	key := "mail/" + hexAleatorio(t)
	t.Cleanup(func() { _ = g.st.Remove(context.Background(), key) })

	_, err := cli.PutObject(ctx, g.bucket, key, bytes.NewReader(claro), int64(len(claro)),
		minio.PutObjectOptions{DisableMultipart: true})
	if err != nil {
		t.Fatalf("PutObject: %v", err)
	}

	// 1. O que ficou no balde não é o que foi enviado.
	bruto := lerCru(t, g, key)
	if bytes.Contains(bruto, []byte("mensagem de correio")) {
		t.Fatal("o objeto no balde tem o texto do correio em claro")
	}
	if string(bruto[:4]) != filecrypt.Magic {
		t.Fatalf("o objeto no balde não está cifrado, começa por %q", bruto[:4])
	}

	// 2. E o cliente recebe-o de volta inteiro, sem saber de nada disto.
	obj, err := cli.GetObject(ctx, g.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer obj.Close()
	volta, err := io.ReadAll(obj)
	if err != nil {
		t.Fatalf("ler: %v", err)
	}
	if !bytes.Equal(volta, claro) {
		t.Fatalf("voltou diferente: %d bytes vs %d", len(volta), len(claro))
	}
}

// TestIntegrationGatewayRange é a razão de o formato ser em blocos: o Stalwart pede pedaços de uma
// mensagem para servir uma parte MIME, e não pode ter de decifrar o anexo todo para isso.
func TestIntegrationGatewayRange(t *testing.T) {
	g, _, cli := montar(t)
	ctx := context.Background()

	claro := make([]byte, 3<<20)
	if _, err := rand.Read(claro); err != nil {
		t.Fatal(err)
	}
	key := "mail/" + hexAleatorio(t)
	t.Cleanup(func() { _ = g.st.Remove(context.Background(), key) })

	if _, err := cli.PutObject(ctx, g.bucket, key, bytes.NewReader(claro), int64(len(claro)),
		minio.PutObjectOptions{DisableMultipart: true}); err != nil {
		t.Fatal(err)
	}

	for _, caso := range []struct{ de, ate int64 }{
		{0, 99},                     // início
		{1<<20 + 500, 1<<20 + 1499}, // a cavalo entre dois blocos
		{int64(len(claro)) - 10, int64(len(claro)) - 1}, // o fim
	} {
		opts := minio.GetObjectOptions{}
		if err := opts.SetRange(caso.de, caso.ate); err != nil {
			t.Fatal(err)
		}
		obj, err := cli.GetObject(ctx, g.bucket, key, opts)
		if err != nil {
			t.Fatal(err)
		}
		lido, err := io.ReadAll(obj)
		obj.Close()
		if err != nil {
			t.Fatalf("[%d-%d] ler: %v", caso.de, caso.ate, err)
		}
		if !bytes.Equal(lido, claro[caso.de:caso.ate+1]) {
			t.Fatalf("[%d-%d] devolveu %d bytes errados", caso.de, caso.ate, len(lido))
		}
	}
}

// TestIntegrationGatewayHeadDepoisDoPut cobre o verify_after_write do Stalwart: ele faz HEAD logo a
// seguir a cada escrita para se defender de armazenamentos que confirmam antes de tornar visível.
func TestIntegrationGatewayHeadDepoisDoPut(t *testing.T) {
	g, _, cli := montar(t)
	ctx := context.Background()

	claro := []byte("uma mensagem curta")
	key := "mail/" + hexAleatorio(t)
	t.Cleanup(func() { _ = g.st.Remove(context.Background(), key) })

	if _, err := cli.PutObject(ctx, g.bucket, key, bytes.NewReader(claro), int64(len(claro)),
		minio.PutObjectOptions{DisableMultipart: true}); err != nil {
		t.Fatal(err)
	}

	info, err := cli.StatObject(ctx, g.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		t.Fatalf("StatObject: %v", err)
	}
	// O tamanho tem de ser o do texto em claro. Devolver o do cifrado faria o cliente pedir bytes
	// que não existem.
	if info.Size != int64(len(claro)) {
		t.Fatalf("HEAD diz %d bytes, o claro tem %d", info.Size, len(claro))
	}

	if err := cli.RemoveObject(ctx, g.bucket, key, minio.RemoveObjectOptions{}); err != nil {
		t.Fatalf("RemoveObject: %v", err)
	}
	if _, err := cli.StatObject(ctx, g.bucket, key, minio.StatObjectOptions{}); err == nil {
		t.Fatal("o objeto continua lá depois do DELETE")
	}
}

// TestIntegrationGatewayNaoExiste: o 404 tem de passar como 404. O Stalwart distingue "não existe"
// (resposta legítima, devolve None) de "avariou" (que ele repete com recuo) — trocar os dois faria
// uma caixa vazia parecer uma avaria, ou pior.
func TestIntegrationGatewayNaoExiste(t *testing.T) {
	g, _, cli := montar(t)
	_, err := cli.GetObject(context.Background(), g.bucket, "mail/"+hexAleatorio(t), minio.GetObjectOptions{})
	if err == nil {
		// o minio-go só falha na primeira leitura
		obj, _ := cli.GetObject(context.Background(), g.bucket, "mail/nao-existe", minio.GetObjectOptions{})
		if _, err = io.ReadAll(obj); err == nil {
			t.Fatal("ler um objeto que não existe devia falhar")
		}
	}
	if resp := minio.ToErrorResponse(err); resp.StatusCode != http.StatusNotFound && resp.Code != "NoSuchKey" {
		t.Fatalf("esperava 404/NoSuchKey, veio %+v", resp)
	}
}

// TestGatewayRecusaSemAssinatura e os seguintes não precisam de armazenamento: param antes.
func TestGatewayRecusaSemAssinatura(t *testing.T) {
	g := &gateway{bucket: "b", prefixos: []string{"mail/"},
		cred: credencial{acesso: acessoTeste, segredo: segredoTeste, regiao: "auto"}}
	w := httptest.NewRecorder()
	g.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/b/mail/x", nil))
	if w.Code != http.StatusForbidden {
		t.Fatalf("sem assinatura devia dar 403, deu %d", w.Code)
	}
}

func TestGatewayRecusaForaDoPrefixo(t *testing.T) {
	g := &gateway{bucket: "b", prefixos: []string{"mail/"},
		cred: credencial{acesso: acessoTeste, segredo: segredoTeste, regiao: "auto"}}
	for _, caminho := range []string{"/b/recordings/x.mp4", "/b/outro/x", "/outro-balde/mail/x"} {
		w := httptest.NewRecorder()
		g.ServeHTTP(w, httptest.NewRequest(http.MethodGet, caminho, nil))
		if w.Code != http.StatusForbidden {
			t.Fatalf("%s devia dar 403, deu %d", caminho, w.Code)
		}
	}
}

func TestGatewayRecusaOperacaoNaoSuportada(t *testing.T) {
	g, _, cli := montar(t)
	// ListObjects é das operações que este gateway NÃO implementa, de propósito.
	for obj := range cli.ListObjects(context.Background(), g.bucket, minio.ListObjectsOptions{Prefix: "mail/"}) {
		if obj.Err == nil {
			t.Fatal("a listagem devia ser recusada")
		}
		return
	}
}

func TestLerRange(t *testing.T) {
	casos := []struct {
		cabecalho string
		total     int64
		de, ate   int64
		erro      bool
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
	for _, c := range casos {
		de, ate, err := lerRange(c.cabecalho, c.total)
		if c.erro {
			if err == nil {
				t.Errorf("%q devia falhar", c.cabecalho)
			}
			continue
		}
		if err != nil {
			t.Errorf("%q: %v", c.cabecalho, err)
			continue
		}
		if de != c.de || ate != c.ate {
			t.Errorf("%q deu [%d-%d], esperava [%d-%d]", c.cabecalho, de, ate, c.de, c.ate)
		}
	}
}

func TestConferidorApanhaCorpoTrocado(t *testing.T) {
	c := conferente(strings.NewReader("o corpo verdadeiro"), hexSHA256([]byte("outro corpo")))
	if _, err := io.ReadAll(c); err != nil {
		t.Fatal(err)
	}
	if err := c.confere(); err == nil {
		t.Fatal("um corpo diferente do assinado tinha de ser recusado")
	}

	ok := conferente(strings.NewReader("o corpo verdadeiro"), hexSHA256([]byte("o corpo verdadeiro")))
	if _, err := io.ReadAll(ok); err != nil {
		t.Fatal(err)
	}
	if err := ok.confere(); err != nil {
		t.Fatalf("o corpo certo foi recusado: %v", err)
	}
}

// lerCru vai buscar os bytes DIRETAMENTE ao armazenamento, sem passar pelo gateway — é o que a
// Cloudflare tem. Não serve um Service sem cifra: esse recusa-se a servir um objeto cifrado, e ainda
// bem.
func lerCru(t *testing.T, g *gateway, key string) []byte {
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
		t.Fatalf("ler cru: %v", err)
	}
	defer r.Close()
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("ler cru: %v", err)
	}
	return b
}

func hexAleatorio(t *testing.T) string {
	t.Helper()
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("teste-%s", hex.EncodeToString(b))
}
