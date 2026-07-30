// Package storage é o serviço de arquivos sobre MinIO (S3-compatível, self-hosted, brief §3).
// Isolamento por tenant via prefixo de chave (tenant_id/…) e checagem no control plane.
// Credenciais só no backend (lei §10).
package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"mailin/backend/internal/filecrypt"
)

type Service struct {
	client *minio.Client
	bucket string
	// keys é nil quando a cifra está desligada: nesse caso o serviço escreve em claro, como antes.
	// Ver crypto.go e docs/cifra-ficheiros.md.
	keys *keyring
}

// New conecta a um storage S3-compatível e garante o bucket. Funciona com MinIO (endpoint
// "minio:9000", region "") E com Cloudflare R2 (endpoint "<acct>.r2.cloudflarestorage.com",
// useSSL=true, region "auto"). No R2 o bucket é pré-criado no painel — se não conseguirmos criá-lo
// (o R2 restringe o CreateBucket por S3), assumimos que já existe em vez de rebentar.
func New(endpoint, accessKey, secretKey, bucket, region string, useSSL bool) (*Service, error) {
	c, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: useSSL,
		Region: region, // R2 assina com "auto"; MinIO fica com "" (default)
	})
	if err != nil {
		return nil, err
	}
	s := &Service{client: c, bucket: bucket}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	exists, err := c.BucketExists(ctx, bucket)
	if err != nil {
		return nil, err
	}
	if !exists {
		// No R2 os buckets criam-se no painel; o CreateBucket via S3 pode ser negado — não é fatal.
		if err := c.MakeBucket(ctx, bucket, minio.MakeBucketOptions{Region: region}); err != nil {
			return nil, fmt.Errorf("bucket %q não existe e não foi possível criá-lo (cria-o no painel do storage): %w", bucket, err)
		}
	}
	return s, nil
}

// Put envia um blob. key deve ser prefixado por tenant (ex.: "<tenant>/<hash>").
//
// Com a cifra ligada, o que chega ao armazenamento é cifrado e o fornecedor nunca vê o conteúdo. É
// UMA escrita: a chave do objeto viaja dentro do próprio objeto, embrulhada, e por isso não há uma
// segunda escrita para ordenar — ver docs/cifra-ficheiros.md §2d.
func (s *Service) Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	if s.keys == nil {
		_, err := s.client.PutObject(ctx, s.bucket, key, r, size, minio.PutObjectOptions{ContentType: contentType})
		return err
	}

	dek, h, err := s.keys.wrapForKey(ctx, key)
	if err != nil {
		return fmt.Errorf("chave do objeto %q: %w", key, err)
	}

	// O cifrado sai de um pipe enquanto o envio o consome: um ficheiro de 2 GB nunca fica em
	// memória nem em disco.
	pr, pw := io.Pipe()
	go func() {
		pw.CloseWithError(filecrypt.EncryptTo(pw, r, size, dek, h))
	}()
	defer pr.Close()

	cifrado := filecrypt.EncryptedSize(size, h.ChunkSize, h.WrappedKeyLen())
	// O tipo de conteúdo deixa de ir para o armazenamento: é metadado sobre o que está lá dentro, e
	// o que está lá dentro passou a ser opaco. Quem serve o ficheiro já o sabe pela base de dados.
	_, err = s.client.PutObject(ctx, s.bucket, key, pr, cifrado, minio.PutObjectOptions{
		ContentType: "application/octet-stream",
	})
	return err
}

// PutPlain escreve SEM cifrar, mesmo com a cifra ligada. Existe para desfazer uma selagem feita por
// engano (ver cmd/unseal) — um objeto que afinal é de outro processo, que o lê diretamente do balde
// e não sabe do nosso formato. Não tem outro uso legítimo: o caminho normal é o Put.
func (s *Service) PutPlain(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	_, err := s.client.PutObject(ctx, s.bucket, key, r, size, minio.PutObjectOptions{ContentType: contentType})
	return err
}

// Get devolve o conteúdo do blob para streaming ao cliente, já decifrado quando for o caso. Um
// objeto anterior à cifra sai tal como está — a deteção é pela magia no cabeçalho, não por
// configuração.
func (s *Service) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	obj, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	return s.abre(ctx, key, obj)
}

// GetSeek devolve o objeto como ReadSeekCloser (o *minio.Object busca por Range/StatObject). Serve
// para o http.ServeContent honrar o cabeçalho Range — player de vídeo com seek (avançar/retroceder).
//
// Com o objeto cifrado, o seek passa a ser sobre o conteúdo EM CLARO: o leitor traduz a posição
// pedida em blocos e vai buscar só esses ao armazenamento. Um salto para o meio de uma gravação de
// duas horas lê um bloco, não a gravação inteira.
func (s *Service) GetSeek(ctx context.Context, key string) (io.ReadSeekCloser, error) {
	obj, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}

	h, _, perr := lerCabecalho(obj)
	if errors.Is(perr, filecrypt.ErrNotEncrypted) {
		// Objeto anterior à cifra: rebobina e devolve-o tal como está.
		if _, serr := obj.Seek(0, io.SeekStart); serr != nil {
			obj.Close()
			return nil, serr
		}
		return obj, nil
	}
	if perr != nil {
		obj.Close()
		return nil, perr
	}
	if s.keys == nil {
		obj.Close()
		return nil, errors.New("storage: objeto cifrado mas a cifra está desligada (falta FILE_ENC_KEYS)")
	}
	dek, err := s.keys.unwrapFromHeader(ctx, key, h)
	if err != nil {
		obj.Close()
		return nil, err
	}
	sr, err := filecrypt.NewSeekReader(obj, h, dek)
	if err != nil {
		obj.Close()
		return nil, err
	}
	return &seekCloser{SeekReader: sr, c: obj}, nil
}

// seekCloser junta o leitor com seek ao Closer do objeto do armazenamento.
type seekCloser struct {
	*filecrypt.SeekReader
	c io.Closer
}

func (s *seekCloser) Close() error { return s.c.Close() }

// Exists diz se o blob já existe (usado para dedup: não reenviar conteúdo idêntico).
func (s *Service) Exists(ctx context.Context, key string) (bool, error) {
	_, err := s.client.StatObject(ctx, s.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		resp := minio.ToErrorResponse(err)
		if resp.Code == "NoSuchKey" || resp.StatusCode == 404 {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// Remove apaga um blob (usar só quando nenhum registro lógico aponta mais para ele). Não há chave
// para limpar à parte: ela vivia dentro do objeto e desaparece com ele.
func (s *Service) Remove(ctx context.Context, key string) error {
	return s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{})
}
