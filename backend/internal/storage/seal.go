package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

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
//
// Um objeto mais curto do que o cabeçalho não pode estar cifrado, e o armazenamento responde a esse
// pedido com 416 (intervalo impossível). Isso não é uma falha — é a resposta. Apareceu no primeiro
// dia em produção, com os marcadores de pasta do Drive, que têm zero bytes.
func (s *Service) IsEncrypted(ctx context.Context, key string) (bool, error) {
	opts := minio.GetObjectOptions{}
	if err := opts.SetRange(0, int64(filecrypt.FixedHeaderSize)-1); err != nil {
		return false, err
	}
	obj, err := s.client.GetObject(ctx, s.bucket, key, opts)
	if err != nil {
		if curtoDemais(err) {
			return false, nil
		}
		return false, err
	}
	defer obj.Close()

	buf := make([]byte, filecrypt.FixedHeaderSize)
	n, err := io.ReadFull(obj, buf)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		if curtoDemais(err) {
			return false, nil
		}
		return false, err
	}
	_, perr := filecrypt.ParseHeader(buf[:n])
	if errors.Is(perr, filecrypt.ErrNotEncrypted) {
		return false, nil
	}
	return perr == nil, perr
}

// curtoDemais reconhece o 416 do armazenamento: pediu-se um intervalo que o objeto não tem.
func curtoDemais(err error) bool {
	var resp minio.ErrorResponse
	if errors.As(err, &resp) {
		return resp.StatusCode == http.StatusRequestedRangeNotSatisfiable ||
			resp.Code == "InvalidRange"
	}
	return false
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
	//
	// O tipo de conteúdo NÃO é preservado, de propósito: o Put guarda tudo como octet-stream quando
	// cifra. Dizer ao fornecedor que aquele objeto opaco é um `video/mp4` seria entregar-lhe de
	// volta parte do que a cifra acabou de esconder — e quem serve o ficheiro tira o tipo do nome
	// guardado na nossa base de dados, não do armazenamento.
	if err := s.Put(ctx, key, tmp, tamanho, ""); err != nil {
		return fmt.Errorf("selar %q: reenviar: %w", key, err)
	}
	return nil
}

// Object é o que a listagem entrega. O tamanho vem junto porque a decisão de selar depende dele:
// um objeto de zero bytes não tem conteúdo para proteger.
type Object struct {
	Key  string
	Size int64
}

// Selavel diz se vale a pena selar este objeto.
//
// Fica de fora o que não é conteúdo: os marcadores de pasta do Drive (chave terminada em barra,
// zero bytes). Cifrá-los trocaria um marcador de zero bytes por um objeto de ~90 bytes, e um
// marcador de pasta não tem nada que esconder.
func (o Object) Selavel() bool {
	return o.Size > 0 && !strings.HasSuffix(o.Key, "/")
}

// ListObjects percorre o balde. `prefix` vazio percorre tudo.
func (s *Service) ListObjects(ctx context.Context, prefix string) <-chan Object {
	out := make(chan Object)
	go func() {
		defer close(out)
		for obj := range s.client.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{
			Prefix: prefix, Recursive: true,
		}) {
			if obj.Err != nil {
				return
			}
			select {
			case out <- Object{Key: obj.Key, Size: obj.Size}:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out
}
