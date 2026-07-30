// blobmigrate copia os blocos do correio do balde antigo para o novo, cifrando-os à passagem.
//
// PORQUÊ. O Stalwart escrevia em `beskos`, um balde SEM restrição jurisdicional, e passou a escrever
// através do gateway em `beskos-data`, que tem jurisdição europeia. Os blocos antigos ficaram para
// trás: uma mensagem antiga aberta por um cliente IMAP procura o original no balde novo e não o
// encontra. Isto traz esses.
//
//	blobmigrate            # mostra o que faria
//	blobmigrate --fazer    # copia
//
// Cifra ao copiar, usando o mesmo storage.Service que o gateway usa — o resultado é byte a byte o
// mesmo que se a mensagem tivesse entrado pelo caminho novo. Não apaga a origem: o balde antigo
// fica como está até alguém decidir o contrário, e uma migração que apaga enquanto copia é uma
// migração que não se pode repetir.
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"strconv"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"mailin/backend/internal/storage"
)

func main() {
	log.SetFlags(0)
	fazer := flag.Bool("fazer", false, "copia mesmo; sem isto só mostra")
	prefixo := flag.String("prefixo", "mail/", "prefixo a migrar")
	flag.Parse()

	origem, err := minio.New(env("ORIGEM_ENDPOINT"), &minio.Options{
		Creds:  credentials.NewStaticV4(env("ORIGEM_ACCESS_KEY"), env("ORIGEM_SECRET_KEY"), ""),
		Secure: true, Region: "auto",
	})
	if err != nil {
		log.Fatalf("origem: %v", err)
	}
	baldeOrigem := env("ORIGEM_BUCKET")

	ssl, _ := strconv.ParseBool(envOu("DESTINO_SSL", "true"))
	destino, err := storage.New(env("DESTINO_ENDPOINT"), env("DESTINO_ACCESS_KEY"),
		env("DESTINO_SECRET_KEY"), env("DESTINO_BUCKET"), envOu("DESTINO_REGION", "auto"), ssl)
	if err != nil {
		log.Fatalf("destino: %v", err)
	}
	if err := destino.EnableEncryption(nil, env("FILE_ENC_KEYS")); err != nil {
		log.Fatalf("cifra: %v", err)
	}
	if !destino.Encrypting() {
		log.Fatal("sem FILE_ENC_KEYS não vale a pena migrar — o objetivo é chegar lá cifrado")
	}

	ctx := context.Background()
	var vistos, copiados, jaCifrados, falhas int

	for obj := range origem.ListObjects(ctx, baldeOrigem, minio.ListObjectsOptions{
		Prefix: *prefixo, Recursive: true,
	}) {
		if obj.Err != nil {
			log.Fatalf("listar: %v", obj.Err)
		}
		if obj.Size == 0 {
			continue
		}
		vistos++

		// Já lá está cifrado? Então não se toca: repetir a migração tem de ser inofensivo.
		if cifrado, err := destino.IsEncrypted(ctx, obj.Key); err == nil && cifrado {
			jaCifrados++
			continue
		}

		if !*fazer {
			log.Printf("  copiaria %s (%d bytes)", obj.Key, obj.Size)
			copiados++
			continue
		}
		if err := copiar(ctx, origem, baldeOrigem, destino, obj.Key, obj.Size); err != nil {
			log.Printf("  ✗ %s: %v", obj.Key, err)
			falhas++
			continue
		}
		copiados++
		log.Printf("  ✓ %s (%d bytes, cifrado)", obj.Key, obj.Size)
	}

	verbo := "por copiar"
	if *fazer {
		verbo = "copiados"
	}
	log.Printf("%d objetos na origem, %d %s, %d já estavam cifrados, %d falhas",
		vistos, copiados, verbo, jaCifrados, falhas)
	if falhas > 0 {
		os.Exit(1)
	}
}

func copiar(ctx context.Context, origem *minio.Client, balde string, destino *storage.Service, key string, tamanho int64) error {
	src, err := origem.GetObject(ctx, balde, key, minio.GetObjectOptions{})
	if err != nil {
		return err
	}
	defer src.Close()
	return destino.Put(ctx, key, src, tamanho, "")
}

func env(nome string) string {
	v := os.Getenv(nome)
	if v == "" {
		log.Fatalf("%s em falta", nome)
	}
	return v
}

func envOu(nome, omissao string) string {
	if v := os.Getenv(nome); v != "" {
		return v
	}
	return omissao
}
