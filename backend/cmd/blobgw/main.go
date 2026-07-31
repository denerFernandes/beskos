// blobgw é um servidor S3 que cifra o que lhe passa pelas mãos.
//
// PORQUÊ. O Stalwart escreve as mensagens e os anexos direto no armazenamento de objetos, com
// credenciais S3, sem passar pelo nosso código — e por isso ficavam legíveis na Cloudflare, a linha
// mais fraca do que dizemos publicamente. O armazenamento S3 do Stalwart não tem opção de cifra
// nenhuma (nem SSE-S3, nem SSE-C, nem chave do cliente: a lista de campos é bucket, region,
// accessKey, secretKey, securityToken, profile, timeout, maxRetries, verifyAfterWrite, keyPrefix e
// allowInvalidCerts). E não modificamos o Stalwart (CLAUDE.md §5.4).
//
// A saída é falar a língua dele. O Stalwart aponta o endpoint para aqui em vez de para a
// Cloudflare; nós ciframos com o formato que já existe (internal/filecrypt) e reencaminhamos. Ele
// não sabe que mudou nada.
//
// PORQUE É PEQUENO. Lido o cliente S3 do Stalwart (crates/store/src/backend/s3/mod.rs), ele usa
// exatamente quatro operações — GET, GET com Range, PUT de corpo inteiro, HEAD e DELETE — em estilo
// de caminho (`with_path_style`). NÃO usa multipart: o `put_object` recebe o corpo todo de uma vez.
// É isso que permite a este serviço não ter estado nenhum e, portanto, escalar em réplicas.
//
// O que NÃO está aqui, de propósito: multipart, URLs pré-assinados, listagens. O LiveKit precisa de
// tudo isso para as gravações e por isso continua a ser tratado pela selagem em fundo. Uma peça que
// serve dois clientes com necessidades diferentes acaba por servir mal os dois.
//
// LISTA BRANCA DE PREFIXOS. O gateway só aceita chaves dentro dos prefixos configurados. É a mesma
// lição que a selagem custou a aprender: quem partilha armazenamento diz explicitamente o que é
// seu, em vez de assumir que tudo o é.
package main

import (
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"mailin/backend/internal/storage"
)

type gateway struct {
	st *storage.Service
	// bucket é o nome que o cliente usa no caminho. Serve só para validar: o balde de destino real
	// vem da configuração do storage.
	bucket string
	// prefixos que este gateway aceita servir.
	prefixes []string
	cred     credential
}

func main() {
	log.SetFlags(log.LstdFlags | log.LUTC)

	st, err := storage.New(
		env("BLOBGW_S3_ENDPOINT"), env("BLOBGW_S3_ACCESS_KEY"), env("BLOBGW_S3_SECRET_KEY"),
		env("BLOBGW_S3_BUCKET"), envOr("BLOBGW_S3_REGION", "auto"),
		envOr("BLOBGW_S3_SSL", "true") == "true",
	)
	if err != nil {
		log.Fatalf("armazenamento: %v", err)
	}

	// Sem base de dados: as chaves servidas aqui não têm tenant. Uma mensagem é endereçada pelo
	// resumo do conteúdo e o Stalwart deduplica-a entre contas — pode pertencer a mais do que um
	// tenant ao mesmo tempo, e por isso não pode ser cifrada com a chave de um só. Fica embrulhada
	// pela KEK, como os outros objetos de sistema.
	if err := st.EnableEncryption(nil, env("FILE_ENC_KEYS")); err != nil {
		log.Fatalf("cifra: %v", err)
	}
	if !st.Encrypting() {
		log.Fatal("blobgw: FILE_ENC_KEYS missing — this service exists to encrypt; starting without a key would be worse than not starting")
	}

	g := &gateway{
		st:       st,
		bucket:   env("BLOBGW_S3_BUCKET"),
		prefixes: strings.Split(envOr("BLOBGW_PREFIXES", "mail/"), ","),
		cred: credential{
			access: env("BLOBGW_ACCESS_KEY"),
			secret: env("BLOBGW_SECRET_KEY"),
			region: envOr("BLOBGW_REGION", "auto"),
		},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	mux.Handle("/", g)

	port := envOr("PORT", "8090")
	srv := &http.Server{
		Addr:    ":" + port,
		Handler: mux,
		// Generosos: um anexo grande a subir por uma ligação lenta não pode ser cortado a meio.
		ReadTimeout:       10 * time.Minute,
		WriteTimeout:      10 * time.Minute,
		ReadHeaderTimeout: 30 * time.Second,
	}
	log.Printf("blobgw listening on :%s, bucket %s, prefixes %v", port, g.bucket, g.prefixes)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}

func env(name string) string {
	v := os.Getenv(name)
	if v == "" {
		log.Fatalf("blobgw: %s missing", name)
	}
	return v
}

func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

// ServeHTTP encaminha. Tudo o que não seja uma das quatro operações que o Stalwart usa responde 501
// — é mais honesto do que fingir que se implementou o S3 inteiro.
func (g *gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	key, err := g.key(r.URL.Path)
	if err != nil {
		// COM registo: um pedido recusado que não deixa rasto é indistinguível de um pedido que
		// nunca chegou. Custou uma diagnose inteira a perceber que o cliente mandava o nome de
		// balde antigo e nós respondíamos 403 em silêncio.
		log.Printf("%s %s -> 403 rejected: %v", r.Method, r.URL.Path, err)
		s3Err(w, http.StatusForbidden, "AccessDenied", err.Error())
		return
	}
	if err := g.cred.check(r); err != nil {
		log.Printf("blobgw: signature rejected on %s %s: %v", r.Method, key, err)
		s3Err(w, http.StatusForbidden, "SignatureDoesNotMatch", "assinatura inválida")
		return
	}

	ctx := r.Context()
	var status int
	switch r.Method {
	case http.MethodPut:
		status = g.put(ctx, w, r, key)
	case http.MethodGet:
		status = g.get(ctx, w, r, key)
	case http.MethodHead:
		status = g.head(ctx, w, key)
	case http.MethodDelete:
		status = g.delete(ctx, w, key)
	default:
		s3Err(w, http.StatusNotImplemented, "NotImplemented", "operação não suportada por este gateway")
		status = http.StatusNotImplemented
	}
	log.Printf("%s %s -> %d (%s)", r.Method, key, status, time.Since(start).Round(time.Millisecond))
}

// chave extrai a chave do objeto do caminho e confirma que está num prefixo nosso.
//
// O cliente usa estilo de caminho (`/<balde>/<chave>`), que é o que o Stalwart configura.
func (g *gateway) key(path string) (string, error) {
	p := strings.TrimPrefix(path, "/")
	bucket, key, ok := strings.Cut(p, "/")
	if !ok || bucket != g.bucket {
		return "", errf("balde %q desconhecido", bucket)
	}
	for _, pre := range g.prefixes {
		if pre != "" && strings.HasPrefix(key, pre) {
			return key, nil
		}
	}
	return "", errf("chave %q fora dos prefixos servidos", key)
}
