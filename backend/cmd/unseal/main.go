// unseal desfaz uma selagem: decifra objetos e volta a escrevê-los em claro no mesmo sítio.
//
// PARA QUE SERVE. A passagem de selagem cifrou, à primeira corrida em produção, 43 mensagens que o
// Stalwart guarda no mesmo balde e lê diretamente, sem passar pelo nosso código. Cifrar o ficheiro
// de outro processo não é segurança acrescentada, é uma avaria — e desfaz-se aqui.
//
// Fica no repositório em vez de ser um script deitado fora: se voltar a acontecer com outro
// vizinho do balde, quer-se a reversão pronta e não um programa escrito à pressa durante a avaria.
//
//	FILE_ENC_KEYS=… MINIO_… go run ./cmd/unseal <prefixo>          # mostra o que faria
//	FILE_ENC_KEYS=… MINIO_… go run ./cmd/unseal --apply <prefixo>  # faz
//
// Não corre sem prefixo: o comando que desfaz a cifra de tudo não deve existir por engano.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strconv"

	"github.com/beskosorg/beskos/backend/internal/storage"
)

func main() {
	apply := flag.Bool("apply", false, "actually write; without this it only reports")
	flag.Parse()

	prefix := flag.Arg(0)
	if prefix == "" {
		log.Fatal("usage: unseal [--apply] <prefix>")
	}

	ssl, _ := strconv.ParseBool(os.Getenv("MINIO_SSL"))
	st, err := storage.New(os.Getenv("MINIO_ENDPOINT"), os.Getenv("MINIO_ACCESS_KEY"),
		os.Getenv("MINIO_SECRET_KEY"), os.Getenv("MINIO_BUCKET"), os.Getenv("MINIO_REGION"), ssl)
	if err != nil {
		log.Fatalf("armazenamento: %v", err)
	}
	// Sem base de dados: só se desfazem objetos embrulhados pela KEK, que é o caso dos vizinhos do
	// balde. Um objeto de um tenant precisaria da chave do tenant e não é o que este comando faz.
	if err := st.EnableEncryption(nil, os.Getenv("FILE_ENC_KEYS")); err != nil {
		log.Fatalf("cifra: %v", err)
	}

	ctx := context.Background()
	var seen, done int
	for obj := range st.ListObjects(ctx, prefix) {
		seen++
		cifrado, err := st.IsEncrypted(ctx, obj.Key)
		if err != nil {
			log.Printf("%s: %v", obj.Key, err)
			continue
		}
		if !cifrado {
			continue
		}
		if !*apply {
			fmt.Printf("  decifraria %s (%d bytes)\n", obj.Key, obj.Size)
			done++
			continue
		}
		if err := undo(ctx, st, obj.Key); err != nil {
			log.Printf("%s: %v", obj.Key, err)
			continue
		}
		done++
		fmt.Printf("  %s reposto em claro\n", obj.Key)
	}

	verb := "por decifrar"
	if *apply {
		verb = "repostos em claro"
	}
	fmt.Printf("%d objetos vistos em %q, %d %s\n", seen, prefix, done, verb)
}

// desfazer lê o objeto decifrado e reescreve-o em claro na mesma chave. Uma escrita, como a
// selagem: o PUT ou acontece por inteiro ou não acontece.
func undo(ctx context.Context, st *storage.Service, key string) error {
	r, err := st.Get(ctx, key)
	if err != nil {
		return fmt.Errorf("read: %w", err)
	}
	defer r.Close()

	tmp, err := os.CreateTemp("", "unseal-*")
	if err != nil {
		return err
	}
	defer func() {
		tmp.Close()
		os.Remove(tmp.Name())
	}()

	n, err := io.Copy(tmp, r)
	if err != nil {
		return fmt.Errorf("decifrar: %w", err)
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return err
	}
	return st.PutPlain(ctx, key, tmp, n, "")
}
