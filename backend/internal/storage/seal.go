package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/minio/minio-go/v7"

	"mailin/backend/internal/filecrypt"
)

// Selagem: pegar num objeto que está em claro e reescrevê-lo cifrado.
//
// É a MESMA operação para os dois casos que faltavam, e por isso é uma peça só:
//
//   - os objetos que já estavam no balde antes de a cifra existir;
//   - as gravações compostas, que o LiveKit Egress escreve diretamente no armazenamento com as
//     credenciais S3, sem passar pelo nosso backend (docs/cifra-ficheiros.md §3a).
//
// UMA ESCRITA. Reescreve-se a MESMA chave: o PUT do armazenamento é atómico, quem estiver a ler
// nesse instante recebe a versão antiga ou a nova, e ambas se leem. Não há apagar a seguir, não há
// chave a atualizar noutro sítio, não há marca na base de dados a acertar.
//
// IDEMPOTENTE POR CONSTRUÇÃO. O que decide se um objeto já está selado é o próprio objeto — a magia
// no cabeçalho — e não uma coluna que possa dessincronizar. Selar duas vezes cifraria o cifrado, e
// uma marca na base de dados que falhasse a escrever levaria exatamente a isso.

// IsEncrypted diz se o objeto já está cifrado por nós. Lê só o cabeçalho, com um pedido Range.
func (s *Service) IsEncrypted(ctx context.Context, key string) (bool, error) {
	opts := minio.GetObjectOptions{}
	if err := opts.SetRange(0, int64(filecrypt.FixedHeaderSize)-1); err != nil {
		return false, err
	}
	obj, err := s.client.GetObject(ctx, s.bucket, key, opts)
	if err != nil {
		return false, err
	}
	defer obj.Close()

	buf := make([]byte, filecrypt.FixedHeaderSize)
	n, err := io.ReadFull(obj, buf)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return false, err
	}
	_, perr := filecrypt.ParseHeader(buf[:n])
	if errors.Is(perr, filecrypt.ErrNotEncrypted) {
		return false, nil
	}
	return perr == nil, perr
}

// Seal cifra um objeto que esteja em claro, reescrevendo-o no mesmo sítio. Não faz nada se já
// estiver cifrado ou se a cifra estiver desligada.
//
// O conteúdo passa por um ficheiro temporário em vez de ir direto de leitura para escrita: não se
// pode ler e escrever a mesma chave ao mesmo tempo, e um objeto pode ter gigabytes que não cabem em
// memória.
func (s *Service) Seal(ctx context.Context, key string) error {
	if s.keys == nil {
		return errors.New("storage: selagem pedida com a cifra desligada")
	}
	cifrado, err := s.IsEncrypted(ctx, key)
	if err != nil {
		return err
	}
	if cifrado {
		return nil
	}

	obj, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return err
	}
	defer obj.Close()

	tmp, err := os.CreateTemp("", "seal-*")
	if err != nil {
		return err
	}
	defer func() {
		tmp.Close()
		os.Remove(tmp.Name())
	}()

	tamanho, err := io.Copy(tmp, obj)
	if err != nil {
		return fmt.Errorf("selar %q: descarregar: %w", key, err)
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return err
	}

	// Put cifra e reescreve a mesma chave. Uma escrita, atómica.
	if err := s.Put(ctx, key, tmp, tamanho, "application/octet-stream"); err != nil {
		return fmt.Errorf("selar %q: reenviar: %w", key, err)
	}
	return nil
}

// ListKeys percorre o balde e entrega as chaves dos objetos. `prefix` vazio percorre tudo.
func (s *Service) ListKeys(ctx context.Context, prefix string) <-chan string {
	out := make(chan string)
	go func() {
		defer close(out)
		for obj := range s.client.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{
			Prefix: prefix, Recursive: true,
		}) {
			if obj.Err != nil {
				return
			}
			select {
			case out <- obj.Key:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out
}
