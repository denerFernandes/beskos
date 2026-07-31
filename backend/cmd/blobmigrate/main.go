// blobmigrate copia os blocos do correio do balde antigo para o novo, cifrando-os à passagem.
//
// PORQUÊ. O Stalwart escrevia em `beskos`, um balde SEM restrição jurisdicional, e passou a escrever
// através do gateway em `beskos-data`, que tem jurisdição europeia. Os blocos antigos ficaram para
// trás: uma mensagem antiga aberta por um cliente IMAP procura o original no balde novo e não o
// encontra. Isto traz esses.
//
//	blobmigrate            # mostra o que faria
//	blobmigrate --apply    # copia
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
	run := flag.Bool("apply", false, "actually copy; without this it only reports")
	prefix := flag.String("prefixo", "mail/", "prefixo a migrar")
	flag.Parse()

	source, err := minio.New(env("ORIGEM_ENDPOINT"), &minio.Options{
		Creds:  credentials.NewStaticV4(env("ORIGEM_ACCESS_KEY"), env("ORIGEM_SECRET_KEY"), ""),
		Secure: true, Region: "auto",
	})
	if err != nil {
		log.Fatalf("source: %v", err)
	}
	sourceBucket := env("ORIGEM_BUCKET")

	ssl, _ := strconv.ParseBool(envOr("DESTINO_SSL", "true"))
	dest, err := storage.New(env("DESTINO_ENDPOINT"), env("DESTINO_ACCESS_KEY"),
		env("DESTINO_SECRET_KEY"), env("DESTINO_BUCKET"), envOr("DESTINO_REGION", "auto"), ssl)
	if err != nil {
		log.Fatalf("destino: %v", err)
	}
	if err := dest.EnableEncryption(nil, env("FILE_ENC_KEYS")); err != nil {
		log.Fatalf("cifra: %v", err)
	}
	if !dest.Encrypting() {
		log.Fatal("without FILE_ENC_KEYS there is no point migrating — the goal is to arrive encrypted")
	}

	ctx := context.Background()
	var seen, copied, alreadyEncrypted, failures int

	for obj := range source.ListObjects(ctx, sourceBucket, minio.ListObjectsOptions{
		Prefix: *prefix, Recursive: true,
	}) {
		if obj.Err != nil {
			log.Fatalf("list: %v", obj.Err)
		}
		if obj.Size == 0 {
			continue
		}
		seen++

		// Já lá está cifrado? Então não se toca: repetir a migração tem de ser inofensivo.
		if cifrado, err := dest.IsEncrypted(ctx, obj.Key); err == nil && cifrado {
			alreadyEncrypted++
			continue
		}

		if !*run {
			log.Printf("  copiaria %s (%d bytes)", obj.Key, obj.Size)
			copied++
			continue
		}
		if err := copyObject(ctx, source, sourceBucket, dest, obj.Key, obj.Size); err != nil {
			log.Printf("  ✗ %s: %v", obj.Key, err)
			failures++
			continue
		}
		copied++
		log.Printf("  ✓ %s (%d bytes, encrypted)", obj.Key, obj.Size)
	}

	verb := "por copiar"
	if *run {
		verb = "copiados"
	}
	log.Printf("%d objects at the source, %d %s, %d were already encrypted, %d failures",
		seen, copied, verb, alreadyEncrypted, failures)
	if failures > 0 {
		os.Exit(1)
	}
}

func copyObject(ctx context.Context, source *minio.Client, bucket string, dest *storage.Service, key string, size int64) error {
	src, err := source.GetObject(ctx, bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return err
	}
	defer src.Close()
	return dest.Put(ctx, key, src, size, "")
}

func env(name string) string {
	v := os.Getenv(name)
	if v == "" {
		log.Fatalf("%s missing", name)
	}
	return v
}

func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}
