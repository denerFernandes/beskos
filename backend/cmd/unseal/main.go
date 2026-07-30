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
//	FILE_ENC_KEYS=… MINIO_… go run ./cmd/unseal --fazer <prefixo>  # faz
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

	"mailin/backend/internal/storage"
)

func main() {
	fazer := flag.Bool("fazer", false, "escreve mesmo; sem isto só mostra")
	flag.Parse()

	prefixo := flag.Arg(0)
	if prefixo == "" {
		log.Fatal("uso: unseal [--fazer] <prefixo>")
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
	var vistos, feitos int
	for obj := range st.ListObjects(ctx, prefixo) {
		vistos++
		cifrado, err := st.IsEncrypted(ctx, obj.Key)
		if err != nil {
			log.Printf("%s: %v", obj.Key, err)
			continue
		}
		if !cifrado {
			continue
		}
		if !*fazer {
			fmt.Printf("  decifraria %s (%d bytes)\n", obj.Key, obj.Size)
			feitos++
			continue
		}
		if err := desfazer(ctx, st, obj.Key); err != nil {
			log.Printf("%s: %v", obj.Key, err)
			continue
		}
		feitos++
		fmt.Printf("  %s reposto em claro\n", obj.Key)
	}

	verbo := "por decifrar"
	if *fazer {
		verbo = "repostos em claro"
	}
	fmt.Printf("%d objetos vistos em %q, %d %s\n", vistos, prefixo, feitos, verbo)
}

// desfazer lê o objeto decifrado e reescreve-o em claro na mesma chave. Uma escrita, como a
// selagem: o PUT ou acontece por inteiro ou não acontece.
func desfazer(ctx context.Context, st *storage.Service, key string) error {
	r, err := st.Get(ctx, key)
	if err != nil {
		return fmt.Errorf("ler: %w", err)
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
